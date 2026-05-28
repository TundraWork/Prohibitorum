-- name: GetUpstreamIDPBySlug :one
SELECT * FROM upstream_idp WHERE slug = $1 AND NOT disabled;

-- name: ListUpstreamIDPs :many
SELECT * FROM upstream_idp WHERE NOT disabled ORDER BY display_name;

-- name: InsertUpstreamIDP :one
INSERT INTO upstream_idp (slug, display_name, issuer_url, client_id,
  client_secret_enc, secret_nonce, key_version, scopes, mode,
  allowed_domains, username_claim, display_name_claim, email_claim,
  require_verified_email)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING *;

-- name: UpdateUpstreamIDP :exec
UPDATE upstream_idp
SET display_name = $2, issuer_url = $3, client_id = $4,
    client_secret_enc = $5, secret_nonce = $6, key_version = $7,
    scopes = $8, mode = $9, allowed_domains = $10,
    username_claim = $11, display_name_claim = $12, email_claim = $13,
    disabled = $14, require_verified_email = $15
WHERE id = $1;

-- name: DeleteUpstreamIDP :exec
DELETE FROM upstream_idp WHERE id = $1;
