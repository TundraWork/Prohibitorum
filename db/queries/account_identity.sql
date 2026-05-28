-- name: GetAccountIdentityByIssuerSub :one
SELECT * FROM account_identity WHERE upstream_iss = $1 AND upstream_sub = $2;

-- name: ListAccountIdentitiesByAccount :many
SELECT ai.*, ip.slug AS idp_slug, ip.display_name AS idp_display_name
FROM account_identity ai
JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
WHERE ai.account_id = $1;

-- name: InsertAccountIdentity :one
INSERT INTO account_identity (account_id, upstream_idp_id, upstream_iss, upstream_sub, upstream_email)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: DeleteAccountIdentity :exec
DELETE FROM account_identity WHERE id = $1 AND account_id = $2;

-- name: UpdateAccountIdentityEmail :exec
UPDATE account_identity SET upstream_email = $2 WHERE id = $1;
