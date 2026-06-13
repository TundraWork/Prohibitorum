-- +goose Up
ALTER TABLE account ADD COLUMN avatar_content_type text;
ALTER TABLE account ADD COLUMN avatar_etag text;

CREATE TABLE account_avatar (
  account_id int PRIMARY KEY REFERENCES account(id) ON DELETE CASCADE,
  bytes      bytea NOT NULL
);

-- +goose Down
DROP TABLE account_avatar;
ALTER TABLE account DROP COLUMN avatar_etag;
ALTER TABLE account DROP COLUMN avatar_content_type;
