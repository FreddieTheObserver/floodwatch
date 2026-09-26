package config

import (
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DSN          string
	LogLevel     slog.Level
	PollInterval time.Duration
	FetchTimeout time.Duration
	Provinces    []string
	BMARain      bool
}

const (
	DefaultPollInterval = 10 * time.Minute
	DefaultFetchTimeout = 30 * time.Second
	// Bangkok and the five provinces around it. Stations just across the border
	// are often the nearest ones for the city's outer districts.
	DefaultProvinces = "10,11,12,13,73,74"

	// The feeds are public servers that are busiest exactly when floods happen.
	MinPollInterval = 5 * time.Minute
)

var provinceCode = regexp.MustCompile(`^[0-9]{2}$`)

// Load reads the configuration through getenv and reports every invalid
// variable at once, rather than one per restart.
func Load(getenv func(string) string) (Config, error) {
	l := loader{getenv: getenv}
	cfg := Config{
		DSN:          l.text("FLOODWATCH_DSN", ""),
		LogLevel:     l.level("FLOODWATCH_LOG_LEVEL"),
		PollInterval: l.duration("FLOODWATCH_POLL_INTERVAL", DefaultPollInterval, MinPollInterval),
		FetchTimeout: l.duration("FLOODWATCH_FETCH_TIMEOUT", DefaultFetchTimeout, time.Second),
		Provinces:    l.provinces("FLOODWATCH_PROVINCES", DefaultProvinces),
		// Off until the BMA Drainage Department permits automated access to its
		// rain gauges; permission was requested on 2026-09-26.
		BMARain: l.boolean("FLOODWATCH_BMA_RAIN_ENABLED", false),
	}
	if cfg.DSN == "" {
		l.errs = append(l.errs, errors.New("FLOODWATCH_DSN is required"))
	}
	return cfg, errors.Join(l.errs...)
}

type loader struct {
	getenv func(string) string
	errs   []error
}

func (l *loader) fail(name, want, raw string) {
	l.errs = append(l.errs, fmt.Errorf("%s must be %s, got %q", name, want, raw))
}

func (l *loader) text(name, def string) string {
	if raw := l.getenv(name); raw != "" {
		return raw
	}
	return def
}

func (l *loader) duration(name string, def, minimum time.Duration) time.Duration {
	raw := l.getenv(name)
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < minimum {
		l.fail(name, "a duration of at least "+minimum.String(), raw)
		return def
	}
	return d
}

func (l *loader) level(name string) slog.Level {
	raw := l.getenv(name)
	var level slog.Level
	if raw == "" {
		return level
	}
	if err := level.UnmarshalText([]byte(raw)); err != nil {
		l.fail(name, "one of debug, info, warn or error", raw)
	}
	return level
}

func (l *loader) boolean(name string, def bool) bool {
	raw := l.getenv(name)
	if raw == "" {
		return def
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		l.fail(name, "true or false", raw)
		return def
	}
	return b
}

func (l *loader) provinces(name, def string) []string {
	raw := l.text(name, def)
	var codes []string
	for _, part := range strings.Split(raw, ",") {
		code := strings.TrimSpace(part)
		if !provinceCode.MatchString(code) {
			l.fail(name, "a comma-separated list of two-digit province codes", raw)
			return nil
		}
		if !slices.Contains(codes, code) {
			codes = append(codes, code)
		}
	}
	return codes
}
