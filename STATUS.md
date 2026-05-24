# Status — what's done, what's pending

Prohibitorum's roadmap, with v0.1 (this commit) as the rescope + decoupling
skeleton and v0.1.1 through v0.7+ ahead.

## v0.1 (current commit) — rescope + decoupling

The first commit on this branch (`3d79583`) lifted the identity-layer
code from its origin project and renamed the identifier prefix to
Prohibitorum, but kept domain-flavoured permission vocabulary in
schema and contracts. **This commit rescopes the project** to four
upstream methods + two downstream protocols, strips that vocabulary,
and lands the schema / package layout / stubs needed for v0.2+ to
build on without further migrations.

### Done

- **Approach A three-layer package layout:**
  `pkg/{account, credential/{webauthn,password,totp,pairing,enrollment},
  federation/oidc, session, authn, protocol/{oidc,saml}, audit}`.
  Files moved with `git mv` to preserve blame history. `pkg/auth/` and
  `pkg/oidc/` deleted.
- **Domain-flavoured vocabulary stripped:**
  - `account.attributes jsonb` replaces five `can_*` boolean columns.
  - `enrollment.template_attributes jsonb` replaces five `template_can_*`
    columns; the intent-check CHECK constraint adapted.
  - `errorx`'s envelope type renamed to the project-agnostic `errorx.Error`.
  - `RPDisplayName` lifted to `configx.WebAuthn.RPDisplayName`
    (default `"Prohibitorum"`).
  - `contract.Permission` enum and `contract.Permissions` struct
    deleted; `AccountView` / `EnrollmentTemplate` carry
    `Attributes map[string]any` instead.
  - `auth.HasPermission` deleted; admin-only endpoints gate on
    `role = 'admin'`, anything finer is per-route attribute inspection.
- **Five migrations applied:**
  - `001_initial.sql` — account, session, webauthn_credential
    (with `user_handle`, `cose_alg`, `uv_initialized`,
    `clone_warning_at`), enrollment (with `template_attributes` +
    `expected_upstream_idp_slug`), credential_event, auth_throttle.
  - `002_oidc.sql` — `signing_key` (unified, with `use sig|enc` and
    `not_before`), `oidc_client` extended per audit
    (`post_logout_redirect_uris`, `allowed_code_challenge_methods`,
    `token_endpoint_auth_method`, `id_token_signed_response_alg`,
    `subject_type`, `application_type`, `default_max_age`,
    `require_auth_time`, `contacts`, `logo_uri`, `tos_uri`,
    `policy_uri`, `disabled`), `revoked_jti`.
  - `003_password_totp.sql` — `password_credential`, `totp_credential`
    (with `secret_enc` + `secret_nonce` + `key_version` + `last_step`),
    `recovery_code` (with `used_session_id` + `used_ip`).
  - `004_federation.sql` — `upstream_idp` (with encrypted
    `client_secret_enc` + `secret_nonce` + `key_version` and three
    provisioning modes), `account_identity` keyed
    `(upstream_iss, upstream_sub)`, forward FK
    `session.upstream_idp_id`.
  - `005_saml.sql` — `saml_sp` (with ordered-array `attribute_map`,
    `require_signed_authn_request`, metadata-freshness fields,
    per-SP `session_lifetime`), `saml_sp_acs`, `saml_sp_key`,
    `saml_subject_id`, `saml_session`.
- **Stub packages** with TODO(v0.X) markers so the import graph is
  whole: `password`, `totp`, `federation/oidc`, `protocol/saml`,
  `authn/flow`, `audit/event`. `audit.Writer` is wired into
  `server.New` so v0.2 handlers can record events without further
  plumbing.
- **`configx` extensions:** `OIDC`, `Federation`, `TOTP`, `SAML`,
  `PasswordHashParams`, `WebAuthn.RPDisplayName`, and a versioned
  `DataEncryptionKeys map[int][]byte` parsed from
  `PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>` env vars.
- **Doc rewrites:** `DESIGN.md`, `STATUS.md`, `AUDIT.md`,
  `INTEGRATION.md`, `README.md` aligned to the rescoped service. The
  spec at `docs/superpowers/specs/2026-05-24-multi-protocol-rescope-design.md`
  and the three audit reports next to it retain the original
  vocabulary as the audit trail of what was removed.
- **Build state:** `go build ./...` clean, `go test ./...` passes,
  `mise run db:up` applies all five migrations against a fresh
  Postgres.

### Out of scope for this commit

- Any v0.2+ business logic (password / TOTP / federation / SAML /
  OIDC OP). Stubs only.
- Frontend changes (`dashboard/` is empty; v0.6).
- Live smoke test — see v0.1.1 below.

## v0.1.1 (next session) — smoke test

Verify the skeleton against a real environment before adding behavior.

- `go mod tidy` and lock the indirect dep graph; commit if `go.sum` changed.
- Apply all five migrations to a real Postgres; inspect schemas match the
  spec (`\d account`, `\d session`, `\d webauthn_credential`, etc.).
- Drive `POST /api/prohibitorum/enrollments/{token}/register/{begin,complete}`
  with an HTTP client. The full browser ceremony lands in v0.6 with the
  dashboard; before then, exercise via the API and a virtual-authenticator
  Go integration test (recommended) — see "WebAuthn smoke without a
  frontend" below.
- Hit `/.well-known/openid-configuration` and `/oauth/jwks`; both are
  mounted in v0.1. The discovery doc advertises the planned v0.4 OP
  endpoints; the JWKS endpoint returns an empty `keys` array until v0.4
  introduces signing keys. `/oauth/authorize`, `/oauth/token`,
  `/oauth/userinfo`, `/oidc/logout` are NOT mounted in v0.1 — they land
  in v0.4 with real handler bodies.

### WebAuthn smoke without a frontend

`dashboard/` is empty (v0.6 work). For v0.1.1 smoke testing the WebAuthn
ceremony, two options:

1. **Go integration tests with a virtual authenticator** (recommended).
   Use `go-webauthn`'s test helpers (or a small mock authenticator) to
   drive `register/begin` → `register/complete` server-side. Runs in CI;
   pins ceremony behavior so future migrations can't break it silently.
2. **Defer the full ceremony test to v0.6**. Only run the server-side
   checks above for v0.1.1 (boot, migrations, discovery shape, JWKS
   shape, enrollment token preview). Carries silent-breakage risk between
   now and v0.6 if anyone touches `pkg/credential/webauthn`.

### Operational notes for the smoke test

Two things to know before running v0.1 against any environment:

1. **`PROHIBITORUM_DATA_ENCRYPTION_KEY_V1` is required at boot.**
   `configx.Parse()` hard-requires at least one
   `PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>` env var (32 bytes,
   base64-encoded) to be present before it will return successfully.
   Nothing in v0.1 actually uses the DEK yet — TOTP and upstream-OIDC
   client secrets are the only consumers, and both ship in v0.2 / v0.3
   — but the variable is still mandatory so deployments don't discover
   the requirement only when they try to enroll a TOTP credential.
   Quick generator:

   ```bash
   export PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(openssl rand -base64 32)"
   ```

   Multiple versions can be loaded simultaneously (`_V1`, `_V2`, …);
   the row's `key_version` column selects which key decrypts it. See
   `docs/superpowers/specs/2026-05-24-multi-protocol-rescope-design.md`
   §"AES-GCM at-rest encryption" for the rotation procedure.

2. **`mise.toml` pins `goose = "3.27.0"`, but mise's default registry
   doesn't ship a `goose` tool.** `mise install` fails on a clean
   machine with a "no plugin found" error. Two workarounds:

   - Use the aqua registry: edit `mise.toml` to read
     `goose = { version = "3.27.0", source = "aqua:pressly/goose" }`.
     **Do not change `mise.toml` as part of v0.1.1** — the fix lives in
     a separate, intentional commit so the change is auditable.
   - Or install goose manually:
     ```bash
     go install github.com/pressly/goose/v3/cmd/goose@latest
     ```
     and ensure `$GOPATH/bin` is on `$PATH`. This is the workaround the
     smoke test should use; revisit the `mise.toml` edit in a small
     maintenance task once v0.1.1 confirms migrations apply.

## v0.2 — password + TOTP

Deliver the fallback method:

- `pkg/credential/password`: argon2id PHC verify + set, re-hash on
  param upgrade, persistent throttle (`auth_throttle`).
- `pkg/credential/totp`: enrollment (secret + QR `otpauth://` URI +
  10 recovery codes shown once), AES-GCM at-rest with AAD per spec,
  ±1 period drift, `last_step` replay protection. Recovery code helpers
  live alongside TOTP in the same package (single-use, argon2id-hashed;
  redemption captures `used_session_id` + `used_ip`).
- Login flow endpoints: `POST /api/prohibitorum/auth/password/begin`,
  `POST /api/prohibitorum/auth/totp/verify`,
  `POST /api/prohibitorum/auth/recovery-code/verify`. Partial-session
  token in KV (5 min TTL) bridges the two-step flow.
- Factor-policy enforcement on WebAuthn enrollment: transactional
  delete of password + TOTP + recovery rows when the user opts into
  "disable backup" (default yes).
- `credential_event` rows written for every register / use / fail /
  revoke.

## v0.3 — upstream OIDC federation

- `upstream_idp` admin CRUD (SQL for now; admin UI in v0.6).
- `pkg/federation/oidc` via `zitadel/oidc/v3`: discovery doc fetch +
  cache, per-IdP RP flow with PKCE, state KV with snapshotted
  `expected_iss` / `expected_token_endpoint`.
- Three provisioning modes (`auto_provision`, `invite_only`,
  `link_only`) with the policy semantics from the spec.
- `account_identity` linkage on `(upstream_iss, upstream_sub)`.
- `/me/identities` UX: list / unlink linked IdPs.
- New endpoints: `GET /api/prohibitorum/auth/federation/{slug}/login`,
  `GET /api/prohibitorum/auth/federation/{slug}/callback`.

## v0.4 — OIDC OP (downstream)

- `signing-key generate` subcommand: RSA-2048, JWK + self-signed x509
  + PEM in one shot, written to `signing_key`.
- `/oauth/authorize`: query-param validation, session check, code
  mint, redirect with `iss`.
- `/oauth/token`: `authorization_code` + `refresh_token` grants;
  refresh rotation with reuse detection; family revocation on code
  replay.
- `/oauth/userinfo`: access-token verification, claim projection.
- `/oauth/introspect`: Pattern B for first-party RPs.
- `/oidc/logout`: RP-Initiated Logout 1.0 with
  `post_logout_redirect_uri` exact-match.
- ID-token claims include `auth_time`, `amr`, `acr`, `azp`, `at_hash`
  per OIDC Core §2.
- Access tokens emit RFC 9068 `typ: at+jwt` + `jti`; `revoked_jti`
  consulted on introspection.
- Rate limit on `/authorize` and `/token` (audit-flagged ❌ gap from v0.1).

## v0.5 — SAML IdP

- `crewjam/saml` integration; `/saml/metadata` publishes the IdP
  `<EntityDescriptor>` with all live + grace-period signing keys.
- `/saml/sso` SP-initiated SSO (HTTP-Redirect + HTTP-POST AuthnRequest).
- Signed `<Response>` + `<Assertion>`; `Destination` = ACS URL;
  `<Audience>` = `entity_id`; `AuthnContextClassRef` per spec.
- Stable pairwise NameID via `saml_subject_id` (32-byte random base64url
  generated on first SSO).
- Attribute mapping (ordered JSONB array): URI / basic / unspecified
  NameFormat; multi-valued attributes (`emails`, `public_keys`,
  `gpg_keys`).
- `saml_session` populated from day one; `/saml/slo` (Single Logout)
  consumes it.
- GHES preset: `sp_kind='ghes'` auto-sets
  `require_signed_authn_request=true` and uses persistent 1.1 NameID.

## v0.6 — Frontend

- `dashboard/` scaffolded with pnpm + vite + Vue 3 + Tailwind v4.
- Passkey ceremony SDK (lifted from the origin project), `PasskeyPopupHost`,
  `SessionsCard`, `PairApproveDialog`, `PairingCode` /
  `PairingCodeInput`.
- `LoginView` with method selection (WebAuthn / password+TOTP /
  federation), `?return_to=` handling that posts the user back to
  `/oauth/authorize` after sign-in.
- `EnrollView`, `MeView` (with attributes + linked IdPs +
  passkeys + password/TOTP setup), `AccountsView`,
  `RecoverChoiceView`, `AdminRecoveryView`, `CodeLoginView`.
- New views: `ClientsView` (OIDC clients), `IdPsView` (upstream OIDC),
  `SPsView` (SAML).

## v0.7+ — Hardening

- KMS-backed signing keys (AWS KMS / GCP KMS / Vault Transit adapter).
- Signing-key rotation UX (admin button + scheduled rotation job).
- Audit-log export to SIEM (kafka / file / stdout structured).
- Admin UI polish for clients / SPs / IdPs.
- Pairwise sub identifiers (RFC 9068 + OIDC Core §8.1).
- DPoP / PAR / JAR (only when a low-trust client demands them).
- Front-channel / back-channel logout for coordinated SSO sign-out.
- Conditional UI (passkey autofill) for username-first flows.
- Content Security Policy, HSTS, X-Frame-Options headers from the
  static handler (currently reverse-proxy responsibility).

## Why ship the rescope as a skeleton

The schema and package boundaries are the load-bearing decisions; once
they're committed, v0.2+ work can land in focused PRs without
disturbing them. Splitting "rescope" from "v0.2 password+TOTP
implementation" is reviewable in isolation — the v0.1 commit is purely
structural, and the audit reports in `docs/superpowers/specs/` give
the per-layer authoritative checklist that v0.2+ will tick off.
