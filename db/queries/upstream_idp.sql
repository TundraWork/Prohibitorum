-- name: GetUpstreamIDPBySlug :one
SELECT * FROM upstream_idp WHERE slug = $1 AND NOT disabled;

-- name: ListUpstreamIDPs :many
SELECT * FROM upstream_idp WHERE NOT disabled ORDER BY display_name;

-- name: InsertUpstreamIDP :one
INSERT INTO upstream_idp (
  slug, display_name, protocol, mode, provider_config,
  secret_enc, secret_nonce, key_version, secret_status,
  secret_validated_at, disabled
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: DeleteUpstreamIDP :exec
DELETE FROM upstream_idp WHERE id = $1;

-- name: GetUpstreamIDPBySlugAny :one
SELECT * FROM upstream_idp WHERE slug = $1;

-- name: ListAllUpstreamIDPs :many
SELECT * FROM upstream_idp
WHERE (sqlc.narg('after_created_at')::timestamptz IS NULL OR (created_at, id) < (sqlc.narg('after_created_at'), sqlc.narg('after_id')::int8))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('limit');

-- name: UpdateUpstreamIDPConfig :one
UPDATE upstream_idp SET
  display_name = $2, mode = $3, provider_config = $4
WHERE slug = $1
RETURNING *;

-- name: UpdateUpstreamIDPSecret :one
UPDATE upstream_idp SET
  secret_enc = $2, secret_nonce = $3, key_version = $4,
  secret_status = $5, secret_validated_at = $6
WHERE slug = $1
RETURNING *;

-- name: UpdateUpstreamIDPHealth :one
UPDATE upstream_idp SET secret_status = $2, secret_validated_at = $3
WHERE slug = $1
RETURNING *;

-- name: UpdateVRChatOperatorSecret :one
UPDATE upstream_idp SET
  secret_enc = sqlc.arg('secret_enc'),
  secret_nonce = sqlc.arg('secret_nonce'),
  key_version = sqlc.arg('key_version'),
  secret_status = 'valid',
  secret_validated_at = sqlc.arg('secret_validated_at')
WHERE id = sqlc.arg('id')
  AND slug = sqlc.arg('slug')
  AND protocol = 'vrchat'
RETURNING *;

-- name: UpdateVRChatOperatorHealth :one
UPDATE upstream_idp SET
  secret_status = sqlc.arg('secret_status'),
  secret_validated_at = sqlc.narg('secret_validated_at')
WHERE id = sqlc.arg('id')
  AND slug = sqlc.arg('slug')
  AND protocol = 'vrchat'
RETURNING *;

-- name: SetUpstreamIDPDisabled :one
UPDATE upstream_idp SET disabled = $2 WHERE slug = $1 RETURNING *;
