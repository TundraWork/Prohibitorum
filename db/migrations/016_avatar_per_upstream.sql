-- +goose Up
-- Per-(account, upstream) inherited avatars. The avatar source string becomes
-- "upstream:<slug>" (one row per linked upstream), and idp_id records WHICH
-- upstream each inherited avatar came from. ON DELETE CASCADE: removing an
-- upstream_idp cleanly drops the avatars inherited from it.
ALTER TABLE account_avatar
  ADD COLUMN IF NOT EXISTS idp_id bigint REFERENCES upstream_idp(id) ON DELETE CASCADE;

-- Index the FK so deleting an upstream_idp doesn't seq-scan account_avatar
-- (partial: only inherited rows carry an idp_id). Matches the 015 pattern.
CREATE INDEX IF NOT EXISTS account_avatar_idp_id_idx ON account_avatar(idp_id) WHERE idp_id IS NOT NULL;

-- Legacy single 'upstream' rows pre-date per-upstream keying and cannot be
-- attributed to a slug. Drop them (they re-inherit per-slug on the next
-- federated login) and reset any active pointer that referenced them.
UPDATE account SET avatar_source = NULL, avatar_etag = NULL, avatar_content_type = NULL
  WHERE avatar_source = 'upstream';
DELETE FROM account_avatar WHERE source = 'upstream';

-- +goose Down
-- Lossy reverse: the column is dropped, but the legacy 'upstream' rows deleted
-- above are not restored (they would re-inherit on the next federated login).
ALTER TABLE account_avatar DROP COLUMN IF EXISTS idp_id;
