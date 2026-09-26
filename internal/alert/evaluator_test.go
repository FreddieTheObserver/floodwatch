package alert

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/FreddieTheObserver/floodwatch/internal/source"
)

type memStore struct {
	subs   []Subscription
	snap   Snapshot
	states map[Key]int
}

func (m *memStore) ListSubscriptions(context.Context) ([]Subscription, error) { return m.subs, nil }
func (m *memStore) ListStations(context.Context) ([]Station, error)           { return m.snap.Stations, nil }
func (m *memStore) LatestWater(context.Context, time.Time) (map[int64]WaterPoint, error) {
	return m.snap.LatestWater, nil
}
func (m *memStore) RecentWater(context.Context, time.Time) (map[int64][]WaterPoint, error) {
	return m.snap.RecentWater, nil
}
func (m *memStore) LatestRain(context.Context, time.Time) (map[int64]RainPoint, error) {
	return m.snap.LatestRain, nil
}
func (m *memStore) AlertStates(context.Context) (map[Key]int, error) {
	out := make(map[Key]int, len(m.states))
	for k, v := range m.states {
		out[k] = v
	}
	return out, nil
}
func (m *memStore) RecordAlertStates(_ context.Context, states map[Key]int, _ bool) error {
	for k, v := range states {
		m.states[k] = v
	}
	return nil
}

func newEvaluator(m *memStore) *Evaluator {
	e := NewEvaluator(m, []string{"thaiwater"})
	e.now = func() time.Time { return now }
	return e
}

// alerts evaluates and keeps the digests that are due as alerts.
func alerts(t *testing.T, e *Evaluator) []Digest {
	t.Helper()
	digests, err := e.Evaluate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return slices.DeleteFunc(digests, func(d Digest) bool { return !d.Changed() })
}

func TestRiskAlertCycle(t *testing.T) {
	ctx := context.Background()
	f := newFixture().water(north(1, source.KindWater, 1, mm(2)), at(0, 2.1))
	m := &memStore{subs: []Subscription{home}, snap: f.snap, states: map[Key]int{}}
	e := newEvaluator(m)

	digests := alerts(t, e)
	if len(digests) != 1 || digests[0].Risk.Level != SeveritySevere || digests[0].From != SeverityNone {
		t.Fatalf("first evaluation = %+v, want one change from low to high", digests)
	}
	// The gauge-level state is recorded as judged, delivered or not, since
	// hysteresis works from it.
	if m.states[Key{SubscriptionID: 1, StationID: 1, Rule: RuleWaterLevel}] != SeveritySevere {
		t.Errorf("gauge state not recorded: %v", m.states)
	}

	// Until the digest is acknowledged as delivered, it keeps coming back.
	if again := alerts(t, e); len(again) != 1 {
		t.Fatalf("unacknowledged digest was dropped: %+v", again)
	}
	if err := e.Ack(ctx, digests[0]); err != nil {
		t.Fatal(err)
	}
	if after := alerts(t, e); len(after) != 0 {
		t.Errorf("acknowledged digest came back: %+v", after)
	}

	// The water recedes well below the bank: one all clear.
	m.snap.LatestWater[1] = at(0, 1.0)
	m.snap.RecentWater[1] = []WaterPoint{at(0, 1.0)}
	clear := alerts(t, e)
	if len(clear) != 1 || clear[0].Risk.Level != SeverityNone || clear[0].From != SeveritySevere {
		t.Fatalf("receding water = %+v, want an all clear from high", clear)
	}
}

func TestAQuietGaugeNeverSendsAnAllClear(t *testing.T) {
	ctx := context.Background()
	f := newFixture().water(north(1, source.KindWater, 1, mm(2)), at(0, 2.1))
	m := &memStore{subs: []Subscription{home}, snap: f.snap, states: map[Key]int{}}
	e := newEvaluator(m)
	e.Ack(ctx, alerts(t, e)[0])

	// The only gauge stops reporting while over its bank.
	m.snap.LatestWater[1] = at(4*time.Hour, 2.1)
	m.snap.RecentWater[1] = []WaterPoint{at(4*time.Hour, 2.1)}
	if quiet := alerts(t, e); len(quiet) != 0 {
		t.Errorf("silence changed the risk: %+v", quiet)
	}
	a, _ := e.Status(ctx, home)
	if a.Risk.Level != SeveritySevere || len(a.Risk.Drivers) != 1 || !a.Risk.Drivers[0].Held {
		t.Errorf("risk = %+v, want high, held from the quiet gauge", a.Risk)
	}
}

func TestSwitchedOffSourcesAreNotUsed(t *testing.T) {
	gauge := north(11, source.KindRain, 1, nil)
	gauge.Source = "bma"
	f := newFixture().rain(gauge, RainPoint{At: now, Rain1h: mm(50)})
	m := &memStore{subs: []Subscription{home}, snap: f.snap, states: map[Key]int{}}

	a, err := newEvaluator(m).Status(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range a.Findings {
		if f.Station.Source == "bma" {
			t.Errorf("a switched-off source was used: %+v", f)
		}
	}
	if a.Risk.Level != RiskUnknown {
		t.Errorf("risk = %d, want unknown with the only gauge excluded", a.Risk.Level)
	}
}

func TestBaselineSuppressesTheFirstAlert(t *testing.T) {
	ctx := context.Background()
	f := newFixture().
		water(north(1, source.KindWater, 1, mm(2)), at(0, 1.9)).
		rain(north(11, source.KindRain, 1, nil), RainPoint{At: now, Rain1h: mm(50)})
	m := &memStore{subs: []Subscription{home}, snap: f.snap, states: map[Key]int{}}
	e := newEvaluator(m)

	if err := e.Baseline(ctx, home); err != nil {
		t.Fatal(err)
	}
	if digests := alerts(t, e); len(digests) != 0 {
		t.Errorf("baseline was repeated as an alert: %+v", digests)
	}
}

func TestEvaluateReportsEveryPlace(t *testing.T) {
	ctx := context.Background()
	f := newFixture().water(north(1, source.KindWater, 1, mm(2)), at(0, 2.1))
	// A second place by the same gauge, whose subscriber was already told.
	told := home
	told.ID, told.Label = 2, "office"
	m := &memStore{subs: []Subscription{home, told}, snap: f.snap, states: map[Key]int{riskKey(told): SeveritySevere}}
	e := newEvaluator(m)

	digests, err := e.Evaluate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(digests) != 2 || digests[0].Subscription.ID != 1 || digests[1].Subscription.ID != 2 {
		t.Fatalf("digests = %+v, want both places in subscription order", digests)
	}
	if !digests[0].Changed() || digests[1].Changed() {
		t.Errorf("changed = %v, %v; want only the place not yet told", digests[0].Changed(), digests[1].Changed())
	}
}
