-- +goose Up
-- 048_avatar_sync_fetch.sql — upstream avatars are fetched during sign-in, and
-- only when the upstream hands out a different URL than last time, so each
-- upstream row remembers the URL it was fetched from. Existing rows start with
-- NULL and are fetched once more on the next sign-in.
--
-- avatar_selected_at records the first time the user picked an avatar. Until
-- then an upload or an upstream avatar may take effect on its own; afterwards
-- the user's choice stays. Backfill the states that under the old rules could
-- only come from the user: 'none' (picked "no avatar", or the fallback after
-- deleting an upload), and an upstream source while an upload exists (an upload
-- took effect at once and an upstream avatar never replaced it, so the user
-- switched back by hand). Every other state may have been applied
-- automatically and stays NULL.
ALTER TABLE account_avatar ADD COLUMN upstream_url text;
ALTER TABLE account ADD COLUMN avatar_selected_at timestamptz;

UPDATE account a
SET avatar_selected_at = a.updated_at
WHERE a.avatar_source = 'none'
   OR (a.avatar_source LIKE 'upstream:%'
       AND EXISTS (SELECT 1 FROM account_avatar av WHERE av.account_id = a.id AND av.source = 'user'));

-- +goose Down
ALTER TABLE account DROP COLUMN avatar_selected_at;
ALTER TABLE account_avatar DROP COLUMN upstream_url;
