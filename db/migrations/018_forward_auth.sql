-- +goose Up
-- Forward-auth: a protected service is a normal oidc_client flagged for
-- forward-auth. forward_auth_host is the X-Forwarded-Host the verify endpoint
-- matches to resolve the backing client. forward_auth_scopes is the admin-defined
-- scope vocabulary (JSONB array of { "name": "...", "description": "..." }):
-- opaque to the gateway, surfaced in the user's PAT scope picker and emitted (the
-- chosen subset) as the Remote-Scopes header for that app.
ALTER TABLE oidc_client
  ADD COLUMN IF NOT EXISTS forward_auth_enabled boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS forward_auth_host text NULL,
  ADD COLUMN IF NOT EXISTS forward_auth_scopes jsonb NOT NULL DEFAULT '[]'::jsonb;

CREATE UNIQUE INDEX IF NOT EXISTS oidc_client_forward_auth_host_uq
  ON oidc_client(forward_auth_host) WHERE forward_auth_enabled;

-- +goose Down
DROP INDEX IF EXISTS oidc_client_forward_auth_host_uq;
ALTER TABLE oidc_client
  DROP COLUMN IF EXISTS forward_auth_enabled,
  DROP COLUMN IF EXISTS forward_auth_host,
  DROP COLUMN IF EXISTS forward_auth_scopes;
