package alert

import (
	"context"
	"fmt"
	"time"
)

type Store interface {
	ListSubscriptions(ctx context.Context) ([]Subscription, error)
	ListStations(ctx context.Context) ([]Station, error)
	LatestWater(ctx context.Context, since time.Time) (map[int64]WaterPoint, error)
	RecentWater(ctx context.Context, since time.Time) (map[int64][]WaterPoint, error)
	LatestRain(ctx context.Context, since time.Time) (map[int64]RainPoint, error)
	AlertStates(ctx context.Context) (map[Key]int, error)
	RecordAlertStates(ctx context.Context, states map[Key]int, notified bool) error
}

type Evaluator struct {
	store Store
	now   func() time.Time
}

func NewEvaluator(store Store) *Evaluator {
	return &Evaluator{store: store, now: time.Now}
}

// Digest is everything one place's subscriber should be told after an
// evaluation, sent as a single message.
type Digest struct {
	Subscription Subscription
	Changes      []Change
}

// Pending returns a digest for every place whose situation changed since its
// subscriber was last told. Nothing is recorded until Ack, so a digest that
// fails to send is simply produced again next time.
func (e *Evaluator) Pending(ctx context.Context) ([]Digest, error) {
	snap, err := e.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	subs, err := e.store.ListSubscriptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	current, err := e.store.AlertStates(ctx)
	if err != nil {
		return nil, fmt.Errorf("alert states: %w", err)
	}

	var out []Digest
	for _, sub := range subs {
		if changes := Changes(Assess(snap, sub, current), current); len(changes) > 0 {
			out = append(out, Digest{Subscription: sub, Changes: changes})
		}
	}
	return out, nil
}

// Ack records that a digest was delivered.
func (e *Evaluator) Ack(ctx context.Context, d Digest) error {
	states := make(map[Key]int, len(d.Changes))
	for _, c := range d.Changes {
		states[c.Key] = c.Severity
	}
	return e.store.RecordAlertStates(ctx, states, true)
}

// Status assesses a place without changing anything. The place need not be
// subscribed; one with ID 0 is judged without any history.
func (e *Evaluator) Status(ctx context.Context, sub Subscription) ([]Finding, error) {
	snap, err := e.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	current, err := e.store.AlertStates(ctx)
	if err != nil {
		return nil, fmt.Errorf("alert states: %w", err)
	}
	return Assess(snap, sub, current), nil
}

// Baseline records a new place's current situation as already told, for when
// its subscriber has just been shown a status. Otherwise the first evaluation
// would repeat all of it as alerts.
func (e *Evaluator) Baseline(ctx context.Context, sub Subscription) error {
	findings, err := e.Status(ctx, sub)
	if err != nil {
		return err
	}
	states := make(map[Key]int)
	for _, f := range findings {
		if f.Known && f.Severity > SeverityNone {
			states[f.Key] = f.Severity
		}
	}
	return e.store.RecordAlertStates(ctx, states, false)
}

func (e *Evaluator) snapshot(ctx context.Context) (Snapshot, error) {
	now := e.now()
	snap := Snapshot{Now: now}
	var err error
	if snap.Stations, err = e.store.ListStations(ctx); err != nil {
		return Snapshot{}, fmt.Errorf("list stations: %w", err)
	}
	if snap.LatestWater, err = e.store.LatestWater(ctx, now.Add(-AliveWithin)); err != nil {
		return Snapshot{}, fmt.Errorf("latest water: %w", err)
	}
	if snap.LatestRain, err = e.store.LatestRain(ctx, now.Add(-AliveWithin)); err != nil {
		return Snapshot{}, fmt.Errorf("latest rain: %w", err)
	}
	// The latest reading may itself be up to waterFresh old, and the rise is
	// measured back from it.
	if snap.RecentWater, err = e.store.RecentWater(ctx, now.Add(-waterFresh-RiseMaxSpan)); err != nil {
		return Snapshot{}, fmt.Errorf("recent water: %w", err)
	}
	return snap, nil
}
