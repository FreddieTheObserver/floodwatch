package alert

import (
	"context"
	"fmt"
	"slices"
	"time"
)

type Store interface {
	ListSubscriptions(ctx context.Context) ([]Subscription, error)
	ListStations(ctx context.Context) ([]Station, error)
	LatestWater(ctx context.Context, since time.Time) (map[int64]WaterPoint, error)
	RecentWater(ctx context.Context, since time.Time) (map[int64][]WaterPoint, error)
	LatestRain(ctx context.Context, since time.Time) (map[int64]RainPoint, error)
	TideForecasts(ctx context.Context, since time.Time) (map[int64]TideForecast, error)
	AlertStates(ctx context.Context) (map[Key]int, error)
	RecordAlertStates(ctx context.Context, states map[Key]int, notified bool) error
}

type Evaluator struct {
	store   Store
	sources []string
	now     func() time.Time
}

// NewEvaluator judges places using stations from the given sources only.
// Readings already stored from a source that has since been switched off stay
// unused, as they would be used without permission.
func NewEvaluator(store Store, sources []string) *Evaluator {
	return &Evaluator{store: store, sources: sources, now: time.Now}
}

// Assessment is one place's findings and the overall risk judged from them.
type Assessment struct {
	Findings []Finding
	Risk     Risk
}

// Digest is one place's assessment beside what its subscriber was last told.
type Digest struct {
	Subscription Subscription
	Assessment
	// From is the level the subscriber was last told, which is low for a place
	// never told anything.
	From int
}

// Changed reports whether the subscriber should be alerted: the overall risk
// is no longer what they were last told.
func (d Digest) Changed() bool { return d.Risk.Level != d.From }

func riskKey(sub Subscription) Key { return Key{SubscriptionID: sub.ID, Rule: RuleRisk} }

// Evaluate judges every place and returns a digest for each, in subscription
// order; those that Changed are due as alerts. The per-gauge severities behind
// each risk are recorded as they stand, since hysteresis works from them; the
// risk itself is recorded only on Ack, so an alert that fails to send is due
// again next time.
func (e *Evaluator) Evaluate(ctx context.Context) ([]Digest, error) {
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
	factors := make(map[Key]int)
	for _, sub := range subs {
		a := assess(snap, sub, current)
		for k, v := range factorStates(a.Findings) {
			factors[k] = v
		}
		out = append(out, Digest{Subscription: sub, Assessment: a, From: current[riskKey(sub)]})
	}
	if err := e.store.RecordAlertStates(ctx, factors, false); err != nil {
		return nil, fmt.Errorf("record gauge states: %w", err)
	}
	return out, nil
}

// Ack records that a digest was delivered.
func (e *Evaluator) Ack(ctx context.Context, d Digest) error {
	return e.store.RecordAlertStates(ctx, map[Key]int{riskKey(d.Subscription): d.Risk.Level}, true)
}

// Status assesses a place without changing anything. The place need not be
// subscribed; one with ID 0 is judged without any history.
func (e *Evaluator) Status(ctx context.Context, sub Subscription) (Assessment, error) {
	snap, err := e.snapshot(ctx)
	if err != nil {
		return Assessment{}, err
	}
	current, err := e.store.AlertStates(ctx)
	if err != nil {
		return Assessment{}, fmt.Errorf("alert states: %w", err)
	}
	return assess(snap, sub, current), nil
}

// Baseline records a new place's current situation as already told, for when
// its subscriber has just been shown a status. Otherwise the first evaluation
// would repeat it as an alert.
func (e *Evaluator) Baseline(ctx context.Context, sub Subscription) error {
	a, err := e.Status(ctx, sub)
	if err != nil {
		return err
	}
	states := factorStates(a.Findings)
	states[riskKey(sub)] = a.Risk.Level
	return e.store.RecordAlertStates(ctx, states, false)
}

func assess(snap Snapshot, sub Subscription, current map[Key]int) Assessment {
	findings := Assess(snap, sub, current)
	return Assessment{Findings: findings, Risk: Overall(findings, sub.RadiusM)}
}

func factorStates(findings []Finding) map[Key]int {
	states := make(map[Key]int)
	for _, f := range findings {
		if f.Known {
			states[f.Key] = f.Severity
		}
	}
	return states
}

func (e *Evaluator) snapshot(ctx context.Context) (Snapshot, error) {
	now := e.now()
	snap := Snapshot{Now: now}
	var err error
	if snap.Stations, err = e.store.ListStations(ctx); err != nil {
		return Snapshot{}, fmt.Errorf("list stations: %w", err)
	}
	snap.Stations = slices.DeleteFunc(snap.Stations, func(st Station) bool {
		return !slices.Contains(e.sources, st.Source)
	})
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
	if snap.Forecasts, err = e.store.TideForecasts(ctx, now.Add(-waterFresh)); err != nil {
		return Snapshot{}, fmt.Errorf("tide forecasts: %w", err)
	}
	return snap, nil
}
