-- name: ListRecoveryCodesByAccount :many
SELECT * FROM recovery_code
WHERE account_id = $1 AND used_at IS NULL
ORDER BY id;

-- name: InsertRecoveryCode :one
INSERT INTO recovery_code (account_id, hash) VALUES ($1, $2) RETURNING *;

-- name: ConsumeRecoveryCode :one
UPDATE recovery_code
SET used_at = now(), used_session_id = $2, used_ip = $3
WHERE id = $1 AND used_at IS NULL
RETURNING *;

-- name: DeleteAllRecoveryCodesByAccount :exec
DELETE FROM recovery_code WHERE account_id = $1;
