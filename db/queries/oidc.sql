-- name: GetOIDCClient :one
SELECT * FROM oidc_client WHERE client_id = $1;

-- name: ListOIDCClients :many
SELECT * FROM oidc_client ORDER BY display_name ASC;

-- name: InsertOIDCClient :one
INSERT INTO oidc_client (client_id, client_secret_hash, display_name, redirect_uris, allowed_scopes)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: DeleteOIDCClient :exec
DELETE FROM oidc_client WHERE client_id = $1;

-- name: GetActiveSigningKey :one
SELECT * FROM oidc_signing_key WHERE active = TRUE LIMIT 1;

-- name: ListVerifyingSigningKeys :many
-- Every non-retired key: the active signing key + any keys still inside
-- their rollover window. JWKS endpoint serves the public_jwk of each.
SELECT * FROM oidc_signing_key
WHERE retired_at IS NULL OR retired_at > now()
ORDER BY created_at DESC;

-- name: InsertSigningKey :one
INSERT INTO oidc_signing_key (kid, algorithm, public_jwk, private_pem, active)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: DeactivateAllSigningKeys :exec
UPDATE oidc_signing_key SET active = FALSE WHERE active = TRUE;

-- name: RetireSigningKey :exec
UPDATE oidc_signing_key SET retired_at = $2 WHERE kid = $1;
