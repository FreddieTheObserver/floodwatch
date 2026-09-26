package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	checkUUID = "0a1b2c3d-1111-2222-3333-444455556666"
	pingURL   = "https://hc-ping.com/" + checkUUID
	apiKey    = "rw-key-not-for-logs"
)

func TestPauseCallsTheManagementAPI(t *testing.T) {
	var method, path, key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, key = r.Method, r.URL.Path, r.Header.Get("X-Api-Key")
	}))
	defer srv.Close()

	if err := pause(context.Background(), srv.Client(), srv.URL+"/api/v3", pingURL, apiKey); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/api/v3/checks/"+checkUUID+"/pause" || key != apiKey {
		t.Errorf("request = %s %s with key %q", method, path, key)
	}
}

func TestPauseExplainsARefusedKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	err := pause(context.Background(), srv.Client(), srv.URL+"/api/v3", pingURL, apiKey)
	if err == nil || !strings.Contains(err.Error(), "read-write") {
		t.Errorf("refused key = %v, want it to say a read-write key is needed", err)
	}
}

// The check's UUID lets anyone forge its pings, and the key lets anyone
// change the project's checks, so neither appears in an error.
func TestPauseKeepsSecretsOutOfErrors(t *testing.T) {
	for name, base := range map[string]string{
		"unreachable": "http://127.0.0.1:1/api/v3",
		"bad URL":     "http://[::1/api/v3",
	} {
		err := pause(context.Background(), http.DefaultClient, base, pingURL, apiKey)
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		if strings.Contains(err.Error(), checkUUID) || strings.Contains(err.Error(), apiKey) {
			t.Errorf("%s: error leaks a secret: %v", name, err)
		}
	}
}

func TestPauseNeedsAPingURLNamingTheCheck(t *testing.T) {
	for _, u := range []string{"https://hc-ping.com/ping-key/floodwatch", "https://example.com/" + checkUUID} {
		if err := pause(context.Background(), http.DefaultClient, "http://unused", u, apiKey); err == nil {
			t.Errorf("paused through %q", u)
		}
	}
}
