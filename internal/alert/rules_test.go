package alert

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/FreddieTheObserver/floodwatch/internal/source"
)

var (
	ict = time.FixedZone("ICT", 7*60*60)
	now = time.Date(2026, 9, 26, 14, 0, 0, 0, ict)

	home = Subscription{ID: 1, Channel: "telegram", Recipient: "42", Label: "home", Lat: 13.6515, Lng: 100.4945, RadiusM: 5000}
)

func mm(v float64) *float64 { return &v }

// north places a station km kilometres due north of home.
func north(id int64, kind source.Kind, km float64, bank *float64) Station {
	return Station{ID: id, Kind: kind, Name: "st", Lat: home.Lat + km/111.195, Lng: home.Lng, BankMSL: bank}
}

type fixture struct{ snap Snapshot }

func newFixture() *fixture {
	return &fixture{snap: Snapshot{
		Now:         now,
		LatestWater: map[int64]WaterPoint{},
		LatestRain:  map[int64]RainPoint{},
		RecentWater: map[int64][]WaterPoint{},
	}}
}

func (f *fixture) water(st Station, readings ...WaterPoint) *fixture {
	f.snap.Stations = append(f.snap.Stations, st)
	if len(readings) > 0 {
		f.snap.LatestWater[st.ID] = readings[len(readings)-1]
		f.snap.RecentWater[st.ID] = readings
	}
	return f
}

func (f *fixture) rain(st Station, p RainPoint) *fixture {
	f.snap.Stations = append(f.snap.Stations, st)
	f.snap.LatestRain[st.ID] = p
	return f
}

func find(t *testing.T, fs []Finding, stationID int64, r Rule) Finding {
	t.Helper()
	for _, f := range fs {
		if f.StationID == stationID && f.Rule == r {
			return f
		}
	}
	t.Fatalf("no %s finding for station %d in %d findings", r, stationID, len(fs))
	return Finding{}
}

func at(ago time.Duration, level float64) WaterPoint {
	return WaterPoint{At: now.Add(-ago), LevelMSL: level}
}

func TestDistanceM(t *testing.T) {
	if d := DistanceM(0, 0, 1, 0); math.Abs(d-111_195) > 1 {
		t.Errorf("one degree of latitude = %.0f m", d)
	}
	a, b := DistanceM(13.6515, 100.4945, 13.70, 100.49), DistanceM(13.70, 100.49, 13.6515, 100.4945)
	if a != b || a < 5_000 || a > 6_000 {
		t.Errorf("Bang Mot to Thon Buri = %.0f / %.0f m", a, b)
	}
}

func TestWatchedStations(t *testing.T) {
	bank := mm(2)
	f := newFixture().
		water(north(1, source.KindWater, 2, bank), at(0, 1)).
		water(north(2, source.KindWater, 6.5, bank), at(0, 1)).            // outside the radius, but among the 2 nearest
		water(north(3, source.KindWater, 8.5, bank), at(0, 1)).            // third nearest, outside the radius
		water(north(4, source.KindWater, 1, nil), at(0, 1)).               // no bank level to judge against
		water(north(5, source.KindWater, 1.5, bank), at(72*time.Hour, 1)). // retired
		water(north(6, source.KindWater, 30, bank), at(0, 1)).             // out of reach
		rain(north(11, source.KindRain, 1, nil), RainPoint{At: now, Rain1h: mm(0)}).
		rain(north(12, source.KindRain, 3, nil), RainPoint{At: now, Rain1h: mm(0)}).
		rain(north(13, source.KindRain, 7, nil), RainPoint{At: now, Rain1h: mm(0)}).
		rain(north(14, source.KindRain, 9, nil), RainPoint{At: now, Rain1h: mm(0)})
	// Retired stations are absent from the snapshot's latest maps, as the store
	// only loads readings within AliveWithin.
	delete(f.snap.LatestWater, 5)

	water, rain := watched(f.snap, home)
	ids := func(ns []nearby) (out []int64) {
		for _, n := range ns {
			out = append(out, n.ID)
		}
		return out
	}
	if got := ids(water); !slices.Equal(got, []int64{1, 2}) {
		t.Errorf("water = %v, want [1 2]", got)
	}
	if got := ids(rain); !slices.Equal(got, []int64{11, 12, 13}) {
		t.Errorf("rain = %v, want [11 12 13]", got)
	}
}

// The case found on real data: the nearest gauges belong to a source that was
// switched off, so they are alive but stale, and a working gauge sits just
// outside the radius.
func TestFallbackSkipsStaleNeighbours(t *testing.T) {
	stale := RainPoint{At: now.Add(-4 * time.Hour), Rain1h: mm(0)}
	f := newFixture().
		rain(north(11, source.KindRain, 2, nil), stale).
		rain(north(12, source.KindRain, 3, nil), stale).
		rain(north(13, source.KindRain, 4, nil), stale).
		rain(north(14, source.KindRain, 5.4, nil), RainPoint{At: now, Rain1h: mm(25)})

	fs := Assess(f.snap, home, nil)
	if rain := find(t, fs, 0, RuleRain); !rain.Known || rain.Station.ID != 14 || rain.Severity != SeverityWatch {
		t.Errorf("rain = %+v, want watch from the working gauge 14", rain)
	}
	if stale := find(t, fs, 0, RuleRainStale); stale.Severity != SeverityNone {
		t.Errorf("rain_stale = %d while a gauge is working", stale.Severity)
	}
}

func TestWaterLevelSeverity(t *testing.T) {
	cases := []struct {
		level float64
		want  int
	}{
		{1.40, SeverityNone},
		{1.50, SeverityWatch},
		{1.85, SeverityWarning},
		{2.00, SeveritySevere},
		{2.62, SeveritySevere},
	}
	for _, c := range cases {
		f := newFixture().water(north(1, source.KindWater, 1, mm(2)), at(0, c.level))
		got := find(t, Assess(f.snap, home, nil), 1, RuleWaterLevel)
		if !got.Known || got.Severity != c.want {
			t.Errorf("level %.2f (bank 2.00) = severity %d known %v, want %d", c.level, got.Severity, got.Known, c.want)
		}
	}
}

func TestWaterLevelHysteresis(t *testing.T) {
	key := Key{SubscriptionID: 1, StationID: 1, Rule: RuleWaterLevel}
	current := map[Key]int{key: SeverityWarning}

	for _, c := range []struct {
		level float64
		want  int
	}{
		{1.78, SeverityWarning}, // 22 cm below the bank: inside the 5 cm band, stays
		{1.74, SeverityWatch},   // 26 cm below: clearly past it, drops
	} {
		f := newFixture().water(north(1, source.KindWater, 1, mm(2)), at(0, c.level))
		if got := find(t, Assess(f.snap, home, current), 1, RuleWaterLevel); got.Severity != c.want {
			t.Errorf("falling to %.2f from warning = %d, want %d", c.level, got.Severity, c.want)
		}
	}
}

func TestWaterRising(t *testing.T) {
	st := north(1, source.KindWater, 1, mm(2))

	fast := newFixture().water(st, at(70*time.Minute, 1.5), at(60*time.Minute, 1.5), at(0, 1.7))
	got := find(t, Assess(fast.snap, home, nil), 1, RuleWaterRising)
	if !got.Known || got.Severity != SeverityWatch || got.RiseCmPerHour == nil || math.Abs(*got.RiseCmPerHour-20) > 1e-9 {
		t.Errorf("20 cm/h with 30 cm to go = %+v", got)
	}

	slow := newFixture().water(st, at(time.Hour, 1.67), at(0, 1.7))
	if got := find(t, Assess(slow.snap, home, nil), 1, RuleWaterRising); got.Severity != SeverityNone {
		t.Errorf("3 cm/h = severity %d, want none", got.Severity)
	}

	// Once warned, a rise slowing to 3 cm/h with 10 cm to go still reaches the
	// bank within the clear horizon, so the warning stands.
	current := map[Key]int{{SubscriptionID: 1, StationID: 1, Rule: RuleWaterRising}: SeverityWatch}
	easing := newFixture().water(st, at(time.Hour, 1.87), at(0, 1.9))
	if got := find(t, Assess(easing.snap, home, current), 1, RuleWaterRising); got.Severity != SeverityWatch {
		t.Errorf("easing rise = severity %d, want it held", got.Severity)
	}

	noHistory := newFixture().water(st, at(0, 1.7))
	if got := find(t, Assess(noHistory.snap, home, nil), 1, RuleWaterRising); got.Known {
		t.Errorf("rise judged without history: %+v", got)
	}
}

func TestStaleWaterNeverReadsAsAllClear(t *testing.T) {
	f := newFixture().water(north(1, source.KindWater, 1, mm(2)), at(4*time.Hour, 1.0))
	current := map[Key]int{{SubscriptionID: 1, StationID: 1, Rule: RuleWaterLevel}: SeveritySevere}

	changes := Changes(Assess(f.snap, home, current), current)
	if len(changes) != 1 || changes[0].Rule != RuleWaterStale || changes[0].Severity != SeverityWatch {
		t.Fatalf("changes = %+v, want only the gauge going quiet", changes)
	}
}

func TestRainTakesWorstFreshGauge(t *testing.T) {
	f := newFixture().
		rain(north(11, source.KindRain, 1, nil), RainPoint{At: now, Rain1h: mm(5), Rain24h: mm(60)}).
		rain(north(12, source.KindRain, 2, nil), RainPoint{At: now.Add(-30 * time.Minute), Rain1h: mm(45), Rain24h: mm(80)}).
		rain(north(13, source.KindRain, 3, nil), RainPoint{At: now, Rain24h: mm(100)}).
		rain(north(14, source.KindRain, 4, nil), RainPoint{At: now.Add(-4 * time.Hour), Rain1h: mm(90)}) // stale

	got := find(t, Assess(f.snap, home, nil), 0, RuleRain)
	if !got.Known || got.Severity != SeverityWarning || got.Station.ID != 12 || got.FreshGauges != 3 {
		t.Errorf("rain = severity %d station %d fresh %d known %v, want warning from 12 of 3",
			got.Severity, got.Station.ID, got.FreshGauges, got.Known)
	}
}

func TestRainHysteresis(t *testing.T) {
	current := map[Key]int{{SubscriptionID: 1, Rule: RuleRain}: SeverityWarning}
	for _, c := range []struct {
		rain1h float64
		want   int
	}{
		{35, SeverityWarning}, // within 20% of the 40 mm line, stays
		{30, SeverityWatch},
	} {
		f := newFixture().rain(north(11, source.KindRain, 1, nil), RainPoint{At: now, Rain1h: mm(c.rain1h)})
		if got := find(t, Assess(f.snap, home, current), 0, RuleRain); got.Severity != c.want {
			t.Errorf("easing to %.0f mm/h from warning = %d, want %d", c.rain1h, got.Severity, c.want)
		}
	}
}

func TestRainGaugesAllQuiet(t *testing.T) {
	f := newFixture().rain(north(11, source.KindRain, 1, nil), RainPoint{At: now.Add(-4 * time.Hour), Rain1h: mm(0)})
	fs := Assess(f.snap, home, nil)
	if rain := find(t, fs, 0, RuleRain); rain.Known {
		t.Errorf("rain judged from a stale gauge: %+v", rain)
	}
	if stale := find(t, fs, 0, RuleRainStale); !stale.Known || stale.Severity != SeverityWatch {
		t.Errorf("rain_stale = %+v", stale)
	}
}

func TestNoCoverageIsSilent(t *testing.T) {
	f := newFixture().
		water(north(1, source.KindWater, 40, mm(2)), at(0, 3)).
		rain(north(11, source.KindRain, 40, nil), RainPoint{At: now, Rain1h: mm(99)})
	if changes := Changes(Assess(f.snap, home, nil), nil); len(changes) != 0 {
		t.Errorf("a place with no stations in reach produced %+v", changes)
	}
}
