-- +goose Up
-- Forward-auth: a protected service is a normal oidc_client flagged for
-- forward-auth. forward_auth_host is the X-Forwarded-Host the verify endpoint
-- matches to resolve the backing client.
ALTER TABLE oidc_client
  ADD COLUMN IF NOT EXISTS forward_auth_enabled boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS forward_auth_host text NULL;

CREATE UNIQUE INDEX IF NOT EXISTS oidc_client_forward_auth_host_uq
  ON oidc_client(forward_auth_host) WHERE forward_auth_enabled;

-- +goose Down
DROP INDEX IF EXISTS oidc_client_forward_auth_host_uq;
ALTER TABLE oidc_client
  DROP COLUMN IF EXISTS forward_auth_enabled,
  DROP COLUMN IF EXISTS forward_auth_host;
