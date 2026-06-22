-- +goose Up
ALTER TABLE oidc_client ADD COLUMN launch_url text;

-- +goose Down
ALTER TABLE oidc_client DROP COLUMN launch_url;
