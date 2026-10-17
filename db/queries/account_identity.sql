-- name: GetAccountIdentityByIssuerSub :one
SELECT * FROM account_identity WHERE upstream_iss = $1 AND upstream_sub = $2;

-- name: ListAccountIdentitiesByAccount :many
SELECT ai.*, ip.slug AS idp_slug, ip.display_name AS idp_display_name, ip.protocol
FROM account_identity ai
JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
WHERE ai.account_id = $1;

-- name: InsertAccountIdentity :one
INSERT INTO account_identity (
  account_id, upstream_idp_id, upstream_iss, upstream_sub, upstream_email, upstream_data
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: DeleteAccountIdentity :one
-- Returns the deleted row's id when one matched; pgx.ErrNoRows when the
-- (id, account_id) pair matches nothing (foreign identity, already-
-- deleted, or unknown id). Callers map ErrNoRows to a 404 + skip audit.
DELETE FROM account_identity WHERE id = $1 AND account_id = $2 RETURNING id;

-- name: DeleteAccountIdentitiesByUpstreamIDP :many
-- Every identity linked to one provider, removed together when the provider
-- itself is deleted. The account rows survive: an account that could also sign
-- in another way keeps those, and one that could only use this provider is
-- left for an admin to reissue an enrollment for. RETURNING account_id lets the
-- caller audit exactly whose links were dropped.
DELETE FROM account_identity WHERE upstream_idp_id = $1 RETURNING account_id;

-- name: CountAccountsLinkedToUpstreamIDP :one
-- How many accounts hold an identity from this provider, distinct because one
-- account can link the same provider twice (two subjects).
SELECT COUNT(DISTINCT account_id)::bigint FROM account_identity WHERE upstream_idp_id = $1;

-- name: CountAccountsLinkedToUpstreamIDPs :many
-- The same count as CountAccountsLinkedToUpstreamIDP for a whole page of
-- providers at once, so the list can fill linkedAccountCount without a query
-- per row. Providers with no linked identity produce no row here; the caller
-- reads a missing id as zero.
SELECT upstream_idp_id, COUNT(DISTINCT account_id)::bigint AS linked_account_count
FROM account_identity
WHERE upstream_idp_id = ANY(sqlc.arg(upstream_idp_ids)::bigint[])
GROUP BY upstream_idp_id
ORDER BY upstream_idp_id;

-- name: UpdateAccountIdentityVerifiedData :exec
UPDATE account_identity
SET upstream_email = $2, upstream_data = $3
WHERE id = $1;

-- name: CountUsableSignInFederation :one
-- Linked identities the account can actually sign in / step up with: the
-- upstream IdP must still exist, be enabled, and allow sign-in.
-- ListAccountIdentitiesByAccount intentionally returns all links, including
-- disabled and sign-in-disabled providers, for display/unlink.
SELECT COUNT(*) FROM account_identity ai
JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
WHERE ai.account_id = $1 AND NOT ip.disabled AND ip.allow_sign_in;

-- name: ConfirmAccountIdentity :exec
UPDATE account_identity SET confirmed_at = now() WHERE id = $1 AND confirmed_at IS NULL;

-- name: ListConfirmedIdentitiesForProviders :many
-- The confirmed links an account holds at the given enabled providers, for the
-- identities claim of an identity:<slug> scope.
SELECT ip.slug AS idp_slug, ai.upstream_sub, ai.upstream_data
FROM account_identity ai
JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
WHERE ai.account_id = sqlc.arg(account_id)
  AND ai.confirmed_at IS NOT NULL
  AND NOT ip.disabled
  AND ip.slug = ANY(sqlc.arg(slugs)::text[])
ORDER BY ai.linked_at, ai.id;

-- name: HasConfirmedIdentityForProviders :one
-- Whether the account holds a confirmed link at any of the given enabled
-- providers: what an application's require-identity request asks.
SELECT EXISTS (
  SELECT 1
  FROM account_identity ai
  JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
  WHERE ai.account_id = sqlc.arg(account_id)
    AND ai.confirmed_at IS NOT NULL
    AND NOT ip.disabled
    AND ip.slug = ANY(sqlc.arg(slugs)::text[])
);
