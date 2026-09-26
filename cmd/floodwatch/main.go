package main

import (
	"context"
	"errors"
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
	"github.com/FreddieTheObserver/floodwatch/internal/health"
	"github.com/FreddieTheObserver/floodwatch/internal/obs"
	"github.com/FreddieTheObserver/floodwatch/internal/source"
	"github.com/FreddieTheObserver/floodwatch/internal/store"
	"github.com/FreddieTheObserver/floodwatch/internal/telegram"
	"github.com/FreddieTheObserver/floodwatch/internal/tide"
)

func main() {
	// SIGHUP too, as closing the tmux session that runs the service sends it.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	// After the first signal starts a graceful shutdown, a second one kills.
	context.AfterFunc(ctx, stop)
	// Closing that session also ends the tee the output goes through, and Go
	// kills a program by SIGPIPE when it writes to a standard stream whose
	// reader is gone, so the first log line of the shutdown would cut it
	// short. Ignored, such writes fail quietly and the shutdown completes.
	signal.Ignore(syscall.SIGPIPE)

	var err error
	switch args := os.Args[1:]; {
	case len(args) == 0:
		err = run(ctx, os.Getenv, os.Stdout)
	case len(args) == 1 && args[0] == "pause-watchdog":
		err = pauseWatchdog(ctx, os.Getenv, os.Stdout)
	default:
		err = errors.New("usage: floodwatch [pause-watchdog]")
	}
	stop()
	if err != nil {
		fmt.Fprintf(os.Stderr, "floodwatch: %v\n", err)
		os.Exit(1)
	}
}

// pauseWatchdog is for stopping floodwatch on purpose: it pauses the outside
// watchdog, which the service's next report after starting rearms.
func pauseWatchdog(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	w, err := config.LoadWatchdog(getenv)
	if err != nil {
		return err
	}
	switch {
	case w.URL == "":
		fmt.Fprintln(stdout, "no watchdog is set up, so there is nothing to pause")
		return nil
	case w.APIKey == "":
		return errors.New("FLOODWATCH_HEALTHCHECK_API_KEY is not set, so the watchdog cannot be paused")
	}
	if err := health.Pause(ctx, &http.Client{Timeout: 10 * time.Second}, w.URL, w.APIKey); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "watchdog paused until floodwatch runs again")
	return nil
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
	sources := []string{"thaiwater"}
	if cfg.BMARain {
		fetchers = append(fetchers, source.BMARain(client))
		sources = append(sources, "bma")
	}
	for _, p := range cfg.Provinces {
		fetchers = append(fetchers, source.ThaiWaterLevels(client, p), source.ThaiWaterRain(client, p))
	}
	collector := collect.New(fetchers, st, log, cfg.PollInterval, cfg.FetchTimeout)

	log.Info("floodwatch starting", "sources", len(fetchers), "bma_rain", cfg.BMARain,
		"poll_interval", cfg.PollInterval.String(), "telegram", cfg.TelegramToken != "",
		"watchdog", cfg.HealthcheckURL != "")

	g, ctx := errgroup.WithContext(ctx)
	var b *bot.Bot
	if cfg.TelegramToken != "" {
		// Past the bot's 50 s long poll, so only a request that has truly hung
		// is cut off, and a stuck send cannot stall the collector for ever.
		tg := telegram.New(cfg.TelegramToken, &http.Client{Timeout: 90 * time.Second})
		b = bot.New(tg, st, alert.NewEvaluator(st, sources), log, cfg.PollInterval)
		// A rejected token ends the whole process: collecting without ever
		// alerting would look healthy while doing nothing useful.
		g.Go(func() error { return b.Run(ctx) })
	} else {
		log.Warn("FLOODWATCH_TELEGRAM_TOKEN is not set; collecting without the bot")
	}
	var watchdog *health.Monitor
	if cfg.HealthcheckURL != "" {
		watchdog = health.New(cfg.HealthcheckURL, client, log)
	} else {
		log.Warn("FLOODWATCH_HEALTHCHECK_URL is not set; nobody will be told if floodwatch stops working")
	}

	tideModel := tide.NewModel(st, log)
	afterPoll := func(ctx context.Context, r collect.PollResult) {
		var problems []string
		if !r.Healthy() {
			problems = append(problems, "no source delivered data")
		}
		// Before alerts, so they carry the forecast from the newest readings.
		// Alerts do not depend on it, so a failure is no reason to alarm.
		if err := tideModel.Update(ctx); err != nil {
			log.Warn("tide model update failed", "err", err)
		}
		if b != nil {
			if err := b.Notify(ctx); err != nil {
				problems = append(problems, "alerts: "+err.Error())
			}
			if err := b.Healthy(); err != nil {
				problems = append(problems, "telegram: "+err.Error())
			}
		}
		if watchdog != nil {
			watchdog.Observe(ctx, problems, r.String())
		}
	}
	g.Go(func() error {
		collector.Run(ctx, afterPoll)
		return nil
	})
	tides := collect.NewTideSyncer(source.HIITides(client), st, log, cfg.TideStations)
	g.Go(func() error {
		tides.Run(ctx)
		return nil
	})
	history := collect.NewHistorySyncer(source.ThaiWaterHistory(client), st, log, cfg.FetchTimeout)
	g.Go(func() error {
		history.Run(ctx)
		return nil
	})

	err = g.Wait()
	log.Info("floodwatch stopped")
	return err
}
