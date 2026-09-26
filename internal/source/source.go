// Package source fetches water level and rainfall from public feeds and
// normalises them into one shape the store can save.
package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Kind string

const (
	KindWater Kind = "water"
	KindRain  Kind = "rain"
)

// Thailand has no daylight saving, so a fixed zone is exact and needs no tzdata.
var ict = time.FixedZone("ICT", 7*60*60)

type Station struct {
	ExternalID string
	Name       string
	NameTH     string
	District   string
	Lat, Lng   float64
	BankMSL    *float64
	// Agency is the short name of the organisation that runs the gauge, which
	// for a republishing feed like ThaiWater is not the feed itself.
	Agency string
}

type WaterReading struct {
	ExternalID string
	ObservedAt time.Time
	LevelMSL   float64
}

type RainReading struct {
	ExternalID              string
	ObservedAt              time.Time
	Rain1h, Rain3h, Rain24h *float64
}

// Batch is the result of one fetch from one endpoint. Every reading refers to a
// station in the same batch by ExternalID. Skipped counts source rows that were
// dropped because they could not be trusted.
type Batch struct {
	Source   string
	Kind     Kind
	Stations []Station
	Water    []WaterReading
	Rain     []RainReading
	Skipped  int
}

type Fetcher interface {
	Name() string
	Fetch(ctx context.Context) (Batch, error)
}

const (
	userAgent    = "floodwatch/0.1 (personal flood alert bot)"
	maxBodyBytes = 16 << 20
	// A reading stamped further ahead than this is a broken clock, not data.
	maxClockSkew = time.Hour
)

var utf8BOM = []byte("\xEF\xBB\xBF")

func fetchJSON(ctx context.Context, client *http.Client, method, url string, v any) error {
	body, err := fetch(ctx, client, method, url, "application/json")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("%s %s: decode: %w", method, url, err)
	}
	return nil
}

// fetch returns a response body, bounded in size and without a UTF-8 byte
// order mark, which some feeds prefix.
func fetch(ctx context.Context, client *http.Client, method, url, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", accept)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s %s: status %d", method, url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%s %s: read body: %w", method, url, err)
	}
	if len(body) > maxBodyBytes {
		return nil, fmt.Errorf("%s %s: body exceeds %d bytes", method, url, maxBodyBytes)
	}
	return bytes.TrimPrefix(body, utf8BOM), nil
}

// num decodes a value the feeds send as a JSON number, a numeric string, an
// empty string or null. Anything unparseable is treated as missing, so one bad
// row is dropped instead of failing the whole batch.
type num struct {
	v  float64
	ok bool
}

func (n *num) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if unquoted, err := strconv.Unquote(s); err == nil {
		s = unquoted
	}
	*n = parseNum(s)
	return nil
}

// parseNum reads a number from text, as missing when it is not one.
func parseNum(s string) num {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return num{}
	}
	return num{v: f, ok: true}
}

func (n num) ptr() *float64 {
	if !n.ok {
		return nil
	}
	v := n.v
	return &v
}

// nonNegative drops negative rainfall, which sensors emit as an error sentinel.
func (n num) nonNegative() *float64 {
	if !n.ok || n.v < 0 {
		return nil
	}
	return n.ptr()
}

func validCoords(lat, lng num) bool {
	return lat.ok && lng.ok &&
		lat.v >= -90 && lat.v <= 90 && lng.v >= -180 && lng.v <= 180 &&
		(lat.v != 0 || lng.v != 0)
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}
