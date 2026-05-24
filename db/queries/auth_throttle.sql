-- name: GetAuthThrottle :one
SELECT * FROM auth_throttle WHERE account_id = $1 AND factor = $2;

-- name: UpsertAuthThrottle :one
INSERT INTO auth_throttle (account_id, factor, failed_attempts, window_start, locked_until)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (account_id, factor) DO UPDATE
SET failed_attempts = EXCLUDED.failed_attempts,
    window_start = EXCLUDED.window_start,
    locked_until = EXCLUDED.locked_until
RETURNING *;

-- name: ResetAuthThrottle :exec
DELETE FROM auth_throttle WHERE account_id = $1 AND factor = $2;
