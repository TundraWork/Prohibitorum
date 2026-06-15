-- +goose Up
CREATE TABLE IF NOT EXISTS user_group (
  id                    serial PRIMARY KEY,
  slug                  text NOT NULL UNIQUE,
  display_name          text NOT NULL,
  description           text,
  exposed_to_downstream boolean NOT NULL DEFAULT true,
  created_at            timestamptz NOT NULL DEFAULT now(),
  updated_at            timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT user_group_slug_format CHECK (slug ~ '^[a-z0-9](-?[a-z0-9])*$')
);

CREATE TABLE IF NOT EXISTS group_member (
  group_id   integer NOT NULL REFERENCES user_group(id) ON DELETE CASCADE,
  account_id integer NOT NULL REFERENCES account(id)    ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (group_id, account_id)
);
CREATE INDEX IF NOT EXISTS group_member_account_idx ON group_member(account_id);

CREATE TABLE IF NOT EXISTS oidc_client_access (
  client_id  text    NOT NULL REFERENCES oidc_client(client_id) ON DELETE CASCADE,
  group_id   integer          REFERENCES user_group(id)         ON DELETE CASCADE,
  account_id integer          REFERENCES account(id)            ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT oidc_client_access_one_principal CHECK (num_nonnulls(group_id, account_id) = 1)
);
CREATE UNIQUE INDEX IF NOT EXISTS oidc_client_access_group_uq
  ON oidc_client_access(client_id, group_id) WHERE group_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS oidc_client_access_account_uq
  ON oidc_client_access(client_id, account_id) WHERE account_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS oidc_client_access_group_id_idx ON oidc_client_access(group_id) WHERE group_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS saml_sp_access (
  saml_sp_id bigint  NOT NULL REFERENCES saml_sp(id)    ON DELETE CASCADE,
  group_id   integer          REFERENCES user_group(id) ON DELETE CASCADE,
  account_id integer          REFERENCES account(id)    ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT saml_sp_access_one_principal CHECK (num_nonnulls(group_id, account_id) = 1)
);
CREATE UNIQUE INDEX IF NOT EXISTS saml_sp_access_group_uq
  ON saml_sp_access(saml_sp_id, group_id) WHERE group_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS saml_sp_access_account_uq
  ON saml_sp_access(saml_sp_id, account_id) WHERE account_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS saml_sp_access_group_id_idx ON saml_sp_access(group_id) WHERE group_id IS NOT NULL;

ALTER TABLE oidc_client ADD COLUMN IF NOT EXISTS access_restricted boolean NOT NULL DEFAULT false;
ALTER TABLE saml_sp      ADD COLUMN IF NOT EXISTS access_restricted boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE saml_sp      DROP COLUMN IF EXISTS access_restricted;
ALTER TABLE oidc_client  DROP COLUMN IF EXISTS access_restricted;
DROP TABLE IF EXISTS saml_sp_access;
DROP TABLE IF EXISTS oidc_client_access;
DROP TABLE IF EXISTS group_member;
DROP TABLE IF EXISTS user_group;
