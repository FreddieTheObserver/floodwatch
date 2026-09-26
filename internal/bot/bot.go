// Package bot is floodwatch's Telegram front end: it signs people up for
// places, answers their commands, and delivers alerts.
package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/FreddieTheObserver/floodwatch/internal/alert"
	"github.com/FreddieTheObserver/floodwatch/internal/store"
	"github.com/FreddieTheObserver/floodwatch/internal/telegram"
)

const (
	channel        = "telegram"
	defaultRadiusM = 5000

	pollTimeoutSeconds = 50
	maxRetryDelay      = time.Minute
)

type API interface {
	GetUpdates(ctx context.Context, offset int64, timeoutSeconds int) ([]telegram.Update, error)
	SendMessage(ctx context.Context, m telegram.OutgoingMessage) error
	SendVenue(ctx context.Context, chatID int64, lat, lng float64, title, address string) error
	EditMessageText(ctx context.Context, chatID, messageID int64, text, parseMode string) error
	AnswerCallbackQuery(ctx context.Context, id string) error
	SetMyCommands(ctx context.Context, commands []telegram.Command, languageCode string) error
}

type Store interface {
	SaveSubscription(ctx context.Context, sub alert.Subscription) (alert.Subscription, error)
	RecipientSubscriptions(ctx context.Context, channel, recipient string) ([]alert.Subscription, error)
	RenameSubscription(ctx context.Context, channel, recipient, from, to string) (bool, error)
	RemoveSubscription(ctx context.Context, channel, recipient, label string) (bool, error)
	ForgetRecipient(ctx context.Context, channel, recipient string) (int64, error)
	RecipientLanguage(ctx context.Context, channel, recipient string) (string, error)
	SetRecipientLanguage(ctx context.Context, channel, recipient, language string) error
	LastAlertRun(ctx context.Context) (time.Time, error)
	RecordAlertRun(ctx context.Context, at time.Time) error
}

type Evaluator interface {
	Evaluate(ctx context.Context) ([]alert.Digest, error)
	Ack(ctx context.Context, d alert.Digest) error
	Status(ctx context.Context, sub alert.Subscription) (alert.Assessment, error)
	Baseline(ctx context.Context, sub alert.Subscription) error
}

type Bot struct {
	api   API
	store Store
	eval  Evaluator
	log   *slog.Logger
	now   func() time.Time

	offlineAfter time.Duration

	// lastContact is when Telegram last answered a long poll, in Unix
	// nanoseconds; zero while Run has not started. The poll loop writes it and
	// the health check reads it from another goroutine.
	lastContact atomic.Int64
}

// A long poll returns at least every pollTimeoutSeconds, so this long without
// an answer means commands are no longer getting through.
const maxSilence = 5 * time.Minute

// Healthy reports whether the bot is still in contact with Telegram.
func (b *Bot) Healthy() error {
	last := b.lastContact.Load()
	if last == 0 {
		return nil
	}
	if silence := b.now().Sub(time.Unix(0, last)); silence > maxSilence {
		return fmt.Errorf("no contact with Telegram for %s", silence.Round(time.Minute))
	}
	return nil
}

// New makes a bot whose Notify runs after every collector poll, pollInterval
// apart.
func New(api API, store Store, eval Evaluator, log *slog.Logger, pollInterval time.Duration) *Bot {
	return &Bot{api: api, store: store, eval: eval, log: log, now: time.Now, offlineAfter: offlineAfter(pollInterval)}
}

// Run answers incoming messages until ctx is done. It returns an error only
// when Telegram rejects the token, which no retry can fix.
func (b *Bot) Run(ctx context.Context) error {
	// The English menu is the default; Thai apps get the Thai one.
	for _, set := range []struct {
		t    *texts
		code string
	}{{&english, ""}, {&thai, thai.Code}} {
		if err := b.api.SetMyCommands(ctx, set.t.Commands, set.code); err != nil {
			if telegram.Unauthorized(err) {
				return fmt.Errorf("telegram rejected the bot token: %w", err)
			}
			b.log.Warn("set bot commands failed", "language", set.t.Code, "err", err)
		}
	}

	var offset int64
	delay := time.Second
	b.lastContact.Store(b.now().UnixNano())
	for ctx.Err() == nil {
		pollCtx, cancel := context.WithTimeout(ctx, (pollTimeoutSeconds+15)*time.Second)
		updates, err := b.api.GetUpdates(pollCtx, offset, pollTimeoutSeconds)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			if telegram.Unauthorized(err) {
				return fmt.Errorf("telegram rejected the bot token: %w", err)
			}
			b.log.Warn("get updates failed", "err", err, "retry_in", delay.String())
			sleep(ctx, delay)
			delay = min(delay*2, maxRetryDelay)
			continue
		}
		delay = time.Second
		b.lastContact.Store(b.now().UnixNano())
		for _, u := range updates {
			offset = u.UpdateID + 1
		}
		b.handleAll(ctx, updates)
	}
	return nil
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// A message this old on arrival waited while floodwatch was not running, as
// Telegram holds messages for a bot for up to a day. The margin allows for the
// computer's clock being slightly off after it wakes.
const lateAfter = 5 * time.Minute

// handleAll answers a batch of updates. Anyone whose message waited while
// floodwatch was not running first gets one apology for the batch.
func (b *Bot) handleAll(ctx context.Context, updates []telegram.Update) {
	apologised := map[int64]bool{}
	for _, u := range updates {
		if m := u.Message; m != nil && m.Date > 0 && m.Chat.Type == "private" && !apologised[m.Chat.ID] {
			if sent, now := time.Unix(m.Date, 0), b.now(); now.Sub(sent) > lateAfter {
				apologised[m.Chat.ID] = true
				c := b.converse(ctx, m.Chat.ID, m.From)
				b.send(ctx, c, c.t.LateReply(c.t.When(sent, now)), nil)
			}
		}
		b.handle(ctx, u)
	}
}

// handle contains a panic to the one update that caused it, so a bad message
// cannot take the collector down with the bot.
func (b *Bot) handle(ctx context.Context, u telegram.Update) {
	defer func() {
		if r := recover(); r != nil {
			b.log.Error("update handler panicked", "update_id", u.UpdateID, "panic", r)
		}
	}()
	switch {
	case u.Message != nil:
		b.onMessage(ctx, u.Message)
	case u.CallbackQuery != nil:
		b.onCallback(ctx, u.CallbackQuery)
	}
}

func recipient(chatID int64) string { return strconv.FormatInt(chatID, 10) }

// conversation is who the bot is answering and in which language.
type conversation struct {
	id int64
	t  *texts
}

// converse opens a conversation in the person's language: the one they chose,
// or else their Telegram app's, which is then remembered so that alerts,
// which come with no app language, use it too.
func (b *Bot) converse(ctx context.Context, chatID int64, from *telegram.User) conversation {
	c := conversation{id: chatID, t: b.languageOf(ctx, recipient(chatID))}
	if c.t != nil {
		return c
	}
	code := ""
	if from != nil {
		code = from.LanguageCode
	}
	c.t = languageFor(code)
	if err := b.store.SetRecipientLanguage(ctx, channel, recipient(chatID), c.t.Code); err != nil {
		b.log.Warn("remember language failed", "err", err)
	}
	return c
}

// languageOf is a person's stored language, or nil if they have none.
func (b *Bot) languageOf(ctx context.Context, rcpt string) *texts {
	code, err := b.store.RecipientLanguage(ctx, channel, rcpt)
	if err != nil {
		b.log.Warn("read language failed", "err", err)
	}
	return languages[code]
}

func (b *Bot) onMessage(ctx context.Context, m *telegram.Message) {
	// Alerts are personal, and a group would expose every member's places.
	if m.Chat.Type != "private" {
		return
	}
	c := b.converse(ctx, m.Chat.ID, m.From)
	if m.Location != nil {
		b.onLocation(ctx, c, m.Location.Latitude, m.Location.Longitude)
		return
	}
	text := strings.TrimSpace(m.Text)
	// A command wins even as a reply to a naming question, so /status still
	// works from the reply box that question opens.
	if cmd := command(text); cmd != "" {
		b.onCommand(ctx, c, cmd)
		return
	}
	if label, ok := renameTarget(m.ReplyToMessage); ok {
		b.rename(ctx, c, label, text)
		return
	}
	if lat, lng, ok := parseCoords(text); ok {
		b.onLocation(ctx, c, lat, lng)
		return
	}
	b.send(ctx, c, c.t.Hint, locationKeyboard(c.t))
}

func (b *Bot) onCommand(ctx context.Context, c conversation, cmd string) {
	switch cmd {
	case "/start", "/help":
		b.send(ctx, c, c.t.Welcome+"\n\n"+c.t.Disclaimer, locationKeyboard(c.t))
	case "/status":
		b.statusAll(ctx, c)
	case "/places":
		b.places(ctx, c)
	case "/language":
		b.send(ctx, c, c.t.LanguagePrompt, telegram.InlineKeyboard{
			InlineKeyboard: [][]telegram.InlineButton{{
				{Text: "🇹🇭 ไทย", CallbackData: "lang:" + thai.Code},
				{Text: "🇬🇧 English", CallbackData: "lang:" + english.Code},
			}},
		})
	case "/stop":
		b.send(ctx, c, c.t.StopConfirm, telegram.InlineKeyboard{
			InlineKeyboard: [][]telegram.InlineButton{{
				{Text: c.t.DeleteEverything, CallbackData: "stop:yes"},
				{Text: c.t.Cancel, CallbackData: "stop:no"},
			}},
		})
	default:
		b.send(ctx, c, c.t.Hint, locationKeyboard(c.t))
	}
}

func command(text string) string {
	fields := strings.Fields(text)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return ""
	}
	cmd, _, _ := strings.Cut(fields[0], "@")
	return strings.ToLower(cmd)
}

var coordsPattern = regexp.MustCompile(`^(-?\d{1,2}(?:\.\d+)?)\s*,\s*(-?\d{1,3}(?:\.\d+)?)$`)

// parseCoords accepts "lat, lng" as copied from a map app, for people on
// Telegram Desktop, which cannot share a location.
func parseCoords(s string) (lat, lng float64, ok bool) {
	m := coordsPattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, 0, false
	}
	lat, errLat := strconv.ParseFloat(m[1], 64)
	lng, errLng := strconv.ParseFloat(m[2], 64)
	if errLat != nil || errLng != nil || lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return 0, 0, false
	}
	return lat, lng, true
}

// Five decimals is about a metre, finer than any gauge network, and keeps
// coordinates short enough for Telegram's 64-byte button data.
func coords(lat, lng float64) string { return fmt.Sprintf("%.5f,%.5f", lat, lng) }

func round5(v float64) float64 { return math.Round(v*1e5) / 1e5 }

func (b *Bot) onLocation(ctx context.Context, c conversation, lat, lng float64) {
	lat, lng = round5(lat), round5(lng)
	candidate, err := b.eval.Status(ctx, alert.Subscription{Channel: channel, Recipient: recipient(c.id), Lat: lat, Lng: lng, RadiusM: defaultRadiusM})
	if err != nil {
		b.fail(ctx, c, "check a location", err)
		return
	}
	if !covered(candidate.Findings) {
		b.send(ctx, c, c.t.NotCovered, nil)
		return
	}

	places, err := b.store.RecipientSubscriptions(ctx, channel, recipient(c.id))
	if err != nil {
		b.fail(ctx, c, "list places", err)
		return
	}
	if len(places) == 0 {
		b.savePlace(ctx, c, c.t.DefaultHome, lat, lng)
		return
	}

	var rows [][]telegram.InlineButton
	labels := make([]string, len(places))
	for i, p := range places {
		labels[i] = esc(p.Label)
		rows = append(rows, []telegram.InlineButton{{Text: c.t.MoveHere(p.Label), CallbackData: fmt.Sprintf("mv:%d:%s", p.ID, coords(lat, lng))}})
	}
	if len(places) < store.MaxPlaces {
		rows = append(rows, []telegram.InlineButton{{Text: c.t.AddNew, CallbackData: "add:" + coords(lat, lng)}})
	}
	b.send(ctx, c, c.t.AlreadyWatch(strings.Join(labels, ", ")), telegram.InlineKeyboard{InlineKeyboard: rows})
}

// savePlace reports whether the place was saved and its status delivered.
func (b *Bot) savePlace(ctx context.Context, c conversation, label string, lat, lng float64) (alert.Subscription, bool) {
	sub, err := b.store.SaveSubscription(ctx, alert.Subscription{
		Channel: channel, Recipient: recipient(c.id), Label: label, Lat: lat, Lng: lng, RadiusM: defaultRadiusM,
	})
	if errors.Is(err, store.ErrTooManyPlaces) {
		b.send(ctx, c, c.t.TooManyPlaces(store.MaxPlaces), nil)
		return sub, false
	}
	if err != nil {
		b.fail(ctx, c, "save a place", err)
		return sub, false
	}

	a, err := b.eval.Status(ctx, sub)
	if err != nil {
		b.fail(ctx, c, "read the gauges", err)
		return sub, false
	}
	if !b.send(ctx, c, c.t.Watching(esc(sub.Label))+statusText(c.t, sub, a, b.now()), mapButton(c.t, sub)) {
		return sub, false
	}
	// The subscriber has just seen all of this; without a baseline the next
	// evaluation would repeat it as alerts.
	if err := b.eval.Baseline(ctx, sub); err != nil {
		b.log.Error("baseline failed", "subscription", sub.ID, "err", err)
	}
	return sub, true
}

func (b *Bot) statusAll(ctx context.Context, c conversation) {
	places, err := b.store.RecipientSubscriptions(ctx, channel, recipient(c.id))
	if err != nil {
		b.fail(ctx, c, "list places", err)
		return
	}
	if len(places) == 0 {
		b.send(ctx, c, c.t.NoPlaces, locationKeyboard(c.t))
		return
	}
	for _, p := range places {
		a, err := b.eval.Status(ctx, p)
		if err != nil {
			b.fail(ctx, c, "read the gauges", err)
			return
		}
		b.send(ctx, c, statusText(c.t, p, a, b.now()), mapButton(c.t, p))
	}
}

func mapButton(t *texts, sub alert.Subscription) telegram.InlineKeyboard {
	return telegram.InlineKeyboard{InlineKeyboard: [][]telegram.InlineButton{{
		{Text: t.MapButton, CallbackData: fmt.Sprintf("map:%d", sub.ID)},
	}}}
}

func locationKeyboard(t *texts) telegram.ReplyKeyboard {
	return telegram.ReplyKeyboard{
		Keyboard:        [][]telegram.KeyboardButton{{{Text: t.ShareLocation, RequestLocation: true}}},
		ResizeKeyboard:  true,
		OneTimeKeyboard: true,
	}
}

// Pins beyond these add clutter without changing the picture.
const maxWaterPins = 3

type pin struct {
	lat, lng       float64
	title, address string
}

// mapPins are a place and the gauges its risk is judged from. Telegram cannot
// show a drawn map inside a message, but venues open in the phone's own maps
// app with no server of ours involved.
func mapPins(t *texts, place alert.Subscription, a alert.Assessment) []pin {
	pins := []pin{{place.Lat, place.Lng, "📍 " + place.Label, t.PinPlace}}
	water := 0
	for _, f := range a.Findings {
		switch {
		case f.Rule == alert.RuleWaterLevel && water < maxWaterPins:
			water++
			reading := t.PinNoData(t.Clock(f.At))
			if f.Known {
				reading = t.PinReading(t.Bank(f.LevelMSL, f.BankMSL), t.Clock(f.At))
			}
			pins = append(pins, pin{f.Station.Lat, f.Station.Lng, "🌊 " + t.stationName(f.Station),
				t.PinWater(t.Distance(f.DistanceM), place.Label, reading)})
		case f.Rule == alert.RuleRain && f.Known:
			pins = append(pins, pin{f.Station.Lat, f.Station.Lng, "🌧️ " + t.stationName(f.Station),
				t.PinRain(t.Distance(f.DistanceM), place.Label, rainAmounts(t, f), t.Clock(f.At))})
		}
	}
	return pins
}

func (b *Bot) showMap(ctx context.Context, c conversation, place alert.Subscription) {
	a, err := b.eval.Status(ctx, place)
	if err != nil {
		b.fail(ctx, c, "read the gauges", err)
		return
	}
	for _, p := range mapPins(c.t, place, a) {
		err := b.api.SendVenue(ctx, c.id, p.lat, p.lng, p.title, p.address)
		if telegram.Blocked(err) {
			b.forgetBlocked(ctx, recipient(c.id))
			return
		}
		if err != nil {
			b.log.Warn("send map pin failed", "chat", c.id, "err", err)
			return
		}
	}
}

func (b *Bot) places(ctx context.Context, c conversation) {
	places, err := b.store.RecipientSubscriptions(ctx, channel, recipient(c.id))
	if err != nil {
		b.fail(ctx, c, "list places", err)
		return
	}
	if len(places) == 0 {
		b.send(ctx, c, c.t.NoPlaces, locationKeyboard(c.t))
		return
	}
	var lines []string
	var rows [][]telegram.InlineButton
	for _, p := range places {
		lines = append(lines, fmt.Sprintf("• <b>%s</b> (%s)", esc(p.Label), coords(p.Lat, p.Lng)))
		rows = append(rows, []telegram.InlineButton{
			{Text: c.t.RenameButton(p.Label), CallbackData: fmt.Sprintf("rn:%d", p.ID)},
			{Text: c.t.RemoveButton(p.Label), CallbackData: fmt.Sprintf("rm:%d", p.ID)},
		})
	}
	b.send(ctx, c, c.t.PlacesList(strings.Join(lines, "\n")), telegram.InlineKeyboard{InlineKeyboard: rows})
}

func (b *Bot) onCallback(ctx context.Context, cq *telegram.CallbackQuery) {
	if err := b.api.AnswerCallbackQuery(ctx, cq.ID); err != nil {
		b.log.Debug("answer callback failed", "err", err)
	}
	if cq.Message == nil || cq.Message.Chat.Type != "private" {
		return
	}
	c := b.converse(ctx, cq.Message.Chat.ID, cq.From)
	prompt := cq.Message.MessageID
	action, arg, _ := strings.Cut(cq.Data, ":")

	switch action {
	case "add":
		lat, lng, ok := parseCoords(arg)
		if !ok {
			return
		}
		places, err := b.store.RecipientSubscriptions(ctx, channel, recipient(c.id))
		if err != nil {
			b.fail(ctx, c, "list places", err)
			return
		}
		label := nextLabel(c.t, places)
		b.edit(ctx, c, prompt, c.t.Adding(esc(label)))
		if sub, ok := b.savePlace(ctx, c, label, lat, lng); ok {
			b.send(ctx, c, c.t.RenamePrompt(esc(sub.Label)), renameReply(c.t))
		}

	case "mv":
		ref, at, _ := strings.Cut(arg, ":")
		lat, lng, ok := parseCoords(at)
		if !ok {
			return
		}
		place, ok := b.ownPlace(ctx, c, ref)
		if !ok {
			b.edit(ctx, c, prompt, c.t.PlaceGoneAdd)
			return
		}
		b.edit(ctx, c, prompt, c.t.Moving(esc(place.Label)))
		b.savePlace(ctx, c, place.Label, lat, lng)

	case "map":
		place, ok := b.ownPlace(ctx, c, arg)
		if !ok {
			b.send(ctx, c, c.t.PlaceGoneList, nil)
			return
		}
		b.showMap(ctx, c, place)

	case "rn":
		place, ok := b.ownPlace(ctx, c, arg)
		if !ok {
			b.send(ctx, c, c.t.PlaceGoneList, nil)
			return
		}
		b.send(ctx, c, c.t.RenamePrompt(esc(place.Label)), renameReply(c.t))

	case "rm":
		place, ok := b.ownPlace(ctx, c, arg)
		if !ok {
			b.edit(ctx, c, prompt, c.t.PlaceGone)
			return
		}
		removed, err := b.store.RemoveSubscription(ctx, channel, recipient(c.id), place.Label)
		if err != nil {
			b.fail(ctx, c, "remove a place", err)
			return
		}
		if removed {
			b.edit(ctx, c, prompt, c.t.Removed(esc(place.Label)))
		} else {
			b.edit(ctx, c, prompt, c.t.AlreadyRemoved(esc(place.Label)))
		}

	case "lang":
		t, ok := languages[arg]
		if !ok {
			return
		}
		if err := b.store.SetRecipientLanguage(ctx, channel, recipient(c.id), t.Code); err != nil {
			b.fail(ctx, c, "set the language", err)
			return
		}
		b.edit(ctx, conversation{id: c.id, t: t}, prompt, t.LanguageSet)

	case "stop":
		if arg != "yes" {
			b.edit(ctx, c, prompt, c.t.NothingDeleted)
			return
		}
		n, err := b.store.ForgetRecipient(ctx, channel, recipient(c.id))
		if err != nil {
			b.fail(ctx, c, "delete your data", err)
			return
		}
		b.edit(ctx, c, prompt, c.t.Forgot(n))
	}
}

// ownPlace resolves a button's reference to one of the subscriber's own places,
// so a button can never touch someone else's. Buttons carry the place ID,
// which stays within Telegram's 64-byte limit whatever the name; ones sent
// before that carried the name itself.
func (b *Bot) ownPlace(ctx context.Context, c conversation, ref string) (alert.Subscription, bool) {
	places, err := b.store.RecipientSubscriptions(ctx, channel, recipient(c.id))
	if err != nil {
		b.fail(ctx, c, "list places", err)
		return alert.Subscription{}, false
	}
	id, err := strconv.ParseInt(ref, 10, 64)
	for _, p := range places {
		if (err == nil && p.ID == id) || (err != nil && p.Label == ref) {
			return p, true
		}
	}
	return alert.Subscription{}, false
}

const maxLabelRunes = 30

func renameReply(t *texts) telegram.ForceReply {
	return telegram.ForceReply{ForceReply: true, InputFieldPlaceholder: t.RenamePlaceholder}
}

// The naming question as it was worded before it was translated, which may
// still be answered from older chats.
var legacyRenamePattern = regexp.MustCompile(`^What should I call (.+)\? Reply with a name`)

// renameTarget recognises a reply to the bot's naming question and returns the
// place it asked about. Telegram quotes that question back as plain text, and
// in every language it opens with the marker and the place's name, so no
// conversation state has to be kept.
func renameTarget(replyTo *telegram.Message) (string, bool) {
	if replyTo == nil || replyTo.From == nil || !replyTo.From.IsBot {
		return "", false
	}
	first, _, _ := strings.Cut(replyTo.Text, "\n")
	if label, ok := strings.CutPrefix(first, renameMarker+" "); ok && label != "" {
		return label, true
	}
	if m := legacyRenamePattern.FindStringSubmatch(replyTo.Text); m != nil {
		return m[1], true
	}
	return "", false
}

func (b *Bot) rename(ctx context.Context, c conversation, from, raw string) {
	name, problem := cleanLabel(c.t, raw)
	if problem != "" {
		b.send(ctx, c, problem+" "+c.t.RenameRetry, nil)
		return
	}
	places, err := b.store.RecipientSubscriptions(ctx, channel, recipient(c.id))
	if err != nil {
		b.fail(ctx, c, "rename a place", err)
		return
	}
	for _, p := range places {
		if p.Label != from && strings.EqualFold(p.Label, name) {
			b.send(ctx, c, c.t.NameTaken(esc(p.Label)), nil)
			return
		}
	}

	renamed, err := b.store.RenameSubscription(ctx, channel, recipient(c.id), from, name)
	switch {
	case errors.Is(err, store.ErrLabelTaken):
		b.send(ctx, c, c.t.NameTaken(esc(name)), nil)
	case err != nil:
		b.fail(ctx, c, "rename a place", err)
	case !renamed:
		b.send(ctx, c, c.t.PlaceMissing(esc(from)), nil)
	default:
		b.send(ctx, c, c.t.Renamed(esc(from), esc(name)), nil)
	}
}

// cleanLabel tidies a name typed by a subscriber, or says what is wrong with it.
func cleanLabel(t *texts, raw string) (name, problem string) {
	name = strings.Join(strings.Fields(raw), " ")
	switch {
	case !strings.ContainsFunc(name, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }):
		return "", t.NameNeedsLetter
	case utf8.RuneCountInString(name) > maxLabelRunes:
		return "", t.NameTooLong(maxLabelRunes)
	case strings.ContainsFunc(name, unicode.IsControl):
		return "", t.NameBadChars
	}
	return name, ""
}

// nextLabel names a new place: Home first, then Place 2, Place 3 and so on in
// the subscriber's language, reusing the lowest free number.
func nextLabel(t *texts, places []alert.Subscription) string {
	used := make([]string, len(places))
	for i, p := range places {
		used[i] = p.Label
	}
	if !slices.Contains(used, t.DefaultHome) {
		return t.DefaultHome
	}
	for n := 2; ; n++ {
		if label := t.DefaultPlace(n); !slices.Contains(used, label) {
			return label
		}
	}
}

// send reports whether the message was delivered. A recipient who blocked the
// bot can never be reached again, so everything stored about them is deleted.
func (b *Bot) send(ctx context.Context, c conversation, text string, markup any) bool {
	err := b.api.SendMessage(ctx, telegram.OutgoingMessage{ChatID: c.id, Text: text, ParseMode: "HTML", ReplyMarkup: markup})
	switch {
	case telegram.Blocked(err):
		b.forgetBlocked(ctx, recipient(c.id))
		return false
	case err != nil:
		b.log.Warn("send failed", "chat", c.id, "err", err)
		return false
	}
	return true
}

func (b *Bot) edit(ctx context.Context, c conversation, messageID int64, text string) {
	if err := b.api.EditMessageText(ctx, c.id, messageID, text, "HTML"); err != nil {
		b.log.Debug("edit failed", "chat", c.id, "err", err)
	}
}

func (b *Bot) fail(ctx context.Context, c conversation, doing string, err error) {
	b.log.Error("request failed", "chat", c.id, "doing", doing, "err", err)
	b.send(ctx, c, c.t.Failed, nil)
}

func (b *Bot) forgetBlocked(ctx context.Context, rcpt string) {
	n, err := b.store.ForgetRecipient(ctx, channel, rcpt)
	if err != nil {
		b.log.Error("forget blocked recipient failed", "err", err)
		return
	}
	b.log.Info("recipient blocked the bot; their places were deleted", "places", n)
}
