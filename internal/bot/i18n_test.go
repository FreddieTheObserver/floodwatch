package bot

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/FreddieTheObserver/floodwatch/internal/alert"
	"github.com/FreddieTheObserver/floodwatch/internal/telegram"
)

// No language may leave a phrase out, or its readers would get a message with
// a hole in it, or English halfway through.
func TestEveryLanguageIsComplete(t *testing.T) {
	for code, lang := range languages {
		v := reflect.ValueOf(*lang)
		for i := range v.NumField() {
			name, f := v.Type().Field(i).Name, v.Field(i)
			switch f.Kind() {
			case reflect.String:
				if f.String() == "" {
					t.Errorf("%s: %s is empty", code, name)
				}
			case reflect.Func, reflect.Slice:
				if f.IsNil() || (f.Kind() == reflect.Slice && f.Len() == 0) {
					t.Errorf("%s: %s is missing", code, name)
				}
			case reflect.Array:
				for j := range f.Len() {
					e := f.Index(j)
					// The unknown trend is shown as nothing, deliberately.
					if name == "Trends" && j == int(alert.TrendUnknown) {
						continue
					}
					if (e.Kind() == reflect.String && e.String() == "") || (e.Kind() == reflect.Slice && e.Len() == 0) {
						t.Errorf("%s: %s[%d] is empty", code, name, j)
					}
				}
			}
		}
	}
}

func TestEveryLanguageOffersTheSameCommands(t *testing.T) {
	names := func(cs []telegram.Command) (out []string) {
		for _, c := range cs {
			out = append(out, c.Command)
		}
		return out
	}
	if en, th := names(english.Commands), names(thai.Commands); !slices.Equal(en, th) {
		t.Errorf("commands differ: en %v, th %v", en, th)
	}
}

func (h *harness) textFrom(s, languageCode string) {
	h.bot.handle(context.Background(), telegram.Update{Message: &telegram.Message{
		Chat: telegram.Chat{ID: me, Type: "private"}, Text: s, From: &telegram.User{ID: me, LanguageCode: languageCode},
	}})
}

func TestLanguageFollowsTheApp(t *testing.T) {
	for code, want := range map[string]*texts{"th": &thai, "th-TH": &thai, "en": &english, "my": &english, "": &english} {
		h := newHarness()
		h.textFrom("/start", code)
		if got := h.api.last(t).Text; !strings.HasPrefix(got, want.Welcome) {
			t.Errorf("app language %q got the wrong welcome:\n%s", code, got)
		}
		// Remembered, since alerts arrive with no app language to go by.
		if h.store.languages["42"] != want.Code {
			t.Errorf("app language %q stored %q, want %q", code, h.store.languages["42"], want.Code)
		}
	}
}

func TestLanguageCommandSwitches(t *testing.T) {
	h := newHarness()
	h.textFrom("/language", "en")
	if got := buttons(t, h.api.last(t)); strings.Join(got, " ") != "lang:th lang:en" {
		t.Fatalf("buttons = %v", got)
	}
	h.tap("lang:th")
	if h.store.languages["42"] != "th" || h.api.edits[len(h.api.edits)-1] != thai.LanguageSet {
		t.Fatalf("after choosing Thai: stored %q, edit %q", h.store.languages["42"], h.api.edits)
	}
	// The app is still English; the choice made in the bot wins.
	h.textFrom("/places", "en")
	if got := h.api.last(t).Text; got != thai.NoPlaces {
		t.Errorf("/places after choosing Thai = %q", got)
	}
}

func TestAlertsUseTheRecipientsLanguage(t *testing.T) {
	h := newHarness()
	h.store.languages = map[string]string{"42": "th"}
	h.eval.pending = []alert.Digest{digest(alert.SeverityWatch, alert.SeverityWarning, alert.TrendWorse)}
	h.eval.pending[0].Subscription.Recipient = "42"
	if err := h.bot.Notify(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := h.api.last(t).Text; !strings.Contains(got, "<b>Home: เฝ้าระวัง → เตือนภัย</b> · ↗ สถานการณ์แย่ลง") {
		t.Errorf("alert to a Thai reader:\n%s", got)
	}
}

func thaiAssessment() alert.Assessment {
	a := sampleAssessment()
	for i := range a.Findings {
		switch a.Findings[i].Station.Name {
		case "Chao Phraya 15":
			a.Findings[i].Station.NameTH = "เจ้าพระยา 15"
		case "Krung Thep 3":
			a.Findings[i].Station.NameTH = "กรุงเทพ 3"
		}
	}
	a.Risk = alert.Overall(a.Findings, defaultRadiusM)
	return a
}

func TestThaiStatus(t *testing.T) {
	text := statusText(&thai, home, thaiAssessment(), checked)
	inOrder(t, text,
		"🟡 <b>เฝ้าระวัง</b> บริเวณ <b>Home</b> · ↘ สถานการณ์ดีขึ้น",
		"กรุงเทพ 3 ห่าง 4.9 กม. วัดปริมาณฝนได้ 124 มม. ใน 24 ชั่วโมงที่ผ่านมา เกินเกณฑ์ 90 มม. ของ FloodWatch และ 0.5 มม. ในชั่วโมงที่ผ่านมา",
		"<b>สิ่งที่ควรทำ</b>\n• ติดตามข่าวสารอย่างต่อเนื่อง",
		thai.FloodRoads,
		"<blockquote expandable><b>รายละเอียด</b>",
		"<b>ระดับน้ำ</b>\nเจ้าพระยา 15 (5.4 กม., นอกรัศมี)\nต่ำกว่าตลิ่ง 1.82 ม.\nวัดเมื่อ 13:30 น. · 50 นาทีที่แล้ว",
		"<b>ฝน</b>\nกรุงเทพ 3 (4.9 กม.)\n1 ชั่วโมง: 0.5 มม.\n24 ชั่วโมง: 124 มม.",
		"มีปริมาณฝนสูงสุดจาก 7 สถานีใกล้เคียงที่ส่งข้อมูล",
		"สถานีที่อยู่นอกรัศมี 5 กม. จากพื้นที่ของคุณ",
		"<b>แนวโน้ม</b>\nสถานการณ์ดีขึ้นเพราะฝนที่ กรุงเทพ 3 เบาลง",
		"<b>แหล่งข้อมูล</b>\nสถานีวัดระดับน้ำ: HII\nสถานีวัดฝน: HII\nแหล่งข้อมูล: ThaiWater (สสน.)",
		"ตรวจสอบล่าสุด 14:20 น.",
		thai.Disclaimer,
	)
	// Nothing may fall back to English. Station codes and agency names stay as
	// the sources publish them, so look for English wording only.
	for _, english := range []string{"What to do", "Details", "Measured", "around", "away", "threshold", "Checked", "min ago"} {
		if strings.Contains(text, english) {
			t.Errorf("Thai status contains the English %q:\n%s", english, text)
		}
	}
}

func TestEveryStateKeepsTheSameShapeInThai(t *testing.T) {
	for level := alert.SeverityNone; level <= alert.RiskUnknown; level++ {
		a := thaiAssessment()
		a.Risk = alert.Risk{Level: level, Trend: alert.TrendStable}
		if level > alert.SeverityNone && level < alert.RiskUnknown {
			a.Risk.Drivers = []alert.Finding{waterDriver(alert.RuleWaterLevel, 1.36, 3, 3)}
		}
		inOrder(t, statusText(&thai, home, a, checked),
			thai.RiskNames[level], thai.WhatToDo, thai.FloodRoads, thai.Details, thai.SourcesHeading, "ตรวจสอบล่าสุด", thai.Disclaimer)
	}
}

func TestThaiRename(t *testing.T) {
	h := newHarness()
	h.store.languages = map[string]string{"42": "th"}
	h.location(13.6515, 100.4945)
	if h.store.subs[0].Label != "บ้าน" {
		t.Fatalf("first Thai place = %q, want บ้าน", h.store.subs[0].Label)
	}
	h.tap("add:13.70000,100.49000")
	prompt := h.api.last(t)
	if !strings.HasPrefix(prompt.Text, "✏️ <b>สถานที่ 2</b>\n") {
		t.Fatalf("Thai naming question = %q", prompt.Text)
	}
	h.replyTo(prompt, "ที่ทำงาน")
	if h.store.subs[1].Label != "ที่ทำงาน" || !strings.Contains(h.api.last(t).Text, "เปลี่ยนชื่อ <b>สถานที่ 2</b> เป็น <b>ที่ทำงาน</b> แล้ว") {
		t.Errorf("label %q, reply %q", h.store.subs[1].Label, h.api.last(t).Text)
	}
}

// Naming questions already sitting in chats were asked before translation,
// in the old wording, and must still work when answered.
func TestAnsweringAnOldNamingQuestion(t *testing.T) {
	h := newHarness()
	h.location(13.6515, 100.4945)
	h.tap("add:13.70000,100.49000")
	old := telegram.OutgoingMessage{Text: "What should I call <b>Place 2</b>? Reply with a name, like Office or Mum's house."}
	h.replyTo(old, "Office")
	if h.store.subs[1].Label != "Office" {
		t.Errorf("label = %q", h.store.subs[1].Label)
	}
}

func TestStopForgetsTheLanguage(t *testing.T) {
	h := newHarness()
	h.textFrom("/stop", "th")
	h.tap("stop:yes")
	if _, ok := h.store.languages["42"]; ok {
		t.Error("the language outlived /stop")
	}
}

// A distant rain gauge must not be introduced as water-level information,
// which is what the Thai for a distant water gauge says.
func TestThaiRegionalPrefixNamesTheKindOfReading(t *testing.T) {
	water := waterDriver(alert.RuleWaterRising, 2.07, 26, 6.6)
	rain := rainDriver("ส.วัดไทร", 7.2, 24*time.Hour, alert.SeverityWatch, ptr(0), ptr(96.5))
	cases := map[string]alert.Finding{
		"ข้อมูลระดับน้ำในพื้นที่: Khlong Lat Bang Yo 1 Gate ห่าง 6.6 กม. (นอกรัศมี 5 กม. จากพื้นที่ของคุณ) ระดับน้ำสูงกว่าตลิ่ง 0.56 ม. และยังเพิ่มขึ้น 26 ซม./ชม.":                              water,
		"ข้อมูลฝนในพื้นที่: ส.วัดไทร ห่าง 7.2 กม. (นอกรัศมี 5 กม. จากพื้นที่ของคุณ) วัดปริมาณฝนได้ 96.5 มม. ใน 24 ชั่วโมงที่ผ่านมา เกินเกณฑ์ 90 มม. ของ FloodWatch และ 0 มม. ในชั่วโมงที่ผ่านมา": rain,
	}
	for want, driver := range cases {
		a := alert.Assessment{Risk: alert.Risk{Level: alert.SeverityWarning, Drivers: []alert.Finding{driver}}}
		if got := happeningText(&thai, home, a); got != want {
			t.Errorf("\n got  %q\n want %q", got, want)
		}
	}
}

func TestThaiRunsStraightOnIntoAThaiName(t *testing.T) {
	for label, want := range map[string]string{
		"บ้าน":         "🟢 <b>ปกติ</b> บริเวณ<b>บ้าน</b>",
		"Elio Del Ray": "🟢 <b>ปกติ</b> บริเวณ <b>Elio Del Ray</b>",
		"3rd floor":    "🟢 <b>ปกติ</b> บริเวณ <b>3rd floor</b>",
	} {
		if got := thai.Headline("🟢", "ปกติ", label); got != want {
			t.Errorf("headline for %q = %q, want %q", label, got, want)
		}
	}
}

func TestThaiPlaceNamesInSentences(t *testing.T) {
	for label, want := range map[string][2]string{
		"บ้าน": {
			"ระดับน้ำและปริมาณฝนจากสถานีวัดใกล้บ้านยังไม่ถึงเกณฑ์เฝ้าระวัง",
			"ไม่มีสถานีวัดใกล้บ้านส่งข้อมูลในช่วงไม่กี่ชั่วโมงที่ผ่านมา",
		},
		"Elio Del Ray": {
			"ระดับน้ำและปริมาณฝนจากสถานีวัดใกล้ Elio Del Ray ยังไม่ถึงเกณฑ์เฝ้าระวัง",
			"ไม่มีสถานีวัดใกล้ Elio Del Ray ส่งข้อมูลในช่วงไม่กี่ชั่วโมงที่ผ่านมา",
		},
	} {
		if got := thai.NothingRaised(label); got != want[0] {
			t.Errorf("nothing raised near %q = %q, want %q", label, got, want[0])
		}
		if got := thai.NoRecentData(label); got != want[1] {
			t.Errorf("no recent data near %q = %q, want %q", label, got, want[1])
		}
	}
}
