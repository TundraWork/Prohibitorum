-- +goose Up
ALTER TABLE oidc_client
  ADD COLUMN principal_source text NOT NULL DEFAULT 'sub',
  ADD COLUMN claim_aliases jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD CONSTRAINT oidc_client_principal_source_check
    CHECK (principal_source IN ('sub', 'username', 'verified_email')),
  ADD CONSTRAINT oidc_client_claim_aliases_object_check
    CHECK (jsonb_typeof(claim_aliases) = 'object');

UPDATE oidc_client
SET principal_source = 'username'
WHERE forward_auth_enabled;

-- +goose Down
ALTER TABLE oidc_client
  DROP CONSTRAINT IF EXISTS oidc_client_claim_aliases_object_check,
  DROP CONSTRAINT IF EXISTS oidc_client_principal_source_check,
  DROP COLUMN IF EXISTS claim_aliases,
  DROP COLUMN IF EXISTS principal_source;
