package collect

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/FreddieTheObserver/floodwatch/internal/source"
)

const (
	// Predictions change rarely; a few syncs a day catch revisions, and a
	// failed one is retried sooner.
	tideSyncEvery  = 6 * time.Hour
	tideRetryAfter = time.Hour

	// Kept around now: the recent past to compare gauges with, and the days
	// ahead to say when the next high water comes. The rest of HII's year is
	// fetched again on every sync anyway.
	tideKeepBehind = 3 * 24 * time.Hour
	tideKeepAhead  = 14 * 24 * time.Hour
)

type TideSource interface {
	Stations(ctx context.Context) ([]source.TideStation, error)
	Predictions(ctx context.Context, code string) ([]source.TidePrediction, error)
}

type TideStore interface {
	SaveTides(ctx context.Context, st source.TideStation, preds []source.TidePrediction) error
}

type TideSyncer struct {
	source TideSource
	store  TideStore
	log    *slog.Logger
	codes  []string
	now    func() time.Time
}

func NewTideSyncer(src TideSource, store TideStore, log *slog.Logger, codes []string) *TideSyncer {
	return &TideSyncer{source: src, store: store, log: log, codes: codes, now: time.Now}
}

// Run syncs straight away, then every tideSyncEvery, or tideRetryAfter after
// a sync that failed, until ctx is done.
func (s *TideSyncer) Run(ctx context.Context) {
	for {
		wait := tideSyncEvery
		if err := s.Sync(ctx); err != nil {
			s.log.Warn("tide sync failed", "err", err, "retry_in", tideRetryAfter.String())
			wait = tideRetryAfter
		}
		if !sleepCtx(ctx, wait) {
			return
		}
	}
}

// Sync stores the predictions around now for every configured station. One
// station failing does not stop the others.
func (s *TideSyncer) Sync(ctx context.Context) error {
	stations, err := s.source.Stations(ctx)
	if err != nil {
		return fmt.Errorf("list tide stations: %w", err)
	}
	byCode := make(map[string]source.TideStation, len(stations))
	for _, st := range stations {
		byCode[st.Code] = st
	}

	now := s.now()
	from, to := now.Add(-tideKeepBehind), now.Add(tideKeepAhead)
	var errs []error
	saved := 0
	for _, code := range s.codes {
		st, ok := byCode[code]
		if !ok {
			errs = append(errs, fmt.Errorf("tide station %s is not in HII's list", code))
			continue
		}
		preds, err := s.source.Predictions(ctx, code)
		if err != nil {
			errs = append(errs, fmt.Errorf("tide predictions for %s: %w", code, err))
			continue
		}
		var window []source.TidePrediction
		for _, p := range preds {
			if !p.At.Before(from) && !p.At.After(to) {
				window = append(window, p)
			}
		}
		// HII publishes a year at a time; a file that no longer covers now has
		// stopped being updated, which matters more than any one bad row.
		if len(window) == 0 {
			errs = append(errs, fmt.Errorf("tide predictions for %s do not cover %s", code, now.Format(time.DateOnly)))
			continue
		}
		if err := s.store.SaveTides(ctx, st, window); err != nil {
			errs = append(errs, err)
			continue
		}
		saved += len(window)
	}
	s.log.Info("tides synced", "stations", len(s.codes), "predictions", saved, "failed", len(errs))
	return errors.Join(errs...)
}
