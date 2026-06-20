-- +goose Up
-- Singleton instance-branding overrides. Exactly one row (id = 1); NULL columns
-- mean "no override — fall back to config / built-in default". The icon is a
-- pre-processed square PNG (see pkg/branding.ProcessIcon).
CREATE TABLE instance_settings (
  id            smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  instance_name text NULL,
  icon_png      bytea NULL,
  icon_etag     text NULL,
  updated_at    timestamptz NOT NULL DEFAULT now()
);
INSERT INTO instance_settings (id, instance_name, icon_png, icon_etag) VALUES (1, NULL, NULL, NULL);

-- +goose Down
DROP TABLE instance_settings;
