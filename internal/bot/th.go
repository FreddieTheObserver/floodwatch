package bot

import (
	"fmt"
	"math"
	"time"

	"github.com/FreddieTheObserver/floodwatch/internal/telegram"
)

// thai has had one round of native-speaker review (26 September 2026); the
// risk names and actions still need checking against the terms Thai
// residents know from official flood warnings.
var thai = texts{
	Code: "th",

	Distance: func(m float64) string {
		if m < 1000 {
			return fmt.Sprintf("%.0f ม.", math.Round(m/100)*100)
		}
		return fmt.Sprintf("%.1f กม.", m/1000)
	},
	Millimetres: func(v float64) string {
		if v == math.Trunc(v) {
			return fmt.Sprintf("%.0f มม.", v)
		}
		return fmt.Sprintf("%.1f มม.", v)
	},
	Clock: func(t time.Time) string { return t.In(ict).Format("15:04") + " น." },
	Ago: func(t, now time.Time) string {
		switch h, m, fresh := agoParts(t, now); {
		case fresh:
			return "เมื่อสักครู่"
		case h == 0:
			return fmt.Sprintf("%d นาทีที่แล้ว", m)
		case m == 0:
			return fmt.Sprintf("%d ชม. ที่แล้ว", h)
		default:
			return fmt.Sprintf("%d ชม. %d นาทีที่แล้ว", h, m)
		}
	},
	Duration: func(hours float64) string {
		switch {
		case hours < 0.75:
			return fmt.Sprintf("%.0f นาที", roundedMinutes(hours))
		case hours < 1.5:
			return "1 ชั่วโมง"
		default:
			return fmt.Sprintf("%.0f ชั่วโมง", math.Round(hours))
		}
	},

	RiskNames: [5]string{"ปกติ", "เฝ้าระวัง", "เตือนภัย", "วิกฤต", "ไม่มีข้อมูล"},
	Actions: [5][]string{
		{"ยังไม่ต้องทำอะไรในตอนนี้"},
		{"ติดตามข่าวสารอย่างต่อเนื่อง", "ตรวจสอบแผนที่น้ำท่วมก่อนออกเดินทาง"},
		{"ย้ายรถไปจอดในที่สูง", "ยกของมีค่าขึ้นจากพื้น", "หลีกเลี่ยงถนนที่อยู่ต่ำ"},
		{"ย้ายรถและของมีค่าขึ้นที่สูงทันที", "หลีกเลี่ยงถนนที่มีน้ำท่วม", "ปฏิบัติตามคำแนะนำของ กทม. (สายด่วน 1555)"},
		{"ติดตามประกาศทางการของ กทม.", "ตรวจสอบแผนที่น้ำท่วมก่อนออกเดินทาง"},
	},
	Trends: [4]string{"", "↘ สถานการณ์ดีขึ้น", "→ สถานการณ์ทรงตัว", "↗ สถานการณ์แย่ลง"},
	Headline: func(icon, risk, label string) string {
		return fmt.Sprintf("%s <b>%s</b> บริเวณ <b>%s</b>", icon, risk, label)
	},
	Transition: func(icon, label, from, to string) string {
		return fmt.Sprintf("%s <b>%s: %s → %s</b>", icon, label, from, to)
	},
	BackToLow: func(label string) string {
		return fmt.Sprintf("%s <b>%s: กลับสู่ระดับปกติ</b>", clearIcon, label)
	},
	NoDataHead: func(label string) string {
		return fmt.Sprintf("%s <b>%s: ไม่มีข้อมูล</b>", staleIcon, label)
	},
	WhatToDo:   "สิ่งที่ควรทำ",
	FloodRoads: `🚗 <a href="` + floodRoadsURL + `">ถนนที่มีน้ำท่วมตอนนี้</a> บนแผนที่ของ กทม. (ซูมไปที่พื้นที่ของคุณ)`,
	NothingRaised: func(label string) string {
		return fmt.Sprintf("ยังไม่มีสถานีวัดใกล้ %s ที่ถึงระดับเฝ้าระวัง", label)
	},
	NoRecentData: func(label string) string {
		return fmt.Sprintf("ไม่มีสถานีวัดใกล้ %s ส่งข้อมูลในช่วงไม่กี่ชั่วโมงที่ผ่านมา", label)
	},

	StationAway: func(name, distance string) string { return fmt.Sprintf("%s ห่าง %s", name, distance) },
	OutsideRadius: func(radius string) string {
		return fmt.Sprintf(" (นอกรัศมี %s จากพื้นที่ของคุณ)", radius)
	},
	RegionalWater: "ข้อมูลระดับน้ำในพื้นที่: ",
	RegionalRain:  "ข้อมูลฝนในพื้นที่: ",
	Bank: func(level, bank float64) string {
		switch d := level - bank; {
		case d >= 0.005:
			return fmt.Sprintf("สูงกว่าตลิ่ง %.2f ม.", d)
		case d <= -0.005:
			return fmt.Sprintf("ต่ำกว่าตลิ่ง %.2f ม.", -d)
		default:
			return "ระดับเดียวกับตลิ่ง"
		}
	},
	BankSentence: func(level, bank float64) string {
		switch d := level - bank; {
		case d >= 0.005:
			return fmt.Sprintf("ระดับน้ำสูงกว่าตลิ่ง %.2f ม.", d)
		case d <= -0.005:
			return fmt.Sprintf("ระดับน้ำต่ำกว่าตลิ่ง %.2f ม.", -d)
		default:
			return "ระดับน้ำเท่ากับตลิ่ง"
		}
	},
	Rise: func(rate float64) string {
		switch {
		case rate >= 1:
			return fmt.Sprintf("เพิ่มขึ้น %.0f ซม./ชม.", rate)
		case rate <= -1:
			return fmt.Sprintf("ลดลง %.0f ซม./ชม.", -rate)
		default:
			return "ทรงตัว"
		}
	},
	WaterIs: func(station, bank, rise string) string {
		if rise == "" {
			return fmt.Sprintf("%s %s", station, bank)
		}
		return fmt.Sprintf("%s %s และ%s", station, bank, rise)
	},
	WaterRising: func(station, bank, rise, eta string) string {
		return fmt.Sprintf("%s %s และ%s หากเป็นเช่นนี้ต่อไปจะถึงตลิ่งในอีกประมาณ %s", station, bank, rise, eta)
	},
	WaterStillRising: func(station, bank, rise string) string {
		return fmt.Sprintf("%s %s และยัง%s", station, bank, rise)
	},
	WaterHeld: func(station, bank, at string) string {
		return fmt.Sprintf("%s %s เมื่อส่งข้อมูลครั้งล่าสุดเวลา %s และไม่มีข้อมูลตั้งแต่นั้น", station, bank, at)
	},
	RainRecorded: func(station, amount, window, threshold string, above bool, lastHour string) string {
		relation := "เกิน"
		if !above {
			relation = "ใกล้ถึง"
		}
		text := fmt.Sprintf("%s วัดปริมาณฝนได้ %s %s %sเกณฑ์ %s ของ FloodWatch", station, amount, window, relation, threshold)
		if lastHour != "" {
			text += fmt.Sprintf(" และ %s ในชั่วโมงที่ผ่านมา", lastHour)
		}
		return text
	},
	RainHeld:    "สถานีวัดฝนใกล้เคียงหยุดส่งข้อมูล ค่าล่าสุดเกินเกณฑ์ของ FloodWatch",
	RainWindows: [3]string{"ในชั่วโมงที่ผ่านมา", "ใน 3 ชั่วโมงที่ผ่านมา", "ใน 24 ชั่วโมงที่ผ่านมา"},

	Details:        "รายละเอียด",
	WaterHeading:   "ระดับน้ำ",
	RainHeading:    "ฝน",
	TrendHeading:   "แนวโน้ม",
	SourcesHeading: "แหล่งข้อมูล",
	NoWaterGauge: func(km int) string {
		return fmt.Sprintf("ไม่มีสถานีวัดระดับน้ำในระยะ %d กม.", km)
	},
	NoRainGauge: func(km int) string {
		return fmt.Sprintf("ไม่มีสถานีวัดฝนในระยะ %d กม.", km)
	},
	NoFreshRain: "ไม่มีข้อมูลฝนล่าสุดจากสถานีใกล้เคียง",
	StationLabel: func(name, distance string, regional bool) string {
		if regional {
			return fmt.Sprintf("%s (%s, นอกรัศมี)", name, distance)
		}
		return fmt.Sprintf("%s (%s)", name, distance)
	},
	Reading: func(bank, rise string) string {
		if rise == "" {
			return bank
		}
		return bank + " · " + rise
	},
	NoDataSince: func(at, ago string) string {
		return fmt.Sprintf("ไม่มีข้อมูลตั้งแต่ %s · %s", at, ago)
	},
	Measured:   func(at, ago string) string { return fmt.Sprintf("วัดเมื่อ %s · %s", at, ago) },
	RainTotals: [3]string{"1 ชั่วโมง", "3 ชั่วโมง", "24 ชั่วโมง"},
	WettestOf: func(n int) string {
		return fmt.Sprintf("มีปริมาณฝนสูงสุดเมื่อเทียบกับ %d สถานีใกล้เคียงที่ส่งข้อมูล", n)
	},
	RegionalNote: func(radius string) string {
		return fmt.Sprintf("สถานีที่อยู่นอกรัศมี %s จากพื้นที่ของคุณ แสดงไว้เพราะอาจบ่งบอกสถานการณ์น้ำท่วมในภาพรวม", radius)
	},
	TrendUnknown: "ไม่ทราบ: ไม่มีข้อมูลล่าสุดให้ประเมิน",
	TrendSteady:  "สถานการณ์ทรงตัว: ไม่มีการเปลี่ยนแปลงสำคัญในชั่วโมงที่ผ่านมา",
	TrendWorse:   "สถานการณ์แย่ลง",
	TrendBetter:  "สถานการณ์ดีขึ้น",
	WorseRain: func(station, amount string) string {
		return fmt.Sprintf("สถานการณ์แย่ลงเพราะฝนที่ %s หนักขึ้น (%s ในชั่วโมงที่ผ่านมา)", station, amount)
	},
	WorseWater: func(station, rise string) string {
		return fmt.Sprintf("สถานการณ์แย่ลงเพราะน้ำที่ %s %s", station, rise)
	},
	BetterRain: func(station string) string {
		return fmt.Sprintf("สถานการณ์ดีขึ้นเพราะฝนที่ %s เบาลง", station)
	},
	BetterWater: func(station, rise string) string {
		return fmt.Sprintf("สถานการณ์ดีขึ้นเพราะน้ำที่ %s %s", station, rise)
	},
	WaterGauges: "สถานีวัดระดับน้ำ",
	RainGauges:  "สถานีวัดฝน",
	DataVia:     "แหล่งข้อมูล: ThaiWater (สสน.)",
	Checked:     func(at string) string { return "ตรวจสอบล่าสุด " + at },
	Disclaimer: "<i>ข้อมูลนี้ไม่ใช่ประกาศเตือนภัยอย่างเป็นทางการ อาจล่าช้า ไม่ครบถ้วน หรือคลาดเคลื่อน\n" +
		"ข้อมูลทางการ: สายด่วน กทม. 1555 · เหตุฉุกเฉิน 1669</i>",
	RainPinWindows: [3]string{"1 ชม.", "3 ชม.", "24 ชม."},
	RainPinAmount:  func(amount, window string) string { return amount + " ใน " + window },
	PinPlace:       "สถานที่ของคุณ",
	PinReading:     func(bank, at string) string { return bank + " เวลา " + at },
	PinNoData:      func(at string) string { return "ไม่มีข้อมูลตั้งแต่ " + at },
	PinWater: func(distance, label, reading string) string {
		return fmt.Sprintf("ห่างจาก %s %s · %s", label, distance, reading)
	},
	PinRain: func(distance, label, amounts, at string) string {
		return fmt.Sprintf("ห่างจาก %s %s · ฝนมากที่สุดในบริเวณ %s เวลา %s", label, distance, amounts, at)
	},
	MapButton: "📍 ดูสถานีวัดบนแผนที่",

	Welcome: `🌊 <b>FloodWatch BKK</b>

FloodWatch ติดตามสถานีวัดระดับน้ำในคลองและแม่น้ำ รวมถึงสถานีวัดฝนรอบสถานที่ที่คุณเลือก และจะแจ้งเตือนเมื่อระดับความเสี่ยงน้ำท่วมเปลี่ยนไป

<b>เริ่มต้นโดยส่งตำแหน่ง</b>: แตะปุ่มด้านล่าง หรือ 📎 แล้วเลือก ตำแหน่ง บนคอมพิวเตอร์ให้วางพิกัด เช่น <code>13.6515, 100.4945</code>

/status  ความเสี่ยงน้ำท่วมรอบสถานที่ของคุณ
/places  ดู เปลี่ยนชื่อ หรือลบสถานที่ (สูงสุด 5 แห่ง)
/language  ภาษาไทย / English
/stop  หยุดการแจ้งเตือนและลบข้อมูลของคุณ

FloodWatch เก็บเพียงหมายเลขแชต ภาษาที่เลือก และตำแหน่งที่คุณส่งมาเท่านั้น /stop จะลบข้อมูลทั้งหมด`,
	Commands: []telegram.Command{
		{Command: "status", Description: "ความเสี่ยงน้ำท่วมรอบสถานที่ของคุณ"},
		{Command: "places", Description: "ดู เปลี่ยนชื่อ หรือลบสถานที่"},
		{Command: "language", Description: "ภาษาไทย / English"},
		{Command: "stop", Description: "หยุดการแจ้งเตือนและลบข้อมูล"},
		{Command: "help", Description: "วิธีใช้ FloodWatch"},
	},
	ShareLocation:    "📍 ส่งตำแหน่งของฉัน",
	Hint:             "ส่งตำแหน่งที่ต้องการติดตามมาได้เลย หรือดู /help",
	NotCovered:       "ติดตามตำแหน่งนี้ไม่ได้ เพราะไม่มีสถานีวัดที่ทำงานอยู่ในระยะ 10 กม. FloodWatch ครอบคลุมกรุงเทพฯ และจังหวัดโดยรอบ",
	StopConfirm:      "การดำเนินการนี้จะหยุดการแจ้งเตือนทั้งหมดและลบสถานที่ของคุณ แน่ใจหรือไม่?",
	DeleteEverything: "ลบทั้งหมด",
	Cancel:           "ยกเลิก",
	NothingDeleted:   "ไม่ได้ลบข้อมูลใด",
	Forgot: func(n int64) string {
		return fmt.Sprintf("เรียบร้อย ลบสถานที่ %d แห่งของคุณแล้ว และจะไม่ส่งข้อความหาคุณอีก ส่งตำแหน่งมาได้ทุกเมื่อเพื่อเริ่มใหม่", n)
	},
	AlreadyWatch: func(labels string) string {
		return fmt.Sprintf("คุณติดตาม %s อยู่แล้ว ต้องการทำอะไรกับตำแหน่งนี้?", labels)
	},
	MoveHere: func(label string) string { return "ย้าย " + label + " มาที่นี่" },
	AddNew:   "เพิ่มเป็นสถานที่ใหม่",
	TooManyPlaces: func(max int) string {
		return fmt.Sprintf("ติดตามได้สูงสุด %d แห่ง ลบสถานที่หนึ่งแห่งผ่าน /places ก่อน", max)
	},
	Watching: func(label string) string {
		return fmt.Sprintf("กำลังติดตาม <b>%s</b> จะแจ้งเตือนเมื่อระดับความเสี่ยงน้ำท่วมเปลี่ยนไป\n\n", label)
	},
	NoPlaces: "คุณยังไม่ได้ติดตามสถานที่ใด ส่งตำแหน่งมาเพื่อเริ่มต้น",
	PlacesList: func(lines string) string {
		return "สถานที่ของคุณ:\n" + lines + "\n\nส่งตำแหน่งเพื่อเพิ่มหรือย้ายสถานที่"
	},
	RenameButton:  func(label string) string { return "เปลี่ยนชื่อ " + label },
	RemoveButton:  func(label string) string { return "ลบ " + label },
	PlaceGoneAdd:  "สถานที่นี้ถูกลบไปแล้ว ส่งตำแหน่งอีกครั้งเพื่อเพิ่ม",
	PlaceGoneList: "สถานที่นี้ถูกลบไปแล้ว ดู /places",
	PlaceGone:     "สถานที่นี้ถูกลบไปแล้ว",
	Moving:        func(label string) string { return "กำลังย้าย <b>" + label + "</b>" },
	Adding:        func(label string) string { return "กำลังเพิ่ม <b>" + label + "</b>" },
	Removed: func(label string) string {
		return "ลบ <b>" + label + "</b> แล้ว จะไม่มีการแจ้งเตือนสำหรับสถานที่นี้อีก"
	},
	AlreadyRemoved: func(label string) string { return "<b>" + label + "</b> ถูกลบไปแล้ว" },
	RenamePrompt: func(label string) string {
		return renameMarker + " <b>" + label + "</b>\nจะตั้งชื่อสถานที่นี้ว่าอะไรดี? ตอบกลับด้วยชื่อ เช่น ที่ทำงาน หรือ บ้านแม่"
	},
	RenamePlaceholder: "ที่ทำงาน, บ้านแม่, ...",
	NameNeedsLetter:   "ชื่อต้องมีตัวอักษรหรือตัวเลขอย่างน้อยหนึ่งตัว",
	NameTooLong: func(max int) string {
		return fmt.Sprintf("ชื่อยาวได้ไม่เกิน %d ตัวอักษร", max)
	},
	NameBadChars: "ชื่อนี้มีตัวอักษรที่ใช้ไม่ได้",
	RenameRetry:  "แตะ เปลี่ยนชื่อ ใน /places เพื่อลองอีกครั้ง",
	NameTaken: func(label string) string {
		return fmt.Sprintf("คุณมีสถานที่ชื่อ <b>%s</b> อยู่แล้ว เลือกชื่ออื่นผ่าน /places", label)
	},
	PlaceMissing: func(label string) string {
		return fmt.Sprintf("ไม่พบ <b>%s</b> แล้ว ดู /places", label)
	},
	Renamed: func(from, to string) string {
		return fmt.Sprintf("เปลี่ยนชื่อ <b>%s</b> เป็น <b>%s</b> แล้ว", from, to)
	},
	Failed:         "ขออภัย เกิดข้อผิดพลาด โปรดลองอีกครั้งในอีกสักครู่",
	DefaultHome:    "บ้าน",
	DefaultPlace:   func(n int) string { return fmt.Sprintf("สถานที่ %d", n) },
	LanguagePrompt: "เลือกภาษา / Choose a language",
	LanguageSet:    "ตั้งค่าเป็นภาษาไทยแล้ว",

	When: func(t, now time.Time) string {
		t = t.In(ict)
		if sameDay(t, now) {
			return t.Format("15:04") + " น."
		}
		return fmt.Sprintf("%d %s %s น.", t.Day(), thaiMonths[t.Month()-1], t.Format("15:04"))
	},
	Offline: func(from, to, took string) string {
		return fmt.Sprintf("⚠️ <b>FloodWatch ไม่ได้ทำงานตั้งแต่ %s ถึง %s</b> (ประมาณ %s) จึงไม่สามารถแจ้งเตือนคุณได้ในช่วงเวลานั้น", from, to, took)
	},
	OfflinePlaces: "ตอนนี้กลับมาทำงานแล้ว สถานการณ์ของสถานที่ของคุณตอนนี้:",
	OfflineStatus: "ส่ง /status เพื่อดูรายละเอียด",
	LateReply: func(at string) string {
		return "ขออภัยที่ตอบช้า FloodWatch ไม่ได้ทำงานตอนที่คุณส่งข้อความมาเมื่อ " + at
	},
}

var thaiMonths = [12]string{"ม.ค.", "ก.พ.", "มี.ค.", "เม.ย.", "พ.ค.", "มิ.ย.", "ก.ค.", "ส.ค.", "ก.ย.", "ต.ค.", "พ.ย.", "ธ.ค."}
