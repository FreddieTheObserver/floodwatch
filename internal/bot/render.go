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

	// Promised to the BMA Drainage and Sewerage Department in the request for
	// access: every message using their rain data credits them in these words,
	// whatever language the rest of it is in.
	bmaCredit = "ข้อมูลฝน: สำนักการระบายน้ำ กรุงเทพมหานคร"

	// Gauges cannot say whether a street is under water; BMA's road sensor map
	// can. Linking to the public page needs no permission, unlike reading its
	// data. The page cannot be opened on a location, hence the zoom hint.
	floodRoadsURL = "https://now.bangkok.go.th/flood-alert.html"

	// Starts the naming question, in every language, so a reply to it can be
	// recognised without knowing which language it was asked in.
	renameMarker = "✏️"
)

// Indexed by risk level, the last entry being alert.RiskUnknown.
var riskIcons = [...]string{"🟢", "🟡", "🟠", "🔴", staleIcon}

func esc(s string) string { return html.EscapeString(s) }

// agoParts splits a reading's age for the per-language wording: minutes are
// dropped from three hours on, where they are noise.
func agoParts(t, now time.Time) (hours, minutes int, justNow bool) {
	d := now.Sub(t).Round(time.Minute)
	if d < time.Minute {
		return 0, 0, true
	}
	hours, minutes = int(d.Hours()), int(d.Minutes())%60
	if hours >= 3 {
		minutes = 0
	}
	return hours, minutes, false
}

func roundedMinutes(hours float64) float64 {
	return math.Max(10, math.Round(hours*60/10)*10)
}

// radius drops a whole number's decimal: "5 km" rather than "5.0 km".
func radius(t *texts, sub alert.Subscription) string {
	return strings.Replace(t.Distance(float64(sub.RadiusM)), ".0 ", " ", 1)
}

func rise(t *texts, rate *float64) string {
	if rate == nil {
		return ""
	}
	return t.Rise(*rate)
}

func rainIn(f alert.Finding, i int) *float64 {
	return [3]*float64{f.Rain1h, f.Rain3h, f.Rain24h}[i]
}

// rainAmounts is a gauge's totals on one line, for map pins.
func rainAmounts(t *texts, f alert.Finding) string {
	var parts []string
	for i, window := range t.RainPinWindows {
		if mm := rainIn(f, i); mm != nil {
			parts = append(parts, t.RainPinAmount(t.Millimetres(*mm), window))
		}
	}
	return strings.Join(parts, ", ")
}

func regional(f alert.Finding, sub alert.Subscription) bool {
	return f.DistanceM > float64(sub.RadiusM)
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
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

// happeningText says what the measurements show, in words, from whatever set
// the risk. It interprets nothing beyond naming the threshold crossed.
func happeningText(t *texts, sub alert.Subscription, a alert.Assessment) string {
	switch {
	case a.Risk.Level == alert.RiskUnknown:
		return t.NoRecentData(esc(sub.Label))
	case len(a.Risk.Drivers) == 0:
		return t.NothingRaised(esc(sub.Label))
	}
	parts := make([]string, len(a.Risk.Drivers))
	for i, d := range a.Risk.Drivers {
		parts[i] = driverText(t, d, sub)
	}
	return strings.Join(parts, "\n")
}

func driverText(t *texts, f alert.Finding, sub alert.Subscription) string {
	station := t.StationAway(esc(t.stationName(f.Station)), t.Distance(f.DistanceM))
	prefix := ""
	if regional(f, sub) {
		prefix = t.RegionalWater
		if f.Rule == alert.RuleRain {
			prefix = t.RegionalRain
		}
		station += t.OutsideRadius(radius(t, sub))
	}

	switch f.Rule {
	case alert.RuleRain:
		if f.Held {
			return t.RainHeld
		}
		i := rainWindowIndex(f.RainWindow)
		mm := rainIn(f, i)
		threshold, ok := alert.RainThreshold(f.RainWindow, f.Severity)
		if mm == nil || !ok {
			return prefix + station + ": " + rainAmounts(t, f)
		}
		lastHour := ""
		if i != 0 && f.Rain1h != nil {
			lastHour = t.Millimetres(*f.Rain1h)
		}
		// Hysteresis can hold a severity while the total eases just under it.
		return prefix + t.RainRecorded(station, t.Millimetres(*mm), t.RainWindows[i], t.Millimetres(threshold), *mm >= threshold, lastHour)

	case alert.RuleWaterLevel, alert.RuleWaterRising:
		bank := t.BankSentence(f.LevelMSL, f.BankMSL)
		if f.Held {
			return prefix + t.WaterHeld(station, bank, t.Clock(f.At))
		}
		if f.Rule == alert.RuleWaterLevel {
			return prefix + t.WaterIs(station, bank, rise(t, f.RiseCmPerHour))
		}
		freeboard := f.BankMSL - f.LevelMSL
		if freeboard <= 0 || f.RiseCmPerHour == nil || *f.RiseCmPerHour <= 0 {
			return prefix + t.WaterStillRising(station, bank, rise(t, f.RiseCmPerHour))
		}
		return prefix + t.WaterRising(station, bank, rise(t, f.RiseCmPerHour), t.Duration(freeboard*100 / *f.RiseCmPerHour))
	}
	return ""
}

// trendReason explains the trend by the reading moving it.
func trendReason(t *texts, a alert.Assessment) string {
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
		return t.TrendUnknown
	case a.Risk.Trend == alert.TrendStable:
		return t.TrendSteady
	case f.Rule == "" && a.Risk.Trend == alert.TrendWorse:
		return t.TrendWorse
	case f.Rule == "":
		return t.TrendBetter
	}
	name := esc(t.stationName(f.Station))
	switch {
	case a.Risk.Trend == alert.TrendWorse && f.Rule == alert.RuleRain:
		return t.WorseRain(name, t.Millimetres(*f.Rain1h))
	case a.Risk.Trend == alert.TrendWorse:
		return t.WorseWater(name, rise(t, f.RiseCmPerHour))
	case f.Rule == alert.RuleRain:
		return t.BetterRain(name)
	default:
		return t.BetterWater(name, rise(t, f.RiseCmPerHour))
	}
}

// detailsText lists the measurements behind a risk, and nothing else.
func detailsText(t *texts, sub alert.Subscription, a alert.Assessment, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s</b>\n\n<b>%s</b>\n", t.Details, t.WaterHeading)

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

	label := func(f alert.Finding) string {
		return t.StationLabel(esc(t.stationName(f.Station)), t.Distance(f.DistanceM), regional(f, sub))
	}
	if len(order) == 0 {
		b.WriteString(t.NoWaterGauge(alert.FallbackRadiusM/1000) + "\n")
	}
	anyRegional := false
	for _, id := range order {
		s := stations[id]
		anyRegional = anyRegional || regional(s.stale, sub)
		b.WriteString(label(s.stale) + "\n")
		if s.stale.Severity > alert.SeverityNone || !s.level.Known {
			b.WriteString(t.NoDataSince(t.Clock(s.stale.At), t.Ago(s.stale.At, now)) + "\n\n")
			continue
		}
		b.WriteString(t.Reading(t.Bank(s.level.LevelMSL, s.level.BankMSL), rise(t, s.level.RiseCmPerHour)) + "\n")
		b.WriteString(t.Measured(t.Clock(s.level.At), t.Ago(s.level.At, now)) + "\n\n")
	}

	fmt.Fprintf(&b, "<b>%s</b>\n", t.RainHeading)
	switch {
	case rain.Known:
		anyRegional = anyRegional || regional(rain, sub)
		b.WriteString(label(rain) + "\n")
		for i, total := range t.RainTotals {
			if mm := rainIn(rain, i); mm != nil {
				fmt.Fprintf(&b, "%s: %s\n", total, t.Millimetres(*mm))
			}
		}
		b.WriteString(t.Measured(t.Clock(rain.At), t.Ago(rain.At, now)) + "\n")
		b.WriteString(t.WettestOf(rain.FreshGauges) + "\n")
	case rainStale.Known:
		b.WriteString(t.NoFreshRain + "\n")
	default:
		b.WriteString(t.NoRainGauge(alert.FallbackRadiusM/1000) + "\n")
	}
	if anyRegional {
		b.WriteString("\n" + t.RegionalNote(radius(t, sub)) + "\n")
	}

	fmt.Fprintf(&b, "\n<b>%s</b>\n%s\n", t.TrendHeading, trendReason(t, a))
	fmt.Fprintf(&b, "\n<b>%s</b>\n%s", t.SourcesHeading, sourcesText(t, a.Findings))
	return b.String()
}

// sourcesText names who runs the gauges behind each kind of reading, all of
// which currently arrive through ThaiWater.
func sourcesText(t *texts, findings []alert.Finding) string {
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
		lines = append(lines, t.WaterGauges+": "+strings.Join(water, ", "))
	}
	if len(rain) > 0 {
		lines = append(lines, t.RainGauges+": "+strings.Join(rain, ", "))
	}
	return strings.Join(append(lines, t.DataVia), "\n")
}

func footer(t *texts, findings []alert.Finding, checked time.Time) string {
	lines := []string{t.Checked(t.Clock(checked))}
	if slices.ContainsFunc(findings, usesBMAData) {
		lines = append(lines, bmaCredit)
	}
	return strings.Join(append(lines, "", t.Disclaimer), "\n")
}

// body is everything below the headline, in the same order for every state:
// what is happening, what to do, the flood map, the measurements, then
// sources, time and disclaimer.
func body(t *texts, sub alert.Subscription, a alert.Assessment, checked time.Time, withMap bool) string {
	var b strings.Builder
	b.WriteString(happeningText(t, sub, a) + "\n\n")
	b.WriteString("<b>" + t.WhatToDo + "</b>\n")
	for _, step := range t.Actions[a.Risk.Level] {
		b.WriteString("• " + step + "\n")
	}
	if withMap {
		b.WriteString("\n" + t.FloodRoads + "\n")
	}
	b.WriteString("\n<blockquote expandable>" + detailsText(t, sub, a, checked) + "</blockquote>\n")
	b.WriteString(footer(t, a.Findings, checked))
	return b.String()
}

func withTrend(t *texts, head string, trend alert.Trend) string {
	if s := t.Trends[trend]; s != "" {
		return head + " · " + s
	}
	return head
}

// statusText is the full picture for one place.
func statusText(t *texts, sub alert.Subscription, a alert.Assessment, checked time.Time) string {
	head := withTrend(t, t.Headline(riskIcons[a.Risk.Level], t.RiskNames[a.Risk.Level], esc(sub.Label)), a.Risk.Trend)
	return head + "\n\n" + body(t, sub, a, checked, true)
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.In(ict).Date()
	by, bm, bd := b.In(ict).Date()
	return ay == by && am == bm && ad == bd
}

// offlineText owns up to a stretch when floodwatch could not have warned a
// subscriber, and says where each of their places stands now.
func offlineText(t *texts, from, to time.Time, places []alert.Digest) string {
	var b strings.Builder
	b.WriteString(t.Offline(t.When(from, to), t.When(to, to), t.Duration(to.Sub(from).Hours())))
	b.WriteString("\n\n" + t.OfflinePlaces + "\n")
	for _, d := range places {
		line := fmt.Sprintf("%s <b>%s</b>: %s", riskIcons[d.Risk.Level], esc(d.Subscription.Label), t.RiskNames[d.Risk.Level])
		b.WriteString(withTrend(t, line, d.Risk.Trend) + "\n")
	}
	b.WriteString("\n" + t.OfflineStatus)
	return b.String()
}

// digestText tells a subscriber that a place's overall risk changed, with the
// same picture a status gives.
func digestText(t *texts, d alert.Digest, checked time.Time) string {
	sub, r := d.Subscription, d.Risk
	var head string
	switch r.Level {
	case alert.SeverityNone:
		head = t.BackToLow(esc(sub.Label))
	case alert.RiskUnknown:
		head = t.NoDataHead(esc(sub.Label))
	default:
		head = withTrend(t, t.Transition(riskIcons[r.Level], esc(sub.Label), t.RiskNames[d.From], t.RiskNames[r.Level]), r.Trend)
	}
	return head + "\n\n" + body(t, sub, d.Assessment, checked, r.Level != alert.SeverityNone)
}
