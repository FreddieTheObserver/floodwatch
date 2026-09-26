package alert

import (
	"testing"
	"time"

	"github.com/FreddieTheObserver/floodwatch/internal/source"
)

func risk(f *fixture, current map[Key]int) Risk {
	return Overall(Assess(f.snap, home, current), home.RadiusM)
}

func TestRiskIsLowWhenNothingIsRaised(t *testing.T) {
	f := newFixture().
		water(north(1, source.KindWater, 1, mm(2)), at(time.Hour, 0.9), at(0, 0.9)).
		rain(north(11, source.KindRain, 1, nil), RainPoint{At: now, Rain1h: mm(0), Rain24h: mm(10)})
	got := risk(f, nil)
	if got.Level != SeverityNone || len(got.Drivers) != 0 || got.Trend != TrendStable {
		t.Errorf("risk = %+v, want low, no drivers, stable", got)
	}
}

func TestRiskTakesTheWorstFactor(t *testing.T) {
	f := newFixture().
		water(north(1, source.KindWater, 1, mm(2)), at(0, 1.85)).                    // warning
		rain(north(11, source.KindRain, 1, nil), RainPoint{At: now, Rain1h: mm(25)}) // watch
	got := risk(f, nil)
	if got.Level != SeverityWarning || len(got.Drivers) != 1 || got.Drivers[0].Rule != RuleWaterLevel {
		t.Errorf("risk = %+v, want warning driven by the water gauge", got)
	}
}

// The distance rule agreed on 26 September 2026: a gauge beyond the place's
// radius counts one level lower.
func TestGaugesBeyondTheRadiusCountOneLevelLower(t *testing.T) {
	inside := newFixture().water(north(1, source.KindWater, 4, mm(2)), at(0, 2.1))
	beyond := newFixture().water(north(1, source.KindWater, 7, mm(2)), at(0, 2.1))
	if got := risk(inside, nil).Level; got != SeveritySevere {
		t.Errorf("overflow 4 km away = %d, want high", got)
	}
	if got := risk(beyond, nil).Level; got != SeverityWarning {
		t.Errorf("overflow 7 km away = %d, want warning", got)
	}
}

func TestRisingFastNearTheBankRaisesTheRisk(t *testing.T) {
	f := newFixture().water(north(1, source.KindWater, 1, mm(2)), at(time.Hour, 1.4), at(0, 1.6)) // watch, 20 cm/h
	got := risk(f, nil)
	if got.Level != SeverityWarning || got.Drivers[0].Rule != RuleWaterRising || got.Trend != TrendWorse {
		t.Errorf("risk = %+v, want warning from the rise, getting worse", got)
	}
}

// Seen on 26 September 2026 at 17:40: Khlong Lat Bang Yo 1 Gate, 6.6 km from
// Home, was 0.56 m over its bank on the evening tide and rising 26 cm/h. The
// rise must not win back the level its distance costs it; applying the bump
// after the discount made this HIGH instead of WARNING.
func TestARiseBeyondTheRadiusStillCountsALevelLower(t *testing.T) {
	gate := north(1, source.KindWater, 6.6, mm(1.51))
	f := newFixture().water(gate, at(time.Hour, 1.81), at(0, 2.07))
	got := risk(f, nil)
	if got.Level != SeverityWarning || got.Drivers[0].Rule != RuleWaterRising {
		t.Errorf("risk = %+v, want warning from the rising gate", got)
	}
}

func TestRiskIsUnknownWithNoFreshData(t *testing.T) {
	f := newFixture().
		water(north(1, source.KindWater, 1, mm(2)), at(5*time.Hour, 0.5)).
		rain(north(11, source.KindRain, 1, nil), RainPoint{At: now.Add(-5 * time.Hour), Rain1h: mm(0)})
	if got := risk(f, nil); got.Level != RiskUnknown || got.Trend != TrendUnknown {
		t.Errorf("risk = %+v, want unknown", got)
	}
}

func TestQuietGaugesHoldTheirLastLevel(t *testing.T) {
	f := newFixture().
		water(north(1, source.KindWater, 1, mm(2)), at(5*time.Hour, 1.9)).
		rain(north(11, source.KindRain, 1, nil), RainPoint{At: now, Rain1h: mm(0)})
	current := map[Key]int{{SubscriptionID: 1, StationID: 1, Rule: RuleWaterLevel}: SeverityWarning}
	got := risk(f, current)
	if got.Level != SeverityWarning || !got.Drivers[0].Held || got.Trend != TrendUnknown {
		t.Errorf("risk = %+v, want warning held from the quiet gauge, trend unknown", got)
	}
}

func TestTrend(t *testing.T) {
	bank := mm(2)
	cases := []struct {
		name string
		f    *fixture
		want Trend
	}{
		{"water falling", newFixture().water(north(1, source.KindWater, 1, bank), at(time.Hour, 1.9), at(0, 1.8)), TrendBetter},
		{"water steady", newFixture().water(north(1, source.KindWater, 1, bank), at(time.Hour, 1.81), at(0, 1.8)), TrendStable},
		{"rain intensifying", newFixture().rain(north(11, source.KindRain, 1, nil), RainPoint{At: now, Rain1h: mm(30), Rain3h: mm(45)}), TrendWorse},
		// 130 mm in a day averages 5.4 mm/h, so 5 in the last hour is steady
		// and 2 is easing.
		{"rain steady", newFixture().rain(north(11, source.KindRain, 1, nil), RainPoint{At: now, Rain1h: mm(5), Rain24h: mm(130)}), TrendStable},
		{"rain easing", newFixture().rain(north(11, source.KindRain, 1, nil), RainPoint{At: now, Rain1h: mm(2), Rain24h: mm(130)}), TrendBetter},
		{"rain stopped after a wet day", newFixture().rain(north(11, source.KindRain, 1, nil), RainPoint{At: now, Rain1h: mm(0), Rain24h: mm(130)}), TrendBetter},
	}
	for _, c := range cases {
		if got := risk(c.f, nil).Trend; got != c.want {
			t.Errorf("%s: trend = %d, want %d", c.name, got, c.want)
		}
	}
}

// With two gauges in the same rain class, the one inside the radius must be
// the one reported, or the distance rule would understate the risk.
func TestRainPrefersTheGaugeInsideTheRadius(t *testing.T) {
	f := newFixture().
		rain(north(11, source.KindRain, 8, nil), RainPoint{At: now, Rain1h: mm(55)}).
		rain(north(12, source.KindRain, 2, nil), RainPoint{At: now, Rain1h: mm(45)})
	got := risk(f, nil)
	if got.Level != SeverityWarning || got.Drivers[0].Station.ID != 12 {
		t.Errorf("risk = %+v, want warning from the nearer gauge 12", got)
	}
}
