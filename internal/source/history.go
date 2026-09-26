package source

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// ThaiWaterHistory reads water gauges' past readings from ThaiWater: the
// series behind the graphs on its website. The latest-reading feeds miss
// whatever happened while floodwatch was not running, and comparing a gauge
// with the tide needs days of unbroken readings.
func ThaiWaterHistory(client *http.Client) History {
	return History{client: client, base: thaiWaterBase}
}

type History struct {
	client *http.Client
	base   string
}

// Water returns a ThaiWater gauge's readings from the start of from's day to
// to. Gaps in the gauge's record are simply absent.
func (h History) Water(ctx context.Context, externalID string, from, to time.Time) ([]WaterReading, error) {
	q := url.Values{
		"station_type": {"tele_waterlevel"},
		"station_id":   {externalID},
		"start_date":   {from.In(ict).Format(time.DateOnly)},
		"end_date":     {to.In(ict).Format("2006-01-02 15:04")},
	}
	var resp thaiWaterGraphResponse
	if err := fetchJSON(ctx, h.client, http.MethodGet, h.base+"/public/waterlevel_graph?"+q.Encode(), &resp); err != nil {
		return nil, err
	}
	if resp.Result != "OK" {
		return nil, fmt.Errorf("history for ThaiWater station %s: result %q", externalID, resp.Result)
	}
	return resp.readings(externalID, time.Now()), nil
}

type thaiWaterGraphResponse struct {
	Result string `json:"result"`
	Data   struct {
		GraphData []struct {
			DateTime string `json:"datetime"`
			Value    num    `json:"value"`
		} `json:"graph_data"`
	} `json:"data"`
}

func (r thaiWaterGraphResponse) readings(externalID string, now time.Time) []WaterReading {
	var out []WaterReading
	for _, p := range r.Data.GraphData {
		at, err := parseThaiWaterTime(p.DateTime)
		if err != nil || !p.Value.ok || at.After(now.Add(maxClockSkew)) {
			continue
		}
		out = append(out, WaterReading{ExternalID: externalID, ObservedAt: at, LevelMSL: p.Value.v})
	}
	return out
}
