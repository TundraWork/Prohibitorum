-- name: GetEnrollmentByToken :one
SELECT * FROM enrollment WHERE token = $1;

-- name: InsertEnrollment :one
INSERT INTO enrollment (
  token, intent, target_account_id, expires_at,
  template_role, template_attributes, expected_upstream_idp_slug,
  template_username, template_display_name
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: InsertFederatedRegistrationEnrollment :one
INSERT INTO enrollment (
  token, intent, expires_at,
  federated_upstream_idp_id, federated_upstream_idp_slug,
  federated_upstream_iss, federated_upstream_sub,
  federated_display_name, federated_upstream_data, federated_avatar_url
)
VALUES (
  sqlc.arg('token'), 'federated_register', sqlc.arg('expires_at'),
  sqlc.arg('federated_upstream_idp_id'), sqlc.arg('federated_upstream_idp_slug'),
  sqlc.arg('federated_upstream_iss'), sqlc.arg('federated_upstream_sub'),
  sqlc.arg('federated_display_name'), sqlc.arg('federated_upstream_data'),
  sqlc.narg('federated_avatar_url')
)
RETURNING *;

-- name: InsertProviderRecoveryEnrollment :one
INSERT INTO enrollment (
  token, intent, target_account_id, expires_at, recovery_source_upstream_idp_id
)
VALUES (
  sqlc.arg('token'), 'reset', sqlc.arg('target_account_id'),
  sqlc.arg('expires_at'), sqlc.arg('recovery_source_upstream_idp_id')
)
RETURNING *;

-- name: ConsumeEnrollment :one
-- Atomic single-use consume. Returns the row only if it was unconsumed and unexpired.
-- Callers detect any "not consumable" branch via pgx.ErrNoRows.
UPDATE enrollment
SET consumed_at = now()
WHERE token = $1 AND consumed_at IS NULL AND expires_at > now()
RETURNING *;

-- name: ConsumeInviteEnrollment :one
-- Atomic single-use consume, intent-restricted to 'invite' AND unexpired. Used
-- by the federation invite-redemption path (applyInviteOnly) so a bootstrap or
-- reset token can never be marked consumed via the federation callback —
-- defense-in-depth on top of the begin-time intent gate (audit OIDCFED-2).
-- pgx.ErrNoRows surfaces on any not-consumable branch.
UPDATE enrollment
SET consumed_at = now()
WHERE token = $1 AND intent = 'invite' AND consumed_at IS NULL AND expires_at > now()
RETURNING *;

-- name: ListPendingInvitations :many
SELECT * FROM enrollment
WHERE intent = 'invite'
  AND consumed_at IS NULL
  AND expires_at > now()
  AND (sqlc.narg('after_created_at')::timestamptz IS NULL OR (created_at, token) < (sqlc.narg('after_created_at'), sqlc.narg('after_token')::text))
ORDER BY created_at DESC, token DESC
LIMIT sqlc.arg('limit');

-- name: RevokeInvitation :one
-- Same DB effect as ConsumeEnrollment but intent-restricted to 'invite' so an
-- admin cannot accidentally use this to mark a bootstrap/reset token consumed.
-- Returns the row only if it was unconsumed AND of intent=invite; otherwise
-- pgx.ErrNoRows surfaces and the handler maps to invitation_not_found.
UPDATE enrollment
SET consumed_at = now()
WHERE token = $1 AND intent = 'invite' AND consumed_at IS NULL
RETURNING *;
