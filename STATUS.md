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
  mounted in v0.1. **(Updated for v0.4 — see the v0.4 section below.)**
  In the v0.1 skeleton the discovery doc advertised the planned OP
  endpoints and `/oauth/jwks` returned an empty `keys` array. As of
  v0.4 all of `/oauth/authorize`, `/oauth/token`, `/oauth/userinfo`,
  `/oauth/introspect`, `/oauth/revoke`, and `/oidc/logout` are mounted
  with real handler bodies, `/oauth/jwks` serves the active signing
  key(s), and the discovery doc reflects the live surface. Do not rely
  on this v0.1.1 manual-check text for the OP shape — the v0.4 section
  is authoritative.

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

## v0.3 — upstream OIDC federation (done)

Shipped the upstream OIDC RP surface: all three provisioning modes
(`auto_provision`, `link_only`, **`invite_only`**) end-to-end,
`/me/identities` list / unlink / link, AES-256-GCM at-rest for
upstream client secrets, JWT alg allowlist, RFC 9207 iss callback
validation, federation-state KV with cross-namespace defense,
session-swap defense on the link flow, and AMR pass-through (with
`["federated"]` backfill). Smoke extended from 45 to **69 steps**
against a real Postgres + dev server + in-process mock OP (see
`cmd/smoke/mockop`).

`invite_only` ships as token-bearing redemption: an admin mints an
invite via the existing `/admin/enrollments/*` surface with
`intent='invite'` and `expected_upstream_idp_slug='<slug>'`. The user
clicks `GET /api/prohibitorum/enrollments/{token}/start-federation`,
which redirects to the upstream `/authorize`; the callback dispatches
into `applyInviteOnly` instead of the IdP-mode default and atomically
consumes the enrollment + mints the account + inserts the identity
inside one pgx transaction (`pkg/federation/oidc/modes.go`
`runInviteTx`). Audit (`credential_event` register + use) is emitted
via a tx-scoped Writer so the `account_id` FK to `account.id` resolves
against the just-inserted-but-not-yet-committed row — see the
"Bugs found and fixed" note below.

### What shipped

- **`pkg/federation/oidc/secret.go`** — AES-256-GCM for
  `upstream_idp.client_secret_enc` with the versioned-DEK family
  (`PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>`). AAD bound to
  `'upstream_idp:'||id||':'||key_version` so a ciphertext lifted
  between rows fails to decrypt. 12-byte per-row nonce. 5/5 unit tests
  in `secret_test.go`.
- **`pkg/federation/oidc/client.go`** — wraps `zitadel/oidc/v3 v3.47.5`.
  Discovery fired once at `NewClient`; the library caches JWKS
  internally. ID-token alg allowlist
  (`DefaultAllowedAlgs() = {RS256, ES256, EdDSA}`) enforced at the
  library layer AND re-checked post-decode (defense-in-depth against
  a library bug that admits `HS256` / `none`). Nonce threaded via
  context-key.
- **`pkg/federation/oidc/federation.go`** — `Federator` orchestrates
  `BeginLogin` / `HandleCallback` / `LinkBegin` / `LinkCallback`.
  Federation-state KV is keyed under `LoginKey(token)` vs
  `LinkKey(token)` — a state token minted for a link flow cannot be
  consumed by the public login callback, and vice versa
  (cross-namespace defense, unit-tested). State is single-use via
  `kvStore.Pop`. State payload snapshots `ExpectedIss` +
  `ExpectedTokenEndpoint` + `Nonce` + `CodeVerifier` so a discovery
  change mid-flow can't silently re-target the user to a different OP.
  RFC 9207 `iss` callback parameter validated against
  `state.ExpectedIss`. Post-`Resolve` disabled-account check returns
  `authn.ErrBadCredentials()` — same enumeration-safe path as the
  password login (federation.go:269).
- **`pkg/federation/oidc/modes.go`** — three provisioning modes:
  - `auto_provision` gated by `RequireVerifiedEmail` +
    `AllowedDomains` + `preferred_username` presence + local
    username-collision check. Mints a fresh `webauthn_user_handle` on
    JIT so a federated user can enroll a passkey later. Emits
    `register` + `use` audit rows on success.
  - `invite_only` — token-bearing redemption via
    `GET /enrollments/{token}/start-federation`. The HTTP shim
    (`pkg/server/handle_invite_federation.go`) validates the invite
    upfront via `Federator.BeginInviteRedemption`, stashes the
    enrollment token in `FedState.EnrollmentToken`, and 302s to the
    upstream `/authorize`. The callback notices the token on FedState
    and dispatches `applyInviteOnly` instead of `Resolve`-by-mode.
    Inside `runInviteTx`, a single pgx transaction wraps
    `ConsumeEnrollment` + `InsertAccount` + `InsertAccountIdentity` +
    the `register`/`use` audit emission via a tx-scoped Writer.
    Skips `RequireVerifiedEmail` + `AllowedDomains` by design (D11):
    the admin minted the invite specifically for this user, which IS
    the authorization. Username collision is re-checked at redemption
    time (race-bound to invite-create). Audit emits
    `reason: "invite_only_redemption"` on success;
    `invite_consumed_or_expired` / `invite_slug_mismatch` /
    `username_collision` on the in-tx fail branches; and
    `invite_lookup_failed` / `invite_wrong_intent` /
    `invite_already_consumed` / `invite_expired` /
    `invite_not_federated` at the `BeginInviteRedemption` pre-flight
    (each emits `failNoAccount` via the outer audit writer).
  - `link_only` — rejects unknown `(iss, sub)` with `link_required`.
  - Re-login claim sync (spec D2): updates `account.display_name`
    when upstream `name` drifts; updates `account_identity.upstream_email`
    when upstream email drifts; both conditional on a diff so the
    `updated_at` trigger doesn't fire on no-op logins
    (`modes.go:240–267`).
- **HTTP surface** — `pkg/server/handle_federation.go` (public login
  + callback) and `pkg/server/handle_me_identities.go` (sudo-gated
  link + unlink). No IP-keyed rate limit at the HTTP edge — audit
  fix M5 (commit `4c4412c`) removed all 13 IP-keyed buckets project-wide
  because `sessstore.ClientIP` is not trustworthy behind NAT / CDN
  (false positives shared-IP lockout; false negatives IP-rotating
  attacker). Per-account `auth_throttle` and PKCE + single-use KV state
  carry the replay/brute-force defense; reverse-proxy / WAF owns
  edge DoS. See AUDIT.md "Rate limiting policy (v0.3 audit)".
  Return-to validation rejects anything that isn't a relative path
  beginning with `/` and not `//` (`validateFederationReturnTo` in
  `handle_federation.go`). AMR backfilled to `["federated"]` when
  upstream omits the claim (`handle_federation.go:113–119`, citing
  RFC 8176 §2 which explicitly defines `federated` as a valid AMR
  value).
- **`/me/identities` flow** — link begin is sudo-gated; link callback
  is NOT sudo-gated (the user just elevated at `/begin` and a fresh
  sudo prompt after the upstream round-trip would be hostile UX).
  `LinkCallback` validates that the current session matches the
  `LinkingAccountID` stashed in state — defeats a session-swap
  mid-flow where the attacker lures the victim's browser to complete
  the attacker's link (`federation.go:307–312`, unit-tested).
- **Unlink last-method check** — `handleMeIdentitiesUnlinkHTTP`
  computes the post-unlink method set and rejects with
  `last_sign_in_method` when the only remaining method on the account
  is the very identity row being unlinked
  (`handle_me_identities.go:121–145`).
- **`pkg/authn` errors** — 8 new structured errors:
  403 `email_not_verified` / `username_collision` /
  `invite_required` / `link_required`;
  401 `federation_state_invalid`;
  400 `last_sign_in_method` / `invalid_return_to` /
  `upstream_error{code, description}` (`errors.go:274–339`).
- **`pkg/authn.AvailableMethods`** — now appends
  `MethodFederationOIDC` when the account has ≥1 `account_identity`
  row (`flow.go:75–81`). Drives the `/me/sudo/methods` discovery
  surface and the unlink last-method computation.
- **Migration 006** — `006_federation_v03.sql` added
  `upstream_idp.require_verified_email BOOLEAN NOT NULL DEFAULT true`.
  The `account_identity` table and the rest of the `upstream_idp`
  schema were already in migration 004 (v0.1 skeleton).

### Endpoints introduced in v0.3

| Method | Path | Notes |
|---|---|---|
| GET | `/api/prohibitorum/auth/federation/{slug}/login` | public; 302 to upstream `/authorize` |
| GET | `/api/prohibitorum/auth/federation/{slug}/callback` | public; handles `?error=`; issues session |
| GET | `/api/prohibitorum/enrollments/{token}/start-federation` | public (token-bearing); 302 to upstream `/authorize` after validating the invite; emits `Referrer-Policy: no-referrer` so the token doesn't leak to the upstream |
| GET | `/api/prohibitorum/me/identities` | session-required; JSON array `[{id, idpSlug, idpDisplayName, upstreamEmail, linkedAt}]` |
| POST | `/api/prohibitorum/me/identities/{id}/unlink` | session + sudo; 204 on success; refuses when this is the last sign-in method |
| GET | `/api/prohibitorum/me/identities/link/{slug}/begin` | session + sudo; 302 to upstream `/authorize` (LinkKey-namespaced state) |
| GET | `/api/prohibitorum/me/identities/link/{slug}/callback` | session-required (not sudo); validates session matches state.LinkingAccountID; does NOT issue a new session |

### Smoke-covered runtime paths

`cmd/smoke` extends from 45 to **69 steps**. The v0.3 block is steps
46–69; the in-process mock OP under `cmd/smoke/mockop`
signs ES256 ID tokens against a JWKS served from the test process.
Each entry below references the smoke step counter in
`cmd/smoke/main.go`:

- **Seed upstream_idp** — step 46/69 inserts `mockop` (auto_provision,
  AES-GCM-encrypted client secret, allowed_domains `["example.com"]`,
  `require_verified_email = true`).
- **Happy-path login** — steps 47/69–49/69 walk
  `/auth/federation/mockop/login` → upstream `/authorize` →
  RP `/callback` → 302 to `/me` with a session cookie.
  Step 50/69 round-trips `/me` as the federated user.
- **JIT row inserted** — step 51/69 DB-asserts an `account_identity`
  row exists for `ext-user-1` with `(upstream_iss, upstream_sub)`
  matching the mock OP's claims.
- **Re-login claim sync (D2)** — step 52/69 changes the upstream
  display name, re-logs in, DB-asserts `account.display_name` updated.
- **email_not_verified** — step 53/69 sets the mock OP's
  `email_verified=false`, drives a fresh login, asserts 403 +
  `email_not_verified` error code.
- **username_collision** — step 54/69 changes the mock OP's
  `preferred_username` to a value that already exists locally,
  asserts 403 + `username_collision`.
- **invalid_return_to** — step 55/69 passes
  `return_to=https://evil.example.com` to `/login`, asserts 400 +
  `invalid_return_to`. Caught at the HTTP layer before the federator
  runs (no audit emission expected). The `//`-prefix branch of the
  validator is unit-tested but not driven by the smoke.
- **upstream_error** — step 56/69 simulates the OP returning
  `?error=access_denied`, asserts 400 + `upstream_error` and a
  `fail` audit row with `reason: "upstream_error"`.
- **`GET /me/identities`** — step 57/69 lists 1 row for the
  federated user; asserts `idpSlug`, `idpDisplayName`,
  `upstreamEmail`, and an ISO-8601 `linkedAt`.
- **Seed second IdP** — step 58/69 inserts `mockop-link`
  (link_only mode).
- **link_only refuses unknown** — step 59/69 drives a login for a
  fresh upstream sub against the `link_only` IdP; asserts 403 +
  `link_required` + a `fail` audit row.
- **Self-service link** — step 60/69 re-logs in as `smoke-admin`
  via WebAuthn. Step 61/69 sudos via WebAuthn, hits
  `/me/identities/link/mockop/begin`, follows through the mock OP,
  asserts the original session cookie survives the round-trip
  (no new session minted by the link callback).
- **Link DB-asserted** — step 62/69 confirms an `account_identity`
  row exists for `admin-link-1` owned by the `smoke-admin` account.
- **List as admin** — step 63/69 confirms `/me/identities` returns
  exactly 1 row for `smoke-admin` post-link.
- **Unlink** — step 64/69 sudos via WebAuthn and POSTs
  `/me/identities/{id}/unlink`, asserts 204 and DB row gone.
  The smoke-admin survives the unlink because they still have
  WebAuthn — the last-sign-in-method guard is satisfied.
- **invite_only end-to-end** — step 65/69 seeds an
  `intent='invite'` enrollment with
  `expected_upstream_idp_slug='mockop'`, then drives
  `GET /enrollments/{token}/start-federation?return_to=/me` →
  upstream `/authorize` → RP `/callback` → 302 to `/me` + session
  cookie. Confirms `/me` returns the template `username` and
  `displayName` (NOT the upstream `preferred_username`/`name`,
  proving the invite template overrides the OP claims).
- **invite_only DB asserts** — step 66/69 confirms
  `enrollment.consumed_at IS NOT NULL`, the account row exists
  with the template username, the `account_identity` row links to
  the upstream sub under the IdP, and a `credential_event` row
  exists with `factor='federation_oidc' event='register'
  detail->>'reason' = 'invite_only_redemption'`.
- **invite_only consumed-token rejection** — step 67/69 reuses
  the already-redeemed token; the federator's
  `BeginInviteRedemption` rejects pre-flight with
  403 `invite_required` (no upstream hop made).
- **invite_only expired-token rejection** — step 68/69 seeds a
  fresh enrollment with `expires_at = now() - interval '1 second'`
  and confirms 403 `invite_required`.
- **Audit emission** — step 69/69 asserts `credential_event`
  lower bounds for the v0.3 lifecycle:
  `federation_oidc:register ≥ 2` (auto_provision +
  invite_only_redemption),
  `federation_oidc:use ≥ 4` (3× existing + invite session-start),
  `federation_oidc:fail ≥ 6` (4× existing + invite consumed +
  invite expired),
  `federation_oidc:link ≥ 1`, `federation_oidc:unlink ≥ 1`.
  Observed run: `register:2, use:4, fail:6, link:1, unlink:1`.

### Smoke-untested runtime paths (acknowledged)

The following v0.3-touched paths are wired but not exercised
end-to-end by `cmd/smoke`. Most carry unit-test coverage in
`pkg/federation/oidc/*_test.go` or `pkg/server/handle_me_identities_test.go`.

- **invite_only username_collision at redemption.**
  `applyInviteOnly` re-checks username availability at redemption
  time (race against a concurrent invite mint or password sign-up
  taking the slot between invite-create and redemption);
  `pkg/federation/oidc/modes.go` `username_collision` branch is
  unit-tested in `modes_test.go` but the smoke can't easily stage
  the race so it's not exercised end-to-end.
- **invite_only transactional rollback.** `runInviteTx` rolls back
  the consume + insert + audit when any in-tx step fails. Unit-tested
  via fake querier in `modes_test.go`. The smoke can't reliably stage
  a mid-tx failure (the upstream OP doesn't lie about the code
  exchange).
- **invite_only `expected_upstream_idp_slug` mismatch.** If the
  invite is bound to slug A but the user somehow drives it through
  slug B's callback, `applyInviteOnly` rejects with
  `invite_slug_mismatch`. Unit-tested only — the smoke's
  `start-federation` handler binds the IdP by reading
  `enrollment.expected_upstream_idp_slug`, so reaching the
  mid-flight slug edit branch requires DB-level tampering that
  isn't worth automating.
- **`iss_mismatch_callback` (RFC 9207 reject).** Federator rejects a
  callback whose `?iss=` doesn't match `state.ExpectedIss`. Unit-test
  in `federation_test.go` only — the smoke uses a single mock OP, so
  a mismatch can't be staged without a second OP.
- **Cross-namespace state reuse.** A `LoginKey`-namespaced token
  cannot be redeemed via the link callback (and vice versa).
  Unit-tested in `federation_test.go`. The smoke never attempts the
  swap.
- **Session-swap defense on LinkCallback.** `state.LinkingAccountID`
  must match the current session's `Account.ID`. Unit-tested
  (`federation_test.go`). The smoke flows the link callback under
  the same session that issued `/begin`.
- **`code_exchange_failed` from upstream.** The mock OP always
  honors the code. Unit-tested via a stubbed exchange in
  `federation_test.go`.
- **Disabled-account check post-Resolve.** Federator returns
  `authn.ErrBadCredentials()` if an admin disables the account
  between provisioning and session-mint
  (`federation.go:269–278`). Unit-tested; the smoke account is
  never disabled mid-flow.
- **Unlink-last for a federated-only user.** The
  `last_sign_in_method` reject is unit-tested in
  `handle_me_identities_test.go`. The smoke can't drive it
  end-to-end: the unlink endpoint is sudo-gated, and a
  federated-only user has no sudo method available
  (`recovery_code` was de-listed in v0.2 post-2026-05-28 hardening,
  and federation is not a sudo method).
- **Upstream refresh tokens.** Not implemented. Federated accounts
  re-authenticate via `/login` each time — Prohibitorum does not
  store or refresh upstream OIDC tokens. Tracked as ❌ in AUDIT.md.
- **HS256 / `none` rejection by the alg allowlist.** The mock OP
  only signs ES256, so the post-decode alg recheck branch is
  `t.Skip`ed in `client_test.go`. The library-level allowlist
  still rejects via configuration.
- **Per-IdP claim-name overrides (smoke-untested, ✅ implemented).**
  `upstream_idp.username_claim` / `display_name_claim` / `email_claim`
  are honored end-to-end (commit `45083bc`, audit fix M4). The
  auto-provision path reads via `ClaimString(tokens.Raw, idp.Username
  Claim/DisplayNameClaim/EmailClaim)` in
  `pkg/federation/oidc/modes.go:133–135`; the re-login drift sync
  honors the same overrides at `modes.go:518–519`; the link-flow
  email override fires at `pkg/federation/oidc/federation.go:453`;
  invite redemption uses the email override at `modes.go:383`.
  Unit-tested in `modes_test.go` (override-key coverage). Smoke does
  not stage an Entra-style OP (the mock OP only ships
  `preferred_username` / `name` / `email`), so the override branch
  is unit-tested only — schema defaults match the OIDC standard
  claim names, so the typical deployment path is exercised by every
  smoke run.

### Notes

- The mock OP under `cmd/smoke/mockop` is a deliberately
  minimal in-process OP — discovery, JWKS, `/authorize`, `/token`
  with PKCE, ES256 signing. It exists so the smoke can drive an
  upstream round-trip without booting Keycloak. Not safe for
  production; never reuse outside the smoke harness.
- Federation handlers no longer carry IP-keyed rate limits — audit
  fix M5 (commit `4c4412c`) removed all 13 IP buckets project-wide.
  What remains: per-account `auth_throttle` (DB-backed lockout),
  account/session-keyed buckets for pairing + sudo, and the PKCE +
  single-use KV state token. Edge DoS protection is the reverse
  proxy / WAF's job. The multi-replica caveat documented in
  `docs/superpowers/notes/2026-05-28-v0.2-deployment-notes.md` §1
  still applies to the remaining account/session-keyed buckets.

### Hardening fixes since initial v0.3 ship

Five audit-driven defenses landed on top of the initial v0.3 smoke
pass, each backed by a smoke step (where smoke-driveable) or unit
test coverage:

- **M4 — per-IdP claim-name overrides honored** (commit `45083bc`).
  `pkg/federation/oidc/modes.go:133–135,518–519,383` +
  `federation.go:453` route through the shared `ClaimString` helper,
  closing the schema-vs-code gap previously tracked here as a ⚠️
  ("schema only"). Smoke continues to exercise the default OIDC
  claim names; override-key behavior covered in `modes_test.go`.
- **M5 — IP-keyed rate limits removed project-wide**
  (commit `4c4412c`). `sessstore.ClientIP` cannot reliably identify
  a client behind NAT / CDN / corporate egress; both false positives
  (legitimate-user lockout) and false negatives (IP-rotating attacker
  bypass) demonstrated. Federation, enrollment, pairing, sudo, and
  auth handlers now rely on per-account / per-session keys plus
  PKCE + KV single-use tokens.
- **C1 + H3-di + H4-di — `applyAutoProvision` wrapped in a
  transaction with clean 23505 mapping** (commit `9ee15a4`).
  `runProvisionTx` is now shared by both `applyInviteOnly` and
  `applyAutoProvision`. Concurrent same-username inserts and
  duplicate `(iss, sub)` callbacks surface as
  `ErrUsernameCollision` / `ErrInviteRequired` (the latter
  collapsed onto the link_conflict anti-enumeration treatment)
  rather than wrapped 500s; tx rollback drops the partial account
  row. See `pkg/federation/oidc/modes.go:198–230` for the
  auto-provision branch and `:367–402` for the invite branch.
- **M1-int — federation-bound invites reject the WebAuthn enrollment
  path** (commit `9ed0b1b`).
  `/enrollments/{token}/register/{begin,complete}` reject any invite
  whose `expected_upstream_idp_slug` is set, returning
  `ErrEnrollmentFederationRequired()` so the invitee is forced
  through `/enrollments/{token}/start-federation`. Belt-and-suspenders
  rejection at both `/begin` (`handle_enrollment.go:181–189`) and
  `/complete` (`handle_enrollment.go:383–392`).
- **H3-sch — `ExpectedTokenEndpoint` snapshot validated at callback**
  (commit `4576a05`). FedState already snapshotted the upstream
  `token_endpoint` at BeginLogin; `HandleCallback`
  (`federation.go:310–316`) and `LinkCallback`
  (`federation.go:420–426`) now reject when the live discovery's
  token_endpoint drifts from the snapshot, audited with
  `reason=token_endpoint_drift`. Mix-up resistance per RFC 9700
  §4.4.2.1.
- **M1-di — `DeleteAccountIdentity` returns rows-affected and the
  handler 404s on no match** (commit `5cd1f07`).
  `db/queries/account_identity.sql:15–19` converted the DELETE to
  `:one` with `RETURNING id`; the handler at
  `handle_me_identities.go:177–192` maps `pgx.ErrNoRows` to
  `ErrCredentialNotFound` (404, no audit), preventing audit-log
  pollution from no-op unlinks of foreign / already-deleted rows.

### Bugs found in stage 3 (invite_only smoke) and fixed

- **`applyInviteOnly` audit FK race against uncommitted account row.**
  The first invite-redemption smoke run surfaced missing
  `credential_event` rows for `factor='federation_oidc'
  event='register' detail->>'reason'='invite_only_redemption'`.
  Root cause: the audit `Writer` was the federator's outer
  pool-bound writer (`f.audit`), so `InsertCredentialEvent` ran on
  a different connection from the in-tx `InsertAccount`. The
  `credential_event.account_id` FK to `account.id` was checked
  against the MVCC snapshot of the other connection — which
  didn't yet see the uncommitted account row — so the insert
  silently failed (the Writer swallows errors by convention) and
  no audit row landed. Fix: `runInviteTx` now constructs a
  tx-scoped `audit.Writer` (`audit.NewWriter(qtx)`) and passes it
  to the closure; the audit insert runs inside the same tx as
  the account insert, so the FK resolves and rollback semantics
  are correct (no orphan audit rows). See
  `pkg/federation/oidc/modes.go` `runInviteTx` + the
  `applyInviteOnly` audit block. The smoke step 66/69 assertion
  is the regression gate.

## v0.4 — downstream OIDC OP (done)

Shipped Prohibitorum's first-party OpenID Connect Provider surface: the
full Authorization Code + PKCE flow, RFC 9068 access tokens, OIDC Core
ID tokens, refresh rotation with reuse-detection + family revocation,
RFC 7662 introspection, RFC 7009 revocation, and RP-Initiated Logout
1.0 — plus the `signing-key` and `oidc-client` CLIs that provision keys
and clients. All handlers are `Provider` methods in `pkg/protocol/oidc`,
routes mounted root-level (NOT under `/api/prohibitorum`) in
`pkg/server/server.go:286–294`. Smoke extended from 69 to **87 steps**
(v0.4 block is steps 70–87), all green end-to-end against live Postgres
+ a fresh dev server + an in-process mock RP in `cmd/smoke`.

### What shipped

- **`pkg/protocol/oidc` Provider surface** — hand-rolled chi-mounted
  handlers (design D5); `go-jose/v4` for JWT sign/verify only.
  - **Discovery** (`oidc.go` `HandleDiscovery`): expanded
    `/.well-known/openid-configuration` — `scopes_supported`
    `[openid, profile, offline_access]`; `introspection_endpoint`,
    `revocation_endpoint`, `end_session_endpoint`;
    `code_challenge_methods_supported [S256]`;
    `authorization_response_iss_parameter_supported: true`;
    `token_endpoint_auth_methods_supported`
    `[client_secret_basic, client_secret_post, none]`;
    `claims_supported` `[sub, iss, aud, exp, iat, nonce, auth_time,
    amr, acr, sid, at_hash, username, displayName, role, attributes]`;
    `id_token_signing_alg_values_supported [RS256]`;
    `Cache-Control: public, max-age=300`.
  - **JWKS** (`keys.go` + `HandleJWKS`): real key set from the active +
    cached `signing_key` rows (RFC 7517 RSA JWK, RS256), replacing the
    v0.1 empty-array stub.
  - **`/oauth/authorize`** (`authorize.go`): Authorization Code + PKCE
    (S256-only); `redirect_uri` exact-match with an open-redirect guard
    (invalid client / unregistered `redirect_uri` → direct error page,
    never a redirect to the unvalidated URI); session-gated via the
    existing middleware; 302 back to `redirect_uri?code=…&state=…&iss=…`
    (RFC 9207).
  - **`/oauth/token`** (`token.go` + `refresh.go`):
    `authorization_code` grant (client auth basic/post/none, PKCE
    verify, single-use code via KV `Pop`; replay → family revoke) and
    `refresh_token` grant (rotation + reuse-detection → family revoke +
    disabled-account re-check). Access token = RFC 9068 JWT
    (`typ:at+jwt`, `jti`, `iss/aud/sub/client_id/exp/iat/scope`);
    ID token = OIDC Core JWT with `at_hash`, `sid`, `auth_time`, `amr`;
    refresh token issued only when `offline_access` is granted.
  - **`/oauth/userinfo`** (`userinfo.go`, GET + POST): Bearer
    access-token verify (signature by `kid` + `iss` + `exp` +
    `typ:at+jwt` + `revoked_jti` denylist), scope-gated claim
    projection; 401 + `WWW-Authenticate: Bearer error="invalid_token"`
    on failure.
  - **`/oauth/introspect`** (`introspect.go`, RFC 7662):
    client-authenticated, per-client ownership (a client sees only its
    own tokens); `{active:false}` with no detail leak otherwise.
  - **`/oauth/revoke`** (`revoke.go`, RFC 7009): client-authenticated,
    per-client ownership; access token → `revoked_jti` PG denylist
    (self-pruning, TTL = token exp), refresh token → family revoke;
    always 200.
  - **`/oidc/logout`** (`logout.go`, RP-Initiated Logout 1.0):
    validates `id_token_hint` signature + `iss` (tolerates expiry),
    revokes the session named by the hint's `sid` (SSO sign-out),
    exact-match `post_logout_redirect_uri` (mismatch → direct error),
    302 with `state`.
- **CLIs (`cmd/prohibitorum`):**
  - `signing-key generate [--activate] [--retire <kid>]` — mints an
    RSA-2048 signing key (RFC 7638 thumbprint `kid`, JWK, self-signed
    x509, PKCS#8 PEM) into one `signing_key` row; first key / `--activate`
    becomes the active key (deactivating the prior active in one tx);
    `--retire <kid>` stamps `retired_at`.
  - `oidc-client create --client-id --display-name --redirect-uri(repeatable)
    [--post-logout-redirect-uri] [--scope] [--public] [--require-consent]`
    — registers a client; confidential (default) generates a 32-byte
    secret printed ONCE (only the argon2id hash is stored;
    `client_secret_basic`); `--public` → no secret, `none` auth, PKCE
    required. `oidc-client list` — lists client_id / display_name /
    auth_method / disabled.
- **Storage model (D8):** authorization codes + refresh tokens live in
  KV (codes single-use via `Pop` + a replay used-marker; refresh tokens
  opaque, rotated, with a per-family record for reuse detection and
  family revocation); access tokens are stateless RFC 9068 JWTs revoked
  via the `revoked_jti` PG denylist; ID tokens are stateless JWTs.
  `sub` = `account.oidc_subject` (uuid; D6). No new tables — the
  refresh-family forensics table stays deferred.

### Endpoints introduced in v0.4

All routes are root-mounted (not under the `/api/prohibitorum` prefix
the v0.2/v0.3 surfaces use), because OIDC clients expect them at the
issuer root.

| Method | Path | Notes |
|---|---|---|
| GET | `/.well-known/openid-configuration` | expanded discovery doc (was a v0.1 stub) |
| GET | `/oauth/jwks` | real RSA JWK set from active+cached signing keys (was empty in v0.1) |
| GET | `/oauth/authorize` | Authorization Code + PKCE (S256); session-gated; 302 with `code`+`state`+`iss` |
| POST | `/oauth/token` | `authorization_code` + `refresh_token` grants; client auth basic/post/none |
| GET / POST | `/oauth/userinfo` | Bearer access-token verify; scope-gated claims |
| POST | `/oauth/introspect` | RFC 7662; client-authenticated; per-client ownership |
| POST | `/oauth/revoke` | RFC 7009; client-authenticated; per-client ownership; always 200 |
| GET | `/oidc/logout` | RP-Initiated Logout 1.0; `id_token_hint` + exact-match `post_logout_redirect_uri` |

### Smoke-covered runtime paths

`cmd/smoke` extends to **87 steps**; the v0.4 block is steps 70–87
against a real Postgres + dev server + an in-process mock RP. Each
entry references the smoke step counter in `cmd/smoke/main.go`:

- **70** — `signing-key generate` then `GET /oauth/jwks` returns
  **exactly 1 key** whose `kid` matches the minted key.
- **71** — `oidc-client create` (confidential; scopes
  `openid+profile+offline_access`) returns a non-empty one-time secret.
- **72** — `GET /oauth/authorize` with PKCE S256 + a live WebAuthn
  session → 302 to `redirect_uri` carrying `code`+`state`+`iss`.
- **73** — `POST /oauth/token` `authorization_code` (HTTP Basic client
  auth + PKCE `code_verifier`) → `token_type:Bearer`, `expires_in>0`,
  access + id + refresh tokens. The id_token is verified against JWKS
  (`iss`, `aud`, `sub`, `nonce`, `at_hash`, `sid`, `auth_time`, `amr`);
  the access token verifies with JOSE `typ:at+jwt` and a `jti`; the
  refresh token is present (because `offline_access` was granted).
- **74** — `GET /oauth/userinfo` (Bearer): `sub` matches the id_token,
  `username` matches the smoke account, `displayName` present (profile
  scope).
- **75** — `POST /oauth/introspect` (Basic) on the access token →
  `active:true`, `token_type:access_token`, `client_id`, `sub` match.
- **76** — `POST /oauth/token` `refresh_token` → rotated refresh token
  (new ≠ old) + a re-issued id_token that re-verifies against JWKS.
- **77** — replay the OLD (superseded) refresh token → 400
  `invalid_grant` (reuse detection).
- **78** — the reuse at step 77 revoked the whole family: the current
  (rotated) refresh token is now ALSO dead → 400 `invalid_grant`.
- **79** — fresh authorize+token → `POST /oauth/revoke` the refresh
  token (200) → a subsequent refresh → 400 `invalid_grant`.
- **80** — fresh authorize+token → `POST /oauth/revoke` the access
  token → introspect now shows `active:false`; a `revoked_jti` row is
  written.
- **81** — negative: unregistered `redirect_uri` at `/oauth/authorize`
  → direct 400 `invalid_request`, with NO `Location` to the bad URI
  (open-redirect guard).
- **82** — negative: wrong PKCE `code_verifier` at `/oauth/token` →
  400 `invalid_grant`.
- **83** — negative: wrong client secret at `/oauth/token` → 401
  `invalid_client`.
- **84** — `GET /oidc/logout` with `id_token_hint` +
  `post_logout_redirect_uri` + `state` → 302 to the post-logout URI
  with `state` echoed.
- **85** — the logout revoked the session named by the id_token's
  `sid`: the client's `/me` now returns 401.
- **86** — DB assert: a `revoked_jti` row exists for the access token
  revoked at step 80.
- **87** — DB assert: `credential_event` (factor `oidc_client`) covers
  the lifecycle — `use:authorize ≥5`, `use:token_issued ≥3`,
  `use:refresh_rotated ≥1`, `use:logout ≥1`, `fail:refresh_reuse ≥1`,
  `revoke:revoked ≥2`.

### Smoke-untested runtime paths (acknowledged)

The following v0.4-touched paths are wired and unit-tested (per-file
`*_test.go` in `pkg/protocol/oidc`) but not exercised end-to-end by
`cmd/smoke`. The smoke always authenticates the test account first and
drives a single confidential client over HTTP Basic, so these branches
do not fire:

- **No-session `/oauth/authorize` → 302 to `Issuer+/login?return_to=…`**
  (D7). The smoke holds a live WebAuthn session before hitting
  `/authorize`, so the redirect-to-login branch is unit-tested only.
- **`prompt=none` + no session → `login_required`** (no redirect).
  Unit-tested only.
- **`require_consent=true` → `consent_required`** (D2). The
  `oidc_client.require_consent` column ships and is honored at
  `/authorize`, but auto-approve is the default policy and there is no
  consent UI until a later version. The smoke's client leaves
  `require_consent` at its default `false`.
- **JWKS with multiple keys / signing-key rotation + `--retire`**, and
  the `signing-key generate --activate` re-activation path. The smoke
  mints exactly one key and never rotates.
- **Public client (`none` auth method) full code flow**, and
  `client_secret_post` auth at the token endpoint. The smoke uses a
  confidential client with HTTP Basic (`client_secret_basic`) — that is
  the only auth method exercised end-to-end.
- **`oidc-client list`.** The CLI exists and is unit-/manually testable
  but the smoke only calls `create`.

### Notes

- **D2 — Consent.** Auto-approve once a valid session exists;
  `require_consent` is a reserved flag honored at `/authorize`
  (returns `consent_required`) with no consent UI until the v0.6
  frontend.
- **D3 — Rate limits keyed on identity, NOT IP.** `/authorize` per
  `account_id`; `/token`, `/introspect`, `/revoke` per `client_id`;
  `/userinfo` per `sub` (keys like `oidc:token:client:<id>`,
  `oidc:authorize:acct:<id>`, `oidc:userinfo:sub:<sub>`). This both
  satisfies the long-standing "rate limit `/authorize` + `/token`"
  goal AND respects the v0.3 M5 decision that client IP is
  untrustworthy behind NAT/CDN — no per-IP buckets were reintroduced.
- **D5 — Hand-rolled handlers** in `pkg/protocol/oidc`; `go-jose/v4`
  used for JWT only. The `zitadel/oidc/v3 pkg/op` framework is NOT
  adopted (it would invert control over the bespoke session/sudo/audit
  model).
- **D6 — `sub` = `account.oidc_subject`** (uuid, DB-side
  `gen_random_uuid()` default; no account-creation path changed).
  Pairwise `sub` salting stays deferred to v0.7+.
- **D8 — Storage split.** Codes + refresh tokens in KV; access + ID
  tokens stateless JWTs; access-token revocation via the `revoked_jti`
  PG denylist. The refresh-family forensics table is deferred (KV-only
  for v0.4).
- **Audit factor is `oidc_client`** (not `oidc`): the v0.4 spec drafted
  `oidc`, but the shipped handlers and the smoke step-87 assertion use
  `oidc_client` — that is the value to query in `credential_event`.

## v0.5 — downstream SAML IdP (done)

Shipped Prohibitorum's downstream **SAML 2.0 Identity Provider** with a
**GitHub Enterprise Server (GHES)-compatible profile**: SP-initiated SSO
(HTTP-Redirect AuthnRequest in, HTTP-POST signed Response out), IdP-local
Single Logout, IdP `EntityDescriptor` metadata, a metadata-ingesting
`saml-sp` CLI, stable opaque persistent NameID, the GHES attribute
profile, and a first-class XML-security hardening pass. Handlers are
`IdP` methods in `pkg/protocol/saml`; the 3 routes are mounted root-level
(NOT under `/api/prohibitorum`) in `pkg/server/server.go:320–324`. Smoke
extended from 87 to **99 steps** (v0.5 block is steps 88–99), all green
end-to-end against live Postgres + a fresh dev server + an in-process
mock SP in `cmd/smoke`. The final smoke tally is `45/45 (v0.2) + 46–69
(v0.3) + 70–87 (v0.4) + 88–99 (v0.5 SAML IdP)` with `SMOKE_EXIT=0`.

> This section is the authoritative current state of the SAML surface.
> The v0.1 skeleton text (and INTEGRATION's old "routes not mounted /
> return 501" note) describes the pre-implementation state and is
> superseded here.

### What shipped

- **`pkg/protocol/saml` IdP surface** — a hand-rolled, DB-backed IdP
  (design D1: `crewjam/saml` + `goxmldsig` as crypto/marshaling
  primitives only; we own the handlers and drive everything from the
  `saml_sp*` schema + the existing session store, mirroring the v0.4
  OIDC philosophy).
  - **IdP metadata** (`metadata.go` `HandleMetadata`): renders the IdP
    `EntityDescriptor` — every non-retired signing cert as a
    `KeyDescriptor` (D7), both the SSO and SLO endpoints under
    HTTP-Redirect + HTTP-POST bindings, the persistent-1.1 NameIDFormat,
    and `WantAuthnRequestsSigned=true`.
  - **SP-initiated SSO** (`sso.go` `HandleSSO`, `authnreq.go`,
    `assertion.go`): parse + validate the inbound AuthnRequest
    (HTTP-Redirect binding), session-gate via the existing
    `LoadSession` middleware, then auto-POST a signed Response +
    Assertion to the SP's ACS. Both `<Response>` and `<Assertion>` are
    signed RSA-SHA256 / exclusive C14N (D4). `Destination` = chosen
    ACS, `SubjectConfirmationData Recipient` = ACS, `AudienceRestriction`
    = `saml_sp.entity_id` verbatim, `InResponseTo` echoed.
  - **IdP-local SLO** (`slo.go` `HandleSLO`): validate a signed
    LogoutRequest (HTTP-Redirect + POST parsing both supported), revoke
    the Prohibitorum session bound to the `saml_session`, delete the
    `saml_session` rows, return a signed LogoutResponse. This is
    **IdP-LOCAL** logout (D2) — it revokes the bound Prohibitorum
    session only; there is NO front-channel propagation to the user's
    other SPs.
  - **Stable opaque NameID** (`subjectid.go` `SubjectID`, D6): a 32-byte
    `crypto/rand` base64url value per `(account, sp)`, generated on
    first SSO into `saml_subject_id` and reused forever; default format
    `urn:oasis:names:tc:SAML:1.1:nameid-format:persistent` (the 1.1
    URI, for GHES re-link safety).
  - **GHES attribute profile** (`attributes.go`): the ordered JSONB
    `attribute_map` projects an `account` to `USERNAME` (basic),
    `administrator` (basic; literal name, emitted only as `"true"` when
    `role=='admin'` or `attributes.administrator` is truthy), `emails`
    (basic, multi), `public_keys` (`Name="urn:oid:1.2.840.113549.1.1.1"`,
    URI NameFormat, multi), and `gpg_keys` (basic, multi). Non-GHES SPs
    default to a minimal map.
  - **Hardened XML/DSig layer** (`xmlsec.go`): DTD/XXE-off parsing with
    duplicate-ID rejection, SHA-256-only verify (SHA-1 rejected on both
    signature and digest), XSW defense (the signature `Reference` must
    resolve to the processed element's own ID), exclusive-C14N config,
    and a 10 MB DEFLATE decompression bound.
- **Signing-key reuse (D7):** the SAME active `signing_key` RSA key +
  `x509_cert_pem` that signs OIDC ID/access tokens also signs SAML — no
  new key infra; the smoke's step-70 key is reused at step 91.
- **Issuer/EntityID (D8):** the IdP `entityID` = `configx`
  `PublicOrigins[0]` (same source as the OIDC issuer); endpoint URLs are
  `…/saml/metadata`, `…/saml/sso`, `…/saml/slo`.
- **CLI (`cmd/prohibitorum`):**
  - `saml-sp create` — registers an SP. `--metadata-file <path>` /
    `--metadata-url <url>` ingests the SP's SAML metadata
    (auto-populating `entity_id`, ACS endpoints, signing certs), or set
    `--entity-id` + `--acs-url` manually. `--kind ghes` installs the
    GHES attribute profile and FORCES `require_signed_authn_request=true`.
    Explicit flags override metadata-parsed values.
  - `saml-sp list` — lists registered SPs.

### Endpoints introduced in v0.5

All routes are root-mounted (not under the `/api/prohibitorum` prefix),
because SAML SPs expect the IdP at the issuer root.

| Method | Path | Notes |
|---|---|---|
| GET | `/saml/metadata` | IdP `EntityDescriptor` (all non-retired signing certs, SSO/SLO bindings, persistent-1.1 NameIDFormat, `WantAuthnRequestsSigned=true`) |
| GET / POST | `/saml/sso` | SP-initiated SSO; parse+validate AuthnRequest, session-gate, signed Response+Assertion auto-POSTed to the SP ACS |
| GET / POST | `/saml/slo` | IdP-local SLO; validate signed LogoutRequest, revoke the bound session, signed LogoutResponse |

### Smoke-covered runtime paths

`cmd/smoke` extends to **99 steps**; the v0.5 block is steps 88–99
against a real Postgres + dev server + an in-process mock SP. Each entry
references the smoke step counter in `cmd/smoke/main.go`:

- **88** — re-login via WebAuthn (the smoke account's session was
  revoked by `/oidc/logout` at v0.4 step 84; SAML needs a live session).
- **89** — `GET /saml/metadata` → `EntityDescriptor` with ≥1 signing
  `KeyDescriptor` (the reused `signing_key` cert).
- **90** — `saml-sp create --kind ghes --metadata-file <mock SP
  metadata>` registers the mock SP (ingests entity_id + ACS + cert;
  forces `require_signed_authn_request`).
- **91** — a signed (HTTP-Redirect-binding) AuthnRequest → `GET
  /saml/sso` with a live session → the auto-POSTed `SAMLResponse` is
  parsed back with **crewjam/saml's SP-side `ParseXMLResponse`** against
  our `/saml/metadata`: signature, `Destination`, `Recipient`,
  `Audience`, and the GHES `USERNAME` attribute all verify.
- **92** — a SECOND SSO (same account + SP) yields an **identical
  NameID** (stability).
- **93** — DB assert: exactly 1 `saml_subject_id` row with a stable
  `name_id`, and ≥2 `saml_session` rows (one per SSO).
- **94** — drive a DEDICATED second session's SSO, then sign a
  LogoutRequest targeting THAT session's `saml_session`.
- **95** — the signed (HTTP-Redirect-binding) LogoutRequest → `/saml/slo`
  → a signed LogoutResponse comes back via redirect, and the bound
  session is revoked while the OTHER session survives.
- **96** — negative: an UNSIGNED AuthnRequest to the
  `require_signed_authn_request` GHES SP → rejected.
- **97** — negative: an AuthnRequest with a bad / unregistered ACS URL →
  rejected (open-redirect guard).
- **98** — negative: a replayed AuthnRequest ID (same request twice) →
  the 2nd is rejected (single-use replay cache).
- **99** — DB assert: `credential_event` (factor `saml_sp`) covers
  `use` for SSO and `session_end` for SLO.

### Smoke-untested runtime paths (acknowledged)

The following v0.5-touched paths are wired and unit-tested (per-file
`*_test.go` in `pkg/protocol/saml`) but not exercised end-to-end by
`cmd/smoke`. The smoke drives the HTTP-Redirect binding with a single
GHES SP and an authenticated session, so these branches do not fire:

- **IdP-initiated SSO** — NOT implemented (only SP-initiated; D2).
- **Front-channel SLO propagation to OTHER SPs** — NOT implemented; SLO
  is IdP-LOCAL only (revokes the one Prohibitorum session bound to the
  `saml_session`; D2).
- **Assertion / NameID encryption** — NOT implemented. The
  `saml_sp_key.use='encryption'` column exists but is unused (GHES does
  not require it; D2).
- **`ForceAuthn`** — ignored (D3): an existing valid session satisfies
  the AuthnRequest (parity with v0.4's OIDC `prompt=login` deferral).
- **`IsPassive`** — IS honored: no-session + `IsPassive` → a SAML
  `NoPassive` status Response. Unit-tested in `sso_test.go`; the smoke
  does not drive the `IsPassive` path.
- **POST-binding AuthnRequest + POST-binding LogoutRequest** —
  parsing/verify implemented and unit-tested; the smoke exercises the
  HTTP-Redirect binding for both SSO and SLO (the SLO LogoutResponse
  returns via redirect). The no-stored-SLO-endpoint case falls back to a
  200 `text/xml` LogoutResponse (unit-tested only).
- **SLO LogoutResponse signature** — verified by `slo_test.go` (unit),
  not re-verified by the smoke (the smoke asserts the redirect + the
  session-revocation side effect).
- **No-session SSO → 302 to `Issuer+/login?return_to=<SSO URL>`** — the
  smoke holds a live session before hitting `/saml/sso`, so the
  login-bounce branch is unit-tested only.

### Notes

- **Spec decisions D1–D9** (`docs/superpowers/specs/2026-05-30-v0.5-saml-idp-design.md`):
  - **D1 — Hybrid library.** `crewjam/saml` + `goxmldsig` as primitives
    only; hand-rolled, DB-backed handlers; `samlidp.Server` NOT adopted.
  - **D2 — Scope.** SP-initiated SSO + IdP-local SLO + metadata + CLI.
    Out (deferred): IdP-initiated SSO, front-channel multi-SP SLO,
    assertion encryption, AttributeQuery / NameIDMapping / Artifact.
  - **D3 — `ForceAuthn` deferred, `IsPassive` honored minimally.** A
    valid session satisfies the request; `IsPassive` + no session → a
    `NoPassive` error Response (not a login-UI bounce).
  - **D4 — Sign BOTH `<Response>` and `<Assertion>`** (RSA-SHA256 +
    exclusive C14N) — the safe superset of GHES's requirement.
  - **D5 — Verify SP signatures against the registered cert only**
    (`saml_sp_key`), never a cert embedded in the incoming message
    (correct trust-anchoring + sidesteps crewjam/saml#384).
  - **D6 — Stable opaque NameID** per `(account, sp)`: 32-byte
    `crypto/rand` base64url generated on first SSO, reused forever;
    default `…SAML:1.1:nameid-format:persistent`.
  - **D7 — Reuse the OIDC signing-key infra.** The same active
    `signing_key` RSA key + `x509_cert_pem` signs SAML; metadata
    publishes every non-retired signing cert (incl. grace-period).
  - **D8 — Issuer/EntityID = `PublicOrigins[0]`** (same source as the
    OIDC issuer): metadata `…/saml/metadata`, SSO `…/saml/sso`, SLO
    `…/saml/slo`.
  - **D9 — Per-identity rate limiting** (NOT per-IP; consistent with
    v0.3/v0.4): reuses `s.rateLimiter` with keys like
    `saml:sso:sp:<entity_id>` / `saml:sso:acct:<id>`.
- **Signing-key reuse.** The single active `signing_key` that signs OIDC
  ID/access tokens ALSO signs SAML Responses + Assertions — there is no
  separate SAML key. The smoke proves this by reusing the step-70 key at
  step 91.
- **AuthnRequest-replay deferral (implementation note).** The
  single-use replay marker for the AuthnRequest `ID` is written on the
  ISSUE path (after the session gate / before the Response is built),
  not at first parse — so the no-session login bounce can re-drive the
  same request after the user authenticates. Replayed IDs that reach the
  issue path twice are rejected (smoke step 98).

## v0.6 — protocol completeness (done)

Closed the deferred OIDC OP + SAML IdP behaviors tracked across the
v0.4/v0.5 audits, staying backend-only (no frontend). Four work areas:
cross-protocol forced re-authentication, OIDC PKCE-method policy +
introspection client-auth, SAML `NameIDPolicy/@Format` honoring, and SAML
IdP-initiated SSO (plus POST-binding AuthnRequest intake and signed
metadata). Handlers extend the existing `pkg/protocol/oidc` (Provider) and
`pkg/protocol/saml` (IdP) surfaces in place; the cross-protocol re-auth
gate lives in `pkg/authn` (`DemandReauth` / `ConsumeReauth`). Smoke
extended from 99 to **111 steps** (v0.6 block is steps 100–111), all green
end-to-end against live Postgres + a fresh dev server + the in-process
mock RP and mock SP. Final smoke tally: `45 (v0.2) + 46–69 (v0.3) +
70–87 (v0.4) + 88–99 (v0.5) + 100–111 (v0.6)`, `SMOKE_EXIT=0`.

> This section is the authoritative current state of the forced-re-auth,
> PKCE-policy, introspection-auth, and the v0.6 SAML behaviors. The v0.4
> AUDIT "Accepted / deferred" note that `prompt=login`/`max_age` are
> ignored, and the v0.5 notes that `ForceAuthn`/`NameIDPolicy`/IdP-initiated
> SSO/POST-binding/signed-metadata are deferred, are superseded here.

### What shipped

- **OIDC forced re-auth** (`pkg/protocol/oidc/authorize.go`): `/oauth/authorize`
  honors `prompt=login`, `max_age`, and `prompt=none`. The mechanism is a
  full fresh re-login plus a single-use KV nonce marker (`pkg/authn`
  `DemandReauth`/`ConsumeReauth`, key prefix `oidc:reauth:`): when the IdP
  demands re-auth it stamps `{demand_instant}` under the nonce, embeds the
  nonce in the `/login?return_to=…&reauth=<nonce>` bounce, and on return
  requires the marker to still exist AND `session.auth_time >= demand_instant`,
  then consumes it. A stale pre-existing session cannot satisfy
  `prompt=login` because its `auth_time` predates the demand. `prompt=none`
  + a re-auth demand returns `login_required` (no bounce); `prompt=login`
  combined with `prompt=none` is `invalid_request`. `max_age=0` always
  demands; a large `max_age` is satisfied by a sufficiently recent session.
- **OIDC PKCE method policy** (`pkg/protocol/oidc/authorize.go`):
  `/oauth/authorize` consults the per-client `require_pkce` (if true,
  `code_challenge` is mandatory) and `allowed_code_challenge_methods` (the
  requested method must be in the set). `plain` is forbidden ENTIRELY by a
  DB CHECK on `oidc_client.allowed_code_challenge_methods` (OAuth 2.1 / RFC
  9700: S256 mandatory) — a `code_challenge_method=plain` request →
  `invalid_request`.
- **OIDC public-client introspection rejected** (`pkg/protocol/oidc/introspect.go`):
  a public (`none`-auth) client calling `/oauth/introspect` → `invalid_client`
  (401) per RFC 7662 §2.1 (callers are resource servers; no public-client
  exemption). Confidential-client introspection still works; public clients
  may still `/oauth/revoke` their own tokens (RFC 7009 permits it). This is a
  **BEHAVIOR CHANGE** from v0.4, which allowed public-client introspection of
  their own tokens.
- **SAML ForceAuthn** (`pkg/protocol/saml/sso.go`): `ForceAuthn=true` triggers
  the same re-auth bounce + single-use nonce (key prefix `saml:reauth:`);
  after a fresh login the assertion is issued with an `AuthnInstant`
  reflecting the fresh `auth_time`. `ForceAuthn` + `IsPassive` (both set) →
  a `NoPassive` status Response, no assertion (IsPassive wins, OASIS SAML
  Core normative).
- **SAML NameIDPolicy/@Format** (`pkg/protocol/saml/sso.go`): a requested
  concrete `NameIDPolicy/@Format` that the IdP cannot produce (≠ the
  configured persistent format, ≠ `unspecified`, present + non-empty) →
  a status Response with `InvalidNameIDPolicy` and NO assertion;
  `unspecified` / absent / a matching format → a normal assertion.
- **SAML POST-binding AuthnRequest** (`pkg/protocol/saml/sso.go`):
  `POST /saml/sso` accepts an enveloped-signed AuthnRequest (form
  `SAMLRequest`, base64 with no inflate, verified against the registered
  `saml_sp_key` cert); the POST SSO binding is re-advertised in metadata
  (reversing the v0.5 audit-batch-D removal, now that the intake is real).
- **SAML signed metadata** (`pkg/protocol/saml/metadata.go`): the
  `/saml/metadata` `EntityDescriptor` is signed (verifies against its embedded
  signing cert) and carries `validUntil` + `cacheDuration`, both from
  `configx.SAML.MetadataValidity`. It fails OPEN to an unsigned descriptor if
  there is no active signing key (never 500s).
- **SAML IdP-initiated SSO** (`pkg/protocol/saml/sso_init.go`): a new
  `GET /saml/sso/init?sp=<entity_id>&RelayState=<deep-link>` app-launcher
  emits an UNSOLICITED Response (no `InResponseTo`) to the SP's DEFAULT ACS,
  gated by the per-SP `saml_sp.allow_idp_initiated` boolean (default false; a
  non-opted-in SP → 403). `RelayState` is passed through verbatim as the SP's
  deep-link target. Rate-limited per-account + per-SP; audited with
  `reason: "idp_initiated"`. The `saml-sp create --allow-idp-initiated` flag
  opts an SP in.

### Endpoints changed

| Method | Path | Change |
|---|---|---|
| GET | `/oauth/authorize` | honor `prompt=login`/`prompt=none`/`max_age` (D1–D5); consult per-client `require_pkce` + `allowed_code_challenge_methods` (D6) |
| POST | `/oauth/introspect` | reject public (`none`-auth) clients → `invalid_client` 401 (D7) — behavior change from v0.4 |
| GET / POST | `/saml/sso` | re-add POST-binding intake (D9); honor `ForceAuthn` (D1/D2/D5); honor `NameIDPolicy/@Format` (D8) |
| GET | `/saml/metadata` | sign the `EntityDescriptor` + `validUntil`/`cacheDuration` (D10); re-advertise the POST SSO binding (D9) |
| GET | `/saml/sso/init` | **new** — IdP-initiated SSO app-launcher (D11) |

Schema: `saml_sp.allow_idp_initiated boolean NOT NULL DEFAULT false`
(`db/migrations/005_saml.sql`, amended in place per the pre-deployment
squash convention); the `plain`-exclusion CHECK on
`oidc_client.allowed_code_challenge_methods` was already present in
`db/migrations/002_oidc.sql` (v0.4) and is now consulted by D6;
`configx.SAML.MetadataValidity` added (D10).

### Smoke-covered runtime paths

`cmd/smoke` extends to **111 steps**; the v0.6 block is steps 100–111
against a real Postgres + dev server + the in-process mock RP and mock SP.
Each entry references the smoke step counter in `cmd/smoke/main.go`:

- **100** — OIDC `prompt=login`: a stale session bounces to `/login` with a
  single-use `reauth` nonce and does NOT issue a code; a fresh login + the
  nonce → issues.
- **101** — OIDC `max_age`: `max_age=0` always bounces (even the
  just-minted session from step 100); a large `max_age=3600` issues a code.
- **102** — OIDC `prompt=none` + stale session → a redirect carrying
  `error=login_required` (no `/login` bounce).
- **103** — OIDC `code_challenge_method=plain` → redirect `error=invalid_request`
  (the method is excluded by the DB CHECK + the per-client policy).
- **104** — a public (`none`-auth) client minted via the PKCE-only code flow:
  `/oauth/introspect` → 401 `invalid_client`; a confidential client's
  introspection of its own token still returns `active:true`; the public
  client's `/oauth/revoke` of its own token → 200.
- **105** — SAML `ForceAuthn`: a stale session bounces (reauth nonce), a fresh
  login + the nonce → an assertion.
- **106** — SAML `ForceAuthn` + `IsPassive` → a `NoPassive` status Response,
  no assertion (IsPassive wins).
- **107** — SAML `NameIDPolicy` `Format=emailAddress` (≠ persistent) →
  `InvalidNameIDPolicy` status, no assertion.
- **108** — SAML POST-binding (enveloped-signed) AuthnRequest at `POST /saml/sso`
  → a valid assertion.
- **109** — `GET /saml/metadata` is SIGNED, verifies against its own embedded
  cert, and `validUntil` is in the future.
- **110** — SAML IdP-initiated SSO: `GET /saml/sso/init?sp=…&RelayState=deep`
  for the opted-in SP returns a 200 auto-POST unsolicited Response with
  `RelayState` echoed verbatim; the v0.5 SP without the `allow_idp_initiated`
  flag → 403.
- **111** — DB assert: `credential_event` (factor `saml_sp`) covers the v0.6
  SAML re-auth / IdP-initiated lifecycle, including a `reason=idp_initiated`
  record from step 110.

### Smoke-untested runtime paths (acknowledged)

Still out of scope (unchanged from v0.5 — never claimed for v0.6):

- **Assertion / NameID encryption** — NOT implemented; the
  `saml_sp_key.use='encryption'` column remains reserved + unused.
- **Front-channel multi-SP SLO propagation** — NOT implemented; SLO stays
  IdP-local.

Known issues recorded during v0.6 implementation (deferred; details in
AUDIT.md "Accepted / deferred"):

- **`require_pkce=false` + no `code_challenge` cannot complete token exchange**
  — a pre-existing v0.4 behavior surfaced during Task 3: `verifyPKCE` rejects
  an empty challenge, so a `require_pkce=false` client that sends NO PKCE gets
  `invalid_grant` at `/oauth/token`. Only affects non-default clients (the
  default is `require_pkce=true`). Deferred.
- **`sloParseError` omits `errBadSigAlg`** — a SLO POST LogoutRequest with a
  non-SHA256/non-SHA1 sig alg maps to 500 instead of 400 (the SSO path's
  `ssoParseError` was fixed to include it; SLO's was not). Cosmetic — still
  rejects. Deferred.
- **`ForceAuthn` + POST-binding AuthnRequest** — the re-auth bounce rebuilds
  `return_to` from the query string, but a POST-binding AuthnRequest body is
  not in the query, so after the login bounce the return GET has no
  `SAMLRequest` and fails safe with an error. A degenerate combination
  (POST-binding SPs rarely also set ForceAuthn). Deferred / documented
  limitation.
- **`oidc-client create --public` requires `--post-logout-redirect-uri`** —
  the public path passes `nil` post-logout URIs, violating the NOT NULL
  column; the workaround is to supply one (the smoke does exactly this at
  `cmd/smoke/main.go:4064` `createPublicOIDCClient`). Deferred CLI ergonomics
  fix.

### Notes

Spec decisions D1–D12
(`docs/superpowers/specs/2026-05-31-v0.6-protocol-completeness-design.md`):

- **D1 — Forced re-auth = full fresh re-login + per-request freshness gate.**
  `prompt=login`/`max_age`-exceeded (OIDC) and `ForceAuthn` (SAML) refuse to
  issue from the current session and bounce to the login flow.
- **D2 — Freshness gate via a single-use KV nonce.** The shared `reauthGate`
  (`pkg/authn` `DemandReauth`/`ConsumeReauth`) stamps `<proto>:reauth:<nonce>
  = <demand_instant>` (10m TTL), embeds the nonce in `return_to`, and on
  return requires `session.auth_time >= demand_instant` + marker present, then
  consumes it. A stale session's `auth_time` predates the demand, so it cannot
  pass.
- **D3 — `max_age` semantics.** `max_age=N` demands re-auth iff
  `now - auth_time > N`; `max_age=0` ≡ always demand; absent ≡ no demand. The
  issued `auth_time` reflects the (possibly refreshed) value.
- **D4 — `prompt=none` + demand → `login_required`** (no bounce);
  `prompt=login`+`none` → `invalid_request`.
- **D5 — SAML `ForceAuthn` + `IsPassive` → `NoPassive`** (IsPassive wins,
  OASIS normative); `ForceAuthn` + an existing session → the re-auth bounce.
- **D6 — OIDC PKCE method policy: S256-only by default.** `/authorize`
  consults `require_pkce` + `allowed_code_challenge_methods`; `plain` is kept
  out of the default set by a DB CHECK.
- **D7 — Introspection requires a confidential client.** Public (`none`-auth)
  clients may NOT introspect (RFC 7662); they may still revoke their own
  tokens (RFC 7009).
- **D8 — SAML `NameIDPolicy/@Format`: honor-if-producible.**
  `unspecified`/absent is free; a genuine mismatch → `InvalidNameIDPolicy`,
  no assertion.
- **D9 — SAML POST-binding AuthnRequest intake** re-added (enveloped
  signature); POST SSO binding re-advertised in metadata.
- **D10 — Signed IdP metadata + `validUntil`/`cacheDuration`** from
  `configx.SAML.MetadataValidity`; fails open to unsigned when no active key.
- **D11 — SAML IdP-initiated SSO with per-SP opt-in** (default false),
  delivered only to the registered default ACS (open-redirect guard),
  `RelayState` verbatim as deep-link, rate-limited per-account + per-SP. The
  inherently weaker login-CSRF posture (no `InResponseTo`, SAML Profiles
  §4.1.5) is the documented trade-off. Mirrors GHES's own default-reject
  posture.
- **D12 — No new auth UI.** The bounce targets the existing `/login?return_to=…`;
  the re-auth is the existing API ceremony. The smoke drives re-auth via the
  API ceremony then retries the protocol request, exactly as the v0.4/v0.5
  no-session paths.

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
