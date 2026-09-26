package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/FreddieTheObserver/floodwatch/internal/alert"
)

func TestDigestPutsWorstNewsFirst(t *testing.T) {
	bank := 1.26
	gate := alert.Station{ID: 3, Source: "thaiwater", Name: "Wat Bangkachao Nok Gate", BankMSL: &bank}
	at := time.Date(2026, 9, 26, 14, 10, 0, 0, ict)
	d := alert.Digest{
		Subscription: alert.Subscription{Label: "Home"},
		Changes: []alert.Change{
			{From: alert.SeverityWatch, Finding: alert.Finding{Key: alert.Key{Rule: alert.RuleRain}, Known: true,
				Station: alert.Station{Name: "Krung Thep 3"}, DistanceM: 3100, At: at, Rain1h: ptr(2)}},
			{From: alert.SeverityWatch, Finding: alert.Finding{Key: alert.Key{StationID: 3, Rule: alert.RuleWaterLevel}, Known: true,
				Severity: alert.SeveritySevere, Station: gate, DistanceM: 7800, At: at, LevelMSL: 1.40, BankMSL: bank, RiseCmPerHour: ptr(12)}},
			{Finding: alert.Finding{Key: alert.Key{StationID: 3, Rule: alert.RuleWaterStale}, Known: true,
				Severity: alert.SeverityWatch, Station: gate, DistanceM: 7800, At: at}},
		},
	}

	text := digestText(d, checked)
	overflow := strings.Index(text, "Overflowing")
	stale := strings.Index(text, "No data")
	eased := strings.Index(text, "Rain has eased")
	if overflow < 0 || stale < 0 || eased < 0 || !(overflow < stale && stale < eased) {
		t.Fatalf("order wrong or missing lines:\n%s", text)
	}
	if !strings.Contains(text, "0.14 m above the bank, rising 12 cm/h at 14:10") {
		t.Errorf("overflow detail missing:\n%s", text)
	}
	if strings.Contains(text, bmaCredit) {
		t.Errorf("BMA credited without any BMA data:\n%s", text)
	}
	if link, footer := strings.Index(text, floodRoadsLine), strings.Index(text, "Readings from"); link < 0 || link > footer {
		t.Errorf("worsening news lacks the flooded roads link above the footer:\n%s", text)
	}
}

func TestAllClearDigestHasNoRoadsLink(t *testing.T) {
	d := alert.Digest{
		Subscription: alert.Subscription{Label: "Home"},
		Changes: []alert.Change{{From: alert.SeverityWatch, Finding: alert.Finding{Key: alert.Key{Rule: alert.RuleRain}, Known: true,
			Station: alert.Station{Name: "Krung Thep 3"}, DistanceM: 3100, Rain1h: ptr(2)}}},
	}
	if text := digestText(d, checked); strings.Contains(text, "flood-alert.html") {
		t.Errorf("an all clear sends people to the flood map:\n%s", text)
	}
}

func TestDigestCreditsBMAWhenItsDataIsUsed(t *testing.T) {
	d := alert.Digest{
		Subscription: alert.Subscription{Label: "Home"},
		Changes: []alert.Change{{Finding: alert.Finding{Key: alert.Key{Rule: alert.RuleRain}, Known: true, Severity: alert.SeverityWatch,
			Station: alert.Station{Source: "bma", Name: "Thung Khru District Office"}, DistanceM: 4700, Rain1h: ptr(25)}}},
	}
	if text := digestText(d, checked); !strings.Contains(text, bmaCredit) {
		t.Errorf("no BMA credit:\n%s", text)
	}
}

func TestStatusCreditsBMAGaugesRepublishedByThaiWater(t *testing.T) {
	findings := sampleFindings()
	if text := statusText(alert.Subscription{Label: "Home"}, findings, checked); strings.Contains(text, bmaCredit) {
		t.Errorf("BMA credited though only HII gauges were used:\n%s", text)
	}
	// Not the wettest gauge, but among those judged, which is still using it.
	findings[3].Agencies = []string{"HII", "BMA"}
	if text := statusText(alert.Subscription{Label: "Home"}, findings, checked); !strings.Contains(text, bmaCredit) {
		t.Errorf("no BMA credit though BMA gauges were used:\n%s", text)
	}
}

func TestNamesAreEscaped(t *testing.T) {
	d := alert.Digest{
		Subscription: alert.Subscription{Label: "Mum & Dad's"},
		Changes: []alert.Change{{Finding: alert.Finding{Key: alert.Key{Rule: alert.RuleWaterStale}, Known: true, Severity: 1,
			Station: alert.Station{Name: "Gate <A&B>"}}}},
	}
	text := digestText(d, checked)
	if strings.Contains(text, "<A&B>") || !strings.Contains(text, "Gate &lt;A&amp;B&gt;") || !strings.Contains(text, "Mum &amp; Dad&#39;s") {
		t.Errorf("unescaped names:\n%s", text)
	}
}

func TestRisingDetail(t *testing.T) {
	f := alert.Finding{LevelMSL: 1.86, BankMSL: 2.16, RiseCmPerHour: ptr(20)}
	if got, want := risingDetail(f), "Rising 20 cm/h, 0.30 m below the bank; at this rate it reaches the bank in about 2 hours"; got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	over := alert.Finding{LevelMSL: 2.30, BankMSL: 2.16, RiseCmPerHour: ptr(8)}
	if got, want := risingDetail(over), "Rising 8 cm/h while 0.14 m above the bank"; got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestStatusText(t *testing.T) {
	text := statusText(alert.Subscription{Label: "Home"}, sampleFindings(), checked)
	for _, want := range []string{
		"📍 <b>Home</b>",
		"🟢 Chao Phraya 15 (5.4 km): 1.82 m below the bank at 13:30",
		"🟡 Heavy rain over the last 24 hours: 0.5 mm in 1 h, 124 mm in 24 h at Krung Thep 3 (4.9 km), 13:30",
		"Wettest of 7 gauges reporting nearby.",
		floodRoadsLine,
		"checked 14:20",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}

func TestStatusTextWithoutWaterGauges(t *testing.T) {
	findings := sampleFindings()[3:]
	if text := statusText(alert.Subscription{Label: "Home"}, findings, checked); !strings.Contains(text, "No water level gauge within 10 km.") {
		t.Errorf("missing coverage note:\n%s", text)
	}
}
