-- name: GetLoginAppearance :one
SELECT login_appearance, unsplash_key_enc, unsplash_key_nonce, unsplash_key_version
FROM instance_settings WHERE id = 1;

-- name: SetLoginAppearance :exec
UPDATE instance_settings SET login_appearance = $1, updated_at = now() WHERE id = 1;

-- name: SetUnsplashKey :exec
UPDATE instance_settings
SET unsplash_key_enc = $1, unsplash_key_nonce = $2, unsplash_key_version = $3, updated_at = now()
WHERE id = 1;

-- name: ClearUnsplashKey :exec
UPDATE instance_settings
SET unsplash_key_enc = NULL, unsplash_key_nonce = NULL, unsplash_key_version = NULL, updated_at = now()
WHERE id = 1;

-- name: LockInstanceSettings :one
SELECT id FROM instance_settings WHERE id = 1 FOR UPDATE;

-- name: ListLoginBackgroundImages :many
SELECT id, etag FROM login_background_image ORDER BY id;

-- name: GetLoginBackgroundImage :one
SELECT data, etag FROM login_background_image WHERE id = $1;

-- name: CountLoginBackgroundImages :one
SELECT count(*) FROM login_background_image;

-- name: InsertLoginBackgroundImage :one
INSERT INTO login_background_image (data, etag) VALUES ($1, $2) RETURNING id;

-- name: DeleteLoginBackgroundImage :execrows
DELETE FROM login_background_image WHERE id = $1;
