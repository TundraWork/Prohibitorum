-- name: GetAccountRefKey :one
SELECT account_ref_key FROM instance_settings WHERE id = 1;

-- name: InitAccountRefKey :exec
UPDATE instance_settings SET account_ref_key = $1 WHERE id = 1 AND account_ref_key IS NULL;
