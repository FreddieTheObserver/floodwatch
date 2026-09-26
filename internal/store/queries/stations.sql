-- name: UpsertStation :one
-- A feed that briefly omits the bank level must not switch off the overflow
-- rule, so a missing bank keeps the last known one.
INSERT INTO stations (source, kind, external_id, name, name_th, district, lat, lng, bank_msl, agency)
VALUES (sqlc.arg('source'), sqlc.arg('kind'), sqlc.arg('external_id'), sqlc.arg('name'),
        sqlc.narg('name_th'), sqlc.narg('district'), sqlc.arg('lat'), sqlc.arg('lng'),
        sqlc.narg('bank_msl'), sqlc.narg('agency'))
ON CONFLICT (source, kind, external_id) DO UPDATE
   SET name         = EXCLUDED.name,
       name_th      = EXCLUDED.name_th,
       district     = EXCLUDED.district,
       lat          = EXCLUDED.lat,
       lng          = EXCLUDED.lng,
       bank_msl     = COALESCE(EXCLUDED.bank_msl, stations.bank_msl),
       agency       = COALESCE(EXCLUDED.agency, stations.agency),
       last_seen_at = now()
RETURNING id;
