-- name: GetConsent :one
SELECT granted_scopes FROM oidc_consent
WHERE account_id = $1 AND client_id = $2;

-- name: UpsertConsent :exec
INSERT INTO oidc_consent (account_id, client_id, granted_scopes, created_at, updated_at)
VALUES ($1, $2, $3, now(), now())
ON CONFLICT (account_id, client_id)
DO UPDATE SET granted_scopes = $3, updated_at = now();

-- name: DeleteConsent :exec
DELETE FROM oidc_consent WHERE account_id = $1 AND client_id = $2;
