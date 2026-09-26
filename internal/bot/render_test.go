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

// The point of the status: verdict, reason and action first, and the numbers
// behind them tucked away for whoever wants them.
func TestStatusLeadsWithTheVerdict(t *testing.T) {
	text := statusText(home, sampleAssessment(), checked)
	inOrder(t, text,
		"🟡 <b>WATCH</b> around <b>Home</b> · ↘ improving",
		"Heavy rain over the last 24 hours at Krung Thep 3 (4.9 km): 0.5 mm in 1 h, 124 mm in 24 h.",
		"<b>Keep an eye on it",
		floodRoadsLine,
		"<blockquote expandable><b>Details</b>",
		"🟢 Chao Phraya 15 (5.4 km): 1.82 m below the bank at 13:30, 50 min ago",
		"Wettest of 7 gauges reporting nearby.",
		"Sources: water gauges by HII; rain gauges by HII; all via ThaiWater (HII).",
		"</blockquote>",
		"Checked 14:20.",
		disclaimer,
	)
	if strings.Contains(text, bmaCredit) {
		t.Errorf("BMA credited though only HII gauges were used:\n%s", text)
	}
}

func TestStatusWithNothingRaised(t *testing.T) {
	a := sampleAssessment()
	a.Risk = alert.Risk{Level: alert.SeverityNone, Trend: alert.TrendStable}
	inOrder(t, statusText(home, a, checked),
		"🟢 <b>LOW</b> around <b>Home</b> · → steady",
		"Nothing near Home is at a warning level.",
		"<b>Nothing to do right now.</b>",
	)
}

func TestStatusWithoutWaterGauges(t *testing.T) {
	a := sampleAssessment()
	a.Findings = a.Findings[3:]
	if text := statusText(home, a, checked); !strings.Contains(text, "No water level gauge within 10 km.") {
		t.Errorf("missing coverage note:\n%s", text)
	}
}

func TestStatusCreditsBMAGaugesRepublishedByThaiWater(t *testing.T) {
	a := sampleAssessment()
	// Not the wettest gauge, but among those judged, which is still using it.
	a.Findings[3].Agencies = []string{"HII", "BMA"}
	text := statusText(home, a, checked)
	if !strings.Contains(text, bmaCredit) || !strings.Contains(text, "rain gauges by HII, BMA") {
		t.Errorf("BMA not credited though its gauges were used:\n%s", text)
	}
}

func waterDriver(rule alert.Rule, level, rate, km float64) alert.Finding {
	return alert.Finding{
		Key: alert.Key{StationID: 3, Rule: rule}, Known: true, Station: alert.Station{Name: "Khlong Lat Bang Yo 1 Gate"},
		DistanceM: km * 1000, At: time.Date(2026, 9, 26, 14, 10, 0, 0, ict), LevelMSL: level, BankMSL: 1.51, RiseCmPerHour: ptr(rate),
	}
}

func TestReasons(t *testing.T) {
	held := waterDriver(alert.RuleWaterLevel, 1.41, 0, 3)
	held.Known, held.Held = false, true
	cases := []struct {
		name   string
		driver alert.Finding
		want   string
	}{
		{"near the bank", waterDriver(alert.RuleWaterLevel, 1.36, 3, 3),
			"Khlong Lat Bang Yo 1 Gate (3.0 km) is 0.15 m below the bank, rising 3 cm/h."},
		{"rising fast", waterDriver(alert.RuleWaterRising, 1.21, 20, 3),
			"Khlong Lat Bang Yo 1 Gate (3.0 km) is 0.30 m below the bank and rising 20 cm/h; at this rate it reaches the bank in about 2 hours."},
		{"over the bank and rising", waterDriver(alert.RuleWaterRising, 1.61, 8, 3),
			"Khlong Lat Bang Yo 1 Gate (3.0 km) is 0.10 m above the bank and still rising 8 cm/h."},
		{"beyond the radius", waterDriver(alert.RuleWaterLevel, 1.61, 0, 7.4),
			"Khlong Lat Bang Yo 1 Gate (7.4 km, outside your 5.0 km radius) is 0.10 m above the bank, steady."},
		{"gone quiet", held,
			"Khlong Lat Bang Yo 1 Gate (3.0 km) was 0.10 m below the bank when it last reported at 14:10, and has been silent since."},
	}
	for _, c := range cases {
		a := alert.Assessment{Risk: alert.Risk{Level: alert.SeverityWarning, Drivers: []alert.Finding{c.driver}}}
		if got := reasonText(home, a); got != c.want {
			t.Errorf("%s:\n got  %q\n want %q", c.name, got, c.want)
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
	worse := digestText(digest(alert.SeverityWatch, alert.SeverityWarning, alert.TrendWorse), checked)
	inOrder(t, worse,
		"🟠 <b>Home: WATCH → WARNING</b> · ↗ getting worse",
		"rising 20 cm/h; at this rate it reaches the bank in about 2 hours.",
		"<b>Move your car to higher ground",
		floodRoadsLine,
		"Send /status for the full picture.",
		disclaimer,
	)

	clear := digestText(digest(alert.SeverityWarning, alert.SeverityNone, alert.TrendBetter), checked)
	inOrder(t, clear, "✅ <b>Home: back to LOW</b>", "Nothing near Home is at a warning level.")
	if strings.Contains(clear, floodRoadsLine) || strings.Contains(clear, "Move your car") {
		t.Errorf("an all clear carries warnings:\n%s", clear)
	}

	quiet := digestText(digest(alert.SeverityWatch, alert.RiskUnknown, alert.TrendUnknown), checked)
	inOrder(t, quiet, "⚪ <b>Home: no fresh readings</b>", "No gauge near Home has reported recently.", "Check official BMA updates")
}

func TestNamesAreEscaped(t *testing.T) {
	d := digest(alert.SeverityNone, alert.SeverityWarning, alert.TrendWorse)
	d.Subscription.Label = "Mum & Dad's"
	d.Risk.Drivers[0].Station.Name = "Gate <A&B>"
	text := digestText(d, checked)
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
		if got := ago(checked.Add(-d), checked); got != want {
			t.Errorf("ago(%v) = %q, want %q", d, got, want)
		}
	}
}
