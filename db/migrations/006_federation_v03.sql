-- +goose Up

ALTER TABLE upstream_idp
  ADD COLUMN require_verified_email boolean NOT NULL DEFAULT true;

-- +goose Down

ALTER TABLE upstream_idp
  DROP COLUMN require_verified_email;
