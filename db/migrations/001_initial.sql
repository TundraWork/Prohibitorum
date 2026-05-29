-- +goose Up

CREATE TABLE account (
  id                   SERIAL PRIMARY KEY,
  username             text NOT NULL UNIQUE,
  display_name         text NOT NULL,
  webauthn_user_handle bytea NOT NULL UNIQUE,
  oidc_subject         uuid NOT NULL UNIQUE DEFAULT gen_random_uuid(),
  role                 text NOT NULL DEFAULT 'user' CHECK (role IN ('user','admin')),
  attributes           jsonb NOT NULL DEFAULT '{}'::jsonb,
  disabled             boolean NOT NULL DEFAULT false,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now()
);

-- PG-persisted authentication facts; doubles as the OIDC `sid` claim. KV-stored
-- session state (sliding refresh, last activity) is keyed on session.id and
-- holds ephemeral fields; this table holds the immutable "moment of auth."
CREATE TABLE session (
  id              text PRIMARY KEY,
  account_id      integer NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  auth_time       timestamptz NOT NULL,
  amr             text[] NOT NULL DEFAULT '{}',  -- 'pwd','otp','mfa','hwk','user',etc.
  acr             text,
  -- upstream_idp_id added in migration 004 (forward FK)
  created_at      timestamptz NOT NULL DEFAULT now(),
  revoked_at      timestamptz
);
CREATE INDEX session_account_id_idx ON session (account_id);

CREATE TABLE webauthn_credential (
  id              SERIAL PRIMARY KEY,
  account_id      integer NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  credential_id   bytea NOT NULL UNIQUE,
  public_key      bytea NOT NULL,
  cose_alg        int NOT NULL,                   -- COSEAlgorithmIdentifier (e.g. -7 ES256, -257 RS256)
  user_handle     bytea NOT NULL,                 -- value sent in PublicKeyCredentialUserEntity.id at registration
  sign_count      bigint NOT NULL DEFAULT 0,
  transports      text[],
  aaguid          bytea,
  attestation_type text,
  backup_eligible boolean,
  backup_state    boolean,
  uv_initialized  boolean NOT NULL DEFAULT false, -- WebAuthn L3 §4: true once UV observed
  nickname        text,
  last_used_at    timestamptz,
  clone_warning_at timestamptz,                   -- stamped on first sign-count regression
  created_at      timestamptz NOT NULL DEFAULT now()
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
  expected_upstream_idp_slug text,                -- optional; pre-binds an invite to a specific upstream IdP
  created_at                 timestamptz NOT NULL DEFAULT now(),
  expires_at                 timestamptz NOT NULL,
  consumed_at                timestamptz,
  CONSTRAINT enrollment_intent_target_check CHECK (
    (intent = 'invite' AND target_account_id IS NULL)
    OR (intent <> 'invite')
  ),
  CONSTRAINT enrollment_template_intent_check CHECK (
    intent = 'invite' OR (
      template_username IS NULL AND template_display_name IS NULL
      AND template_role IS NULL AND template_attributes IS NULL
    )
  )
);

-- Audit log for every credential lifecycle event. Queryable; satisfies the
-- "standalone IdP must answer who did what when" requirement (NIST §4.1-4.2).
CREATE TABLE credential_event (
  id             bigserial PRIMARY KEY,
  account_id     integer REFERENCES account(id) ON DELETE SET NULL,
  factor         text NOT NULL,                   -- 'webauthn','password','totp','recovery_code','federation_oidc','enrollment','session','oidc_client','saml_sp'
  event          text NOT NULL,                   -- 'register','use','fail','revoke','clone_warning','link','unlink','enrollment_issued','enrollment_consumed','session_start','session_end','factor_disabled','admin_action'
  credential_ref bigint,                          -- factor-specific row id; null when factor has no per-row id (e.g. password)
  ip             inet,
  user_agent     text,
  detail         jsonb,                           -- free-form structured context (claim values, redirect URIs, etc.)
  at             timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX credential_event_account_at_idx ON credential_event (account_id, at DESC);
CREATE INDEX credential_event_at_idx ON credential_event (at DESC);

-- Persistent failed-attempt counters across restarts (RFC 4226 §7.3).
CREATE TABLE auth_throttle (
  account_id      integer NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  factor          text NOT NULL,                  -- 'password','totp','recovery_code','federation_oidc','webauthn'
  failed_attempts int NOT NULL DEFAULT 0,
  window_start    timestamptz NOT NULL DEFAULT now(),
  locked_until    timestamptz,
  PRIMARY KEY (account_id, factor)
);

-- +goose Down

DROP TABLE IF EXISTS auth_throttle;
DROP TABLE IF EXISTS credential_event;
DROP TABLE IF EXISTS enrollment;
DROP INDEX IF EXISTS webauthn_credential_user_handle_idx;
DROP INDEX IF EXISTS webauthn_credential_account_idx;
DROP TABLE IF EXISTS webauthn_credential;
DROP INDEX IF EXISTS session_account_id_idx;
DROP TABLE IF EXISTS session;
DROP TABLE IF EXISTS account;
