-- +goose Up
-- A public client cannot hold a client secret, so PKCE is its only protection
-- of the authorization code: that requirement is protocol-level (OAuth 2.1 /
-- RFC 9700) and not an operator knob. require_pkce IS the knob — it stays
-- writable for confidential clients. client_auth_method is insert-only (no
-- query updates it), so this constraint can never fire on a legal change.
ALTER TABLE oidc_client ADD CONSTRAINT oidc_client_public_requires_pkce_check
  CHECK (require_pkce OR client_auth_method <> 'none');

-- +goose Down
ALTER TABLE oidc_client DROP CONSTRAINT oidc_client_public_requires_pkce_check;
