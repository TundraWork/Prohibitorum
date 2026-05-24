-- name: GetAccountByID :one
SELECT * FROM account WHERE id = $1;

-- name: GetAccountByUsername :one
SELECT * FROM account WHERE username = $1;

-- name: GetAccountByWebauthnUserHandle :one
SELECT * FROM account WHERE webauthn_user_handle = $1;

-- name: InsertAccount :one
INSERT INTO account (
  username, display_name, webauthn_user_handle, role,
  can_view_own_usage, can_manage_own_api_keys, can_view_models, can_view_own_traces,
  can_manage_own_projects,
  disabled
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
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
  display_name = $2, role = $3,
  can_view_own_usage = $4, can_manage_own_api_keys = $5,
  can_view_models = $6, can_view_own_traces = $7,
  can_manage_own_projects = $8,
  disabled = $9, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteAccountByID :exec
DELETE FROM account WHERE id = $1;

-- name: CountActiveAdminsForUpdate :one
SELECT COUNT(*) FROM account WHERE role = 'admin' AND NOT disabled FOR UPDATE;
