-- +goose Up
CREATE TABLE entity_icon (
  owner_kind text        NOT NULL,
  owner_id   text        NOT NULL,
  png        bytea       NOT NULL,
  etag       text        NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (owner_kind, owner_id)
);

-- +goose Down
DROP TABLE entity_icon;
