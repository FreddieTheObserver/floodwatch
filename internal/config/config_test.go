package config

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{"FLOODWATCH_DSN": "postgres://x"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PollInterval != DefaultPollInterval || cfg.FetchTimeout != DefaultFetchTimeout {
		t.Errorf("intervals = %v/%v", cfg.PollInterval, cfg.FetchTimeout)
	}
	if want := []string{"10", "11", "12", "13", "73", "74"}; !slices.Equal(cfg.Provinces, want) {
		t.Errorf("provinces = %v, want %v", cfg.Provinces, want)
	}
	if cfg.BMARain {
		t.Error("BMA rain is on by default; it must stay off until permission is granted")
	}
}

func TestLoadBMARain(t *testing.T) {
	cfg, err := Load(env(map[string]string{"FLOODWATCH_DSN": "postgres://x", "FLOODWATCH_BMA_RAIN_ENABLED": "true"}))
	if err != nil || !cfg.BMARain {
		t.Errorf("enabled = %v, %v", cfg.BMARain, err)
	}
}

func TestLoadProvinces(t *testing.T) {
	cfg, err := Load(env(map[string]string{"FLOODWATCH_DSN": "postgres://x", "FLOODWATCH_PROVINCES": " 10, 11,10 "}))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"10", "11"}; !slices.Equal(cfg.Provinces, want) {
		t.Errorf("provinces = %v, want %v", cfg.Provinces, want)
	}
}

func TestLoadReportsEveryProblem(t *testing.T) {
	_, err := Load(env(map[string]string{
		"FLOODWATCH_POLL_INTERVAL":    "1m",
		"FLOODWATCH_FETCH_TIMEOUT":    "soon",
		"FLOODWATCH_PROVINCES":        "10,bangkok",
		"FLOODWATCH_LOG_LEVEL":        "loud",
		"FLOODWATCH_BMA_RAIN_ENABLED": "yes please",
	}))
	if err == nil {
		t.Fatal("want an error")
	}
	for _, name := range []string{
		"FLOODWATCH_DSN", "FLOODWATCH_POLL_INTERVAL", "FLOODWATCH_FETCH_TIMEOUT",
		"FLOODWATCH_PROVINCES", "FLOODWATCH_LOG_LEVEL", "FLOODWATCH_BMA_RAIN_ENABLED",
	} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error does not mention %s: %v", name, err)
		}
	}
}

func TestPollIntervalFloor(t *testing.T) {
	cfg, err := Load(env(map[string]string{"FLOODWATCH_DSN": "postgres://x", "FLOODWATCH_POLL_INTERVAL": "5m"}))
	if err != nil || cfg.PollInterval != 5*time.Minute {
		t.Errorf("5m: %v, %v", cfg.PollInterval, err)
	}
}
