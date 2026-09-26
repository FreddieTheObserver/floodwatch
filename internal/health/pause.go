package health

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const apiBase = "https://healthchecks.io/api/v3"

// The check a ping URL of the form https://hc-ping.com/<uuid> reports to.
var pingUUID = regexp.MustCompile(`^https://hc-ping\.com/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})/?$`)

// Pause stops the watchdog alarming, for when floodwatch is stopped on
// purpose. Healthchecks.io resumes a paused check at its next ping, so
// floodwatch's first report after it starts again rearms the alarm.
func Pause(ctx context.Context, client *http.Client, pingURL, apiKey string) error {
	return pause(ctx, client, apiBase, pingURL, apiKey)
}

func pause(ctx context.Context, client *http.Client, base, pingURL, apiKey string) error {
	m := pingUUID.FindStringSubmatch(pingURL)
	if m == nil {
		return errors.New("pausing needs a ping URL of the form https://hc-ping.com/<uuid>")
	}
	uuid := m[1]
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/checks/"+uuid+"/pause", http.NoBody)
	if err != nil {
		return redactCheck(err, uuid)
	}
	req.Header.Set("X-Api-Key", apiKey)
	resp, err := client.Do(req)
	if err != nil {
		return redactCheck(err, uuid)
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("pause the watchdog: healthchecks.io refused the API key (status %d); it must be a current read-write key of the check's project", resp.StatusCode)
	default:
		return fmt.Errorf("pause the watchdog: status %d", resp.StatusCode)
	}
}

// redactCheck keeps the check's UUID, which its ping URL is made of, out of
// an error.
func redactCheck(err error, uuid string) error {
	if urlErr, ok := errors.AsType[*url.Error](err); ok {
		return fmt.Errorf("pause the watchdog: %s: %w", urlErr.Op, urlErr.Err)
	}
	return fmt.Errorf("pause the watchdog: %s", strings.ReplaceAll(err.Error(), uuid, "<check>"))
}
