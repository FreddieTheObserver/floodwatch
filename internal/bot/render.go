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

	disclaimer = "<i>Unofficial, and it can be late or wrong. Official info: BMA 1555, emergencies 1669.</i>"
	// Promised to the BMA Drainage and Sewerage Department in the request for
	// access: every message using their rain data credits them in these words.
	bmaCredit = "ข้อมูลฝน: สำนักการระบายน้ำ กรุงเทพมหานคร"

	// Gauges cannot say whether a street is under water; BMA's road sensor map
	// can. Linking to the public page needs no permission, unlike reading its
	// data. The page cannot be opened on a location, hence the zoom hint.
	floodRoadsLine = `🚗 <a href="https://now.bangkok.go.th/flood-alert.html">Flooded roads right now</a> on the BMA map (zoom to your area)`
)

// Indexed by risk level, the last entry being alert.RiskUnknown.
var (
	riskIcons   = [...]string{"🟢", "🟡", "🟠", "🔴", staleIcon}
	riskNames   = [...]string{"LOW", "WATCH", "WARNING", "HIGH", "UNKNOWN"}
	riskActions = [...]string{
		"Nothing to do right now.",
		"Keep an eye on it, and check the flooded roads map before driving through low roads.",
		"Move your car to higher ground and valuables off the floor. Avoid low roads.",
		"Water is over the bank nearby. Stay off flooded roads, move vehicles and valuables up now, and follow official BMA instructions (1555).",
		"I can't judge the risk without fresh readings. Check official BMA updates and the flooded roads map.",
	}
)

var (
	severityIcons = [...]string{"🟢", "🟡", "🟠", "🔴"}
	rainTitles    = [...]string{"Light or no rain", "Heavy rain", "Very heavy rain", "Rain beyond drainage capacity"}
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

func millimetres(v float64) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("%.0f mm", v)
	}
	return fmt.Sprintf("%.1f mm", v)
}

func bankText(level, bank float64) string {
	switch d := level - bank; {
	case d >= 0.005:
		return fmt.Sprintf("%.2f m above the bank", d)
	case d <= -0.005:
		return fmt.Sprintf("%.2f m below the bank", -d)
	default:
		return "at the bank"
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

// rainTitle names the window behind a rain severity, so a day's total is not
// mistaken for rain falling now.
func rainTitle(f alert.Finding) string {
	title := rainTitles[f.Severity]
	if f.Severity == alert.SeverityNone {
		return title
	}
	switch f.RainWindow {
	case time.Hour:
		return title + " in the last hour"
	case 3 * time.Hour:
		return title + " in the last 3 hours"
	case 24 * time.Hour:
		return title + " over the last 24 hours"
	}
	return title
}

func rainAmounts(f alert.Finding) string {
	var parts []string
	for _, w := range []struct {
		mm     *float64
		window string
	}{{f.Rain1h, "1 h"}, {f.Rain3h, "3 h"}, {f.Rain24h, "24 h"}} {
		if w.mm != nil {
			parts = append(parts, millimetres(*w.mm)+" in "+w.window)
		}
	}
	return strings.Join(parts, ", ")
}

func where(f alert.Finding) string {
	return fmt.Sprintf("%s (%s)", esc(f.Station.Name), distance(f.DistanceM))
}

// whereFrom also says when a station is beyond the place's radius, which is
// why it counts a level lower than its own reading suggests.
func whereFrom(f alert.Finding, sub alert.Subscription) string {
	if f.DistanceM > float64(sub.RadiusM) {
		return fmt.Sprintf("%s (%s, outside your %s radius)", esc(f.Station.Name), distance(f.DistanceM), distance(float64(sub.RadiusM)))
	}
	return where(f)
}

func joinNonEmpty(sep string, parts ...string) string {
	return strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), sep)
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

func headline(sub alert.Subscription, r alert.Risk) string {
	return joinNonEmpty(" · ",
		fmt.Sprintf("%s <b>%s</b> around <b>%s</b>", riskIcons[r.Level], riskNames[r.Level], esc(sub.Label)),
		trendText(r.Trend))
}

// reasonText explains a risk level in words, from what set it.
func reasonText(sub alert.Subscription, a alert.Assessment) string {
	switch {
	case a.Risk.Level == alert.RiskUnknown:
		return fmt.Sprintf("No gauge near %s has reported recently.", esc(sub.Label))
	case len(a.Risk.Drivers) == 0:
		return fmt.Sprintf("Nothing near %s is at a warning level.", esc(sub.Label))
	}
	parts := make([]string, len(a.Risk.Drivers))
	for i, d := range a.Risk.Drivers {
		parts[i] = driverText(d, sub)
	}
	return strings.Join(parts, " ")
}

func driverText(f alert.Finding, sub alert.Subscription) string {
	switch f.Rule {
	case alert.RuleRain:
		if f.Held {
			return rainTitles[f.Severity] + " was the last reading before the rain gauges nearby went quiet."
		}
		return fmt.Sprintf("%s at %s: %s.", rainTitle(f), whereFrom(f, sub), rainAmounts(f))

	case alert.RuleWaterRising, alert.RuleWaterLevel:
		bank := bankText(f.LevelMSL, f.BankMSL)
		if f.Held {
			return fmt.Sprintf("%s was %s when it last reported at %s, and has been silent since.", whereFrom(f, sub), bank, clock(f.At))
		}
		if f.Rule == alert.RuleWaterLevel {
			return fmt.Sprintf("%s is %s.", whereFrom(f, sub), joinNonEmpty(", ", bank, riseText(f.RiseCmPerHour)))
		}
		freeboard := f.BankMSL - f.LevelMSL
		if freeboard <= 0 || f.RiseCmPerHour == nil || *f.RiseCmPerHour <= 0 {
			return fmt.Sprintf("%s is %s and still %s.", whereFrom(f, sub), bank, riseText(f.RiseCmPerHour))
		}
		return fmt.Sprintf("%s is %s and %s; at this rate it reaches the bank in about %s.",
			whereFrom(f, sub), bank, riseText(f.RiseCmPerHour), roughDuration(freeboard*100 / *f.RiseCmPerHour))
	}
	return ""
}

// detailsText lists every reading behind a risk, for anyone who wants the
// numbers rather than the verdict.
func detailsText(a alert.Assessment, now time.Time) string {
	var b strings.Builder
	b.WriteString("<b>Details</b>\n🌊 <b>Water</b>\n")

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
	for _, id := range order {
		s := stations[id]
		if s.stale.Severity > alert.SeverityNone || !s.level.Known {
			fmt.Fprintf(&b, "%s %s: no data since %s, %s\n", staleIcon, where(s.stale), clock(s.stale.At), ago(s.stale.At, now))
			continue
		}
		detail := joinNonEmpty(", ", bankText(s.level.LevelMSL, s.level.BankMSL), riseText(s.level.RiseCmPerHour))
		fmt.Fprintf(&b, "%s %s: %s at %s, %s\n", severityIcons[s.level.Severity], where(s.level), detail, clock(s.level.At), ago(s.level.At, now))
	}

	b.WriteString("🌧️ <b>Rain</b>\n")
	switch {
	case rain.Known:
		fmt.Fprintf(&b, "%s %s: %s at %s, %s, %s\n", severityIcons[rain.Severity], rainTitle(rain),
			rainAmounts(rain), where(rain), clock(rain.At), ago(rain.At, now))
		fmt.Fprintf(&b, "Wettest of %d gauges reporting nearby.\n", rain.FreshGauges)
	case rainStale.Known:
		fmt.Fprintf(&b, "%s No fresh rain readings from any gauge nearby.\n", staleIcon)
	default:
		fmt.Fprintf(&b, "No rain gauge within %d km.\n", alert.FallbackRadiusM/1000)
	}

	b.WriteString(sourcesText(a.Findings))
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
	var parts []string
	if len(water) > 0 {
		parts = append(parts, "water gauges by "+strings.Join(water, ", "))
	}
	if len(rain) > 0 {
		parts = append(parts, "rain gauges by "+strings.Join(rain, ", "))
	}
	return "Sources: " + strings.Join(append(parts, "all via ThaiWater (HII)."), "; ")
}

func footer(findings []alert.Finding, checked time.Time) string {
	lines := []string{"Checked " + clock(checked) + "."}
	if slices.ContainsFunc(findings, usesBMAData) {
		lines = append(lines, bmaCredit)
	}
	return strings.Join(append(lines, disclaimer), "\n")
}

// statusText leads with the verdict, its reason and what to do, and keeps the
// readings behind it in a collapsed section.
func statusText(sub alert.Subscription, a alert.Assessment, checked time.Time) string {
	var b strings.Builder
	b.WriteString(headline(sub, a.Risk) + "\n\n")
	b.WriteString(reasonText(sub, a) + "\n")
	b.WriteString("<b>" + riskActions[a.Risk.Level] + "</b>\n\n")
	b.WriteString(floodRoadsLine + "\n\n")
	b.WriteString("<blockquote expandable>" + detailsText(a, checked) + "</blockquote>\n")
	b.WriteString(footer(a.Findings, checked))
	return b.String()
}

// digestText tells a subscriber that a place's overall risk changed.
func digestText(d alert.Digest, checked time.Time) string {
	sub, r := d.Subscription, d.Risk
	var b strings.Builder
	switch r.Level {
	case alert.SeverityNone:
		fmt.Fprintf(&b, "%s <b>%s: back to LOW</b>\n", clearIcon, esc(sub.Label))
	case alert.RiskUnknown:
		fmt.Fprintf(&b, "%s <b>%s: no fresh readings</b>\n", staleIcon, esc(sub.Label))
	default:
		fmt.Fprintf(&b, "%s\n", joinNonEmpty(" · ",
			fmt.Sprintf("%s <b>%s: %s → %s</b>", riskIcons[r.Level], esc(sub.Label), riskNames[d.From], riskNames[r.Level]),
			trendText(r.Trend)))
	}
	b.WriteString(reasonText(sub, d.Assessment) + "\n")
	if r.Level != alert.SeverityNone {
		b.WriteString("<b>" + riskActions[r.Level] + "</b>\n")
		b.WriteString("\n" + floodRoadsLine + "\n")
	}
	b.WriteString("Send /status for the full picture.\n\n")
	b.WriteString(footer(d.Findings, checked))
	return b.String()
}
