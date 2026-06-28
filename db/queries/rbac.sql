-- name: CreateGroup :one
INSERT INTO user_group (slug, display_name, description, exposed_to_downstream)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetGroup :one
SELECT * FROM user_group WHERE id = $1;

-- name: GetGroupBySlug :one
SELECT * FROM user_group WHERE slug = $1;

-- name: ListGroups :many
SELECT g.*, (SELECT count(*) FROM group_member m WHERE m.group_id = g.id) AS member_count
FROM user_group g
ORDER BY g.display_name;

-- name: UpdateGroup :one
UPDATE user_group
SET slug = $2, display_name = $3, description = $4, exposed_to_downstream = $5, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteGroup :execrows
DELETE FROM user_group WHERE id = $1;

-- name: AddGroupMember :exec
INSERT INTO group_member (group_id, account_id)
VALUES ($1, $2)
ON CONFLICT (group_id, account_id) DO NOTHING;

-- name: RemoveGroupMember :execrows
DELETE FROM group_member WHERE group_id = $1 AND account_id = $2;

-- name: ListGroupMembers :many
SELECT a.id, a.username, a.display_name
FROM group_member m
JOIN account a ON a.id = m.account_id
WHERE m.group_id = $1
ORDER BY a.username;

-- name: ListGroupsForAccount :many
SELECT g.*
FROM group_member m
JOIN user_group g ON g.id = m.group_id
WHERE m.account_id = $1
ORDER BY g.display_name;

-- name: ListExposedGroupSlugsByAccount :many
SELECT g.slug
FROM group_member m
JOIN user_group g ON g.id = m.group_id
WHERE m.account_id = $1 AND g.exposed_to_downstream
ORDER BY g.slug;

-- name: GrantOIDCClientAccessGroup :exec
INSERT INTO oidc_client_access (client_id, group_id) VALUES ($1, $2)
ON CONFLICT (client_id, group_id) WHERE group_id IS NOT NULL DO NOTHING;

-- name: GrantOIDCClientAccessAccount :exec
INSERT INTO oidc_client_access (client_id, account_id) VALUES ($1, $2)
ON CONFLICT (client_id, account_id) WHERE account_id IS NOT NULL DO NOTHING;

-- name: RevokeOIDCClientAccessGroup :execrows
DELETE FROM oidc_client_access WHERE client_id = $1 AND group_id = $2;

-- name: RevokeOIDCClientAccessAccount :execrows
DELETE FROM oidc_client_access WHERE client_id = $1 AND account_id = $2;

-- name: ListOIDCClientAccessGroups :many
SELECT g.id, g.slug, g.display_name
FROM oidc_client_access a JOIN user_group g ON g.id = a.group_id
WHERE a.client_id = $1 ORDER BY g.display_name;

-- name: ListOIDCClientAccessAccounts :many
SELECT acc.id, acc.username, acc.display_name
FROM oidc_client_access a JOIN account acc ON acc.id = a.account_id
WHERE a.client_id = $1 ORDER BY acc.username;

-- name: GrantSAMLSPAccessGroup :exec
INSERT INTO saml_sp_access (saml_sp_id, group_id) VALUES ($1, $2)
ON CONFLICT (saml_sp_id, group_id) WHERE group_id IS NOT NULL DO NOTHING;

-- name: GrantSAMLSPAccessAccount :exec
INSERT INTO saml_sp_access (saml_sp_id, account_id) VALUES ($1, $2)
ON CONFLICT (saml_sp_id, account_id) WHERE account_id IS NOT NULL DO NOTHING;

-- name: RevokeSAMLSPAccessGroup :execrows
DELETE FROM saml_sp_access WHERE saml_sp_id = $1 AND group_id = $2;

-- name: RevokeSAMLSPAccessAccount :execrows
DELETE FROM saml_sp_access WHERE saml_sp_id = $1 AND account_id = $2;

-- name: ListSAMLSPAccessGroups :many
SELECT g.id, g.slug, g.display_name
FROM saml_sp_access a JOIN user_group g ON g.id = a.group_id
WHERE a.saml_sp_id = $1 ORDER BY g.display_name;

-- name: ListSAMLSPAccessAccounts :many
SELECT acc.id, acc.username, acc.display_name
FROM saml_sp_access a JOIN account acc ON acc.id = a.account_id
WHERE a.saml_sp_id = $1 ORDER BY acc.username;

-- name: SetOIDCClientAccessRestricted :one
UPDATE oidc_client SET access_restricted = $2 WHERE client_id = $1 RETURNING *;

-- name: SetSAMLSPAccessRestricted :one
UPDATE saml_sp SET access_restricted = $2 WHERE id = $1 RETURNING *;

-- name: IsAccountAuthorizedForOIDCClient :one
SELECT
  NOT c.access_restricted
  OR EXISTS (SELECT 1 FROM oidc_client_access a
             WHERE a.client_id = c.client_id AND a.account_id = sqlc.arg(account_id))
  OR EXISTS (SELECT 1 FROM oidc_client_access a
             JOIN group_member m ON m.group_id = a.group_id
             WHERE a.client_id = c.client_id AND m.account_id = sqlc.arg(account_id))
FROM oidc_client c
WHERE c.client_id = sqlc.arg(client_id);

-- name: IsAccountAuthorizedForSAMLSP :one
SELECT
  NOT s.access_restricted
  OR EXISTS (SELECT 1 FROM saml_sp_access a
             WHERE a.saml_sp_id = s.id AND a.account_id = sqlc.arg(account_id))
  OR EXISTS (SELECT 1 FROM saml_sp_access a
             JOIN group_member m ON m.group_id = a.group_id
             WHERE a.saml_sp_id = s.id AND m.account_id = sqlc.arg(account_id))
FROM saml_sp s
WHERE s.id = sqlc.arg(sp_id);

-- name: ListAuthorizedOIDCClientsForAccount :many
SELECT c.client_id, c.display_name, c.launch_url, c.redirect_uris
FROM oidc_client c
WHERE c.disabled = false
  AND c.forward_auth_enabled = false
  AND (
    NOT c.access_restricted
    OR EXISTS (SELECT 1 FROM oidc_client_access a
               WHERE a.client_id = c.client_id AND a.account_id = sqlc.arg(account_id))
    OR EXISTS (SELECT 1 FROM oidc_client_access a
               JOIN group_member m ON m.group_id = a.group_id
               WHERE a.client_id = c.client_id AND m.account_id = sqlc.arg(account_id))
  )
ORDER BY c.display_name;

-- name: ListAuthorizedForwardAuthAppsForAccount :many
SELECT c.client_id, c.display_name, c.forward_auth_host, c.forward_auth_scopes
FROM oidc_client c
WHERE c.disabled = false
  AND c.forward_auth_enabled = true
  AND c.forward_auth_host IS NOT NULL
  AND (
    NOT c.access_restricted
    OR EXISTS (SELECT 1 FROM oidc_client_access a
               WHERE a.client_id = c.client_id AND a.account_id = sqlc.arg(account_id))
    OR EXISTS (SELECT 1 FROM oidc_client_access a
               JOIN group_member m ON m.group_id = a.group_id
               WHERE a.client_id = c.client_id AND m.account_id = sqlc.arg(account_id))
  )
ORDER BY c.display_name;

-- name: ListAuthorizedSAMLSPsForAccount :many
SELECT s.id, s.entity_id, s.display_name
FROM saml_sp s
WHERE s.disabled = false
  AND s.allow_idp_initiated = true
  AND (
    NOT s.access_restricted
    OR EXISTS (SELECT 1 FROM saml_sp_access a
               WHERE a.saml_sp_id = s.id AND a.account_id = sqlc.arg(account_id))
    OR EXISTS (SELECT 1 FROM saml_sp_access a
               JOIN group_member m ON m.group_id = a.group_id
               WHERE a.saml_sp_id = s.id AND m.account_id = sqlc.arg(account_id))
  )
ORDER BY s.display_name;
