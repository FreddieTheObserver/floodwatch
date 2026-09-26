package bot

import (
	"strings"
	"time"

	"github.com/FreddieTheObserver/floodwatch/internal/alert"
	"github.com/FreddieTheObserver/floodwatch/internal/telegram"
)

// texts is everything the bot says, in one language. Whole sentences are
// kept together rather than assembled from translated words, since word
// order differs between languages. Every language fills in every field:
// TestEveryLanguageIsComplete fails on any left empty, so no message can
// fall back to another language halfway through.
type texts struct {
	Code string

	// Units and times.
	Distance    func(metres float64) string
	Millimetres func(mm float64) string
	Clock       func(t time.Time) string
	Ago         func(t, now time.Time) string
	Duration    func(hours float64) string

	// The verdict, indexed by risk level with alert.RiskUnknown last.
	RiskNames     [5]string
	Actions       [5][]string
	Trends        [4]string // indexed by alert.Trend; the first, unknown, is shown as nothing
	Headline      func(icon, risk, label string) string
	Transition    func(icon, label, from, to string) string
	BackToLow     func(label string) string
	NoDataHead    func(label string) string
	WhatToDo      string
	FloodRoads    string
	NothingRaised func(label string) string
	NoRecentData  func(label string) string

	// What is happening, stated as measurements.
	StationAway      func(name, distance string) string
	OutsideRadius    func(radius string) string
	Regional         string
	Bank             func(level, bank float64) string // a bare measurement
	BankSentence     func(level, bank float64) string // the same inside a sentence
	Rise             func(cmPerHour float64) string
	WaterIs          func(station, bank, rise string) string
	WaterRising      func(station, bank, rise, eta string) string
	WaterStillRising func(station, bank, rise string) string
	WaterHeld        func(station, bank, at string) string
	// RainRecorded states a total against the threshold it crossed; lastHour
	// is the last hour's total when the window is longer, and empty otherwise.
	RainRecorded func(station, amount, window, threshold string, above bool, lastHour string) string
	RainHeld     string
	RainWindows  [3]string // the last 1, 3 and 24 hours, inside a sentence

	// The measurements in detail.
	Details        string
	WaterHeading   string
	RainHeading    string
	TrendHeading   string
	SourcesHeading string
	NoWaterGauge   func(km int) string
	NoRainGauge    func(km int) string
	NoFreshRain    string
	StationLabel   func(name, distance string, regional bool) string
	Reading        func(bank, rise string) string
	NoDataSince    func(at, ago string) string
	Measured       func(at, ago string) string
	RainTotals     [3]string // labels for the 1, 3 and 24 hour totals
	WettestOf      func(gauges int) string
	RegionalNote   func(radius string) string
	TrendUnknown   string
	TrendSteady    string
	TrendWorse     string
	TrendBetter    string
	WorseRain      func(station, amount string) string
	WorseWater     func(station, rise string) string
	BetterRain     func(station string) string
	BetterWater    func(station, rise string) string
	WaterGauges    string
	RainGauges     string
	DataVia        string
	Checked        func(at string) string
	Disclaimer     string
	RainPinWindows [3]string // short labels for the 1, 3 and 24 hour totals
	RainPinAmount  func(amount, window string) string
	PinPlace       string
	PinReading     func(bank, at string) string
	PinNoData      func(at string) string
	PinWater       func(distance, label, reading string) string
	PinRain        func(distance, label, amounts, at string) string
	MapButton      string

	// The conversation.
	Welcome           string
	Commands          []telegram.Command
	ShareLocation     string
	Hint              string
	NotCovered        string
	StopConfirm       string
	DeleteEverything  string
	Cancel            string
	NothingDeleted    string
	Forgot            func(places int64) string
	AlreadyWatch      func(labels string) string
	MoveHere          func(label string) string
	AddNew            string
	TooManyPlaces     func(max int) string
	Watching          func(label string) string
	NoPlaces          string
	PlacesList        func(lines string) string
	RenameButton      func(label string) string
	RemoveButton      func(label string) string
	PlaceGoneAdd      string
	PlaceGoneList     string
	PlaceGone         string
	Moving            func(label string) string
	Adding            func(label string) string
	Removed           func(label string) string
	AlreadyRemoved    func(label string) string
	RenamePrompt      func(label string) string
	RenamePlaceholder string
	NameNeedsLetter   string
	NameTooLong       func(max int) string
	NameBadChars      string
	RenameRetry       string
	NameTaken         func(label string) string
	PlaceMissing      func(label string) string
	Renamed           func(from, to string) string
	Failed            string
	DefaultHome       string
	DefaultPlace      func(n int) string
	LanguagePrompt    string
	LanguageSet       string
}

var languages = map[string]*texts{english.Code: &english, thai.Code: &thai}

// languageFor picks a language from Telegram's app language code: Thai for a
// Thai app, English for everything else.
func languageFor(code string) *texts {
	if strings.HasPrefix(strings.ToLower(code), "th") {
		return &thai
	}
	return &english
}

// stationName prefers a station's Thai name for Thai readers.
func (t *texts) stationName(st alert.Station) string {
	if t.Code == thai.Code && st.NameTH != "" {
		return st.NameTH
	}
	return st.Name
}

// rainWindowIndex maps the windows rain is judged over to the entries of the
// per-window arrays above.
func rainWindowIndex(w time.Duration) int {
	switch w {
	case time.Hour:
		return 0
	case 3 * time.Hour:
		return 1
	}
	return 2
}
