package source

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestParseGaugeHistory(t *testing.T) {
	var resp thaiWaterGraphResponse
	body, err := io.ReadAll(openFixture(t, "thaiwater_waterlevel_graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	got := resp.readings("4", time.Date(2026, 9, 27, 0, 40, 0, 0, ict))
	if len(got) != 8 {
		t.Fatalf("got %d readings, want the 8 with values", len(got))
	}
	first, last := got[0], got[len(got)-1]
	if first.ExternalID != "4" || !first.ObservedAt.Equal(time.Date(2026, 9, 26, 21, 10, 0, 0, ict)) || first.LevelMSL != 1.32 {
		t.Errorf("first = %+v", first)
	}
	if !last.ObservedAt.Equal(time.Date(2026, 9, 26, 23, 10, 0, 0, ict)) || last.LevelMSL != 0.84 {
		t.Errorf("last = %+v", last)
	}
}

func TestGaugeHistoryDropsReadingsFromTheFuture(t *testing.T) {
	var resp thaiWaterGraphResponse
	body := `{"result":"OK","data":{"graph_data":[
		{"datetime":"2026-09-26 21:10","value":1.32},
		{"datetime":"2026-09-27 03:00","value":9.9},
		{"datetime":"26/09/2026 21:20","value":1.28}]}}`
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	got := resp.readings("4", time.Date(2026, 9, 27, 0, 40, 0, 0, ict))
	if len(got) != 1 || got[0].LevelMSL != 1.32 {
		t.Errorf("readings = %+v, want only the one with a sane time", got)
	}
}

func TestGaugeHistoryRequest(t *testing.T) {
	var query url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/public/waterlevel_graph" {
			http.NotFound(w, r)
			return
		}
		query = r.URL.Query()
		body, _ := io.ReadAll(openFixture(t, "thaiwater_waterlevel_graph.json"))
		w.Write(body)
	}))
	defer srv.Close()

	h := History{client: srv.Client(), base: srv.URL}
	// Times are sent as Thai dates and clock times, whatever zone they arrive in.
	to := time.Date(2026, 9, 26, 17, 40, 0, 0, time.UTC)
	got, err := h.Water(context.Background(), "4", to.Add(-72*time.Hour), to)
	if err != nil {
		t.Fatal(err)
	}
	want := url.Values{"station_type": {"tele_waterlevel"}, "station_id": {"4"}, "start_date": {"2026-09-24"}, "end_date": {"2026-09-27 00:40"}}
	if query.Encode() != want.Encode() {
		t.Errorf("query = %s, want %s", query.Encode(), want.Encode())
	}
	if len(got) != 8 {
		t.Errorf("got %d readings", len(got))
	}
}

func TestGaugeHistoryReportsAFailedResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"result":"FAIL","data":{}}`))
	}))
	defer srv.Close()

	h := History{client: srv.Client(), base: srv.URL}
	if _, err := h.Water(context.Background(), "4", time.Now().Add(-time.Hour), time.Now()); err == nil {
		t.Error("a failed result was taken as no readings")
	}
}
