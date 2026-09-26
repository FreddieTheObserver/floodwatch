package collect

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/FreddieTheObserver/floodwatch/internal/source"
)

var ict = time.FixedZone("ICT", 7*60*60)

type fakeTides struct {
	stations []source.TideStation
	preds    map[string][]source.TidePrediction
	failFor  string
}

func (f fakeTides) Stations(context.Context) ([]source.TideStation, error) { return f.stations, nil }

func (f fakeTides) Predictions(_ context.Context, code string) ([]source.TidePrediction, error) {
	if code == f.failFor {
		return nil, errors.New("503")
	}
	return f.preds[code], nil
}

type savedTides map[string][]source.TidePrediction

func (s savedTides) SaveTides(_ context.Context, st source.TideStation, preds []source.TidePrediction) error {
	s[st.Code] = preds
	return nil
}

// hourly returns a prediction every hour from start for n hours.
func hourly(start time.Time, n int) []source.TidePrediction {
	out := make([]source.TidePrediction, n)
	for i := range out {
		out[i] = source.TidePrediction{At: start.Add(time.Duration(i) * time.Hour), LevelM: float64(i%12) / 10}
	}
	return out
}

func TestTideSyncKeepsTheWindowAroundNow(t *testing.T) {
	now := time.Date(2026, 9, 26, 19, 0, 0, 0, ict)
	year := hourly(time.Date(2026, 9, 1, 0, 0, 0, 0, ict), 365*24)
	src := fakeTides{
		stations: []source.TideStation{{Code: "N02", Name: "Bangkok Harbour"}, {Code: "N03", Name: "Fort Chula"}},
		preds:    map[string][]source.TidePrediction{"N02": year, "N03": year},
	}
	saved := savedTides{}
	s := NewTideSyncer(src, saved, quiet(), []string{"N02", "N03"})
	s.now = func() time.Time { return now }

	if err := s.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := saved["N02"]
	if len(got) != (3+14)*24+1 {
		t.Fatalf("kept %d hours, want the %d from 3 days back to 14 ahead", len(got), (3+14)*24+1)
	}
	if first, last := got[0].At, got[len(got)-1].At; !first.Equal(now.Add(-72*time.Hour)) || !last.Equal(now.Add(14*24*time.Hour)) {
		t.Errorf("window = %v to %v", first, last)
	}
}

func TestTideSyncReportsProblemsButKeepsGoing(t *testing.T) {
	now := time.Date(2026, 9, 26, 19, 0, 0, 0, ict)
	src := fakeTides{
		stations: []source.TideStation{{Code: "N01"}, {Code: "N02"}, {Code: "N04"}},
		preds: map[string][]source.TidePrediction{
			"N01": hourly(now.Add(-time.Hour), 48),
			// A file that stopped being updated last year.
			"N04": hourly(time.Date(2025, 9, 1, 0, 0, 0, 0, ict), 24),
		},
		failFor: "N02",
	}
	saved := savedTides{}
	s := NewTideSyncer(src, saved, quiet(), []string{"N01", "N02", "N04", "N99"})
	s.now = func() time.Time { return now }

	err := s.Sync(context.Background())
	if err == nil {
		t.Fatal("want the failures reported")
	}
	for _, want := range []string{"N02: 503", "N04 do not cover 2026-09-26", "N99 is not in HII's list"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	if len(saved["N01"]) != 48 {
		t.Errorf("the healthy station was not saved: %d predictions", len(saved["N01"]))
	}
}
