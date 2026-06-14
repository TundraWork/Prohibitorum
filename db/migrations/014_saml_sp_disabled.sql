-- +goose Up
ALTER TABLE saml_sp ADD COLUMN disabled boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE saml_sp DROP COLUMN disabled;
