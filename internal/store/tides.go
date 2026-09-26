package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/FreddieTheObserver/floodwatch/internal/source"
	"github.com/FreddieTheObserver/floodwatch/internal/store/gen"
)

// SaveTides stores one station's predictions atomically, replacing any
// earlier prediction for the same hour.
func (s *Store) SaveTides(ctx context.Context, st source.TideStation, preds []source.TidePrediction) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.WithTx(tx)
		err := q.UpsertTideStation(ctx, gen.UpsertTideStationParams{
			Code: st.Code, Name: st.Name, NameTh: optional(st.NameTH), Lat: st.Lat, Lng: st.Lng,
		})
		if err != nil {
			return fmt.Errorf("save tide station %s: %w", st.Code, err)
		}
		for _, p := range preds {
			err := q.UpsertTidePrediction(ctx, gen.UpsertTidePredictionParams{StationCode: st.Code, At: p.At, LevelM: p.LevelM})
			if err != nil {
				return fmt.Errorf("save tide prediction %s %v: %w", st.Code, p.At, err)
			}
		}
		return nil
	})
}
