-- +goose Up
-- 046_login_layout.sql — the sign-in page appearance gains the card's position
-- and the public pages' theme. The document is decoded strictly, so a saved one
-- without these keys would fall back to the defaults; fill in the values that
-- keep the page as it was. NULL still means the defaults in pkg/branding.
UPDATE instance_settings
SET login_appearance = login_appearance || '{"cardPosition": "center", "theme": "switchable"}'::jsonb
WHERE id = 1 AND login_appearance IS NOT NULL;

-- +goose Down
UPDATE instance_settings
SET login_appearance = login_appearance - 'cardPosition' - 'theme'
WHERE id = 1 AND login_appearance IS NOT NULL;
