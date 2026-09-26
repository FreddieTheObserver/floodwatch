-- name: ListWaterSeries :many
SELECT w.station_id, w.observed_at, w.level_msl
  FROM water_readings w
  JOIN stations s ON s.id = w.station_id
 WHERE s.kind = 'water'
   AND w.observed_at >= sqlc.arg('since')
 ORDER BY w.station_id, w.observed_at;

-- name: ListTidePredictions :many
SELECT station_code, at, level_m
  FROM tide_predictions
 WHERE at BETWEEN sqlc.arg('from_at') AND sqlc.arg('to_at')
 ORDER BY station_code, at;

-- name: DeleteTideFits :exec
DELETE FROM tide_fits;

-- name: InsertTideFit :exec
INSERT INTO tide_fits (station_id, tide_station, lag_minutes, intercept_m, gain, r,
                       residual_sd_m, samples, fitted_from, fitted_to)
VALUES (sqlc.arg('station_id'), sqlc.arg('tide_station'), sqlc.arg('lag_minutes'),
        sqlc.arg('intercept_m'), sqlc.arg('gain'), sqlc.arg('r'), sqlc.arg('residual_sd_m'),
        sqlc.arg('samples'), sqlc.arg('fitted_from'), sqlc.arg('fitted_to'));

-- name: InsertTideForecast :execrows
INSERT INTO tide_forecasts (station_id, based_on, peak_at, peak_level_msl, offset_m, tide_station, lag_minutes)
VALUES (sqlc.arg('station_id'), sqlc.arg('based_on'), sqlc.arg('peak_at'), sqlc.arg('peak_level_msl'),
        sqlc.arg('offset_m'), sqlc.arg('tide_station'), sqlc.arg('lag_minutes'))
ON CONFLICT (station_id, based_on) DO NOTHING;

-- name: ListLatestTideForecasts :many
SELECT DISTINCT ON (station_id) station_id, based_on, peak_at, peak_level_msl
  FROM tide_forecasts
 WHERE based_on >= sqlc.arg('since')
 ORDER BY station_id, based_on DESC;
