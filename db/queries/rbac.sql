-- name: CreateOIDCAppGroup :one
INSERT INTO user_group (
  kind, slug, display_name, description, exposed_to_downstream, rule, oidc_client_id
)
VALUES (
  sqlc.arg(kind), sqlc.arg(slug), sqlc.arg(display_name), sqlc.narg(description),
  sqlc.arg(exposed_to_downstream), sqlc.narg(rule), sqlc.arg(oidc_client_id)::text
)
RETURNING *;

-- name: CreateSAMLAppGroup :one
INSERT INTO user_group (
  kind, slug, display_name, description, exposed_to_downstream, rule, saml_sp_id
)
VALUES (
  sqlc.arg(kind), sqlc.arg(slug), sqlc.arg(display_name), sqlc.narg(description),
  sqlc.arg(exposed_to_downstream), sqlc.narg(rule), sqlc.arg(saml_sp_id)::bigint
)
RETURNING *;

-- name: GetOIDCAppGroup :one
SELECT *
FROM user_group
WHERE id = sqlc.arg(group_id)
  AND oidc_client_id = sqlc.arg(oidc_client_id)::text;

-- name: GetSAMLAppGroup :one
SELECT *
FROM user_group
WHERE id = sqlc.arg(group_id)
  AND saml_sp_id = sqlc.arg(saml_sp_id)::bigint;

-- name: ListOIDCAppGroups :many
SELECT *
FROM user_group
WHERE oidc_client_id = sqlc.arg(oidc_client_id)::text
ORDER BY display_name ASC, id ASC;

-- name: ListSAMLAppGroups :many
SELECT *
FROM user_group
WHERE saml_sp_id = sqlc.arg(saml_sp_id)::bigint
ORDER BY display_name ASC, id ASC;

-- name: UpdateAppGroup :one
UPDATE user_group
SET slug = sqlc.arg(slug),
    display_name = sqlc.arg(display_name),
    description = sqlc.narg(description),
    exposed_to_downstream = sqlc.arg(exposed_to_downstream),
    rule = sqlc.narg(rule),
    updated_at = now()
WHERE id = sqlc.arg(group_id)
  AND num_nonnulls(
    sqlc.narg(oidc_client_id)::text,
    sqlc.narg(saml_sp_id)::bigint
  ) = 1
  AND (
    user_group.oidc_client_id = sqlc.narg(oidc_client_id)::text
    OR user_group.saml_sp_id = sqlc.narg(saml_sp_id)::bigint
  )
RETURNING *;

-- name: DeleteOIDCAppGroup :execrows
DELETE FROM user_group
WHERE id = sqlc.arg(group_id)
  AND oidc_client_id = sqlc.arg(oidc_client_id)::text;

-- name: DeleteSAMLAppGroup :execrows
DELETE FROM user_group
WHERE id = sqlc.arg(group_id)
  AND saml_sp_id = sqlc.arg(saml_sp_id)::bigint;

-- name: GetManualDecisionForOIDCApp :one
SELECT d.*
FROM group_manual_decision d
JOIN user_group g ON g.id = d.group_id AND g.kind = d.group_kind
WHERE g.oidc_client_id = sqlc.arg(oidc_client_id)::text
  AND g.kind = 'manual'
  AND d.account_id = sqlc.arg(account_id);

-- name: GetManualDecisionForSAMLApp :one
SELECT d.*
FROM group_manual_decision d
JOIN user_group g ON g.id = d.group_id AND g.kind = d.group_kind
WHERE g.saml_sp_id = sqlc.arg(saml_sp_id)::bigint
  AND g.kind = 'manual'
  AND d.account_id = sqlc.arg(account_id);

-- name: ListManualDecisionsPage :many
SELECT
  d.group_id,
  d.account_id,
  d.effect,
  d.created_at,
  d.updated_at,
  d.created_by,
  a.username,
  a.display_name,
  a.disabled
FROM group_manual_decision d
JOIN account a ON a.id = d.account_id
WHERE d.group_id = sqlc.arg(group_id)
  AND (
    sqlc.narg(after_username)::text IS NULL
    OR (a.username, a.id) > (sqlc.narg(after_username), sqlc.narg(after_account_id)::int4)
  )
ORDER BY a.username ASC, a.id ASC
LIMIT sqlc.arg(row_limit);

-- name: UpsertManualDecision :one
INSERT INTO group_manual_decision (group_id, account_id, effect, created_by)
VALUES (
  sqlc.arg(group_id), sqlc.arg(account_id), sqlc.arg(effect), sqlc.narg(created_by)
)
ON CONFLICT (group_id, account_id) DO UPDATE
SET effect = EXCLUDED.effect,
    updated_at = now()
RETURNING *;

-- name: ClearManualDecision :execrows
DELETE FROM group_manual_decision
WHERE group_id = sqlc.arg(group_id)
  AND account_id = sqlc.arg(account_id);

-- name: ListOIDCClientManagers :many
SELECT
  m.client_id,
  m.account_id,
  m.created_at,
  m.created_by,
  a.username,
  a.display_name,
  a.role,
  a.disabled
FROM oidc_client_manager m
JOIN account a ON a.id = m.account_id
WHERE m.client_id = sqlc.arg(client_id)
ORDER BY a.username ASC, a.id ASC;

-- name: AssignOIDCClientManager :exec
INSERT INTO oidc_client_manager (client_id, account_id, created_by)
VALUES (sqlc.arg(client_id), sqlc.arg(account_id), sqlc.narg(created_by))
ON CONFLICT (client_id, account_id) DO NOTHING;

-- name: RemoveOIDCClientManager :execrows
DELETE FROM oidc_client_manager
WHERE client_id = sqlc.arg(client_id)
  AND account_id = sqlc.arg(account_id);

-- name: ListSAMLSPManagers :many
SELECT
  m.saml_sp_id,
  m.account_id,
  m.created_at,
  m.created_by,
  a.username,
  a.display_name,
  a.role,
  a.disabled
FROM saml_sp_manager m
JOIN account a ON a.id = m.account_id
WHERE m.saml_sp_id = sqlc.arg(saml_sp_id)
ORDER BY a.username ASC, a.id ASC;

-- name: AssignSAMLSPManager :exec
INSERT INTO saml_sp_manager (saml_sp_id, account_id, created_by)
VALUES (sqlc.arg(saml_sp_id), sqlc.arg(account_id), sqlc.narg(created_by))
ON CONFLICT (saml_sp_id, account_id) DO NOTHING;

-- name: RemoveSAMLSPManager :execrows
DELETE FROM saml_sp_manager
WHERE saml_sp_id = sqlc.arg(saml_sp_id)
  AND account_id = sqlc.arg(account_id);

-- name: IsOIDCClientManager :one
SELECT EXISTS (
  SELECT 1
  FROM oidc_client_manager
  WHERE client_id = sqlc.arg(client_id)
    AND account_id = sqlc.arg(account_id)
);

-- name: IsSAMLSPManager :one
SELECT EXISTS (
  SELECT 1
  FROM saml_sp_manager
  WHERE saml_sp_id = sqlc.arg(saml_sp_id)
    AND account_id = sqlc.arg(account_id)
);

-- name: DeleteManagerAssignmentsForAccount :exec
WITH deleted_oidc AS (
  DELETE FROM oidc_client_manager
  WHERE oidc_client_manager.account_id = sqlc.arg(account_id)
  RETURNING account_id
)
DELETE FROM saml_sp_manager
WHERE saml_sp_manager.account_id = sqlc.arg(account_id);

-- name: GetAccountAccessFacts :one
SELECT
  a.id,
  a.username,
  a.display_name,
  a.disabled,
  EXISTS (
    SELECT 1 FROM webauthn_credential w WHERE w.account_id = a.id
  ) AS has_passkey,
  EXISTS (
    SELECT 1
    FROM password_credential p
    WHERE p.account_id = a.id
      AND EXISTS (
        SELECT 1
        FROM totp_credential t
        WHERE t.account_id = a.id AND t.confirmed_at IS NOT NULL
      )
  ) AS has_password_totp,
  EXISTS (
    SELECT 1
    FROM account_identity ai
    JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
    WHERE ai.account_id = a.id
      AND ai.confirmed_at IS NOT NULL
      AND NOT ip.disabled
      AND ip.protocol <> 'vrchat'
  ) AS has_federation,
  ARRAY(
    SELECT DISTINCT ip.slug
    FROM account_identity ai
    JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
    WHERE ai.account_id = a.id
      AND ai.confirmed_at IS NOT NULL
    ORDER BY ip.slug
  )::text[] AS confirmed_provider_slugs,
  ARRAY(
    SELECT DISTINCT ip.protocol
    FROM account_identity ai
    JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
    WHERE ai.account_id = a.id
      AND ai.confirmed_at IS NOT NULL
    ORDER BY ip.protocol
  )::text[] AS confirmed_protocols,
  EXISTS (
    SELECT 1 FROM account_avatar av WHERE av.account_id = a.id
  ) AS has_any_avatar,
  EXISTS (
    SELECT 1
    FROM account_avatar av
    WHERE av.account_id = a.id AND av.source = 'user'
  ) AS has_user_avatar
FROM account a
WHERE a.id = sqlc.arg(account_id);

-- name: ListActiveAccountAccessFacts :many
SELECT
  a.id,
  a.username,
  a.display_name,
  a.disabled,
  EXISTS (
    SELECT 1 FROM webauthn_credential w WHERE w.account_id = a.id
  ) AS has_passkey,
  EXISTS (
    SELECT 1
    FROM password_credential p
    WHERE p.account_id = a.id
      AND EXISTS (
        SELECT 1
        FROM totp_credential t
        WHERE t.account_id = a.id AND t.confirmed_at IS NOT NULL
      )
  ) AS has_password_totp,
  EXISTS (
    SELECT 1
    FROM account_identity ai
    JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
    WHERE ai.account_id = a.id
      AND ai.confirmed_at IS NOT NULL
      AND NOT ip.disabled
      AND ip.protocol <> 'vrchat'
  ) AS has_federation,
  ARRAY(
    SELECT DISTINCT ip.slug
    FROM account_identity ai
    JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
    WHERE ai.account_id = a.id
      AND ai.confirmed_at IS NOT NULL
    ORDER BY ip.slug
  )::text[] AS confirmed_provider_slugs,
  ARRAY(
    SELECT DISTINCT ip.protocol
    FROM account_identity ai
    JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
    WHERE ai.account_id = a.id
      AND ai.confirmed_at IS NOT NULL
    ORDER BY ip.protocol
  )::text[] AS confirmed_protocols,
  EXISTS (
    SELECT 1 FROM account_avatar av WHERE av.account_id = a.id
  ) AS has_any_avatar,
  EXISTS (
    SELECT 1
    FROM account_avatar av
    WHERE av.account_id = a.id AND av.source = 'user'
  ) AS has_user_avatar
FROM account a
WHERE NOT a.disabled
ORDER BY a.username ASC, a.id ASC;

-- name: ListActiveAccountAccessFactsPage :many
SELECT
  a.id,
  a.username,
  a.display_name,
  a.disabled,
  EXISTS (
    SELECT 1 FROM webauthn_credential w WHERE w.account_id = a.id
  ) AS has_passkey,
  EXISTS (
    SELECT 1
    FROM password_credential p
    WHERE p.account_id = a.id
      AND EXISTS (
        SELECT 1
        FROM totp_credential t
        WHERE t.account_id = a.id AND t.confirmed_at IS NOT NULL
      )
  ) AS has_password_totp,
  EXISTS (
    SELECT 1
    FROM account_identity ai
    JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
    WHERE ai.account_id = a.id
      AND ai.confirmed_at IS NOT NULL
      AND NOT ip.disabled
      AND ip.protocol <> 'vrchat'
  ) AS has_federation,
  ARRAY(
    SELECT DISTINCT ip.slug
    FROM account_identity ai
    JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
    WHERE ai.account_id = a.id
      AND ai.confirmed_at IS NOT NULL
    ORDER BY ip.slug
  )::text[] AS confirmed_provider_slugs,
  ARRAY(
    SELECT DISTINCT ip.protocol
    FROM account_identity ai
    JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
    WHERE ai.account_id = a.id
      AND ai.confirmed_at IS NOT NULL
    ORDER BY ip.protocol
  )::text[] AS confirmed_protocols,
  EXISTS (
    SELECT 1 FROM account_avatar av WHERE av.account_id = a.id
  ) AS has_any_avatar,
  EXISTS (
    SELECT 1
    FROM account_avatar av
    WHERE av.account_id = a.id AND av.source = 'user'
  ) AS has_user_avatar
FROM account a
WHERE NOT a.disabled
  AND (
    sqlc.narg(after_username)::text IS NULL
    OR (a.username, a.id) > (sqlc.narg(after_username), sqlc.narg(after_account_id)::int4)
  )
ORDER BY a.username ASC, a.id ASC
LIMIT sqlc.arg(row_limit);

-- name: ListOIDCAppRuleGroups :many
SELECT *
FROM user_group
WHERE oidc_client_id = sqlc.arg(oidc_client_id)::text
  AND kind = 'rule'
ORDER BY id ASC;

-- name: ListSAMLAppRuleGroups :many
SELECT *
FROM user_group
WHERE saml_sp_id = sqlc.arg(saml_sp_id)::bigint
  AND kind = 'rule'
ORDER BY id ASC;

-- name: ListOIDCAccessCandidates :many
SELECT
  client_id,
  display_name,
  launch_url,
  redirect_uris,
  require_consent,
  access_restricted
FROM oidc_client
WHERE NOT disabled
  AND NOT forward_auth_enabled
ORDER BY display_name ASC, client_id ASC;

-- name: ListForwardAuthAccessCandidates :many
SELECT
  client_id,
  display_name,
  forward_auth_host,
  forward_auth_scopes,
  access_restricted
FROM oidc_client
WHERE NOT disabled
  AND forward_auth_enabled
  AND forward_auth_host IS NOT NULL
ORDER BY display_name ASC, client_id ASC;

-- name: ListSAMLAccessCandidates :many
SELECT
  id,
  entity_id,
  display_name,
  access_restricted
FROM saml_sp
WHERE NOT disabled
  AND allow_idp_initiated
ORDER BY display_name ASC, id ASC;

-- Management candidates deliberately do not reuse the launchpad candidate
-- queries above: an assigned manager must be able to inspect and change policy
-- for disabled, not-yet-launchable, and non-IdP-initiated applications.
-- name: ListOIDCManagementCandidates :many
SELECT
  client_id,
  display_name,
  launch_url,
  redirect_uris,
  access_restricted
FROM oidc_client
WHERE NOT forward_auth_enabled
ORDER BY display_name ASC, client_id ASC;

-- name: ListForwardAuthManagementCandidates :many
SELECT
  client_id,
  display_name,
  forward_auth_host,
  forward_auth_scopes,
  access_restricted
FROM oidc_client
WHERE forward_auth_enabled
ORDER BY display_name ASC, client_id ASC;

-- name: ListSAMLManagementCandidates :many
SELECT
  id,
  entity_id,
  display_name,
  access_restricted
FROM saml_sp
ORDER BY display_name ASC, id ASC;

-- name: SetOIDCClientAccessRestricted :one
UPDATE oidc_client
SET access_restricted = sqlc.arg(access_restricted)
WHERE client_id = sqlc.arg(client_id)
RETURNING *;

-- name: SetSAMLSPAccessRestricted :one
UPDATE saml_sp
SET access_restricted = sqlc.arg(access_restricted)
WHERE id = sqlc.arg(saml_sp_id)
RETURNING *;
