package source

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

var fixtureNow = time.Date(2026, 9, 26, 14, 0, 0, 0, ict)

func loadFixture(t *testing.T, name string, v any) {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
}

func serveFixture(t *testing.T, name string) *httptest.Server {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != userAgent {
			t.Errorf("User-Agent = %q, want %q", got, userAgent)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write(raw)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestThaiWaterLevels(t *testing.T) {
	var resp thaiWaterLevelResponse
	loadFixture(t, "thaiwater_waterlevel.json", &resp)
	b := resp.batch(fixtureNow)

	if b.Source != "thaiwater" || b.Kind != KindWater {
		t.Fatalf("source/kind = %s/%s", b.Source, b.Kind)
	}
	if len(b.Stations) != 4 || len(b.Water) != 3 || b.Skipped != 1 {
		t.Fatalf("stations=%d water=%d skipped=%d, want 4/3/1", len(b.Stations), len(b.Water), b.Skipped)
	}

	over := b.Stations[0]
	if over.ExternalID != "1" || over.Name != "Klong Ladprao Bang Bua Temple" ||
		over.NameTH != "คลองลาดพร้าว วัดบางบัว" || over.District != "Bang Khen District" {
		t.Errorf("station = %+v", over)
	}
	if deref(over.BankMSL) != 2.2 {
		t.Errorf("bank = %v, want 2.2", deref(over.BankMSL))
	}
	want := WaterReading{ExternalID: "1", ObservedAt: time.Date(2026, 9, 26, 13, 0, 0, 0, ict), LevelMSL: 2.82}
	if got := b.Water[0]; got.ExternalID != want.ExternalID || !got.ObservedAt.Equal(want.ObservedAt) || got.LevelMSL != want.LevelMSL {
		t.Errorf("reading = %+v, want %+v", got, want)
	}

	thaiOnly := b.Stations[1]
	if thaiOnly.Name != "กรมชลประทานสามเสน" || thaiOnly.NameTH != thaiOnly.Name {
		t.Errorf("thai-only station names = %q / %q", thaiOnly.Name, thaiOnly.NameTH)
	}

	// The station with a null level is still known; only its reading is dropped.
	if b.Stations[3].ExternalID != "999001" {
		t.Errorf("station with null level was dropped: %+v", b.Stations[3])
	}
}

func TestThaiWaterLevelsRejectsFutureReadings(t *testing.T) {
	var resp thaiWaterLevelResponse
	loadFixture(t, "thaiwater_waterlevel.json", &resp)
	b := resp.batch(time.Date(2026, 9, 26, 11, 0, 0, 0, ict))

	for _, r := range b.Water {
		if r.ObservedAt.After(time.Date(2026, 9, 26, 12, 0, 0, 0, ict)) {
			t.Errorf("kept reading from the future: %+v", r)
		}
	}
}

func TestThaiWaterRain(t *testing.T) {
	var resp thaiWaterRainResponse
	loadFixture(t, "thaiwater_rain24.json", &resp)
	b := resp.batch(fixtureNow)

	if len(b.Stations) != 2 || len(b.Rain) != 2 || b.Skipped != 1 {
		t.Fatalf("stations=%d rain=%d skipped=%d, want 2/2/1", len(b.Stations), len(b.Rain), b.Skipped)
	}
	if b.Stations[0].BankMSL != nil {
		t.Errorf("rain station has a bank level")
	}
	r := b.Rain[0]
	if r.ExternalID != "2" || deref(r.Rain1h) != 3.0 || deref(r.Rain24h) != 198.2 || r.Rain3h != nil {
		t.Errorf("reading = %+v 1h=%v 3h=%v 24h=%v", r, deref(r.Rain1h), deref(r.Rain3h), deref(r.Rain24h))
	}
	if b.Rain[1].Rain1h != nil || deref(b.Rain[1].Rain24h) != 188.7 {
		t.Errorf("reading without rain_1h = 1h=%v 24h=%v", deref(b.Rain[1].Rain1h), deref(b.Rain[1].Rain24h))
	}
}

func TestBMARain(t *testing.T) {
	srv := serveFixture(t, "bma_rain.json")

	var rows []bmaRainRow
	if err := fetchJSON(context.Background(), srv.Client(), http.MethodPost, srv.URL, &rows); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	b := bmaRainBatch(rows, fixtureNow)

	if b.Source != "bma" || b.Kind != KindRain {
		t.Fatalf("source/kind = %s/%s", b.Source, b.Kind)
	}
	if len(b.Stations) != 2 || len(b.Rain) != 2 || b.Skipped != 0 {
		t.Fatalf("stations=%d rain=%d skipped=%d, want 2/2/0", len(b.Stations), len(b.Rain), b.Skipped)
	}
	st := b.Stations[0]
	if st.ExternalID != "RF.TKU.01" || st.Name != "Thung Khru District Office" || st.District != "Thung Khru" ||
		st.Lat != 13.61135 || st.Lng != 100.50878 {
		t.Errorf("station = %+v", st)
	}
	r := b.Rain[0]
	if !r.ObservedAt.Equal(time.Date(2026, 9, 26, 13, 10, 0, 0, ict)) ||
		deref(r.Rain1h) != 1.0 || deref(r.Rain3h) != 4.0 || deref(r.Rain24h) != 92.5 {
		t.Errorf("reading = %v 1h=%v 3h=%v 24h=%v", r.ObservedAt, deref(r.Rain1h), deref(r.Rain3h), deref(r.Rain24h))
	}
	// A gauge that went quiet in February keeps its last reading and its old
	// timestamp, so freshness is judged from the data rather than a status flag.
	if !b.Rain[1].ObservedAt.Equal(time.Date(2026, 2, 21, 12, 5, 0, 0, ict)) {
		t.Errorf("stale reading time = %v", b.Rain[1].ObservedAt)
	}
}

func TestFetchJSONRejectsNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "busy", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	var v any
	if err := fetchJSON(context.Background(), srv.Client(), http.MethodGet, srv.URL, &v); err == nil {
		t.Fatal("want an error for a 503")
	}
}

func TestNum(t *testing.T) {
	cases := []struct {
		in   string
		want any
	}{
		{`1.5`, 1.5},
		{`"2.82"`, 2.82},
		{`" 3 "`, 3.0},
		{`-1.79`, -1.79},
		{`""`, nil},
		{`null`, nil},
		{`"abc"`, nil},
		{`"NaN"`, nil},
		{`1e999`, nil},
	}
	for _, c := range cases {
		var n num
		if err := json.Unmarshal([]byte(c.in), &n); err != nil {
			t.Errorf("%s: unexpected error %v", c.in, err)
			continue
		}
		if got := deref(n.ptr()); got != c.want {
			t.Errorf("%s = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseDotNetDate(t *testing.T) {
	want := time.Date(2026, 9, 26, 13, 10, 0, 0, ict)
	for _, in := range []string{"/Date(1790403000000)/", "/Date(1790403000000+0700)/"} {
		got, err := parseDotNetDate(in)
		if err != nil || !got.Equal(want) {
			t.Errorf("%s = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "/Date()/", "/Date(abc)/", "2026-09-26"} {
		if _, err := parseDotNetDate(in); err == nil {
			t.Errorf("%q: want an error", in)
		}
	}
}
