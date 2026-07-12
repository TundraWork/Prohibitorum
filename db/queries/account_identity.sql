-- name: GetAccountIdentityByIssuerSub :one
SELECT * FROM account_identity WHERE upstream_iss = $1 AND upstream_sub = $2;

-- name: ListAccountIdentitiesByAccount :many
SELECT ai.*, ip.slug AS idp_slug, ip.display_name AS idp_display_name
FROM account_identity ai
JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
WHERE ai.account_id = $1;

-- name: ListAccountIdentitiesByAccountPage :many
-- Keyset-paginated identities for an account, ordered by (linked_at DESC, id DESC).
-- NULL after_linked_at starts a new page. LIMIT is limit+1 for next-page detection.
SELECT ai.*, ip.slug AS idp_slug, ip.display_name AS idp_display_name
FROM account_identity ai
JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
WHERE ai.account_id = sqlc.arg(account_id)
  AND (sqlc.arg(after_linked_at)::timestamptz IS NULL OR (ai.linked_at, ai.id) < (sqlc.arg(after_linked_at), sqlc.arg(after_id)::bigint))
ORDER BY ai.linked_at DESC, ai.id DESC
LIMIT sqlc.arg(row_limit);

-- name: InsertAccountIdentity :one
INSERT INTO account_identity (account_id, upstream_idp_id, upstream_iss, upstream_sub, upstream_email)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: DeleteAccountIdentity :one
-- Returns the deleted row's id when one matched; pgx.ErrNoRows when the
-- (id, account_id) pair matches nothing (foreign identity, already-
-- deleted, or unknown id). Callers map ErrNoRows to a 404 + skip audit.
DELETE FROM account_identity WHERE id = $1 AND account_id = $2 RETURNING id;

-- name: UpdateAccountIdentityEmail :exec
UPDATE account_identity SET upstream_email = $2 WHERE id = $1;

-- name: CountUsableSignInFederation :one
-- Linked identities the account can actually sign in / step up with: the
-- upstream IdP must still exist and be enabled. (ListAccountIdentitiesByAccount
-- intentionally returns ALL links, incl. disabled-upstream, for display/unlink.)
SELECT COUNT(*) FROM account_identity ai
JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
WHERE ai.account_id = $1 AND NOT ip.disabled;

-- name: ConfirmAccountIdentity :exec
UPDATE account_identity SET confirmed_at = now() WHERE id = $1 AND confirmed_at IS NULL;
