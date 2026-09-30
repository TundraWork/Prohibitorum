-- +goose Up
-- 044_pat_access_levels.sql — a PAT carries one access level instead of an
-- all_apps flag plus per-app scope grants. selected_apps tokens list their
-- applications in personal_access_token_app; the forward-auth scope vocabulary
-- on oidc_client is dropped.
ALTER TABLE personal_access_token
  ADD COLUMN access text;
UPDATE personal_access_token
SET access = CASE WHEN all_apps THEN 'all_apps' ELSE 'selected_apps' END;
ALTER TABLE personal_access_token
  ALTER COLUMN access SET NOT NULL,
  ADD CONSTRAINT personal_access_token_access_check
    CHECK (access IN ('selected_apps', 'all_apps', 'full', 'sudo'));

CREATE TABLE personal_access_token_app (
  pat_id    integer NOT NULL REFERENCES personal_access_token(id) ON DELETE CASCADE,
  client_id text    NOT NULL REFERENCES oidc_client(client_id) ON DELETE CASCADE,
  PRIMARY KEY (pat_id, client_id)
);
CREATE INDEX personal_access_token_app_client_idx ON personal_access_token_app(client_id);

INSERT INTO personal_access_token_app (pat_id, client_id)
SELECT p.id, g.key
FROM personal_access_token p
CROSS JOIN LATERAL jsonb_object_keys(p.app_grants) AS g(key)
JOIN oidc_client c ON c.client_id = g.key
WHERE p.access = 'selected_apps'
  AND jsonb_typeof(p.app_grants) = 'object';

ALTER TABLE personal_access_token
  DROP COLUMN all_apps,
  DROP COLUMN app_grants;
ALTER TABLE oidc_client DROP COLUMN forward_auth_scopes;

-- +goose Down
ALTER TABLE oidc_client
  ADD COLUMN forward_auth_scopes jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE personal_access_token
  ADD COLUMN all_apps boolean NOT NULL DEFAULT false,
  ADD COLUMN app_grants jsonb NOT NULL DEFAULT '{}'::jsonb;
UPDATE personal_access_token SET all_apps = true WHERE access <> 'selected_apps';
UPDATE personal_access_token p
SET app_grants = COALESCE((
  SELECT jsonb_object_agg(a.client_id, '[]'::jsonb)
  FROM personal_access_token_app a
  WHERE a.pat_id = p.id
), '{}'::jsonb)
WHERE p.access = 'selected_apps';
DROP TABLE personal_access_token_app;
ALTER TABLE personal_access_token
  DROP CONSTRAINT personal_access_token_access_check,
  DROP COLUMN access;
