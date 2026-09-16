-- +goose Up
UPDATE upstream_idp
SET provider_config = provider_config || '{"configurationMode":"discovery","endpoints":{"authorization":null,"token":null,"userinfo":null,"jwks":null},"tokenAuthMethod":"discovery","pkceMethod":"S256"}'::jsonb
WHERE protocol = 'oidc';

-- +goose Down
UPDATE upstream_idp
SET provider_config = provider_config - 'configurationMode' - 'endpoints' - 'tokenAuthMethod' - 'pkceMethod'
WHERE protocol = 'oidc';
