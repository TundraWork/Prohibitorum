-- +goose Up

-- OIDC tables. v1 surface is intentionally minimal:
--   * No dynamic registration — admin inserts client rows manually.
--   * RS256 only.
--   * Authorization codes + refresh tokens live in KV (ephemeral) not here.
--   * Signing key rotation = insert new row + flip the active flag.

CREATE TABLE oidc_client (
  client_id           TEXT PRIMARY KEY,
  client_secret_hash  TEXT,         -- argon2id; NULL for public (PKCE-only) clients
  display_name        TEXT NOT NULL,
  redirect_uris       TEXT[] NOT NULL,
  allowed_scopes      TEXT[] NOT NULL DEFAULT ARRAY['openid','profile']::TEXT[],
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT redirect_uris_nonempty CHECK (array_length(redirect_uris, 1) > 0)
);

CREATE TABLE oidc_signing_key (
  kid          TEXT PRIMARY KEY,
  algorithm    TEXT NOT NULL CHECK (algorithm = 'RS256'),
  public_jwk   JSONB NOT NULL,
  private_pem  BYTEA NOT NULL,
  active       BOOLEAN NOT NULL DEFAULT FALSE,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  retired_at   TIMESTAMPTZ
);
-- At most one active key at a time; multiple non-active keys remain to
-- verify tokens issued before rotation, until retired_at < now() - max(TTL).
CREATE UNIQUE INDEX oidc_signing_key_active ON oidc_signing_key (active) WHERE active = TRUE;

-- +goose Down

DROP INDEX IF EXISTS oidc_signing_key_active;
DROP TABLE IF EXISTS oidc_signing_key;
DROP TABLE IF EXISTS oidc_client;
