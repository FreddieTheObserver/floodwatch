package bot

import (
	"context"
	"strconv"

	"github.com/FreddieTheObserver/floodwatch/internal/telegram"
)

// Notify delivers every pending alert digest. It runs after each collector
// poll. A digest is acknowledged only once Telegram accepts it, so one that
// fails is simply produced again after the next poll.
func (b *Bot) Notify(ctx context.Context) {
	digests, err := b.eval.Pending(ctx)
	if err != nil {
		b.log.Error("evaluate alerts failed", "err", err)
		return
	}
	var sent, failed int
	for _, d := range digests {
		if ctx.Err() != nil {
			return
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
}
