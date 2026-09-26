-- name: UpsertTideStation :exec
INSERT INTO tide_stations (code, name, name_th, lat, lng)
VALUES (sqlc.arg('code'), sqlc.arg('name'), sqlc.narg('name_th'), sqlc.arg('lat'), sqlc.arg('lng'))
ON CONFLICT (code) DO UPDATE
   SET name         = EXCLUDED.name,
       name_th      = EXCLUDED.name_th,
       lat          = EXCLUDED.lat,
       lng          = EXCLUDED.lng,
       last_seen_at = now();

-- name: UpsertTidePrediction :exec
INSERT INTO tide_predictions (station_code, at, level_m)
VALUES (sqlc.arg('station_code'), sqlc.arg('at'), sqlc.arg('level_m'))
ON CONFLICT (station_code, at) DO UPDATE
   SET level_m    = EXCLUDED.level_m,
       fetched_at = now();
