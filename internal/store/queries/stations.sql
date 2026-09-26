-- name: UpsertStation :one
-- A feed that briefly omits the bank level must not switch off the overflow
-- rule, so a missing bank keeps the last known one.
INSERT INTO stations (source, kind, external_id, name, name_th, district, lat, lng, bank_msl)
VALUES (sqlc.arg('source'), sqlc.arg('kind'), sqlc.arg('external_id'), sqlc.arg('name'),
        sqlc.narg('name_th'), sqlc.narg('district'), sqlc.arg('lat'), sqlc.arg('lng'),
        sqlc.narg('bank_msl'))
ON CONFLICT (source, kind, external_id) DO UPDATE
   SET name         = EXCLUDED.name,
       name_th      = EXCLUDED.name_th,
       district     = EXCLUDED.district,
       lat          = EXCLUDED.lat,
       lng          = EXCLUDED.lng,
       bank_msl     = COALESCE(EXCLUDED.bank_msl, stations.bank_msl),
       last_seen_at = now()
RETURNING id;
