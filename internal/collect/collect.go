// Package collect polls every source on a fixed interval and saves what it gets.
package collect

import (
	"context"
	"fmt"
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

// PollResult is the outcome of one poll, for judging whether collection works.
type PollResult struct {
	Sources, Failed, BackingOff int
	Saved                       store.Saved
}

// Healthy reports whether any source delivered. A source backing off counts
// as failing, since it is only skipped because it kept failing.
func (r PollResult) Healthy() bool {
	return r.Sources == 0 || r.Failed+r.BackingOff < r.Sources
}

func (r PollResult) String() string {
	return fmt.Sprintf("sources %d, failed %d, backing off %d, new water readings %d, new rain readings %d",
		r.Sources, r.Failed, r.BackingOff, r.Saved.Water, r.Saved.Rain)
}

// Run polls once straight away, then on every tick until ctx is done, calling
// afterPoll (if not nil) once each poll's readings are saved. A tick that comes
// due while a poll is still running is dropped, not queued.
func (c *Collector) Run(ctx context.Context, afterPoll func(context.Context, PollResult)) {
	poll := func() {
		r := c.Poll(ctx)
		if afterPoll != nil && ctx.Err() == nil {
			afterPoll(ctx, r)
		}
	}
	poll()
	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			poll()
		}
	}
}

// Poll fetches every source once. Sources run one after another so the public
// servers never see a burst, and a failing source never blocks the others.
func (c *Collector) Poll(ctx context.Context) PollResult {
	start := time.Now()
	r := PollResult{Sources: len(c.sources)}
	for _, s := range c.sources {
		if ctx.Err() != nil {
			return r
		}
		if s.skips > 0 {
			s.skips--
			r.BackingOff++
			continue
		}
		saved, err := c.collect(ctx, s)
		if err != nil {
			r.Failed++
			continue
		}
		r.Saved.Stations += saved.Stations
		r.Saved.Water += saved.Water
		r.Saved.Rain += saved.Rain
	}
	c.log.Info("poll done",
		"sources", r.Sources, "failed", r.Failed, "backing_off", r.BackingOff,
		"stations", r.Saved.Stations, "new_water", r.Saved.Water, "new_rain", r.Saved.Rain,
		"took", time.Since(start).Round(time.Millisecond).String())
	return r
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
