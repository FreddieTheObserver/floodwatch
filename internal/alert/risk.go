package alert

import (
	"cmp"
	"slices"
)

// RiskUnknown is a level beyond the severity scale: nothing near the place has
// fresh data, which must never read as low risk.
const RiskUnknown = 4

type Trend int

const (
	TrendUnknown Trend = iota
	TrendBetter
	TrendStable
	TrendWorse
)

// Risk is one place's overall situation, judged from all its findings.
type Risk struct {
	Level int // SeverityNone to SeveritySevere, or RiskUnknown
	Trend Trend
	// Drivers are the findings that set the level, most telling first. Empty
	// when the level is low or unknown.
	Drivers []Finding
}

const (
	// A water level counts as moving once it changes this fast; slower is noise.
	trendCmPerHour = 2.0
	// The last hour counts as heavier or lighter than the recent average when
	// it differs by this factor, and has at least trendMinRainMM to judge by.
	rainTrendFactor = 1.25
	trendMinRainMM  = 2.0
	maxDrivers      = 2
)

// Overall combines a place's findings into one risk level.
//
// The level is the worst of its water gauges and its rain, where a gauge
// beyond the place's radius counts one level lower: a canal overflowing 7 km
// away is a warning for the place, not a certainty. Water rising fast near its
// bank counts one level higher, since it is about to get worse.
func Overall(findings []Finding, radiusM int) Risk {
	type station struct{ level, rising Finding }
	stations := map[int64]*station{}
	var order []int64
	var rain Finding
	for _, f := range findings {
		switch f.Rule {
		case RuleRain:
			rain = f
		case RuleWaterLevel, RuleWaterRising:
			s, ok := stations[f.StationID]
			if !ok {
				s = &station{}
				stations[f.StationID] = s
				order = append(order, f.StationID)
			}
			if f.Rule == RuleWaterLevel {
				s.level = f
			} else {
				s.rising = f
			}
		}
	}

	type candidate struct {
		level  int
		driver Finding
	}
	var cands []candidate
	// Held severities count toward the level, so data going quiet never lowers
	// it; only fresh data counts as knowing anything.
	fresh := false
	for _, id := range order {
		s := stations[id]
		if !s.level.Known && !s.level.Held {
			continue
		}
		fresh = fresh || s.level.Known
		c := candidate{discount(s.level.Severity, s.level, radiusM), s.level}
		if (s.rising.Known || s.rising.Held) && s.rising.Severity > SeverityNone {
			c = candidate{min(c.level+1, SeveritySevere), s.rising}
		}
		cands = append(cands, c)
	}
	if rain.Known || rain.Held {
		fresh = fresh || rain.Known
		cands = append(cands, candidate{discount(rain.Severity, rain, radiusM), rain})
	}

	risk := Risk{Level: SeverityNone}
	for _, c := range cands {
		risk.Level = max(risk.Level, c.level)
	}
	if !fresh && risk.Level == SeverityNone {
		return Risk{Level: RiskUnknown, Trend: TrendUnknown}
	}
	if risk.Level > SeverityNone {
		for _, c := range cands {
			if c.level == risk.Level {
				risk.Drivers = append(risk.Drivers, c.driver)
			}
		}
		// Water before rain, as the more direct sign, then nearest first.
		slices.SortStableFunc(risk.Drivers, func(a, b Finding) int {
			return cmp.Or(cmp.Compare(b2i(a.Rule == RuleRain), b2i(b.Rule == RuleRain)), cmp.Compare(a.DistanceM, b.DistanceM))
		})
		risk.Drivers = risk.Drivers[:min(len(risk.Drivers), maxDrivers)]
	}
	risk.Trend = overallTrend(risk.Drivers, rain)
	return risk
}

func discount(severity int, f Finding, radiusM int) int {
	if f.DistanceM > float64(radiusM) {
		return max(severity-1, SeverityNone)
	}
	return severity
}

// overallTrend is worse if anything behind the level is getting worse, better
// if everything is easing, and stable otherwise. With nothing raised, only
// rain that is actually falling says which way things are heading.
func overallTrend(drivers []Finding, rain Finding) Trend {
	if len(drivers) == 0 {
		if rain.Known && rain.Rain1h != nil && *rain.Rain1h >= trendMinRainMM {
			return findingTrend(rain)
		}
		return TrendStable
	}
	better, judged := 0, 0
	for _, d := range drivers {
		if d.Held {
			continue // a held severity has no current readings to show a direction
		}
		judged++
		switch findingTrend(d) {
		case TrendWorse:
			return TrendWorse
		case TrendBetter:
			better++
		}
	}
	switch {
	case judged == 0:
		return TrendUnknown
	case better == judged:
		return TrendBetter
	}
	return TrendStable
}

func findingTrend(f Finding) Trend {
	if f.Rule == RuleRain {
		return rainTrend(f)
	}
	switch rate := f.RiseCmPerHour; {
	case rate == nil:
		return TrendStable
	case *rate >= trendCmPerHour:
		return TrendWorse
	case *rate <= -trendCmPerHour:
		return TrendBetter
	}
	return TrendStable
}

// rainTrend compares the last hour with the hourly average over the longest
// window the gauge reports.
func rainTrend(f Finding) Trend {
	if f.Rain1h == nil {
		return TrendStable
	}
	var average float64
	switch {
	case f.Rain3h != nil:
		average = *f.Rain3h / 3
	case f.Rain24h != nil:
		average = *f.Rain24h / 24
	default:
		return TrendStable
	}
	lastHour := *f.Rain1h
	switch {
	case lastHour >= trendMinRainMM && lastHour > average*rainTrendFactor:
		return TrendWorse
	case lastHour < average/rainTrendFactor:
		return TrendBetter
	}
	return TrendStable
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
