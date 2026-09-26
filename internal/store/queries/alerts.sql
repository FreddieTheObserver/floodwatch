-- name: ListStationsForAlerts :many
SELECT id, source, kind, name, district, lat, lng, bank_msl, agency
  FROM stations
 ORDER BY id;

-- The lateral joins walk each station's primary key backwards, so these stay
-- cheap however long the reading history grows.

-- name: LatestWaterReadings :many
SELECT s.id AS station_id, r.observed_at, r.level_msl
  FROM stations s
 CROSS JOIN LATERAL (
       SELECT w.observed_at, w.level_msl
         FROM water_readings w
        WHERE w.station_id = s.id
          AND w.observed_at > sqlc.arg('since')
        ORDER BY w.observed_at DESC
        LIMIT 1) r
 WHERE s.kind = 'water';

-- name: RecentWaterReadings :many
SELECT w.station_id, w.observed_at, w.level_msl
  FROM stations s
  JOIN water_readings w
    ON w.station_id = s.id
   AND w.observed_at > sqlc.arg('since')
 WHERE s.kind = 'water'
 ORDER BY w.station_id, w.observed_at;

-- name: LatestRainReadings :many
SELECT s.id AS station_id, r.observed_at, r.rain_1h_mm, r.rain_3h_mm, r.rain_24h_mm
  FROM stations s
 CROSS JOIN LATERAL (
       SELECT rr.observed_at, rr.rain_1h_mm, rr.rain_3h_mm, rr.rain_24h_mm
         FROM rain_readings rr
        WHERE rr.station_id = s.id
          AND rr.observed_at > sqlc.arg('since')
        ORDER BY rr.observed_at DESC
        LIMIT 1) r
 WHERE s.kind = 'rain';

-- name: ListAlertStates :many
SELECT subscription_id, station_id, rule, severity
  FROM alert_states;

-- name: UpsertAlertState :exec
-- A subscription deleted since it was evaluated simply records nothing.
INSERT INTO alert_states (subscription_id, station_id, rule, severity, notified_at)
SELECT sqlc.arg('subscription_id')::bigint, sqlc.narg('station_id')::bigint,
       sqlc.arg('rule')::text, sqlc.arg('severity')::smallint,
       CASE WHEN sqlc.arg('notified')::boolean THEN now() END
 WHERE EXISTS (SELECT 1 FROM subscriptions WHERE id = sqlc.arg('subscription_id')::bigint)
ON CONFLICT ON CONSTRAINT alert_states_key DO UPDATE
   SET severity    = EXCLUDED.severity,
       changed_at  = now(),
       notified_at = COALESCE(EXCLUDED.notified_at, alert_states.notified_at);
