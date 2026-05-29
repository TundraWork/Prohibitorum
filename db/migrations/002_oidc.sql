-- +goose Up

-- Protocol-agnostic signing key — same row services OIDC (via JWK) and SAML
-- (via x509_cert_pem). One rotation domain by kid. Unified from day one so
-- SAML and OIDC key rotation share the same subcommand.
CREATE TABLE signing_key (
  kid           text PRIMARY KEY,
  algorithm     text NOT NULL DEFAULT 'RS256',
  use           text NOT NULL DEFAULT 'sig' CHECK (use IN ('sig','enc')),
  public_jwk    jsonb NOT NULL,
  x509_cert_pem text,                              -- populated when used for SAML
  private_pem   text NOT NULL,
  active        boolean NOT NULL DEFAULT false,
  not_before    timestamptz NOT NULL DEFAULT now(),
  created_at    timestamptz NOT NULL DEFAULT now(),
  retired_at    timestamptz
);
-- One active key per use (sig vs enc) at any time.
CREATE UNIQUE INDEX signing_key_one_active ON signing_key (use) WHERE active;

CREATE TABLE oidc_client (
  client_id                       text PRIMARY KEY,
  display_name                    text NOT NULL,
  client_secret_hash              text,            -- argon2id PHC; NULL for public clients
  redirect_uris                   text[] NOT NULL,
  post_logout_redirect_uris       text[] NOT NULL DEFAULT '{}',
  allowed_scopes                  text[] NOT NULL DEFAULT ARRAY['openid','profile'],
  require_pkce                    boolean NOT NULL DEFAULT true,
  allowed_code_challenge_methods  text[] NOT NULL DEFAULT ARRAY['S256'],  -- reject 'plain'
  token_endpoint_auth_method      text NOT NULL DEFAULT 'client_secret_basic',
  id_token_signed_response_alg    text NOT NULL DEFAULT 'RS256',
  subject_type                    text NOT NULL DEFAULT 'public' CHECK (subject_type IN ('public','pairwise')),
  application_type                text NOT NULL DEFAULT 'web' CHECK (application_type IN ('web','native')),
  default_max_age                 int,
  require_auth_time               boolean NOT NULL DEFAULT false,
  contacts                        text[],
  logo_uri                        text,
  tos_uri                         text,
  policy_uri                      text,
  disabled                        boolean NOT NULL DEFAULT false,
  require_consent                 boolean NOT NULL DEFAULT false,
  created_at                      timestamptz NOT NULL DEFAULT now()
);

-- Denylist for self-contained access tokens (RFC 9068 + RFC 7009 §3).
-- Pruning sweep removes rows past expires_at — at that point the JTI would be
-- rejected by signature/exp validation anyway.
CREATE TABLE revoked_jti (
  jti        text PRIMARY KEY,
  expires_at timestamptz NOT NULL,
  reason     text,
  revoked_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX revoked_jti_expires_at_idx ON revoked_jti (expires_at);

-- +goose Down

DROP INDEX IF EXISTS revoked_jti_expires_at_idx;
DROP TABLE IF EXISTS revoked_jti;
DROP TABLE IF EXISTS oidc_client;
DROP INDEX IF EXISTS signing_key_one_active;
DROP TABLE IF EXISTS signing_key;
