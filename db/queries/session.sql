-- name: InsertSession :one
INSERT INTO session (id, account_id, auth_time, amr, acr, upstream_idp_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetSession :one
SELECT * FROM session WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeSession :exec
UPDATE session SET revoked_at = now() WHERE id = $1;

-- name: ListSessionsByAccount :many
SELECT * FROM session
WHERE account_id = $1 AND revoked_at IS NULL
ORDER BY created_at DESC;

-- name: RevokeAllSessionsByAccount :exec
UPDATE session SET revoked_at = now()
WHERE account_id = $1 AND revoked_at IS NULL;
