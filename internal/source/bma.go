package source

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const bmaRainURL = "https://weather.bangkok.go.th/rain/PageMap/GetDataForUpdate"

// BMARain fetches the rain gauges of the Bangkok Metropolitan Administration's
// Drainage and Sewerage Department, a far denser network than ThaiWater's.
func BMARain(client *http.Client) Fetcher { return bmaRain{client: client} }

type bmaRain struct{ client *http.Client }

func (bmaRain) Name() string { return "bma-rain" }

func (f bmaRain) Fetch(ctx context.Context) (Batch, error) {
	var rows []bmaRainRow
	// The endpoint only answers POST, with an empty body.
	if err := fetchJSON(ctx, f.client, http.MethodPost, bmaRainURL, &rows); err != nil {
		return Batch{}, err
	}
	return bmaRainBatch(rows, time.Now()), nil
}

type bmaRainRow struct {
	Code      string `json:"rain_code"`
	NameEN    string `json:"rain_name_en"`
	NameTH    string `json:"rain_name"`
	District  string `json:"district_name_en"`
	Lat       num    `json:"latitude"`
	Lng       num    `json:"longitude"`
	Timestamp string `json:"site_timestamp"`
	Rain1h    num    `json:"rf1hr"`
	Rain3h    num    `json:"rf3hr"`
	Rain24h   num    `json:"rf24hr"`
}

func bmaRainBatch(rows []bmaRainRow, now time.Time) Batch {
	b := Batch{Source: "bma", Kind: KindRain}
	for _, row := range rows {
		name := firstNonEmpty(row.NameEN, row.NameTH)
		code := strings.TrimSpace(row.Code)
		if code == "" || name == "" || !validCoords(row.Lat, row.Lng) {
			b.Skipped++
			continue
		}
		b.Stations = append(b.Stations, Station{
			ExternalID: code,
			Name:       name,
			NameTH:     firstNonEmpty(row.NameTH),
			District:   firstNonEmpty(row.District),
			Lat:        row.Lat.v,
			Lng:        row.Lng.v,
			Agency:     "BMA",
		})

		at, err := parseDotNetDate(row.Timestamp)
		rain := RainReading{
			ExternalID: code,
			ObservedAt: at,
			Rain1h:     row.Rain1h.nonNegative(),
			Rain3h:     row.Rain3h.nonNegative(),
			Rain24h:    row.Rain24h.nonNegative(),
		}
		if err != nil || at.After(now.Add(maxClockSkew)) ||
			(rain.Rain1h == nil && rain.Rain3h == nil && rain.Rain24h == nil) {
			b.Skipped++
			continue
		}
		b.Rain = append(b.Rain, rain)
	}
	return b
}

// parseDotNetDate reads ASP.NET's "/Date(1790403000000)/", milliseconds since
// the Unix epoch, optionally followed by a UTC offset that does not change the
// instant.
func parseDotNetDate(s string) (time.Time, error) {
	inner, ok := strings.CutPrefix(s, "/Date(")
	if ok {
		inner, ok = strings.CutSuffix(inner, ")/")
	}
	if !ok || inner == "" {
		return time.Time{}, &time.ParseError{Layout: "/Date(ms)/", Value: s, Message: ": not an ASP.NET date"}
	}
	if i := strings.IndexAny(inner[1:], "+-"); i >= 0 {
		inner = inner[:i+1]
	}
	ms, err := strconv.ParseInt(inner, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(ms).In(ict), nil
}
