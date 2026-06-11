-- name: GetOIDCClient :one
SELECT * FROM oidc_client WHERE client_id = $1 AND disabled = false;

-- name: GetOIDCClientAny :one
SELECT * FROM oidc_client WHERE client_id = $1;

-- name: UpdateOIDCClient :one
UPDATE oidc_client SET
  display_name = $2, redirect_uris = $3, post_logout_redirect_uris = $4,
  allowed_scopes = $5, require_consent = $6, disabled = $7
WHERE client_id = $1
RETURNING *;

-- name: UpdateOIDCClientSecret :exec
UPDATE oidc_client SET client_secret_hash = $2 WHERE client_id = $1;

-- name: DeleteOIDCClient :execrows
DELETE FROM oidc_client WHERE client_id = $1;

-- name: InsertOIDCClient :one
INSERT INTO oidc_client (
  client_id, display_name, client_secret_hash, redirect_uris,
  post_logout_redirect_uris, allowed_scopes, require_pkce,
  allowed_code_challenge_methods, token_endpoint_auth_method,
  subject_type, require_consent
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
RETURNING *;

-- name: ListOIDCClients :many
SELECT client_id, display_name, redirect_uris, allowed_scopes,
       token_endpoint_auth_method, disabled, created_at
FROM oidc_client ORDER BY created_at DESC;

-- name: GetAccountByOIDCSubject :one
SELECT * FROM account WHERE oidc_subject = $1;

-- name: GetActiveSigningKey :one
SELECT * FROM signing_key WHERE use = 'sig' AND status = 'active';

-- name: ListPublishableSigningKeys :many
SELECT * FROM signing_key
WHERE use = 'sig' AND status IN ('pending','active','decommissioning')
ORDER BY created_at DESC;

-- name: ListAllSigningKeys :many
SELECT * FROM signing_key WHERE use = 'sig' ORDER BY created_at DESC;

-- name: GetSigningKeyByKID :one
SELECT * FROM signing_key WHERE kid = $1;

-- name: InsertPendingSigningKey :one
INSERT INTO signing_key (kid, algorithm, use, public_jwk, x509_cert_pem, private_pem, active, status, not_before)
VALUES ($1,$2,'sig',$3,$4,$5,false,'pending', now())
RETURNING *;

-- name: DemoteActiveSigningKey :exec
UPDATE signing_key
SET status='decommissioning', active=false, decommissioned_at=now(), retire_after=$1
WHERE use='sig' AND status='active';

-- name: PromoteSigningKey :one
UPDATE signing_key
SET status='active', active=true, activated_at=now()
WHERE kid=$1 AND status='pending'
RETURNING *;

-- name: RetireSigningKey :one
UPDATE signing_key
SET status='decommissioning', active=false,
    decommissioned_at=COALESCE(decommissioned_at, now()), retire_after=$2
WHERE kid=$1 AND status IN ('pending','decommissioning')
RETURNING *;

-- name: ReconcileRetiredSigningKeys :execrows
UPDATE signing_key SET status='retired'
WHERE use='sig' AND status='decommissioning'
  AND retire_after IS NOT NULL AND now() >= retire_after;

-- name: InsertRevokedJTI :exec
INSERT INTO revoked_jti (jti, expires_at, reason) VALUES ($1,$2,$3)
ON CONFLICT (jti) DO NOTHING;

-- name: IsJTIRevoked :one
SELECT EXISTS (SELECT 1 FROM revoked_jti WHERE jti = $1);

-- name: PruneExpiredRevokedJTI :exec
DELETE FROM revoked_jti WHERE expires_at < now();
