package source

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// HII publishes a year of hourly tide predictions per station as plain CSV,
// the same files ThaiWater's sea level page reads.
const hiiTideBase = "https://fews2.hii.or.th/model-output/data_portal/tide_table/"

// TideCode is the form of HII's station codes, such as N02. Codes come from
// configuration and end up in a URL, so nothing else may pass.
var TideCode = regexp.MustCompile(`^[A-Z][0-9]{2}$`)

type TideStation struct {
	Code     string
	Name     string
	NameTH   string
	Lat, Lng float64
}

type TidePrediction struct {
	At     time.Time
	LevelM float64
}

// HIITides reads HII's tide predictions.
func HIITides(client *http.Client) HII { return HII{client: client} }

type HII struct{ client *http.Client }

// Stations lists every station HII predicts tides for.
func (h HII) Stations(ctx context.Context) ([]TideStation, error) {
	body, err := fetch(ctx, h.client, http.MethodGet, hiiTideBase+"summary.txt", "text/plain")
	if err != nil {
		return nil, err
	}
	return parseTideStations(bytes.NewReader(body))
}

// Predictions returns one station's hourly predictions, usually a year's worth.
func (h HII) Predictions(ctx context.Context, code string) ([]TidePrediction, error) {
	if !TideCode.MatchString(code) {
		return nil, fmt.Errorf("invalid tide station code %q", code)
	}
	body, err := fetch(ctx, h.client, http.MethodGet, hiiTideBase+code+".txt", "text/plain")
	if err != nil {
		return nil, err
	}
	return parseTidePredictions(bytes.NewReader(body), code)
}

// readCSV reads a CSV with a header row, indexing columns by name so that a
// column added or moved by the publisher does not shift the rest.
func readCSV(r io.Reader, want ...string) ([][]string, map[string]int, error) {
	rd := csv.NewReader(r)
	rd.FieldsPerRecord = -1
	rows, err := rd.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return nil, nil, fmt.Errorf("empty file")
	}
	cols := make(map[string]int)
	for i, name := range rows[0] {
		cols[strings.TrimSpace(name)] = i
	}
	for _, name := range want {
		if _, ok := cols[name]; !ok {
			return nil, nil, fmt.Errorf("no %q column in %v", name, rows[0])
		}
	}
	return rows[1:], cols, nil
}

func field(row []string, cols map[string]int, name string) string {
	if i := cols[name]; i < len(row) {
		return strings.TrimSpace(row[i])
	}
	return ""
}

func parseTideStations(r io.Reader) ([]TideStation, error) {
	rows, cols, err := readCSV(r, "code", "station.name.EN", "station.name.TH", "lat", "long")
	if err != nil {
		return nil, fmt.Errorf("tide stations: %w", err)
	}
	var out []TideStation
	for _, row := range rows {
		lat, lng := parseNum(field(row, cols, "lat")), parseNum(field(row, cols, "long"))
		st := TideStation{
			Code:   field(row, cols, "code"),
			Name:   firstNonEmpty(field(row, cols, "station.name.EN"), field(row, cols, "station.name.TH")),
			NameTH: field(row, cols, "station.name.TH"),
			Lat:    lat.v, Lng: lng.v,
		}
		if !TideCode.MatchString(st.Code) || st.Name == "" || !validCoords(lat, lng) {
			continue
		}
		out = append(out, st)
	}
	return out, nil
}

// parseTidePredictions reads a station's file. Its times are Thai local time,
// which is why the predicted highs line up with what Bangkok gauges record.
func parseTidePredictions(r io.Reader, code string) ([]TidePrediction, error) {
	rows, cols, err := readCSV(r, "station", "date", "time", "value")
	if err != nil {
		return nil, fmt.Errorf("tide predictions for %s: %w", code, err)
	}
	var out []TidePrediction
	for _, row := range rows {
		if field(row, cols, "station") != code {
			continue
		}
		at, err := time.ParseInLocation("2006-01-02 15:04:05", field(row, cols, "date")+" "+field(row, cols, "time"), ict)
		if err != nil {
			continue
		}
		level := parseNum(field(row, cols, "value"))
		if !level.ok {
			continue
		}
		out = append(out, TidePrediction{At: at, LevelM: level.v})
	}
	return out, nil
}
