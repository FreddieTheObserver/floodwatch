// Package collect polls every source on a fixed interval and saves what it gets.
package collect

import (
	"context"
	"log/slog"
	"time"

	"github.com/FreddieTheObserver/floodwatch/internal/source"
	"github.com/FreddieTheObserver/floodwatch/internal/store"
)

// A source that keeps failing is retried at most this rarely. Failures are often
// a server shedding load or a firewall rate limit, and hammering either during a
// flood helps nobody.
const maxBackoff = time.Hour

type Saver interface {
	SaveBatch(ctx context.Context, b source.Batch) (store.Saved, error)
}

type sourceState struct {
	fetcher  source.Fetcher
	failures int
	skips    int
}

type Collector struct {
	sources      []*sourceState
	saver        Saver
	log          *slog.Logger
	interval     time.Duration
	fetchTimeout time.Duration
}

func New(fetchers []source.Fetcher, saver Saver, log *slog.Logger, interval, fetchTimeout time.Duration) *Collector {
	c := &Collector{saver: saver, log: log, interval: interval, fetchTimeout: fetchTimeout}
	for _, f := range fetchers {
		c.sources = append(c.sources, &sourceState{fetcher: f})
	}
	return c
}

// Run polls once straight away, then on every tick until ctx is done. A tick
// that comes due while a poll is still running is dropped, not queued.
func (c *Collector) Run(ctx context.Context) {
	c.Poll(ctx)
	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.Poll(ctx)
		}
	}
}

// Poll fetches every source once. Sources run one after another so the public
// servers never see a burst, and a failing source never blocks the others.
func (c *Collector) Poll(ctx context.Context) {
	start := time.Now()
	var total store.Saved
	var failed, backingOff int
	for _, s := range c.sources {
		if ctx.Err() != nil {
			return
		}
		if s.skips > 0 {
			s.skips--
			backingOff++
			continue
		}
		saved, err := c.collect(ctx, s)
		if err != nil {
			failed++
			continue
		}
		total.Stations += saved.Stations
		total.Water += saved.Water
		total.Rain += saved.Rain
	}
	c.log.Info("poll done",
		"sources", len(c.sources), "failed", failed, "backing_off", backingOff,
		"stations", total.Stations, "new_water", total.Water, "new_rain", total.Rain,
		"took", time.Since(start).Round(time.Millisecond).String())
}

func (c *Collector) collect(ctx context.Context, s *sourceState) (store.Saved, error) {
	name := s.fetcher.Name()
	fetchCtx, cancel := context.WithTimeout(ctx, c.fetchTimeout)
	b, err := s.fetcher.Fetch(fetchCtx)
	cancel()
	if err != nil {
		s.failures++
		s.skips = c.skipsAfter(s.failures)
		c.log.Warn("fetch failed", "source", name, "err", err,
			"failures", s.failures, "next_try_in", (time.Duration(s.skips+1) * c.interval).String())
		return store.Saved{}, err
	}
	if s.failures > 0 {
		c.log.Info("source recovered", "source", name, "after_failures", s.failures)
		s.failures = 0
	}

	// Neither case is an error for the feed, but both are what a silent change
	// of its format looks like from here.
	switch {
	case len(b.Stations) == 0:
		c.log.Warn("source returned no stations", "source", name, "skipped", b.Skipped)
	case len(b.Water)+len(b.Rain) == 0:
		c.log.Warn("source returned no usable readings", "source", name, "skipped", b.Skipped)
	}
	saved, err := c.saver.SaveBatch(ctx, b)
	if err != nil {
		c.log.Error("save failed", "source", name, "err", err)
		return store.Saved{}, err
	}
	c.log.Debug("collected", "source", name,
		"stations", saved.Stations, "new_water", saved.Water, "new_rain", saved.Rain, "skipped", b.Skipped)
	return saved, nil
}

// skipsAfter doubles the wait with each consecutive failure: the first failure
// retries on the next poll, then after 2, 4, 8 polls, capped at maxBackoff.
func (c *Collector) skipsAfter(failures int) int {
	maxSkips := max(int(maxBackoff/c.interval)-1, 0)
	return min(1<<min(failures-1, 16)-1, maxSkips)
}
