-- +goose Up
UPDATE upstream_idp
SET provider_config = provider_config || '{"subjectClaim":"sub"}'::jsonb
WHERE protocol = 'oidc';

-- +goose Down
UPDATE upstream_idp
SET provider_config = provider_config - 'subjectClaim'
WHERE protocol = 'oidc';
