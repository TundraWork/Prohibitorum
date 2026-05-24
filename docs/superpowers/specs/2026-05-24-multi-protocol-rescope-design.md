# Multi-protocol rescope — design

**Date:** 2026-05-24
**Status:** approved (in brainstorm); skeleton commit pending
**Driver:** rescoping Prohibitorum from "WebAuthn-only IdP with OIDC OP downstream"
to support four upstream auth methods (Password, TOTP, OIDC federation,
WebAuthn) and two downstream protocols (OIDC OP, SAML IdP).

This spec is the durable record of the architecture, data model, and
roadmap changes. It supersedes the v0.1 sections of `DESIGN.md`,
`STATUS.md`, `AUDIT.md`, and `INTEGRATION.md`; those files will be
rewritten in the same skeleton commit to align with this spec.

## Scope summary

| Layer | Before rescope (v0.1 skeleton @ `3d79583`) | After rescope (this commit + onwards) |
|---|---|---|
| Upstream auth methods | WebAuthn | WebAuthn (v0.1), Password+TOTP (v0.2), OIDC federation (v0.3) |
| Downstream protocols | OIDC OP, cookie+introspect | OIDC OP (v0.4), SAML IdP (v0.5) |
| Account directory | Local, picotera-flavored permissions | Local, free-form `attributes jsonb`; JIT-seeded from upstream IdPs |
| Tenancy | Single | Single (unchanged) |

Out of scope (unchanged): multi-tenancy, social-login UX, email/SMS
channels, self-service account recovery (admin-issued enrollment
remains the only recovery path), SAML SP (consuming upstream SAML).

## Architecture — Approach A (three-layer)

Industry-convergent layout drawn from Keycloak, Ory Kratos+Hydra,
Authelia, Dex, Zitadel. Three layers, acyclic import graph:

1. **Identity store** — directory + credentials + linkages. Facts about users.
2. **Authentication subsystem** — factors + federation. Produces a session.
3. **Protocol subsystem** — OIDC OP + SAML IdP. Consumes a session.

The `session` package is the contract between (2) and (3). Protocols
don't know how the user authenticated; factors don't know what RPs
will consume the result.

### Target package layout

```
pkg/
  account/                # directory: Account, list, disable, role, attributes
  credential/
    webauthn/             # lifted from pkg/auth/webauthn.go
    password/             # NEW: argon2id hash store + verify
    totp/                 # NEW: RFC 6238 + recovery codes; AES-GCM at-rest
    pairing/              # device-pairing code (was pkg/auth/pairing.go)
    enrollment/           # invite/reset/add-device tokens
  federation/
    oidc/                 # NEW: upstream OIDC RP; per-IdP provisioning mode
  session/                # KV-backed session store
  authn/                  # login orchestrator + sudo + rate limit + middleware
  protocol/
    oidc/                 # downstream OP (was pkg/oidc)
    saml/                 # NEW: SAML 2.0 IdP, GHES-compatible profile
  server/                 # HTTP wiring, routes mounted from each subsystem
  contract/               # types exposed to dashboard / RPs
  kv/  logx/  errorx/  configx/   # unchanged
```

### File-move map (pkg/auth → new homes)

| From | To |
|---|---|
| `pkg/auth/account.go` (+ test) | `pkg/account/account.go` (+ test) |
| `pkg/auth/webauthn.go`, `webauthn_errors.go` | `pkg/credential/webauthn/` |
| `pkg/auth/enrollment.go` (+ test) | `pkg/credential/enrollment/` |
| `pkg/auth/pairing.go` | `pkg/credential/pairing/` |
| `pkg/auth/session.go` (+ test) | `pkg/session/session.go` |
| `pkg/auth/middleware.go` (+ test) | split: session loading → `pkg/session/middleware.go`; auth-required, role/attribute checks → `pkg/authn/middleware.go` |
| `pkg/auth/ratelimit.go` (+ test) | `pkg/authn/ratelimit.go` |
| `pkg/auth/sudo.go` | `pkg/authn/sudo.go` |
| `pkg/auth/errors.go` | duplicated as small per-package files (sentinel errors live with their package) |
| `pkg/oidc/oidc.go` | `pkg/protocol/oidc/oidc.go` |

Identifier rewrites tracked by `go build`. No backwards-compatibility
shim package — pkg/auth disappears entirely.

## Picotera decoupling

The v0.1 commit (`3d79583`) lifted code from picotera and renamed the
identifier prefix but left picotera **vocabulary** in the schema and
contracts:

- `account.can_view_own_usage`, `can_manage_own_api_keys`,
  `can_view_models`, `can_view_own_traces`, `can_manage_own_projects`
- `enrollment.template_can_*` mirrors of the same five booleans + the
  `enrollment_template_intent_check` CHECK constraint binding them
- `contract.Permission` enum + `contract.Permissions` struct
- `auth.HasPermission` switch + `auth.PermissionsView` projection
- `auth.EnrollmentTemplate.Perms` field + binding params
- `errorx.PicoTeraError` envelope type
- `RPDisplayName: "PicoTera"` constant in `pkg/auth/webauthn.go`
- "lifted from picotera" header in `db/migrations/001_initial.sql`
- "mirrors picotera's but…" comment in `pkg/server/server.go`
- Hardcoded admin-bootstrap perm sets in `handle_account.go`

These are picotera domain concepts (API-keys / projects / LLM traces /
LLM models / usage billing) embedded in a service that's now meant to
be domain-agnostic. None of them belong in a standalone IdP. They are
**stripped in this commit**, not deferred:

- The five picotera columns on `account` are replaced by
  `attributes jsonb NOT NULL DEFAULT '{}'::jsonb`. The five
  template columns on `enrollment` are replaced by
  `template_attributes jsonb`. The CHECK constraint becomes
  `template_attributes IS NULL` for non-invite intents.
- `contract.Permission` enum and `contract.Permissions` struct are
  removed entirely. `AccountView` gets `Attributes map[string]any`.
- `auth.HasPermission` is removed; permission decisions are RP-side
  (per `DESIGN.md` §authorization model). Server-side gating uses
  `role = 'admin'` for admin-only endpoints; any finer gate is
  per-route attribute inspection.
- `auth.EnrollmentTemplate.Perms` becomes `Attributes map[string]any`.
- `errorx.PicoTeraError` → `errorx.Error`. All call sites and string
  literals in `errors.go` follow.
- `RPDisplayName` becomes `configx.Config.WebAuthn.RPDisplayName`
  (already implicit; just lift the constant out). Default
  `"Prohibitorum"`.
- Doc comments referencing picotera are updated; migration 001 header
  describes the schema on its own terms.
- Admin-bootstrap and invite-default code paths set
  `Attributes: nil` (no special claims; the bootstrap admin has
  `role = 'admin'` and that's enough).

## Data model

v0.1's `001_initial.sql` and `002_oidc.sql` are **rewritten in place**.
The project hasn't been deployed yet (only the skeleton commit
exists, no Postgres has ever applied these migrations against real
data), so squashing the decoupling — and the audit-driven schema
expansion below — into the initial schema is cleaner than chaining
cleanup migrations. The v0.1 commit `3d79583` serves as the
pre-rescope snapshot in git history.

The data-model details below are derived from three audit reports
against the authoritative specs:
- `2026-05-24-audit-oidc.md` — RFC 6749 / OIDC Core / RFC 9068 / RFC 9700 / RFC 9207 / RFC 8414 / RFC 7636 / RFC 7009 / RFC 7662 / RP-Initiated Logout 1.0
- `2026-05-24-audit-credentials.md` — WebAuthn L3 / RFC 6238 / RFC 4226 / NIST SP 800-63B-4 / RFC 9106
- `2026-05-24-audit-saml.md` — SAML 2.0 Core/Bindings/Metadata/Profiles + GHES SAML SSO docs

Findings of "Critical" severity are reflected in the schemas below;
"Recommended" findings are also folded in unless flagged as deferred.

### `db/migrations/001_initial.sql` (rewritten)

```sql
CREATE TABLE account (
  id                   bigserial PRIMARY KEY,
  username             text NOT NULL UNIQUE,
  display_name         text NOT NULL,
  webauthn_user_handle bytea NOT NULL UNIQUE,
  role                 text NOT NULL DEFAULT 'user' CHECK (role IN ('user','admin')),
  attributes           jsonb NOT NULL DEFAULT '{}'::jsonb,
  disabled             boolean NOT NULL DEFAULT false,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now()
);

-- PG-persisted authentication facts; doubles as the OIDC `sid` claim. KV-stored
-- session state (sliding refresh, last activity) is keyed on session.id and
-- holds ephemeral fields; this table holds the immutable "moment of auth."
CREATE TABLE session (
  id              text PRIMARY KEY,
  account_id      bigint NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  auth_time       timestamptz NOT NULL,
  amr             text[] NOT NULL DEFAULT '{}',  -- 'pwd','otp','mfa','hwk','user',etc.
  acr             text,
  -- upstream_idp_id added in migration 004 (forward FK)
  created_at      timestamptz NOT NULL DEFAULT now(),
  revoked_at      timestamptz
);
CREATE INDEX session_account_id_idx ON session (account_id);

CREATE TABLE webauthn_credential (
  id              bigserial PRIMARY KEY,
  account_id      bigint NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  credential_id   bytea NOT NULL UNIQUE,
  public_key      bytea NOT NULL,
  cose_alg        int NOT NULL,                   -- COSEAlgorithmIdentifier (e.g. -7 ES256, -257 RS256)
  user_handle     bytea NOT NULL,                 -- value sent in PublicKeyCredentialUserEntity.id at registration
  sign_count      bigint NOT NULL DEFAULT 0,
  transports      text[],
  aaguid          bytea,
  attestation_type text,
  backup_eligible boolean,
  backup_state    boolean,
  uv_initialized  boolean NOT NULL DEFAULT false, -- WebAuthn L3 §4: true once UV observed
  nickname        text,
  last_used_at    timestamptz,
  clone_warning_at timestamptz,                   -- stamped on first sign-count regression
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX webauthn_credential_user_handle_idx ON webauthn_credential (user_handle);

CREATE TABLE enrollment (
  token                      text PRIMARY KEY,
  intent                     text NOT NULL CHECK (intent IN ('bootstrap','invite','reset','add_device')),
  target_account_id          bigint REFERENCES account(id) ON DELETE CASCADE,
  template_username          text,
  template_display_name      text,
  template_role              text CHECK (template_role IN ('user','admin')),
  template_attributes        jsonb,
  expected_upstream_idp_slug text,                -- optional; pre-binds an invite to a specific upstream IdP
  created_at                 timestamptz NOT NULL DEFAULT now(),
  expires_at                 timestamptz NOT NULL,
  consumed_at                timestamptz,
  CONSTRAINT enrollment_intent_target_check CHECK (
    (intent = 'invite' AND target_account_id IS NULL)
    OR (intent <> 'invite')
  ),
  CONSTRAINT enrollment_template_intent_check CHECK (
    intent = 'invite' OR (
      template_username IS NULL AND template_display_name IS NULL
      AND template_role IS NULL AND template_attributes IS NULL
    )
  )
);

-- Audit log for every credential lifecycle event. Queryable; satisfies the
-- "standalone IdP must answer who did what when" requirement (NIST §4.1-4.2).
CREATE TABLE credential_event (
  id             bigserial PRIMARY KEY,
  account_id     bigint REFERENCES account(id) ON DELETE SET NULL,
  factor         text NOT NULL,                   -- 'webauthn','password','totp','recovery_code','federation_oidc','enrollment','session','oidc_client','saml_sp'
  event          text NOT NULL,                   -- 'register','use','fail','revoke','clone_warning','link','unlink','enrollment_issued','enrollment_consumed','session_start','session_end','factor_disabled','admin_action'
  credential_ref bigint,                          -- factor-specific row id; null when factor has no per-row id (e.g. password)
  ip             inet,
  user_agent     text,
  detail         jsonb,                           -- free-form structured context (claim values, redirect URIs, etc.)
  at             timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX credential_event_account_at_idx ON credential_event (account_id, at DESC);
CREATE INDEX credential_event_at_idx ON credential_event (at DESC);

-- Persistent failed-attempt counters across restarts (RFC 4226 §7.3).
CREATE TABLE auth_throttle (
  account_id      bigint NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  factor          text NOT NULL,                  -- 'password','totp','recovery_code','federation_oidc','webauthn'
  failed_attempts int NOT NULL DEFAULT 0,
  window_start    timestamptz NOT NULL DEFAULT now(),
  locked_until    timestamptz,
  PRIMARY KEY (account_id, factor)
);
```

### `db/migrations/002_oidc.sql` (rewritten)

Ships protocol-agnostic `signing_key` (not `oidc_signing_key`) from the start.
Same row services OIDC (via JWK) and SAML (via cert). One rotation domain by `kid`.

```sql
CREATE TABLE signing_key (
  kid           text PRIMARY KEY,
  algorithm     text NOT NULL DEFAULT 'RS256',
  use           text NOT NULL DEFAULT 'sig' CHECK (use IN ('sig','enc')),
  public_jwk    jsonb NOT NULL,
  x509_cert_pem text,                              -- populated when used for SAML
  private_pem   text NOT NULL,
  active        boolean NOT NULL DEFAULT false,
  not_before    timestamptz NOT NULL DEFAULT now(),
  created_at    timestamptz NOT NULL DEFAULT now(),
  retired_at    timestamptz
);
-- One active key per use (sig vs enc) at any time.
CREATE UNIQUE INDEX signing_key_one_active ON signing_key (use) WHERE active;

CREATE TABLE oidc_client (
  client_id                       text PRIMARY KEY,
  display_name                    text NOT NULL,
  client_secret_hash              text,            -- argon2id PHC; NULL for public clients
  redirect_uris                   text[] NOT NULL,
  post_logout_redirect_uris       text[] NOT NULL DEFAULT '{}',
  allowed_scopes                  text[] NOT NULL DEFAULT ARRAY['openid','profile'],
  require_pkce                    boolean NOT NULL DEFAULT true,
  allowed_code_challenge_methods  text[] NOT NULL DEFAULT ARRAY['S256'],  -- reject 'plain'
  token_endpoint_auth_method      text NOT NULL DEFAULT 'client_secret_basic',
  id_token_signed_response_alg    text NOT NULL DEFAULT 'RS256',
  subject_type                    text NOT NULL DEFAULT 'public' CHECK (subject_type IN ('public','pairwise')),
  application_type                text NOT NULL DEFAULT 'web' CHECK (application_type IN ('web','native')),
  default_max_age                 int,
  require_auth_time               boolean NOT NULL DEFAULT false,
  contacts                        text[],
  logo_uri                        text,
  tos_uri                         text,
  policy_uri                      text,
  disabled                        boolean NOT NULL DEFAULT false,
  created_at                      timestamptz NOT NULL DEFAULT now()
);

-- Denylist for self-contained access tokens (RFC 9068 + RFC 7009 §3).
-- Pruning sweep removes rows past expires_at — at that point the JTI would be
-- rejected by signature/exp validation anyway.
CREATE TABLE revoked_jti (
  jti        text PRIMARY KEY,
  expires_at timestamptz NOT NULL,
  reason     text,
  revoked_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX revoked_jti_expires_at_idx ON revoked_jti (expires_at);
```

The protocol-agnostic `signing-key generate` subcommand (v0.4, reused by v0.5
SAML) creates the RSA key, derives the JWK, and self-signs an x509 cert in one
shot. All three columns populated on insert.

### `db/migrations/003_password_totp.sql`

```sql
CREATE TABLE password_credential (
  account_id          bigint PRIMARY KEY REFERENCES account(id) ON DELETE CASCADE,
  hash                text NOT NULL,                       -- PHC string: $argon2id$v=19$m=...$salt$tag
  password_changed_at timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE totp_credential (
  account_id    bigint PRIMARY KEY REFERENCES account(id) ON DELETE CASCADE,
  secret_enc    bytea NOT NULL,                            -- AES-256-GCM ciphertext (AAD: account_id||':'||key_version)
  secret_nonce  bytea NOT NULL,                            -- 12 bytes, unique per row
  key_version   int NOT NULL DEFAULT 1,                    -- DEK version; supports key rotation
  period        int NOT NULL DEFAULT 30,
  digits        int NOT NULL DEFAULT 6,
  algorithm     text NOT NULL DEFAULT 'SHA1',              -- RFC 6238 §1.2; SHA1 for Google Authenticator interop
  last_step     bigint NOT NULL DEFAULT 0,                 -- RFC 6238 §5.2: reject any T <= last_step
  confirmed_at  timestamptz,                               -- NULL until first successful verify
  created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE recovery_code (
  id              bigserial PRIMARY KEY,
  account_id      bigint NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  hash            text NOT NULL,                           -- argon2id PHC string
  used_at         timestamptz,
  used_session_id text REFERENCES session(id) ON DELETE SET NULL,
  used_ip         inet,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX recovery_code_account_id_idx ON recovery_code (account_id);
```

### `db/migrations/004_federation.sql`

```sql
CREATE TABLE upstream_idp (
  id                bigserial PRIMARY KEY,
  slug              text NOT NULL UNIQUE,
  display_name      text NOT NULL,
  issuer_url        text NOT NULL,
  client_id         text NOT NULL,
  client_secret_enc bytea NOT NULL,                        -- AES-256-GCM (AAD: 'upstream_idp:'||id||':'||key_version)
  secret_nonce      bytea NOT NULL,
  key_version       int NOT NULL DEFAULT 1,
  scopes            text[] NOT NULL DEFAULT ARRAY['openid','profile','email'],
  mode              text NOT NULL CHECK (mode IN ('auto_provision','invite_only','link_only')),
  allowed_domains   text[] NOT NULL DEFAULT ARRAY[]::text[],
  username_claim    text NOT NULL DEFAULT 'preferred_username',
  display_name_claim text NOT NULL DEFAULT 'name',
  email_claim       text NOT NULL DEFAULT 'email',
  disabled          boolean NOT NULL DEFAULT false,
  created_at        timestamptz NOT NULL DEFAULT now()
);

-- OIDC Core §2: sub uniqueness is per-issuer, so the unique key is (iss, sub),
-- not (idp_id, sub). The FK to upstream_idp stays for cascade ergonomics.
CREATE TABLE account_identity (
  id              bigserial PRIMARY KEY,
  account_id      bigint NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  upstream_idp_id bigint NOT NULL REFERENCES upstream_idp(id) ON DELETE CASCADE,
  upstream_iss    text NOT NULL,                           -- snapshotted from discovery doc at link time
  upstream_sub    text NOT NULL,
  upstream_email  text,
  linked_at       timestamptz NOT NULL DEFAULT now(),
  UNIQUE (upstream_iss, upstream_sub)
);
CREATE INDEX account_identity_account_id_idx ON account_identity (account_id);
CREATE INDEX account_identity_idp_id_idx ON account_identity (upstream_idp_id);

-- Add forward FK to session (introduced in 001).
ALTER TABLE session
  ADD COLUMN upstream_idp_id bigint REFERENCES upstream_idp(id) ON DELETE SET NULL;
```

`mode` semantics on callback for unknown `(iss, sub)`:
- `auto_provision` — create account if `email_claim` value is in `allowed_domains` (or `allowed_domains` is empty); link; sign in.
- `invite_only` — look up a pending enrollment whose `expected_upstream_idp_slug` matches this IdP; consume it; link; sign in. No matching enrollment → 403.
- `link_only` — never auto-create. User must already have an account and a pre-existing link; otherwise → 403 with a hint to sign in via another method then link from `/me`.

### `db/migrations/005_saml.sql`

```sql
CREATE TABLE saml_sp (
  id                            bigserial PRIMARY KEY,
  entity_id                     text NOT NULL UNIQUE,
  display_name                  text NOT NULL,
  sp_kind                       text,                     -- 'ghes','salesforce',… admin hint only
  name_id_format                text NOT NULL DEFAULT 'urn:oasis:names:tc:SAML:1.1:nameid-format:persistent',
  name_id_claim                 text NOT NULL DEFAULT 'sub',
  attribute_map                 jsonb NOT NULL DEFAULT '[]'::jsonb,
  -- attribute_map shape (ordered array, not map):
  -- [{"local":"...","name":"...","friendly_name":"...","name_format":"basic|uri|unspecified","multi":false}, ...]
  want_assertions_signed        boolean NOT NULL DEFAULT true,
  authn_requests_signed         boolean NOT NULL DEFAULT false,  -- mirrors SP metadata
  require_signed_authn_request  boolean NOT NULL DEFAULT false,  -- our policy; auto-true when sp_kind='ghes'
  session_lifetime              interval,                  -- per-SP SessionNotOnOrAfter override; NULL = IdP default
  metadata_xml                  text,                      -- raw SP metadata for re-parse / audit
  metadata_valid_until          timestamptz,
  metadata_cache_duration       interval,
  metadata_fetched_at           timestamptz,
  created_at                    timestamptz NOT NULL DEFAULT now()
);

-- Multiple ACS endpoints per SP (SAML Metadata §2.4.4). Lookup precedence at
-- Response delivery time: explicit AssertionConsumerServiceURL in AuthnRequest
-- (must match a row exactly) → AssertionConsumerServiceIndex → is_default=true.
CREATE TABLE saml_sp_acs (
  sp_id      bigint NOT NULL REFERENCES saml_sp(id) ON DELETE CASCADE,
  idx        int NOT NULL,
  binding    text NOT NULL,                                 -- 'urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST'
  location   text NOT NULL,
  is_default boolean NOT NULL DEFAULT false,
  PRIMARY KEY (sp_id, idx)
);

-- Multiple SP certificates per use (SAML Metadata §2.4.1.1). Supports SP cert
-- rotation and future encrypted-assertion SPs (use='encryption').
CREATE TABLE saml_sp_key (
  id        bigserial PRIMARY KEY,
  sp_id     bigint NOT NULL REFERENCES saml_sp(id) ON DELETE CASCADE,
  use       text NOT NULL CHECK (use IN ('signing','encryption')),
  cert_pem  text NOT NULL,
  not_after timestamptz,
  added_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX saml_sp_key_sp_use_idx ON saml_sp_key (sp_id, use);

-- Stable pairwise NameID per (account, SP) (SAML Core §8.3.7). Generated on
-- first SSO and reused forever — prevents account re-linking when a user
-- renames or changes email.
CREATE TABLE saml_subject_id (
  account_id     bigint NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  sp_id          bigint NOT NULL REFERENCES saml_sp(id) ON DELETE CASCADE,
  name_id        text NOT NULL,
  name_id_format text NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (account_id, sp_id)
);

-- Forward-compat: populated on every SAML SSO from day one even though SLO
-- (which consumes this) doesn't ship until v0.5. Avoids a migration when SLO
-- lands; crewjam/saml's Session.Index maps directly into session_index.
CREATE TABLE saml_session (
  id              bigserial PRIMARY KEY,
  session_id      text NOT NULL REFERENCES session(id) ON DELETE CASCADE,
  sp_id           bigint NOT NULL REFERENCES saml_sp(id) ON DELETE CASCADE,
  name_id         text NOT NULL,
  session_index   text NOT NULL,
  not_on_or_after timestamptz NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX saml_session_session_id_idx ON saml_session (session_id);
```

## Cryptographic and behavioral policies

These are decisions that don't live in the schema but constrain how the schema
is used. They're enumerated here so the implementation can't drift.

### AES-GCM at-rest encryption (`totp_credential.secret_enc`, `upstream_idp.client_secret_enc`)

- **Key:** `PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>` (32 bytes, base64-encoded in env or file).
  Multiple versions can be loaded simultaneously; the row's `key_version`
  column selects which key decrypts it. Rotation: deploy `_V2`, re-encrypt rows
  on next touch, retire `_V1` once `MAX(key_version)` reaches 2.
- **Nonce:** 12 bytes from `crypto/rand`, stored in `secret_nonce`. Unique per
  row. Never reused.
- **AAD:**
  - For `totp_credential`: `'totp:' || account_id || ':' || key_version` (UTF-8).
  - For `upstream_idp`: `'upstream_idp:' || id || ':' || key_version`.
  Binds ciphertext to its row identity; an attacker with write access cannot
  copy a ciphertext between accounts and have it decrypt.

### Hash storage (PHC string format)

- `password_credential.hash`, `recovery_code.hash`, and `oidc_client.client_secret_hash`
  are all argon2id PHC strings:
  `$argon2id$v=19$m=65536,t=3,p=4$<base64 salt>$<base64 tag>`.
- Parameters default to `configx.PasswordHashParams{Memory: 64 MiB, Iterations: 3, Parallelism: 4}` and
  may be tuned per deployment.
- On successful login verify, if any parameter in the stored hash is below the
  current `PasswordHashParams`, re-hash and update the row.
- Salts are per-row, 16 bytes from `crypto/rand`, embedded in the PHC string.

### Authorization-code lifecycle (RFC 9700 §4.5 + §4.14.2)

- `oidc:code:<random>` KV rows are **not deleted on first use**. They're marked
  `consumed_at` and kept until TTL expiry. A second exchange attempt against a
  consumed code:
  1. Returns `invalid_grant` to the client.
  2. Triggers immediate revocation of the entire refresh-token family minted
     from that code (writes the family-revocation marker; future refreshes
     against that family return `invalid_grant`).
  3. Writes a `credential_event` row with `event='fail', factor='oidc_client', detail={"reason":"code_reuse"}`.

### Federation state KV (`oidc:fed:state:<random>`)

Stored at request time, consumed once at callback. Shape:
```json
{
  "idp_id": <bigint>,
  "expected_iss": "<snapshot of upstream_idp.issuer_url at request time>",
  "expected_token_endpoint": "<snapshot of discovery doc's token_endpoint>",
  "nonce": "<random>",
  "code_verifier": "<RFC 7636 PKCE>",
  "return_to": "<post-login URL>"
}
```
Snapshotting `expected_iss` and `expected_token_endpoint` defeats mid-flight
admin edits to `upstream_idp` from breaking mix-up resistance (RFC 9700 §4.4.2.1).

### Access-token issuance (RFC 9068)

- `typ` header = `at+jwt`. Resource servers reject any other `typ` per RFC 9068 §4.
- Required claims: `iss`, `sub`, `aud`, `exp`, `iat`, `jti`, `client_id`, `scope`.
- `auth_time`, `acr`, `amr` carried when available from the `session` row.
- `azp` emitted whenever `aud` differs from the authorized party or has > 1
  value (OIDC Core §2).
- `at_hash` included in ID tokens (defense in depth; OIDC Core §3.1.3.6).

### PKCE

- All clients use PKCE (`require_pkce=true` is forced).
- `allowed_code_challenge_methods` rejects `plain` by default; admin can in
  principle relax it but the SQL CHECK in v0.6+ may forbid `plain` entirely.

### SAML assertion construction

- Always sign both `<Response>` and `<Assertion>`. GHES only validates
  `Destination` on signed `<Response>`, which gates the open-redirect check.
- `Destination` attribute on `<Response>` = the chosen ACS URL.
- `<SubjectConfirmationData Recipient="...">` = the same ACS URL.
- `<Audience>` inside `<AudienceRestriction>` = `saml_sp.entity_id` verbatim.
- `<AuthnContextClassRef>` = `urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport` for password+TOTP, `…unspecified` for WebAuthn (no standard ref exists for passkeys yet), with `Comparison="exact"`.
- NameID stability: look up `saml_subject_id(account_id, sp_id)`; if absent,
  generate a 32-byte random opaque value (base64url), persist, reuse forever.
- IdP metadata at `/saml/metadata` publishes every `signing_key` row where
  `retired_at IS NULL OR retired_at > now() - 7 days` (overlap window for
  in-flight assertion verification).

## HTTP surface (new endpoints)

```
# Password+TOTP fallback flow
POST /api/prohibitorum/auth/password/begin
     body: {username, password}
     200 → {partial_session_token}   (5-minute KV TTL)
     401 → bad password / unknown user / no password set / account disabled
POST /api/prohibitorum/auth/totp/verify
     body: {partial_session_token, code}
     204 + session cookie
     401 → bad code (rate-limited per-account)
POST /api/prohibitorum/auth/recovery-code/verify
     body: {partial_session_token, code}
     204 + session cookie; marks code used

# Upstream OIDC federation (per-IdP)
GET  /api/prohibitorum/auth/federation/{idp_slug}/login?return_to=...
     302 → upstream authorize URL; state + nonce stored in KV
GET  /api/prohibitorum/auth/federation/{idp_slug}/callback
     handles upstream code exchange; applies IdP mode policy
     302 → /me or return_to once session established
     403 → invite_only or link_only policy rejection

# SAML IdP
GET  /saml/metadata                       # IdP metadata XML
GET  /saml/sso                            # HTTP-Redirect binding (AuthnRequest)
POST /saml/sso                            # HTTP-POST binding (AuthnRequest)
POST /saml/slo                            # Single Logout (v0.5+; stubbed initially)
```

Existing v0.1 endpoints (login / enroll / me / accounts / pairing /
sudo / OIDC OP) keep their paths under `/api/prohibitorum/`. SAML
lives at top-level `/saml/*` for parity with `/.well-known/...`.

## Authentication orchestrator

`pkg/authn/flow.go` resolves "which methods are available for this
account?":

1. Account has any `webauthn_credential` rows → WebAuthn ceremony.
2. Otherwise, `password_credential` + confirmed `totp_credential` → password+TOTP.
3. Otherwise, at least one `account_identity` row → suggest the matching upstream IdP.
4. None of the above → "no usable method, contact admin." Admin issues an enrollment token; recovery flows are unchanged from v0.1.

**Factor policy enforcement** (WebAuthn-preferred, others fallback):
when a user successfully enrolls WebAuthn via `/me/passkeys/add`, the
handler shows a confirmation: "Disable password+TOTP backup?" Default
**yes**. If yes, transactionally delete `password_credential`,
`totp_credential`, and all `recovery_code` rows for the account.
Opt-in checkbox to keep both. The `disable_backup` decision is
captured server-side; no client-side bypass possible.

Lockout safety: admin can issue a recovery enrollment token to any
disabled or stuck account (existing v0.1 mechanism, unchanged).

## Skeleton commit scope

This commit produces:

1. **Picotera strip-out** per the §"Picotera decoupling" section above —
   schema columns, contract types, auth helpers, error envelope name,
   RPDisplayName constant, doc comments.
2. **File moves** per the map above. `go build ./...` succeeds at HEAD.
3. **Empty stub files** for new functionality, with signatures and
   `// TODO(v0.X)` markers but no behavior:
   - `pkg/credential/password/password.go`
   - `pkg/credential/totp/totp.go`
   - `pkg/federation/oidc/federation.go`
   - `pkg/protocol/saml/saml.go`
   - `pkg/authn/flow.go`
   - `pkg/audit/event.go` (writer interface for `credential_event`)
4. **Migrations 001 and 002 rewritten in place** (picotera vocabulary
   removed; signing_key unified; OIDC client expanded per audit;
   session / revoked_jti / auth_throttle / credential_event added).
   New migrations 003 / 004 / 005 (password+TOTP per audit; federation
   with upstream_iss + key_version; SAML with ACS/key/subject_id/session
   child tables). `mise run db:up` applies cleanly against a fresh
   Postgres.
5. **Queries** in `db/queries/account.sql` and `db/queries/enrollment.sql`
   updated to read/write `attributes` and `template_attributes`; the
   five `can_*` column references removed. New query files for
   `session`, `credential_event`, `auth_throttle`, `revoked_jti`.
6. **`pkg/contract/auth.go`** drops `Permission` + `Permissions`;
   `AccountView` and `EnrollmentTemplate` gain `Attributes
   map[string]any`. Server handlers updated to read/write the map.
7. **Doc rewrites** — DESIGN.md, STATUS.md, AUDIT.md, INTEGRATION.md,
   README.md aligned to this spec; all picotera references removed
   from the user-facing docs (the spec retains them as the explicit
   audit trail of what was removed). AUDIT.md cross-references the
   three protocol-audit reports.
8. **`configx`** gains:
   - `DataEncryptionKeys map[int][]byte` (versioned DEK set for AES-GCM)
   - `PasswordHashParams` (memory/iterations/parallelism for argon2id)
   - `TOTP` substruct (period/digits/algorithm defaults; drift tolerance)
   - `SAML` substruct (entity ID, base URL, default NameID format,
     session lifetime default, rotation grace for metadata publication)
   - `WebAuthn.RPDisplayName` (default `"Prohibitorum"`)
   - `OIDC` substruct (issuer URL, code TTL, access-token TTL,
     refresh-token TTL, jwks cache hint)
   - `Federation` substruct (state TTL, default scopes, default claim
     map per provider kind)

Three explicit non-goals for the skeleton commit:

- No real password / TOTP / federation / SAML / OIDC-OP business logic.
- No frontend changes (`dashboard/` is empty; that's a v0.6 task).
- No smoke test against a live Postgres — that's the immediate
  next-session task once this commit lands.

## Roadmap

| Version | Theme | Headline deliverables |
|---|---|---|
| **v0.1** (this commit) | Rescope + picotera decoupling | Picotera strip-out, file moves, migrations 001–005, doc rewrites, stubs. `go build ./...` clean. |
| **v0.1.1** (next session) | Smoke test | `go mod tidy`, run migrations against real Postgres, exercise WebAuthn ceremony, confirm `/.well-known/openid-configuration` discovery |
| **v0.2** | Password + TOTP | Credential CRUD, enrollment flow, password+TOTP login endpoints, recovery-code mint/verify |
| **v0.3** | Upstream OIDC federation | upstream_idp CRUD, per-IdP RP flow via zitadel/oidc/v3, three provisioning modes, link UX in `/me` |
| **v0.4** | OIDC OP (downstream) | `signing-key generate` subcommand, `/oauth/authorize`, `/oauth/token`, `/oauth/userinfo`, `/oauth/introspect`, RP-initiated logout |
| **v0.5** | SAML IdP | crewjam/saml integration, metadata, SP-initiated SSO (HTTP-Redirect + HTTP-POST), signed assertions, attribute mapping, optional SLO |
| **v0.6** | Frontend | Vue 3 dashboard; passkey ceremony SDK; method-selection login UX |
| **v0.7+** | Hardening | KMS-backed signing keys, audit-log export, signing-key rotation UX, admin UI for clients/SPs/IdPs |

## Threat model deltas

New surfaces added on top of v0.1:

- **Password brute-force.** Per-account exponential backoff persisted
  in `auth_throttle(account_id, factor='password')` so restarts don't
  reset the counter (RFC 4226 §7.3). Argon2id params tuned for
  ≥250ms/verify on prod hardware. No password reset via email channel
  (admin enrollment token only).
- **TOTP code guessing.** 6 digits = 10^6 space; rate-limit to ≤5
  attempts per 5 minutes per account in `auth_throttle`. ±1 period
  drift tolerance. **Same-step replay** prevented by
  `totp_credential.last_step` (RFC 6238 §5.2): on success, store the
  matched step; reject any subsequent verify with `T <= last_step`.
- **Recovery code theft.** Codes shown exactly once at enrollment;
  argon2id-hashed at rest in PHC format; single-use; redemption
  captured in `recovery_code.used_session_id` + `used_ip` for audit.
- **Cross-account credential row-swap.** AES-GCM ciphertext bound to
  its row identity via AAD (`account_id||':'||key_version` for TOTP,
  `'upstream_idp:'||id||':'||key_version` for upstream secrets).
  Copying ciphertext between rows fails decryption.
- **DEK compromise / rotation.** Versioned DEK set in
  `configx.DataEncryptionKeys`; `key_version` per row picks the
  decryptor. Rotation: deploy new version, re-encrypt rows on next
  touch, retire old version once `MAX(key_version)` covers all live
  rows.
- **Federated IdP impersonation.** Strict issuer + audience + nonce
  validation on upstream ID token. Per-IdP `client_secret` AES-GCM
  encrypted. `expected_iss` snapshotted into KV state at request time
  (RFC 9700 §4.4.2.1 mix-up resistance). `account_identity` keyed on
  `(upstream_iss, upstream_sub)` per OIDC Core §2 — admin re-pointing
  an `upstream_idp` to a different `issuer_url` cannot collide subs.
- **JIT account squatting.** `auto_provision` mode gated by
  `allowed_domains` against `email_claim`. `username` collisions: if
  upstream's `username_claim` value matches an existing local account
  with no link to this IdP, reject (admin must intervene).
- **Authorization code replay.** Codes marked `consumed_at` and kept
  until TTL — a replay attempt revokes the entire refresh-token family
  minted from that code and writes a `credential_event` row (RFC 9700
  §4.5 + §4.14.2).
- **Access-token revocation despite stateless JWT.** Every access
  token mints a `jti` (RFC 9068 §2.2 requirement); revocation writes
  to `revoked_jti`. Resource servers introspecting via
  `/oauth/introspect` get `active: false`. Self-validating RSs (the
  norm) check `jti` against the revocation cache.
- **WebAuthn authenticator cloning.** Sign-count regression sets
  `webauthn_credential.clone_warning_at`; admin UI surfaces.
- **SAML assertion replay.** `crewjam/saml` enforces `NotBefore` /
  `NotOnOrAfter` / `InResponseTo` / one-use Assertion ID.
- **SAML open-redirect via spoofed ACS URL.** Validated against
  `saml_sp_acs` rows (exact match on explicit URL, or index lookup, or
  is_default fallback) per Profiles §4.1.4.1. Wildcard / loose match
  not supported.
- **SAML NameID drift.** Stable `saml_subject_id(account_id, sp_id)`
  pairing — renames and email changes don't re-link GHES accounts
  (Core §8.3.7).
- **SAML XML signature wrapping (XSW).** Use crewjam/saml's
  post-canonicalization signature verification; reject assertions with
  multiple `Signature` elements or unexpected structure.
- **Encryption key compromise.** The versioned DEK set
  `PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>` protects TOTP secrets +
  upstream client secrets. Loss of all key versions = loss of all
  federated logins and TOTP enrollments (forces re-enrollment). The
  versioned scheme means a rotation doesn't invalidate existing rows
  — rows decrypt under their original `key_version`, then re-encrypt
  under the new version on next touch. Operator responsibility:
  keep at least two consecutive versions available during rotation.

## Open questions deferred to implementation versions

- TOTP issuer / label format in QR codes (v0.2).
- Whether `account.username` must be unique across linked federated
  identities, or whether `slug:upstream_sub` becomes the secondary
  identifier (v0.3). The audit-resolved `(upstream_iss, upstream_sub)`
  uniqueness on `account_identity` handles federation-side uniqueness;
  the local-username collision policy is separate.
- SAML NameID stability is now resolved by `saml_subject_id` (per-SP
  opaque random) — but the choice between **random opaque** and
  **HMAC-derived pairwise** (`HMAC(server_secret, account_id||entity_id)`)
  is left to v0.5. Random opaque requires DB persistence (which we
  have); HMAC derives without state but locks pairwise to a single
  master secret.
- Whether SAML and OIDC share the same signing key by default or use
  separate `kid` ranges (v0.5). The unified `signing_key` table leaves
  room for either; recommend separate kids (e.g. `oidc-2026-05`,
  `saml-2026-05`) so rotation can be decoupled per protocol.
- Refresh-token family persistence: KV-only (current plan) vs
  `refresh_token_family` table (deferred — adds audit beyond KV TTL
  but isn't required for the rotation+reuse-detection algorithm
  itself). Revisit at v0.4 when refresh tokens are wired.
- Whether `auth_throttle` should be one table per factor or one shared
  table (current design: one shared, keyed `(account_id, factor)`).
  Shared is simpler; per-factor would let throttle windows differ
  cleanly. Revisit if v0.2 ergonomics push back.

## Audit trail

Three protocol-audit reports underpin the schema and behavioral
decisions in this spec. Read them alongside this document:

- `2026-05-24-audit-oidc.md` — OIDC OP + RP federation, 8 critical / 7 recommended findings.
- `2026-05-24-audit-credentials.md` — WebAuthn / Password / TOTP / Recovery, 5 critical / 8 recommended findings.
- `2026-05-24-audit-saml.md` — SAML IdP + GHES interop, 5 critical / 6 recommended findings + 10 GHES-specific call-outs.

`AUDIT.md` (the project-level compliance checklist) cross-references
these in its OIDC, WebAuthn, and SAML sections with explicit ✅ /
⚠️ deferred / ❌ gap labels per spec citation.
