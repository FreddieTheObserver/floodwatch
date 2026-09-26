package store

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/FreddieTheObserver/floodwatch/internal/alert"
	"github.com/FreddieTheObserver/floodwatch/internal/source"
)

func place(recipient, label string) alert.Subscription {
	return alert.Subscription{Channel: "telegram", Recipient: recipient, Label: label, Lat: 13.6515, Lng: 100.4945, RadiusM: 5000}
}

func countRows(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestReadingsForAlerts(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	t0 := time.Date(2026, 9, 26, 12, 0, 0, 0, ict)

	for i, level := range []float64{1.5, 1.6, 1.8} {
		if _, err := s.SaveBatch(ctx, waterBatch(ptr(2.2), level, t0.Add(time.Duration(i)*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	rain := source.Batch{
		Source: "thaiwater", Kind: source.KindRain,
		Stations: []source.Station{{ExternalID: "2", Name: "Krung Thep 8", Lat: 13.76, Lng: 100.64}},
		Rain: []source.RainReading{
			{ExternalID: "2", ObservedAt: t0, Rain24h: ptr(150)},
			{ExternalID: "2", ObservedAt: t0.Add(time.Hour), Rain1h: ptr(3), Rain24h: ptr(198.2)},
		},
	}
	if _, err := s.SaveBatch(ctx, rain); err != nil {
		t.Fatal(err)
	}

	stations, err := s.ListStations(ctx)
	if err != nil || len(stations) != 2 {
		t.Fatalf("stations = %+v, %v", stations, err)
	}
	var waterID, rainID int64
	for _, st := range stations {
		switch st.Kind {
		case source.KindWater:
			waterID = st.ID
			if st.BankMSL == nil || *st.BankMSL != 2.2 || st.District != "Bang Khen District" {
				t.Errorf("water station = %+v", st)
			}
		case source.KindRain:
			rainID = st.ID
		}
	}

	latest, err := s.LatestWater(ctx, t0.Add(-time.Hour))
	if err != nil || latest[waterID].LevelMSL != 1.8 || !latest[waterID].At.Equal(t0.Add(2*time.Hour)) {
		t.Errorf("latest water = %+v, %v", latest, err)
	}
	recent, err := s.RecentWater(ctx, t0.Add(30*time.Minute))
	if err != nil || len(recent[waterID]) != 2 || recent[waterID][0].LevelMSL != 1.6 {
		t.Errorf("recent water = %+v, %v; want the last two, oldest first", recent, err)
	}
	if none, _ := s.LatestWater(ctx, t0.Add(3*time.Hour)); len(none) != 0 {
		t.Errorf("readings older than since were returned: %+v", none)
	}

	latestRain, err := s.LatestRain(ctx, t0.Add(-time.Hour))
	r := latestRain[rainID]
	if err != nil || r.Rain1h == nil || *r.Rain1h != 3 || r.Rain3h != nil || *r.Rain24h != 198.2 {
		t.Errorf("latest rain = %+v, %v", r, err)
	}
}

func TestAlertStatesRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	if _, err := s.SaveBatch(ctx, waterBatch(ptr(2.2), 2.8, time.Now())); err != nil {
		t.Fatal(err)
	}
	stations, _ := s.ListStations(ctx)
	sub, err := s.SaveSubscription(ctx, place("42", "home"))
	if err != nil {
		t.Fatal(err)
	}

	water := alert.Key{SubscriptionID: sub.ID, StationID: stations[0].ID, Rule: alert.RuleWaterLevel}
	area := alert.Key{SubscriptionID: sub.ID, Rule: alert.RuleRain}
	if err := s.RecordAlertStates(ctx, map[alert.Key]int{water: 3, area: 1}, true); err != nil {
		t.Fatal(err)
	}
	// Recording the area rule again must update its row, not add a second one:
	// its station is NULL, which only NULLS NOT DISTINCT makes a conflict.
	if err := s.RecordAlertStates(ctx, map[alert.Key]int{area: 2}, true); err != nil {
		t.Fatal(err)
	}

	got, err := s.AlertStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[water] != 3 || got[area] != 2 {
		t.Errorf("states = %v", got)
	}
	if n := countRows(t, s, "alert_states"); n != 2 {
		t.Errorf("alert_states rows = %d, want 2", n)
	}
}

func TestRecordingForADeletedSubscriptionIsANoOp(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	sub, _ := s.SaveSubscription(ctx, place("42", "home"))
	if _, err := s.ForgetRecipient(ctx, "telegram", "42"); err != nil {
		t.Fatal(err)
	}
	err := s.RecordAlertStates(ctx, map[alert.Key]int{{SubscriptionID: sub.ID, Rule: alert.RuleRain}: 1}, true)
	if err != nil || countRows(t, s, "alert_states") != 0 {
		t.Errorf("recording for a deleted subscription: %v", err)
	}
}

func TestSaveSubscription(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()

	for i := range MaxPlaces {
		if _, err := s.SaveSubscription(ctx, place("42", fmt.Sprintf("place %d", i))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.SaveSubscription(ctx, place("42", "one too many")); !errors.Is(err, ErrTooManyPlaces) {
		t.Errorf("sixth place: %v, want ErrTooManyPlaces", err)
	}
	// Updating an existing place is not adding one, and another person's limit is their own.
	if _, err := s.SaveSubscription(ctx, place("42", "place 0")); err != nil {
		t.Errorf("updating at the limit: %v", err)
	}
	if _, err := s.SaveSubscription(ctx, place("43", "home")); err != nil {
		t.Errorf("another recipient: %v", err)
	}

	mine, err := s.RecipientSubscriptions(ctx, "telegram", "42")
	if err != nil || len(mine) != MaxPlaces {
		t.Errorf("recipient 42 has %d places, %v", len(mine), err)
	}
}

func TestMovingAPlaceForgetsItsAlerts(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	sub, _ := s.SaveSubscription(ctx, place("42", "home"))
	area := alert.Key{SubscriptionID: sub.ID, Rule: alert.RuleRain}
	if err := s.RecordAlertStates(ctx, map[alert.Key]int{area: 2}, true); err != nil {
		t.Fatal(err)
	}

	if _, err := s.SaveSubscription(ctx, place("42", "home")); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, "alert_states"); n != 1 {
		t.Errorf("re-saving the same spot dropped its alerts (%d rows)", n)
	}

	moved := place("42", "home")
	moved.Lat += 0.1
	saved, err := s.SaveSubscription(ctx, moved)
	if err != nil || saved.ID != sub.ID || saved.Lat != moved.Lat {
		t.Fatalf("move = %+v, %v", saved, err)
	}
	if n := countRows(t, s, "alert_states"); n != 0 {
		t.Errorf("moved place kept %d alert rows", n)
	}
}

func TestForgetRecipient(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	a, _ := s.SaveSubscription(ctx, place("42", "home"))
	s.SaveSubscription(ctx, place("42", "office"))
	s.SaveSubscription(ctx, place("43", "home"))
	s.RecordAlertStates(ctx, map[alert.Key]int{{SubscriptionID: a.ID, Rule: alert.RuleRain}: 1}, true)

	n, err := s.ForgetRecipient(ctx, "telegram", "42")
	if err != nil || n != 2 {
		t.Errorf("forgot %d places, %v", n, err)
	}
	if countRows(t, s, "subscriptions") != 1 || countRows(t, s, "alert_states") != 0 {
		t.Error("forgetting a recipient left their data behind")
	}

	removed, err := s.RemoveSubscription(ctx, "telegram", "43", "home")
	if err != nil || !removed {
		t.Errorf("remove = %v, %v", removed, err)
	}
	if removed, _ := s.RemoveSubscription(ctx, "telegram", "43", "home"); removed {
		t.Error("removing a missing place reported success")
	}
}
