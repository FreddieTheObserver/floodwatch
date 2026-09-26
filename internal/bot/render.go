package bot

import (
	"fmt"
	"html"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/FreddieTheObserver/floodwatch/internal/alert"
)

var ict = time.FixedZone("ICT", 7*60*60)

const (
	clearIcon = "✅"
	staleIcon = "⚪"

	disclaimer = "<i>Unofficial data. Measurements may be delayed, incomplete or inaccurate.\n" +
		"Official information: BMA 1555 · Emergency: 1669</i>"
	// Promised to the BMA Drainage and Sewerage Department in the request for
	// access: every message using their rain data credits them in these words.
	bmaCredit = "ข้อมูลฝน: สำนักการระบายน้ำ กรุงเทพมหานคร"

	// Gauges cannot say whether a street is under water; BMA's road sensor map
	// can. Linking to the public page needs no permission, unlike reading its
	// data. The page cannot be opened on a location, hence the zoom hint.
	floodRoadsLine = `🚗 <a href="https://now.bangkok.go.th/flood-alert.html">Flooded roads right now</a> on the BMA map (zoom to your area)`

	regionalNote = "Stations marked regional are outside your %s radius. They are included because they may show regional flood conditions."
)

// Indexed by risk level, the last entry being alert.RiskUnknown.
var (
	riskIcons   = [...]string{"🟢", "🟡", "🟠", "🔴", staleIcon}
	riskNames   = [...]string{"LOW", "WATCH", "WARNING", "HIGH", "NO DATA"}
	riskActions = [...][]string{
		{"Nothing to do right now."},
		{"Keep an eye on updates.", "Check the flood map before driving."},
		{"Move your car to higher ground.", "Move valuables off the floor.", "Avoid low roads."},
		{"Move your car and valuables up now.", "Stay off flooded roads.", "Follow official BMA instructions (1555)."},
		{"Check official BMA updates.", "Check the flood map before driving."},
	}
)

func esc(s string) string { return html.EscapeString(s) }

func clock(t time.Time) string { return t.In(ict).Format("15:04") }

// ago says how old a reading is, since a time alone leaves the reader to work
// out whether it is still current.
func ago(t, now time.Time) string {
	d := now.Sub(t).Round(time.Minute)
	switch h, m := int(d.Hours()), int(d.Minutes())%60; {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", m)
	case m == 0 || h >= 3:
		return fmt.Sprintf("%d h ago", h)
	default:
		return fmt.Sprintf("%d h %d min ago", h, m)
	}
}

func distance(m float64) string {
	if m < 1000 {
		return fmt.Sprintf("%.0f m", math.Round(m/100)*100)
	}
	return fmt.Sprintf("%.1f km", m/1000)
}

func radius(sub alert.Subscription) string {
	km := float64(sub.RadiusM) / 1000
	if km == math.Trunc(km) {
		return fmt.Sprintf("%.0f km", km)
	}
	return fmt.Sprintf("%.1f km", km)
}

func millimetres(v float64) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("%.0f mm", v)
	}
	return fmt.Sprintf("%.1f mm", v)
}

// bankText is a measurement, as in "0.41 m below bank".
func bankText(level, bank float64) string {
	switch d := level - bank; {
	case d >= 0.005:
		return fmt.Sprintf("%.2f m above bank", d)
	case d <= -0.005:
		return fmt.Sprintf("%.2f m below bank", -d)
	default:
		return "at bank level"
	}
}

// bankSentence is the same measurement inside a sentence about a station.
func bankSentence(level, bank float64) string {
	switch d := level - bank; {
	case d >= 0.005:
		return fmt.Sprintf("%.2f m above its bank", d)
	case d <= -0.005:
		return fmt.Sprintf("%.2f m below its bank", -d)
	default:
		return "level with its bank"
	}
}

func riseText(rate *float64) string {
	switch {
	case rate == nil:
		return ""
	case *rate >= 1:
		return fmt.Sprintf("rising %.0f cm/h", *rate)
	case *rate <= -1:
		return fmt.Sprintf("falling %.0f cm/h", -*rate)
	default:
		return "steady"
	}
}

func windowText(w time.Duration) string {
	switch w {
	case time.Hour:
		return "the last hour"
	case 3 * time.Hour:
		return "the last 3 hours"
	}
	return "the last 24 hours"
}

func rainIn(f alert.Finding, w time.Duration) *float64 {
	switch w {
	case time.Hour:
		return f.Rain1h
	case 3 * time.Hour:
		return f.Rain3h
	}
	return f.Rain24h
}

// rainAmounts is a gauge's totals on one line, for map pins.
func rainAmounts(f alert.Finding) string {
	var parts []string
	for _, w := range []struct {
		d     time.Duration
		label string
	}{{time.Hour, "1 h"}, {3 * time.Hour, "3 h"}, {24 * time.Hour, "24 h"}} {
		if mm := rainIn(f, w.d); mm != nil {
			parts = append(parts, millimetres(*mm)+" in "+w.label)
		}
	}
	return strings.Join(parts, ", ")
}

func regional(f alert.Finding, sub alert.Subscription) bool {
	return f.DistanceM > float64(sub.RadiusM)
}

// stationLabel names a station with its distance, marking one beyond the
// place's radius as regional so it is not read as the place itself.
func stationLabel(f alert.Finding, sub alert.Subscription) string {
	if regional(f, sub) {
		return fmt.Sprintf("%s (%s, regional)", esc(f.Station.Name), distance(f.DistanceM))
	}
	return fmt.Sprintf("%s (%s)", esc(f.Station.Name), distance(f.DistanceM))
}

func joinNonEmpty(sep string, parts ...string) string {
	return strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), sep)
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func roughDuration(hours float64) string {
	switch {
	case hours < 0.75:
		return fmt.Sprintf("%.0f minutes", math.Max(10, math.Round(hours*60/10)*10))
	case hours < 1.5:
		return "an hour"
	default:
		return fmt.Sprintf("%.0f hours", math.Round(hours))
	}
}

// usesBMAData reports whether a finding drew on BMA gauges, whether fetched
// from the BMA directly or republished through ThaiWater.
func usesBMAData(f alert.Finding) bool {
	return f.Station.Source == "bma" || f.Station.Agency == "BMA" || slices.Contains(f.Agencies, "BMA")
}

// covered reports whether a place has any station within reach.
func covered(findings []alert.Finding) bool {
	return slices.ContainsFunc(findings, func(f alert.Finding) bool {
		return !f.Rule.IsArea() || f.Known
	})
}

func trendText(t alert.Trend) string {
	switch t {
	case alert.TrendWorse:
		return "↗ getting worse"
	case alert.TrendBetter:
		return "↘ improving"
	case alert.TrendStable:
		return "→ steady"
	}
	return ""
}

// happeningText says what the measurements show, in words, from whatever set
// the risk. It interprets nothing beyond naming the threshold crossed.
func happeningText(sub alert.Subscription, a alert.Assessment) string {
	switch {
	case a.Risk.Level == alert.RiskUnknown:
		return fmt.Sprintf("No gauge near %s has reported in the last few hours.", esc(sub.Label))
	case len(a.Risk.Drivers) == 0:
		return fmt.Sprintf("Nothing near %s is at a warning level.", esc(sub.Label))
	}
	parts := make([]string, len(a.Risk.Drivers))
	for i, d := range a.Risk.Drivers {
		parts[i] = driverText(d, sub)
	}
	return strings.Join(parts, "\n")
}

func driverText(f alert.Finding, sub alert.Subscription) string {
	where := fmt.Sprintf("%s, %s away", esc(f.Station.Name), distance(f.DistanceM))
	prefix := ""
	if regional(f, sub) {
		prefix = "Regional: "
		where += fmt.Sprintf(" (outside your %s radius)", radius(sub))
	}

	switch f.Rule {
	case alert.RuleRain:
		if f.Held {
			return "The rain gauges nearby have stopped reporting; the last readings were above FloodWatch's thresholds."
		}
		text := prefix + rainSentence(f, where)
		if f.RainWindow != time.Hour && f.Rain1h != nil {
			text += fmt.Sprintf(", with %s in the last hour", millimetres(*f.Rain1h))
		}
		return text + "."

	case alert.RuleWaterLevel, alert.RuleWaterRising:
		bank := bankSentence(f.LevelMSL, f.BankMSL)
		if f.Held {
			return fmt.Sprintf("%s%s, was %s when it last reported at %s and has been silent since.", prefix, where, bank, clock(f.At))
		}
		if f.Rule == alert.RuleWaterLevel {
			return fmt.Sprintf("%s%s, is %s.", prefix, where, joinNonEmpty(" and ", bank, riseText(f.RiseCmPerHour)))
		}
		freeboard := f.BankMSL - f.LevelMSL
		if freeboard <= 0 || f.RiseCmPerHour == nil || *f.RiseCmPerHour <= 0 {
			return fmt.Sprintf("%s%s, is %s and still %s.", prefix, where, bank, riseText(f.RiseCmPerHour))
		}
		return fmt.Sprintf("%s%s, is %s and %s; at this rate it reaches the bank in about %s.",
			prefix, where, bank, riseText(f.RiseCmPerHour), roughDuration(freeboard*100 / *f.RiseCmPerHour))
	}
	return ""
}

// rainSentence states the total that set a rain severity against the line it
// crossed, rather than labelling it, so a day's total is never mistaken for
// rain falling now.
func rainSentence(f alert.Finding, where string) string {
	mm := rainIn(f, f.RainWindow)
	threshold, ok := alert.RainThreshold(f.RainWindow, f.Severity)
	if mm == nil || !ok {
		return fmt.Sprintf("Rain at %s: %s", where, rainAmounts(f))
	}
	relation := "above"
	if *mm < threshold {
		relation = "just under" // held up by hysteresis while easing
	}
	return fmt.Sprintf("%s, recorded %s over %s, %s FloodWatch's %s threshold",
		where, millimetres(*mm), windowText(f.RainWindow), relation, millimetres(threshold))
}

// trendReason explains the trend by the reading moving it.
func trendReason(a alert.Assessment) string {
	candidates := slices.DeleteFunc(slices.Clone(a.Risk.Drivers), func(f alert.Finding) bool { return f.Held })
	if len(a.Risk.Drivers) == 0 {
		for _, f := range a.Findings {
			if f.Rule == alert.RuleRain && f.Known {
				candidates = append(candidates, f)
			}
		}
	}
	var f alert.Finding
	for _, c := range candidates {
		if alert.FindingTrend(c) == a.Risk.Trend {
			f = c
			break
		}
	}

	switch {
	case a.Risk.Trend == alert.TrendUnknown:
		return "Unknown: there are no fresh readings to judge by."
	case a.Risk.Trend == alert.TrendStable:
		return "Steady: no significant change over the last hour."
	case f.Rule == "" && a.Risk.Trend == alert.TrendWorse:
		return "Getting worse."
	case f.Rule == "":
		return "Improving."
	}
	name := esc(f.Station.Name)
	switch {
	case a.Risk.Trend == alert.TrendWorse && f.Rule == alert.RuleRain:
		return fmt.Sprintf("Getting worse because rain at %s is getting heavier (%s in the last hour).", name, millimetres(*f.Rain1h))
	case a.Risk.Trend == alert.TrendWorse:
		return fmt.Sprintf("Getting worse because water at %s is %s.", name, riseText(f.RiseCmPerHour))
	case f.Rule == alert.RuleRain:
		return fmt.Sprintf("Improving because rain at %s has eased.", name)
	default:
		return fmt.Sprintf("Improving because water at %s is %s.", name, riseText(f.RiseCmPerHour))
	}
}

// detailsText lists the measurements behind a risk, and nothing else.
func detailsText(sub alert.Subscription, a alert.Assessment, now time.Time) string {
	var b strings.Builder
	b.WriteString("<b>Details</b>\n\n<b>WATER</b>\n")

	type station struct{ level, stale alert.Finding }
	var order []int64
	stations := map[int64]*station{}
	var rain, rainStale alert.Finding
	for _, f := range a.Findings {
		switch f.Rule {
		case alert.RuleRain:
			rain = f
		case alert.RuleRainStale:
			rainStale = f
		case alert.RuleWaterLevel, alert.RuleWaterStale:
			s, ok := stations[f.StationID]
			if !ok {
				s = &station{}
				stations[f.StationID] = s
				order = append(order, f.StationID)
			}
			if f.Rule == alert.RuleWaterLevel {
				s.level = f
			} else {
				s.stale = f
			}
		}
	}

	if len(order) == 0 {
		fmt.Fprintf(&b, "No water level gauge within %d km.\n", alert.FallbackRadiusM/1000)
	}
	anyRegional := false
	for _, id := range order {
		s := stations[id]
		anyRegional = anyRegional || regional(s.stale, sub)
		b.WriteString(stationLabel(s.stale, sub) + "\n")
		if s.stale.Severity > alert.SeverityNone || !s.level.Known {
			fmt.Fprintf(&b, "No data since %s · %s\n\n", clock(s.stale.At), ago(s.stale.At, now))
			continue
		}
		fmt.Fprintf(&b, "%s\nMeasured %s · %s\n\n",
			joinNonEmpty(" · ", bankText(s.level.LevelMSL, s.level.BankMSL), capitalise(riseText(s.level.RiseCmPerHour))),
			clock(s.level.At), ago(s.level.At, now))
	}

	b.WriteString("<b>RAIN</b>\n")
	switch {
	case rain.Known:
		anyRegional = anyRegional || regional(rain, sub)
		b.WriteString(stationLabel(rain, sub) + "\n")
		for _, w := range []struct {
			d     time.Duration
			label string
		}{{time.Hour, "1 hour"}, {3 * time.Hour, "3 hours"}, {24 * time.Hour, "24 hours"}} {
			if mm := rainIn(rain, w.d); mm != nil {
				fmt.Fprintf(&b, "%s: %s\n", w.label, millimetres(*mm))
			}
		}
		fmt.Fprintf(&b, "Measured %s · %s\nWettest of %d nearby gauges reporting.\n", clock(rain.At), ago(rain.At, now), rain.FreshGauges)
	case rainStale.Known:
		b.WriteString("No fresh readings from any rain gauge nearby.\n")
	default:
		fmt.Fprintf(&b, "No rain gauge within %d km.\n", alert.FallbackRadiusM/1000)
	}
	if anyRegional {
		b.WriteString("\n" + fmt.Sprintf(regionalNote, radius(sub)) + "\n")
	}

	b.WriteString("\n<b>TREND</b>\n" + trendReason(a) + "\n")
	b.WriteString("\n<b>SOURCES</b>\n" + sourcesText(a.Findings))
	return b.String()
}

// sourcesText names who runs the gauges behind each kind of reading, all of
// which currently arrive through ThaiWater.
func sourcesText(findings []alert.Finding) string {
	var water, rain []string
	add := func(list *[]string, agency string) {
		if agency != "" && !slices.Contains(*list, agency) {
			*list = append(*list, agency)
		}
	}
	for _, f := range findings {
		switch f.Rule {
		case alert.RuleWaterLevel:
			add(&water, f.Station.Agency)
		case alert.RuleRain:
			for _, a := range f.Agencies {
				add(&rain, a)
			}
		}
	}
	var lines []string
	if len(water) > 0 {
		lines = append(lines, "Water gauges: "+strings.Join(water, ", "))
	}
	if len(rain) > 0 {
		lines = append(lines, "Rain gauges: "+strings.Join(rain, ", "))
	}
	return strings.Join(append(lines, "Data: ThaiWater (HII)"), "\n")
}

func footer(findings []alert.Finding, checked time.Time) string {
	lines := []string{"Checked " + clock(checked)}
	if slices.ContainsFunc(findings, usesBMAData) {
		lines = append(lines, bmaCredit)
	}
	return strings.Join(append(lines, "", disclaimer), "\n")
}

// body is everything below the headline, in the same order for every state:
// what is happening, what to do, the flood map, the measurements, then
// sources, time and disclaimer.
func body(sub alert.Subscription, a alert.Assessment, checked time.Time, withMap bool) string {
	var b strings.Builder
	b.WriteString(happeningText(sub, a) + "\n\n")
	b.WriteString("<b>What to do</b>\n")
	for _, step := range riskActions[a.Risk.Level] {
		b.WriteString("• " + step + "\n")
	}
	if withMap {
		b.WriteString("\n" + floodRoadsLine + "\n")
	}
	b.WriteString("\n<blockquote expandable>" + detailsText(sub, a, checked) + "</blockquote>\n")
	b.WriteString(footer(a.Findings, checked))
	return b.String()
}

func headline(sub alert.Subscription, r alert.Risk) string {
	return joinNonEmpty(" · ",
		fmt.Sprintf("%s <b>%s</b> around <b>%s</b>", riskIcons[r.Level], riskNames[r.Level], esc(sub.Label)),
		trendText(r.Trend))
}

// statusText is the full picture for one place.
func statusText(sub alert.Subscription, a alert.Assessment, checked time.Time) string {
	return headline(sub, a.Risk) + "\n\n" + body(sub, a, checked, true)
}

// digestText tells a subscriber that a place's overall risk changed, with the
// same picture a status gives.
func digestText(d alert.Digest, checked time.Time) string {
	sub, r := d.Subscription, d.Risk
	var head string
	switch r.Level {
	case alert.SeverityNone:
		head = fmt.Sprintf("%s <b>%s: back to LOW</b>", clearIcon, esc(sub.Label))
	case alert.RiskUnknown:
		head = fmt.Sprintf("%s <b>%s: NO DATA</b>", staleIcon, esc(sub.Label))
	default:
		head = joinNonEmpty(" · ",
			fmt.Sprintf("%s <b>%s: %s → %s</b>", riskIcons[r.Level], esc(sub.Label), riskNames[d.From], riskNames[r.Level]),
			trendText(r.Trend))
	}
	return head + "\n\n" + body(sub, d.Assessment, checked, r.Level != alert.SeverityNone)
}
