-- +goose Up
-- 047_unsplash_rotation.sql — the Unsplash background gains the same choice as
-- uploaded images: one photo at random per visit or a carousel, and its
-- interval. The document is decoded strictly, so a saved one without these keys
-- would fall back to the defaults; fill in the values that keep the page as it
-- was. NULL still means the defaults in pkg/branding.
UPDATE instance_settings
SET login_appearance = jsonb_set(
    login_appearance,
    '{background,unsplash}',
    (login_appearance #> '{background,unsplash}') || '{"order": "random", "intervalSeconds": 10}'::jsonb
)
WHERE id = 1 AND login_appearance IS NOT NULL;

-- +goose Down
UPDATE instance_settings
SET login_appearance = login_appearance #- '{background,unsplash,order}' #- '{background,unsplash,intervalSeconds}'
WHERE id = 1 AND login_appearance IS NOT NULL;
