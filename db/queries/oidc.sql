-- name: GetOIDCClient :one
SELECT * FROM oidc_client WHERE client_id = $1 AND NOT disabled;

-- name: ListOIDCClients :many
SELECT * FROM oidc_client ORDER BY display_name ASC;

-- name: InsertOIDCClient :one
INSERT INTO oidc_client (
  client_id, display_name, client_secret_hash, redirect_uris,
  post_logout_redirect_uris, allowed_scopes, require_pkce,
  allowed_code_challenge_methods, token_endpoint_auth_method,
  id_token_signed_response_alg, subject_type, application_type,
  default_max_age, require_auth_time, contacts, logo_uri, tos_uri,
  policy_uri, disabled
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
RETURNING *;

-- name: DeleteOIDCClient :exec
DELETE FROM oidc_client WHERE client_id = $1;

-- name: GetActiveSigningKey :one
SELECT * FROM signing_key
WHERE active AND use = $1
LIMIT 1;

-- name: ListVerifyingSigningKeys :many
-- Every non-retired key: the active signing key + any keys still inside
-- their rollover window. JWKS endpoint serves the public_jwk of each.
SELECT * FROM signing_key
WHERE retired_at IS NULL OR retired_at > $1
ORDER BY created_at DESC;

-- name: InsertSigningKey :one
INSERT INTO signing_key (kid, algorithm, use, public_jwk, x509_cert_pem, private_pem, active, not_before)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: DeactivateAllSigningKeys :exec
UPDATE signing_key SET active = FALSE WHERE active = TRUE;

-- name: RetireSigningKey :exec
UPDATE signing_key SET retired_at = $2 WHERE kid = $1;

-- name: RevokeJTI :exec
INSERT INTO revoked_jti (jti, expires_at, reason) VALUES ($1, $2, $3)
ON CONFLICT (jti) DO NOTHING;

-- name: IsJTIRevoked :one
SELECT EXISTS(SELECT 1 FROM revoked_jti WHERE jti = $1) AS revoked;

-- name: PruneRevokedJTI :exec
DELETE FROM revoked_jti WHERE expires_at < now();
