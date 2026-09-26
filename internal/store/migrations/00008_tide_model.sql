-- +goose Up

-- How each water gauge follows the tide over the last few days: its level is
-- modelled as intercept + gain * the tide predicted at tide_station, lag
-- earlier. r says how well that explains the readings; only gauges it
-- explains well are treated as tidal. Every refit replaces all rows.
CREATE TABLE tide_fits (
    station_id    bigint           PRIMARY KEY REFERENCES stations (id) ON DELETE CASCADE,
    tide_station  text             NOT NULL REFERENCES tide_stations (code),
    lag_minutes   integer          NOT NULL CHECK (lag_minutes BETWEEN 0 AND 720),
    intercept_m   double precision NOT NULL,
    gain          double precision NOT NULL,
    r             double precision NOT NULL CHECK (r BETWEEN -1 AND 1),
    residual_sd_m double precision NOT NULL CHECK (residual_sd_m >= 0),
    samples       integer          NOT NULL CHECK (samples > 0),
    fitted_from   timestamptz      NOT NULL,
    fitted_to     timestamptz      NOT NULL,
    fitted_at     timestamptz      NOT NULL DEFAULT now()
);

-- Each forecast of a tidal gauge's highest level over the next hours, made
-- from its reading at based_on and its offset from the tide model then. They
-- are kept to check against what the gauge went on to do, before forecasts
-- may set any risk.
CREATE TABLE tide_forecasts (
    station_id     bigint           NOT NULL REFERENCES stations (id) ON DELETE CASCADE,
    based_on       timestamptz      NOT NULL,
    peak_at        timestamptz      NOT NULL CHECK (peak_at > based_on),
    peak_level_msl double precision NOT NULL,
    offset_m       double precision NOT NULL,
    tide_station   text             NOT NULL REFERENCES tide_stations (code),
    lag_minutes    integer          NOT NULL CHECK (lag_minutes BETWEEN 0 AND 720),
    made_at        timestamptz      NOT NULL DEFAULT now(),
    PRIMARY KEY (station_id, based_on)
);

-- +goose Down

DROP TABLE tide_forecasts;
DROP TABLE tide_fits;
