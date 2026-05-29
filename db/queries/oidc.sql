-- name: GetOIDCClient :one
SELECT * FROM oidc_client WHERE client_id = $1 AND disabled = false;

-- name: InsertOIDCClient :one
INSERT INTO oidc_client (
  client_id, display_name, client_secret_hash, redirect_uris,
  post_logout_redirect_uris, allowed_scopes, require_pkce,
  allowed_code_challenge_methods, token_endpoint_auth_method,
  subject_type, application_type, require_consent
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
RETURNING *;

-- name: ListOIDCClients :many
SELECT client_id, display_name, redirect_uris, allowed_scopes,
       token_endpoint_auth_method, disabled, created_at
FROM oidc_client ORDER BY created_at DESC;

-- name: GetAccountByOIDCSubject :one
SELECT * FROM account WHERE oidc_subject = $1;

-- name: GetActiveSigningKey :one
SELECT * FROM signing_key WHERE use = 'sig' AND active = true;

-- name: ListActiveSigningKeys :many
SELECT * FROM signing_key WHERE use = 'sig' AND retired_at IS NULL ORDER BY created_at DESC;

-- name: InsertSigningKey :one
INSERT INTO signing_key (kid, algorithm, use, public_jwk, x509_cert_pem, private_pem, active, not_before)
VALUES ($1,$2,'sig',$3,$4,$5,$6, now())
RETURNING *;

-- name: DeactivateSigningKeys :exec
UPDATE signing_key SET active = false WHERE use = 'sig' AND active = true;

-- name: RetireSigningKey :exec
UPDATE signing_key SET active = false, retired_at = now() WHERE kid = $1;

-- name: InsertRevokedJTI :exec
INSERT INTO revoked_jti (jti, expires_at, reason) VALUES ($1,$2,$3)
ON CONFLICT (jti) DO NOTHING;

-- name: IsJTIRevoked :one
SELECT EXISTS (SELECT 1 FROM revoked_jti WHERE jti = $1);

-- name: PruneExpiredRevokedJTI :exec
DELETE FROM revoked_jti WHERE expires_at < now();
