package bot

import (
	"cmp"
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

var (
	severityIcons = [...]string{"🟢", "🟡", "🟠", "🔴"}
	waterTitles   = [...]string{"Normal", "High water", "Near the bank", "Overflowing"}
	rainTitles    = [...]string{"Light or no rain", "Heavy rain", "Very heavy rain", "Rain beyond drainage capacity"}
)

func esc(s string) string { return html.EscapeString(s) }

func clock(t time.Time) string { return t.In(ict).Format("15:04") }

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

func joinNonEmpty(sep string, parts ...string) string {
	return strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), sep)
}

func footer(findings []alert.Finding, checked time.Time) string {
	lines := []string{"Readings from ThaiWater (HII), checked " + clock(checked) + "."}
	if slices.ContainsFunc(findings, func(f alert.Finding) bool { return f.Station.Source == "bma" }) {
		lines = append(lines, bmaCredit)
	}
	return strings.Join(append(lines, disclaimer), "\n")
}

// digestText renders one place's changes, worst news first and recoveries last.
func digestText(d alert.Digest, checked time.Time) string {
	changes := slices.Clone(d.Changes)
	slices.SortStableFunc(changes, func(a, b alert.Change) int {
		return cmp.Or(
			cmp.Compare(b.Severity, a.Severity),
			cmp.Compare(b.Severity-b.From, a.Severity-a.From),
			cmp.Compare(a.DistanceM, b.DistanceM))
	})

	var b strings.Builder
	fmt.Fprintf(&b, "🌊 <b>FloodWatch · %s</b>\n", esc(d.Subscription.Label))
	findings := make([]alert.Finding, len(changes))
	for i, c := range changes {
		b.WriteString("\n" + changeText(c) + "\n")
		findings[i] = c.Finding
	}
	// Only worsening news sends people to check the roads; an all clear does not.
	if slices.ContainsFunc(changes, func(c alert.Change) bool { return c.Severity > c.From }) {
		b.WriteString("\n" + floodRoadsLine + "\n")
	}
	b.WriteString("\n" + footer(findings, checked))
	return b.String()
}

func changeText(c alert.Change) string {
	easing := c.Severity < c.From && c.Severity > alert.SeverityNone
	switch c.Rule {
	case alert.RuleWaterLevel:
		detail := joinNonEmpty(", ", bankText(c.LevelMSL, c.BankMSL), riseText(c.RiseCmPerHour))
		if c.Severity == alert.SeverityNone {
			return fmt.Sprintf("%s <b>Back to normal</b>: %s\n%s at %s.", clearIcon, where(c.Finding), detail, clock(c.At))
		}
		title := waterTitles[c.Severity]
		if easing {
			title = "Easing, now " + strings.ToLower(title)
		}
		return fmt.Sprintf("%s <b>%s</b>: %s\n%s at %s.", severityIcons[c.Severity], title, where(c.Finding), capitalise(detail), clock(c.At))

	case alert.RuleWaterRising:
		if c.Severity == alert.SeverityNone {
			return fmt.Sprintf("%s <b>No longer rising fast</b>: %s", clearIcon, where(c.Finding))
		}
		return fmt.Sprintf("%s <b>Rising fast</b>: %s\n%s.", severityIcons[alert.SeverityWarning], where(c.Finding), risingDetail(c.Finding))

	case alert.RuleWaterStale:
		if c.Severity == alert.SeverityNone {
			return fmt.Sprintf("%s <b>Reporting again</b>: %s", clearIcon, where(c.Finding))
		}
		return fmt.Sprintf("%s <b>No data</b>: %s has not reported since %s.", staleIcon, where(c.Finding), clock(c.At))

	case alert.RuleRain:
		if c.Severity == alert.SeverityNone {
			return fmt.Sprintf("%s <b>Rain has eased</b>. Wettest gauge now: %s at %s.", clearIcon, rainAmounts(c.Finding), where(c.Finding))
		}
		title := rainTitle(c.Finding)
		if easing {
			title = "Rain easing, now " + strings.ToLower(title)
		}
		return fmt.Sprintf("%s <b>%s</b>: %s at %s, %s.", severityIcons[c.Severity], title, rainAmounts(c.Finding), where(c.Finding), clock(c.At))

	case alert.RuleRainStale:
		if c.Severity == alert.SeverityNone {
			return fmt.Sprintf("%s <b>Rain readings are back</b> nearby.", clearIcon)
		}
		return fmt.Sprintf("%s <b>No fresh rain readings</b> from any gauge nearby.", staleIcon)
	}
	return ""
}

func risingDetail(f alert.Finding) string {
	rate := riseText(f.RiseCmPerHour)
	freeboard := f.BankMSL - f.LevelMSL
	if freeboard <= 0 || f.RiseCmPerHour == nil || *f.RiseCmPerHour <= 0 {
		return capitalise(joinNonEmpty(" while ", rate, bankText(f.LevelMSL, f.BankMSL)))
	}
	hours := freeboard * 100 / *f.RiseCmPerHour
	return capitalise(fmt.Sprintf("%s, %s; at this rate it reaches the bank in about %s",
		rate, bankText(f.LevelMSL, f.BankMSL), roughDuration(hours)))
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

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// covered reports whether a place has any station within reach.
func covered(findings []alert.Finding) bool {
	return slices.ContainsFunc(findings, func(f alert.Finding) bool {
		return !f.Rule.IsArea() || f.Known
	})
}

// statusText renders the current readings around one place.
func statusText(sub alert.Subscription, findings []alert.Finding, checked time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📍 <b>%s</b>\n\n<b>Water</b>\n", esc(sub.Label))

	type station struct{ level, stale alert.Finding }
	var order []int64
	stations := map[int64]*station{}
	var rain, rainStale alert.Finding
	for _, f := range findings {
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
			fmt.Fprintf(&b, "%s %s: no data since %s\n", staleIcon, where(s.stale), clock(s.stale.At))
			continue
		}
		detail := joinNonEmpty(", ", bankText(s.level.LevelMSL, s.level.BankMSL), riseText(s.level.RiseCmPerHour))
		fmt.Fprintf(&b, "%s %s: %s at %s\n", severityIcons[s.level.Severity], where(s.level), detail, clock(s.level.At))
	}

	b.WriteString("\n<b>Rain</b>\n")
	switch {
	case rain.Known:
		fmt.Fprintf(&b, "%s %s: %s at %s, %s\n", severityIcons[rain.Severity], rainTitle(rain),
			rainAmounts(rain), where(rain), clock(rain.At))
		fmt.Fprintf(&b, "Wettest of %d gauges reporting nearby.\n", rain.FreshGauges)
	case rainStale.Known:
		fmt.Fprintf(&b, "%s No fresh rain readings from any gauge nearby.\n", staleIcon)
	default:
		fmt.Fprintf(&b, "No rain gauge within %d km.\n", alert.FallbackRadiusM/1000)
	}

	b.WriteString("\n" + floodRoadsLine + "\n")
	b.WriteString("\n" + footer(findings, checked))
	return b.String()
}
