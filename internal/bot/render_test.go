package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/FreddieTheObserver/floodwatch/internal/alert"
)

var home = alert.Subscription{ID: 1, Label: "Home", RadiusM: defaultRadiusM}

func inOrder(t *testing.T, text string, parts ...string) {
	t.Helper()
	last := -1
	for _, p := range parts {
		i := strings.Index(text, p)
		if i < 0 {
			t.Errorf("missing %q in:\n%s", p, text)
			return
		}
		if i < last {
			t.Errorf("%q is out of order in:\n%s", p, text)
			return
		}
		last = i
	}
}

func details(text string) string {
	start := strings.Index(text, "<blockquote expandable>")
	end := strings.Index(text, "</blockquote>")
	if start < 0 || end < start {
		return ""
	}
	return text[start:end]
}

func TestStatusFollowsTheAgreedOrder(t *testing.T) {
	text := statusText(&english, home, sampleAssessment(), checked)
	inOrder(t, text,
		"🟡 <b>WATCH</b> around <b>Home</b> · ↘ improving",
		"Krung Thep 3, 4.9 km away, recorded 124 mm over the last 24 hours, above FloodWatch's 90 mm threshold, with 0.5 mm in the last hour.",
		"<b>What to do</b>\n• Keep an eye on updates.\n• Check the flood map before driving.",
		english.FloodRoads,
		"<blockquote expandable><b>Details</b>",
		"<b>WATER</b>\nChao Phraya 15 (5.4 km, regional)\n1.82 m below bank\nMeasured 13:30 · 50 min ago",
		"<b>RAIN</b>\nKrung Thep 3 (4.9 km)\n1 hour: 0.5 mm\n24 hours: 124 mm\nMeasured 13:30 · 50 min ago\nWettest of 7 nearby gauges reporting.",
		"Stations marked regional are outside your 5 km radius.",
		"<b>TREND</b>\nImproving because rain at Krung Thep 3 has eased.",
		"<b>SOURCES</b>\nWater gauges: HII\nRain gauges: HII\nData: ThaiWater (HII)",
		"</blockquote>",
		"Checked 14:20",
		english.Disclaimer,
	)
	if strings.Contains(text, bmaCredit) {
		t.Errorf("BMA credited though only HII gauges were used:\n%s", text)
	}
}

// Measurement and interpretation stay apart: the details carry numbers and
// times only, and every verdict lives above them.
func TestDetailsHoldMeasurementsOnly(t *testing.T) {
	d := details(statusText(&english, home, sampleAssessment(), checked))
	if d == "" {
		t.Fatal("no details section")
	}
	for _, verdict := range []string{"🟢", "🟡", "🟠", "🔴", "Heavy rain", "WATCH", "WARNING", "threshold"} {
		if strings.Contains(d, verdict) {
			t.Errorf("details contain the interpretation %q:\n%s", verdict, d)
		}
	}
}

func TestEveryStateKeepsTheSameShape(t *testing.T) {
	for level := alert.SeverityNone; level <= alert.RiskUnknown; level++ {
		a := sampleAssessment()
		a.Risk = alert.Risk{Level: level, Trend: alert.TrendStable}
		if level > alert.SeverityNone && level < alert.RiskUnknown {
			a.Risk.Drivers = []alert.Finding{waterDriver(alert.RuleWaterLevel, 1.36, 3, 3)}
		}
		inOrder(t, statusText(&english, home, a, checked),
			english.RiskNames[level], "<b>What to do</b>", english.FloodRoads, "<b>Details</b>", "<b>SOURCES</b>", "Checked 14:20", english.Disclaimer)
	}
}

func TestStatusWithoutWaterGauges(t *testing.T) {
	a := sampleAssessment()
	a.Findings = a.Findings[3:]
	if text := statusText(&english, home, a, checked); !strings.Contains(text, "No water level gauge within 10 km.") {
		t.Errorf("missing coverage note:\n%s", text)
	}
}

func TestStatusCreditsBMAGaugesRepublishedByThaiWater(t *testing.T) {
	a := sampleAssessment()
	// Not the wettest gauge, but among those judged, which is still using it.
	a.Findings[3].Agencies = []string{"HII", "BMA"}
	text := statusText(&english, home, a, checked)
	if !strings.Contains(text, bmaCredit) || !strings.Contains(text, "Rain gauges: HII, BMA") {
		t.Errorf("BMA not credited though its gauges were used:\n%s", text)
	}
}

func waterDriver(rule alert.Rule, level, rate, km float64) alert.Finding {
	return alert.Finding{
		Key: alert.Key{StationID: 3, Rule: rule}, Known: true, Station: alert.Station{Name: "Khlong Lat Bang Yo 1 Gate"},
		DistanceM: km * 1000, At: time.Date(2026, 9, 26, 14, 10, 0, 0, ict), LevelMSL: level, BankMSL: 1.51, RiseCmPerHour: ptr(rate),
	}
}

func rainDriver(name string, km float64, window time.Duration, severity int, mm1h, mm24h *float64) alert.Finding {
	return alert.Finding{
		Key: alert.Key{Rule: alert.RuleRain}, Known: true, Severity: severity, Station: alert.Station{Name: name},
		DistanceM: km * 1000, RainWindow: window, Rain1h: mm1h, Rain24h: mm24h,
	}
}

func TestWhatIsHappening(t *testing.T) {
	held := waterDriver(alert.RuleWaterLevel, 1.41, 0, 3)
	held.Known, held.Held = false, true
	cases := []struct {
		name   string
		driver alert.Finding
		want   string
	}{
		{"near the bank", waterDriver(alert.RuleWaterLevel, 1.36, 3, 3),
			"Khlong Lat Bang Yo 1 Gate, 3.0 km away, is 0.15 m below its bank and rising 3 cm/h."},
		{"rising fast", waterDriver(alert.RuleWaterRising, 1.21, 20, 3),
			"Khlong Lat Bang Yo 1 Gate, 3.0 km away, is 0.30 m below its bank and rising 20 cm/h; at this rate it reaches the bank in about 2 hours."},
		{"over the bank and rising", waterDriver(alert.RuleWaterRising, 1.61, 8, 3),
			"Khlong Lat Bang Yo 1 Gate, 3.0 km away, is 0.10 m above its bank and still rising 8 cm/h."},
		// The reading of 26 September 2026, 17:40.
		{"regional", waterDriver(alert.RuleWaterRising, 2.07, 26, 6.6),
			"Regional: Khlong Lat Bang Yo 1 Gate, 6.6 km away (outside your 5 km radius), is 0.56 m above its bank and still rising 26 cm/h."},
		{"gone quiet", held,
			"Khlong Lat Bang Yo 1 Gate, 3.0 km away, was 0.10 m below its bank when it last reported at 14:10 and has been silent since."},
		// The day's total set the level while the last hour was dry, which must
		// not read as heavy rain falling now.
		{"a wet day, a dry hour", rainDriver("ส.วัดไทร", 4.7, 24*time.Hour, alert.SeverityWatch, ptr(0), ptr(96.5)),
			"ส.วัดไทร, 4.7 km away, recorded 96.5 mm over the last 24 hours, above FloodWatch's 90 mm threshold, with 0 mm in the last hour."},
		{"a downpour", rainDriver("Krung Thep 3", 3.1, time.Hour, alert.SeverityWarning, ptr(45), ptr(80)),
			"Krung Thep 3, 3.1 km away, recorded 45 mm over the last hour, above FloodWatch's 40 mm threshold."},
	}
	for _, c := range cases {
		a := alert.Assessment{Risk: alert.Risk{Level: alert.SeverityWarning, Drivers: []alert.Finding{c.driver}}}
		if got := happeningText(&english, home, a); got != c.want {
			t.Errorf("%s:\n got  %q\n want %q", c.name, got, c.want)
		}
	}
}

func TestTrendReason(t *testing.T) {
	rising := waterDriver(alert.RuleWaterRising, 2.07, 26, 6.6)
	cases := []struct {
		risk alert.Risk
		want string
	}{
		{alert.Risk{Level: alert.SeverityWarning, Trend: alert.TrendWorse, Drivers: []alert.Finding{rising}},
			"Getting worse because water at Khlong Lat Bang Yo 1 Gate is rising 26 cm/h."},
		{alert.Risk{Level: alert.SeverityWatch, Trend: alert.TrendStable},
			"Steady: no significant change over the last hour."},
		{alert.Risk{Level: alert.RiskUnknown, Trend: alert.TrendUnknown},
			"Unknown: there are no fresh readings to judge by."},
	}
	for _, c := range cases {
		if got := trendReason(&english, alert.Assessment{Risk: c.risk}); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

func digest(from, to int, trend alert.Trend) alert.Digest {
	a := sampleAssessment()
	a.Risk = alert.Risk{Level: to, Trend: trend}
	if to != alert.SeverityNone && to != alert.RiskUnknown {
		a.Risk.Drivers = []alert.Finding{waterDriver(alert.RuleWaterRising, 1.21, 20, 3)}
	}
	return alert.Digest{Subscription: home, Assessment: a, From: from}
}

func TestDigests(t *testing.T) {
	worse := digestText(&english, digest(alert.SeverityWatch, alert.SeverityWarning, alert.TrendWorse), checked)
	inOrder(t, worse,
		"🟠 <b>Home: WATCH → WARNING</b> · ↗ getting worse",
		"rising 20 cm/h; at this rate it reaches the bank in about 2 hours.",
		"<b>What to do</b>\n• Move your car to higher ground.\n• Move valuables off the floor.\n• Avoid low roads.",
		english.FloodRoads,
		"<b>Details</b>",
		english.Disclaimer,
	)

	clear := digestText(&english, digest(alert.SeverityWarning, alert.SeverityNone, alert.TrendBetter), checked)
	inOrder(t, clear, "✅ <b>Home: back to LOW</b>", "Nothing near Home is at a warning level.", "• Nothing to do right now.")
	if strings.Contains(clear, english.FloodRoads) {
		t.Errorf("an all clear sends people to the flood map:\n%s", clear)
	}

	quiet := digestText(&english, digest(alert.SeverityWatch, alert.RiskUnknown, alert.TrendUnknown), checked)
	inOrder(t, quiet, "⚪ <b>Home: NO DATA</b>", "No gauge near Home has reported in the last few hours.", "• Check official BMA updates.")
}

func TestNamesAreEscaped(t *testing.T) {
	d := digest(alert.SeverityNone, alert.SeverityWarning, alert.TrendWorse)
	d.Subscription.Label = "Mum & Dad's"
	d.Risk.Drivers[0].Station.Name = "Gate <A&B>"
	text := digestText(&english, d, checked)
	if strings.Contains(text, "<A&B>") || !strings.Contains(text, "Gate &lt;A&amp;B&gt;") || !strings.Contains(text, "Mum &amp; Dad&#39;s") {
		t.Errorf("unescaped names:\n%s", text)
	}
}

func TestAgo(t *testing.T) {
	for d, want := range map[time.Duration]string{
		20 * time.Second:             "just now",
		50 * time.Minute:             "50 min ago",
		80 * time.Minute:             "1 h 20 min ago",
		2 * time.Hour:                "2 h ago",
		4*time.Hour + 10*time.Minute: "4 h ago",
	} {
		if got := english.Ago(checked.Add(-d), checked); got != want {
			t.Errorf("ago(%v) = %q, want %q", d, got, want)
		}
	}
}
