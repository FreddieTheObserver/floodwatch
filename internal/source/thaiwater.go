package source

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const thaiWaterBase = "https://api-v3.thaiwater.net/api/v1/thaiwater30"

type thaiWaterName struct {
	EN string `json:"en"`
	TH string `json:"th"`
}

type thaiWaterStation struct {
	ID      int64         `json:"id"`
	Name    thaiWaterName `json:"tele_station_name"`
	Lat     num           `json:"tele_station_lat"`
	Lng     num           `json:"tele_station_long"`
	MinBank num           `json:"min_bank"`
}

type thaiWaterGeocode struct {
	Amphoe thaiWaterName `json:"amphoe_name"`
}

type thaiWaterAgency struct {
	ShortName thaiWaterName `json:"agency_shortname"`
}

func (s thaiWaterStation) normalise(g thaiWaterGeocode, a thaiWaterAgency, withBank bool) Station {
	st := Station{
		ExternalID: strconv.FormatInt(s.ID, 10),
		Name:       firstNonEmpty(s.Name.EN, s.Name.TH),
		NameTH:     firstNonEmpty(s.Name.TH),
		District:   firstNonEmpty(g.Amphoe.EN, g.Amphoe.TH),
		Lat:        s.Lat.v,
		Lng:        s.Lng.v,
		Agency:     firstNonEmpty(a.ShortName.EN, a.ShortName.TH),
	}
	if withBank {
		st.BankMSL = s.MinBank.ptr()
	}
	return st
}

func (s thaiWaterStation) usable() bool {
	return s.ID > 0 && validCoords(s.Lat, s.Lng) && firstNonEmpty(s.Name.EN, s.Name.TH) != ""
}

func parseThaiWaterTime(s string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02 15:04", s, ict)
}

// ThaiWaterLevels fetches the telemetered water level stations of one province.
func ThaiWaterLevels(client *http.Client, provinceCode string) Fetcher {
	return thaiWaterLevels{client: client, province: provinceCode}
}

type thaiWaterLevels struct {
	client   *http.Client
	province string
}

func (f thaiWaterLevels) Name() string { return "thaiwater-water-" + f.province }

func (f thaiWaterLevels) Fetch(ctx context.Context) (Batch, error) {
	var resp thaiWaterLevelResponse
	u := thaiWaterBase + "/provinces/waterlevel?province_code=" + url.QueryEscape(f.province)
	if err := fetchJSON(ctx, f.client, http.MethodGet, u, &resp); err != nil {
		return Batch{}, err
	}
	return resp.batch(time.Now()), nil
}

type thaiWaterLevelResponse struct {
	Data []struct {
		DateTime string           `json:"waterlevel_datetime"`
		LevelMSL num              `json:"waterlevel_msl"`
		Station  thaiWaterStation `json:"station"`
		Geocode  thaiWaterGeocode `json:"geocode"`
		Agency   thaiWaterAgency  `json:"agency"`
	} `json:"data"`
}

func (r thaiWaterLevelResponse) batch(now time.Time) Batch {
	b := Batch{Source: "thaiwater", Kind: KindWater}
	for _, row := range r.Data {
		if !row.Station.usable() {
			b.Skipped++
			continue
		}
		st := row.Station.normalise(row.Geocode, row.Agency, true)
		b.Stations = append(b.Stations, st)

		at, err := parseThaiWaterTime(row.DateTime)
		if err != nil || at.After(now.Add(maxClockSkew)) || !row.LevelMSL.ok {
			b.Skipped++
			continue
		}
		b.Water = append(b.Water, WaterReading{ExternalID: st.ExternalID, ObservedAt: at, LevelMSL: row.LevelMSL.v})
	}
	return b
}

// ThaiWaterRain fetches the telemetered rain gauges of one province.
func ThaiWaterRain(client *http.Client, provinceCode string) Fetcher {
	return thaiWaterRain{client: client, province: provinceCode}
}

type thaiWaterRain struct {
	client   *http.Client
	province string
}

func (f thaiWaterRain) Name() string { return "thaiwater-rain-" + f.province }

func (f thaiWaterRain) Fetch(ctx context.Context) (Batch, error) {
	var resp thaiWaterRainResponse
	u := thaiWaterBase + "/provinces/rain24?include_zero=1&province_code=" + url.QueryEscape(f.province)
	if err := fetchJSON(ctx, f.client, http.MethodGet, u, &resp); err != nil {
		return Batch{}, err
	}
	return resp.batch(time.Now()), nil
}

type thaiWaterRainResponse struct {
	Data []struct {
		DateTime string           `json:"rainfall_datetime"`
		Rain1h   num              `json:"rain_1h"`
		Rain24h  num              `json:"rain_24h"`
		Station  thaiWaterStation `json:"station"`
		Geocode  thaiWaterGeocode `json:"geocode"`
		Agency   thaiWaterAgency  `json:"agency"`
	} `json:"data"`
}

func (r thaiWaterRainResponse) batch(now time.Time) Batch {
	b := Batch{Source: "thaiwater", Kind: KindRain}
	for _, row := range r.Data {
		if !row.Station.usable() {
			b.Skipped++
			continue
		}
		st := row.Station.normalise(row.Geocode, row.Agency, false)
		b.Stations = append(b.Stations, st)

		at, err := parseThaiWaterTime(row.DateTime)
		rain := RainReading{
			ExternalID: st.ExternalID,
			ObservedAt: at,
			Rain1h:     row.Rain1h.nonNegative(),
			Rain24h:    row.Rain24h.nonNegative(),
		}
		if err != nil || at.After(now.Add(maxClockSkew)) || (rain.Rain1h == nil && rain.Rain24h == nil) {
			b.Skipped++
			continue
		}
		b.Rain = append(b.Rain, rain)
	}
	return b
}
