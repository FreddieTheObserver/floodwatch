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
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/FreddieTheObserver/floodwatch/internal/alert"
	"github.com/FreddieTheObserver/floodwatch/internal/bot"
	"github.com/FreddieTheObserver/floodwatch/internal/collect"
	"github.com/FreddieTheObserver/floodwatch/internal/config"
	"github.com/FreddieTheObserver/floodwatch/internal/obs"
	"github.com/FreddieTheObserver/floodwatch/internal/source"
	"github.com/FreddieTheObserver/floodwatch/internal/store"
	"github.com/FreddieTheObserver/floodwatch/internal/telegram"
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
	collector := collect.New(fetchers, st, log, cfg.PollInterval, cfg.FetchTimeout)

	log.Info("floodwatch starting", "sources", len(fetchers), "bma_rain", cfg.BMARain,
		"poll_interval", cfg.PollInterval.String(), "telegram", cfg.TelegramToken != "")

	g, ctx := errgroup.WithContext(ctx)
	var afterPoll func(context.Context)
	if cfg.TelegramToken != "" {
		// Past the bot's 50 s long poll, so only a request that has truly hung
		// is cut off, and a stuck send cannot stall the collector for ever.
		tg := telegram.New(cfg.TelegramToken, &http.Client{Timeout: 90 * time.Second})
		b := bot.New(tg, st, alert.NewEvaluator(st), log)
		afterPoll = b.Notify
		// A rejected token ends the whole process: collecting without ever
		// alerting would look healthy while doing nothing useful.
		g.Go(func() error { return b.Run(ctx) })
	} else {
		log.Warn("FLOODWATCH_TELEGRAM_TOKEN is not set; collecting without the bot")
	}
	g.Go(func() error {
		collector.Run(ctx, afterPoll)
		return nil
	})

	err = g.Wait()
	log.Info("floodwatch stopped")
	return err
}
