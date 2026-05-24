-- name: GetPasswordCredential :one
SELECT * FROM password_credential WHERE account_id = $1;

-- name: UpsertPasswordCredential :exec
INSERT INTO password_credential (account_id, hash)
VALUES ($1, $2)
ON CONFLICT (account_id) DO UPDATE
SET hash = EXCLUDED.hash,
    password_changed_at = now(),
    updated_at = now();

-- name: DeletePasswordCredential :exec
DELETE FROM password_credential WHERE account_id = $1;
