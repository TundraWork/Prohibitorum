-- name: GetPasswordCredential :one
SELECT * FROM password_credential WHERE account_id = $1;

-- name: UpsertPasswordCredential :exec
INSERT INTO password_credential (account_id, hash)
VALUES ($1, $2)
ON CONFLICT (account_id) DO UPDATE
SET hash = EXCLUDED.hash,
    password_changed_at = now(),
    updated_at = now();

-- name: UpdatePasswordHashOnly :exec
-- Replace the stored hash WITHOUT touching password_changed_at — used by the
-- transparent argon2id param-upgrade rehash on a successful Verify (T4.3a). The
-- secret has not changed, so password_changed_at (which feeds password-age /
-- forced-reauth policy) must not move; only the at-rest encoding did.
UPDATE password_credential
SET hash = $2,
    updated_at = now()
WHERE account_id = $1;

-- name: DeletePasswordCredential :exec
DELETE FROM password_credential WHERE account_id = $1;
