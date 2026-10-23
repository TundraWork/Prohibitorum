-- +goose Up
-- 053_pixabay_source.sql — Pixabay joins Unsplash as a picture background.
-- Its terms forbid permanent hotlinking, so the server downloads each picture
-- and serves it from /branding/wallpaper/images/pixabay/{id}; that endpoint
-- reads the batch this build keeps in memory, so no new state lives here. The
-- document is decoded strictly, so a saved one without the pixabay object
-- would fall back to the defaults; fill in the values that keep the page as
-- it was. The API key is sealed with the data encryption key, like the
-- Unsplash key.
ALTER TABLE instance_settings
  ADD COLUMN pixabay_key_enc     bytea NULL,
  ADD COLUMN pixabay_key_nonce   bytea NULL,
  ADD COLUMN pixabay_key_version int   NULL,
  ADD CONSTRAINT instance_settings_pixabay_key_check CHECK (
    (pixabay_key_enc IS NULL AND pixabay_key_nonce IS NULL AND pixabay_key_version IS NULL)
    OR (pixabay_key_enc IS NOT NULL AND pixabay_key_nonce IS NOT NULL AND pixabay_key_version IS NOT NULL)
  );

UPDATE instance_settings
SET login_appearance = jsonb_set(
    login_appearance,
    '{background,pixabay}',
    '{"query": "", "lang": "zh", "order": "random", "intervalSeconds": 10}'::jsonb
)
WHERE id = 1 AND login_appearance IS NOT NULL;

-- +goose Down
UPDATE instance_settings
SET login_appearance = login_appearance #- '{background,pixabay}'
WHERE id = 1 AND login_appearance IS NOT NULL;

ALTER TABLE instance_settings
  DROP CONSTRAINT instance_settings_pixabay_key_check,
  DROP COLUMN pixabay_key_enc,
  DROP COLUMN pixabay_key_nonce,
  DROP COLUMN pixabay_key_version;
