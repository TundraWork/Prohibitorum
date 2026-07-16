-- name: GetAccountByID :one
SELECT * FROM account WHERE id = $1;

-- name: GetAccountByIDForUpdate :one
SELECT * FROM account WHERE id = $1 FOR UPDATE;

-- name: GetAccountByUsername :one
SELECT * FROM account WHERE username = $1;

-- name: GetAccountByWebauthnUserHandle :one
SELECT * FROM account WHERE webauthn_user_handle = $1;

-- name: InsertAccount :one
INSERT INTO account (
  username, display_name, webauthn_user_handle, role, attributes, disabled, email, email_verified
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: HasAnyActiveAdmin :one
SELECT EXISTS(SELECT 1 FROM account WHERE role = 'admin' AND NOT disabled) AS has_admin;

-- name: ListAccounts :many
SELECT
  a.*,
  (SELECT MAX(c.last_used_at) FROM webauthn_credential c WHERE c.account_id = a.id)::timestamptz AS last_sign_in_at,
  COALESCE((
    SELECT jsonb_agg(
      jsonb_build_object(
        'id', ai.id,
        'providerSlug', ip.slug,
        'providerDisplayName', ip.display_name,
        'protocol', ip.protocol,
        'subject', ai.upstream_sub,
        'email', ai.upstream_email,
        'data', ai.upstream_data,
        'linkedAt', ai.linked_at
      )
      ORDER BY ai.linked_at DESC, ai.id DESC
    )
    FROM account_identity ai
    JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
    CROSS JOIN LATERAL (
      SELECT CASE sqlc.narg('field')::text
        WHEN 'subject' THEN ai.upstream_sub
        WHEN 'email' THEN COALESCE(ai.upstream_email, '')
        WHEN 'steamId' THEN ai.upstream_sub
        WHEN 'personaName' THEN COALESCE(ai.upstream_data->>'personaName', '')
        WHEN 'userId' THEN ai.upstream_sub
        WHEN 'displayName' THEN COALESCE(ai.upstream_data->>'displayName', '')
        ELSE ''
      END AS field_value
    ) selected
    WHERE ai.account_id = a.id
      AND (
        (
          sqlc.narg('q')::text IS NOT NULL
          AND (
            ai.upstream_sub ILIKE '%' || sqlc.narg('q')::text || '%'
            OR COALESCE(ai.upstream_email, '') ILIKE '%' || sqlc.narg('q')::text || '%'
            OR COALESCE(ai.upstream_data->>'personaName', '') ILIKE '%' || sqlc.narg('q')::text || '%'
            OR COALESCE(ai.upstream_data->>'displayName', '') ILIKE '%' || sqlc.narg('q')::text || '%'
            OR COALESCE(ai.upstream_data->>'profileUrl', '') ILIKE '%' || sqlc.narg('q')::text || '%'
          )
        )
        OR (
          sqlc.narg('provider')::text IS NOT NULL
          AND ip.slug = sqlc.narg('provider')::text
          AND (
            sqlc.narg('field')::text IS NULL
            OR CASE
              WHEN sqlc.narg('match')::text = 'exact'
                AND sqlc.narg('field')::text IN ('personaName', 'displayName')
                THEN ai.upstream_data @> jsonb_build_object(
                  sqlc.narg('field')::text,
                  to_jsonb(sqlc.narg('value')::text)
                )
              WHEN sqlc.narg('match')::text = 'exact'
                THEN lower(selected.field_value) = lower(sqlc.narg('value')::text)
              WHEN sqlc.narg('match')::text = 'prefix'
                THEN lower(selected.field_value) LIKE lower(sqlc.narg('value')::text) || '%'
              WHEN sqlc.narg('match')::text = 'contains'
                THEN lower(selected.field_value) LIKE '%' || lower(sqlc.narg('value')::text) || '%'
              ELSE false
            END
          )
        )
      )
  ), '[]'::jsonb)::text AS matching_identities
FROM account a
WHERE (
    sqlc.narg('q')::text IS NULL
    OR a.id IN (
      SELECT searched.id
      FROM account searched
      WHERE (
        searched.username || E'\n' ||
        searched.display_name || E'\n' ||
        COALESCE(searched.email, '')
      ) ILIKE '%' || sqlc.narg('q')::text || '%'
      UNION
      SELECT ai.account_id
      FROM account_identity ai
      WHERE (
        ai.upstream_sub || E'\n' ||
        COALESCE(ai.upstream_email, '') || E'\n' ||
        COALESCE(ai.upstream_data->>'personaName', '') || E'\n' ||
        COALESCE(ai.upstream_data->>'displayName', '') || E'\n' ||
        COALESCE(ai.upstream_data->>'profileUrl', '')
      ) ILIKE '%' || sqlc.narg('q')::text || '%'
    )
  )
  AND (
    sqlc.narg('provider')::text IS NULL
    OR EXISTS (
      SELECT 1
      FROM account_identity ai
      JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
      CROSS JOIN LATERAL (
        SELECT CASE sqlc.narg('field')::text
          WHEN 'subject' THEN ai.upstream_sub
          WHEN 'email' THEN COALESCE(ai.upstream_email, '')
          WHEN 'steamId' THEN ai.upstream_sub
          WHEN 'personaName' THEN COALESCE(ai.upstream_data->>'personaName', '')
          WHEN 'userId' THEN ai.upstream_sub
          WHEN 'displayName' THEN COALESCE(ai.upstream_data->>'displayName', '')
          ELSE ''
        END AS field_value
      ) selected
      WHERE ai.account_id = a.id
        AND ip.slug = sqlc.narg('provider')::text
        AND (
          sqlc.narg('field')::text IS NULL
          OR CASE
            WHEN sqlc.narg('match')::text = 'exact'
              AND sqlc.narg('field')::text IN ('personaName', 'displayName')
              THEN ai.upstream_data @> jsonb_build_object(
                sqlc.narg('field')::text,
                to_jsonb(sqlc.narg('value')::text)
              )
            WHEN sqlc.narg('match')::text = 'exact'
              THEN lower(selected.field_value) = lower(sqlc.narg('value')::text)
            WHEN sqlc.narg('match')::text = 'prefix'
              THEN lower(selected.field_value) LIKE lower(sqlc.narg('value')::text) || '%'
            WHEN sqlc.narg('match')::text = 'contains'
              THEN lower(selected.field_value) LIKE '%' || lower(sqlc.narg('value')::text) || '%'
            ELSE false
          END
        )
    )
  )
  AND (
    sqlc.narg('after_created_at')::timestamptz IS NULL
    OR (a.created_at, a.id) < (sqlc.narg('after_created_at'), sqlc.narg('after_id')::int4)
  )
ORDER BY a.created_at DESC, a.id DESC
LIMIT sqlc.arg('limit');

-- name: UpdateAccount :one
UPDATE account SET
  display_name = $2, role = $3, attributes = $4, disabled = $5,
  email = $6, email_verified = $7,
  updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetAccountDisabled :one
UPDATE account SET disabled = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: UpdateAccountEmail :exec
-- Refreshes an account's email from a verified upstream on re-login (federation
-- claim drift), keeping it in lockstep with account_identity.upstream_email.
UPDATE account SET email = $2, email_verified = $3, updated_at = now() WHERE id = $1;

-- name: DeleteAccountByID :exec
DELETE FROM account WHERE id = $1;

-- name: CountActiveAdminsForUpdate :one
SELECT COUNT(*) FROM account WHERE role = 'admin' AND NOT disabled FOR UPDATE;

-- name: UpdateAccountDisplayName :exec
UPDATE account SET display_name = $2, updated_at = now() WHERE id = $1;

-- name: UpsertAvatarSource :exec
-- idp_id records the source upstream for an inherited avatar (NULL for a user
-- upload); source carries the upstream slug ("upstream:<slug>") so the
-- (account_id, source) PK yields one row per (account, upstream).
INSERT INTO account_avatar (account_id, source, bytes, content_type, etag, idp_id)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (account_id, source) DO UPDATE
  SET bytes = EXCLUDED.bytes, content_type = EXCLUDED.content_type, etag = EXCLUDED.etag, idp_id = EXCLUDED.idp_id;

-- name: SetActiveAvatar :exec
-- source is forced non-null text (the column is nullable, but this query only
-- ever sets a concrete sentinel) so callers cannot accidentally NULL the pointer.
UPDATE account SET
  avatar_source       = sqlc.arg(source)::text,
  avatar_etag         = (SELECT etag         FROM account_avatar av WHERE av.account_id = sqlc.arg(account_id) AND av.source = sqlc.arg(source)),
  avatar_content_type = (SELECT content_type FROM account_avatar av WHERE av.account_id = sqlc.arg(account_id) AND av.source = sqlc.arg(source)),
  updated_at = now()
WHERE id = sqlc.arg(account_id);

-- name: ClearActiveAvatar :exec
UPDATE account SET avatar_source = sqlc.arg(source)::text, avatar_etag = NULL, avatar_content_type = NULL, updated_at = now()
WHERE id = sqlc.arg(account_id);

-- name: DeleteAvatarSource :exec
DELETE FROM account_avatar WHERE account_id = $1 AND source = $2;

-- name: GetActiveAvatarBySubject :one
SELECT av.bytes, av.content_type, av.etag, a.disabled
FROM account a JOIN account_avatar av ON av.account_id = a.id AND av.source = a.avatar_source
WHERE a.oidc_subject = $1;

-- name: GetAvatarSourceBySubject :one
SELECT av.bytes, av.content_type, av.etag, a.disabled
FROM account a JOIN account_avatar av ON av.account_id = a.id
WHERE a.oidc_subject = $1 AND av.source = sqlc.arg(source);

-- name: ListAvatarSourcesByAccount :many
-- LEFT JOIN so the 'user' row (NULL idp_id) is kept with an empty label; the
-- join is by id (unconditional) so even a disabled upstream's inherited avatar
-- still resolves its display name.
SELECT av.source, av.etag, COALESCE(i.display_name, '') AS idp_display_name
FROM account_avatar av
LEFT JOIN upstream_idp i ON i.id = av.idp_id
WHERE av.account_id = $1;
