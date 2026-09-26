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
	EditMessageText(ctx context.Context, chatID, messageID int64, text, parseMode string) error
	AnswerCallbackQuery(ctx context.Context, id string) error
	SetMyCommands(ctx context.Context, commands []telegram.Command) error
}

type Store interface {
	SaveSubscription(ctx context.Context, sub alert.Subscription) (alert.Subscription, error)
	RecipientSubscriptions(ctx context.Context, channel, recipient string) ([]alert.Subscription, error)
	RenameSubscription(ctx context.Context, channel, recipient, from, to string) (bool, error)
	RemoveSubscription(ctx context.Context, channel, recipient, label string) (bool, error)
	ForgetRecipient(ctx context.Context, channel, recipient string) (int64, error)
}

type Evaluator interface {
	Pending(ctx context.Context) ([]alert.Digest, error)
	Ack(ctx context.Context, d alert.Digest) error
	Status(ctx context.Context, sub alert.Subscription) ([]alert.Finding, error)
	Baseline(ctx context.Context, sub alert.Subscription) error
}

type Bot struct {
	api   API
	store Store
	eval  Evaluator
	log   *slog.Logger
	now   func() time.Time
}

func New(api API, store Store, eval Evaluator, log *slog.Logger) *Bot {
	return &Bot{api: api, store: store, eval: eval, log: log, now: time.Now}
}

var commands = []telegram.Command{
	{Command: "status", Description: "Current water and rain near your places"},
	{Command: "places", Description: "List, rename or remove your places"},
	{Command: "stop", Description: "Stop alerts and delete your data"},
	{Command: "help", Description: "How FloodWatch works"},
}

// Run answers incoming messages until ctx is done. It returns an error only
// when Telegram rejects the token, which no retry can fix.
func (b *Bot) Run(ctx context.Context) error {
	if err := b.api.SetMyCommands(ctx, commands); err != nil {
		if telegram.Unauthorized(err) {
			return fmt.Errorf("telegram rejected the bot token: %w", err)
		}
		b.log.Warn("set bot commands failed", "err", err)
	}

	var offset int64
	delay := time.Second
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
		for _, u := range updates {
			offset = u.UpdateID + 1
			b.handle(ctx, u)
		}
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

func (b *Bot) onMessage(ctx context.Context, m *telegram.Message) {
	// Alerts are personal, and a group would expose every member's places.
	if m.Chat.Type != "private" {
		return
	}
	chat := m.Chat.ID
	if m.Location != nil {
		b.onLocation(ctx, chat, m.Location.Latitude, m.Location.Longitude)
		return
	}
	text := strings.TrimSpace(m.Text)
	// A command wins even as a reply to a naming question, so /status still
	// works from the reply box that question opens.
	if cmd := command(text); cmd != "" {
		b.onCommand(ctx, chat, cmd)
		return
	}
	if label, ok := renameTarget(m.ReplyToMessage); ok {
		b.rename(ctx, chat, label, text)
		return
	}
	if lat, lng, ok := parseCoords(text); ok {
		b.onLocation(ctx, chat, lat, lng)
		return
	}
	b.send(ctx, chat, "Send me a location to watch, or see /help.", locationKeyboard)
}

func (b *Bot) onCommand(ctx context.Context, chat int64, cmd string) {
	switch cmd {
	case "/start", "/help":
		b.send(ctx, chat, welcomeText, locationKeyboard)
	case "/status":
		b.statusAll(ctx, chat)
	case "/places":
		b.places(ctx, chat)
	case "/stop":
		b.send(ctx, chat, "This stops all alerts and deletes your places. Are you sure?", telegram.InlineKeyboard{
			InlineKeyboard: [][]telegram.InlineButton{{
				{Text: "Delete everything", CallbackData: "stop:yes"},
				{Text: "Cancel", CallbackData: "stop:no"},
			}},
		})
	default:
		b.send(ctx, chat, "Send me a location to watch, or see /help.", locationKeyboard)
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

func (b *Bot) onLocation(ctx context.Context, chat int64, lat, lng float64) {
	lat, lng = round5(lat), round5(lng)
	findings, err := b.eval.Status(ctx, alert.Subscription{Channel: channel, Recipient: recipient(chat), Lat: lat, Lng: lng, RadiusM: defaultRadiusM})
	if err != nil {
		b.fail(ctx, chat, "check a location", err)
		return
	}
	if !covered(findings) {
		b.send(ctx, chat, notCoveredText, nil)
		return
	}

	places, err := b.store.RecipientSubscriptions(ctx, channel, recipient(chat))
	if err != nil {
		b.fail(ctx, chat, "list places", err)
		return
	}
	if len(places) == 0 {
		b.savePlace(ctx, chat, "Home", lat, lng)
		return
	}

	var rows [][]telegram.InlineButton
	labels := make([]string, len(places))
	for i, p := range places {
		labels[i] = esc(p.Label)
		rows = append(rows, []telegram.InlineButton{{Text: "Move " + p.Label + " here", CallbackData: fmt.Sprintf("mv:%d:%s", p.ID, coords(lat, lng))}})
	}
	if len(places) < store.MaxPlaces {
		rows = append(rows, []telegram.InlineButton{{Text: "Add as a new place", CallbackData: "add:" + coords(lat, lng)}})
	}
	b.send(ctx, chat, "You already watch "+strings.Join(labels, ", ")+". What should I do with this location?",
		telegram.InlineKeyboard{InlineKeyboard: rows})
}

// savePlace reports whether the place was saved and its status delivered.
func (b *Bot) savePlace(ctx context.Context, chat int64, label string, lat, lng float64) (alert.Subscription, bool) {
	sub, err := b.store.SaveSubscription(ctx, alert.Subscription{
		Channel: channel, Recipient: recipient(chat), Label: label, Lat: lat, Lng: lng, RadiusM: defaultRadiusM,
	})
	if errors.Is(err, store.ErrTooManyPlaces) {
		b.send(ctx, chat, fmt.Sprintf("You can watch up to %d places. Remove one with /places first.", store.MaxPlaces), nil)
		return sub, false
	}
	if err != nil {
		b.fail(ctx, chat, "save a place", err)
		return sub, false
	}

	findings, err := b.eval.Status(ctx, sub)
	if err != nil {
		b.fail(ctx, chat, "read the gauges", err)
		return sub, false
	}
	intro := fmt.Sprintf("Watching <b>%s</b>. I'll message you when anything here changes.\n\n", esc(sub.Label))
	if !b.send(ctx, chat, intro+statusText(sub, findings, b.now()), nil) {
		return sub, false
	}
	// The subscriber has just seen all of this; without a baseline the next
	// evaluation would repeat it as alerts.
	if err := b.eval.Baseline(ctx, sub); err != nil {
		b.log.Error("baseline failed", "subscription", sub.ID, "err", err)
	}
	return sub, true
}

func (b *Bot) statusAll(ctx context.Context, chat int64) {
	places, err := b.store.RecipientSubscriptions(ctx, channel, recipient(chat))
	if err != nil {
		b.fail(ctx, chat, "list places", err)
		return
	}
	if len(places) == 0 {
		b.send(ctx, chat, "You're not watching anywhere yet. Send me a location to start.", locationKeyboard)
		return
	}
	for _, p := range places {
		findings, err := b.eval.Status(ctx, p)
		if err != nil {
			b.fail(ctx, chat, "read the gauges", err)
			return
		}
		b.send(ctx, chat, statusText(p, findings, b.now()), nil)
	}
}

func (b *Bot) places(ctx context.Context, chat int64) {
	places, err := b.store.RecipientSubscriptions(ctx, channel, recipient(chat))
	if err != nil {
		b.fail(ctx, chat, "list places", err)
		return
	}
	if len(places) == 0 {
		b.send(ctx, chat, "You're not watching anywhere yet. Send me a location to start.", locationKeyboard)
		return
	}
	var lines []string
	var rows [][]telegram.InlineButton
	for _, p := range places {
		lines = append(lines, fmt.Sprintf("• <b>%s</b> (%s)", esc(p.Label), coords(p.Lat, p.Lng)))
		rows = append(rows, []telegram.InlineButton{
			{Text: "Rename " + p.Label, CallbackData: fmt.Sprintf("rn:%d", p.ID)},
			{Text: "Remove " + p.Label, CallbackData: fmt.Sprintf("rm:%d", p.ID)},
		})
	}
	text := "Your places:\n" + strings.Join(lines, "\n") + "\n\nSend a location to add a place or move one."
	b.send(ctx, chat, text, telegram.InlineKeyboard{InlineKeyboard: rows})
}

func (b *Bot) onCallback(ctx context.Context, cq *telegram.CallbackQuery) {
	if err := b.api.AnswerCallbackQuery(ctx, cq.ID); err != nil {
		b.log.Debug("answer callback failed", "err", err)
	}
	if cq.Message == nil || cq.Message.Chat.Type != "private" {
		return
	}
	chat, prompt := cq.Message.Chat.ID, cq.Message.MessageID
	action, arg, _ := strings.Cut(cq.Data, ":")

	switch action {
	case "add":
		lat, lng, ok := parseCoords(arg)
		if !ok {
			return
		}
		places, err := b.store.RecipientSubscriptions(ctx, channel, recipient(chat))
		if err != nil {
			b.fail(ctx, chat, "list places", err)
			return
		}
		label := nextLabel(places)
		b.edit(ctx, chat, prompt, "Adding <b>"+esc(label)+"</b>.")
		if sub, ok := b.savePlace(ctx, chat, label, lat, lng); ok {
			b.send(ctx, chat, renamePrompt(sub.Label), renameReply)
		}

	case "mv":
		ref, at, _ := strings.Cut(arg, ":")
		lat, lng, ok := parseCoords(at)
		if !ok {
			return
		}
		place, ok := b.ownPlace(ctx, chat, ref)
		if !ok {
			b.edit(ctx, chat, prompt, "That place is gone. Send the location again to add it.")
			return
		}
		b.edit(ctx, chat, prompt, "Moving <b>"+esc(place.Label)+"</b>.")
		b.savePlace(ctx, chat, place.Label, lat, lng)

	case "rn":
		place, ok := b.ownPlace(ctx, chat, arg)
		if !ok {
			b.send(ctx, chat, "That place is gone. See /places.", nil)
			return
		}
		b.send(ctx, chat, renamePrompt(place.Label), renameReply)

	case "rm":
		place, ok := b.ownPlace(ctx, chat, arg)
		if !ok {
			b.edit(ctx, chat, prompt, "That place was already removed.")
			return
		}
		removed, err := b.store.RemoveSubscription(ctx, channel, recipient(chat), place.Label)
		if err != nil {
			b.fail(ctx, chat, "remove a place", err)
			return
		}
		if removed {
			b.edit(ctx, chat, prompt, "Removed <b>"+esc(place.Label)+"</b>. No more alerts for it.")
		} else {
			b.edit(ctx, chat, prompt, "<b>"+esc(place.Label)+"</b> was already removed.")
		}

	case "stop":
		if arg != "yes" {
			b.edit(ctx, chat, prompt, "Nothing was deleted.")
			return
		}
		n, err := b.store.ForgetRecipient(ctx, channel, recipient(chat))
		if err != nil {
			b.fail(ctx, chat, "delete your data", err)
			return
		}
		b.edit(ctx, chat, prompt, fmt.Sprintf("Done. I deleted your %d %s and will not message you again. Send a location any time to start over.", n, plural(n, "place", "places")))
	}
}

// ownPlace resolves a button's reference to one of the subscriber's own places,
// so a button can never touch someone else's. Buttons carry the place ID,
// which stays within Telegram's 64-byte limit whatever the name; ones sent
// before that carried the name itself.
func (b *Bot) ownPlace(ctx context.Context, chat int64, ref string) (alert.Subscription, bool) {
	places, err := b.store.RecipientSubscriptions(ctx, channel, recipient(chat))
	if err != nil {
		b.fail(ctx, chat, "list places", err)
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

var (
	renameReply   = telegram.ForceReply{ForceReply: true, InputFieldPlaceholder: "Office, Mum's house, ..."}
	renamePattern = regexp.MustCompile(`^What should I call (.+)\? Reply with a name`)
)

func renamePrompt(label string) string {
	return fmt.Sprintf("What should I call <b>%s</b>? Reply with a name, like Office or Mum's house.", esc(label))
}

// renameTarget recognises a reply to the bot's naming question and returns the
// place it asked about. Telegram quotes that question back as plain text, so
// the name is read from it and no conversation state has to be kept.
func renameTarget(replyTo *telegram.Message) (string, bool) {
	if replyTo == nil || replyTo.From == nil || !replyTo.From.IsBot {
		return "", false
	}
	m := renamePattern.FindStringSubmatch(replyTo.Text)
	if m == nil {
		return "", false
	}
	return m[1], true
}

func (b *Bot) rename(ctx context.Context, chat int64, from, raw string) {
	name, problem := cleanLabel(raw)
	if problem != "" {
		b.send(ctx, chat, problem+" Tap Rename in /places to try again.", nil)
		return
	}
	places, err := b.store.RecipientSubscriptions(ctx, channel, recipient(chat))
	if err != nil {
		b.fail(ctx, chat, "rename a place", err)
		return
	}
	for _, p := range places {
		if p.Label != from && strings.EqualFold(p.Label, name) {
			b.send(ctx, chat, fmt.Sprintf("You already have a place called <b>%s</b>. Pick another name with /places.", esc(p.Label)), nil)
			return
		}
	}

	renamed, err := b.store.RenameSubscription(ctx, channel, recipient(chat), from, name)
	switch {
	case errors.Is(err, store.ErrLabelTaken):
		b.send(ctx, chat, fmt.Sprintf("You already have a place called <b>%s</b>. Pick another name with /places.", esc(name)), nil)
	case err != nil:
		b.fail(ctx, chat, "rename a place", err)
	case !renamed:
		b.send(ctx, chat, fmt.Sprintf("I couldn't find <b>%s</b> any more. See /places.", esc(from)), nil)
	default:
		b.send(ctx, chat, fmt.Sprintf("Renamed <b>%s</b> to <b>%s</b>.", esc(from), esc(name)), nil)
	}
}

// cleanLabel tidies a name typed by a subscriber, or says what is wrong with it.
func cleanLabel(raw string) (name, problem string) {
	name = strings.Join(strings.Fields(raw), " ")
	switch {
	case !strings.ContainsFunc(name, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }):
		return "", "A name needs at least one letter or number."
	case utf8.RuneCountInString(name) > maxLabelRunes:
		return "", fmt.Sprintf("Please keep names to %d characters or fewer.", maxLabelRunes)
	case strings.ContainsFunc(name, unicode.IsControl):
		return "", "That name has characters I can't use."
	}
	return name, ""
}

// nextLabel names a new place: Home first, then Place 2, Place 3 and so on,
// reusing the lowest free number.
func nextLabel(places []alert.Subscription) string {
	used := make([]string, len(places))
	for i, p := range places {
		used[i] = p.Label
	}
	if !slices.Contains(used, "Home") {
		return "Home"
	}
	for n := 2; ; n++ {
		if label := fmt.Sprintf("Place %d", n); !slices.Contains(used, label) {
			return label
		}
	}
}

func plural(n int64, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// send reports whether the message was delivered. A recipient who blocked the
// bot can never be reached again, so everything stored about them is deleted.
func (b *Bot) send(ctx context.Context, chat int64, text string, markup any) bool {
	err := b.api.SendMessage(ctx, telegram.OutgoingMessage{ChatID: chat, Text: text, ParseMode: "HTML", ReplyMarkup: markup})
	switch {
	case telegram.Blocked(err):
		b.forgetBlocked(ctx, recipient(chat))
		return false
	case err != nil:
		b.log.Warn("send failed", "chat", chat, "err", err)
		return false
	}
	return true
}

func (b *Bot) edit(ctx context.Context, chat, messageID int64, text string) {
	if err := b.api.EditMessageText(ctx, chat, messageID, text, "HTML"); err != nil {
		b.log.Debug("edit failed", "chat", chat, "err", err)
	}
}

func (b *Bot) fail(ctx context.Context, chat int64, doing string, err error) {
	b.log.Error("request failed", "chat", chat, "doing", doing, "err", err)
	b.send(ctx, chat, "Sorry, I couldn't "+doing+" just now. Please try again in a minute.", nil)
}

func (b *Bot) forgetBlocked(ctx context.Context, rcpt string) {
	n, err := b.store.ForgetRecipient(ctx, channel, rcpt)
	if err != nil {
		b.log.Error("forget blocked recipient failed", "err", err)
		return
	}
	b.log.Info("recipient blocked the bot; their places were deleted", "places", n)
}

var locationKeyboard = telegram.ReplyKeyboard{
	Keyboard:        [][]telegram.KeyboardButton{{{Text: "📍 Share my location", RequestLocation: true}}},
	ResizeKeyboard:  true,
	OneTimeKeyboard: true,
}

const welcomeText = `🌊 <b>FloodWatch BKK</b>

I watch the canal and river gauges and rain gauges around places you choose, and message you when water nears a bank, rises fast, or rain gets heavy.

<b>To start, send me a location</b>: tap the button below, or 📎 then Location. On a computer, paste coordinates like <code>13.6515, 100.4945</code>.

/status  current readings near your places
/places  list, rename or remove places (up to 5)
/stop  stop alerts and delete your data

I store only your chat ID and the locations you send. /stop deletes them.

` + disclaimer

const notCoveredText = "I can't watch that location: there's no working gauge within 10 km of it. FloodWatch covers Bangkok and the provinces around it."
