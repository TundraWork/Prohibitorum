-- +goose Up
-- 023_personal_access_token.sql — user-owned Personal Access Tokens (PATs) for
-- programmatic access at the forward-auth gateway. A PAT authenticates AS its
-- owning account with reduced privileges. token_hash = sha256(raw token);
-- token_hint is a non-secret display aid. allowed_client_ids optionally
-- restricts the PAT to specific forward-auth apps (empty = any app the owner may
-- reach). upstream_scopes are opaque labels emitted as the Remote-Scopes header.
CREATE TABLE personal_access_token (
  id                 serial PRIMARY KEY,
  account_id         integer NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  name               text NOT NULL,
  token_hash         bytea NOT NULL UNIQUE,
  token_hint         text NOT NULL,
  upstream_scopes    text[] NOT NULL DEFAULT '{}',
  allowed_client_ids text[] NOT NULL DEFAULT '{}',
  created_at         timestamptz NOT NULL DEFAULT now(),
  expires_at         timestamptz,
  last_used_at       timestamptz,
  revoked_at         timestamptz
);
CREATE INDEX personal_access_token_account_idx ON personal_access_token(account_id);

-- +goose Down
DROP TABLE IF EXISTS personal_access_token;
