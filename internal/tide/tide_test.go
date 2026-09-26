package tide

import (
	"context"
	"io"
	"log/slog"
	"math"
	"testing"
	"time"
)

var (
	ict = time.FixedZone("ICT", 7*60*60)
	now = time.Date(2026, 9, 27, 9, 0, 0, 0, ict)
)

// astronomical is a tide with the Gulf of Thailand's daily and twice-daily
// cycles, shifted by some hours.
func astronomical(shift time.Duration) func(time.Time) float64 {
	return func(t time.Time) float64 {
		h := t.Sub(now).Hours() - shift.Hours()
		return 0.6*math.Cos(2*math.Pi*h/24.84) + 0.3*math.Cos(2*math.Pi*h/12.42+1)
	}
}

// hourly predicts a tide every hour over the days around now.
func hourly(tide func(time.Time) float64) Series {
	var s Series
	for t := now.Add(-4 * 24 * time.Hour); !t.After(now.Add(24 * time.Hour)); t = t.Add(time.Hour) {
		s = append(s, Point{At: t, Level: tide(t)})
	}
	return s
}

// gauge reads level(t) every 10 minutes for span up to now.
func gauge(span time.Duration, level func(time.Time) float64) []Point {
	var out []Point
	for t := now.Add(-span); !t.After(now); t = t.Add(10 * time.Minute) {
		out = append(out, Point{At: t, Level: level(t)})
	}
	return out
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestSeriesInterpolatesBetweenHours(t *testing.T) {
	s := Series{{now, 0.2}, {now.Add(time.Hour), 0.6}, {now.Add(4 * time.Hour), 1.0}}
	for _, c := range []struct {
		at   time.Time
		want float64
		ok   bool
	}{
		{now, 0.2, true},
		{now.Add(15 * time.Minute), 0.3, true},
		{now.Add(time.Hour), 0.6, true},
		{now.Add(-time.Minute), 0, false},
		{now.Add(2 * time.Hour), 0, false}, // inside a three-hour gap
		{now.Add(5 * time.Hour), 0, false},
	} {
		got, ok := s.At(c.at)
		if ok != c.ok || !near(got, c.want, 1e-9) {
			t.Errorf("At(%v) = %v, %v; want %v, %v", c.at.Format("15:04"), got, ok, c.want, c.ok)
		}
	}
}

func TestFitFindsTheTideAndTheDelay(t *testing.T) {
	harbour, mouth := astronomical(0), astronomical(3*time.Hour)
	tides := map[string]Series{"N02": hourly(harbour), "N05": hourly(mouth)}
	readings := gauge(3*24*time.Hour, func(t time.Time) float64 { return 0.5 + 1.2*harbour(t.Add(-40*time.Minute)) })

	f, ok := FitGauge(7, readings, tides)
	if !ok {
		t.Fatal("no fit")
	}
	if f.StationID != 7 || f.TideStation != "N02" || f.Lag != 40*time.Minute {
		t.Errorf("fit = %s %v for station %d, want N02 40m for 7", f.TideStation, f.Lag, f.StationID)
	}
	// Hourly predictions interpolated in between explain the gauge almost,
	// but not quite, exactly.
	if !near(f.Gain, 1.2, 0.02) || !near(f.Intercept, 0.5, 0.02) || f.R < 0.999 || f.ResidualSD > 0.01 {
		t.Errorf("fit = %+v, want level 0.5 + 1.2 x tide", f)
	}
	if !f.Tidal() || !f.From.Equal(readings[0].At) || !f.To.Equal(now) || f.Samples != len(readings) {
		t.Errorf("fit = %+v", f)
	}
}

func TestAGaugeTheTideDoesNotExplainIsNotTidal(t *testing.T) {
	tides := map[string]Series{"N02": hourly(astronomical(0))}
	// A canal behind pumps and gates, filling steadily in the rain.
	readings := gauge(3*24*time.Hour, func(t time.Time) float64 { return 1 + 0.01*t.Sub(now).Hours() })

	f, ok := FitGauge(7, readings, tides)
	if ok && f.Tidal() {
		t.Errorf("fit = %+v; a steady rise was taken for the tide", f)
	}
}

func TestAFitNeedsADayAndAHalf(t *testing.T) {
	tide := astronomical(0)
	tides := map[string]Series{"N02": hourly(tide)}
	readings := gauge(30*time.Hour, func(t time.Time) float64 { return tide(t) })
	if f, ok := FitGauge(7, readings, tides); ok {
		t.Errorf("fitted %+v from 30 hours of readings", f)
	}
}

func TestForecastAddsTheOffsetFromTheTide(t *testing.T) {
	// High water comes a few hours from now.
	tides := hourly(astronomical(3 * time.Hour))
	fit := Fit{StationID: 7, TideStation: "N02", Lag: 40 * time.Minute, Intercept: 0.5, Gain: 1.2, R: 0.9}
	// Flood water has lifted the gauge 30 cm above what the tide explains.
	readings := gauge(3*time.Hour, func(t time.Time) float64 {
		level, _ := fit.Level(tides, t)
		return level + 0.3
	})

	fc, ok := Predict(fit, readings, tides, now.Add(5*time.Minute))
	if !ok {
		t.Fatal("no forecast")
	}
	if !near(fc.Offset, 0.3, 0.001) || !fc.BasedOn.Equal(now) || fc.TideStation != "N02" || fc.Lag != 40*time.Minute {
		t.Errorf("forecast = %+v", fc)
	}
	var peakAt time.Time
	peak := math.Inf(-1)
	for at := now.Add(10 * time.Minute); !at.After(now.Add(Horizon)); at = at.Add(10 * time.Minute) {
		if level, _ := fit.Level(tides, at); level > peak {
			peak, peakAt = level, at
		}
	}
	if peakAt.Sub(now) < time.Hour || peakAt.Sub(now) > 5*time.Hour {
		t.Fatalf("the test tide peaks at %v, not mid-horizon", peakAt.Format("15:04"))
	}
	if !fc.PeakAt.Equal(peakAt) || !near(fc.PeakLevel, peak+0.3, 0.001) {
		t.Errorf("peak = %.3f at %v, want %.3f at %v", fc.PeakLevel, fc.PeakAt.Format("15:04"), peak+0.3, peakAt.Format("15:04"))
	}
}

func TestNoForecastWithoutACurrentReadingOrATidalFit(t *testing.T) {
	tides := hourly(astronomical(0))
	fit := Fit{TideStation: "N02", Gain: 1, R: 0.9}
	readings := gauge(3*time.Hour, func(time.Time) float64 { return 1 })

	if _, ok := Predict(fit, readings, tides, now.Add(40*time.Minute)); ok {
		t.Error("forecast from a reading 40 minutes old")
	}
	loose := fit
	loose.R = 0.6
	if _, ok := Predict(loose, readings, tides, now); ok {
		t.Error("forecast from a gauge that is not tidal")
	}
	if _, ok := Predict(fit, readings, tides[:len(tides)-22], now); ok {
		t.Error("forecast past the end of the tide predictions")
	}
}

type memStore struct {
	water     map[int64][]Point
	tides     map[string]Series
	fits      []Fit
	forecasts []Forecast
}

func (m *memStore) WaterSeries(context.Context, time.Time) (map[int64][]Point, error) {
	return m.water, nil
}
func (m *memStore) TidePredictions(context.Context, time.Time, time.Time) (map[string]Series, error) {
	return m.tides, nil
}
func (m *memStore) ReplaceTideFits(_ context.Context, fits []Fit) error {
	m.fits = fits
	return nil
}
func (m *memStore) SaveTideForecasts(_ context.Context, fcs []Forecast) error {
	m.forecasts = fcs
	return nil
}

func TestUpdateFitsEveryGaugeAndForecastsTheTidalOnes(t *testing.T) {
	tide := astronomical(0)
	m := &memStore{
		tides: map[string]Series{"N02": hourly(tide)},
		water: map[int64][]Point{
			7: gauge(3*24*time.Hour, func(t time.Time) float64 { return 0.5 + tide(t.Add(-time.Hour)) }),
			8: gauge(3*24*time.Hour, func(t time.Time) float64 { return 1 + 0.01*t.Sub(now).Hours() }),
			9: gauge(time.Hour, func(time.Time) float64 { return 1 }),
		},
	}
	model := NewModel(m, slog.New(slog.NewTextHandler(io.Discard, nil)))
	model.now = func() time.Time { return now.Add(5 * time.Minute) }

	if err := model.Update(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(m.fits) != 2 || m.fits[0].StationID != 7 || m.fits[1].StationID != 8 {
		t.Errorf("fits = %+v, want the two gauges with enough readings", m.fits)
	}
	if len(m.forecasts) != 1 || m.forecasts[0].StationID != 7 {
		t.Errorf("forecasts = %+v, want one for the tidal gauge", m.forecasts)
	}
}
