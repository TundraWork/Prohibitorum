-- name: ListCredentialsByAccount :many
SELECT * FROM webauthn_credential WHERE account_id = $1 ORDER BY created_at DESC;

-- name: ListCredentialsByAccountPage :many
-- Keyset-paginated credentials for an account, ordered by (created_at DESC, id DESC).
-- The after_created_at / after_id pair is the keyset cursor from the last row
-- of the previous page; NULL starts a new page. LIMIT is limit+1 so the handler
-- can detect the presence of a next page without a separate count.
SELECT * FROM webauthn_credential
WHERE account_id = sqlc.arg(account_id)
  AND (sqlc.arg(after_created_at)::timestamptz IS NULL OR (created_at, id) < (sqlc.arg(after_created_at), sqlc.arg(after_id)::integer))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(row_limit);

-- name: GetCredentialByCredentialID :one
SELECT * FROM webauthn_credential WHERE credential_id = $1;

-- name: InsertCredential :one
INSERT INTO webauthn_credential (
  account_id, credential_id, public_key, cose_alg, user_handle, sign_count,
  transports, aaguid, attestation_type, backup_eligible, backup_state,
  uv_initialized, nickname
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING *;

-- name: UpdateCredentialUsage :exec
UPDATE webauthn_credential
SET sign_count = $3, last_used_at = now()
WHERE id = $1 AND account_id = $2;

-- name: SetCredentialCloneWarning :exec
UPDATE webauthn_credential
SET clone_warning_at = now()
WHERE id = $1 AND clone_warning_at IS NULL;

-- name: DeleteCredentialByID :execrows
-- Owner-scoped delete: zero rows affected means the id doesn't match an owned
-- credential; handlers map that to credential_not_found.
DELETE FROM webauthn_credential
WHERE id = $1 AND account_id = $2;

-- name: CountCredentialsByAccount :one
SELECT COUNT(*) FROM webauthn_credential WHERE account_id = $1;

-- name: DeleteAllCredentialsForAccount :exec
DELETE FROM webauthn_credential WHERE account_id = $1;

-- name: UpdateMyCredentialNickname :execrows
-- Owner-scoped update: only the account's own credential row is updated.
-- Zero rows affected means the id doesn't match an owned credential; the
-- handler then surfaces credential_not_found.
UPDATE webauthn_credential
SET nickname = $3
WHERE id = $1 AND account_id = $2;
