-- +goose Up

-- Rain is judged across all gauges around a place, not per gauge, so area rules
-- carry no station. NULLS NOT DISTINCT keeps one row per area rule.
ALTER TABLE alert_states DROP CONSTRAINT alert_states_pkey;
ALTER TABLE alert_states ALTER COLUMN station_id DROP NOT NULL;
ALTER TABLE alert_states
    ADD CONSTRAINT alert_states_key UNIQUE NULLS NOT DISTINCT (subscription_id, rule, station_id),
    ADD CONSTRAINT alert_states_area_rules_have_no_station
        CHECK ((rule IN ('rain', 'rain_stale')) = (station_id IS NULL));

-- +goose Down

DELETE FROM alert_states WHERE station_id IS NULL;
ALTER TABLE alert_states
    DROP CONSTRAINT alert_states_area_rules_have_no_station,
    DROP CONSTRAINT alert_states_key;
ALTER TABLE alert_states ALTER COLUMN station_id SET NOT NULL;
ALTER TABLE alert_states ADD PRIMARY KEY (subscription_id, station_id, rule);
