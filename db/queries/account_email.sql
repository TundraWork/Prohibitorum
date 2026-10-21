-- name: ListAccountEmails :many
-- Primary first, the rest by creation time. verified_by_username joins the
-- admin who asserted the address (verified_via='admin') so admin views can
-- show provenance without a second query.
SELECT ae.id, ae.account_id, ae.address, ae.verified_at, ae.verified_via,
       ae.verified_by_idp_id, ae.created_at,
       (a.primary_email_id = ae.id) AS is_primary,
       idp.display_name AS verified_provider,
       admin.username AS verified_by_username
FROM account a
JOIN account_email ae ON ae.account_id = a.id
LEFT JOIN upstream_idp idp ON idp.id = ae.verified_by_idp_id
LEFT JOIN account admin ON admin.id = ae.verified_by_admin_id
WHERE a.id = $1
ORDER BY is_primary DESC, ae.created_at, ae.id;

-- name: GetAccountEmail :one
SELECT ae.*, (a.primary_email_id = ae.id) AS is_primary
FROM account_email ae
JOIN account a ON a.id = ae.account_id
WHERE ae.id = $1 AND ae.account_id = $2;

-- name: InsertAccountEmail :one
INSERT INTO account_email (account_id, address)
VALUES ($1, $2)
RETURNING *;

-- name: InsertAccountEmailVerified :one
-- Used by admin assertion and upstream absorption; the caller has already
-- checked that lower(address) is not verified on another account.
INSERT INTO account_email (account_id, address, verified_at, verified_via, verified_by_idp_id, verified_by_admin_id)
VALUES ($1, $2, now(), $3, $4, $5)
RETURNING *;

-- name: MarkAccountEmailVerified :execrows
-- Bumps an existing unverified row to verified. Returns 0 rows when another
-- account already verified the address (the partial unique index rejects it)
-- or the row vanished.
UPDATE account_email SET verified_at = now(), verified_via = $3, verified_by_idp_id = $4, verified_by_admin_id = $5
WHERE id = $1 AND account_id = $2 AND verified_at IS NULL;

-- name: DeleteAccountEmail :execrows
DELETE FROM account_email WHERE id = $1 AND account_id = $2;

-- name: CountAccountEmails :one
SELECT count(*) FROM account_email WHERE account_id = $1;

-- name: GetAccountPrimaryEmail :one
-- The single read every downstream projection (OIDC claims, forward-auth,
-- SAML, federation confirm) goes through; ErrNoRows when the account has no
-- primary.
SELECT ae.address
FROM account a
JOIN account_email ae ON ae.id = a.primary_email_id
WHERE a.id = $1;

-- name: SetAccountPrimaryEmail :exec
UPDATE account SET primary_email_id = $2, updated_at = now() WHERE id = $1;

-- name: ClearAccountPrimaryEmail :exec
UPDATE account SET primary_email_id = NULL, updated_at = now() WHERE id = $1;

-- name: GetAccountEmailByAddress :one
SELECT ae.*, (a.primary_email_id = ae.id) AS is_primary
FROM account_email ae
JOIN account a ON a.id = ae.account_id
WHERE ae.account_id = $1 AND lower(ae.address) = lower($2);

-- name: PickFallbackPrimary :one
-- The earliest-verified other address, used when an admin removes the
-- primary. ErrNoRows when nothing can take over.
SELECT id, address FROM account_email
WHERE account_id = $1 AND verified_at IS NOT NULL AND id <> $2
ORDER BY verified_at, id
LIMIT 1;

-- name: CountVerifiedAccountEmailsByAddress :one
-- Cross-account check for admin assertion: how many OTHER accounts already
-- verified this address (0 or 1 under the partial unique index).
SELECT count(*) FROM account_email
WHERE verified_at IS NOT NULL AND lower(address) = lower($1) AND account_id <> $2;
