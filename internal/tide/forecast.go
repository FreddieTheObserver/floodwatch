package tide

import "time"

const (
	// Forecasts look this far ahead, as far as the backtest checked them.
	Horizon = 6 * time.Hour
	// A forecast starts from a current reading and the gauge's departure from
	// its fit over the hour before, which is assumed to hold.
	maxReadingAge = 30 * time.Minute
	offsetWindow  = time.Hour
	forecastStep  = 10 * time.Minute
)

// Forecast is the highest level a tidal gauge is expected to reach over the
// Horizon after its reading at BasedOn.
type Forecast struct {
	StationID   int64
	BasedOn     time.Time
	PeakAt      time.Time
	PeakLevel   float64
	Offset      float64
	TideStation string
	Lag         time.Duration
}

// Predict forecasts a tidal gauge's highest level over the Horizon from its
// latest reading, if that is current at now. The readings must be oldest
// first, and the predictions must cover the whole horizon.
func Predict(f Fit, readings []Point, tides Series, now time.Time) (Forecast, bool) {
	if !f.Tidal() || len(readings) == 0 {
		return Forecast{}, false
	}
	latest := readings[len(readings)-1]
	if now.Sub(latest.At) > maxReadingAge {
		return Forecast{}, false
	}

	var sum float64
	n := 0
	for i := len(readings) - 1; i >= 0 && latest.At.Sub(readings[i].At) < offsetWindow; i-- {
		if level, ok := f.Level(tides, readings[i].At); ok {
			sum += readings[i].Level - level
			n++
		}
	}
	if n == 0 {
		return Forecast{}, false
	}
	offset := sum / float64(n)

	fc := Forecast{StationID: f.StationID, BasedOn: latest.At, Offset: offset, TideStation: f.TideStation, Lag: f.Lag}
	for t := latest.At.Add(forecastStep); !t.After(latest.At.Add(Horizon)); t = t.Add(forecastStep) {
		level, ok := f.Level(tides, t)
		if !ok {
			return Forecast{}, false
		}
		if fc.PeakAt.IsZero() || level+offset > fc.PeakLevel {
			fc.PeakAt, fc.PeakLevel = t, level+offset
		}
	}
	return fc, true
}
