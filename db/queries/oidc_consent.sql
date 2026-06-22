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

-- name: ListConsentsByAccount :many
SELECT c.client_id, oc.display_name, c.granted_scopes, c.updated_at
FROM oidc_consent c
JOIN oidc_client oc ON oc.client_id = c.client_id
WHERE c.account_id = $1
ORDER BY oc.display_name;
