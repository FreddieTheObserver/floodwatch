package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/FreddieTheObserver/floodwatch/internal/alert"
	"github.com/FreddieTheObserver/floodwatch/internal/store/gen"
	"github.com/FreddieTheObserver/floodwatch/internal/tide"
)

var _ tide.Store = (*Store)(nil)

// WaterSeries is every water gauge's readings since a time, oldest first.
func (s *Store) WaterSeries(ctx context.Context, since time.Time) (map[int64][]tide.Point, error) {
	rows, err := s.ListWaterSeries(ctx, since)
	if err != nil {
		return nil, err
	}
	out := make(map[int64][]tide.Point)
	for _, r := range rows {
		out[r.StationID] = append(out[r.StationID], tide.Point{At: r.ObservedAt, Level: r.LevelMsl})
	}
	return out, nil
}

func (s *Store) TidePredictions(ctx context.Context, from, to time.Time) (map[string]tide.Series, error) {
	rows, err := s.ListTidePredictions(ctx, gen.ListTidePredictionsParams{FromAt: from, ToAt: to})
	if err != nil {
		return nil, err
	}
	out := make(map[string]tide.Series)
	for _, r := range rows {
		out[r.StationCode] = append(out[r.StationCode], tide.Point{At: r.At, Level: r.LevelM})
	}
	return out, nil
}

// ReplaceTideFits swaps in a fresh set of fits, all or nothing.
func (s *Store) ReplaceTideFits(ctx context.Context, fits []tide.Fit) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.WithTx(tx)
		if err := q.DeleteTideFits(ctx); err != nil {
			return err
		}
		for _, f := range fits {
			err := q.InsertTideFit(ctx, gen.InsertTideFitParams{
				StationID: f.StationID, TideStation: f.TideStation, LagMinutes: int32(f.Lag.Minutes()),
				InterceptM: f.Intercept, Gain: f.Gain, R: f.R, ResidualSdM: f.ResidualSD,
				Samples: int32(f.Samples), FittedFrom: f.From, FittedTo: f.To,
			})
			if err != nil {
				return fmt.Errorf("save the tide fit of station %d: %w", f.StationID, err)
			}
		}
		return nil
	})
}

// SaveTideForecasts records forecasts, keeping the first made from any one
// reading.
func (s *Store) SaveTideForecasts(ctx context.Context, forecasts []tide.Forecast) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.WithTx(tx)
		for _, f := range forecasts {
			_, err := q.InsertTideForecast(ctx, gen.InsertTideForecastParams{
				StationID: f.StationID, BasedOn: f.BasedOn, PeakAt: f.PeakAt, PeakLevelMsl: f.PeakLevel,
				OffsetM: f.Offset, TideStation: f.TideStation, LagMinutes: int32(f.Lag.Minutes()),
			})
			if err != nil {
				return fmt.Errorf("save the tide forecast of station %d: %w", f.StationID, err)
			}
		}
		return nil
	})
}

// TideForecasts is each gauge's latest forecast made from a reading since a time.
func (s *Store) TideForecasts(ctx context.Context, since time.Time) (map[int64]alert.TideForecast, error) {
	rows, err := s.ListLatestTideForecasts(ctx, since)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]alert.TideForecast, len(rows))
	for _, r := range rows {
		out[r.StationID] = alert.TideForecast{BasedOn: r.BasedOn, PeakAt: r.PeakAt, PeakLevel: r.PeakLevelMsl}
	}
	return out, nil
}
