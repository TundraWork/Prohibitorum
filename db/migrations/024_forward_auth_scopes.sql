-- +goose Up
-- 024_forward_auth_scopes.sql — admin-defined scope vocabulary for a forward-auth
-- app. JSONB array of { "name": "...", "description": "..." }. Opaque to the
-- gateway; surfaced in the user's PAT scope picker and emitted (the chosen subset)
-- as the Remote-Scopes header for that app.
ALTER TABLE oidc_client
  ADD COLUMN IF NOT EXISTS forward_auth_scopes jsonb NOT NULL DEFAULT '[]'::jsonb;

-- +goose Down
ALTER TABLE oidc_client DROP COLUMN IF EXISTS forward_auth_scopes;
