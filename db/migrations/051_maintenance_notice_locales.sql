-- +goose Up
-- 051_maintenance_notice_locales.sql — the maintenance notice becomes one
-- notice per interface language, plus the language visitors fall back to. The
-- single saved notice moves under the default language "en": after the
-- migration every visitor still sees the same text (a "zh" visitor falls back
-- to "en", which holds it), matching what they saw before. The old
-- maintenance_message column is dropped; empty strings are never stored, so an
-- absent or empty notice becomes {}. Locale keys are validated by the
-- application (the language list grows with the UI languages), not by a CHECK.
ALTER TABLE instance_settings
  ADD COLUMN maintenance_messages       jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN maintenance_default_locale text  NOT NULL DEFAULT 'en';

UPDATE instance_settings
SET maintenance_messages = jsonb_build_object('en', maintenance_message)
WHERE id = 1 AND maintenance_message IS NOT NULL AND maintenance_message <> '';

ALTER TABLE instance_settings
  DROP COLUMN maintenance_message;

-- +goose Down
ALTER TABLE instance_settings
  ADD COLUMN maintenance_message text NULL;

UPDATE instance_settings
SET maintenance_message = NULLIF(maintenance_messages ->> maintenance_default_locale, '')
WHERE id = 1;

ALTER TABLE instance_settings
  DROP COLUMN maintenance_messages,
  DROP COLUMN maintenance_default_locale;
