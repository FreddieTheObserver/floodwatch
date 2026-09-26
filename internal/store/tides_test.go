package store

import (
	"testing"
	"time"

	"github.com/FreddieTheObserver/floodwatch/internal/source"
)

func TestSaveTides(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	harbour := source.TideStation{Code: "N02", Name: "Bangkok Harbour", NameTH: "ท่าเรือกรุงเทพ", Lat: 13.73247, Lng: 100.512017}
	six := time.Date(2026, 9, 26, 18, 0, 0, 0, ict)

	first := []source.TidePrediction{{At: six.Add(-time.Hour), LevelM: 0.62}, {At: six, LevelM: 0.73}}
	if err := s.SaveTides(ctx, harbour, first); err != nil {
		t.Fatal(err)
	}
	// A later run revises one hour and adds the next; the revision must win.
	second := []source.TidePrediction{{At: six, LevelM: 0.75}, {At: six.Add(time.Hour), LevelM: 0.72}}
	if err := s.SaveTides(ctx, harbour, second); err != nil {
		t.Fatal(err)
	}

	if n := countRows(t, s, "tide_predictions"); n != 3 {
		t.Errorf("tide_predictions rows = %d, want 3", n)
	}
	var level float64
	if err := s.pool.QueryRow(ctx, `SELECT level_m FROM tide_predictions WHERE station_code = 'N02' AND at = $1`, six).Scan(&level); err != nil {
		t.Fatal(err)
	}
	if level != 0.75 {
		t.Errorf("18:00 = %v, want the revised 0.75", level)
	}
	var nameTH string
	s.pool.QueryRow(ctx, `SELECT name_th FROM tide_stations WHERE code = 'N02'`).Scan(&nameTH)
	if nameTH != "ท่าเรือกรุงเทพ" {
		t.Errorf("name_th = %q", nameTH)
	}
}

func TestSaveTidesRejectsAMalformedCode(t *testing.T) {
	s := newTestStore(t)
	err := s.SaveTides(t.Context(), source.TideStation{Code: "bad", Name: "x", Lat: 13, Lng: 100}, nil)
	if err == nil {
		t.Error("a malformed station code was stored")
	}
}
