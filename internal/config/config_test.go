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
	if want := []string{"N01", "N02", "N03", "N04", "N05"}; !slices.Equal(cfg.TideStations, want) {
		t.Errorf("tide stations = %v, want %v", cfg.TideStations, want)
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
		"FLOODWATCH_TIDE_STATIONS":    "N02,../N03",
	}))
	if err == nil {
		t.Fatal("want an error")
	}
	for _, name := range []string{
		"FLOODWATCH_DSN", "FLOODWATCH_POLL_INTERVAL", "FLOODWATCH_FETCH_TIMEOUT",
		"FLOODWATCH_PROVINCES", "FLOODWATCH_LOG_LEVEL", "FLOODWATCH_BMA_RAIN_ENABLED", "FLOODWATCH_TIDE_STATIONS",
	} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error does not mention %s: %v", name, err)
		}
	}
}

func TestHealthcheckURL(t *testing.T) {
	const ping = "https://hc-ping.com/0a1b2c3d-secret"
	cfg, err := Load(env(map[string]string{"FLOODWATCH_DSN": "postgres://x", "FLOODWATCH_HEALTHCHECK_URL": ping}))
	if err != nil || cfg.HealthcheckURL != ping {
		t.Errorf("url = %q, %v", cfg.HealthcheckURL, err)
	}

	const bad = "http://hc-ping.com/0a1b2c3d-secret"
	_, err = Load(env(map[string]string{"FLOODWATCH_DSN": "postgres://x", "FLOODWATCH_HEALTHCHECK_URL": bad}))
	if err == nil || !strings.Contains(err.Error(), "FLOODWATCH_HEALTHCHECK_URL") {
		t.Fatalf("plain http accepted: %v", err)
	}
	// Anyone holding the URL can forge pings, so errors never repeat it.
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("error leaks the URL: %v", err)
	}
}

func TestPollIntervalFloor(t *testing.T) {
	cfg, err := Load(env(map[string]string{"FLOODWATCH_DSN": "postgres://x", "FLOODWATCH_POLL_INTERVAL": "5m"}))
	if err != nil || cfg.PollInterval != 5*time.Minute {
		t.Errorf("5m: %v, %v", cfg.PollInterval, err)
	}
}

func TestHealthcheckAPIKey(t *testing.T) {
	const ping, key = "https://hc-ping.com/0a1b2c3d-secret", "rw-key-not-for-logs"
	cfg, err := Load(env(map[string]string{"FLOODWATCH_DSN": "postgres://x", "FLOODWATCH_HEALTHCHECK_URL": ping, "FLOODWATCH_HEALTHCHECK_API_KEY": key}))
	if err != nil || cfg.HealthcheckAPIKey != key {
		t.Errorf("key = %q, %v", cfg.HealthcheckAPIKey, err)
	}

	_, err = Load(env(map[string]string{"FLOODWATCH_DSN": "postgres://x", "FLOODWATCH_HEALTHCHECK_API_KEY": key}))
	if err == nil || !strings.Contains(err.Error(), "FLOODWATCH_HEALTHCHECK_API_KEY") {
		t.Fatalf("a key with no check to pause = %v, want an error", err)
	}
	if strings.Contains(err.Error(), key) {
		t.Errorf("error leaks the key: %v", err)
	}
}

// Pausing the watchdog must work outside the Makefile, which is what sets
// the database for everything else.
func TestTheWatchdogLoadsOnItsOwn(t *testing.T) {
	const ping, key = "https://hc-ping.com/0a1b2c3d-secret", "rw-key-not-for-logs"
	w, err := LoadWatchdog(env(map[string]string{"FLOODWATCH_HEALTHCHECK_URL": ping, "FLOODWATCH_HEALTHCHECK_API_KEY": key}))
	if err != nil || w.URL != ping || w.APIKey != key {
		t.Errorf("watchdog = %+v, %v", w, err)
	}
	if _, err := LoadWatchdog(env(map[string]string{"FLOODWATCH_HEALTHCHECK_URL": "http://hc-ping.com/x"})); err == nil {
		t.Error("plain http accepted")
	}
}
