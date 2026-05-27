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

## v0.1.1 — smoke test (done)

Verified the skeleton against a real environment via `cmd/smoke`, an
in-process virtual-authenticator client that drives WebAuthn
ceremonies without a browser. **17/17 steps + DB-state assertions
pass end-to-end** against the live dev server, covering enrollment →
/me → logout → login → second-client login → revoke-by-session-id →
add-second-passkey.

Three runtime bugs the smoke test surfaced (all fixed in the same
session — see commit history):

- `webauthn_credential.cose_alg` was always stored as 0 because the
  server read `cred.Attestation.PublicKeyAlgorithm`, a go-webauthn
  field declared but never assigned by the library. Replaced with
  `pkg/credential/webauthn.COSEAlg(cred.PublicKey)`, which decodes
  the COSE_Key CBOR and extracts integer key 3 per RFC 8152 §7.1.
  New registrations now persist ES256 = -7 correctly.
- The PG `session` table was never written to — sessions stayed
  KV-only. Wired `SessionStore.Issue` to `db.InsertSession` (and
  Revoke variants to `db.RevokeSession` /
  `db.RevokeAllSessionsByAccount`) so v0.4 OIDC can carry
  `sid`/`auth_time`/`amr`/`acr` claims without a follow-up
  migration. WebAuthn issues `amr=["hwk"]`; v0.2 will add `pwd`/
  `otp`/`mfa` for password+TOTP, v0.3 will add `federated`.
- `/.well-known/openid-configuration`'s `claims_supported` advertised
  the picotera-vocabulary `"permissions"` claim. Replaced with the
  spec-correct set: `auth_time`, `amr`, `acr`, `attributes` (plus
  the standard `sub/iss/aud/exp/iat/nonce/username/displayName/role`).

`cmd/smoke` is committed as permanent v0.1.x tooling; v0.2+ will
extend it with password/TOTP and federation flows.

### Smoke-covered runtime paths

The following touched-by-v0.1.x code paths are verified by `cmd/smoke`
against a real Postgres + dev server (see commit `a1ff8a6`):

- `pkg/server/handle_enrollment.go` `insertCredentialForTx` writes
  `cose_alg=-7` (step 4 + DB assertion).
- `pkg/server/handle_me.go:201` `InsertCredential` for the
  add-second-passkey path (steps 16–17 + DB assertion).
- `pkg/session.SessionStore.Issue` writes a row to the PG `session`
  table with `amr={hwk}` on enrollment-complete (step 4) and
  login-complete (step 9) (DB assertion: 3+ rows for the test
  account).
- `pkg/session.SessionStore.Revoke` (called by `/auth/logout`) stamps
  `revoked_at` (step 6 + DB assertion: ≥2 revoked rows).
- `pkg/session.SessionStore.RevokeBySessionID` (called by
  `/me/sessions/revoke`) revokes a non-current session of the same
  account (steps 11–15: client B's session terminated by client A).
- `/.well-known/openid-configuration` `claims_supported` lists
  `attributes` (no `permissions` leak); manual curl confirmed.

### Smoke-untested runtime paths (acknowledged)

The following v0.1.x-touched paths are wired but not currently
exercised by `cmd/smoke`:

- `pkg/session.SessionStore.RevokeAllForAccount` (called by the admin
  endpoint `/accounts/{id}/revoke-sessions`). Code path is
  structurally identical to `RevokeBySessionID` + the
  `RevokeAllSessionsByAccount` SQL UPDATE; would need a second
  account + an admin-impersonation step to drive end-to-end. Deferred.
- `pkg/server/handle_pairing.go:152` (device pairing's session
  issuer with `amr=["hwk"]`). Multi-actor ceremony; the `amr` value
  is the same constant the smoke already verifies in
  enrollment/login. Deferred.

`pkg/session/session_test.go` covers the
`Issue → InsertSession fails → KV rolled back` consistency claim
with a `failingSessionQueries` stub.

### How to re-run the smoke

```bash
# Start the dev server (background terminal):
export PROHIBITORUM_DATABASE_URL="postgres://..."
export PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(openssl rand -base64 32)"
export PROHIBITORUM_PUBLIC_ORIGIN="http://localhost:8080"
mise exec -- go run ./cmd/prohibitorum

# In a second terminal (same env vars; smoke shells out to enroll-admin):
mise exec -- go run ./cmd/smoke
```

## Original v0.1.1 plan (kept for reference)

The pre-smoke version of this section listed manual verification
steps. Most are now automated by `cmd/smoke`; the remaining manual
checks are below.

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

## v0.2 — password + TOTP (done)

Shipped the password + TOTP + recovery-code fallback method and the
sudo-step-up extension that gates sensitive `/me` operations behind a
fresh credential proof. Smoke test extended from 17 to **45 steps +
DB-state assertions**, all passing against a live dev server (see
commit `5ccf3fe`).

### What shipped

- **`pkg/credential/password`** — argon2id PHC string at rest, current
  OWASP defaults (`m=64 MiB`, `t=3`, `p=1`), automatic re-hash on
  verify when `configx.PasswordHashParams` advances. Package-init
  `dummyArgon2idHash` defeats step-1 username enumeration (spec D3).
- **`pkg/credential/totp`** — RFC 6238 SHA-1 / 6-digit / 30-second TOTP
  with ±1-step drift, `last_step` defeats same-step replay (RFC 6238
  §5.2). Secrets stored AES-256-GCM with versioned DEK
  (`PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>`); AAD bound to
  `'totp:'||account_id||':'||key_version`. Recovery codes
  (10/account, 80-bit entropy, `XXXX-XXXX-XXXX-XXXX` formatted,
  argon2id-hashed at rest) minted at confirmation and regenerable via
  the new `/me/recovery-codes/regenerate` endpoint.
- **`pkg/authn/throttle`** — exponential backoff
  `[0,0,1s,2s,4s,8s,16s,32s,1m,2m,4m,8m,15m]` per `(account_id,
  factor)` (spec D2). Verify entry rejects with `429 Too Many Requests`
  + `Retry-After` when `locked_until > now`, never running the
  expensive crypto check on a locked row. Reset on success.
- **`pkg/audit/event`** — writer body wired up; emits
  `credential_event` rows for `register` / `use` / `fail` / `revoke`
  across `password` / `totp` / `recovery_code` factors, plus
  `session:sudo_granted` on every sudo completion.
- **Two-step login** — `POST /auth/password/begin` returns a
  single-use, 5-minute, KV-backed `partial_session_token`;
  `POST /auth/totp/verify` consumes it and issues a session cookie with
  `amr=["pwd","otp","mfa"]`. Disabled accounts are rejected at
  `/auth/password/begin` after a dummy verify (no timing oracle).
- **Recovery ceremony (2026-05-28 hardening; BREAKING change to the v0.2
  surface).** `/auth/recovery-code/verify` no longer issues a session.
  It consumes the recovery code + partial-session token and returns a
  narrow-scope `recovery_session_token` (10-min TTL, separate KV
  namespace) which the user redeems at the new
  `/auth/recovery/totp/{begin,verify}` endpoints. `/begin` wipes the old
  TOTP credential row and starts a fresh enrollment (recovery codes are
  preserved so the user can retry with another code if they abandon
  mid-ceremony). `/verify` atomically consumes the recovery_session_token,
  confirms the new TOTP, wipes the remaining old recovery codes, mints
  10 fresh ones, and issues a session with `amr=["pwd","otp","mfa"]`.
  Rationale: NIST SP 800-63B-4 §5.2 cautions against knowledge factors
  for reauthentication — the previous design let a stolen session +
  leaked recovery code escalate to full takeover via sudo.
- **Sudo extension** — `pkg/authn/flow.AvailableMethods` enumerates
  per-account sudo factors in priority order
  (`webauthn` → `password_totp`). `recovery_code` is **NOT** a sudo
  method (recovery routes through the ceremony, not the gate). New
  `GET /me/sudo/methods` returns the list; `/me/sudo/begin` and
  `/me/sudo/complete` accept a `method` discriminator (was
  WebAuthn-only in v0.1).
- **WebAuthn-preferred factor policy** —
  `POST /me/auth/revoke-password-totp` transactionally deletes the
  caller's `password_credential`, `totp_credential`, and
  `recovery_code` rows. Sudo-gated.

### Endpoints introduced in v0.2

| Method | Path | Notes |
|---|---|---|
| POST | `/api/prohibitorum/auth/password/begin` | step 1 of two-step login |
| POST | `/api/prohibitorum/auth/totp/verify` | step 2: TOTP |
| POST | `/api/prohibitorum/auth/recovery-code/verify` | step 2 of recovery: returns `recovery_session_token` (no session) |
| POST | `/api/prohibitorum/auth/recovery/totp/begin` | recovery ceremony: re-enroll TOTP (recovery codes preserved) |
| POST | `/api/prohibitorum/auth/recovery/totp/verify` | recovery ceremony: confirm + mint 10 fresh codes + issue session |
| POST | `/api/prohibitorum/me/password/set` | sudo-gated |
| POST | `/api/prohibitorum/me/totp/begin` | sudo-gated iff confirmed TOTP exists |
| POST | `/api/prohibitorum/me/totp/verify` | confirms enrollment, returns recovery codes |
| POST | `/api/prohibitorum/me/recovery-codes/regenerate` | sudo-gated |
| POST | `/api/prohibitorum/me/auth/revoke-password-totp` | sudo-gated, destructive |
| GET  | `/api/prohibitorum/me/sudo/methods` | NEW in v0.2 |
| POST | `/api/prohibitorum/me/sudo/begin` | extended to accept `method` param |
| POST | `/api/prohibitorum/me/sudo/complete` | extended to dispatch on `method` |

### Smoke-covered runtime paths

`cmd/smoke` exercises every v0.2 endpoint end-to-end against a real
Postgres + dev server. Each entry below references the smoke step
counter in `cmd/smoke/main.go`:

- `/me/sudo/begin` + `/me/sudo/complete` via WebAuthn — steps 18, 41
  (and prerequisite for password/set and revoke-password-totp).
- `/me/password/set` — step 19; DB assert `password_credential.hash`
  prefix `$argon2id$v=19$` at step 20.
- `/me/totp/begin` — step 21 (no sudo, first enrollment); decodes
  `secret_base32`, captures `otpauth_uri`.
- `/me/totp/verify` — step 22 (confirmation); DB assert
  `totp_credential.confirmed_at IS NOT NULL` + 10 `recovery_code` rows
  at step 23.
- `/auth/password/begin` + `/auth/totp/verify` (two-step login) —
  steps 25–26, RFC 6238 §5.2 replay window respected
  (`waitForNextTOTPStep`). `/me` round-trips post-login at step 27.
- **Recovery ceremony** — `/auth/password/begin` (step 29) →
  `/auth/recovery-code/verify` returning `recovery_session_token`
  (step 30; no session cookie) → DB assert `recovery_code[0].used_at`
  (step 31) → `/auth/recovery/totp/begin` (step 32a) → DB assert TOTP
  unconfirmed + 9 recovery codes preserved (32b) →
  `/auth/recovery/totp/verify` (32c) → DB assert TOTP confirmed +
  exactly 10 recovery codes (32d) → `/me` round-trip post-recovery
  (32e). Catches the most common regression (premature recovery-code
  wipe at `/begin`).
- `/me/sudo/begin` + `/me/sudo/complete` via `password_totp` — step
  37.
- `/me/recovery-codes/regenerate` — step 38 (consumes a sudo grant;
  asserts 10 fresh codes returned and old set invalidated).
- **Recovery-code sudo rejection** — `/me/sudo/methods` must NOT list
  `recovery_code` (step 39); `/me/sudo/begin {method:"recovery_code"}`
  must return 400 `sudo_method_unavailable` (step 40). Guards the
  hardening invariant.
- `/me/auth/revoke-password-totp` — step 42; DB assert that
  `password_credential` / `totp_credential` / `recovery_code` are all
  empty for the account at step 43; step 44 confirms
  `/auth/password/begin` returns 401 post-revoke.
- **Throttle observation** — step 34 drives wrong TOTP codes through
  `/me/sudo/begin` + `/me/sudo/complete` until the throttle responds
  `429`; step 35 asserts the `auth_throttle` row has
  `failed_attempts >= 3` and `locked_until > now`. Step 36 is a
  HARNESS-ONLY `DELETE FROM auth_throttle` so the rest of the smoke
  can proceed.
- **Audit emission** — step 45 asserts `credential_event` covers the
  union of (factor, event) pairs the v0.2 surface emits this run:
  `password:{register,use,revoke}`, `totp:{register,use,revoke}`,
  `recovery_code:{register≥10,use≥1,revoke≥9}`,
  `session:sudo_granted≥3`. The `recovery_code:use` count dropped from
  ≥2 to ≥1 post 2026-05-28 hardening (sudo-via-recovery-code path
  removed); `recovery_code:revoke` raised to ≥9 to cover the
  `recovery_complete` revoke chain emitted by the ceremony.
  `totp:{register,revoke}` are ≥2 (initial + recovery-ceremony commit).

### Smoke-untested runtime paths (acknowledged)

The following v0.2-touched paths are wired and unit-tested but not
exercised end-to-end by `cmd/smoke`:

- **Disabled-account rejection at `/auth/password/begin`.** The
  handler at `handle_auth_password.go:70` runs the dummy argon2id
  verify and returns `bad_credentials` when `account.Disabled = true`,
  matching spec D3 (no timing oracle for disabled-vs-enabled). The
  smoke account is never disabled mid-run.
- **`/me/sudo/methods`.** The endpoint is mounted and unit-tested
  (handler computes priority order via `AvailableMethods`), but the
  smoke calls `/me/sudo/begin` directly with each method rather than
  reading from the discovery endpoint first.
- **TOTP enrollment overwrite after a failed verify.** Spec D4 says
  a second `/me/totp/begin` UPSERTs the row with a fresh secret when
  `confirmed_at` is still NULL. Unit-tested in `handle_me_totp_test.go`
  but the smoke confirms a fresh TOTP on the first try, so the
  overwrite path doesn't fire.
- **`/me/totp/begin` sudo-gated re-enrollment.** When a confirmed TOTP
  already exists, a fresh `/me/totp/begin` requires sudo. Unit-tested;
  the smoke enrolls once, never replaces.
- **`PasswordHashParams` upgrade-on-verify re-hash.** Verify path in
  `pkg/credential/password` re-encodes the PHC string when the
  configured params advance. Unit-tested; the smoke runs against a
  single param set, so the re-hash branch isn't taken.
- **Throttle clearing on subsequent success.** The DELETE-on-success
  branch in `pkg/authn/throttle` is unit-tested. The smoke deliberately
  clears the throttle row via `psql DELETE` (`resetThrottle`) so it
  can continue past the lockout, rather than waiting it out and
  observing the natural reset.
- **Partial-session-token replay.** Single-use, expire-after-5-min
  semantics in `pkg/authn/flow`. Unit-tested. The smoke consumes each
  token exactly once; no replay attempt.

`pkg/credential/password`, `pkg/credential/totp`, `pkg/authn/throttle`,
`pkg/authn/flow`, and `pkg/server/handle_*_test.go` carry the unit
coverage for the cases above.

### Notes

- `pkg/credential/totp.ComputeCodeForTesting` is exported intentionally
  so `cmd/smoke` can compute the current RFC 6238 code with the same
  primitive the server uses on verify. It is the only path that exposes
  the secret post-encryption; never call from production code.
- Smoke step count is **45**, not the 46 the plan originally drafted.
  The 46th step (sudo before `/me/totp/begin` for first-time
  enrollment) was redundant per spec — first TOTP enrollment is not
  sudo-gated when no confirmed credential exists. 45 is correct.

## v0.2.1 — open follow-ups

None currently identified. The reality audit at the close of v0.2
(this section) is the canonical follow-up list; reopen when concrete
deferred items materialise.

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
