package collect

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/FreddieTheObserver/floodwatch/internal/source"
	"github.com/FreddieTheObserver/floodwatch/internal/store"
)

type fakeFetcher struct {
	name string
	b    source.Batch
	err  error
}

func (f fakeFetcher) Name() string { return f.name }

func (f fakeFetcher) Fetch(context.Context) (source.Batch, error) { return f.b, f.err }

type fakeSaver struct {
	saved []string
	fail  string
}

func (s *fakeSaver) SaveBatch(_ context.Context, b source.Batch) (store.Saved, error) {
	if b.Source == s.fail {
		return store.Saved{}, errors.New("db down")
	}
	s.saved = append(s.saved, b.Source)
	return store.Saved{Stations: len(b.Stations)}, nil
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestPollIsolatesFailures(t *testing.T) {
	batch := func(src string) source.Batch {
		return source.Batch{Source: src, Stations: []source.Station{{ExternalID: "1"}}}
	}
	fetchers := []source.Fetcher{
		fakeFetcher{name: "a", b: batch("a")},
		fakeFetcher{name: "broken", err: errors.New("503")},
		fakeFetcher{name: "unsaveable", b: batch("unsaveable")},
		fakeFetcher{name: "d", b: batch("d")},
	}
	saver := &fakeSaver{fail: "unsaveable"}

	New(fetchers, saver, quiet(), time.Hour, time.Second).Poll(context.Background())

	if len(saver.saved) != 2 || saver.saved[0] != "a" || saver.saved[1] != "d" {
		t.Errorf("saved = %v, want [a d]", saver.saved)
	}
}

// scriptedFetcher fails while fail returns true for the 1-based call number,
// and records on which poll each call happened.
type scriptedFetcher struct {
	fail  func(call int) bool
	poll  *int
	calls []int
}

func (f *scriptedFetcher) Name() string { return "scripted" }

func (f *scriptedFetcher) Fetch(context.Context) (source.Batch, error) {
	f.calls = append(f.calls, *f.poll)
	if f.fail(len(f.calls)) {
		return source.Batch{}, errors.New("403")
	}
	return source.Batch{Source: "scripted", Stations: []source.Station{{ExternalID: "1"}}}, nil
}

func pollTimes(c *Collector, poll *int, n int) {
	for *poll = 1; *poll <= n; *poll++ {
		c.Poll(context.Background())
	}
}

func TestBackoffDoublesUpToAnHour(t *testing.T) {
	var poll int
	f := &scriptedFetcher{fail: func(int) bool { return true }, poll: &poll}
	c := New([]source.Fetcher{f}, &fakeSaver{}, quiet(), 10*time.Minute, time.Second)

	pollTimes(c, &poll, 20)

	// Waits of 1, 2, 4 polls, then capped at 6 polls (an hour at 10m).
	want := []int{1, 2, 4, 8, 14, 20}
	if !slices.Equal(f.calls, want) {
		t.Errorf("fetched on polls %v, want %v", f.calls, want)
	}
}

func TestBackoffResetsAfterSuccess(t *testing.T) {
	var poll int
	f := &scriptedFetcher{fail: func(call int) bool { return call <= 2 }, poll: &poll}
	c := New([]source.Fetcher{f}, &fakeSaver{}, quiet(), 10*time.Minute, time.Second)

	pollTimes(c, &poll, 6)

	want := []int{1, 2, 4, 5, 6}
	if !slices.Equal(f.calls, want) {
		t.Errorf("fetched on polls %v, want %v", f.calls, want)
	}
}

func TestRunCallsAfterPollOnceReadingsAreSaved(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	saver := &fakeSaver{}
	fetchers := []source.Fetcher{fakeFetcher{name: "a", b: source.Batch{Source: "a", Stations: []source.Station{{ExternalID: "1"}}}}}

	evaluated := make(chan int, 1)
	go New(fetchers, saver, quiet(), time.Hour, time.Second).Run(ctx, func(context.Context) {
		evaluated <- len(saver.saved)
	})
	select {
	case n := <-evaluated:
		if n != 1 {
			t.Errorf("afterPoll ran with %d batches saved, want the poll finished first", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("afterPoll was not called after the first poll")
	}
}

func TestRunStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		New(nil, &fakeSaver{}, quiet(), time.Hour, time.Second).Run(ctx, nil)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
