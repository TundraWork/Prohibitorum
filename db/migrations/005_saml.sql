-- +goose Up

CREATE TABLE saml_sp (
  id                            bigserial PRIMARY KEY,
  entity_id                     text NOT NULL UNIQUE,
  display_name                  text NOT NULL,
  sp_kind                       text,
  name_id_format                text NOT NULL DEFAULT 'urn:oasis:names:tc:SAML:1.1:nameid-format:persistent',
  name_id_claim                 text NOT NULL DEFAULT 'sub',
  attribute_map                 jsonb NOT NULL DEFAULT '[]'::jsonb,
  want_assertions_signed        boolean NOT NULL DEFAULT true,
  authn_requests_signed         boolean NOT NULL DEFAULT false,
  require_signed_authn_request  boolean NOT NULL DEFAULT false,
  session_lifetime              interval,
  metadata_xml                  text,
  metadata_valid_until          timestamptz,
  metadata_cache_duration       interval,
  metadata_fetched_at           timestamptz,
  created_at                    timestamptz NOT NULL DEFAULT now()
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
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX saml_session_session_id_idx ON saml_session (session_id);

-- +goose Down
DROP TABLE IF EXISTS saml_session;
DROP TABLE IF EXISTS saml_subject_id;
DROP TABLE IF EXISTS saml_sp_key;
DROP TABLE IF EXISTS saml_sp_acs;
DROP TABLE IF EXISTS saml_sp;
