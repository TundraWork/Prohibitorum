-- +goose Up
ALTER TABLE account ADD COLUMN IF NOT EXISTS avatar_content_type text;
ALTER TABLE account ADD COLUMN IF NOT EXISTS avatar_etag text;

CREATE TABLE IF NOT EXISTS account_avatar (
  account_id int PRIMARY KEY REFERENCES account(id) ON DELETE CASCADE,
  bytes      bytea NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS account_avatar;
ALTER TABLE account DROP COLUMN IF EXISTS avatar_etag;
ALTER TABLE account DROP COLUMN IF EXISTS avatar_content_type;
