-- +goose Up

-- HII's tide prediction stations along the coast and river mouths. Gauges
-- near the Chao Phraya mouth rise and fall with these tides, so a prediction
-- separates what the tide explains from what it does not.
CREATE TABLE tide_stations (
    code          text             PRIMARY KEY CHECK (code ~ '^[A-Z][0-9]{2}$'),
    name          text             NOT NULL CHECK (length(btrim(name)) > 0),
    name_th       text,
    lat           double precision NOT NULL CHECK (lat BETWEEN -90 AND 90),
    lng           double precision NOT NULL CHECK (lng BETWEEN -180 AND 180),
    first_seen_at timestamptz      NOT NULL DEFAULT now(),
    last_seen_at  timestamptz      NOT NULL DEFAULT now()
);

-- Hourly predicted tide in metres on HII's datum, which centres on zero; only
-- the shape and timing are relied on, not the absolute height. A later
-- prediction for the same hour replaces an earlier one.
CREATE TABLE tide_predictions (
    station_code text             NOT NULL REFERENCES tide_stations (code),
    at           timestamptz      NOT NULL,
    level_m      double precision NOT NULL,
    fetched_at   timestamptz      NOT NULL DEFAULT now(),
    PRIMARY KEY (station_code, at)
);

-- +goose Down

DROP TABLE tide_predictions;
DROP TABLE tide_stations;
