-- name: InsertCredentialEvent :exec
INSERT INTO credential_event (account_id, factor, event, credential_ref, ip, user_agent, detail)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListCredentialEventsByAccount :many
SELECT * FROM credential_event
WHERE account_id = $1
ORDER BY at DESC
LIMIT $2 OFFSET $3;

-- name: ListCredentialEventsByFactor :many
SELECT * FROM credential_event
WHERE factor = $1 AND at > $2
ORDER BY at DESC
LIMIT $3;

-- name: ListCredentialEvents :many
SELECT * FROM credential_event
WHERE (sqlc.narg('factor')::text IS NULL OR factor = sqlc.narg('factor'))
  AND (sqlc.narg('event')::text IS NULL OR event = sqlc.narg('event'))
  AND (sqlc.narg('account_id')::int IS NULL OR account_id = sqlc.narg('account_id'))
  AND (sqlc.narg('since')::timestamptz IS NULL OR at >= sqlc.narg('since'))
  AND (sqlc.narg('until')::timestamptz IS NULL OR at <= sqlc.narg('until'))
  AND (sqlc.narg('before_id')::bigint IS NULL OR id < sqlc.narg('before_id'))
ORDER BY id DESC
LIMIT sqlc.arg('lim');
