-- name: InsertPAT :one
INSERT INTO personal_access_token (
  account_id, name, token_hash, token_hint, all_apps, app_grants, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: RevokePATByID :execrows
UPDATE personal_access_token
SET revoked_at = now()
WHERE id = $1 AND revoked_at IS NULL;

-- name: ListPATsByAccount :many
SELECT * FROM personal_access_token
WHERE account_id = $1 AND revoked_at IS NULL
ORDER BY created_at DESC;

-- name: GetPATByTokenHash :one
SELECT * FROM personal_access_token
WHERE token_hash = $1
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > now());

-- name: RevokePAT :execrows
UPDATE personal_access_token
SET revoked_at = now()
WHERE id = $1 AND account_id = $2 AND revoked_at IS NULL;

-- name: TouchPATLastUsed :exec
UPDATE personal_access_token
SET last_used_at = now()
WHERE id = $1
  AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute');
