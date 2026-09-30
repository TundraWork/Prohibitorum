-- +goose Up
-- 045_login_appearance.sql — the sign-in page's look: a background source
-- (colour, gradient preset, Bing, Unsplash or uploaded images) and the surface
-- style of the card and the toolbar capsules. login_appearance holds that as one
-- JSON document; NULL means the defaults defined in pkg/branding. Uploaded
-- images move to their own table (up to 10, served verbatim, id order is upload
-- and carousel order). The Unsplash access key is sealed with the data
-- encryption key, like upstream IdP secrets.
CREATE TABLE login_background_image (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  data       bytea       NOT NULL,
  etag       text        NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE instance_settings
  ADD COLUMN login_appearance     jsonb NULL,
  ADD COLUMN unsplash_key_enc     bytea NULL,
  ADD COLUMN unsplash_key_nonce   bytea NULL,
  ADD COLUMN unsplash_key_version int   NULL,
  ADD CONSTRAINT instance_settings_unsplash_key_check CHECK (
    (unsplash_key_enc IS NULL AND unsplash_key_nonce IS NULL AND unsplash_key_version IS NULL)
    OR (unsplash_key_enc IS NOT NULL AND unsplash_key_nonce IS NOT NULL AND unsplash_key_version IS NOT NULL)
  );

-- An instance with a background keeps it: the image becomes the first upload
-- and the source becomes "images, random".
INSERT INTO login_background_image (data, etag)
SELECT login_bg, COALESCE(login_bg_etag, encode(sha256(login_bg), 'hex'))
FROM instance_settings
WHERE id = 1 AND login_bg IS NOT NULL;

UPDATE instance_settings
SET login_appearance = '{
  "background": {
    "source": "images",
    "color": "#1f6f8b",
    "gradient": "lagoon",
    "bing": {"market": "zh-CN", "showCaption": true},
    "unsplash": {"query": ""},
    "images": {"order": "random", "intervalSeconds": 10}
  },
  "card": {"translucent": false, "opacity": 80, "blur": true},
  "capsules": {"translucent": true, "opacity": 70, "blur": true}
}'::jsonb
WHERE id = 1 AND login_bg IS NOT NULL;

ALTER TABLE instance_settings
  DROP COLUMN login_bg,
  DROP COLUMN login_bg_etag;

-- +goose Down
ALTER TABLE instance_settings
  ADD COLUMN login_bg      bytea NULL,
  ADD COLUMN login_bg_etag text  NULL;

UPDATE instance_settings s
SET login_bg = i.data, login_bg_etag = i.etag
FROM (SELECT data, etag FROM login_background_image ORDER BY id LIMIT 1) i
WHERE s.id = 1;

ALTER TABLE instance_settings
  DROP CONSTRAINT instance_settings_unsplash_key_check,
  DROP COLUMN login_appearance,
  DROP COLUMN unsplash_key_enc,
  DROP COLUMN unsplash_key_nonce,
  DROP COLUMN unsplash_key_version;

DROP TABLE login_background_image;
