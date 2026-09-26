-- name: ListSubscriptions :many
SELECT id, channel, recipient, label, lat, lng, radius_m, created_at
  FROM subscriptions
 ORDER BY id;

-- name: ListRecipientSubscriptions :many
SELECT id, channel, recipient, label, lat, lng, radius_m, created_at
  FROM subscriptions
 WHERE channel = sqlc.arg('channel')
   AND recipient = sqlc.arg('recipient')
 ORDER BY label;

-- name: LockRecipient :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg('channel')::text || ':' || sqlc.arg('recipient')::text, 0));

-- name: GetRecipientSubscription :one
SELECT id, channel, recipient, label, lat, lng, radius_m, created_at
  FROM subscriptions
 WHERE channel = sqlc.arg('channel')
   AND recipient = sqlc.arg('recipient')
   AND label = sqlc.arg('label');

-- name: CountRecipientSubscriptions :one
SELECT count(*)
  FROM subscriptions
 WHERE channel = sqlc.arg('channel')
   AND recipient = sqlc.arg('recipient');

-- name: UpsertSubscription :one
INSERT INTO subscriptions (channel, recipient, label, lat, lng, radius_m)
VALUES (sqlc.arg('channel'), sqlc.arg('recipient'), sqlc.arg('label'),
        sqlc.arg('lat'), sqlc.arg('lng'), sqlc.arg('radius_m'))
ON CONFLICT (channel, recipient, label) DO UPDATE
   SET lat      = EXCLUDED.lat,
       lng      = EXCLUDED.lng,
       radius_m = EXCLUDED.radius_m
RETURNING id, channel, recipient, label, lat, lng, radius_m, created_at;

-- name: RenameRecipientSubscription :execrows
UPDATE subscriptions
   SET label = sqlc.arg('new_label')
 WHERE channel = sqlc.arg('channel')
   AND recipient = sqlc.arg('recipient')
   AND label = sqlc.arg('old_label');

-- name: ResetAlertStates :exec
DELETE FROM alert_states
 WHERE subscription_id = sqlc.arg('subscription_id');

-- name: DeleteRecipientSubscription :execrows
DELETE FROM subscriptions
 WHERE channel = sqlc.arg('channel')
   AND recipient = sqlc.arg('recipient')
   AND label = sqlc.arg('label');

-- name: DeleteRecipient :execrows
DELETE FROM subscriptions
 WHERE channel = sqlc.arg('channel')
   AND recipient = sqlc.arg('recipient');

-- name: GetRecipientLanguage :one
SELECT language
  FROM recipients
 WHERE channel = sqlc.arg('channel')
   AND recipient = sqlc.arg('recipient');

-- name: UpsertRecipientLanguage :exec
INSERT INTO recipients (channel, recipient, language)
VALUES (sqlc.arg('channel'), sqlc.arg('recipient'), sqlc.arg('language'))
ON CONFLICT (channel, recipient) DO UPDATE
   SET language   = EXCLUDED.language,
       updated_at = now();

-- name: DeleteRecipientPreferences :exec
DELETE FROM recipients
 WHERE channel = sqlc.arg('channel')
   AND recipient = sqlc.arg('recipient');
