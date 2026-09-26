package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/FreddieTheObserver/floodwatch/internal/alert"
	"github.com/FreddieTheObserver/floodwatch/internal/store/gen"
)

// MaxPlaces is how many places one subscriber may watch.
const MaxPlaces = 5

var ErrTooManyPlaces = errors.New("too many places")

// SaveSubscription adds a place, or moves an existing one with the same label.
// A moved place watches different stations, so what was told about the old
// spot is forgotten.
func (s *Store) SaveSubscription(ctx context.Context, sub alert.Subscription) (alert.Subscription, error) {
	var saved alert.Subscription
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.WithTx(tx)
		// Serialises one subscriber's changes, so two at once cannot both slip
		// under MaxPlaces.
		if err := q.LockRecipient(ctx, gen.LockRecipientParams{Channel: sub.Channel, Recipient: sub.Recipient}); err != nil {
			return err
		}

		existing, err := q.GetRecipientSubscription(ctx, gen.GetRecipientSubscriptionParams{
			Channel: sub.Channel, Recipient: sub.Recipient, Label: sub.Label,
		})
		isNew := errors.Is(err, pgx.ErrNoRows)
		if err != nil && !isNew {
			return err
		}
		if isNew {
			n, err := q.CountRecipientSubscriptions(ctx, gen.CountRecipientSubscriptionsParams{
				Channel: sub.Channel, Recipient: sub.Recipient,
			})
			if err != nil {
				return err
			}
			if n >= MaxPlaces {
				return ErrTooManyPlaces
			}
		}

		row, err := q.UpsertSubscription(ctx, gen.UpsertSubscriptionParams{
			Channel: sub.Channel, Recipient: sub.Recipient, Label: sub.Label,
			Lat: sub.Lat, Lng: sub.Lng, RadiusM: int32(sub.RadiusM),
		})
		if err != nil {
			return err
		}
		if !isNew && (existing.Lat != row.Lat || existing.Lng != row.Lng || existing.RadiusM != row.RadiusM) {
			if err := q.ResetAlertStates(ctx, row.ID); err != nil {
				return err
			}
		}
		saved = subscription(row)
		return nil
	})
	return saved, err
}

func (s *Store) RecipientSubscriptions(ctx context.Context, channel, recipient string) ([]alert.Subscription, error) {
	rows, err := s.ListRecipientSubscriptions(ctx, gen.ListRecipientSubscriptionsParams{Channel: channel, Recipient: recipient})
	if err != nil {
		return nil, err
	}
	out := make([]alert.Subscription, len(rows))
	for i, r := range rows {
		out[i] = subscription(r)
	}
	return out, nil
}

// ErrLabelTaken means the subscriber already has a place by that name.
var ErrLabelTaken = errors.New("label taken")

// RenameSubscription renames one place and reports whether it existed. Its
// alert history is kept, since the place itself has not moved.
func (s *Store) RenameSubscription(ctx context.Context, channel, recipient, from, to string) (bool, error) {
	n, err := s.RenameRecipientSubscription(ctx, gen.RenameRecipientSubscriptionParams{
		Channel: channel, Recipient: recipient, OldLabel: from, NewLabel: to,
	})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == uniqueViolation {
		return false, ErrLabelTaken
	}
	return n > 0, err
}

const uniqueViolation = "23505"

// RemoveSubscription deletes one place and reports whether it existed.
func (s *Store) RemoveSubscription(ctx context.Context, channel, recipient, label string) (bool, error) {
	n, err := s.DeleteRecipientSubscription(ctx, gen.DeleteRecipientSubscriptionParams{
		Channel: channel, Recipient: recipient, Label: label,
	})
	return n > 0, err
}

// ForgetRecipient deletes every place of one subscriber, with them all alert
// history, and their preferences: everything stored about that person.
func (s *Store) ForgetRecipient(ctx context.Context, channel, recipient string) (int64, error) {
	var n int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.WithTx(tx)
		var err error
		if n, err = q.DeleteRecipient(ctx, gen.DeleteRecipientParams{Channel: channel, Recipient: recipient}); err != nil {
			return err
		}
		return q.DeleteRecipientPreferences(ctx, gen.DeleteRecipientPreferencesParams{Channel: channel, Recipient: recipient})
	})
	return n, err
}

// RecipientLanguage is a person's chosen language, or "" if they have none.
func (s *Store) RecipientLanguage(ctx context.Context, channel, recipient string) (string, error) {
	lang, err := s.GetRecipientLanguage(ctx, gen.GetRecipientLanguageParams{Channel: channel, Recipient: recipient})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return lang, err
}

func (s *Store) SetRecipientLanguage(ctx context.Context, channel, recipient, language string) error {
	return s.UpsertRecipientLanguage(ctx, gen.UpsertRecipientLanguageParams{Channel: channel, Recipient: recipient, Language: language})
}
