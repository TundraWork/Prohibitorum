-- +goose Up
-- Provider filters normalize slugs to lowercase before lookup. Canonicalize the
-- stored side once so that exact lookup and cursor bindings share that invariant.
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (
    SELECT lower(slug)
    FROM upstream_idp
    GROUP BY lower(slug)
    HAVING count(*) > 1
  ) THEN
    RAISE EXCEPTION 'cannot canonicalize upstream provider slugs: case-insensitive collision';
  END IF;
END $$;
-- +goose StatementEnd

UPDATE enrollment
SET expected_upstream_idp_slug = lower(expected_upstream_idp_slug)
WHERE expected_upstream_idp_slug IS NOT NULL;

UPDATE upstream_idp
SET slug = lower(slug)
WHERE slug <> lower(slug);

ALTER TABLE upstream_idp
  ADD CONSTRAINT upstream_idp_slug_lowercase_check CHECK (slug = lower(slug));

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX account_created_at_id_idx
  ON account (created_at DESC, id DESC);

CREATE INDEX account_identity_upstream_idp_account_idx
  ON account_identity (upstream_idp_id, account_id);

CREATE INDEX account_search_trgm_idx
  ON account USING gin ((
    username || E'\n' ||
    display_name || E'\n' ||
    COALESCE(email, '')
  ) gin_trgm_ops);

CREATE INDEX account_identity_search_trgm_idx
  ON account_identity USING gin ((
    upstream_sub || E'\n' ||
    COALESCE(upstream_email, '') || E'\n' ||
    COALESCE(upstream_data->>'personaName', '') || E'\n' ||
    COALESCE(upstream_data->>'displayName', '') || E'\n' ||
    COALESCE(upstream_data->>'profileUrl', '')
  ) gin_trgm_ops);

-- +goose Down
DROP INDEX account_identity_search_trgm_idx;
DROP INDEX account_search_trgm_idx;
DROP INDEX account_identity_upstream_idp_account_idx;
DROP INDEX account_created_at_id_idx;
ALTER TABLE upstream_idp DROP CONSTRAINT upstream_idp_slug_lowercase_check;
