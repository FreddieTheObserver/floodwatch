// Package tide models how water gauges near the river mouths follow the
// predicted tide, and forecasts from that how high they will go.
//
// Each gauge is fitted to the tide station and delay that best explain its
// last few days of readings, as level = intercept + gain * tide(t - lag).
// For a gauge the tide explains well, the highest level of the next hours is
// forecast from the predicted tide, shifted by how far the gauge now departs
// from its fit; that departure is the water the tide does not explain, such
// as flood water coming down the river.
package tide

import (
	"math"
	"slices"
	"sort"
	"time"
)

const (
	// Fits use this much of each gauge's recent history: enough to cover the
	// tide's daily cycles, short enough to follow a river whose flow changes.
	FitWindow = 3 * 24 * time.Hour
	// A fit needs readings spanning at least a day and a half, taking in a
	// full daily cycle of the tide and some of the next.
	minSpan    = 36 * time.Hour
	minSamples = 150
	maxLag     = 6 * time.Hour
	lagStep    = 10 * time.Minute
	// Hourly predictions further apart than this have a gap between them.
	maxPredictionGap = 90 * time.Minute

	// A gauge is tidal when the tide explains its readings at least this well.
	// Below it, forecasting from the tide did worse than assuming no change
	// in a backtest over the gauges around Bangkok on 27 September 2026.
	TidalR = 0.8
)

type Point struct {
	At    time.Time
	Level float64
}

// Series is a tide station's hourly predictions, oldest first.
type Series []Point

// At is the predicted tide at t, interpolated between the hours either side.
func (s Series) At(t time.Time) (float64, bool) {
	i := sort.Search(len(s), func(i int) bool { return !s[i].At.Before(t) })
	switch {
	case i == len(s):
		return 0, false
	case s[i].At.Equal(t):
		return s[i].Level, true
	case i == 0:
		return 0, false
	}
	a, b := s[i-1], s[i]
	gap := b.At.Sub(a.At)
	if gap > maxPredictionGap {
		return 0, false
	}
	return a.Level + (b.Level-a.Level)*float64(t.Sub(a.At))/float64(gap), true
}

type Fit struct {
	StationID   int64
	TideStation string
	Lag         time.Duration
	Intercept   float64
	Gain        float64
	// R is the correlation between the modelled and the actual levels, and
	// ResidualSD how far apart they typically are, in metres.
	R          float64
	ResidualSD float64
	Samples    int
	From, To   time.Time
}

func (f Fit) Tidal() bool { return f.R >= TidalR }

// Level is what the fit expects the gauge to read at t.
func (f Fit) Level(tides Series, t time.Time) (float64, bool) {
	x, ok := tides.At(t.Add(-f.Lag))
	return f.Intercept + f.Gain*x, ok
}

// FitGauge finds the tide station and delay that best explain a gauge's
// readings, which must be oldest first, and the straight line from that tide
// to the gauge's level.
func FitGauge(stationID int64, readings []Point, tides map[string]Series) (Fit, bool) {
	if len(readings) < minSamples || readings[len(readings)-1].At.Sub(readings[0].At) < minSpan {
		return Fit{}, false
	}
	codes := make([]string, 0, len(tides))
	for code := range tides {
		codes = append(codes, code)
	}
	slices.Sort(codes)

	var best Fit
	found := false
	for _, code := range codes {
		for lag := time.Duration(0); lag <= maxLag; lag += lagStep {
			f, ok := fitLine(readings, tides[code], lag)
			if ok && (!found || f.R > best.R) {
				best, found = f, true
				best.TideStation, best.Lag = code, lag
			}
		}
	}
	if !found {
		return Fit{}, false
	}
	best.StationID = stationID
	best.From, best.To = readings[0].At, readings[len(readings)-1].At
	return best, true
}

// fitLine regresses the readings on the tide lag earlier, by least squares.
func fitLine(readings []Point, tides Series, lag time.Duration) (Fit, bool) {
	xs := make([]float64, 0, len(readings))
	ys := make([]float64, 0, len(readings))
	for _, r := range readings {
		if x, ok := tides.At(r.At.Add(-lag)); ok {
			xs = append(xs, x)
			ys = append(ys, r.Level)
		}
	}
	n := len(xs)
	if n < minSamples {
		return Fit{}, false
	}
	mx, my := mean(xs), mean(ys)
	var sxx, syy, sxy float64
	for i := range xs {
		dx, dy := xs[i]-mx, ys[i]-my
		sxx += dx * dx
		syy += dy * dy
		sxy += dx * dy
	}
	if sxx == 0 || syy == 0 {
		return Fit{}, false
	}
	r := sxy / math.Sqrt(sxx*syy)
	gain := sxy / sxx
	return Fit{
		Intercept:  my - gain*mx,
		Gain:       gain,
		R:          r,
		ResidualSD: math.Sqrt(max(0, syy*(1-r*r)/float64(n))),
		Samples:    n,
	}, true
}

func mean(v []float64) float64 {
	var sum float64
	for _, x := range v {
		sum += x
	}
	return sum / float64(len(v))
}
