package tide

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"
)

type Store interface {
	WaterSeries(ctx context.Context, since time.Time) (map[int64][]Point, error)
	TidePredictions(ctx context.Context, from, to time.Time) (map[string]Series, error)
	ReplaceTideFits(ctx context.Context, fits []Fit) error
	SaveTideForecasts(ctx context.Context, forecasts []Forecast) error
}

type Model struct {
	store Store
	log   *slog.Logger
	now   func() time.Time
}

func NewModel(store Store, log *slog.Logger) *Model {
	return &Model{store: store, log: log, now: time.Now}
}

// Update refits every water gauge to the tide and records a forecast for each
// tidal one from its latest reading. It runs after every poll: fitting takes
// milliseconds, and a fit kept current follows the river as a flood changes it.
func (m *Model) Update(ctx context.Context) error {
	now := m.now()
	since := now.Add(-FitWindow)
	readings, err := m.store.WaterSeries(ctx, since)
	if err != nil {
		return fmt.Errorf("water series: %w", err)
	}
	tides, err := m.store.TidePredictions(ctx, since.Add(-maxLag), now.Add(Horizon))
	if err != nil {
		return fmt.Errorf("tide predictions: %w", err)
	}

	ids := make([]int64, 0, len(readings))
	for id := range readings {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	var fits []Fit
	var forecasts []Forecast
	tidal := 0
	for _, id := range ids {
		f, ok := FitGauge(id, readings[id], tides)
		if !ok {
			continue
		}
		fits = append(fits, f)
		if !f.Tidal() {
			continue
		}
		tidal++
		if fc, ok := Predict(f, readings[id], tides[f.TideStation], now); ok {
			forecasts = append(forecasts, fc)
		}
	}
	if err := m.store.ReplaceTideFits(ctx, fits); err != nil {
		return fmt.Errorf("save fits: %w", err)
	}
	if err := m.store.SaveTideForecasts(ctx, forecasts); err != nil {
		return fmt.Errorf("save forecasts: %w", err)
	}
	m.log.Info("tide model updated", "gauges", len(fits), "tidal", tidal, "forecasts", len(forecasts))
	return nil
}
