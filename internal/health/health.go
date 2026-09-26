// Package health reports to an outside watchdog such as healthchecks.io,
// which raises the alarm when the reports turn to failures or stop. A dead
// process cannot report its own death, so the alarm has to live elsewhere.
package health

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	pingTimeout = 10 * time.Second
	// One failed poll is usually a blip the next one fixes, like a DNS
	// timeout; this many in a row is an outage worth waking someone for.
	failuresToAlarm = 3
)

type Monitor struct {
	client      *http.Client
	url         string
	log         *slog.Logger
	consecutive int
}

func New(pingURL string, client *http.Client, log *slog.Logger) *Monitor {
	return &Monitor{client: client, url: strings.TrimSuffix(pingURL, "/"), log: log}
}

// Observe reports one poll. problems empty means it went well; detail is a
// summary the watchdog shows beside the ping.
func (m *Monitor) Observe(ctx context.Context, problems []string, detail string) {
	if len(problems) == 0 {
		m.consecutive = 0
		m.ping(ctx, m.url, detail)
		return
	}
	m.consecutive++
	m.log.Warn("unhealthy poll", "consecutive", m.consecutive, "problems", strings.Join(problems, "; "))
	if m.consecutive >= failuresToAlarm {
		body := fmt.Sprintf("%d polls in a row failed:\n- %s\n\n%s", m.consecutive, strings.Join(problems, "\n- "), detail)
		m.ping(ctx, m.url+"/fail", body)
	}
}

func (m *Monitor) ping(ctx context.Context, target, body string) {
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(body))
	if err != nil {
		m.log.Warn("healthcheck ping failed", "err", m.redact(err))
		return
	}
	resp, err := m.client.Do(req)
	if err != nil {
		m.log.Warn("healthcheck ping failed", "err", m.redact(err))
		return
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		m.log.Warn("healthcheck ping refused", "status", resp.StatusCode)
	}
}

// redact keeps the ping URL out of logs; anyone holding it can forge pings.
func (m *Monitor) redact(err error) error {
	if urlErr, ok := errors.AsType[*url.Error](err); ok {
		return fmt.Errorf("%s <healthcheck url>: %w", urlErr.Op, urlErr.Err)
	}
	return errors.New(strings.ReplaceAll(err.Error(), m.url, "<healthcheck url>"))
}
