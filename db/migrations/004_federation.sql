-- +goose Up

CREATE TABLE upstream_idp (
  id                bigserial PRIMARY KEY,
  slug              text NOT NULL UNIQUE,
  display_name      text NOT NULL,
  issuer_url        text NOT NULL,
  client_id         text NOT NULL,
  client_secret_enc bytea NOT NULL,
  secret_nonce      bytea NOT NULL,
  key_version       int NOT NULL DEFAULT 1,
  scopes            text[] NOT NULL DEFAULT ARRAY['openid','profile','email'],
  mode              text NOT NULL CHECK (mode IN ('auto_provision','invite_only','link_only')),
  allowed_domains   text[] NOT NULL DEFAULT ARRAY[]::text[],
  username_claim     text NOT NULL DEFAULT 'preferred_username',
  display_name_claim text NOT NULL DEFAULT 'name',
  email_claim        text NOT NULL DEFAULT 'email',
  disabled          boolean NOT NULL DEFAULT false,
  created_at        timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE account_identity (
  id              bigserial PRIMARY KEY,
  account_id      int NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  -- ON DELETE RESTRICT: deleting an upstream_idp with bound identities
  -- must fail loudly. CASCADE would silently strip a user's only sign-in
  -- method if their account had no other factor — admin must unlink
  -- (or migrate) every user before removing the IdP. Audit finding H2-di.
  upstream_idp_id bigint NOT NULL REFERENCES upstream_idp(id) ON DELETE RESTRICT,
  upstream_iss    text NOT NULL,
  upstream_sub    text NOT NULL,
  upstream_email  text,
  linked_at       timestamptz NOT NULL DEFAULT now(),
  UNIQUE (upstream_iss, upstream_sub)
);
CREATE INDEX account_identity_account_id_idx ON account_identity (account_id);
CREATE INDEX account_identity_idp_id_idx ON account_identity (upstream_idp_id);

ALTER TABLE session
  ADD COLUMN upstream_idp_id bigint REFERENCES upstream_idp(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE session DROP COLUMN IF EXISTS upstream_idp_id;
DROP TABLE IF EXISTS account_identity;
DROP TABLE IF EXISTS upstream_idp;
