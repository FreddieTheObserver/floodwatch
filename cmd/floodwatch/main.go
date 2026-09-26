package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/FreddieTheObserver/floodwatch/internal/collect"
	"github.com/FreddieTheObserver/floodwatch/internal/config"
	"github.com/FreddieTheObserver/floodwatch/internal/obs"
	"github.com/FreddieTheObserver/floodwatch/internal/source"
	"github.com/FreddieTheObserver/floodwatch/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// After the first signal starts a graceful shutdown, a second one kills.
	context.AfterFunc(ctx, stop)

	err := run(ctx, os.Getenv, os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintf(os.Stderr, "floodwatch: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	log := obs.NewLogger(stdout, cfg.LogLevel)
	slog.SetDefault(log)

	st, err := store.New(ctx, store.Config{DSN: cfg.DSN})
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	if err := st.Migrate(ctx); err != nil {
		return err
	}

	client := &http.Client{}
	var fetchers []source.Fetcher
	if cfg.BMARain {
		fetchers = append(fetchers, source.BMARain(client))
	}
	for _, p := range cfg.Provinces {
		fetchers = append(fetchers, source.ThaiWaterLevels(client, p), source.ThaiWaterRain(client, p))
	}

	log.Info("floodwatch starting", "sources", len(fetchers), "bma_rain", cfg.BMARain,
		"poll_interval", cfg.PollInterval.String())
	collect.New(fetchers, st, log, cfg.PollInterval, cfg.FetchTimeout).Run(ctx)
	log.Info("floodwatch stopped")
	return nil
}
