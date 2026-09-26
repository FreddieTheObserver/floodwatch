package collect

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/FreddieTheObserver/floodwatch/internal/source"
	"github.com/FreddieTheObserver/floodwatch/internal/store"
)

type fakeHistory struct {
	asked  []string
	from   time.Time
	to     time.Time
	failed map[string]bool
}

func (f *fakeHistory) Water(_ context.Context, externalID string, from, to time.Time) ([]source.WaterReading, error) {
	f.asked = append(f.asked, externalID)
	f.from, f.to = from, to
	if f.failed[externalID] {
		return nil, errors.New("503")
	}
	return []source.WaterReading{{ExternalID: externalID, ObservedAt: to, LevelMSL: 1}}, nil
}

type historyStore struct {
	gauges []store.StationRef
	since  time.Time
	saved  map[int64]int
}

func (s *historyStore) ReportingWaterStations(_ context.Context, src string, since time.Time) ([]store.StationRef, error) {
	s.since = since
	if src != "thaiwater" {
		return nil, nil
	}
	return s.gauges, nil
}

func (s *historyStore) SaveWaterHistory(_ context.Context, stationID int64, readings []source.WaterReading) (int64, error) {
	s.saved[stationID] += len(readings)
	return int64(len(readings)), nil
}

func gauges(ids ...string) []store.StationRef {
	out := make([]store.StationRef, len(ids))
	for i, id := range ids {
		out[i] = store.StationRef{ID: int64(i + 1), ExternalID: id}
	}
	return out
}

func newHistorySyncer(src *fakeHistory, st *historyStore, now time.Time) *HistorySyncer {
	s := NewHistorySyncer(src, st, quiet(), time.Second)
	s.pause = 0
	s.now = func() time.Time { return now }
	return s
}

func TestHistoryFillsEveryReportingGauge(t *testing.T) {
	now := time.Date(2026, 9, 27, 7, 45, 0, 0, ict)
	src := &fakeHistory{}
	st := &historyStore{gauges: gauges("4", "575567"), saved: map[int64]int{}}

	if err := newHistorySyncer(src, st, now).Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(src.asked, []string{"4", "575567"}) {
		t.Errorf("asked for %v", src.asked)
	}
	if !src.from.Equal(now.Add(-72*time.Hour)) || !src.to.Equal(now) {
		t.Errorf("window = %v to %v, want the 3 days to now", src.from, src.to)
	}
	if !st.since.Equal(now.Add(-48 * time.Hour)) {
		t.Errorf("gauges reporting since %v, want the last 2 days", st.since)
	}
	if st.saved[1] != 1 || st.saved[2] != 1 {
		t.Errorf("saved = %v", st.saved)
	}
}

func TestHistoryCarriesOnPastOneFailure(t *testing.T) {
	src := &fakeHistory{failed: map[string]bool{"4": true}}
	st := &historyStore{gauges: gauges("4", "575567"), saved: map[int64]int{}}

	if err := newHistorySyncer(src, st, time.Now()).Sync(context.Background()); err == nil {
		t.Error("a failed gauge went unreported")
	}
	if st.saved[2] != 1 {
		t.Errorf("the gauge after the failure was not filled: %v", st.saved)
	}
}

func TestHistoryGivesUpOnAServerInTrouble(t *testing.T) {
	src := &fakeHistory{failed: map[string]bool{"1": true, "2": true, "3": true, "4": true}}
	st := &historyStore{gauges: gauges("1", "2", "3", "4", "5"), saved: map[int64]int{}}

	if err := newHistorySyncer(src, st, time.Now()).Sync(context.Background()); err == nil {
		t.Error("failures went unreported")
	}
	if len(src.asked) != historyMaxFailures {
		t.Errorf("asked %d times, want to stop after %d failures in a row", len(src.asked), historyMaxFailures)
	}
}
