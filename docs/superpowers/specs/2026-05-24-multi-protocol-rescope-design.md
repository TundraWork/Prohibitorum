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
data), so squashing the decoupling into the initial schema is cleaner
than chaining cleanup migrations. The v0.1 commit `3d79583` serves as
the pre-rescope snapshot in git history.

### `db/migrations/001_initial.sql` (rewritten)

`account` table replaces the 5 picotera permission booleans with
`attributes jsonb`. `enrollment` table replaces the 5 template booleans
with `template_attributes jsonb`. The CHECK constraint becomes:

```sql
CONSTRAINT enrollment_template_intent_check CHECK (
  (intent = 'invite' AND target_account_id IS NULL)
  OR (intent <> 'invite' AND template_attributes IS NULL)
)
```

`webauthn_credential` table unchanged from v0.1.

### `db/migrations/002_oidc.sql` (rewritten)

Ships `signing_key` (not `oidc_signing_key`) from the start, with
`x509_cert_pem` column included. Same row services both OIDC (via
JWK) and SAML (via cert). One rotation domain by `kid`.

```sql
CREATE TABLE signing_key (
  kid           text PRIMARY KEY,
  algorithm     text NOT NULL DEFAULT 'RS256',
  public_jwk    jsonb NOT NULL,
  x509_cert_pem text,                 -- populated when used for SAML
  private_pem   text NOT NULL,
  active        boolean NOT NULL DEFAULT false,
  created_at    timestamptz NOT NULL DEFAULT now(),
  retired_at    timestamptz
);
CREATE UNIQUE INDEX signing_key_one_active ON signing_key (active) WHERE active;

CREATE TABLE oidc_client (
  client_id           text PRIMARY KEY,
  client_secret_hash  text,            -- NULL for public clients
  display_name        text NOT NULL,
  redirect_uris       text[] NOT NULL,
  allowed_scopes      text[] NOT NULL DEFAULT ARRAY['openid','profile'],
  require_pkce        boolean NOT NULL DEFAULT true,
  created_at          timestamptz NOT NULL DEFAULT now()
);
```

A protocol-agnostic `signing-key generate` subcommand (v0.4, alongside
the OIDC OP work; reused by v0.5 SAML) creates the RSA key, derives
the JWK, and self-signs the x509 cert in one shot. All three columns
are populated on insert.

### `db/migrations/003_password_totp.sql`

```sql
-- +goose Up
CREATE TABLE password_credential (
  account_id   bigint PRIMARY KEY REFERENCES account(id) ON DELETE CASCADE,
  hash         text NOT NULL,
  algorithm    text NOT NULL DEFAULT 'argon2id',
  updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE totp_credential (
  account_id    bigint PRIMARY KEY REFERENCES account(id) ON DELETE CASCADE,
  secret_enc    bytea NOT NULL,        -- AES-GCM ciphertext
  secret_nonce  bytea NOT NULL,        -- 12 bytes
  period        int NOT NULL DEFAULT 30,
  digits        int NOT NULL DEFAULT 6,
  algorithm     text NOT NULL DEFAULT 'SHA1',
  confirmed_at  timestamptz,           -- NULL until first successful verify
  created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE recovery_code (
  id          bigserial PRIMARY KEY,
  account_id  bigint NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  hash        text NOT NULL,
  used_at     timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX recovery_code_account_id_idx ON recovery_code (account_id);

-- +goose Down
DROP TABLE recovery_code;
DROP TABLE totp_credential;
DROP TABLE password_credential;
```

- **Password**: argon2id with parameters from `configx.PasswordHashParams`.
  Verify is constant-time. Re-hash on login if params have been
  upgraded since the hash was created.
- **TOTP secret**: encrypted via AES-256-GCM with
  `PROHIBITORUM_DATA_ENCRYPTION_KEY` (32-byte key, from env or file).
  Decrypt on demand; never log plaintext.
- **Recovery codes**: 10 codes minted at TOTP enrollment confirmation,
  shown once, then only argon2id hashes retained. Single-use.

### `db/migrations/004_federation.sql`

```sql
-- +goose Up
CREATE TABLE upstream_idp (
  id                bigserial PRIMARY KEY,
  slug              text NOT NULL UNIQUE,
  display_name      text NOT NULL,
  issuer_url        text NOT NULL,
  client_id         text NOT NULL,
  client_secret_enc bytea NOT NULL,
  secret_nonce      bytea NOT NULL,
  scopes            text[] NOT NULL DEFAULT ARRAY['openid','profile','email'],
  mode              text NOT NULL CHECK (mode IN ('auto_provision','invite_only','link_only')),
  allowed_domains   text[] NOT NULL DEFAULT ARRAY[]::text[],
  username_claim    text NOT NULL DEFAULT 'preferred_username',
  display_name_claim text NOT NULL DEFAULT 'name',
  email_claim       text NOT NULL DEFAULT 'email',
  disabled          bool NOT NULL DEFAULT false,
  created_at        timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE account_identity (
  upstream_idp_id   bigint NOT NULL REFERENCES upstream_idp(id) ON DELETE CASCADE,
  upstream_sub      text NOT NULL,
  account_id        bigint NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  upstream_email    text,
  linked_at         timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (upstream_idp_id, upstream_sub)
);
CREATE INDEX account_identity_account_id_idx ON account_identity (account_id);

-- +goose Down
DROP TABLE account_identity;
DROP TABLE upstream_idp;
```

`mode` semantics on callback for unknown `(idp_id, sub)`:
- `auto_provision` — create account if `email_claim` value is in `allowed_domains` (or `allowed_domains` is empty); link; sign in.
- `invite_only` — look up a pending enrollment whose `target_account_id` is configured to await this IdP; consume it; link; sign in. No enrollment → 403.
- `link_only` — never auto-create. User must already have an account and pre-existing link → 403 with a hint to sign in via another method then link from `/me`.

### `db/migrations/005_saml.sql`

```sql
-- +goose Up
CREATE TABLE saml_sp (
  id              bigserial PRIMARY KEY,
  entity_id       text NOT NULL UNIQUE,
  display_name    text NOT NULL,
  acs_url         text NOT NULL,
  slo_url         text,
  signing_cert_pem text,                 -- for verifying SP-signed AuthnRequest (optional)
  name_id_format  text NOT NULL DEFAULT 'urn:oasis:names:tc:SAML:1.1:nameid-format:persistent',
  name_id_claim   text NOT NULL DEFAULT 'sub',
  attribute_map   jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at      timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE saml_sp;
```

`attribute_map` shape: `{"<account-attribute-or-claim>": "<saml-attribute-name>"}`.
For GHES: `{"username":"http://schemas.xmlsoap.org/...nameidentifier", "email":"...emailaddress", "full_name":"...name"}`.

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
4. **Migrations 001 and 002 rewritten in place** (picotera vocabulary
   removed; signing_key unified). New migrations 003 / 004 / 005
   (password+TOTP / federation / SAML). `mise run db:up` applies
   cleanly against a fresh Postgres.
5. **Queries** in `db/queries/account.sql` and `db/queries/enrollment.sql`
   updated to read/write `attributes` and `template_attributes`; the
   five `can_*` column references removed.
6. **`pkg/contract/auth.go`** drops `Permission` + `Permissions`;
   `AccountView` and `EnrollmentTemplate` gain `Attributes
   map[string]any`. Server handlers updated to read/write the map.
7. **Doc rewrites** — DESIGN.md, STATUS.md, AUDIT.md, INTEGRATION.md,
   README.md aligned to this spec; all picotera references removed
   from the user-facing docs (the spec retains them as the explicit
   audit trail of what was removed).
8. **`configx`** gains `DataEncryptionKey []byte`, `PasswordHashParams`,
   `TOTP` substruct (period/digits/algorithm defaults), `SAML`
   substruct (entity ID, base URL, key kid, default NameID format),
   and `WebAuthn.RPDisplayName` (default `"Prohibitorum"`).

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

- **Password brute-force.** Per-account exponential backoff via the
  `authn.RateLimiter` (moved from `pkg/auth/ratelimit.go`). Argon2id
  params tuned for ≥250ms/verify on prod hardware. No password reset
  via email channel (admin enrollment token only).
- **TOTP code guessing.** 6 digits = 10^6 space; rate-limit to ≤5
  attempts per 5 minutes per account; lock to 30s window with ±1
  period drift tolerance.
- **Recovery code theft.** Codes shown exactly once at enrollment;
  argon2id-hashed at rest; single-use; deleted after consumption.
- **Federated IdP impersonation.** Strict issuer + audience + nonce
  validation on upstream ID token. Per-IdP `client_secret` stored
  AES-GCM encrypted. Pin `issuer_url`; reject if discovery doc's
  `issuer` doesn't match.
- **JIT account squatting.** `auto_provision` mode gated by
  `allowed_domains` against `email_claim`. `username` collisions: if
  upstream's `username_claim` value matches an existing local account
  with no link to this IdP, reject (admin must intervene).
- **SAML assertion replay.** `crewjam/saml` enforces `NotBefore` /
  `NotOnOrAfter` / `InResponseTo` / one-use Assertion ID.
- **SAML XML signature wrapping (XSW).** Use crewjam/saml's
  post-canonicalization signature verification; reject assertions with
  multiple `Signature` elements or unexpected structure.
- **Encryption key compromise.** `PROHIBITORUM_DATA_ENCRYPTION_KEY`
  protects TOTP secrets + upstream client secrets. Loss of the key =
  loss of all federated logins and TOTP enrollments (forces
  re-enrollment). Documented as an operator responsibility.

## Open questions deferred to implementation versions

- TOTP issuer / label format in QR codes (v0.2).
- Whether `account.username` must be unique across linked federated
  identities, or whether `slug:upstream_sub` becomes the secondary
  identifier (v0.3).
- SAML NameID stability when the user changes `username` (v0.5).
  Likely answer: NameID format `persistent` derives from `sub`
  (account_id), not username.
- Whether SAML and OIDC should share the same signing key by default
  or have separate `kid` ranges per protocol (v0.5). Spec leaves room
  for either via the unified `signing_key` table.
