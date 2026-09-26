// Package alert decides, from the latest readings, what each subscriber should
// be told. Everything here is pure; loading and delivery live elsewhere.
package alert

import (
	"cmp"
	"slices"
	"time"

	"github.com/FreddieTheObserver/floodwatch/internal/source"
)

type Rule string

const (
	RuleWaterLevel  Rule = "water_level"
	RuleWaterRising Rule = "water_rising"
	RuleWaterStale  Rule = "water_stale"
	RuleRain        Rule = "rain"
	RuleRainStale   Rule = "rain_stale"
	// RuleRisk is the overall risk of a place, the one thing subscribers are
	// alerted about; the rules above are what it is judged from.
	RuleRisk Rule = "risk"
)

// IsArea reports whether the rule is judged across all gauges around a place
// rather than per station.
func (r Rule) IsArea() bool { return r == RuleRain || r == RuleRainStale || r == RuleRisk }

const (
	// Readings older than these no longer describe the present. ThaiWater
	// publishes rain hourly and about an hour late, so a working gauge's newest
	// reading is routinely two hours old just before the next one lands.
	waterFresh = 3 * time.Hour
	rainFresh  = 3 * time.Hour
	// A station silent for longer than this is treated as retired, not as a live
	// one that went quiet, so it is neither watched nor alerted on.
	AliveWithin = 48 * time.Hour

	// Every place watches at least this many of its nearest stations, reaching
	// out to FallbackRadiusM when its own radius holds fewer. Water gauges are
	// sparse enough that a 5 km radius often contains none.
	minWater        = 2
	minRain         = 3
	FallbackRadiusM = 10_000

	// Rate of rise is measured against the reading nearest to an hour before the
	// latest one, taken from this window.
	riseMinSpan = 30 * time.Minute
	RiseMaxSpan = 3 * time.Hour

	risingMinCmPerHour   = 5.0
	risingHorizon        = 3 * time.Hour
	risingClearCmPerHour = 2.0
	risingClearHorizon   = 4 * time.Hour
	// Gates near the river mouth rise 20 to 30 cm/h on every flood tide while
	// far below their banks, so a fast rise only counts once the water is
	// already this close.
	risingWithinM = 0.5

	// How far past a threshold a reading must fall before its severity drops,
	// so a value hovering on a line does not alert every poll.
	waterHysteresisM = 0.05
	rainHysteresis   = 0.8
)

// Severity thresholds, lowest severity first.
var (
	// Water level relative to the lowest bank in metres; negative is below it.
	// Watch within 50 cm, warning within 20 cm, overflow at or above the bank.
	waterLevelThresholds = []float64{-0.5, -0.2, 0}
	// Roughly 60 mm/h is the drainage capacity commonly cited for Bangkok.
	rain1hThresholds = []float64{20, 40, 60}
	rain3hThresholds = []float64{40, 70, 100}
	// 90 mm is where the Thai Meteorological Department's "very heavy" daily
	// rain class begins.
	rain24hThresholds = []float64{90, 150, 250}
)

const (
	SeverityNone = iota
	SeverityWatch
	SeverityWarning
	SeveritySevere
)

type Subscription struct {
	ID        int64
	Channel   string
	Recipient string
	Label     string
	Lat, Lng  float64
	RadiusM   int
}

type Station struct {
	ID       int64
	Source   string
	Agency   string
	Kind     source.Kind
	Name     string
	NameTH   string // the station's Thai name, when the source gives one
	District string
	Lat, Lng float64
	BankMSL  *float64
}

type WaterPoint struct {
	At       time.Time
	LevelMSL float64
}

type RainPoint struct {
	At                      time.Time
	Rain1h, Rain3h, Rain24h *float64
}

// Snapshot is everything the rules read, loaded once per evaluation.
type Snapshot struct {
	Now         time.Time
	Stations    []Station
	LatestWater map[int64]WaterPoint // newest reading within AliveWithin
	LatestRain  map[int64]RainPoint  // newest reading within AliveWithin
	RecentWater map[int64][]WaterPoint
}

// Key identifies one thing a subscriber can be told about. StationID is 0 for
// area rules.
type Key struct {
	SubscriptionID int64
	StationID      int64
	Rule           Rule
}

type Finding struct {
	Key
	Severity int
	// Known is false when there is no fresh data to judge by. Held is then
	// true if the last judged severity was raised: it stands until fresh data
	// says otherwise, so a gauge going quiet never reads as an all clear.
	Known bool
	Held  bool

	// The station judged; for rain, the gauge with the most rain.
	Station   Station
	DistanceM float64
	At        time.Time

	LevelMSL, BankMSL       float64
	RiseCmPerHour           *float64
	Rain1h, Rain3h, Rain24h *float64
	// For rain, the window whose total set the severity; 0 when none did.
	RainWindow time.Duration
	// For area rules, how many watched gauges had fresh readings, and the
	// agencies running them, each of which is owed credit for its data.
	FreshGauges int
	Agencies    []string
}

// Assess judges every rule for one place. current holds the severities last
// judged, which hysteresis and held severities work from; it may be nil, as
// for a place not yet subscribed.
func Assess(snap Snapshot, sub Subscription, current map[Key]int) []Finding {
	water, rain := watched(snap, sub)
	var out []Finding
	for _, n := range water {
		out = append(out, assessWater(snap, sub, n, current)...)
	}
	return append(out, assessRain(snap, sub, rain, current)...)
}

// hold carries a finding's last judged severity while it has no fresh data.
func hold(f *Finding, current map[Key]int) {
	f.Severity = current[f.Key]
	f.Held = f.Severity > SeverityNone
}

type nearby struct {
	Station
	DistanceM float64
	fresh     bool
}

func watched(snap Snapshot, sub Subscription) (water, rain []nearby) {
	reach := float64(max(sub.RadiusM, FallbackRadiusM))
	for _, st := range snap.Stations {
		d := DistanceM(sub.Lat, sub.Lng, st.Lat, st.Lng)
		if d > reach {
			continue
		}
		switch st.Kind {
		case source.KindWater:
			// Without a bank level there is nothing to measure a water level against.
			if p, alive := snap.LatestWater[st.ID]; alive && st.BankMSL != nil {
				water = append(water, nearby{st, d, snap.Now.Sub(p.At) <= waterFresh})
			}
		case source.KindRain:
			if p, alive := snap.LatestRain[st.ID]; alive {
				rain = append(rain, nearby{st, d, snap.Now.Sub(p.At) <= rainFresh})
			}
		}
	}
	return pick(water, sub.RadiusM, minWater), pick(rain, sub.RadiusM, minRain)
}

// pick watches every station inside the radius, fresh or not, so one that goes
// quiet is noticed. It then tops up with the nearest fresh stations beyond the
// radius; counting stale ones there would let a dead neighbour crowd out a
// working gauge a little further away.
func pick(cands []nearby, radiusM, minimum int) []nearby {
	slices.SortFunc(cands, func(a, b nearby) int {
		return cmp.Or(cmp.Compare(a.DistanceM, b.DistanceM), cmp.Compare(a.ID, b.ID))
	})
	var out []nearby
	fresh := 0
	for _, c := range cands {
		if c.DistanceM <= float64(radiusM) {
			out = append(out, c)
			if c.fresh {
				fresh++
			}
		}
	}
	for _, c := range cands {
		if fresh >= minimum {
			break
		}
		if c.DistanceM > float64(radiusM) && c.DistanceM <= FallbackRadiusM && c.fresh {
			out = append(out, c)
			fresh++
		}
	}
	return out
}

func assessWater(snap Snapshot, sub Subscription, n nearby, current map[Key]int) []Finding {
	latest := snap.LatestWater[n.ID]
	finding := func(r Rule) Finding {
		return Finding{
			Key:     Key{SubscriptionID: sub.ID, StationID: n.ID, Rule: r},
			Station: n.Station, DistanceM: n.DistanceM, At: latest.At,
			LevelMSL: latest.LevelMSL, BankMSL: *n.BankMSL,
		}
	}
	level, rising, stale := finding(RuleWaterLevel), finding(RuleWaterRising), finding(RuleWaterStale)

	stale.Known = true
	if snap.Now.Sub(latest.At) > waterFresh {
		stale.Severity = SeverityWatch
		hold(&level, current)
		hold(&rising, current)
		return []Finding{level, rising, stale}
	}

	aboveBank := latest.LevelMSL - *n.BankMSL
	level.Known = true
	level.Severity = hysteresis(
		levelOf(aboveBank, waterLevelThresholds),
		levelOf(aboveBank+waterHysteresisM, waterLevelThresholds),
		current[level.Key])

	if rate, ok := riseRate(snap.RecentWater[n.ID], latest); ok {
		level.RiseCmPerHour, rising.RiseCmPerHour = &rate, &rate
		rising.Known = true
		rising.Severity = hysteresis(
			reachesBank(-aboveBank, rate, risingMinCmPerHour, risingHorizon, risingWithinM),
			reachesBank(-aboveBank, rate, risingClearCmPerHour, risingClearHorizon, risingWithinM+waterHysteresisM),
			current[rising.Key])
	}
	return []Finding{level, rising, stale}
}

// reachesBank reports SeverityWatch when water already within withinM of the
// bank, rising at least minRate cm/h, would cover the rest within horizon.
// Water over the bank and still rising counts too.
func reachesBank(freeboardM, rateCmPerHour, minRate float64, horizon time.Duration, withinM float64) int {
	if rateCmPerHour >= minRate && freeboardM <= withinM && freeboardM <= rateCmPerHour/100*horizon.Hours() {
		return SeverityWatch
	}
	return SeverityNone
}

func riseRate(history []WaterPoint, latest WaterPoint) (float64, bool) {
	target := latest.At.Add(-time.Hour)
	var best WaterPoint
	found := false
	for _, p := range history {
		span := latest.At.Sub(p.At)
		if span < riseMinSpan || span > RiseMaxSpan {
			continue
		}
		if !found || absDuration(p.At.Sub(target)) < absDuration(best.At.Sub(target)) {
			best, found = p, true
		}
	}
	if !found {
		return 0, false
	}
	return (latest.LevelMSL - best.LevelMSL) * 100 / latest.At.Sub(best.At).Hours(), true
}

func assessRain(snap Snapshot, sub Subscription, gauges []nearby, current map[Key]int) []Finding {
	rain := Finding{Key: Key{SubscriptionID: sub.ID, Rule: RuleRain}}
	stale := Finding{Key: Key{SubscriptionID: sub.ID, Rule: RuleRainStale}}
	if len(gauges) == 0 {
		// No gauge within reach at all is a gap in coverage, not a gauge gone quiet.
		return []Finding{rain, stale}
	}

	var worst nearby
	var worstPoint RainPoint
	worstRaw, relaxed := -1, 0
	for _, g := range gauges {
		p := snap.LatestRain[g.ID]
		if snap.Now.Sub(p.At) > rainFresh {
			continue
		}
		rain.FreshGauges++
		if g.Agency != "" && !slices.Contains(rain.Agencies, g.Agency) {
			rain.Agencies = append(rain.Agencies, g.Agency)
		}
		relaxed = max(relaxed, rainLevel(p, rainHysteresis))
		raw := rainLevel(p, 1)
		// Among gauges at the same level, one inside the radius outranks one
		// beyond it, which the overall risk would count a level lower.
		inside, worstInside := g.DistanceM <= float64(sub.RadiusM), worst.DistanceM <= float64(sub.RadiusM)
		if raw > worstRaw || (raw == worstRaw && (inside && !worstInside || inside == worstInside && wetter(p, worstPoint))) {
			worst, worstPoint, worstRaw = g, p, raw
		}
	}
	stale.FreshGauges, stale.Agencies = rain.FreshGauges, rain.Agencies
	stale.Known = true
	if rain.FreshGauges == 0 {
		stale.Severity = SeverityWatch
		hold(&rain, current)
		return []Finding{rain, stale}
	}

	rain.Known = true
	rain.Severity = hysteresis(worstRaw, relaxed, current[rain.Key])
	rain.Station, rain.DistanceM, rain.At = worst.Station, worst.DistanceM, worstPoint.At
	rain.Rain1h, rain.Rain3h, rain.Rain24h = worstPoint.Rain1h, worstPoint.Rain3h, worstPoint.Rain24h
	if _, rain.RainWindow = rainDriver(worstPoint, 1); rain.RainWindow == 0 {
		_, rain.RainWindow = rainDriver(worstPoint, rainHysteresis)
	}
	return []Finding{rain, stale}
}

// RainThreshold is the total a window's rain must reach for a severity, so a
// message can state the line a measurement crossed rather than a label.
func RainThreshold(window time.Duration, severity int) (float64, bool) {
	var thresholds []float64
	switch window {
	case time.Hour:
		thresholds = rain1hThresholds
	case 3 * time.Hour:
		thresholds = rain3hThresholds
	case 24 * time.Hour:
		thresholds = rain24hThresholds
	}
	if severity < SeverityWatch || severity > len(thresholds) {
		return 0, false
	}
	return thresholds[severity-1], true
}

func rainLevel(p RainPoint, factor float64) int {
	level, _ := rainDriver(p, factor)
	return level
}

// rainDriver is the worst severity across the windows a gauge reports, and the
// shortest window that reaches it, since "heavy rain" means something quite
// different over one hour than over a day. A factor below 1 relaxes every
// threshold, for judging whether a severity may drop.
func rainDriver(p RainPoint, factor float64) (int, time.Duration) {
	level, window := SeverityNone, time.Duration(0)
	for _, w := range []struct {
		mm         *float64
		window     time.Duration
		thresholds []float64
	}{
		{p.Rain1h, time.Hour, rain1hThresholds},
		{p.Rain3h, 3 * time.Hour, rain3hThresholds},
		{p.Rain24h, 24 * time.Hour, rain24hThresholds},
	} {
		if w.mm == nil {
			continue
		}
		if l := levelOf(*w.mm/factor, w.thresholds); l > level {
			level, window = l, w.window
		}
	}
	return level, window
}

func wetter(a, b RainPoint) bool {
	mm := func(p *float64) float64 {
		if p == nil {
			return -1
		}
		return *p
	}
	return cmp.Or(cmp.Compare(mm(a.Rain1h), mm(b.Rain1h)), cmp.Compare(mm(a.Rain24h), mm(b.Rain24h))) > 0
}

func levelOf(v float64, thresholds []float64) int {
	level := SeverityNone
	for i, t := range thresholds {
		if v >= t {
			level = i + 1
		}
	}
	return level
}

// hysteresis lets a severity rise at once but fall only as far as the relaxed
// thresholds allow.
func hysteresis(raw, relaxed, current int) int {
	if raw >= current {
		return raw
	}
	return max(raw, min(current, relaxed))
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
