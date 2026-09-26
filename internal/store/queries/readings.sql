-- name: InsertWaterReading :execrows
INSERT INTO water_readings (station_id, observed_at, level_msl)
VALUES (sqlc.arg('station_id'), sqlc.arg('observed_at'), sqlc.arg('level_msl'))
ON CONFLICT (station_id, observed_at) DO NOTHING;

-- name: InsertRainReading :execrows
INSERT INTO rain_readings (station_id, observed_at, rain_1h_mm, rain_3h_mm, rain_24h_mm)
VALUES (sqlc.arg('station_id'), sqlc.arg('observed_at'),
        sqlc.narg('rain_1h_mm'), sqlc.narg('rain_3h_mm'), sqlc.narg('rain_24h_mm'))
ON CONFLICT (station_id, observed_at) DO NOTHING;

-- name: ListReportingWaterStations :many
SELECT s.id, s.external_id
  FROM stations s
 WHERE s.source = sqlc.arg('source')
   AND s.kind = 'water'
   AND EXISTS (SELECT 1
                 FROM water_readings w
                WHERE w.station_id = s.id
                  AND w.observed_at >= sqlc.arg('since'))
 ORDER BY s.id;
