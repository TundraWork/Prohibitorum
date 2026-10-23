-- +goose Up
-- 054_openverse_source.sql — Openverse joins the picture backgrounds, with the
-- same shape as Unsplash's search (a keyword, an order, an interval) and no
-- key: the API is used anonymously. The document is decoded strictly, so a
-- saved one without the openverse object would fall back to the defaults;
-- fill in the values that keep the page as it was.
UPDATE instance_settings
SET login_appearance = jsonb_set(
    login_appearance,
    '{background,openverse}',
    '{"query": "", "order": "random", "intervalSeconds": 10}'::jsonb
)
WHERE id = 1 AND login_appearance IS NOT NULL;

-- +goose Down
UPDATE instance_settings
SET login_appearance = login_appearance #- '{background,openverse}'
WHERE id = 1 AND login_appearance IS NOT NULL;
