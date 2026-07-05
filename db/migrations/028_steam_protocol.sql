-- +goose Up
-- protocol discriminates the upstream federation protocol: 'oidc' (issuer/client/
-- token exchange, the existing rows) or 'steam' (OpenID 2.0 + Steam Web API). Steam
-- rows leave the OIDC-only columns as empty sentinels ('' / '{}') and carry an
-- encrypted Steam Web API key in the existing client_secret_enc slot.
ALTER TABLE upstream_idp
  ADD COLUMN IF NOT EXISTS protocol text NOT NULL DEFAULT 'oidc'
    CHECK (protocol IN ('oidc', 'steam'));

-- +goose Down
ALTER TABLE upstream_idp DROP COLUMN IF EXISTS protocol;
