package bot

import (
	"context"
	"fmt"
	"strconv"

	"github.com/FreddieTheObserver/floodwatch/internal/telegram"
)

// Notify delivers every pending alert digest. It runs after each collector
// poll. A digest is acknowledged only once Telegram accepts it, so one that
// fails is simply produced again after the next poll. The error says whether
// alerting as a whole is broken, as opposed to one message not getting
// through.
func (b *Bot) Notify(ctx context.Context) error {
	digests, err := b.eval.Evaluate(ctx)
	if err != nil {
		b.log.Error("evaluate alerts failed", "err", err)
		return fmt.Errorf("evaluate alerts: %w", err)
	}
	var sent, failed int
	var lastErr error
	for _, d := range digests {
		if ctx.Err() != nil {
			return nil
		}
		chat, err := strconv.ParseInt(d.Subscription.Recipient, 10, 64)
		if err != nil {
			b.log.Error("subscription has a malformed recipient", "subscription", d.Subscription.ID)
			continue
		}
		err = b.api.SendMessage(ctx, telegram.OutgoingMessage{ChatID: chat, Text: digestText(d, b.now()), ParseMode: "HTML"})
		switch {
		case telegram.Blocked(err):
			b.forgetBlocked(ctx, d.Subscription.Recipient)
			continue
		case err != nil:
			failed++
			lastErr = err
			b.log.Warn("alert not delivered; will retry after the next poll", "subscription", d.Subscription.ID, "err", err)
			continue
		}
		if err := b.eval.Ack(ctx, d); err != nil {
			b.log.Error("recording a delivered alert failed; it may be sent again", "subscription", d.Subscription.ID, "err", err)
		}
		sent++
	}
	if sent+failed > 0 {
		b.log.Info("alerts delivered", "sent", sent, "failed", failed)
	}
	if failed > 0 && sent == 0 {
		return fmt.Errorf("none of %d alerts could be delivered: %w", failed, lastErr)
	}
	return nil
}
