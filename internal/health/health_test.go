package health

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recorder struct {
	paths  []string
	bodies []string
}

func watchdog(t *testing.T) (*recorder, string) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.paths = append(rec.paths, r.URL.Path)
		rec.bodies = append(rec.bodies, string(body))
	}))
	t.Cleanup(srv.Close)
	return rec, srv.URL + "/0a1b2c3d"
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestHealthyPollsPing(t *testing.T) {
	rec, url := watchdog(t)
	m := New(url, http.DefaultClient, quiet())
	m.Observe(context.Background(), nil, "sources 12, failed 0")
	if len(rec.paths) != 1 || rec.paths[0] != "/0a1b2c3d" || rec.bodies[0] != "sources 12, failed 0" {
		t.Errorf("pings = %v %q", rec.paths, rec.bodies)
	}
}

// A blip must not wake anyone; an outage must, with its reasons.
func TestOnlyAStreakOfFailuresRaisesTheAlarm(t *testing.T) {
	rec, url := watchdog(t)
	m := New(url, http.DefaultClient, quiet())
	ctx := context.Background()

	m.Observe(ctx, []string{"every source failed"}, "")
	m.Observe(ctx, nil, "recovered")
	m.Observe(ctx, []string{"every source failed"}, "")
	m.Observe(ctx, []string{"every source failed"}, "")
	if len(rec.paths) != 1 {
		t.Fatalf("pings after a blip and two failures = %v, want only the recovery", rec.paths)
	}

	m.Observe(ctx, []string{"every source failed", "telegram: no contact for 12m"}, "sources 12, failed 12")
	if len(rec.paths) != 2 || rec.paths[1] != "/0a1b2c3d/fail" {
		t.Fatalf("pings = %v, want a failure after three in a row", rec.paths)
	}
	if body := rec.bodies[1]; !strings.Contains(body, "3 polls in a row failed") || !strings.Contains(body, "telegram: no contact") {
		t.Errorf("failure body = %q", body)
	}
}

func TestThePingURLNeverReachesTheLog(t *testing.T) {
	var logs bytes.Buffer
	const secret = "0a1b2c3d-secret"
	// Nothing listens here, so the request fails in the transport, whose error
	// quotes the URL.
	m := New("http://127.0.0.1:1/"+secret, http.DefaultClient, slog.New(slog.NewTextHandler(&logs, nil)))
	m.Observe(context.Background(), nil, "")
	if !strings.Contains(logs.String(), "healthcheck ping failed") {
		t.Fatalf("no warning logged: %s", logs.String())
	}
	if strings.Contains(logs.String(), secret) {
		t.Errorf("log leaks the ping URL: %s", logs.String())
	}
}
