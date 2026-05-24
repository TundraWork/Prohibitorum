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

-- name: ConsumeEnrollment :one
-- Atomic single-use consume. Returns the row only if it was unconsumed and unexpired.
-- Callers detect any "not consumable" branch via pgx.ErrNoRows.
UPDATE enrollment
SET consumed_at = now()
WHERE token = $1 AND consumed_at IS NULL AND expires_at > now()
RETURNING *;

-- name: ListPendingInvitations :many
SELECT * FROM enrollment
WHERE intent = 'invite'
  AND consumed_at IS NULL
  AND expires_at > now()
ORDER BY created_at DESC;

-- name: RevokeInvitation :one
-- Same DB effect as ConsumeEnrollment but intent-restricted to 'invite' so an
-- admin cannot accidentally use this to mark a bootstrap/reset token consumed.
-- Returns the row only if it was unconsumed AND of intent=invite; otherwise
-- pgx.ErrNoRows surfaces and the handler maps to invitation_not_found.
UPDATE enrollment
SET consumed_at = now()
WHERE token = $1 AND intent = 'invite' AND consumed_at IS NULL
RETURNING *;
