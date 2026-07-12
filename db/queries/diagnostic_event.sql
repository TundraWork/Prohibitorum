-- name: InsertDiagnosticEvent :exec
INSERT INTO diagnostic_event (request_id, occurred_at, expires_at, account_id, method, route, operation, code, retryable, fields)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (request_id) DO NOTHING;

-- name: GetDiagnosticEvent :one
-- Exact-ID lookup. Filters on expires_at > now() so expired rows are
-- invisible (return no rows → 404) even before the prune reaper deletes them.
SELECT * FROM diagnostic_event
WHERE request_id = $1 AND expires_at > now();

-- name: DeleteExpiredDiagnosticEvents :execrows
DELETE FROM diagnostic_event WHERE expires_at <= now();
