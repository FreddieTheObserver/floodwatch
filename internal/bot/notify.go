package bot

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/FreddieTheObserver/floodwatch/internal/alert"
	"github.com/FreddieTheObserver/floodwatch/internal/telegram"
)

// Alert runs normally come one poll apart. A gap this much longer is time
// floodwatch could not have warned anyone, which a quiet bot would otherwise
// pass off as nothing happening.
func offlineAfter(pollInterval time.Duration) time.Duration {
	return max(time.Hour, 3*pollInterval)
}

// Notify delivers every alert that is due. It runs after each collector poll.
// An alert is acknowledged only once Telegram accepts it, so one that fails is
// simply due again after the next poll. If the last completed run was long
// ago, as after the machine running floodwatch slept, subscribers are first
// told that it was offline and where their places stand. The error says
// whether alerting as a whole is broken, as opposed to one message not getting
// through, and only a run without one counts as completed.
func (b *Bot) Notify(ctx context.Context) error {
	now := b.now()
	last, err := b.store.LastAlertRun(ctx)
	if err != nil {
		b.log.Warn("read the last alert run failed", "err", err)
	}
	digests, err := b.eval.Evaluate(ctx)
	if err != nil {
		b.log.Error("evaluate alerts failed", "err", err)
		return fmt.Errorf("evaluate alerts: %w", err)
	}

	var n tally
	if !last.IsZero() && now.Sub(last) >= b.offlineAfter {
		b.tellOffline(ctx, &n, last, now, digests)
	}
	for _, d := range digests {
		if ctx.Err() != nil {
			return nil
		}
		if !d.Changed() {
			continue
		}
		err := b.deliver(ctx, &n, d.Subscription.Recipient, func(t *texts) string { return digestText(t, d, now) })
		switch {
		case errors.Is(err, errBlocked):
			continue
		case err != nil:
			b.log.Warn("alert not delivered; will retry after the next poll", "subscription", d.Subscription.ID, "err", err)
			continue
		}
		if err := b.eval.Ack(ctx, d); err != nil {
			b.log.Error("recording a delivered alert failed; it may be sent again", "subscription", d.Subscription.ID, "err", err)
		}
	}
	if n.sent+n.failed > 0 {
		b.log.Info("messages delivered", "sent", n.sent, "failed", n.failed)
	}
	if n.failed > 0 && n.sent == 0 {
		return fmt.Errorf("none of %d messages could be delivered: %w", n.failed, n.lastErr)
	}
	if err := b.store.RecordAlertRun(ctx, now); err != nil {
		b.log.Warn("record the alert run failed", "err", err)
	}
	return nil
}

// tellOffline tells everyone with a place that floodwatch could not have
// warned them between from and to, ahead of any alert that follows.
func (b *Bot) tellOffline(ctx context.Context, n *tally, from, to time.Time, digests []alert.Digest) {
	var recipients []string
	places := map[string][]alert.Digest{}
	for _, d := range digests {
		// A place added since is owed no account of the time before it.
		if d.Subscription.CreatedAt.After(from) {
			continue
		}
		rcpt := d.Subscription.Recipient
		if _, ok := places[rcpt]; !ok {
			recipients = append(recipients, rcpt)
		}
		places[rcpt] = append(places[rcpt], d)
	}
	b.log.Info("floodwatch was offline; telling subscribers", "from", from, "to", to, "recipients", len(recipients))
	for _, rcpt := range recipients {
		if ctx.Err() != nil {
			return
		}
		err := b.deliver(ctx, n, rcpt, func(t *texts) string { return offlineText(t, from, to, places[rcpt]) })
		if err != nil && !errors.Is(err, errBlocked) {
			b.log.Warn("offline notice not delivered", "recipient", rcpt, "err", err)
		}
	}
}

// tally counts one run's deliveries.
type tally struct {
	sent, failed int
	lastErr      error
}

var errBlocked = errors.New("recipient blocked the bot")

// deliver sends a recipient a message in their language, counting whether
// Telegram accepted it. A recipient who blocked the bot is forgotten, and
// counts as neither sent nor failed.
func (b *Bot) deliver(ctx context.Context, n *tally, rcpt string, text func(*texts) string) error {
	chat, err := strconv.ParseInt(rcpt, 10, 64)
	if err != nil {
		return fmt.Errorf("malformed recipient %q", rcpt)
	}
	t := b.languageOf(ctx, rcpt)
	if t == nil {
		t = &english
	}
	err = b.api.SendMessage(ctx, telegram.OutgoingMessage{ChatID: chat, Text: text(t), ParseMode: "HTML"})
	switch {
	case telegram.Blocked(err):
		b.forgetBlocked(ctx, rcpt)
		return errBlocked
	case err != nil:
		n.failed++
		n.lastErr = err
		return err
	}
	n.sent++
	return nil
}
