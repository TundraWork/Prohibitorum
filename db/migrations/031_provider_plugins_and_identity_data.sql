-- +goose Up
ALTER TABLE upstream_idp
  ADD COLUMN provider_config jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN secret_status text NOT NULL DEFAULT 'configured'
    CHECK (secret_status IN ('unconfigured','configured','valid','invalid')),
  ADD COLUMN secret_validated_at timestamptz;

UPDATE upstream_idp
SET provider_config = CASE protocol
  WHEN 'oidc' THEN jsonb_build_object(
    'issuerUrl', issuer_url,
    'clientId', client_id,
    'scopes', to_jsonb(scopes),
    'allowedDomains', to_jsonb(allowed_domains),
    'usernameClaim', username_claim,
    'displayNameClaim', display_name_claim,
    'emailClaim', email_claim,
    'pictureClaim', picture_claim,
    'requireVerifiedEmail', require_verified_email,
    'allowPrivateNetwork', allow_private_network
  )
  WHEN 'steam' THEN '{}'::jsonb
END;

ALTER TABLE upstream_idp RENAME COLUMN client_secret_enc TO secret_enc;
ALTER TABLE upstream_idp ALTER COLUMN secret_enc DROP NOT NULL;
ALTER TABLE upstream_idp ALTER COLUMN secret_nonce DROP NOT NULL;
ALTER TABLE upstream_idp ALTER COLUMN key_version DROP NOT NULL;
ALTER TABLE upstream_idp
  ADD CONSTRAINT upstream_idp_secret_tuple_check CHECK (
    (secret_enc IS NULL AND secret_nonce IS NULL AND key_version IS NULL)
    OR (secret_enc IS NOT NULL AND secret_nonce IS NOT NULL AND key_version IS NOT NULL)
  ),
  ADD CONSTRAINT upstream_idp_config_object_check CHECK (
    jsonb_typeof(provider_config) = 'object'
    AND octet_length(provider_config::text) <= 8192
  );

ALTER TABLE upstream_idp DROP CONSTRAINT IF EXISTS upstream_idp_protocol_check;
ALTER TABLE upstream_idp ADD CONSTRAINT upstream_idp_protocol_check
  CHECK (protocol IN ('oidc','steam','vrchat'));

ALTER TABLE account_identity
  ADD COLUMN upstream_data jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD CONSTRAINT account_identity_upstream_data_check CHECK (
    jsonb_typeof(upstream_data) = 'object'
    AND octet_length(upstream_data::text) <= 4096
  );
UPDATE account_identity ai
SET upstream_data = jsonb_build_object('steamId', ai.upstream_sub)
FROM upstream_idp ip
WHERE ip.id = ai.upstream_idp_id AND ip.protocol = 'steam';
CREATE INDEX account_identity_upstream_data_gin_idx
  ON account_identity USING gin (upstream_data jsonb_path_ops);

ALTER TABLE upstream_idp
  DROP COLUMN issuer_url,
  DROP COLUMN client_id,
  DROP COLUMN scopes,
  DROP COLUMN allowed_domains,
  DROP COLUMN username_claim,
  DROP COLUMN display_name_claim,
  DROP COLUMN email_claim,
  DROP COLUMN picture_claim,
  DROP COLUMN require_verified_email,
  DROP COLUMN allow_private_network;

-- +goose Down
DELETE FROM account_identity
WHERE upstream_idp_id IN (SELECT id FROM upstream_idp WHERE protocol = 'vrchat');
DELETE FROM upstream_idp WHERE protocol = 'vrchat';

ALTER TABLE upstream_idp
  ADD COLUMN issuer_url text NOT NULL DEFAULT '',
  ADD COLUMN client_id text NOT NULL DEFAULT '',
  ADD COLUMN scopes text[] NOT NULL DEFAULT ARRAY['openid','profile','email'],
  ADD COLUMN allowed_domains text[] NOT NULL DEFAULT ARRAY[]::text[],
  ADD COLUMN username_claim text NOT NULL DEFAULT 'preferred_username',
  ADD COLUMN display_name_claim text NOT NULL DEFAULT 'name',
  ADD COLUMN email_claim text NOT NULL DEFAULT 'email',
  ADD COLUMN picture_claim text NOT NULL DEFAULT 'picture',
  ADD COLUMN require_verified_email boolean NOT NULL DEFAULT true,
  ADD COLUMN allow_private_network boolean NOT NULL DEFAULT false;

UPDATE upstream_idp
SET issuer_url = provider_config->>'issuerUrl',
    client_id = provider_config->>'clientId',
    scopes = ARRAY(SELECT jsonb_array_elements_text(provider_config->'scopes')),
    allowed_domains = ARRAY(SELECT jsonb_array_elements_text(provider_config->'allowedDomains')),
    username_claim = provider_config->>'usernameClaim',
    display_name_claim = provider_config->>'displayNameClaim',
    email_claim = provider_config->>'emailClaim',
    picture_claim = provider_config->>'pictureClaim',
    require_verified_email = (provider_config->>'requireVerifiedEmail')::boolean,
    allow_private_network = (provider_config->>'allowPrivateNetwork')::boolean
WHERE protocol = 'oidc';
UPDATE upstream_idp SET scopes = '{}'::text[] WHERE protocol = 'steam';

ALTER TABLE upstream_idp
  ALTER COLUMN issuer_url DROP DEFAULT,
  ALTER COLUMN client_id DROP DEFAULT,
  DROP CONSTRAINT upstream_idp_protocol_check,
  ADD CONSTRAINT upstream_idp_protocol_check CHECK (protocol IN ('oidc','steam')),
  DROP CONSTRAINT upstream_idp_secret_tuple_check,
  ALTER COLUMN secret_enc SET NOT NULL,
  ALTER COLUMN secret_nonce SET NOT NULL,
  ALTER COLUMN key_version SET NOT NULL;
ALTER TABLE upstream_idp RENAME COLUMN secret_enc TO client_secret_enc;

DROP INDEX account_identity_upstream_data_gin_idx;
ALTER TABLE account_identity DROP COLUMN upstream_data;
ALTER TABLE upstream_idp
  DROP COLUMN provider_config,
  DROP COLUMN secret_status,
  DROP COLUMN secret_validated_at;
