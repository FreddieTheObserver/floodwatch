-- +goose Up

-- ThaiWater republishes other agencies' gauges, including the BMA's, and each
-- owner is credited by name whenever its data reaches a subscriber.
ALTER TABLE stations ADD COLUMN agency text;

-- +goose Down

ALTER TABLE stations DROP COLUMN agency;
