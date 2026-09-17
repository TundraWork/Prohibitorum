-- name: GetEnrollmentByToken :one
SELECT * FROM enrollment WHERE token = $1;

-- name: InsertEnrollment :one
INSERT INTO enrollment (
  token, intent, target_account_id, expires_at,
  template_role, template_attributes, expected_upstream_idp_slug,
  template_username, group_ids, created_by_account_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
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

-- name: ListInvitationGroups :many
SELECT id, slug, display_name
FROM user_group
WHERE id = ANY(sqlc.arg(group_ids)::integer[])
  AND kind = 'manual'
ORDER BY display_name ASC, id ASC;

-- name: ApplyInvitationGroups :many
WITH requested AS (
  SELECT DISTINCT unnest(sqlc.arg(group_ids)::integer[]) AS group_id
), locked AS (
  SELECT g.id
  FROM user_group g
  JOIN requested r ON r.group_id = g.id
  WHERE g.kind = 'manual'
  ORDER BY g.id
  FOR UPDATE
), applied AS (
  INSERT INTO group_manual_decision (group_id, account_id, effect, created_by)
  SELECT id, sqlc.arg(account_id), 'allow', sqlc.narg(created_by)
  FROM locked
  ON CONFLICT (group_id, account_id) DO UPDATE
  SET effect = 'allow', updated_at = now()
  RETURNING group_id
)
SELECT group_id FROM applied ORDER BY group_id;
