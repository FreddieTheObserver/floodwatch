package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/FreddieTheObserver/floodwatch/internal/alert"
	"github.com/FreddieTheObserver/floodwatch/internal/source"
	"github.com/FreddieTheObserver/floodwatch/internal/store/gen"
)

var _ alert.Store = (*Store)(nil)

func (s *Store) ListSubscriptions(ctx context.Context) ([]alert.Subscription, error) {
	rows, err := s.Queries.ListSubscriptions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]alert.Subscription, len(rows))
	for i, r := range rows {
		out[i] = subscription(r)
	}
	return out, nil
}

func (s *Store) ListStations(ctx context.Context) ([]alert.Station, error) {
	rows, err := s.ListStationsForAlerts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]alert.Station, len(rows))
	for i, r := range rows {
		out[i] = alert.Station{
			ID: r.ID, Source: r.Source, Kind: source.Kind(r.Kind), Name: r.Name, NameTH: deref(r.NameTh),
			District: deref(r.District), Lat: r.Lat, Lng: r.Lng, BankMSL: r.BankMsl,
			Agency: deref(r.Agency),
		}
	}
	return out, nil
}

func (s *Store) LatestWater(ctx context.Context, since time.Time) (map[int64]alert.WaterPoint, error) {
	rows, err := s.LatestWaterReadings(ctx, since)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]alert.WaterPoint, len(rows))
	for _, r := range rows {
		out[r.StationID] = alert.WaterPoint{At: r.ObservedAt, LevelMSL: r.LevelMsl}
	}
	return out, nil
}

func (s *Store) RecentWater(ctx context.Context, since time.Time) (map[int64][]alert.WaterPoint, error) {
	rows, err := s.RecentWaterReadings(ctx, since)
	if err != nil {
		return nil, err
	}
	out := make(map[int64][]alert.WaterPoint)
	for _, r := range rows {
		out[r.StationID] = append(out[r.StationID], alert.WaterPoint{At: r.ObservedAt, LevelMSL: r.LevelMsl})
	}
	return out, nil
}

func (s *Store) LatestRain(ctx context.Context, since time.Time) (map[int64]alert.RainPoint, error) {
	rows, err := s.LatestRainReadings(ctx, since)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]alert.RainPoint, len(rows))
	for _, r := range rows {
		out[r.StationID] = alert.RainPoint{At: r.ObservedAt, Rain1h: r.Rain1hMm, Rain3h: r.Rain3hMm, Rain24h: r.Rain24hMm}
	}
	return out, nil
}

func (s *Store) AlertStates(ctx context.Context) (map[alert.Key]int, error) {
	rows, err := s.ListAlertStates(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[alert.Key]int, len(rows))
	for _, r := range rows {
		out[alert.Key{SubscriptionID: r.SubscriptionID, StationID: deref(r.StationID), Rule: alert.Rule(r.Rule)}] = int(r.Severity)
	}
	return out, nil
}

// RecordAlertStates stores severities as told (notified) or as a silent
// baseline, all or nothing.
func (s *Store) RecordAlertStates(ctx context.Context, states map[alert.Key]int, notified bool) error {
	if len(states) == 0 {
		return nil
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.WithTx(tx)
		for k, severity := range states {
			var stationID *int64
			if !k.Rule.IsArea() {
				stationID = &k.StationID
			}
			err := q.UpsertAlertState(ctx, gen.UpsertAlertStateParams{
				SubscriptionID: k.SubscriptionID,
				StationID:      stationID,
				Rule:           string(k.Rule),
				Severity:       int16(severity),
				Notified:       notified,
			})
			if err != nil {
				return fmt.Errorf("record %s for subscription %d: %w", k.Rule, k.SubscriptionID, err)
			}
		}
		return nil
	})
}

// LastAlertRun is when alerts were last checked and delivered, or the zero
// time if they never have been.
func (s *Store) LastAlertRun(ctx context.Context) (time.Time, error) {
	at, err := s.GetLastAlertRun(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, nil
	}
	return at, err
}

func (s *Store) RecordAlertRun(ctx context.Context, at time.Time) error {
	return s.Queries.RecordAlertRun(ctx, at)
}

func subscription(r gen.Subscription) alert.Subscription {
	return alert.Subscription{
		ID: r.ID, Channel: r.Channel, Recipient: r.Recipient, Label: r.Label,
		Lat: r.Lat, Lng: r.Lng, RadiusM: int(r.RadiusM), CreatedAt: r.CreatedAt,
	}
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
