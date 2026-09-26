package alert

import (
	"context"
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

func TestPendingAckCycle(t *testing.T) {
	ctx := context.Background()
	f := newFixture().water(north(1, source.KindWater, 1, mm(2)), at(0, 2.1))
	m := &memStore{subs: []Subscription{home}, snap: f.snap, states: map[Key]int{}}
	e := newEvaluator(m)

	digests, err := e.Pending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(digests) != 1 || len(digests[0].Changes) != 1 || digests[0].Changes[0].Severity != SeveritySevere {
		t.Fatalf("first evaluation = %+v, want one overflow change", digests)
	}

	// Until the digest is acknowledged as delivered, it keeps coming back.
	if again, _ := e.Pending(ctx); len(again) != 1 {
		t.Fatalf("unacknowledged digest was dropped: %+v", again)
	}
	if err := e.Ack(ctx, digests[0]); err != nil {
		t.Fatal(err)
	}
	if after, _ := e.Pending(ctx); len(after) != 0 {
		t.Errorf("acknowledged digest came back: %+v", after)
	}

	// The water recedes well below the bank: one all clear, then silence.
	m.snap.LatestWater[1] = at(0, 1.0)
	m.snap.RecentWater[1] = []WaterPoint{at(0, 1.0)}
	clear, _ := e.Pending(ctx)
	if len(clear) != 1 || clear[0].Changes[0].Severity != SeverityNone || clear[0].Changes[0].From != SeveritySevere {
		t.Fatalf("receding water = %+v, want an all clear", clear)
	}
}

func TestSwitchedOffSourcesAreNotUsed(t *testing.T) {
	gauge := north(11, source.KindRain, 1, nil)
	gauge.Source = "bma"
	f := newFixture().rain(gauge, RainPoint{At: now, Rain1h: mm(50)})
	m := &memStore{subs: []Subscription{home}, snap: f.snap, states: map[Key]int{}}

	findings, err := newEvaluator(m).Status(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		if f.Station.Source == "bma" {
			t.Errorf("a switched-off source was used: %+v", f)
		}
	}
}

func TestBaselineSuppressesTheFirstRepeat(t *testing.T) {
	ctx := context.Background()
	f := newFixture().
		water(north(1, source.KindWater, 1, mm(2)), at(0, 1.9)).
		rain(north(11, source.KindRain, 1, nil), RainPoint{At: now, Rain1h: mm(50)})
	m := &memStore{subs: []Subscription{home}, snap: f.snap, states: map[Key]int{}}
	e := newEvaluator(m)

	if err := e.Baseline(ctx, home); err != nil {
		t.Fatal(err)
	}
	if digests, _ := e.Pending(ctx); len(digests) != 0 {
		t.Errorf("baseline was repeated as alerts: %+v", digests)
	}
}
