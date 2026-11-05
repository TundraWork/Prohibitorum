-- +goose Up
-- 057_account_ref_key.sql — PHB-285: key for the short account reference `u`
-- that selects an account in a tab's URL. Generated at first start; kept apart
-- from the data encryption keys so rotating those does not change any `u`.
ALTER TABLE instance_settings ADD COLUMN account_ref_key bytea NULL;

-- +goose Down
ALTER TABLE instance_settings DROP COLUMN account_ref_key;
