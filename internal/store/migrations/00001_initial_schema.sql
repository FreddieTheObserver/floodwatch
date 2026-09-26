-- +goose Up

CREATE TABLE stations (
    id            bigint           GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    source        text             NOT NULL CHECK (source IN ('thaiwater', 'bma')),
    kind          text             NOT NULL CHECK (kind IN ('water', 'rain')),
    external_id   text             NOT NULL CHECK (length(btrim(external_id)) > 0),
    name          text             NOT NULL CHECK (length(btrim(name)) > 0),
    name_th       text,
    district      text,
    lat           double precision NOT NULL CHECK (lat BETWEEN -90 AND 90),
    lng           double precision NOT NULL CHECK (lng BETWEEN -180 AND 180),
    bank_msl      double precision CHECK (bank_msl IS NULL OR kind = 'water'),
    first_seen_at timestamptz      NOT NULL DEFAULT now(),
    last_seen_at  timestamptz      NOT NULL DEFAULT now(),
    UNIQUE (source, kind, external_id)
);

-- Readings are keyed by the source's own observation time, so polling the same
-- reading twice is a no-op and a gap in polling never invents data.
CREATE TABLE water_readings (
    station_id  bigint           NOT NULL REFERENCES stations (id),
    observed_at timestamptz      NOT NULL,
    level_msl   double precision NOT NULL,
    fetched_at  timestamptz      NOT NULL DEFAULT now(),
    PRIMARY KEY (station_id, observed_at)
);

-- Sources report rolling totals, not increments. Windows a source does not
-- publish stay NULL rather than being derived.
CREATE TABLE rain_readings (
    station_id  bigint           NOT NULL REFERENCES stations (id),
    observed_at timestamptz      NOT NULL,
    rain_1h_mm  double precision CHECK (rain_1h_mm >= 0),
    rain_3h_mm  double precision CHECK (rain_3h_mm >= 0),
    rain_24h_mm double precision CHECK (rain_24h_mm >= 0),
    fetched_at  timestamptz      NOT NULL DEFAULT now(),
    PRIMARY KEY (station_id, observed_at),
    CHECK (num_nonnulls(rain_1h_mm, rain_3h_mm, rain_24h_mm) > 0)
);

CREATE TABLE subscriptions (
    id         bigint           GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    channel    text             NOT NULL CHECK (channel IN ('telegram')),
    recipient  text             NOT NULL CHECK (length(btrim(recipient)) > 0),
    label      text             NOT NULL CHECK (length(btrim(label)) > 0),
    lat        double precision NOT NULL CHECK (lat BETWEEN -90 AND 90),
    lng        double precision NOT NULL CHECK (lng BETWEEN -180 AND 180),
    radius_m   integer          NOT NULL DEFAULT 5000 CHECK (radius_m BETWEEN 500 AND 20000),
    created_at timestamptz      NOT NULL DEFAULT now(),
    UNIQUE (channel, recipient, label)
);

-- One row per subscription, station and rule, holding the last severity told to
-- the subscriber. Alerts fire on a change of severity, which is what keeps a
-- slowly rising canal from paging every poll.
CREATE TABLE alert_states (
    subscription_id bigint      NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,
    station_id      bigint      NOT NULL REFERENCES stations (id),
    rule            text        NOT NULL CHECK (length(btrim(rule)) > 0),
    severity        smallint    NOT NULL CHECK (severity >= 0),
    changed_at      timestamptz NOT NULL DEFAULT now(),
    notified_at     timestamptz,
    PRIMARY KEY (subscription_id, station_id, rule)
);

-- +goose Down

DROP TABLE alert_states;
DROP TABLE subscriptions;
DROP TABLE rain_readings;
DROP TABLE water_readings;
DROP TABLE stations;
