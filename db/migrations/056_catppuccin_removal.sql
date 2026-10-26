-- +goose Up
-- 056_catppuccin_removal.sql — PHB-154: the Catppuccin and Catppuccin Gray
-- colour schemes are removed from the build. The sign-in appearance is decoded
-- strictly, so a saved document naming either would make the store fall back
-- to the whole default look; it moves to the default scheme instead and keeps
-- every other setting. An account that chose either loses that choice (null,
-- as for one never made) and follows the instance's scheme again.
UPDATE instance_settings
SET login_appearance = jsonb_set(login_appearance, '{scheme}', '"graphite"'::jsonb)
WHERE id = 1
  AND login_appearance->>'scheme' IN ('catppuccin', 'catppuccin-gray');

UPDATE account
SET preferences = jsonb_set(preferences, '{appearance,scheme}', 'null'::jsonb)
WHERE preferences->'appearance'->>'scheme' IN ('catppuccin', 'catppuccin-gray');

-- +goose Down
-- No-op on purpose: the schemes are gone from the build, so going back below
-- 56 must not bring back a choice the current code cannot show.
SELECT 1;
