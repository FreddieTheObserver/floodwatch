package bot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/FreddieTheObserver/floodwatch/internal/alert"
	"github.com/FreddieTheObserver/floodwatch/internal/store"
	"github.com/FreddieTheObserver/floodwatch/internal/telegram"
)

const me int64 = 42

type fakeAPI struct {
	sent    []telegram.OutgoingMessage
	edits   []string
	sendErr map[int64]error
}

func (f *fakeAPI) GetUpdates(context.Context, int64, int) ([]telegram.Update, error) { return nil, nil }
func (f *fakeAPI) SetMyCommands(context.Context, []telegram.Command) error           { return nil }
func (f *fakeAPI) AnswerCallbackQuery(context.Context, string) error                 { return nil }
func (f *fakeAPI) SendMessage(_ context.Context, m telegram.OutgoingMessage) error {
	if err := f.sendErr[m.ChatID]; err != nil {
		return err
	}
	f.sent = append(f.sent, m)
	return nil
}
func (f *fakeAPI) EditMessageText(_ context.Context, _, _ int64, text, _ string) error {
	f.edits = append(f.edits, text)
	return nil
}

func (f *fakeAPI) last(t *testing.T) telegram.OutgoingMessage {
	t.Helper()
	if len(f.sent) == 0 {
		t.Fatal("nothing was sent")
	}
	return f.sent[len(f.sent)-1]
}

type fakeStore struct {
	subs      []alert.Subscription
	nextID    int64
	forgotten []string
}

func (s *fakeStore) SaveSubscription(_ context.Context, sub alert.Subscription) (alert.Subscription, error) {
	mine := 0
	for i, x := range s.subs {
		if x.Recipient != sub.Recipient {
			continue
		}
		mine++
		if x.Label == sub.Label {
			sub.ID = x.ID
			s.subs[i] = sub
			return sub, nil
		}
	}
	if mine >= store.MaxPlaces {
		return alert.Subscription{}, store.ErrTooManyPlaces
	}
	s.nextID++
	sub.ID = s.nextID
	s.subs = append(s.subs, sub)
	return sub, nil
}

func (s *fakeStore) RecipientSubscriptions(_ context.Context, _, rcpt string) ([]alert.Subscription, error) {
	var out []alert.Subscription
	for _, x := range s.subs {
		if x.Recipient == rcpt {
			out = append(out, x)
		}
	}
	return out, nil
}

func (s *fakeStore) RemoveSubscription(_ context.Context, _, rcpt, label string) (bool, error) {
	for i, x := range s.subs {
		if x.Recipient == rcpt && x.Label == label {
			s.subs = append(s.subs[:i], s.subs[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (s *fakeStore) ForgetRecipient(_ context.Context, _, rcpt string) (int64, error) {
	s.forgotten = append(s.forgotten, rcpt)
	var kept []alert.Subscription
	var n int64
	for _, x := range s.subs {
		if x.Recipient == rcpt {
			n++
			continue
		}
		kept = append(kept, x)
	}
	s.subs = kept
	return n, nil
}

type fakeEval struct {
	findings  []alert.Finding
	pending   []alert.Digest
	acked     []int64
	baselined []alert.Subscription
}

func (e *fakeEval) Pending(context.Context) ([]alert.Digest, error) { return e.pending, nil }
func (e *fakeEval) Ack(_ context.Context, d alert.Digest) error {
	e.acked = append(e.acked, d.Subscription.ID)
	return nil
}
func (e *fakeEval) Status(context.Context, alert.Subscription) ([]alert.Finding, error) {
	return e.findings, nil
}
func (e *fakeEval) Baseline(_ context.Context, sub alert.Subscription) error {
	e.baselined = append(e.baselined, sub)
	return nil
}

var checked = time.Date(2026, 9, 26, 14, 20, 0, 0, ict)

func sampleFindings() []alert.Finding {
	bank := 2.16
	water := alert.Station{ID: 7, Source: "thaiwater", Name: "Chao Phraya 15", BankMSL: &bank}
	gauge := alert.Station{ID: 9, Source: "thaiwater", Name: "Krung Thep 3"}
	at := time.Date(2026, 9, 26, 13, 30, 0, 0, ict)
	return []alert.Finding{
		{Key: alert.Key{StationID: 7, Rule: alert.RuleWaterLevel}, Known: true, Station: water, DistanceM: 5400, At: at, LevelMSL: 0.34, BankMSL: bank},
		{Key: alert.Key{StationID: 7, Rule: alert.RuleWaterRising}, Station: water, DistanceM: 5400, At: at, LevelMSL: 0.34, BankMSL: bank},
		{Key: alert.Key{StationID: 7, Rule: alert.RuleWaterStale}, Known: true, Station: water, DistanceM: 5400, At: at},
		{Key: alert.Key{Rule: alert.RuleRain}, Known: true, Severity: alert.SeverityWatch, Station: gauge, DistanceM: 4900, At: at,
			Rain1h: ptr(0.5), Rain24h: ptr(124), FreshGauges: 7},
		{Key: alert.Key{Rule: alert.RuleRainStale}, Known: true, FreshGauges: 7},
	}
}

func ptr(v float64) *float64 { return &v }

type harness struct {
	api   *fakeAPI
	store *fakeStore
	eval  *fakeEval
	bot   *Bot
}

func newHarness() *harness {
	h := &harness{api: &fakeAPI{sendErr: map[int64]error{}}, store: &fakeStore{}, eval: &fakeEval{findings: sampleFindings()}}
	h.bot = New(h.api, h.store, h.eval, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.bot.now = func() time.Time { return checked }
	return h
}

func (h *harness) text(s string) {
	h.bot.handle(context.Background(), telegram.Update{Message: &telegram.Message{Chat: telegram.Chat{ID: me, Type: "private"}, Text: s}})
}

func (h *harness) location(lat, lng float64) {
	h.bot.handle(context.Background(), telegram.Update{Message: &telegram.Message{
		Chat: telegram.Chat{ID: me, Type: "private"}, Location: &telegram.Location{Latitude: lat, Longitude: lng},
	}})
}

func (h *harness) tap(data string) {
	h.bot.handle(context.Background(), telegram.Update{CallbackQuery: &telegram.CallbackQuery{
		ID: "cb", Data: data, Message: &telegram.Message{MessageID: 5, Chat: telegram.Chat{ID: me, Type: "private"}},
	}})
}

func buttons(t *testing.T, m telegram.OutgoingMessage) []string {
	t.Helper()
	kb, ok := m.ReplyMarkup.(telegram.InlineKeyboard)
	if !ok {
		t.Fatalf("markup %T is not an inline keyboard", m.ReplyMarkup)
	}
	var out []string
	for _, row := range kb.InlineKeyboard {
		for _, b := range row {
			out = append(out, b.CallbackData)
		}
	}
	return out
}

func TestStartOffersTheLocationButton(t *testing.T) {
	h := newHarness()
	h.text("/start")
	kb, ok := h.api.last(t).ReplyMarkup.(telegram.ReplyKeyboard)
	if !ok || !kb.Keyboard[0][0].RequestLocation {
		t.Errorf("markup = %#v, want a location request button", h.api.last(t).ReplyMarkup)
	}
}

func TestFirstLocationBecomesHome(t *testing.T) {
	h := newHarness()
	h.location(13.651512, 100.494498)

	if len(h.store.subs) != 1 || h.store.subs[0].Label != "Home" || h.store.subs[0].Lat != 13.65151 {
		t.Fatalf("subs = %+v", h.store.subs)
	}
	text := h.api.last(t).Text
	if !strings.Contains(text, "Watching <b>Home</b>") || !strings.Contains(text, "Chao Phraya 15") {
		t.Errorf("reply = %s", text)
	}
	if len(h.eval.baselined) != 1 {
		t.Errorf("baselined %d times, want once", len(h.eval.baselined))
	}
}

func TestCoordinatesWorkLikeALocation(t *testing.T) {
	h := newHarness()
	h.text("13.6515, 100.4945")
	if len(h.store.subs) != 1 || h.store.subs[0].Lng != 100.4945 {
		t.Errorf("subs = %+v", h.store.subs)
	}
}

func TestAnotherLocationAsksFirst(t *testing.T) {
	h := newHarness()
	h.location(13.6515, 100.4945)
	h.location(13.7, 100.49)

	if len(h.store.subs) != 1 {
		t.Fatalf("a second location was saved without asking: %+v", h.store.subs)
	}
	got := buttons(t, h.api.last(t))
	want := []string{"mv:Home:13.70000,100.49000", "add:13.70000,100.49000"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("buttons = %v, want %v", got, want)
	}
}

func TestAddMoveAndRemoveButtons(t *testing.T) {
	h := newHarness()
	h.location(13.6515, 100.4945)

	h.tap("add:13.70000,100.49000")
	if len(h.store.subs) != 2 || h.store.subs[1].Label != "Place 2" {
		t.Fatalf("after add: %+v", h.store.subs)
	}

	h.tap("mv:Home:13.60000,100.50000")
	if h.store.subs[0].Label != "Home" || h.store.subs[0].Lat != 13.6 {
		t.Errorf("after move: %+v", h.store.subs)
	}
	if len(h.eval.baselined) != 3 {
		t.Errorf("baselined %d times, want once per saved place", len(h.eval.baselined))
	}

	h.tap("rm:Place 2")
	if len(h.store.subs) != 1 || !strings.Contains(h.api.edits[len(h.api.edits)-1], "Removed <b>Place 2</b>") {
		t.Errorf("after remove: %+v, edits %v", h.store.subs, h.api.edits)
	}
}

func TestPlaceLimit(t *testing.T) {
	h := newHarness()
	for range store.MaxPlaces {
		h.tap("add:13.70000,100.49000")
	}
	h.tap("add:13.70000,100.49000")
	if len(h.store.subs) != store.MaxPlaces || !strings.Contains(h.api.last(t).Text, "up to 5 places") {
		t.Errorf("subs = %d, last reply = %q", len(h.store.subs), h.api.last(t).Text)
	}
}

func TestStopAsksThenDeletes(t *testing.T) {
	h := newHarness()
	h.location(13.6515, 100.4945)

	h.text("/stop")
	if got := buttons(t, h.api.last(t)); len(got) != 2 || got[0] != "stop:yes" {
		t.Fatalf("buttons = %v", got)
	}
	h.tap("stop:no")
	if len(h.store.subs) != 1 {
		t.Fatal("cancel deleted data")
	}
	h.tap("stop:yes")
	if len(h.store.subs) != 0 || len(h.store.forgotten) != 1 {
		t.Errorf("subs = %+v, forgotten = %v", h.store.subs, h.store.forgotten)
	}
}

func TestGroupChatsAreIgnored(t *testing.T) {
	h := newHarness()
	h.bot.handle(context.Background(), telegram.Update{Message: &telegram.Message{
		Chat: telegram.Chat{ID: -100, Type: "group"}, Location: &telegram.Location{Latitude: 13.65, Longitude: 100.49},
	}})
	if len(h.api.sent) != 0 || len(h.store.subs) != 0 {
		t.Errorf("group message was handled: sent %v, subs %v", h.api.sent, h.store.subs)
	}
}

func TestUncoveredLocationIsRefused(t *testing.T) {
	h := newHarness()
	h.eval.findings = []alert.Finding{{Key: alert.Key{Rule: alert.RuleRain}}, {Key: alert.Key{Rule: alert.RuleRainStale}}}
	h.location(18.79, 98.98)
	if len(h.store.subs) != 0 || h.api.last(t).Text != notCoveredText {
		t.Errorf("subs = %+v, reply = %q", h.store.subs, h.api.last(t).Text)
	}
}

func TestNotifyAcknowledgesOnlyWhatWasDelivered(t *testing.T) {
	h := newHarness()
	change := alert.Change{Finding: sampleFindings()[3], From: alert.SeverityNone}
	h.eval.pending = []alert.Digest{
		{Subscription: alert.Subscription{ID: 1, Recipient: "42", Label: "Home"}, Changes: []alert.Change{change}},
		{Subscription: alert.Subscription{ID: 2, Recipient: "43", Label: "Home"}, Changes: []alert.Change{change}},
		{Subscription: alert.Subscription{ID: 3, Recipient: "44", Label: "Home"}, Changes: []alert.Change{change}},
	}
	h.api.sendErr[43] = errors.New("timeout")
	h.api.sendErr[44] = &telegram.APIError{Code: 403, Description: "Forbidden: bot was blocked by the user"}

	h.bot.Notify(context.Background())

	if len(h.eval.acked) != 1 || h.eval.acked[0] != 1 {
		t.Errorf("acked = %v, want only the delivered digest", h.eval.acked)
	}
	if len(h.store.forgotten) != 1 || h.store.forgotten[0] != "44" {
		t.Errorf("forgotten = %v, want the recipient who blocked the bot", h.store.forgotten)
	}
}

func TestParseCoords(t *testing.T) {
	for _, c := range []struct {
		in string
		ok bool
	}{
		{"13.6515, 100.4945", true},
		{"13.6515,100.4945", true},
		{" -33.8,151.2 ", true},
		{"13.6515", false},
		{"95, 100", false},
		{"hello, world", false},
		{"13.6515, 100.4945, 3", false},
	} {
		if _, _, ok := parseCoords(c.in); ok != c.ok {
			t.Errorf("parseCoords(%q) ok = %v, want %v", c.in, ok, c.ok)
		}
	}
}

func TestCommand(t *testing.T) {
	for in, want := range map[string]string{
		"/status":                    "/status",
		"/STATUS@floodwatch_bkk_bot": "/status",
		"/places now":                "/places",
		"status":                     "",
		"":                           "",
	} {
		if got := command(in); got != want {
			t.Errorf("command(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNextLabel(t *testing.T) {
	subs := func(labels ...string) (out []alert.Subscription) {
		for _, l := range labels {
			out = append(out, alert.Subscription{Label: l})
		}
		return out
	}
	for _, c := range []struct {
		have []alert.Subscription
		want string
	}{
		{nil, "Home"},
		{subs("Home"), "Place 2"},
		{subs("Home", "Place 3"), "Place 2"},
		{subs("Place 2"), "Home"},
	} {
		if got := nextLabel(c.have); got != c.want {
			t.Errorf("nextLabel(%v) = %q, want %q", c.have, got, c.want)
		}
	}
}
