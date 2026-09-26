package collect

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/FreddieTheObserver/floodwatch/internal/source"
	"github.com/FreddieTheObserver/floodwatch/internal/store"
)

const (
	historyEvery      = 6 * time.Hour
	historyRetryAfter = time.Hour
	// Each fill reaches back this far, covering a night's sleep with room to
	// spare; readings already stored are left alone.
	historyWindow = 3 * 24 * time.Hour
	// Gauges silent for longer are dead or removed, and not worth a request.
	historyAlive = 2 * 24 * time.Hour
	// Requests go one at a time with a pause between, to go easy on a public
	// server that is busiest during floods, and a run gives up after a few
	// failures in a row rather than keep asking a server in trouble.
	historyPause       = 2 * time.Second
	historyMaxFailures = 3
)

type HistorySource interface {
	Water(ctx context.Context, externalID string, from, to time.Time) ([]source.WaterReading, error)
}

type HistoryStore interface {
	ReportingWaterStations(ctx context.Context, src string, since time.Time) ([]store.StationRef, error)
	SaveWaterHistory(ctx context.Context, stationID int64, readings []source.WaterReading) (int64, error)
}

// HistorySyncer fills in the ThaiWater water gauges' readings of the last few
// days, which the regular poll misses whenever floodwatch is not running.
type HistorySyncer struct {
	source  HistorySource
	store   HistoryStore
	log     *slog.Logger
	timeout time.Duration
	pause   time.Duration
	now     func() time.Time
}

func NewHistorySyncer(src HistorySource, store HistoryStore, log *slog.Logger, fetchTimeout time.Duration) *HistorySyncer {
	return &HistorySyncer{source: src, store: store, log: log, timeout: fetchTimeout, pause: historyPause, now: time.Now}
}

// Run syncs straight away, then every historyEvery, or historyRetryAfter after
// a sync that failed, until ctx is done.
func (s *HistorySyncer) Run(ctx context.Context) {
	for {
		wait := historyEvery
		if err := s.Sync(ctx); err != nil {
			s.log.Warn("gauge history sync failed", "err", err, "retry_in", historyRetryAfter.String())
			wait = historyRetryAfter
		}
		if !sleepCtx(ctx, wait) {
			return
		}
	}
}

// Sync fills in every reporting gauge's recent readings. One gauge failing
// does not stop the others, but several in a row end the run.
func (s *HistorySyncer) Sync(ctx context.Context) error {
	now := s.now()
	stations, err := s.store.ReportingWaterStations(ctx, "thaiwater", now.Add(-historyAlive))
	if err != nil {
		return fmt.Errorf("list water gauges: %w", err)
	}

	var errs []error
	var added int64
	failures := 0
	for i, st := range stations {
		if i > 0 && !sleepCtx(ctx, s.pause) {
			return ctx.Err()
		}
		n, err := s.fill(ctx, st, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("gauge %s: %w", st.ExternalID, err))
			if failures++; failures == historyMaxFailures {
				errs = append(errs, fmt.Errorf("gave up after %d failures in a row", failures))
				break
			}
			continue
		}
		failures = 0
		added += n
	}
	s.log.Info("gauge history synced", "gauges", len(stations), "new_readings", added, "failed", len(errs))
	return errors.Join(errs...)
}

func (s *HistorySyncer) fill(ctx context.Context, st store.StationRef, now time.Time) (int64, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	readings, err := s.source.Water(fetchCtx, st.ExternalID, now.Add(-historyWindow), now)
	if err != nil {
		return 0, err
	}
	return s.store.SaveWaterHistory(ctx, st.ID, readings)
}

// sleepCtx waits for d and reports whether ctx is still live.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
