-- +goose Up
-- Provider filters normalize slugs to lowercase before lookup. Canonicalize the
-- stored side once so that exact lookup and cursor bindings share that invariant.
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (
    SELECT lower(btrim(slug))
    FROM upstream_idp
    GROUP BY lower(btrim(slug))
    HAVING count(*) > 1
  ) THEN
    RAISE EXCEPTION 'cannot canonicalize upstream provider slugs: normalized collision';
  END IF;
END $$;
-- +goose StatementEnd

-- Avatar source keys embed the provider slug. Move the active pointer first,
-- then its composite-primary-key row, while the original provider slug is
-- still available for an exact join.
UPDATE account a
SET avatar_source = 'upstream:' || lower(btrim(ip.slug))
FROM account_avatar av
JOIN upstream_idp ip ON ip.id = av.idp_id
WHERE a.id = av.account_id
  AND a.avatar_source = av.source
  AND av.source = 'upstream:' || ip.slug
  AND ip.slug <> lower(btrim(ip.slug));

UPDATE account_avatar av
SET source = 'upstream:' || lower(btrim(ip.slug))
FROM upstream_idp ip
WHERE av.idp_id = ip.id
  AND av.source = 'upstream:' || ip.slug
  AND ip.slug <> lower(btrim(ip.slug));

UPDATE enrollment
SET expected_upstream_idp_slug = lower(btrim(expected_upstream_idp_slug))
WHERE expected_upstream_idp_slug IS NOT NULL;

UPDATE upstream_idp
SET slug = lower(btrim(slug))
WHERE slug <> lower(btrim(slug));

ALTER TABLE upstream_idp
  ADD CONSTRAINT upstream_idp_slug_lowercase_check
  CHECK (slug = lower(btrim(slug)));

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
