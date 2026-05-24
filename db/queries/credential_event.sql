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
