package store

import (
	"testing"
	"time"

	"github.com/FreddieTheObserver/floodwatch/internal/source"
	"github.com/FreddieTheObserver/floodwatch/internal/tide"
)

func TestTideModelRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()

	at := time.Date(2026, 9, 27, 9, 0, 0, 0, ict)
	if _, err := s.SaveBatch(ctx, waterBatch(ptr(2.16), 1.32, at)); err != nil {
		t.Fatal(err)
	}
	gauges, err := s.ReportingWaterStations(ctx, "thaiwater", at.Add(-time.Hour))
	if err != nil || len(gauges) != 1 {
		t.Fatalf("gauges = %v, %v", gauges, err)
	}
	id := gauges[0].ID
	if _, err := s.SaveWaterHistory(ctx, id, []source.WaterReading{{ObservedAt: at.Add(-10 * time.Minute), LevelMSL: 1.28}}); err != nil {
		t.Fatal(err)
	}
	preds := []source.TidePrediction{{At: at, LevelM: 0.5}, {At: at.Add(time.Hour), LevelM: 0.7}, {At: at.Add(2 * time.Hour), LevelM: 0.6}}
	if err := s.SaveTides(ctx, source.TideStation{Code: "N02", Name: "Bangkok Harbour", Lat: 13.7, Lng: 100.5}, preds); err != nil {
		t.Fatal(err)
	}

	series, err := s.WaterSeries(ctx, at.Add(-time.Hour))
	if err != nil || len(series[id]) != 2 || series[id][0].Level != 1.28 || series[id][1].Level != 1.32 {
		t.Errorf("water series = %v, %v; want both readings, oldest first", series, err)
	}
	tides, err := s.TidePredictions(ctx, at.Add(30*time.Minute), at.Add(3*time.Hour))
	if err != nil || len(tides["N02"]) != 2 || !tides["N02"][0].At.Equal(at.Add(time.Hour)) {
		t.Errorf("tide predictions = %v, %v; want the two inside the window", tides, err)
	}

	fit := tide.Fit{StationID: id, TideStation: "N02", Lag: 40 * time.Minute, Gain: 1.2, R: 0.9, ResidualSD: 0.2, Samples: 400, From: at.Add(-72 * time.Hour), To: at}
	for range 2 {
		if err := s.ReplaceTideFits(ctx, []tide.Fit{fit}); err != nil {
			t.Fatal(err)
		}
	}
	if n := countRows(t, s, "tide_fits"); n != 1 {
		t.Errorf("%d fits stored, want the refit to replace the first", n)
	}

	forecast := func(basedOn time.Time, peak float64) tide.Forecast {
		return tide.Forecast{StationID: id, BasedOn: basedOn, PeakAt: basedOn.Add(3 * time.Hour), PeakLevel: peak, Offset: 0.1, TideStation: "N02", Lag: 40 * time.Minute}
	}
	err = s.SaveTideForecasts(ctx, []tide.Forecast{forecast(at.Add(-10*time.Minute), 1.9), forecast(at, 2.0)})
	if err != nil {
		t.Fatal(err)
	}
	// A second forecast from the same reading keeps the first.
	if err := s.SaveTideForecasts(ctx, []tide.Forecast{forecast(at, 2.5)}); err != nil {
		t.Fatal(err)
	}
	latest, err := s.TideForecasts(ctx, at.Add(-time.Hour))
	if err != nil || len(latest) != 1 {
		t.Fatalf("forecasts = %v, %v", latest, err)
	}
	if fc := latest[id]; !fc.BasedOn.Equal(at) || fc.PeakLevel != 2.0 || !fc.PeakAt.Equal(at.Add(3*time.Hour)) {
		t.Errorf("latest forecast = %+v, want the first made from the newest reading", fc)
	}
	if old, _ := s.TideForecasts(ctx, at.Add(time.Minute)); len(old) != 0 {
		t.Errorf("forecasts from before the cut-off were returned: %v", old)
	}
}
