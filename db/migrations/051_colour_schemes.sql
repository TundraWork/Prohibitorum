-- +goose Up
-- 051_colour_schemes.sql — PHB-154. Accounts keep their own preferences, one
-- section per kind (this card adds "appearance"); only sections the user has
-- set are stored. The sign-in page appearance gains the instance's default
-- colour scheme; the document is decoded strictly, so fill in the scheme that
-- keeps every page as it was. NULL still means the defaults in pkg/branding.
ALTER TABLE account ADD COLUMN preferences jsonb NOT NULL DEFAULT '{}'::jsonb;

UPDATE instance_settings
SET login_appearance = login_appearance || '{"scheme": "graphite"}'::jsonb
WHERE id = 1 AND login_appearance IS NOT NULL;

-- +goose Down
UPDATE instance_settings
SET login_appearance = login_appearance - 'scheme'
WHERE id = 1 AND login_appearance IS NOT NULL;

ALTER TABLE account DROP COLUMN preferences;
