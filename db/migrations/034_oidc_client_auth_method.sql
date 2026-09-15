-- +goose Up
-- The column no longer carries OIDC Discovery's token_endpoint_auth_method
-- semantics: a confidential client may present its secret through either the
-- Basic header or the request body, so the value is only a confidential-vs-
-- public discriminator. Rename it and collapse the vocabulary to match.
ALTER TABLE oidc_client RENAME COLUMN token_endpoint_auth_method TO client_auth_method;

-- Drop the old default before backfilling so the new CHECK cannot collide with
-- a stale 'client_secret_basic' default, then reinstate it on the new value.
ALTER TABLE oidc_client ALTER COLUMN client_auth_method DROP DEFAULT;
UPDATE oidc_client SET client_auth_method = 'client_secret'
  WHERE client_auth_method IN ('client_secret_basic', 'client_secret_post');
ALTER TABLE oidc_client ALTER COLUMN client_auth_method SET DEFAULT 'client_secret';
ALTER TABLE oidc_client ADD CONSTRAINT oidc_client_client_auth_method_check
  CHECK (client_auth_method IN ('client_secret', 'none'));

-- +goose Down
ALTER TABLE oidc_client DROP CONSTRAINT oidc_client_client_auth_method_check;
ALTER TABLE oidc_client ALTER COLUMN client_auth_method DROP DEFAULT;
UPDATE oidc_client SET client_auth_method = 'client_secret_basic'
  WHERE client_auth_method = 'client_secret';
ALTER TABLE oidc_client ALTER COLUMN client_auth_method SET DEFAULT 'client_secret_basic';
ALTER TABLE oidc_client RENAME COLUMN client_auth_method TO token_endpoint_auth_method;
