package bot

import (
	"fmt"
	"math"
	"time"

	"github.com/FreddieTheObserver/floodwatch/internal/telegram"
)

var english = texts{
	Code: "en",

	Distance: func(m float64) string {
		if m < 1000 {
			return fmt.Sprintf("%.0f m", math.Round(m/100)*100)
		}
		return fmt.Sprintf("%.1f km", m/1000)
	},
	Millimetres: func(v float64) string {
		if v == math.Trunc(v) {
			return fmt.Sprintf("%.0f mm", v)
		}
		return fmt.Sprintf("%.1f mm", v)
	},
	Clock: func(t time.Time) string { return t.In(ict).Format("15:04") },
	Ago: func(t, now time.Time) string {
		switch h, m, fresh := agoParts(t, now); {
		case fresh:
			return "just now"
		case h == 0:
			return fmt.Sprintf("%d min ago", m)
		case m == 0:
			return fmt.Sprintf("%d h ago", h)
		default:
			return fmt.Sprintf("%d h %d min ago", h, m)
		}
	},
	Duration: func(hours float64) string {
		switch {
		case hours < 0.75:
			return fmt.Sprintf("%.0f minutes", roundedMinutes(hours))
		case hours < 1.5:
			return "an hour"
		default:
			return fmt.Sprintf("%.0f hours", math.Round(hours))
		}
	},

	RiskNames: [5]string{"LOW", "WATCH", "WARNING", "HIGH", "NO DATA"},
	Actions: [5][]string{
		{"Nothing to do right now."},
		{"Keep an eye on updates.", "Check the flood map before driving."},
		{"Move your car to higher ground.", "Move valuables off the floor.", "Avoid low roads."},
		{"Move your car and valuables up now.", "Stay off flooded roads.", "Follow official BMA instructions (1555)."},
		{"Check official BMA updates.", "Check the flood map before driving."},
	},
	Trends: [4]string{"", "↘ improving", "→ steady", "↗ getting worse"},
	Headline: func(icon, risk, label string) string {
		return fmt.Sprintf("%s <b>%s</b> around <b>%s</b>", icon, risk, label)
	},
	Transition: func(icon, label, from, to string) string {
		return fmt.Sprintf("%s <b>%s: %s → %s</b>", icon, label, from, to)
	},
	BackToLow:     func(label string) string { return fmt.Sprintf("%s <b>%s: back to LOW</b>", clearIcon, label) },
	NoDataHead:    func(label string) string { return fmt.Sprintf("%s <b>%s: NO DATA</b>", staleIcon, label) },
	WhatToDo:      "What to do",
	FloodRoads:    `🚗 <a href="` + floodRoadsURL + `">Flooded roads right now</a> on the BMA map (zoom to your area)`,
	NothingRaised: func(label string) string { return fmt.Sprintf("Nothing near %s is at a warning level.", label) },
	NoRecentData: func(label string) string {
		return fmt.Sprintf("No gauge near %s has reported in the last few hours.", label)
	},

	StationAway:   func(name, distance string) string { return fmt.Sprintf("%s, %s away", name, distance) },
	OutsideRadius: func(radius string) string { return fmt.Sprintf(" (outside your %s radius)", radius) },
	RegionalWater: "Regional: ",
	RegionalRain:  "Regional: ",
	Bank: func(level, bank float64) string {
		switch d := level - bank; {
		case d >= 0.005:
			return fmt.Sprintf("%.2f m above bank", d)
		case d <= -0.005:
			return fmt.Sprintf("%.2f m below bank", -d)
		default:
			return "at bank level"
		}
	},
	BankSentence: func(level, bank float64) string {
		switch d := level - bank; {
		case d >= 0.005:
			return fmt.Sprintf("%.2f m above its bank", d)
		case d <= -0.005:
			return fmt.Sprintf("%.2f m below its bank", -d)
		default:
			return "level with its bank"
		}
	},
	Rise: func(rate float64) string {
		switch {
		case rate >= 1:
			return fmt.Sprintf("rising %.0f cm/h", rate)
		case rate <= -1:
			return fmt.Sprintf("falling %.0f cm/h", -rate)
		default:
			return "steady"
		}
	},
	WaterIs: func(station, bank, rise string) string {
		if rise == "" {
			return fmt.Sprintf("%s, is %s.", station, bank)
		}
		return fmt.Sprintf("%s, is %s and %s.", station, bank, rise)
	},
	WaterRising: func(station, bank, rise, eta string) string {
		return fmt.Sprintf("%s, is %s and %s; at this rate it reaches the bank in about %s.", station, bank, rise, eta)
	},
	WaterStillRising: func(station, bank, rise string) string {
		return fmt.Sprintf("%s, is %s and still %s.", station, bank, rise)
	},
	WaterHeld: func(station, bank, at string) string {
		return fmt.Sprintf("%s, was %s when it last reported at %s and has been silent since.", station, bank, at)
	},
	RainRecorded: func(station, amount, window, threshold string, above bool, lastHour string) string {
		relation := "above"
		if !above {
			relation = "just under"
		}
		text := fmt.Sprintf("%s, recorded %s %s, %s FloodWatch's %s threshold", station, amount, window, relation, threshold)
		if lastHour != "" {
			text += fmt.Sprintf(", with %s in the last hour", lastHour)
		}
		return text + "."
	},
	RainHeld:    "The rain gauges nearby have stopped reporting; the last readings were above FloodWatch's thresholds.",
	RainWindows: [3]string{"over the last hour", "over the last 3 hours", "over the last 24 hours"},

	Details:        "Details",
	WaterHeading:   "WATER",
	RainHeading:    "RAIN",
	TrendHeading:   "TREND",
	SourcesHeading: "SOURCES",
	NoWaterGauge:   func(km int) string { return fmt.Sprintf("No water level gauge within %d km.", km) },
	NoRainGauge:    func(km int) string { return fmt.Sprintf("No rain gauge within %d km.", km) },
	NoFreshRain:    "No fresh readings from any rain gauge nearby.",
	StationLabel: func(name, distance string, regional bool) string {
		if regional {
			return fmt.Sprintf("%s (%s, regional)", name, distance)
		}
		return fmt.Sprintf("%s (%s)", name, distance)
	},
	Reading: func(bank, rise string) string {
		if rise == "" {
			return bank
		}
		return bank + " · " + capitalise(rise)
	},
	NoDataSince: func(at, ago string) string { return fmt.Sprintf("No data since %s · %s", at, ago) },
	Measured:    func(at, ago string) string { return fmt.Sprintf("Measured %s · %s", at, ago) },
	RainTotals:  [3]string{"1 hour", "3 hours", "24 hours"},
	WettestOf:   func(n int) string { return fmt.Sprintf("Wettest of %d nearby gauges reporting.", n) },
	RegionalNote: func(radius string) string {
		return fmt.Sprintf("Stations marked regional are outside your %s radius. They are included because they may show regional flood conditions.", radius)
	},
	TrendUnknown: "Unknown: there are no fresh readings to judge by.",
	TrendSteady:  "Steady: no significant change over the last hour.",
	TrendWorse:   "Getting worse.",
	TrendBetter:  "Improving.",
	WorseRain: func(station, amount string) string {
		return fmt.Sprintf("Getting worse because rain at %s is getting heavier (%s in the last hour).", station, amount)
	},
	WorseWater: func(station, rise string) string {
		return fmt.Sprintf("Getting worse because water at %s is %s.", station, rise)
	},
	BetterRain: func(station string) string { return fmt.Sprintf("Improving because rain at %s has eased.", station) },
	BetterWater: func(station, rise string) string {
		return fmt.Sprintf("Improving because water at %s is %s.", station, rise)
	},
	WaterGauges: "Water gauges",
	RainGauges:  "Rain gauges",
	DataVia:     "Data: ThaiWater (HII)",
	Checked:     func(at string) string { return "Checked " + at },
	// The readings come from official gauges; what is unofficial is the verdict.
	Disclaimer: "<i>This is not an official warning. Measurements may be delayed, incomplete or inaccurate.\n" +
		"Official information: BMA 1555 · Emergency: 1669</i>",
	RainPinWindows: [3]string{"1 h", "3 h", "24 h"},
	RainPinAmount:  func(amount, window string) string { return amount + " in " + window },
	PinPlace:       "Your place",
	PinReading:     func(bank, at string) string { return bank + " at " + at },
	PinNoData:      func(at string) string { return "no data since " + at },
	PinWater: func(distance, label, reading string) string {
		return fmt.Sprintf("%s from %s · %s", distance, label, reading)
	},
	PinRain: func(distance, label, amounts, at string) string {
		return fmt.Sprintf("%s from %s · wettest nearby, %s at %s", distance, label, amounts, at)
	},
	MapButton: "📍 Show gauges on map",

	Welcome: `🌊 <b>FloodWatch BKK</b>

I watch the canal and river gauges and rain gauges around places you choose, and message you when their flood risk changes.

<b>To start, send me a location</b>: tap the button below, or 📎 then Location. On a computer, paste coordinates like <code>13.6515, 100.4945</code>.

/status  the flood risk around your places
/places  list, rename or remove places (up to 5)
/language  ภาษาไทย / English
/stop  stop alerts and delete your data

I store only your chat ID, your language and the locations you send. /stop deletes them.`,
	Commands: []telegram.Command{
		{Command: "status", Description: "The flood risk around your places"},
		{Command: "places", Description: "List, rename or remove your places"},
		{Command: "language", Description: "ภาษาไทย / English"},
		{Command: "stop", Description: "Stop alerts and delete your data"},
		{Command: "help", Description: "How FloodWatch works"},
	},
	ShareLocation:    "📍 Share my location",
	Hint:             "Send me a location to watch, or see /help.",
	NotCovered:       "I can't watch that location: there's no working gauge within 10 km of it. FloodWatch covers Bangkok and the provinces around it.",
	StopConfirm:      "This stops all alerts and deletes your places. Are you sure?",
	DeleteEverything: "Delete everything",
	Cancel:           "Cancel",
	NothingDeleted:   "Nothing was deleted.",
	Forgot: func(n int64) string {
		places := "places"
		if n == 1 {
			places = "place"
		}
		return fmt.Sprintf("Done. I deleted your %d %s and will not message you again. Send a location any time to start over.", n, places)
	},
	AlreadyWatch: func(labels string) string {
		return fmt.Sprintf("You already watch %s. What should I do with this location?", labels)
	},
	MoveHere: func(label string) string { return "Move " + label + " here" },
	AddNew:   "Add as a new place",
	TooManyPlaces: func(max int) string {
		return fmt.Sprintf("You can watch up to %d places. Remove one with /places first.", max)
	},
	Watching: func(label string) string {
		return fmt.Sprintf("Watching <b>%s</b>. I'll message you when its flood risk changes.\n\n", label)
	},
	NoPlaces: "You're not watching anywhere yet. Send me a location to start.",
	PlacesList: func(lines string) string {
		return "Your places:\n" + lines + "\n\nSend a location to add a place or move one."
	},
	RenameButton:   func(label string) string { return "Rename " + label },
	RemoveButton:   func(label string) string { return "Remove " + label },
	PlaceGoneAdd:   "That place is gone. Send the location again to add it.",
	PlaceGoneList:  "That place is gone. See /places.",
	PlaceGone:      "That place was already removed.",
	Moving:         func(label string) string { return "Moving <b>" + label + "</b>." },
	Adding:         func(label string) string { return "Adding <b>" + label + "</b>." },
	Removed:        func(label string) string { return "Removed <b>" + label + "</b>. No more alerts for it." },
	AlreadyRemoved: func(label string) string { return "<b>" + label + "</b> was already removed." },
	RenamePrompt: func(label string) string {
		return renameMarker + " <b>" + label + "</b>\nWhat should I call this place? Reply with a name, like Office or Mum's house."
	},
	RenamePlaceholder: "Office, Mum's house, ...",
	NameNeedsLetter:   "A name needs at least one letter or number.",
	NameTooLong:       func(max int) string { return fmt.Sprintf("Please keep names to %d characters or fewer.", max) },
	NameBadChars:      "That name has characters I can't use.",
	RenameRetry:       "Tap Rename in /places to try again.",
	NameTaken: func(label string) string {
		return fmt.Sprintf("You already have a place called <b>%s</b>. Pick another name with /places.", label)
	},
	PlaceMissing: func(label string) string {
		return fmt.Sprintf("I couldn't find <b>%s</b> any more. See /places.", label)
	},
	Renamed:        func(from, to string) string { return fmt.Sprintf("Renamed <b>%s</b> to <b>%s</b>.", from, to) },
	Failed:         "Sorry, something went wrong just now. Please try again in a minute.",
	DefaultHome:    "Home",
	DefaultPlace:   func(n int) string { return fmt.Sprintf("Place %d", n) },
	LanguagePrompt: "Choose a language / เลือกภาษา",
	LanguageSet:    "Language set to English.",

	When: func(t, now time.Time) string {
		if sameDay(t, now) {
			return t.In(ict).Format("15:04")
		}
		return t.In(ict).Format("2 Jan 15:04")
	},
	Offline: func(from, to, took string) string {
		return fmt.Sprintf("⚠️ <b>FloodWatch was offline from %s to %s</b> (about %s), so it could not have warned you of anything in that time.", from, to, took)
	},
	OfflinePlaces: "It's running again. Your places now:",
	OfflineStatus: "Send /status for the details.",
	LateReply: func(at string) string {
		return "Sorry for the late reply: FloodWatch was offline when you wrote at " + at + "."
	},
}
