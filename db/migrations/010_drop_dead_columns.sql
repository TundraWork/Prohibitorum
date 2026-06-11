-- +goose Up
-- Prune columns that were modeled but never read at runtime (T4.2). Each was
-- either write-only (set at create, never consulted) or pure dead weight:
--
--   oidc_client.contacts, application_type, id_token_signed_response_alg,
--     default_max_age, require_auth_time — none are read by the OP; the OP signs
--     id_tokens with RS256 unconditionally and derives max_age/auth_time from the
--     request, not stored client policy.
--   saml_sp.name_id_claim       — NameID is always the opaque per-(account,sp)
--     subjectID in the SP's name_id_format; the claim override was never honored.
--   saml_sp.want_assertions_signed — assertions are always signed.
--   saml_sp.authn_requests_signed  — never read; AuthnRequest-signing enforcement
--     uses require_signed_authn_request (kept), and IdP metadata advertises
--     WantAuthnRequestsSigned unconditionally.
--
-- Deliberately KEPT: oidc_client.subject_type (pairwise subjects, deferred) and
-- saml_sp.metadata_{valid_until,cache_duration,fetched_at} (metadata auto-refresh,
-- deferred) — both are roadmap, not dead.
ALTER TABLE oidc_client
  DROP COLUMN contacts,
  DROP COLUMN application_type,
  DROP COLUMN id_token_signed_response_alg,
  DROP COLUMN default_max_age,
  DROP COLUMN require_auth_time;

ALTER TABLE saml_sp
  DROP COLUMN name_id_claim,
  DROP COLUMN want_assertions_signed,
  DROP COLUMN authn_requests_signed;

-- +goose Down
ALTER TABLE oidc_client
  ADD COLUMN id_token_signed_response_alg text NOT NULL DEFAULT 'RS256',
  ADD COLUMN application_type             text NOT NULL DEFAULT 'web' CHECK (application_type IN ('web','native')),
  ADD COLUMN default_max_age              int,
  ADD COLUMN require_auth_time            boolean NOT NULL DEFAULT false,
  ADD COLUMN contacts                     text[];

ALTER TABLE saml_sp
  ADD COLUMN name_id_claim          text NOT NULL DEFAULT 'sub',
  ADD COLUMN want_assertions_signed boolean NOT NULL DEFAULT true,
  ADD COLUMN authn_requests_signed  boolean NOT NULL DEFAULT false;
