-- name: HasSAMLConsent :one
SELECT EXISTS (
  SELECT 1 FROM saml_consent WHERE account_id = $1 AND sp_id = $2
) AS has;

-- name: UpsertSAMLConsent :exec
INSERT INTO saml_consent (account_id, sp_id, created_at, updated_at)
VALUES ($1, $2, now(), now())
ON CONFLICT (account_id, sp_id)
DO UPDATE SET updated_at = now();

-- name: DeleteSAMLConsent :exec
DELETE FROM saml_consent WHERE account_id = $1 AND sp_id = $2;

-- name: ListSAMLConsentsByAccount :many
SELECT sc.sp_id, sp.entity_id, sp.display_name, sc.updated_at
FROM saml_consent sc
JOIN saml_sp sp ON sp.id = sc.sp_id
WHERE sc.account_id = $1
ORDER BY sp.display_name;
