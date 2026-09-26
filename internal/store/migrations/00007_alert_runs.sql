-- +goose Up

-- When alerts were last checked and delivered. A long gap since then is time
-- floodwatch could not have warned anyone, such as the laptop running it
-- asleep, and subscribers are told about it once it runs again. Only the
-- latest run is kept.
CREATE TABLE alert_runs (
    singleton    boolean     PRIMARY KEY DEFAULT true CHECK (singleton),
    completed_at timestamptz NOT NULL
);

-- +goose Down

DROP TABLE alert_runs;
