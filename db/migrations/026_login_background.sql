-- +goose Up
-- Custom login-page background image. Stored and served VERBATIM (no re-encode) —
-- the exact uploaded bytes reach the browser. NULL = no override; the SPA then
-- falls back to its build-time asset / CSS gradient.
ALTER TABLE instance_settings
  ADD COLUMN IF NOT EXISTS login_bg      bytea NULL,
  ADD COLUMN IF NOT EXISTS login_bg_etag text  NULL;

-- +goose Down
ALTER TABLE instance_settings
  DROP COLUMN IF EXISTS login_bg,
  DROP COLUMN IF EXISTS login_bg_etag;
