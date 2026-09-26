-- +goose Up

-- Subscribers are now told about one overall risk per place, stored as an
-- area rule beside the per-gauge states it is judged from.
ALTER TABLE alert_states
    DROP CONSTRAINT alert_states_area_rules_have_no_station,
    ADD CONSTRAINT alert_states_area_rules_have_no_station
        CHECK ((rule IN ('rain', 'rain_stale', 'risk')) = (station_id IS NULL));

-- +goose Down

DELETE FROM alert_states WHERE rule = 'risk';
ALTER TABLE alert_states
    DROP CONSTRAINT alert_states_area_rules_have_no_station,
    ADD CONSTRAINT alert_states_area_rules_have_no_station
        CHECK ((rule IN ('rain', 'rain_stale')) = (station_id IS NULL));
