-- +goose Up
ALTER TABLE account_avatar ADD COLUMN IF NOT EXISTS source       text;
ALTER TABLE account_avatar ADD COLUMN IF NOT EXISTS content_type text;
ALTER TABLE account_avatar ADD COLUMN IF NOT EXISTS etag         text;
UPDATE account_avatar av SET
  source       = COALESCE((SELECT a.avatar_source       FROM account a WHERE a.id = av.account_id), 'user'),
  content_type = (SELECT a.avatar_content_type FROM account a WHERE a.id = av.account_id),
  etag         = (SELECT a.avatar_etag         FROM account a WHERE a.id = av.account_id)
WHERE source IS NULL;
ALTER TABLE account_avatar ALTER COLUMN source SET NOT NULL;
ALTER TABLE account_avatar DROP CONSTRAINT IF EXISTS account_avatar_pkey;
ALTER TABLE account_avatar ADD PRIMARY KEY (account_id, source);

-- +goose Down
DELETE FROM account_avatar a USING account_avatar b
  WHERE a.account_id = b.account_id AND a.source = 'upstream' AND b.source = 'user';
ALTER TABLE account_avatar DROP CONSTRAINT IF EXISTS account_avatar_pkey;
ALTER TABLE account_avatar ADD PRIMARY KEY (account_id);
ALTER TABLE account_avatar DROP COLUMN IF EXISTS etag;
ALTER TABLE account_avatar DROP COLUMN IF EXISTS content_type;
ALTER TABLE account_avatar DROP COLUMN IF EXISTS source;
