-- +goose Up
CREATE TABLE oidc_client_group (
  client_id text NOT NULL REFERENCES oidc_client(client_id) ON DELETE CASCADE,
  group_id integer NOT NULL REFERENCES user_group(id) ON DELETE RESTRICT,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (client_id, group_id)
);
CREATE INDEX oidc_client_group_group_idx ON oidc_client_group(group_id);

CREATE TABLE saml_sp_group (
  saml_sp_id bigint NOT NULL REFERENCES saml_sp(id) ON DELETE CASCADE,
  group_id integer NOT NULL REFERENCES user_group(id) ON DELETE RESTRICT,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (saml_sp_id, group_id)
);
CREATE INDEX saml_sp_group_group_idx ON saml_sp_group(group_id);

INSERT INTO oidc_client_group (client_id, group_id)
SELECT oidc_client_id, id
FROM user_group
WHERE oidc_client_id IS NOT NULL;

INSERT INTO saml_sp_group (saml_sp_id, group_id)
SELECT saml_sp_id, id
FROM user_group
WHERE saml_sp_id IS NOT NULL;

DROP INDEX user_group_oidc_slug_uq;
DROP INDEX user_group_saml_slug_uq;
DROP INDEX user_group_oidc_manual_uq;
DROP INDEX user_group_saml_manual_uq;
ALTER TABLE user_group DROP CONSTRAINT user_group_app_binding_check;
ALTER TABLE user_group DROP COLUMN oidc_client_id;
ALTER TABLE user_group DROP COLUMN saml_sp_id;

-- +goose Down
-- A global group can be represented by the old model only when it belongs to
-- exactly one application. Refuse the whole transactional migration otherwise.
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (
    SELECT g.id
    FROM user_group g
    LEFT JOIN oidc_client_group og ON og.group_id = g.id
    LEFT JOIN saml_sp_group sg ON sg.group_id = g.id
    GROUP BY g.id
    HAVING count(DISTINCT og.client_id) + count(DISTINCT sg.saml_sp_id) <> 1
  ) THEN
    RAISE EXCEPTION 'cannot downgrade global user groups: every group must belong to exactly one application';
  END IF;

  IF EXISTS (
    SELECT 1 FROM (
      SELECT client_id, slug
      FROM oidc_client_group og JOIN user_group g ON g.id = og.group_id
      GROUP BY client_id, slug HAVING count(*) > 1
      UNION ALL
      SELECT saml_sp_id::text, slug
      FROM saml_sp_group sg JOIN user_group g ON g.id = sg.group_id
      GROUP BY saml_sp_id, slug HAVING count(*) > 1
    ) duplicate_slugs
  ) THEN
    RAISE EXCEPTION 'cannot downgrade global user groups: an application has duplicate group slugs';
  END IF;

  IF EXISTS (
    SELECT 1 FROM (
      SELECT client_id
      FROM oidc_client_group og JOIN user_group g ON g.id = og.group_id
      WHERE g.kind = 'manual' GROUP BY client_id HAVING count(*) > 1
      UNION ALL
      SELECT saml_sp_id::text
      FROM saml_sp_group sg JOIN user_group g ON g.id = sg.group_id
      WHERE g.kind = 'manual' GROUP BY saml_sp_id HAVING count(*) > 1
    ) duplicate_manual_groups
  ) THEN
    RAISE EXCEPTION 'cannot downgrade global user groups: an application has multiple manual groups';
  END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE user_group
  ADD COLUMN oidc_client_id text REFERENCES oidc_client(client_id) ON DELETE CASCADE,
  ADD COLUMN saml_sp_id bigint REFERENCES saml_sp(id) ON DELETE CASCADE;

UPDATE user_group g
SET oidc_client_id = og.client_id
FROM oidc_client_group og
WHERE og.group_id = g.id;

UPDATE user_group g
SET saml_sp_id = sg.saml_sp_id
FROM saml_sp_group sg
WHERE sg.group_id = g.id;

ALTER TABLE user_group ADD CONSTRAINT user_group_app_binding_check
  CHECK (num_nonnulls(oidc_client_id, saml_sp_id) = 1);
CREATE UNIQUE INDEX user_group_oidc_slug_uq
  ON user_group(oidc_client_id, slug) WHERE oidc_client_id IS NOT NULL;
CREATE UNIQUE INDEX user_group_saml_slug_uq
  ON user_group(saml_sp_id, slug) WHERE saml_sp_id IS NOT NULL;
CREATE UNIQUE INDEX user_group_oidc_manual_uq
  ON user_group(oidc_client_id) WHERE kind = 'manual';
CREATE UNIQUE INDEX user_group_saml_manual_uq
  ON user_group(saml_sp_id) WHERE kind = 'manual';

DROP TABLE saml_sp_group;
DROP TABLE oidc_client_group;
