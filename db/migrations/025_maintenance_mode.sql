-- +goose Up
-- Maintenance mode: when maintenance_mode is true, every NON-admin principal is
-- denied access (login, dashboard self-service, OIDC/SAML SSO, and the
-- forward-auth gateway) with a 503; admins are unaffected so they can still
-- manage the instance and lift the mode. maintenance_message is an optional
-- admin-authored note surfaced to blocked users on the maintenance screen.
ALTER TABLE instance_settings
  ADD COLUMN IF NOT EXISTS maintenance_mode    boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS maintenance_message text    NULL;

-- +goose Down
ALTER TABLE instance_settings
  DROP COLUMN IF EXISTS maintenance_mode,
  DROP COLUMN IF EXISTS maintenance_message;
