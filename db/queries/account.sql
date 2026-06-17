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
  (SELECT MAX(c.last_used_at) FROM webauthn_credential c WHERE c.account_id = a.id)::timestamptz AS last_sign_in_at
FROM account a
ORDER BY a.created_at ASC, a.id ASC;

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
