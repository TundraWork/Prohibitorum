-- name: CreateOIDCAppGroup :one
WITH created AS (
  INSERT INTO user_group (kind, slug, display_name, description, exposed_to_downstream, rule)
  VALUES (sqlc.arg(kind), sqlc.arg(slug), sqlc.arg(display_name), sqlc.narg(description),
          sqlc.arg(exposed_to_downstream), sqlc.narg(rule))
  RETURNING *
), linked AS (
  INSERT INTO oidc_client_group (client_id, group_id)
  SELECT sqlc.arg(oidc_client_id)::text, id FROM created
)
SELECT g.* FROM user_group g JOIN created c ON c.id = g.id;

-- name: CreateSAMLAppGroup :one
WITH created AS (
  INSERT INTO user_group (kind, slug, display_name, description, exposed_to_downstream, rule)
  VALUES (sqlc.arg(kind), sqlc.arg(slug), sqlc.arg(display_name), sqlc.narg(description),
          sqlc.arg(exposed_to_downstream), sqlc.narg(rule))
  RETURNING *
), linked AS (
  INSERT INTO saml_sp_group (saml_sp_id, group_id)
  SELECT sqlc.arg(saml_sp_id)::bigint, id FROM created
)
SELECT g.* FROM user_group g JOIN created c ON c.id = g.id;

-- name: CreateGlobalGroup :one
INSERT INTO user_group (kind, slug, display_name, description, exposed_to_downstream, rule)
VALUES (sqlc.arg(kind), sqlc.arg(slug), sqlc.arg(display_name), sqlc.narg(description),
        sqlc.arg(exposed_to_downstream), sqlc.narg(rule))
RETURNING *;

-- name: GetGlobalGroup :one
SELECT * FROM user_group WHERE id = sqlc.arg(group_id);

-- name: ListGlobalGroups :many
SELECT * FROM user_group
ORDER BY display_name ASC, id ASC;

-- name: ListGlobalGroupApplications :many
SELECT *
FROM (
  SELECT
    CASE WHEN c.forward_auth_enabled THEN 'forward_auth' ELSE 'oidc' END::text AS kind,
    c.client_id::text AS app_id,
    c.display_name
  FROM oidc_client_group link
  JOIN oidc_client c ON c.client_id = link.client_id
  WHERE link.group_id = sqlc.arg(group_id)

  UNION ALL

  SELECT 'saml'::text AS kind, sp.id::text AS app_id, sp.display_name
  FROM saml_sp_group link
  JOIN saml_sp sp ON sp.id = link.saml_sp_id
  WHERE link.group_id = sqlc.arg(group_id)
) applications
ORDER BY kind ASC, display_name ASC, app_id ASC;

-- name: GetOIDCAppGroup :one
SELECT g.*
FROM user_group g
WHERE g.id = sqlc.arg(group_id)
  AND EXISTS (SELECT 1 FROM oidc_client_group og
              WHERE og.group_id = g.id AND og.client_id = sqlc.arg(oidc_client_id)::text);

-- name: GetSAMLAppGroup :one
SELECT g.*
FROM user_group g
WHERE g.id = sqlc.arg(group_id)
  AND EXISTS (SELECT 1 FROM saml_sp_group sg
              WHERE sg.group_id = g.id AND sg.saml_sp_id = sqlc.arg(saml_sp_id)::bigint);

-- name: ListOIDCAppGroups :many
SELECT g.*
FROM user_group g
JOIN oidc_client_group og ON og.group_id = g.id
WHERE og.client_id = sqlc.arg(oidc_client_id)::text
ORDER BY g.display_name ASC, g.id ASC;

-- name: ListSAMLAppGroups :many
SELECT g.*
FROM user_group g
JOIN saml_sp_group sg ON sg.group_id = g.id
WHERE sg.saml_sp_id = sqlc.arg(saml_sp_id)::bigint
ORDER BY g.display_name ASC, g.id ASC;

-- name: ReplaceOIDCAppGroups :many
WITH locked AS (
  SELECT client_id FROM oidc_client
  WHERE client_id = sqlc.arg(oidc_client_id)::text
  FOR UPDATE
), deleted AS (
  DELETE FROM oidc_client_group og
  USING locked l
  WHERE og.client_id = l.client_id
    AND NOT (og.group_id = ANY(sqlc.arg(group_ids)::int[]))
  RETURNING og.group_id
), inserted AS (
  INSERT INTO oidc_client_group (client_id, group_id)
  SELECT l.client_id, requested.group_id
  FROM locked l
  CROSS JOIN unnest(sqlc.arg(group_ids)::int[]) AS requested(group_id)
  ON CONFLICT (client_id, group_id) DO NOTHING
  RETURNING group_id
)
SELECT g.* FROM user_group g, locked l
WHERE g.id = ANY(sqlc.arg(group_ids)::int[])
ORDER BY g.display_name ASC, g.id ASC;

-- name: ReplaceSAMLAppGroups :many
WITH locked AS (
  SELECT id FROM saml_sp
  WHERE id = sqlc.arg(saml_sp_id)::bigint
  FOR UPDATE
), deleted AS (
  DELETE FROM saml_sp_group sg
  USING locked l
  WHERE sg.saml_sp_id = l.id
    AND NOT (sg.group_id = ANY(sqlc.arg(group_ids)::int[]))
  RETURNING sg.group_id
), inserted AS (
  INSERT INTO saml_sp_group (saml_sp_id, group_id)
  SELECT l.id, requested.group_id
  FROM locked l
  CROSS JOIN unnest(sqlc.arg(group_ids)::int[]) AS requested(group_id)
  ON CONFLICT (saml_sp_id, group_id) DO NOTHING
  RETURNING group_id
)
SELECT g.* FROM user_group g, locked l
WHERE g.id = ANY(sqlc.arg(group_ids)::int[])
ORDER BY g.display_name ASC, g.id ASC;

-- name: UpdateAppGroup :one
UPDATE user_group
SET slug = sqlc.arg(slug),
    display_name = sqlc.arg(display_name),
    description = sqlc.narg(description),
    exposed_to_downstream = sqlc.arg(exposed_to_downstream),
    rule = sqlc.narg(rule),
    updated_at = now()
WHERE id = sqlc.arg(group_id)
  AND (sqlc.narg(oidc_client_id)::text IS NULL OR EXISTS (
    SELECT 1 FROM oidc_client_group og WHERE og.group_id = user_group.id
      AND og.client_id = sqlc.narg(oidc_client_id)::text
  ))
  AND (sqlc.narg(saml_sp_id)::bigint IS NULL OR EXISTS (
    SELECT 1 FROM saml_sp_group sg WHERE sg.group_id = user_group.id
      AND sg.saml_sp_id = sqlc.narg(saml_sp_id)::bigint
  ))
RETURNING *;

-- name: UpdateGlobalGroup :one
UPDATE user_group
SET slug = sqlc.arg(slug), display_name = sqlc.arg(display_name),
    description = sqlc.narg(description), exposed_to_downstream = sqlc.arg(exposed_to_downstream),
    rule = sqlc.narg(rule), updated_at = now()
WHERE id = sqlc.arg(group_id)
RETURNING *;

-- name: DeleteGlobalGroup :execrows
DELETE FROM user_group WHERE id = sqlc.arg(group_id);

-- name: DeleteOIDCAppGroup :execrows
DELETE FROM oidc_client_group
WHERE group_id = sqlc.arg(group_id) AND client_id = sqlc.arg(oidc_client_id)::text;

-- name: DeleteSAMLAppGroup :execrows
DELETE FROM saml_sp_group
WHERE group_id = sqlc.arg(group_id) AND saml_sp_id = sqlc.arg(saml_sp_id)::bigint;

-- name: ListManualDecisionsForOIDCApp :many
SELECT d.*
FROM group_manual_decision d
JOIN user_group g ON g.id = d.group_id AND g.kind = d.group_kind
JOIN oidc_client_group og ON og.group_id = g.id
WHERE og.client_id = sqlc.arg(oidc_client_id)::text
  AND g.kind = 'manual'
  AND d.account_id = sqlc.arg(account_id);

-- name: ListManualDecisionsForSAMLApp :many
SELECT d.*
FROM group_manual_decision d
JOIN user_group g ON g.id = d.group_id AND g.kind = d.group_kind
JOIN saml_sp_group sg ON sg.group_id = g.id
WHERE sg.saml_sp_id = sqlc.arg(saml_sp_id)::bigint
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
SELECT g.*
FROM user_group g
JOIN oidc_client_group og ON og.group_id = g.id
WHERE og.client_id = sqlc.arg(oidc_client_id)::text
  AND g.kind = 'rule'
ORDER BY g.id ASC;

-- name: ListSAMLAppRuleGroups :many
SELECT g.*
FROM user_group g
JOIN saml_sp_group sg ON sg.group_id = g.id
WHERE sg.saml_sp_id = sqlc.arg(saml_sp_id)::bigint
  AND g.kind = 'rule'
ORDER BY g.id ASC;

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
