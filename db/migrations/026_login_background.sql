-- 026_login_background.sql — custom login-page background image.
-- Stored and served VERBATIM (no re-encode). NULL = no override; the SPA then
-- falls back to its build-time asset / CSS gradient.
ALTER TABLE instance_settings
  ADD COLUMN IF NOT EXISTS login_bg      bytea NULL,
  ADD COLUMN IF NOT EXISTS login_bg_etag text  NULL;
