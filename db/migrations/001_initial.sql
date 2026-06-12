-- +goose Up

-- ---------------------------------------------------------------------------
-- Accounts & federation sources (FK targets first)
-- ---------------------------------------------------------------------------

CREATE TABLE account (
  id                   serial PRIMARY KEY,
  username             text NOT NULL UNIQUE,
  display_name         text NOT NULL,
  webauthn_user_handle bytea NOT NULL UNIQUE,
  oidc_subject         uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
  role                 text NOT NULL DEFAULT 'user' CHECK (role IN ('user','admin')),
  attributes           jsonb NOT NULL DEFAULT '{}'::jsonb,
  disabled             boolean NOT NULL DEFAULT false,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  email                text,
  email_verified       boolean NOT NULL DEFAULT false
);

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
  created_at        timestamptz NOT NULL DEFAULT now(),
  require_verified_email boolean NOT NULL DEFAULT true
);

CREATE TABLE session (
  id              text PRIMARY KEY,
  account_id      integer NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  auth_time       timestamptz NOT NULL,
  amr             text[] NOT NULL DEFAULT '{}',
  acr             text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  revoked_at      timestamptz,
  upstream_idp_id bigint REFERENCES upstream_idp(id) ON DELETE SET NULL
);
CREATE INDEX session_account_id_idx ON session (account_id);

-- ---------------------------------------------------------------------------
-- Credentials, enrollment, audit, throttling
-- ---------------------------------------------------------------------------

CREATE TABLE webauthn_credential (
  id               serial PRIMARY KEY,
  account_id       integer NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  credential_id    bytea NOT NULL UNIQUE,
  public_key       bytea NOT NULL,
  cose_alg         int NOT NULL,
  user_handle      bytea NOT NULL,
  sign_count       bigint NOT NULL DEFAULT 0,
  transports       text[],
  aaguid           bytea,
  attestation_type text,
  backup_eligible  boolean,
  backup_state     boolean,
  uv_initialized   boolean NOT NULL DEFAULT false,
  nickname         text,
  last_used_at     timestamptz,
  clone_warning_at timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX webauthn_credential_account_idx ON webauthn_credential (account_id);
CREATE INDEX webauthn_credential_user_handle_idx ON webauthn_credential (user_handle);

CREATE TABLE enrollment (
  token                      text PRIMARY KEY,
  intent                     text NOT NULL CHECK (intent IN ('bootstrap','invite','reset','add_device')),
  target_account_id          integer REFERENCES account(id) ON DELETE CASCADE,
  template_username          text,
  template_display_name      text,
  template_role              text CHECK (template_role IN ('user','admin')),
  template_attributes        jsonb,
  expected_upstream_idp_slug text,
  created_at                 timestamptz NOT NULL DEFAULT now(),
  expires_at                 timestamptz NOT NULL,
  consumed_at                timestamptz,
  CONSTRAINT enrollment_intent_target_check CHECK (
    ((intent = 'invite') AND (target_account_id IS NULL)) OR (intent <> 'invite')
  ),
  CONSTRAINT enrollment_template_intent_check CHECK (
    (intent = 'invite') OR (
      (template_username IS NULL) AND (template_display_name IS NULL)
      AND (template_role IS NULL) AND (template_attributes IS NULL)
    )
  )
);

CREATE TABLE credential_event (
  id             bigserial PRIMARY KEY,
  account_id     integer REFERENCES account(id) ON DELETE SET NULL,
  factor         text NOT NULL,
  event          text NOT NULL,
  credential_ref bigint,
  ip             inet,
  user_agent     text,
  detail         jsonb,
  at             timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX credential_event_account_at_idx ON credential_event (account_id, at DESC);
CREATE INDEX credential_event_at_idx ON credential_event (at DESC);

CREATE TABLE auth_throttle (
  account_id      integer NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  factor          text NOT NULL,
  failed_attempts int NOT NULL DEFAULT 0,
  window_start    timestamptz NOT NULL DEFAULT now(),
  locked_until    timestamptz,
  PRIMARY KEY (account_id, factor)
);

CREATE TABLE password_credential (
  account_id          int PRIMARY KEY REFERENCES account(id) ON DELETE CASCADE,
  hash                text NOT NULL,
  password_changed_at timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE totp_credential (
  account_id   int PRIMARY KEY REFERENCES account(id) ON DELETE CASCADE,
  secret_enc   bytea NOT NULL,
  secret_nonce bytea NOT NULL,
  key_version  int NOT NULL DEFAULT 1,
  period       int NOT NULL DEFAULT 30,
  digits       int NOT NULL DEFAULT 6,
  algorithm    text NOT NULL DEFAULT 'SHA1',
  last_step    bigint NOT NULL DEFAULT 0,
  confirmed_at timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE recovery_code (
  id              serial PRIMARY KEY,
  account_id      int NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  hash            text NOT NULL,
  used_at         timestamptz,
  used_session_id text REFERENCES session(id) ON DELETE SET NULL,
  used_ip         inet,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX recovery_code_account_id_idx ON recovery_code (account_id);

CREATE TABLE account_identity (
  id              bigserial PRIMARY KEY,
  account_id      int NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  upstream_idp_id bigint NOT NULL REFERENCES upstream_idp(id) ON DELETE RESTRICT,
  upstream_iss    text NOT NULL,
  upstream_sub    text NOT NULL,
  upstream_email  text,
  linked_at       timestamptz NOT NULL DEFAULT now(),
  UNIQUE (upstream_iss, upstream_sub)
);
CREATE INDEX account_identity_account_id_idx ON account_identity (account_id);
CREATE INDEX account_identity_idp_id_idx ON account_identity (upstream_idp_id);

-- ---------------------------------------------------------------------------
-- Signing keys (sealed-only at rest; status is the sole lifecycle)
-- ---------------------------------------------------------------------------

CREATE TABLE signing_key (
  kid               text PRIMARY KEY,
  algorithm         text NOT NULL DEFAULT 'RS256',
  use               text NOT NULL DEFAULT 'sig' CHECK (use IN ('sig','enc')),
  public_jwk        jsonb NOT NULL,
  x509_cert_pem     text,
  private_pem_enc   bytea NOT NULL,
  private_pem_nonce bytea NOT NULL,
  key_version       int NOT NULL DEFAULT 1,
  status            text NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending','active','decommissioning','retired')),
  activated_at      timestamptz,
  decommissioned_at timestamptz,
  retire_after      timestamptz,
  created_at        timestamptz NOT NULL DEFAULT now()
);
-- Exactly one active key per use value.
CREATE UNIQUE INDEX one_active_signing_key ON signing_key (use) WHERE status = 'active';

-- ---------------------------------------------------------------------------
-- OIDC OP
-- ---------------------------------------------------------------------------

CREATE TABLE oidc_client (
  client_id                       text PRIMARY KEY,
  display_name                    text NOT NULL,
  client_secret_hash              text,
  redirect_uris                   text[] NOT NULL,
  post_logout_redirect_uris       text[] NOT NULL DEFAULT '{}',
  allowed_scopes                  text[] NOT NULL DEFAULT ARRAY['openid','profile'],
  require_pkce                    boolean NOT NULL DEFAULT true,
  allowed_code_challenge_methods  text[] NOT NULL DEFAULT ARRAY['S256']
    CHECK (NOT ('plain' = ANY(allowed_code_challenge_methods))),
  token_endpoint_auth_method      text NOT NULL DEFAULT 'client_secret_basic',
  subject_type                    text NOT NULL DEFAULT 'public' CHECK (subject_type IN ('public','pairwise')),
  logo_uri                        text,
  tos_uri                         text,
  policy_uri                      text,
  disabled                        boolean NOT NULL DEFAULT false,
  require_consent                 boolean NOT NULL DEFAULT false,
  created_at                      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE oidc_consent (
  account_id     integer NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  client_id      text NOT NULL REFERENCES oidc_client(client_id) ON DELETE CASCADE,
  granted_scopes text[] NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (account_id, client_id)
);

CREATE TABLE revoked_jti (
  jti        text PRIMARY KEY,
  expires_at timestamptz NOT NULL,
  reason     text,
  revoked_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX revoked_jti_expires_at_idx ON revoked_jti (expires_at);

-- ---------------------------------------------------------------------------
-- SAML IdP
-- ---------------------------------------------------------------------------

CREATE TABLE saml_sp (
  id                      bigserial PRIMARY KEY,
  entity_id               text NOT NULL UNIQUE,
  display_name            text NOT NULL,
  sp_kind                 text,
  name_id_format          text NOT NULL DEFAULT 'urn:oasis:names:tc:SAML:1.1:nameid-format:persistent',
  attribute_map           jsonb NOT NULL DEFAULT '[]'::jsonb,
  require_signed_authn_request boolean NOT NULL DEFAULT false,
  allow_idp_initiated     boolean NOT NULL DEFAULT false,
  session_lifetime        interval,
  metadata_xml            text,
  metadata_valid_until    timestamptz,
  metadata_cache_duration interval,
  metadata_fetched_at     timestamptz,
  created_at              timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE saml_sp_acs (
  sp_id      bigint NOT NULL REFERENCES saml_sp(id) ON DELETE CASCADE,
  idx        int NOT NULL,
  binding    text NOT NULL,
  location   text NOT NULL,
  is_default boolean NOT NULL DEFAULT false,
  PRIMARY KEY (sp_id, idx)
);

CREATE TABLE saml_sp_key (
  id        bigserial PRIMARY KEY,
  sp_id     bigint NOT NULL REFERENCES saml_sp(id) ON DELETE CASCADE,
  use       text NOT NULL CHECK (use IN ('signing','encryption')),
  cert_pem  text NOT NULL,
  not_after timestamptz,
  added_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX saml_sp_key_sp_use_idx ON saml_sp_key (sp_id, use);

CREATE TABLE saml_subject_id (
  account_id     int NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  sp_id          bigint NOT NULL REFERENCES saml_sp(id) ON DELETE CASCADE,
  name_id        text NOT NULL,
  name_id_format text NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (account_id, sp_id)
);

CREATE TABLE saml_session (
  id              bigserial PRIMARY KEY,
  session_id      text NOT NULL REFERENCES session(id) ON DELETE CASCADE,
  sp_id           bigint NOT NULL REFERENCES saml_sp(id) ON DELETE CASCADE,
  name_id         text NOT NULL,
  session_index   text NOT NULL,
  not_on_or_after timestamptz NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (session_id, sp_id, session_index)
);
CREATE INDEX saml_session_session_id_idx ON saml_session (session_id);

-- +goose Down
DROP TABLE IF EXISTS saml_session;
DROP TABLE IF EXISTS saml_subject_id;
DROP TABLE IF EXISTS saml_sp_key;
DROP TABLE IF EXISTS saml_sp_acs;
DROP TABLE IF EXISTS saml_sp;
DROP TABLE IF EXISTS revoked_jti;
DROP TABLE IF EXISTS oidc_consent;
DROP TABLE IF EXISTS oidc_client;
DROP TABLE IF EXISTS signing_key;
DROP TABLE IF EXISTS account_identity;
DROP TABLE IF EXISTS recovery_code;
DROP TABLE IF EXISTS totp_credential;
DROP TABLE IF EXISTS password_credential;
DROP TABLE IF EXISTS auth_throttle;
DROP TABLE IF EXISTS credential_event;
DROP TABLE IF EXISTS enrollment;
DROP TABLE IF EXISTS webauthn_credential;
DROP TABLE IF EXISTS session;
DROP TABLE IF EXISTS upstream_idp;
DROP TABLE IF EXISTS account;
