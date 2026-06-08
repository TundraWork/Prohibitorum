-- name: GetSAMLSPByEntityID :one
SELECT * FROM saml_sp WHERE entity_id = $1;

-- name: ListSAMLSPs :many
SELECT * FROM saml_sp ORDER BY display_name;

-- name: InsertSAMLSP :one
INSERT INTO saml_sp (entity_id, display_name, sp_kind, name_id_format, name_id_claim,
  attribute_map, want_assertions_signed, authn_requests_signed,
  require_signed_authn_request, allow_idp_initiated, session_lifetime, metadata_xml,
  metadata_valid_until, metadata_cache_duration, metadata_fetched_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING *;

-- name: ListSAMLSPACSEndpoints :many
SELECT * FROM saml_sp_acs WHERE sp_id = $1 ORDER BY idx;

-- name: InsertSAMLSPACS :exec
INSERT INTO saml_sp_acs (sp_id, idx, binding, location, is_default)
VALUES ($1, $2, $3, $4, $5);

-- name: ListSAMLSPKeys :many
SELECT * FROM saml_sp_key WHERE sp_id = $1 AND use = $2
ORDER BY added_at DESC;

-- name: InsertSAMLSPKey :exec
INSERT INTO saml_sp_key (sp_id, use, cert_pem, not_after)
VALUES ($1, $2, $3, $4);

-- name: GetSAMLSubjectID :one
SELECT * FROM saml_subject_id WHERE account_id = $1 AND sp_id = $2;

-- name: InsertSAMLSubjectID :one
INSERT INTO saml_subject_id (account_id, sp_id, name_id, name_id_format)
VALUES ($1, $2, $3, $4)
ON CONFLICT (account_id, sp_id) DO UPDATE SET name_id = saml_subject_id.name_id
RETURNING *;

-- name: InsertSAMLSession :one
INSERT INTO saml_session (session_id, sp_id, name_id, session_index, not_on_or_after)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (session_id, sp_id, session_index)
  DO UPDATE SET not_on_or_after = EXCLUDED.not_on_or_after
RETURNING *;

-- name: ListSAMLSessionsBySession :many
SELECT * FROM saml_session WHERE session_id = $1;

-- name: ListSAMLSessionsByNameID :many
SELECT * FROM saml_session WHERE sp_id = $1 AND name_id = $2;

-- name: GetSAMLSPByID :one
SELECT * FROM saml_sp WHERE id = $1;

-- name: DeleteSAMLSessionsBySession :exec
DELETE FROM saml_session WHERE session_id = $1;

-- name: DeleteExpiredSAMLSessions :execrows
DELETE FROM saml_session WHERE not_on_or_after < now();

-- name: UpdateSAMLSP :one
UPDATE saml_sp SET
  display_name                 = $2,
  name_id_format               = $3,
  require_signed_authn_request = $4,
  want_assertions_signed       = $5,
  allow_idp_initiated          = $6,
  session_lifetime             = $7,
  name_id_claim                = $8,
  attribute_map                = $9
WHERE id = $1
RETURNING *;

-- name: DeleteSAMLSP :execrows
DELETE FROM saml_sp WHERE id = $1;

-- name: DeleteSAMLSPACSByID :exec
DELETE FROM saml_sp_acs WHERE sp_id = $1;

-- name: DeleteSAMLSPKeysByID :exec
DELETE FROM saml_sp_key WHERE sp_id = $1;
