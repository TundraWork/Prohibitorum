-- +goose Up
-- Avatar provenance: NULL = untouched, 'upstream' = inherited from IdP,
-- 'user' = the account owner uploaded or deliberately removed (locks out upstream).
ALTER TABLE account ADD COLUMN IF NOT EXISTS avatar_source text;
-- Every avatar that exists today was user-uploaded (this feature did not exist),
-- so protect it from upstream clobber on the owner's next federated login.
UPDATE account SET avatar_source = 'user' WHERE avatar_etag IS NOT NULL AND avatar_source IS NULL;

-- Per-IdP claim override for the upstream avatar URL (mirrors username/display_name/email).
ALTER TABLE upstream_idp ADD COLUMN IF NOT EXISTS picture_claim text NOT NULL DEFAULT 'picture';

-- Identity confirmation gate: NULL = pending (must be confirmed on /welcome before a
-- session is issued). Existing identities count as already confirmed.
ALTER TABLE account_identity ADD COLUMN IF NOT EXISTS confirmed_at timestamptz;
UPDATE account_identity SET confirmed_at = linked_at WHERE confirmed_at IS NULL;

-- +goose Down
ALTER TABLE account_identity DROP COLUMN IF EXISTS confirmed_at;
ALTER TABLE upstream_idp DROP COLUMN IF EXISTS picture_claim;
ALTER TABLE account DROP COLUMN IF EXISTS avatar_source;
