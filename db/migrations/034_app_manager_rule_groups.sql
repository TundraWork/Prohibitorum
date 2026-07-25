-- +goose Up
DROP TABLE IF EXISTS saml_sp_access;
DROP TABLE IF EXISTS oidc_client_access;
DROP TABLE IF EXISTS group_member;
DROP TABLE IF EXISTS user_group;

UPDATE oidc_client SET access_restricted = false WHERE access_restricted;
UPDATE saml_sp SET access_restricted = false WHERE access_restricted;

ALTER TABLE account DROP CONSTRAINT account_role_check;
ALTER TABLE account ADD CONSTRAINT account_role_check
  CHECK (role IN ('user', 'app_manager', 'admin'));
ALTER TABLE enrollment DROP CONSTRAINT enrollment_template_role_check;
ALTER TABLE enrollment ADD CONSTRAINT enrollment_template_role_check
  CHECK (template_role IN ('user', 'app_manager', 'admin'));

CREATE TABLE oidc_client_manager (
  client_id text NOT NULL REFERENCES oidc_client(client_id) ON DELETE CASCADE,
  account_id integer NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by integer REFERENCES account(id) ON DELETE SET NULL,
  PRIMARY KEY (client_id, account_id)
);

CREATE TABLE saml_sp_manager (
  saml_sp_id bigint NOT NULL REFERENCES saml_sp(id) ON DELETE CASCADE,
  account_id integer NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by integer REFERENCES account(id) ON DELETE SET NULL,
  PRIMARY KEY (saml_sp_id, account_id)
);

CREATE TABLE user_group (
  id serial PRIMARY KEY,
  kind text NOT NULL CHECK (kind IN ('manual','rule')),
  slug text NOT NULL CHECK (slug ~ '^[a-z0-9](-?[a-z0-9])*$'),
  display_name text NOT NULL,
  description text,
  exposed_to_downstream boolean NOT NULL DEFAULT true,
  rule jsonb,
  oidc_client_id text REFERENCES oidc_client(client_id) ON DELETE CASCADE,
  saml_sp_id bigint REFERENCES saml_sp(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (id, kind),
  CONSTRAINT user_group_app_binding_check
    CHECK (num_nonnulls(oidc_client_id, saml_sp_id) = 1),
  CONSTRAINT user_group_rule_shape_check
    CHECK ((kind = 'manual' AND rule IS NULL) OR
           (kind = 'rule' AND rule IS NOT NULL AND jsonb_typeof(rule) = 'object'))
);

CREATE UNIQUE INDEX user_group_oidc_slug_uq
  ON user_group(oidc_client_id, slug) WHERE oidc_client_id IS NOT NULL;
CREATE UNIQUE INDEX user_group_saml_slug_uq
  ON user_group(saml_sp_id, slug) WHERE saml_sp_id IS NOT NULL;
CREATE UNIQUE INDEX user_group_oidc_manual_uq
  ON user_group(oidc_client_id) WHERE kind = 'manual';
CREATE UNIQUE INDEX user_group_saml_manual_uq
  ON user_group(saml_sp_id) WHERE kind = 'manual';

CREATE TABLE group_manual_decision (
  group_id integer NOT NULL,
  group_kind text NOT NULL DEFAULT 'manual' CHECK (group_kind = 'manual'),
  account_id integer NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  effect text NOT NULL CHECK (effect IN ('allow','deny')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  created_by integer REFERENCES account(id) ON DELETE SET NULL,
  PRIMARY KEY (group_id, account_id),
  CONSTRAINT group_manual_decision_group_fkey
    FOREIGN KEY (group_id, group_kind) REFERENCES user_group(id, kind) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE IF EXISTS group_manual_decision;
DROP TABLE IF EXISTS user_group;
DROP TABLE IF EXISTS saml_sp_manager;
DROP TABLE IF EXISTS oidc_client_manager;

UPDATE oidc_client SET access_restricted = false WHERE access_restricted;
UPDATE saml_sp SET access_restricted = false WHERE access_restricted;

UPDATE enrollment SET template_role = 'user' WHERE template_role = 'app_manager';
UPDATE account SET role = 'user' WHERE role = 'app_manager';

ALTER TABLE enrollment DROP CONSTRAINT enrollment_template_role_check;
ALTER TABLE enrollment ADD CONSTRAINT enrollment_template_role_check
  CHECK (template_role IN ('user', 'admin'));
ALTER TABLE account DROP CONSTRAINT account_role_check;
ALTER TABLE account ADD CONSTRAINT account_role_check
  CHECK (role IN ('user', 'admin'));

CREATE TABLE user_group (
  id serial PRIMARY KEY,
  slug text NOT NULL UNIQUE,
  display_name text NOT NULL,
  description text,
  exposed_to_downstream boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT user_group_slug_format CHECK (slug ~ '^[a-z0-9](-?[a-z0-9])*$')
);

CREATE TABLE group_member (
  group_id integer NOT NULL REFERENCES user_group(id) ON DELETE CASCADE,
  account_id integer NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (group_id, account_id)
);
CREATE INDEX group_member_account_idx ON group_member(account_id);

CREATE TABLE oidc_client_access (
  client_id text NOT NULL REFERENCES oidc_client(client_id) ON DELETE CASCADE,
  group_id integer REFERENCES user_group(id) ON DELETE CASCADE,
  account_id integer REFERENCES account(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT oidc_client_access_one_principal CHECK (num_nonnulls(group_id, account_id) = 1)
);
CREATE UNIQUE INDEX oidc_client_access_group_uq
  ON oidc_client_access(client_id, group_id) WHERE group_id IS NOT NULL;
CREATE UNIQUE INDEX oidc_client_access_account_uq
  ON oidc_client_access(client_id, account_id) WHERE account_id IS NOT NULL;
CREATE INDEX oidc_client_access_group_id_idx
  ON oidc_client_access(group_id) WHERE group_id IS NOT NULL;

CREATE TABLE saml_sp_access (
  saml_sp_id bigint NOT NULL REFERENCES saml_sp(id) ON DELETE CASCADE,
  group_id integer REFERENCES user_group(id) ON DELETE CASCADE,
  account_id integer REFERENCES account(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT saml_sp_access_one_principal CHECK (num_nonnulls(group_id, account_id) = 1)
);
CREATE UNIQUE INDEX saml_sp_access_group_uq
  ON saml_sp_access(saml_sp_id, group_id) WHERE group_id IS NOT NULL;
CREATE UNIQUE INDEX saml_sp_access_account_uq
  ON saml_sp_access(saml_sp_id, account_id) WHERE account_id IS NOT NULL;
CREATE INDEX saml_sp_access_group_id_idx
  ON saml_sp_access(group_id) WHERE group_id IS NOT NULL;
