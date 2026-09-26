package source

import (
	"context"
	"math"
	"net/http"
	"os"
	"testing"
	"time"
)

func openFixture(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestParseTideStations(t *testing.T) {
	stations, err := parseTideStations(openFixture(t, "hii_tide_summary.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stations) != 6 {
		t.Fatalf("got %d stations, want the 6 valid ones", len(stations))
	}
	harbour := stations[1]
	if harbour.Code != "N02" || harbour.Name != "Bangkok Harbour" || harbour.NameTH != "ท่าเรือกรุงเทพ" ||
		harbour.Lat != 13.73247 || harbour.Lng != 100.512017 {
		t.Errorf("N02 = %+v", harbour)
	}
}

// The file for 26 September 2026 predicted high water at Bangkok Harbour at
// 18:00, when the gauges nearby did indeed peak.
func TestParseTidePredictions(t *testing.T) {
	preds, err := parseTidePredictions(openFixture(t, "hii_tide_N02.txt"), "N02")
	if err != nil {
		t.Fatal(err)
	}
	if len(preds) != 24 {
		t.Fatalf("got %d predictions, want the day's 24 valid hours", len(preds))
	}
	high := preds[0]
	for _, p := range preds {
		if p.LevelM > high.LevelM {
			high = p
		}
	}
	if !high.At.Equal(time.Date(2026, 9, 26, 18, 0, 0, 0, ict)) || math.Abs(high.LevelM-0.73469) > 1e-9 {
		t.Errorf("high water = %.5f m at %v, want 0.73469 m at 18:00 ICT", high.LevelM, high.At)
	}
}

func TestTideCodesAreCheckedBeforeUse(t *testing.T) {
	for _, code := range []string{"../etc/passwd", "N2", "n02", "N02.txt"} {
		if _, err := HIITides(http.DefaultClient).Predictions(context.Background(), code); err == nil {
			t.Errorf("%q was accepted", code)
		}
	}
}
