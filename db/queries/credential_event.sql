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
SELECT ce.*, a.username AS account_username
FROM credential_event ce
LEFT JOIN account a ON a.id = ce.account_id
WHERE (sqlc.narg('factor')::text IS NULL OR ce.factor = sqlc.narg('factor'))
  AND (sqlc.narg('event')::text IS NULL OR ce.event = sqlc.narg('event'))
  AND (sqlc.narg('account_id')::int IS NULL OR ce.account_id = sqlc.narg('account_id'))
  AND (sqlc.narg('since')::timestamptz IS NULL OR ce.at >= sqlc.narg('since'))
  AND (sqlc.narg('until')::timestamptz IS NULL OR ce.at <= sqlc.narg('until'))
  AND (sqlc.narg('after_id')::bigint IS NULL OR ce.id < sqlc.narg('after_id'))
ORDER BY ce.id DESC
LIMIT sqlc.arg('lim');
