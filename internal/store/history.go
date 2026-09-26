package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/FreddieTheObserver/floodwatch/internal/source"
	"github.com/FreddieTheObserver/floodwatch/internal/store/gen"
)

// StationRef names a stored station both ways: by its row and by its source's ID.
type StationRef struct {
	ID         int64
	ExternalID string
}

// ReportingWaterStations lists a source's water gauges that have reported
// since a time, leaving out ones long dead.
func (s *Store) ReportingWaterStations(ctx context.Context, src string, since time.Time) ([]StationRef, error) {
	rows, err := s.ListReportingWaterStations(ctx, gen.ListReportingWaterStationsParams{Source: src, Since: since})
	if err != nil {
		return nil, err
	}
	out := make([]StationRef, len(rows))
	for i, r := range rows {
		out[i] = StationRef{ID: r.ID, ExternalID: r.ExternalID}
	}
	return out, nil
}

// SaveWaterHistory stores a gauge's past readings atomically and returns how
// many were new. A reading already stored, from the regular poll or an
// earlier fill, is kept as it is.
func (s *Store) SaveWaterHistory(ctx context.Context, stationID int64, readings []source.WaterReading) (int64, error) {
	var added int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		added = 0
		q := s.WithTx(tx)
		for _, r := range readings {
			n, err := q.InsertWaterReading(ctx, gen.InsertWaterReadingParams{StationID: stationID, ObservedAt: r.ObservedAt, LevelMsl: r.LevelMSL})
			if err != nil {
				return fmt.Errorf("insert water reading for station %d at %v: %w", stationID, r.ObservedAt, err)
			}
			added += n
		}
		return nil
	})
	return added, err
}
