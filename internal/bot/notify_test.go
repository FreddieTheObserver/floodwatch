package bot

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/FreddieTheObserver/floodwatch/internal/alert"
	"github.com/FreddieTheObserver/floodwatch/internal/telegram"
)

// place is a digest for one of a recipient's places; told says the recipient
// already knows its risk, so it is not due as an alert.
func place(id int64, rcpt, label string, told bool) alert.Digest {
	d := alert.Digest{Subscription: alert.Subscription{ID: id, Recipient: rcpt, Label: label}, Assessment: sampleAssessment()}
	if told {
		d.From = d.Risk.Level
	}
	return d
}

func chats(sent []telegram.OutgoingMessage) []int64 {
	out := make([]int64, len(sent))
	for i, m := range sent {
		out[i] = m.ChatID
	}
	return out
}

func TestAGapIsOwnedUpToBeforeAnyAlert(t *testing.T) {
	h := newHarness()
	h.store.lastRun = checked.Add(-9 * time.Hour)
	h.eval.pending = []alert.Digest{place(1, "42", "Home", true), place(2, "42", "Office", false), place(3, "43", "Home", true)}

	if err := h.bot.Notify(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := chats(h.api.sent); len(got) != 3 || got[0] != 42 || got[1] != 43 || got[2] != 42 {
		t.Fatalf("sent to %v, want both notices, then the one alert", got)
	}
	notice := h.api.sent[0].Text
	inOrder(t, notice,
		"FloodWatch was offline from 05:20 to 14:20</b> (about 9 hours)",
		"Your places now:",
		"🟡 <b>Home</b>: WATCH",
		"🟡 <b>Office</b>: WATCH",
		"/status",
	)
	if !strings.Contains(h.api.sent[2].Text, "<b>Office: LOW → WATCH</b>") {
		t.Errorf("alert after the notice = %q", h.api.sent[2].Text)
	}
	if len(h.eval.acked) != 1 || h.eval.acked[0] != 2 {
		t.Errorf("acked = %v, want only the changed place", h.eval.acked)
	}
	if !h.store.lastRun.Equal(checked) {
		t.Errorf("last run = %v, want this one", h.store.lastRun)
	}
}

func TestAnOrdinaryGapIsNotMentioned(t *testing.T) {
	for name, last := range map[string]time.Time{
		"a missed poll": checked.Add(-50 * time.Minute),
		"the first run": {},
	} {
		h := newHarness()
		h.store.lastRun = last
		h.eval.pending = []alert.Digest{place(1, "42", "Home", true)}
		if err := h.bot.Notify(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(h.api.sent) != 0 {
			t.Errorf("%s: sent %q", name, h.api.sent[0].Text)
		}
		if !h.store.lastRun.Equal(checked) {
			t.Errorf("%s: run not recorded", name)
		}
	}
}

func TestTheGapThresholdFollowsThePollInterval(t *testing.T) {
	for poll, want := range map[time.Duration]time.Duration{
		5 * time.Minute:  time.Hour,
		10 * time.Minute: time.Hour,
		30 * time.Minute: 90 * time.Minute,
	} {
		if got := offlineAfter(poll); got != want {
			t.Errorf("polling every %v: a gap counts from %v, want %v", poll, got, want)
		}
	}
}

func TestAnUndeliveredNoticeIsTriedAgain(t *testing.T) {
	h := newHarness()
	last := checked.Add(-2 * time.Hour)
	h.store.lastRun = last
	h.eval.pending = []alert.Digest{place(1, "42", "Home", true)}
	h.api.sendErr[42] = errors.New("telegram unreachable")

	if err := h.bot.Notify(context.Background()); err == nil {
		t.Error("nothing delivered, yet the run was reported as fine")
	}
	if !h.store.lastRun.Equal(last) {
		t.Fatalf("last run = %v; a run that told nobody must not count", h.store.lastRun)
	}

	delete(h.api.sendErr, 42)
	if err := h.bot.Notify(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.api.sent) != 1 || !strings.Contains(h.api.sent[0].Text, "offline from 12:20 to 14:20") {
		t.Errorf("retry sent %v", h.api.sent)
	}
}

func TestPlacesAddedDuringTheGapAreLeftOut(t *testing.T) {
	h := newHarness()
	h.store.lastRun = checked.Add(-3 * time.Hour)
	fresh := place(1, "42", "Home", true)
	fresh.Subscription.CreatedAt = checked.Add(-time.Hour)
	older := place(2, "43", "Home", true)
	older.Subscription.CreatedAt = checked.Add(-48 * time.Hour)
	h.eval.pending = []alert.Digest{fresh, older}

	if err := h.bot.Notify(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := chats(h.api.sent); len(got) != 1 || got[0] != 43 {
		t.Errorf("notices went to %v, want only the place that existed before the gap", got)
	}
}

func TestAGapFromAnotherDayIsDated(t *testing.T) {
	h := newHarness()
	h.store.lastRun = time.Date(2026, 9, 25, 23, 10, 0, 0, ict)
	h.eval.pending = []alert.Digest{place(1, "42", "Home", true)}
	h.store.SetRecipientLanguage(context.Background(), channel, "42", thai.Code)

	if err := h.bot.Notify(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := "FloodWatch ไม่ได้ทำงานตั้งแต่ 25 ก.ย. 23:10 น. ถึง 14:20 น.</b> (ประมาณ 15 ชั่วโมง)"
	if len(h.api.sent) != 1 || !strings.Contains(h.api.sent[0].Text, want) {
		t.Errorf("notice = %v, want it to contain %q", h.api.sent, want)
	}
}

func message(text string, sent time.Time) telegram.Update {
	return telegram.Update{Message: &telegram.Message{Chat: telegram.Chat{ID: me, Type: "private"}, Text: text, Date: sent.Unix()}}
}

func TestMessagesThatWaitedGetOneApology(t *testing.T) {
	h := newHarness()
	h.bot.handleAll(context.Background(), []telegram.Update{
		message("/status", checked.Add(-40*time.Minute)),
		message("/places", checked.Add(-35*time.Minute)),
	})
	if len(h.api.sent) != 3 {
		t.Fatalf("sent %d messages, want an apology and two answers", len(h.api.sent))
	}
	if got := h.api.sent[0].Text; got != "Sorry for the late reply: FloodWatch was offline when you wrote at 13:40." {
		t.Errorf("apology = %q", got)
	}
	for _, m := range h.api.sent[1:] {
		if strings.HasPrefix(m.Text, "Sorry") {
			t.Errorf("apologised twice: %q", m.Text)
		}
	}
}

func TestPromptMessagesGetNoApology(t *testing.T) {
	h := newHarness()
	h.bot.handleAll(context.Background(), []telegram.Update{message("/status", checked.Add(-time.Minute))})
	if len(h.api.sent) != 1 || strings.HasPrefix(h.api.sent[0].Text, "Sorry") {
		t.Errorf("sent %v, want just the answer", h.api.sent)
	}
}
