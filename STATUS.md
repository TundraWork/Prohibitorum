# Status — what's done, what's pending

Roadmap. v0.1 = the rescope + decoupling skeleton; v0.1.1 onward build on it.

## v0.1 (current commit) — rescope + decoupling

First commit (`3d79583`) lifted the identity layer from its origin project and
renamed the prefix to Prohibitorum, but kept domain permission vocabulary. This
commit rescopes to four upstream methods + two downstream protocols, strips that
vocabulary, and lands the schema / package layout / stubs for v0.2+ to build on
without further migrations.

### Done

- **Approach A three-layer package layout:**
  `pkg/{account, credential/{webauthn,password,totp,pairing,enrollment},
  federation/oidc, session, authn, protocol/{oidc,saml}, audit}`. Files `git mv`d
  to preserve blame. `pkg/auth/` and `pkg/oidc/` deleted.
- **Domain vocabulary stripped:**
  - `account.attributes jsonb` replaces five `can_*` boolean columns.
  - `enrollment.template_attributes jsonb` replaces five `template_can_*` columns; intent-check CHECK adapted.
  - `errorx` envelope renamed to project-agnostic `errorx.Error`.
  - `RPDisplayName` lifted to `configx.WebAuthn.RPDisplayName` (default `"Prohibitorum"`).
  - `contract.Permission` enum + `contract.Permissions` struct deleted; `AccountView` / `EnrollmentTemplate` carry `Attributes map[string]any`.
  - `auth.HasPermission` deleted; admin endpoints gate on `role = 'admin'`, anything finer is per-route attribute inspection.
- **Five migrations applied:**
  - `001_initial.sql` — account, session, webauthn_credential (`user_handle`, `cose_alg`, `uv_initialized`, `clone_warning_at`), enrollment (`template_attributes` + `expected_upstream_idp_slug`), credential_event, auth_throttle.
  - `002_oidc.sql` — `signing_key` (unified, `use sig|enc`, `not_before`); `oidc_client` extended per audit (`post_logout_redirect_uris`, `allowed_code_challenge_methods`, `token_endpoint_auth_method`, `id_token_signed_response_alg`, `subject_type`, `application_type`, `default_max_age`, `require_auth_time`, `contacts`, `logo_uri`, `tos_uri`, `policy_uri`, `disabled`); `revoked_jti`.
  - `003_password_totp.sql` — `password_credential`, `totp_credential` (`secret_enc` + `secret_nonce` + `key_version` + `last_step`), `recovery_code` (`used_session_id` + `used_ip`).
  - `004_federation.sql` — `upstream_idp` (encrypted `client_secret_enc` + `secret_nonce` + `key_version`, three provisioning modes), `account_identity` keyed `(upstream_iss, upstream_sub)`, forward FK `session.upstream_idp_id`.
  - `005_saml.sql` — `saml_sp` (ordered-array `attribute_map`, `require_signed_authn_request`, metadata-freshness fields, per-SP `session_lifetime`), `saml_sp_acs`, `saml_sp_key`, `saml_subject_id`, `saml_session`.
- **Stub packages** with TODO(v0.X) markers so the import graph is whole: `password`, `totp`, `federation/oidc`, `protocol/saml`, `authn/flow`, `audit/event`. `audit.Writer` wired into `server.New`.
- **`configx` extensions:** `OIDC`, `Federation`, `TOTP`, `SAML`, `PasswordHashParams`, `WebAuthn.RPDisplayName`, versioned `DataEncryptionKeys map[int][]byte` from `PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>`.
- **Doc rewrites:** `DESIGN.md`, `STATUS.md`, `AUDIT.md`, `INTEGRATION.md`, `README.md` aligned. Spec `docs/superpowers/specs/2026-05-24-multi-protocol-rescope-design.md` + three audit reports retain the original vocabulary as the audit trail.
- **Build state:** `go build ./...` clean, `go test ./...` passes, `mise run db migrate` applies all five against a fresh Postgres.

### Out of scope for this commit

- Any v0.2+ business logic (stubs only).
- Frontend (`dashboard/` empty; v0.6).
- Live smoke — see v0.1.1.

## v0.1.1 — smoke test (done)

Verified the skeleton via `cmd/smoke`, an in-process virtual-authenticator client
driving WebAuthn ceremonies without a browser. **17/17 steps + DB-state assertions
pass** against the live dev server: enrollment → /me → logout → login →
second-client login → revoke-by-session-id → add-second-passkey.

Three runtime bugs the smoke surfaced (all fixed same session):

- `webauthn_credential.cose_alg` always stored 0 — server read `cred.Attestation.PublicKeyAlgorithm`, a go-webauthn field declared but never assigned. Replaced with `pkg/credential/webauthn.COSEAlg(cred.PublicKey)`, decoding COSE_Key CBOR key 3 per RFC 8152 §7.1. ES256 = -7 now persists.
- PG `session` table never written — sessions stayed KV-only. Wired `SessionStore.Issue` → `db.InsertSession` (Revoke variants → `db.RevokeSession` / `db.RevokeAllSessionsByAccount`) so v0.4 OIDC carries `sid`/`auth_time`/`amr`/`acr`. WebAuthn issues `amr=["hwk"]`; v0.2 adds `pwd`/`otp`/`mfa`, v0.3 adds `federated`.
- `/.well-known/openid-configuration` `claims_supported` advertised the picotera `"permissions"` claim. Replaced with the spec-correct set: `auth_time`, `amr`, `acr`, `attributes` (+ standard `sub/iss/aud/exp/iat/nonce/username/displayName/role`).

`cmd/smoke` is committed as permanent v0.1.x tooling.

### Smoke-covered runtime paths

Verified against real Postgres + dev server (commit `a1ff8a6`):

- `handle_enrollment.go` `insertCredentialForTx` writes `cose_alg=-7` (step 4 + DB assert).
- `handle_me.go:201` `InsertCredential` add-second-passkey (steps 16–17 + DB assert).
- `SessionStore.Issue` writes a PG `session` row with `amr={hwk}` on enroll-complete (step 4) + login-complete (step 9) (DB assert: 3+ rows).
- `SessionStore.Revoke` (`/auth/logout`) stamps `revoked_at` (step 6 + DB assert: ≥2 revoked).
- `SessionStore.RevokeBySessionID` (`/me/sessions/revoke`) revokes a non-current session of the same account (steps 11–15: client B terminated by client A).
- `/.well-known/openid-configuration` `claims_supported` lists `attributes` (no `permissions` leak); manual curl confirmed.

### Smoke-untested runtime paths (acknowledged)

Wired but not exercised:

- `SessionStore.RevokeAllForAccount` (`/accounts/{id}/revoke-sessions`). Structurally identical to `RevokeBySessionID` + `RevokeAllSessionsByAccount`; needs a 2nd account + admin-impersonation. Deferred.
- `handle_pairing.go:152` (pairing session issuer, `amr=["hwk"]`). Multi-actor ceremony; same `amr` constant the smoke already verifies. Deferred.

`pkg/session/session_test.go` covers `Issue → InsertSession fails → KV rolled back` via `failingSessionQueries`.

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

Pre-smoke manual steps; most are now automated. Remaining manual checks:

- `go mod tidy` and lock the indirect dep graph; commit if `go.sum` changed.
- Apply all five migrations to a real Postgres; inspect schemas match the spec (`\d account`, `\d session`, `\d webauthn_credential`, etc.).
- Drive `POST /api/prohibitorum/enrollments/{token}/register/{begin,complete}` with an HTTP client. Full browser ceremony lands in v0.6; before then exercise via API + a virtual-authenticator Go integration test — see "WebAuthn smoke without a frontend".
- Hit `/.well-known/openid-configuration` and `/oauth/jwks`. **(Updated for v0.4.)** In the v0.1 skeleton discovery advertised planned endpoints and `/oauth/jwks` returned an empty `keys`. As of v0.4, `/oauth/{authorize,token,userinfo,introspect,revoke}` + `/oidc/logout` are mounted with real bodies, `/oauth/jwks` serves the active key(s), and discovery reflects the live surface. The v0.4 section is authoritative for the OP shape.

### WebAuthn smoke without a frontend

`dashboard/` empty (v0.6). Two options for v0.1.1:

1. **Go integration tests with a virtual authenticator** (recommended). Use `go-webauthn`'s test helpers (or a mock authenticator) to drive `register/begin` → `register/complete` server-side. Runs in CI; pins ceremony behavior.
2. **Defer the full ceremony test to v0.6**. Only run the server-side checks above (boot, migrations, discovery/JWKS shape, enrollment token preview). Carries silent-breakage risk if anyone touches `pkg/credential/webauthn` before v0.6.

### Operational notes for the smoke test

1. **`PROHIBITORUM_DATA_ENCRYPTION_KEY_V1` required at boot.** `configx.Parse()` hard-requires ≥1 `PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>` (32 bytes, base64) before returning. Nothing in v0.1 uses the DEK yet (TOTP + upstream-OIDC secrets, both v0.2/v0.3), but it's mandatory so deployments don't discover the requirement only at TOTP-enroll time. Generator:

   ```bash
   export PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(openssl rand -base64 32)"
   ```

   Multiple versions load simultaneously (`_V1`, `_V2`, …); the row's `key_version` selects the decrypt key. See the rescope spec §"AES-GCM at-rest encryption" for rotation.

2. **`mise.toml` pins `goose = "3.27.0"`, but mise's default registry doesn't ship `goose`.** `mise install` fails on a clean machine ("no plugin found"). Workarounds:
   - Aqua registry: `goose = { version = "3.27.0", source = "aqua:pressly/goose" }`. **Do not change `mise.toml` as part of v0.1.1** — the fix is a separate auditable commit.
   - Or `go install github.com/pressly/goose/v3/cmd/goose@latest` with `$GOPATH/bin` on `$PATH` (the workaround the smoke uses).

## v0.2 — password + TOTP (done)

Shipped the password + TOTP + recovery-code fallback and the sudo-step-up
extension gating sensitive `/me` operations behind a fresh credential proof. Smoke
17 → **45 steps + DB-state assertions**, all passing (commit `5ccf3fe`).

### What shipped

- **`pkg/credential/password`** — argon2id PHC at rest, OWASP defaults (`m=64 MiB`, `t=3`, `p=1`), auto re-hash on verify when `PasswordHashParams` advances. Package-init `dummyArgon2idHash` defeats step-1 enumeration (D3).
- **`pkg/credential/totp`** — RFC 6238 SHA-1 / 6-digit / 30s with ±1-step drift; `last_step` defeats same-step replay (§5.2). Secrets AES-256-GCM with versioned DEK; AAD bound to `'totp:'||account_id||':'||key_version`. Recovery codes (10/account, 80-bit, `XXXX-XXXX-XXXX-XXXX`, argon2id-hashed) minted at confirmation, regenerable via `/me/recovery-codes/regenerate`.
- **`pkg/authn/throttle`** — exponential backoff `[0,0,1s,2s,4s,8s,16s,32s,1m,2m,4m,8m,15m]` per `(account_id, factor)` (D2). Verify rejects `429` + `Retry-After` when `locked_until > now`, never running the crypto check on a locked row. Reset on success.
- **`pkg/audit/event`** — emits `credential_event` for `register`/`use`/`fail`/`revoke` across `password`/`totp`/`recovery_code`, plus `session:sudo_granted` per sudo completion.
- **Two-step login** — `POST /auth/password/begin` returns a single-use, 5-min, KV-backed `partial_session_token`; `POST /auth/totp/verify` consumes it and issues a session with `amr=["pwd","otp","mfa"]`. Disabled accounts rejected at `/begin` after a dummy verify (no timing oracle).
- **Recovery ceremony (2026-05-28 hardening; BREAKING to the v0.2 surface).** `/auth/recovery-code/verify` no longer issues a session — it consumes the recovery code + partial token and returns a narrow `recovery_session_token` (10-min TTL, separate KV namespace) redeemed at `/auth/recovery/totp/{begin,verify}`. `/begin` wipes the old TOTP row and starts a fresh enrollment (recovery codes preserved for retry). `/verify` atomically consumes the token, confirms the new TOTP, wipes old recovery codes, mints 10 fresh, issues a session `amr=["pwd","otp","mfa"]`. Rationale: NIST SP 800-63B-4 §5.2 cautions against knowledge factors for reauth — the old design let a stolen session + leaked recovery code escalate to takeover via sudo.
- **Sudo extension** — `flow.AvailableMethods` enumerates per-account sudo factors in priority order (`webauthn` → `password_totp`). `recovery_code` is **NOT** a sudo method. New `GET /me/sudo/methods`; `/me/sudo/{begin,complete}` accept a `method` discriminator (was WebAuthn-only in v0.1).
- **WebAuthn-preferred factor policy** — `POST /me/auth/revoke-password-totp` transactionally deletes the caller's `password_credential` + `totp_credential` + `recovery_code`. Sudo-gated.

### Endpoints introduced in v0.2

| Method | Path | Notes |
|---|---|---|
| POST | `/api/prohibitorum/auth/password/begin` | step 1 of two-step login |
| POST | `/api/prohibitorum/auth/totp/verify` | step 2: TOTP |
| POST | `/api/prohibitorum/auth/recovery-code/verify` | step 2 of recovery: returns `recovery_session_token` (no session) |
| POST | `/api/prohibitorum/auth/recovery/totp/begin` | recovery: re-enroll TOTP (recovery codes preserved) |
| POST | `/api/prohibitorum/auth/recovery/totp/verify` | recovery: confirm + mint 10 fresh + issue session |
| POST | `/api/prohibitorum/me/password/set` | sudo-gated |
| POST | `/api/prohibitorum/me/totp/begin` | sudo-gated iff confirmed TOTP exists |
| POST | `/api/prohibitorum/me/totp/verify` | confirms enrollment, returns recovery codes |
| POST | `/api/prohibitorum/me/recovery-codes/regenerate` | sudo-gated |
| POST | `/api/prohibitorum/me/auth/revoke-password-totp` | sudo-gated, destructive |
| GET  | `/api/prohibitorum/me/sudo/methods` | NEW in v0.2 |
| POST | `/api/prohibitorum/me/sudo/begin` | extended to accept `method` |
| POST | `/api/prohibitorum/me/sudo/complete` | extended to dispatch on `method` |

### Smoke-covered runtime paths

Every v0.2 endpoint end-to-end (steps from `cmd/smoke/main.go`):

- `/me/sudo/{begin,complete}` via WebAuthn — steps 18, 41.
- `/me/password/set` — step 19; DB assert `password_credential.hash` prefix `$argon2id$v=19$` (step 20).
- `/me/totp/begin` — step 21 (no sudo, first enroll); decodes `secret_base32`, captures `otpauth_uri`.
- `/me/totp/verify` — step 22; DB assert `confirmed_at IS NOT NULL` + 10 `recovery_code` rows (step 23).
- `/auth/password/begin` + `/auth/totp/verify` two-step login — steps 25–26, §5.2 replay window respected (`waitForNextTOTPStep`). `/me` round-trips (step 27).
- **Recovery ceremony** — `/auth/password/begin` (29) → `/auth/recovery-code/verify` returns `recovery_session_token` (30; no session cookie) → DB assert `recovery_code[0].used_at` (31) → `/auth/recovery/totp/begin` (32a) → DB assert TOTP unconfirmed + 9 codes preserved (32b) → `/auth/recovery/totp/verify` (32c) → DB assert TOTP confirmed + exactly 10 codes (32d) → `/me` round-trip (32e). Catches premature recovery-code wipe at `/begin`.
- `/me/sudo/{begin,complete}` via `password_totp` — step 37.
- `/me/recovery-codes/regenerate` — step 38 (consumes a sudo grant; 10 fresh, old invalidated).
- **Recovery-code sudo rejection** — `/me/sudo/methods` must NOT list `recovery_code` (39); `/me/sudo/begin {method:"recovery_code"}` → 400 `sudo_method_unavailable` (40).
- `/me/auth/revoke-password-totp` — step 42; DB assert all three tables empty (43); `/auth/password/begin` → 401 post-revoke (44).
- **Throttle observation** — step 34 drives wrong TOTP through `/me/sudo/{begin,complete}` until `429`; step 35 asserts `auth_throttle.failed_attempts >= 3` + `locked_until > now`. Step 36 is HARNESS-ONLY `DELETE FROM auth_throttle`.
- **Audit emission** — step 45 asserts `credential_event` covers: `password:{register,use,revoke}`, `totp:{register,use,revoke}`, `recovery_code:{register≥10,use≥1,revoke≥9}`, `session:sudo_granted≥3`. `recovery_code:use` dropped ≥2→≥1 post 2026-05-28 (sudo-via-recovery-code removed); `recovery_code:revoke` ≥9 for the `recovery_complete` chain; `totp:{register,revoke}` ≥2 (initial + recovery commit).

### Smoke-untested runtime paths (acknowledged)

Wired and unit-tested, not exercised end-to-end:

- **Disabled-account rejection at `/auth/password/begin`** (`handle_auth_password.go:70` runs dummy argon2id, returns `bad_credentials` when `Disabled`, D3). Smoke account never disabled mid-run.
- **`/me/sudo/methods`** — mounted + unit-tested; smoke calls `/me/sudo/begin` directly per method.
- **TOTP enrollment overwrite after a failed verify** (D4: 2nd `/me/totp/begin` UPSERTs fresh secret when `confirmed_at` NULL). Unit-tested; smoke confirms on first try.
- **`/me/totp/begin` sudo-gated re-enrollment** when a confirmed TOTP exists. Unit-tested; smoke enrolls once.
- **`PasswordHashParams` upgrade-on-verify re-hash.** Unit-tested; smoke runs one param set.
- **Throttle clearing on subsequent success** (DELETE-on-success). Unit-tested; smoke clears the row via `psql DELETE` (`resetThrottle`).
- **Partial-session-token replay** (single-use, 5-min). Unit-tested; smoke consumes each once.

`pkg/credential/{password,totp}`, `pkg/authn/{throttle,flow}`, `pkg/server/handle_*_test.go` carry the unit coverage.

### Notes

- `totp.ComputeCodeForTesting` is exported so `cmd/smoke` computes the current RFC 6238 code with the server's primitive. Only path exposing the secret post-encryption; never call from production.
- Smoke is **45** steps, not the drafted 46. The 46th (sudo before first-time `/me/totp/begin`) was redundant — first TOTP enroll isn't sudo-gated when no confirmed credential exists.

## v0.2.1 — open follow-ups

None identified. The v0.2-close reality audit (this section) is the canonical
follow-up list; reopen when concrete deferred items materialise.

## v0.3 — upstream OIDC federation (done)

Shipped the upstream OIDC RP surface: all three provisioning modes
(`auto_provision`, `link_only`, **`invite_only`**) end-to-end, `/me/identities`
list/unlink/link, AES-256-GCM at-rest for upstream client secrets, JWT alg
allowlist, RFC 9207 iss callback validation, federation-state KV with
cross-namespace defense, session-swap defense on link, AMR pass-through (with
`["federated"]` backfill). Smoke 45 → **69 steps** against real Postgres + dev
server + in-process mock OP (`cmd/smoke/mockop`).

`invite_only` ships as token-bearing redemption: an admin mints an invite via
`/admin/enrollments/*` with `intent='invite'` + `expected_upstream_idp_slug`. The
user clicks `GET /enrollments/{token}/start-federation`, which 302s to upstream
`/authorize`; the callback dispatches `applyInviteOnly` and atomically consumes
the enrollment + mints the account + inserts the identity in one pgx tx
(`modes.go` `runInviteTx`). Audit (`register` + `use`) emits via a tx-scoped
Writer so the `account_id` FK resolves against the just-inserted row — see "Bugs
found and fixed".

### What shipped

- **`secret.go`** — AES-256-GCM for `upstream_idp.client_secret_enc`, versioned DEK; AAD `'upstream_idp:'||id||':'||key_version`; 12-byte per-row nonce. 5/5 unit tests.
- **`client.go`** — wraps `zitadel/oidc/v3 v3.47.5`. Discovery once at `NewClient`; JWKS cached by the library. ID-token alg allowlist (`{RS256, ES256, EdDSA}`) enforced at the library AND re-checked post-decode (defense vs a library bug admitting `HS256`/`none`). Nonce via context-key.
- **`federation.go`** — `Federator` orchestrates `BeginLogin`/`HandleCallback`/`LinkBegin`/`LinkCallback`. State keyed `LoginKey(token)` vs `LinkKey(token)` (cross-namespace defense, unit-tested); single-use via `kvStore.Pop`. Payload snapshots `ExpectedIss` + `ExpectedTokenEndpoint` + `Nonce` + `CodeVerifier` so a discovery change mid-flow can't re-target the user. RFC 9207 `iss` validated vs `state.ExpectedIss`. Post-`Resolve` disabled-account check returns `authn.ErrBadCredentials()` (enumeration-safe, federation.go:269).
- **`modes.go`** — three modes:
  - `auto_provision` gated by `RequireVerifiedEmail` + `AllowedDomains` + `preferred_username` presence + local username-collision check. Mints a fresh `webauthn_user_handle` on JIT. Emits `register` + `use`.
  - `invite_only` — token-bearing redemption via `GET /enrollments/{token}/start-federation`. The HTTP shim (`handle_invite_federation.go`) validates upfront via `Federator.BeginInviteRedemption`, stashes the token in `FedState.EnrollmentToken`, 302s upstream. The callback dispatches `applyInviteOnly`; `runInviteTx` wraps `ConsumeEnrollment` + `InsertAccount` + `InsertAccountIdentity` + audit via a tx-scoped Writer. Skips `RequireVerifiedEmail` + `AllowedDomains` by design (D11): the admin-minted invite IS the authorization. Username collision re-checked at redemption. Audit `reason: "invite_only_redemption"` on success; `invite_consumed_or_expired`/`invite_slug_mismatch`/`username_collision` on in-tx fails; `invite_lookup_failed`/`invite_wrong_intent`/`invite_already_consumed`/`invite_expired`/`invite_not_federated` at the `BeginInviteRedemption` pre-flight.
  - `link_only` — rejects unknown `(iss, sub)` with `link_required`.
  - Re-login claim sync (D2): updates `account.display_name` + `account_identity.upstream_email` on upstream drift, each conditional on a diff so the `updated_at` trigger doesn't fire on no-op logins (`modes.go:240–267`).
- **HTTP surface** — `handle_federation.go` (public login + callback) + `handle_me_identities.go` (sudo-gated link + unlink). No IP-keyed rate limit at the edge — audit fix M5 (`4c4412c`) removed all 13 IP buckets project-wide because `sessstore.ClientIP` is untrustworthy behind NAT/CDN. Per-account `auth_throttle` + PKCE + single-use KV state carry replay/brute-force defense; proxy/WAF owns edge DoS. Return-to validation rejects anything not a relative `/`-path (and not `//`) (`validateFederationReturnTo`). AMR backfilled `["federated"]` when upstream omits it (RFC 8176 §2, `handle_federation.go:113–119`).
- **`/me/identities` flow** — link begin is sudo-gated; link callback is NOT (the user just elevated at `/begin`; a fresh prompt after the round-trip is hostile UX). `LinkCallback` validates the current session matches `state.LinkingAccountID` — defeats a session-swap mid-flow (`federation.go:307–312`, unit-tested).
- **Unlink last-method check** — `handleMeIdentitiesUnlinkHTTP` rejects `last_sign_in_method` when the only remaining method is the row being unlinked (`handle_me_identities.go:121–145`).
- **`pkg/authn` errors** — 8 new: 403 `email_not_verified`/`username_collision`/`invite_required`/`link_required`; 401 `federation_state_invalid`; 400 `last_sign_in_method`/`invalid_return_to`/`upstream_error{code, description}` (`errors.go:274–339`).
- **`AvailableMethods`** — appends `MethodFederationOIDC` when the account has ≥1 `account_identity` (`flow.go:75–81`).
- **Migration 006** — `006_federation_v03.sql` added `upstream_idp.require_verified_email BOOLEAN NOT NULL DEFAULT true`. The rest of the federation schema was already in migration 004.

### Endpoints introduced in v0.3

| Method | Path | Notes |
|---|---|---|
| GET | `/api/prohibitorum/auth/federation/{slug}/login` | public; 302 to upstream `/authorize` |
| GET | `/api/prohibitorum/auth/federation/{slug}/callback` | public; handles `?error=`; issues session |
| GET | `/api/prohibitorum/enrollments/{token}/start-federation` | public (token-bearing); validates invite, 302 upstream; `Referrer-Policy: no-referrer` so the token doesn't leak upstream |
| GET | `/api/prohibitorum/me/identities` | session; `[{id, idpSlug, idpDisplayName, upstreamEmail, linkedAt}]` |
| POST | `/api/prohibitorum/me/identities/{id}/unlink` | session + sudo; 204; refuses last sign-in method |
| GET | `/api/prohibitorum/me/identities/link/{slug}/begin` | session + sudo; 302 upstream (LinkKey state) |
| GET | `/api/prohibitorum/me/identities/link/{slug}/callback` | session (not sudo); validates session==state.LinkingAccountID; issues no new session |

### Smoke-covered runtime paths

Steps 46–69; the mock OP (`cmd/smoke/mockop`) signs ES256 ID tokens against a JWKS in the test process:

- **46** — seed `mockop` (auto_provision, AES-GCM secret, allowed_domains `["example.com"]`, `require_verified_email`).
- **47–49** — `/auth/federation/mockop/login` → upstream `/authorize` → RP `/callback` → 302 `/me` + session. **50** round-trips `/me` as the federated user.
- **51** — DB assert `account_identity` for `ext-user-1` with matching `(upstream_iss, upstream_sub)`.
- **52** — re-login claim sync (D2): change upstream display name, re-login, DB-assert `account.display_name` updated.
- **53** — email_not_verified: set `email_verified=false`, login → 403 `email_not_verified`.
- **54** — username_collision: change `preferred_username` to an existing local value → 403 `username_collision`.
- **55** — invalid_return_to: `return_to=https://evil.example.com` → 400 `invalid_return_to` (HTTP layer, no audit). The `//`-prefix branch is unit-tested only.
- **56** — upstream_error: OP returns `?error=access_denied` → 400 `upstream_error` + `fail` audit `reason: "upstream_error"`.
- **57** — `GET /me/identities` lists 1 row; asserts `idpSlug`/`idpDisplayName`/`upstreamEmail`/ISO-8601 `linkedAt`.
- **58** — seed `mockop-link` (link_only).
- **59** — link_only refuses unknown: fresh sub → 403 `link_required` + `fail` audit.
- **60** — self-service link: re-login as `smoke-admin` via WebAuthn. **61** sudo via WebAuthn, `/me/identities/link/mockop/begin`, follow mock OP, assert original session survives (no new session).
- **62** — DB assert `account_identity` for `admin-link-1` owned by `smoke-admin`.
- **63** — `/me/identities` returns exactly 1 row for `smoke-admin` post-link.
- **64** — unlink: sudo via WebAuthn, `/me/identities/{id}/unlink` → 204 + row gone. smoke-admin survives (still has WebAuthn).
- **65** — invite_only e2e: seed `intent='invite'` + `expected_upstream_idp_slug='mockop'`, drive `start-federation?return_to=/me` → upstream → RP `/callback` → 302 `/me` + session. `/me` returns the template `username`/`displayName` (NOT upstream claims, proving template override).
- **66** — invite_only DB asserts: `enrollment.consumed_at IS NOT NULL`, account row with template username, `account_identity` linked, `credential_event` `factor='federation_oidc' event='register' detail->>'reason'='invite_only_redemption'`.
- **67** — consumed-token rejection: reuse redeemed token → `BeginInviteRedemption` 403 `invite_required` (no upstream hop).
- **68** — expired-token rejection: `expires_at = now() - interval '1 second'` → 403 `invite_required`.
- **69** — audit lower bounds: `federation_oidc:register ≥ 2`, `use ≥ 4`, `fail ≥ 6`, `link ≥ 1`, `unlink ≥ 1`. Observed: `register:2, use:4, fail:6, link:1, unlink:1`.

### Smoke-untested runtime paths (acknowledged)

Wired; mostly unit-tested in `pkg/federation/oidc/*_test.go` or `handle_me_identities_test.go`:

- **invite_only username_collision at redemption** — `applyInviteOnly` re-checks (race against a concurrent invite/sign-up). Unit-tested (`modes_test.go`); race not stageable.
- **invite_only transactional rollback** — `runInviteTx` rolls back consume + insert + audit on any in-tx fail. Unit-tested via fake querier; smoke can't stage a mid-tx failure.
- **invite_only `expected_upstream_idp_slug` mismatch** — slug-A invite through slug-B callback → `invite_slug_mismatch`. Unit-tested; the `start-federation` handler binds by reading the column, so reaching the branch needs DB tampering.
- **`iss_mismatch_callback` (RFC 9207 reject)** — Unit-tested (`federation_test.go`); single mock OP, can't stage a mismatch.
- **Cross-namespace state reuse** — Login token can't redeem via link callback (and vice versa). Unit-tested; smoke never swaps.
- **Session-swap defense on LinkCallback** — `state.LinkingAccountID` must match session. Unit-tested; smoke flows under the same session.
- **`code_exchange_failed` from upstream** — mock OP always honors the code. Unit-tested (stubbed exchange).
- **Disabled-account check post-Resolve** — `ErrBadCredentials()` if disabled between provision and mint (`federation.go:269–278`). Unit-tested; smoke account never disabled mid-flow.
- **Unlink-last for a federated-only user** — `last_sign_in_method` reject. Unit-tested; smoke can't drive (unlink is sudo-gated, a federated-only user has no sudo method — `recovery_code` de-listed in v0.2, federation isn't a sudo method).
- **Upstream refresh tokens** — NOT implemented. Federated accounts re-authenticate via `/login`; no upstream token storage/refresh. ❌ in AUDIT.md.
- **HS256/`none` rejection by the alg allowlist** — mock OP only signs ES256, so the post-decode recheck branch is `t.Skip`ed (`client_test.go`). The library-level allowlist still rejects via config.
- **Per-IdP claim-name overrides (smoke-untested, ✅ implemented)** — `upstream_idp.{username_claim,display_name_claim,email_claim}` honored e2e (commit `45083bc`, M4): auto-provision via `ClaimString(...)` (`modes.go:133–135`), re-login drift sync (`:518–519`), link-flow email (`federation.go:453`), invite redemption (`modes.go:383`). Unit-tested; smoke's mock OP only ships standard claims, so the override branch is unit-only (schema defaults match OIDC standard names, so the typical path is exercised).

### Notes

- The mock OP (`cmd/smoke/mockop`) is a deliberately minimal in-process OP (discovery, JWKS, `/authorize`, `/token` with PKCE, ES256). Drives an upstream round-trip without Keycloak; never reuse outside the smoke.
- Federation handlers carry no IP-keyed rate limits — M5 (`4c4412c`) removed all 13 buckets. Remaining: per-account `auth_throttle`, account/session-keyed buckets for pairing + sudo, PKCE + single-use KV state. Edge DoS is the proxy/WAF's job. The multi-replica caveat (`2026-05-28-v0.2-deployment-notes.md` §1) still applies to the account/session-keyed buckets.

### Hardening fixes since initial v0.3 ship

Five audit-driven defenses on top of the initial v0.3 smoke pass, each smoke- or unit-backed:

- **M4 — per-IdP claim-name overrides honored** (`45083bc`). `modes.go:133–135,518–519,383` + `federation.go:453` route through `ClaimString`. Smoke uses defaults; overrides in `modes_test.go`.
- **M5 — IP-keyed rate limits removed project-wide** (`4c4412c`). `sessstore.ClientIP` can't reliably identify clients behind NAT/CDN (both false positives + false negatives demonstrated). Federation/enrollment/pairing/sudo/auth now rely on per-account/per-session keys + PKCE + KV single-use tokens.
- **C1 + H3-di + H4-di — `applyAutoProvision` wrapped in a tx with clean 23505 mapping** (`9ee15a4`). `runProvisionTx` shared by `applyInviteOnly` + `applyAutoProvision`. Concurrent same-username inserts + duplicate `(iss, sub)` surface as `ErrUsernameCollision`/`ErrInviteRequired` (the latter collapsed onto link_conflict anti-enumeration) not 500s; rollback drops the partial row. `modes.go:198–230` (auto), `:367–402` (invite).
- **M1-int — federation-bound invites reject the WebAuthn enrollment path** (`9ed0b1b`). `/enrollments/{token}/register/{begin,complete}` reject any invite with `expected_upstream_idp_slug` set → `ErrEnrollmentFederationRequired()`, forcing `start-federation`. Rejection at both `/begin` (`handle_enrollment.go:181–189`) + `/complete` (`:383–392`).
- **H3-sch — `ExpectedTokenEndpoint` snapshot validated at callback** (`4576a05`). `HandleCallback` (`federation.go:310–316`) + `LinkCallback` (`:420–426`) reject when live discovery's token_endpoint drifts from the BeginLogin snapshot; audited `reason=token_endpoint_drift`. Mix-up resistance per RFC 9700 §4.4.2.1.
- **M1-di — `DeleteAccountIdentity` returns rows-affected; handler 404s on no match** (`5cd1f07`). `account_identity.sql:15–19` DELETE → `:one RETURNING id`; handler (`handle_me_identities.go:177–192`) maps `pgx.ErrNoRows` → `ErrCredentialNotFound` (404, no audit), preventing audit pollution from no-op unlinks.

### Bugs found in stage 3 (invite_only smoke) and fixed

- **`applyInviteOnly` audit FK race against uncommitted account row.** First invite-redemption run had missing `credential_event` rows for `factor='federation_oidc' event='register' reason='invite_only_redemption'`. Cause: the audit `Writer` was the federator's outer pool-bound writer (`f.audit`), so `InsertCredentialEvent` ran on a different connection from the in-tx `InsertAccount`; the `account_id` FK checked the other connection's MVCC snapshot (didn't see the uncommitted row), the insert silently failed (Writer swallows errors), no audit row landed. Fix: `runInviteTx` constructs a tx-scoped `audit.NewWriter(qtx)`; the audit insert runs in the same tx as the account insert, so the FK resolves and rollback is correct. Smoke step 66/69 is the regression gate.

## v0.4 — downstream OIDC OP (done)

Shipped the first-party OpenID Connect Provider: full Authorization Code + PKCE,
RFC 9068 access tokens, OIDC Core ID tokens, refresh rotation with reuse-detection
+ family revocation, RFC 7662 introspection, RFC 7009 revocation, RP-Initiated
Logout 1.0 — plus the `signing-key` and `oidc-client` CLIs. All handlers are
`Provider` methods in `pkg/protocol/oidc`; routes root-mounted (NOT under
`/api/prohibitorum`) in `server.go:286–294`. Smoke 69 → **87 steps** (v0.4 block
70–87), green against live Postgres + dev server + an in-process mock RP.

### What shipped

- **`pkg/protocol/oidc` Provider** — hand-rolled chi-mounted handlers (D5); `go-jose/v4` for JWT only.
  - **Discovery** (`oidc.go` `HandleDiscovery`): `scopes_supported [openid, profile, offline_access]`; `introspection_endpoint`/`revocation_endpoint`/`end_session_endpoint`; `code_challenge_methods_supported [S256]`; `authorization_response_iss_parameter_supported: true`; `token_endpoint_auth_methods_supported [client_secret_basic, client_secret_post, none]`; `claims_supported [sub, iss, aud, exp, iat, nonce, auth_time, amr, acr, sid, at_hash, username, displayName, role, attributes]`; `id_token_signing_alg_values_supported [RS256]`; `Cache-Control: public, max-age=300`.
  - **JWKS** (`keys.go` + `HandleJWKS`): real key set from active + cached `signing_key` rows (RFC 7517 RSA JWK, RS256), replacing the v0.1 empty-array stub.
  - **`/oauth/authorize`** (`authorize.go`): Code + PKCE (S256-only); `redirect_uri` exact-match with open-redirect guard (invalid client / unregistered URI → direct error page, never a redirect to the unvalidated URI); session-gated; 302 to `redirect_uri?code=…&state=…&iss=…` (RFC 9207).
  - **`/oauth/token`** (`token.go` + `refresh.go`): `authorization_code` (client auth basic/post/none, PKCE verify, single-use code via `Pop`; replay → family revoke) + `refresh_token` (rotation + reuse-detection → family revoke + disabled-account re-check). Access token = RFC 9068 JWT (`typ:at+jwt`, `jti`, `iss/aud/sub/client_id/exp/iat/scope`); ID token = OIDC Core JWT with `at_hash`/`sid`/`auth_time`/`amr`; refresh issued only when `offline_access` granted.
  - **`/oauth/userinfo`** (`userinfo.go`, GET+POST): Bearer access-token verify (signature by `kid` + `iss` + `exp` + `typ:at+jwt` + `revoked_jti` denylist), scope-gated claim projection; 401 + `WWW-Authenticate: Bearer error="invalid_token"` on failure.
  - **`/oauth/introspect`** (`introspect.go`, RFC 7662): client-authenticated, per-client ownership; `{active:false}` with no detail leak otherwise.
  - **`/oauth/revoke`** (`revoke.go`, RFC 7009): client-authenticated, per-client ownership; access → `revoked_jti` PG denylist (self-pruning, TTL=exp), refresh → family revoke; always 200.
  - **`/oidc/logout`** (`logout.go`, RP-Initiated Logout 1.0): validates `id_token_hint` signature + `iss` (tolerates expiry), revokes the session named by `sid` (SSO sign-out), exact-match `post_logout_redirect_uri` (mismatch → direct error), 302 with `state`.
- **CLIs (`cmd/prohibitorum`):**
  - `signing-key generate [--activate] [--retire <kid>]` — mints RSA-2048 (RFC 7638 `kid`, JWK, self-signed x509, PKCS#8 PEM) into one `signing_key` row; first key / `--activate` becomes active (deactivating the prior in one tx); `--retire <kid>` → `decommissioning`.
  - `oidc-client create --client-id --display-name --redirect-uri(repeatable) [--post-logout-redirect-uri] [--scope] [--public] [--require-consent]` — registers a client; confidential (default) generates a 32-byte secret printed ONCE (only argon2id hash stored; `client_secret_basic`); `--public` → no secret, `none` auth, PKCE required. `oidc-client list` — client_id / display_name / auth_method / disabled.
- **Storage model (D8):** codes + refresh tokens in KV (codes single-use via `Pop` + replay used-marker; refresh opaque, rotated, per-family record for reuse detection + family revocation); access tokens stateless RFC 9068 JWTs revoked via `revoked_jti`; ID tokens stateless. `sub` = `account.oidc_subject` (uuid; D6). No new tables — refresh-family forensics table deferred.

### Endpoints introduced in v0.4

Root-mounted (not under `/api/prohibitorum`), because clients expect them at the issuer root.

| Method | Path | Notes |
|---|---|---|
| GET | `/.well-known/openid-configuration` | expanded discovery (was a v0.1 stub) |
| GET | `/oauth/jwks` | real RSA JWK set from active+cached keys (was empty in v0.1) |
| GET | `/oauth/authorize` | Code + PKCE (S256); session-gated; 302 with `code`+`state`+`iss` |
| POST | `/oauth/token` | `authorization_code` + `refresh_token`; client auth basic/post/none |
| GET / POST | `/oauth/userinfo` | Bearer verify; scope-gated claims |
| POST | `/oauth/introspect` | RFC 7662; client-authenticated; per-client ownership |
| POST | `/oauth/revoke` | RFC 7009; client-authenticated; per-client ownership; always 200 |
| GET | `/oidc/logout` | RP-Initiated Logout 1.0; `id_token_hint` + exact-match `post_logout_redirect_uri` |

### Smoke-covered runtime paths

Steps 70–87 against real Postgres + dev server + in-process mock RP:

- **70** — `signing-key generate` then `GET /oauth/jwks` returns exactly 1 key whose `kid` matches.
- **71** — `oidc-client create` (confidential; `openid+profile+offline_access`) returns a non-empty one-time secret.
- **72** — `GET /oauth/authorize` with PKCE S256 + live WebAuthn session → 302 carrying `code`+`state`+`iss`.
- **73** — `POST /oauth/token` `authorization_code` (Basic + PKCE verifier) → `Bearer`, `expires_in>0`, access+id+refresh. id_token verified vs JWKS (`iss`/`aud`/`sub`/`nonce`/`at_hash`/`sid`/`auth_time`/`amr`); access verifies with `typ:at+jwt` + `jti`; refresh present (offline_access granted).
- **74** — `GET /oauth/userinfo` (Bearer): `sub` matches id_token, `username` matches account, `displayName` present (profile scope).
- **75** — `POST /oauth/introspect` (Basic) on the access token → `active:true`, `token_type:access_token`, `client_id`, `sub` match.
- **76** — `POST /oauth/token` `refresh_token` → rotated refresh (new ≠ old) + re-issued id_token re-verifying vs JWKS.
- **77** — replay the OLD refresh → 400 `invalid_grant` (reuse detection).
- **78** — the step-77 reuse revoked the family: the current rotated refresh is now also dead → 400 `invalid_grant`.
- **79** — fresh authorize+token → `POST /oauth/revoke` refresh (200) → subsequent refresh → 400 `invalid_grant`.
- **80** — fresh authorize+token → `POST /oauth/revoke` access → introspect `active:false`; a `revoked_jti` row written.
- **81** — negative: unregistered `redirect_uri` at `/oauth/authorize` → direct 400 `invalid_request`, NO `Location` to the bad URI (open-redirect guard).
- **82** — negative: wrong PKCE verifier at `/oauth/token` → 400 `invalid_grant`.
- **83** — negative: wrong client secret → 401 `invalid_client`.
- **84** — `GET /oidc/logout` with `id_token_hint` + `post_logout_redirect_uri` + `state` → 302 to the URI, `state` echoed.
- **85** — the logout revoked the `sid` session: the client's `/me` now returns 401.
- **86** — DB assert: a `revoked_jti` row exists for the step-80 access token.
- **87** — DB assert: `credential_event` (factor `oidc_client`) covers `use:authorize ≥5`, `use:token_issued ≥3`, `use:refresh_rotated ≥1`, `use:logout ≥1`, `fail:refresh_reuse ≥1`, `revoke:revoked ≥2`.

### Smoke-untested runtime paths (acknowledged)

Wired + unit-tested (`*_test.go` in `pkg/protocol/oidc`); the smoke authenticates first and drives a single confidential client over Basic, so these don't fire:

- **No-session `/oauth/authorize` → 302 to `Issuer+/login?return_to=…`** (D7). Smoke holds a live session. Unit-only.
- **`prompt=none` + no session → `login_required`** (no redirect). Unit-only.
- **`require_consent=true` → `consent_required`** (D2). The column ships + is honored, but auto-approve is default and there's no consent UI yet. Smoke leaves `require_consent` at `false`.
- **JWKS with multiple keys / rotation + `--retire`, and `generate --activate` re-activation.** Smoke mints one key, never rotates.
- **Public client (`none`) full code flow, and `client_secret_post` at /token.** Smoke uses confidential + Basic only.
- **`oidc-client list`.** Smoke only calls `create`.

### Notes

- **D2 — Consent.** Auto-approve once a session exists; `require_consent` honored at `/authorize` (`consent_required`) with no UI until v0.6.
- **D3 — Rate limits keyed on identity, NOT IP.** `/authorize` per `account_id`; `/token`/`/introspect`/`/revoke` per `client_id`; `/userinfo` per `sub` (keys `oidc:token:client:<id>`, `oidc:authorize:acct:<id>`, `oidc:userinfo:sub:<sub>`). Satisfies the "rate limit /authorize + /token" goal AND respects v0.3 M5 (no per-IP buckets).
- **D5 — Hand-rolled handlers**; `go-jose/v4` for JWT only. `zitadel/oidc/v3 pkg/op` NOT adopted (would invert control over the bespoke session/sudo/audit model).
- **D6 — `sub` = `account.oidc_subject`** (uuid, `gen_random_uuid()` default). Pairwise `sub` salting deferred (only if RP correlation-resistance is needed).
- **D8 — Storage split.** Codes + refresh in KV; access + ID stateless JWTs; access revocation via `revoked_jti`. Refresh-family forensics table deferred (KV-only).
- **Audit factor is `oidc_client`** (not `oidc`): the spec drafted `oidc` but shipped handlers + step-87 use `oidc_client`.

## v0.5 — downstream SAML IdP (done)

Shipped the downstream **SAML 2.0 IdP** with a **GHES-compatible profile**:
SP-initiated SSO (HTTP-Redirect AuthnRequest in, HTTP-POST signed Response out),
IdP-local Single Logout, IdP `EntityDescriptor` metadata, a metadata-ingesting
`saml-sp` CLI, stable opaque persistent NameID, the GHES attribute profile, and an
XML-security hardening pass. Handlers are `IdP` methods in `pkg/protocol/saml`; 3
routes root-mounted in `server.go:320–324`. Smoke 87 → **99 steps** (block 88–99),
green against live Postgres + dev server + in-process mock SP. Final tally:
`45 (v0.2) + 46–69 (v0.3) + 70–87 (v0.4) + 88–99 (v0.5)`, `SMOKE_EXIT=0`.

> Authoritative current state of the SAML surface. The v0.1 skeleton text (and
> INTEGRATION's old "routes not mounted / 501" note) is superseded here.

### What shipped

- **`pkg/protocol/saml` IdP** — hand-rolled, DB-backed (D1: `crewjam/saml` + `goxmldsig` as primitives only; we own the handlers, driving from `saml_sp*` schema + the session store, mirroring v0.4).
  - **IdP metadata** (`metadata.go` `HandleMetadata`): every non-retired signing cert as a `KeyDescriptor` (D7), SSO + SLO endpoints under HTTP-Redirect + HTTP-POST, persistent-1.1 NameIDFormat, `WantAuthnRequestsSigned=true`.
  - **SP-initiated SSO** (`sso.go` `HandleSSO`, `authnreq.go`, `assertion.go`): parse + validate the inbound AuthnRequest (HTTP-Redirect), session-gate via `LoadSession`, auto-POST a signed Response + Assertion to the SP ACS. Both signed RSA-SHA256 / exclusive C14N (D4). `Destination` = chosen ACS, `Recipient` = ACS, `AudienceRestriction` = `saml_sp.entity_id` verbatim, `InResponseTo` echoed.
  - **IdP-local SLO** (`slo.go` `HandleSLO`): validate a signed LogoutRequest (Redirect + POST parse), revoke the bound Prohibitorum session, delete `saml_session` rows, return a signed LogoutResponse. **IdP-LOCAL** only (D2) — no front-channel propagation to other SPs.
  - **Stable opaque NameID** (`subjectid.go` `SubjectID`, D6): 32-byte `crypto/rand` base64url per `(account, sp)`, generated on first SSO into `saml_subject_id`, reused forever; default `urn:oasis:names:tc:SAML:1.1:nameid-format:persistent` (1.1 URI for GHES re-link safety).
  - **GHES attribute profile** (`attributes.go`): ordered JSONB `attribute_map` projects an account to `USERNAME` (basic), `administrator` (basic; `"true"` only when `role=='admin'` or `attributes.administrator` truthy), `emails` (basic, multi), `public_keys` (`Name="urn:oid:1.2.840.113549.1.1.1"`, URI NameFormat, multi), `gpg_keys` (basic, multi). Non-GHES SPs default to a minimal map.
  - **Hardened XML/DSig** (`xmlsec.go`): DTD/XXE-off parsing with duplicate-ID rejection, SHA-256-only verify (SHA-1 rejected on signature + digest), XSW defense (the signature `Reference` must resolve to the processed element's own ID), exclusive-C14N, 10 MB DEFLATE bound.
- **Signing-key reuse (D7):** the SAME active `signing_key` RSA key + `x509_cert_pem` that signs OIDC tokens signs SAML — no new key infra; the smoke's step-70 key is reused at step 91.
- **Issuer/EntityID (D8):** IdP `entityID` = `configx PublicOrigins[0]`; endpoints `…/saml/{metadata,sso,slo}`.
- **CLI (`cmd/prohibitorum`):**
  - `saml-sp create` — `--metadata-file <path>` / `--metadata-url <url>` ingests SP metadata (auto-populating `entity_id`, ACS, certs), or set `--entity-id` + `--acs-url` manually. `--kind ghes` installs the GHES profile + FORCES `require_signed_authn_request=true`. Explicit flags override parsed values.
  - `saml-sp list`.

### Endpoints introduced in v0.5

Root-mounted (SPs expect the IdP at the issuer root).

| Method | Path | Notes |
|---|---|---|
| GET | `/saml/metadata` | `EntityDescriptor` (all non-retired certs, SSO/SLO bindings, persistent-1.1 NameIDFormat, `WantAuthnRequestsSigned=true`) |
| GET / POST | `/saml/sso` | SP-initiated SSO; parse+validate AuthnRequest, session-gate, signed Response+Assertion auto-POSTed to ACS |
| GET / POST | `/saml/slo` | IdP-local SLO; validate signed LogoutRequest, revoke the bound session, signed LogoutResponse |

### Smoke-covered runtime paths

Steps 88–99 against real Postgres + dev server + in-process mock SP:

- **88** — re-login via WebAuthn (the account's session was revoked by `/oidc/logout` at v0.4 step 84).
- **89** — `GET /saml/metadata` → `EntityDescriptor` with ≥1 signing `KeyDescriptor` (reused `signing_key` cert).
- **90** — `saml-sp create --kind ghes --metadata-file <mock SP metadata>` (ingests entity_id + ACS + cert; forces `require_signed_authn_request`).
- **91** — a signed (Redirect) AuthnRequest → `GET /saml/sso` with a live session → the auto-POSTed `SAMLResponse` parsed back with **crewjam/saml's SP-side `ParseXMLResponse`** against our `/saml/metadata`: signature, `Destination`, `Recipient`, `Audience`, GHES `USERNAME` all verify.
- **92** — a SECOND SSO (same account+SP) yields an identical NameID (stability).
- **93** — DB assert: exactly 1 `saml_subject_id` row with stable `name_id`, ≥2 `saml_session` rows.
- **94** — drive a dedicated 2nd session's SSO, then sign a LogoutRequest targeting THAT session's `saml_session`.
- **95** — the signed (Redirect) LogoutRequest → `/saml/slo` → a signed LogoutResponse via redirect; the bound session revoked, the OTHER survives.
- **96** — negative: an UNSIGNED AuthnRequest to the GHES SP → rejected.
- **97** — negative: bad/unregistered ACS URL → rejected (open-redirect guard).
- **98** — negative: a replayed AuthnRequest ID → 2nd rejected (single-use replay cache).
- **99** — DB assert: `credential_event` (factor `saml_sp`) covers `use` for SSO + `session_end` for SLO.

### Smoke-untested runtime paths (acknowledged)

Wired + unit-tested (`*_test.go` in `pkg/protocol/saml`); the smoke drives the Redirect binding with a single GHES SP + an authenticated session:

- **IdP-initiated SSO** — NOT implemented (only SP-initiated; D2).
- **Front-channel SLO propagation to OTHER SPs** — NOT implemented; SLO is IdP-LOCAL only (D2).
- **Assertion / NameID encryption** — NOT implemented; `saml_sp_key.use='encryption'` exists but unused (GHES doesn't require it; D2).
- **`ForceAuthn`** — ignored (D3): an existing valid session satisfies the request (parity with v0.4's `prompt=login` deferral).
- **`IsPassive`** — IS honored: no-session + `IsPassive` → a `NoPassive` status Response. Unit-tested (`sso_test.go`); smoke doesn't drive it.
- **POST-binding AuthnRequest + LogoutRequest** — parse/verify implemented + unit-tested; smoke exercises Redirect. The no-stored-SLO-endpoint case falls back to a 200 `text/xml` LogoutResponse (unit-only).
- **SLO LogoutResponse signature** — verified by `slo_test.go` (unit); smoke asserts the redirect + revocation side effect.
- **No-session SSO → 302 to `Issuer+/login?return_to=<SSO URL>`** — smoke holds a live session. Unit-only.

### Notes

- **Spec decisions D1–D9** (`docs/superpowers/specs/2026-05-30-v0.5-saml-idp-design.md`):
  - **D1 — Hybrid library.** `crewjam/saml` + `goxmldsig` as primitives only; hand-rolled DB-backed handlers; `samlidp.Server` NOT adopted.
  - **D2 — Scope.** SP-initiated SSO + IdP-local SLO + metadata + CLI. Deferred: IdP-initiated SSO, front-channel multi-SP SLO, assertion encryption, AttributeQuery / NameIDMapping / Artifact.
  - **D3 — `ForceAuthn` deferred, `IsPassive` honored minimally.** A valid session satisfies; `IsPassive` + no session → `NoPassive` (not a login-UI bounce).
  - **D4 — Sign BOTH `<Response>` and `<Assertion>`** (RSA-SHA256 + exclusive C14N) — safe superset of GHES's requirement.
  - **D5 — Verify SP signatures against the registered cert only** (`saml_sp_key`), never a cert embedded in the message (correct trust-anchoring + sidesteps crewjam/saml#384).
  - **D6 — Stable opaque NameID** per `(account, sp)`: 32-byte `crypto/rand` base64url on first SSO, reused forever; default `…SAML:1.1:nameid-format:persistent`.
  - **D7 — Reuse the OIDC signing-key infra.** The same active `signing_key` RSA key + `x509_cert_pem` signs SAML; metadata publishes every non-retired cert (incl. grace-period).
  - **D8 — Issuer/EntityID = `PublicOrigins[0]`**: metadata `…/saml/metadata`, SSO `…/saml/sso`, SLO `…/saml/slo`.
  - **D9 — Per-identity rate limiting** (NOT per-IP; reuses `s.rateLimiter`, keys `saml:sso:sp:<entity_id>` / `saml:sso:acct:<id>`).
- **Signing-key reuse.** The single active `signing_key` that signs OIDC tokens ALSO signs SAML Responses + Assertions — no separate SAML key (smoke reuses the step-70 key at step 91).
- **AuthnRequest-replay deferral.** The single-use replay marker for the AuthnRequest `ID` is written on the ISSUE path (after the session gate, before the Response is built), not at first parse — so the no-session login bounce can re-drive the same request after auth. IDs reaching the issue path twice are rejected (step 98).

## v0.6 — protocol completeness (done)

Closed the deferred OIDC OP + SAML IdP behaviors from the v0.4/v0.5 audits,
backend-only. Four areas: cross-protocol forced re-authentication, OIDC
PKCE-method policy + introspection client-auth, SAML `NameIDPolicy/@Format`
honoring, and SAML IdP-initiated SSO (plus POST-binding AuthnRequest intake +
signed metadata). Handlers extend `pkg/protocol/{oidc,saml}` in place; the
re-auth gate lives in `pkg/authn` (`DemandReauth`/`ConsumeReauth`). Smoke 99 →
**111 steps** (block 100–111), green. Final tally: `45 (v0.2) + 46–69 (v0.3) +
70–87 (v0.4) + 88–99 (v0.5) + 100–111 (v0.6)`, `SMOKE_EXIT=0`.

> Authoritative current state of forced-re-auth, PKCE policy, introspection-auth,
> and the v0.6 SAML behaviors. The v0.4 AUDIT note that `prompt=login`/`max_age`
> are ignored, and the v0.5 notes that `ForceAuthn`/`NameIDPolicy`/IdP-initiated
> SSO/POST-binding/signed-metadata are deferred, are superseded here.

### What shipped

- **OIDC forced re-auth** (`authorize.go`): `/oauth/authorize` honors `prompt=login`, `max_age`, `prompt=none`. Mechanism = full fresh re-login + a single-use KV nonce (`DemandReauth`/`ConsumeReauth`, prefix `oidc:reauth:`): on demand it stamps `{demand_instant}` under the nonce, embeds it in `/login?return_to=…&reauth=<nonce>`, and on return requires the marker present AND `session.auth_time >= demand_instant`, then consumes it. A stale session can't satisfy `prompt=login` (its `auth_time` predates the demand). `prompt=none` + demand → `login_required` (no bounce); `prompt=login`+`prompt=none` → `invalid_request`. `max_age=0` always demands; a large `max_age` is satisfied by a recent session.
- **OIDC PKCE method policy** (`authorize.go`): consults per-client `require_pkce` (if true, `code_challenge` mandatory) + `allowed_code_challenge_methods` (requested method must be in the set). `plain` forbidden ENTIRELY by a DB CHECK on `oidc_client.allowed_code_challenge_methods` (OAuth 2.1 / RFC 9700) — `code_challenge_method=plain` → `invalid_request`.
- **OIDC public-client introspection rejected** (`introspect.go`): a public (`none`-auth) client at `/oauth/introspect` → `invalid_client` (401) per RFC 7662 §2.1. Confidential introspection still works; public clients may still `/oauth/revoke` their own tokens (RFC 7009). **BEHAVIOR CHANGE** from v0.4, which allowed public-client introspection of their own tokens.
- **SAML ForceAuthn** (`sso.go`): `ForceAuthn=true` triggers the same re-auth bounce + single-use nonce (prefix `saml:reauth:`); after a fresh login the assertion's `AuthnInstant` reflects the fresh `auth_time`. `ForceAuthn` + `IsPassive` → `NoPassive`, no assertion (IsPassive wins, OASIS normative).
- **SAML NameIDPolicy/@Format** (`sso.go`): a concrete requested `NameIDPolicy/@Format` the IdP can't produce (≠ persistent, ≠ `unspecified`, present + non-empty) → `InvalidNameIDPolicy` status, NO assertion; `unspecified`/absent/matching → a normal assertion.
- **SAML POST-binding AuthnRequest** (`sso.go`): `POST /saml/sso` accepts an enveloped-signed AuthnRequest (form `SAMLRequest`, base64 no inflate, verified vs the registered `saml_sp_key`); the POST SSO binding re-advertised in metadata (reversing the v0.5 removal).
- **SAML signed metadata** (`metadata.go`): the `EntityDescriptor` is signed + carries `validUntil` + `cacheDuration` from `configx.SAML.MetadataValidity`. Fails OPEN to unsigned if no active signing key (never 500s).
- **SAML IdP-initiated SSO** (`sso_init.go`): `GET /saml/sso/init?sp=<entity_id>&RelayState=<deep-link>` app-launcher emits an UNSOLICITED Response (no `InResponseTo`) to the SP's DEFAULT ACS, gated by per-SP `saml_sp.allow_idp_initiated` (default false; non-opted-in → 403). `RelayState` passed through verbatim. Rate-limited per-account + per-SP; audited `reason: "idp_initiated"`. `saml-sp create --allow-idp-initiated` opts an SP in.

### Endpoints changed

| Method | Path | Change |
|---|---|---|
| GET | `/oauth/authorize` | honor `prompt=login`/`prompt=none`/`max_age` (D1–D5); consult `require_pkce` + `allowed_code_challenge_methods` (D6) |
| POST | `/oauth/introspect` | reject public (`none`-auth) clients → `invalid_client` 401 (D7) — behavior change from v0.4 |
| GET / POST | `/saml/sso` | re-add POST-binding intake (D9); honor `ForceAuthn` (D1/D2/D5); honor `NameIDPolicy/@Format` (D8) |
| GET | `/saml/metadata` | sign the `EntityDescriptor` + `validUntil`/`cacheDuration` (D10); re-advertise POST SSO binding (D9) |
| GET | `/saml/sso/init` | **new** — IdP-initiated SSO app-launcher (D11) |

Schema: `saml_sp.allow_idp_initiated boolean NOT NULL DEFAULT false`
(`005_saml.sql`, amended in place per the pre-deployment squash convention); the
`plain`-exclusion CHECK on `oidc_client.allowed_code_challenge_methods` was
already in `002_oidc.sql` (v0.4) and is now consulted by D6;
`configx.SAML.MetadataValidity` added (D10).

### Smoke-covered runtime paths

Steps 100–111 against real Postgres + dev server + in-process mock RP and mock SP:

- **100** — OIDC `prompt=login`: a stale session bounces to `/login` with a single-use `reauth` nonce and issues NO code; fresh login + nonce → issues.
- **101** — OIDC `max_age`: `max_age=0` always bounces (even the just-minted session from step 100); `max_age=3600` issues a code.
- **102** — OIDC `prompt=none` + stale session → redirect `error=login_required` (no `/login` bounce).
- **103** — OIDC `code_challenge_method=plain` → redirect `error=invalid_request` (excluded by DB CHECK + per-client policy).
- **104** — a public (`none`) client via the PKCE-only code flow: `/oauth/introspect` → 401 `invalid_client`; a confidential client's own-token introspection still `active:true`; the public client's `/oauth/revoke` of its own token → 200.
- **105** — SAML `ForceAuthn`: a stale session bounces (reauth nonce), fresh login + nonce → assertion.
- **106** — SAML `ForceAuthn` + `IsPassive` → `NoPassive` status, no assertion.
- **107** — SAML `NameIDPolicy Format=emailAddress` (≠ persistent) → `InvalidNameIDPolicy`, no assertion.
- **108** — SAML POST-binding (enveloped-signed) AuthnRequest at `POST /saml/sso` → a valid assertion.
- **109** — `GET /saml/metadata` is SIGNED, verifies vs its own embedded cert, `validUntil` in the future.
- **110** — SAML IdP-initiated SSO: `GET /saml/sso/init?sp=…&RelayState=deep` for the opted-in SP → 200 auto-POST unsolicited Response with `RelayState` echoed verbatim; the v0.5 SP without `allow_idp_initiated` → 403.
- **111** — DB assert: `credential_event` (factor `saml_sp`) covers the v0.6 re-auth / IdP-initiated lifecycle, incl. a `reason=idp_initiated` record from step 110.

### Smoke-untested runtime paths (acknowledged)

Still out of scope (unchanged from v0.5):

- **Assertion / NameID encryption** — NOT implemented; `saml_sp_key.use='encryption'` reserved + unused.
- **Front-channel multi-SP SLO propagation** — NOT implemented; SLO stays IdP-local.

Known issues recorded during v0.6 (deferred; details in AUDIT.md "Accepted / deferred"):

- **`require_pkce=false` + no `code_challenge` cannot complete token exchange** — pre-existing v0.4 behavior: `verifyPKCE` rejects an empty challenge, so a `require_pkce=false` client sending NO PKCE gets `invalid_grant` at `/oauth/token`. Only affects non-default clients (default is `require_pkce=true`). Deferred.
- **`sloParseError` omits `errBadSigAlg`** — a SLO POST LogoutRequest with a non-SHA256/non-SHA1 sig alg maps to 500 instead of 400 (SSO's `ssoParseError` was fixed; SLO's wasn't). Cosmetic — still rejects. Deferred.
- **`ForceAuthn` + POST-binding AuthnRequest** — the bounce rebuilds `return_to` from the query string, but a POST-binding body isn't in the query, so the return GET has no `SAMLRequest` and fails safe with an error. Degenerate combination. Deferred / documented.
- **`oidc-client create --public` requires `--post-logout-redirect-uri`** — the public path passes `nil` post-logout URIs, violating NOT NULL; workaround is to supply one (smoke does this at `cmd/smoke/main.go:4064` `createPublicOIDCClient`). Deferred CLI ergonomics fix.

### Notes

Spec decisions D1–D12 (`docs/superpowers/specs/2026-05-31-v0.6-protocol-completeness-design.md`):

- **D1 — Forced re-auth = full fresh re-login + per-request freshness gate.** `prompt=login`/`max_age`-exceeded (OIDC) and `ForceAuthn` (SAML) refuse to issue from the current session and bounce to login.
- **D2 — Freshness gate via a single-use KV nonce.** `reauthGate` (`DemandReauth`/`ConsumeReauth`) stamps `<proto>:reauth:<nonce> = <demand_instant>` (10m TTL), embeds the nonce in `return_to`, on return requires `session.auth_time >= demand_instant` + marker present, then consumes. A stale session's `auth_time` predates the demand.
- **D3 — `max_age` semantics.** `max_age=N` demands iff `now - auth_time > N`; `max_age=0` ≡ always; absent ≡ never. The issued `auth_time` reflects the (possibly refreshed) value.
- **D4 — `prompt=none` + demand → `login_required`** (no bounce); `prompt=login`+`none` → `invalid_request`.
- **D5 — SAML `ForceAuthn` + `IsPassive` → `NoPassive`** (IsPassive wins, OASIS normative); `ForceAuthn` + an existing session → the re-auth bounce.
- **D6 — OIDC PKCE method policy: S256-only by default.** `/authorize` consults `require_pkce` + `allowed_code_challenge_methods`; `plain` kept out of the default set by a DB CHECK.
- **D7 — Introspection requires a confidential client.** Public (`none`) clients may NOT introspect (RFC 7662); may still revoke their own tokens (RFC 7009).
- **D8 — SAML `NameIDPolicy/@Format`: honor-if-producible.** `unspecified`/absent free; a genuine mismatch → `InvalidNameIDPolicy`, no assertion.
- **D9 — SAML POST-binding AuthnRequest intake** re-added (enveloped signature); POST SSO binding re-advertised in metadata.
- **D10 — Signed IdP metadata + `validUntil`/`cacheDuration`** from `configx.SAML.MetadataValidity`; fails open to unsigned when no active key.
- **D11 — SAML IdP-initiated SSO with per-SP opt-in** (default false), delivered only to the registered default ACS (open-redirect guard), `RelayState` verbatim, rate-limited per-account + per-SP. The weaker login-CSRF posture (no `InResponseTo`, SAML Profiles §4.1.5) is the documented trade-off, mirroring GHES's default-reject posture.
- **D12 — No new auth UI.** The bounce targets the existing `/login?return_to=…`; re-auth is the existing API ceremony. Smoke drives re-auth via the API then retries the protocol request, like the v0.4/v0.5 no-session paths.

## v0.6 — Frontend

- `dashboard/` scaffolded with pnpm + vite + Vue 3 + Tailwind v4.
- Passkey ceremony SDK (lifted from the origin project), `PasskeyPopupHost`, `SessionsCard`, `PairApproveDialog`, `PairingCode` / `PairingCodeInput`.
- `LoginView` with method selection (WebAuthn / password+TOTP / federation), `?return_to=` posting the user back to `/oauth/authorize` after sign-in.
- `EnrollView`, `MeView` (attributes + linked IdPs + passkeys + password/TOTP setup), `AccountsView`, `RecoverChoiceView`, `AdminRecoveryView`, `CodeLoginView`.
- New views: `ClientsView` (OIDC clients), `IdPsView` (upstream OIDC), `SPsView` (SAML).

## Admin Management API (done, smoke steps 114–121)

Full HTTP API for administering OIDC clients, SAML SPs, upstream IdPs, signing
keys, audit events, and account credentials. All handlers under
`/api/prohibitorum` (admin-role gated). Mutations are also fresh-sudo gated via
`registerSudoOpHTTP` (`pkg/server/operations.go`) — a single chokepoint enforcing
admin auth + fresh sudo + 64 KiB body limit + JSON content-type so the policy
can't drift per-handler. Smoke arc steps 114–121 (run last, admin session live),
`SMOKE_EXIT=0`.

### What shipped

- **OIDC client CRUD** — create (secret revealed once, argon2id hash stored; step 114), update (115), rotate-secret (new secret once; 116), delete. Reads never expose the hash/cleartext (unit: `TestAdminOIDCClients_ViewProjection_NeverExposesSecretHash`).
- **SAML SP CRUD** — create (optional metadata XML ingestion), update, reingest-metadata, delete.
- **Upstream IdP CRUD** — create (AES-GCM seal after insert; crash mid-create leaves a fail-closed row), update (excludes secret), rotate-secret, delete. Reads never expose encrypted bytes (unit: `TestAdminUpstreamIDPs_ViewProjection_NeverExposesSecretBytes`).
- **Signing-key lifecycle** — generate (→ `pending`; step 118), activate (demotes prior `active` → `decommissioning`, promotes target; 119), retire. `status` ∈ {pending, active, decommissioning, retired}, with a partial unique index `one_active_signing_key (use) WHERE status='active'`. Publish set for JWKS + SAML metadata = pending+active+decommissioning. Steps 117–119 verify generate→active+pending, activate→new-active+prior-decommissioning, prior-key tokens still verify during grace. Background reconcile loop (`Server.Serve`) advances decommissioning → retired once `retire_after` passes.
- **Audit-events viewer** — `GET /audit-events` with `factor`/`event`/`accountId`/`since`/`until` filters + keyset pagination. Every admin mutation writes a `credential_event` (factor ∈ oidc_client/saml_sp/upstream_idp/signing_key; event ∈ register/update/rotate/revoke; no secret/key material in `detail`). Step 120 asserts filtering + no secret bytes.
- **Account credentials admin view** — `GET /accounts/{id}/credentials` returns the passkey list with only the last-4 suffix of the credential ID. `POST /accounts/credentials/delete` force-revokes a passkey (sudo-gated). Step 121.
- **Route-policy test** — `TestAdminMutationRoutesRequireSudo` (`pkg/server/admin_route_policy_test.go`) serves the REAL `registerOperations()` table and asserts every 🔐 mutation returns `sudo_required` (401) without a grant. Security regression guard.
- **CLI parity** — `signing-key {generate,activate,retire}`, `oidc-client {update,rotate-secret,delete}`, `saml-sp {update,delete}`, `upstream-idp {create,list,update,rotate-secret,delete}` share the same domain path as the HTTP handlers.
- **API documentation** — `api.md` created with the full route table, gate notation, reveal-once semantics, signing-key lifecycle states, known caveats.

### Endpoints introduced in this phase

See `api.md` for the authoritative table. Summary:

| Method | Path | Gate |
|--------|------|------|
| GET | `/api/prohibitorum/oidc-applications` | 🔓 |
| GET | `/api/prohibitorum/oidc-applications/{clientId}` | 🔓 |
| POST | `/api/prohibitorum/oidc-applications` | 🔐 |
| PUT | `/api/prohibitorum/oidc-applications/{clientId}` | 🔐 |
| POST | `/api/prohibitorum/oidc-applications/rotate-secret` | 🔐 |
| POST | `/api/prohibitorum/oidc-applications/delete` | 🔐 |
| GET | `/api/prohibitorum/saml-applications` | 🔓 |
| GET | `/api/prohibitorum/saml-applications/{id}` | 🔓 |
| POST | `/api/prohibitorum/saml-applications` | 🔐 |
| PUT | `/api/prohibitorum/saml-applications/{id}` | 🔐 |
| POST | `/api/prohibitorum/saml-applications/{id}/reingest-metadata` | 🔐 |
| POST | `/api/prohibitorum/saml-applications/delete` | 🔐 |
| GET | `/api/prohibitorum/identity-providers` | 🔓 |
| GET | `/api/prohibitorum/identity-providers/{slug}` | 🔓 |
| POST | `/api/prohibitorum/identity-providers` | 🔐 |
| PUT | `/api/prohibitorum/identity-providers/{slug}` | 🔐 |
| POST | `/api/prohibitorum/identity-providers/rotate-secret` | 🔐 |
| POST | `/api/prohibitorum/identity-providers/delete` | 🔐 |
| GET | `/api/prohibitorum/signing-keys` | 🔓 |
| POST | `/api/prohibitorum/signing-keys/generate` | 🔐 |
| POST | `/api/prohibitorum/signing-keys/{kid}/activate` | 🔐 |
| POST | `/api/prohibitorum/signing-keys/{kid}/retire` | 🔐 |
| GET | `/api/prohibitorum/audit-events` | 🔓 |
| GET | `/api/prohibitorum/accounts/{id}/credentials` | 🔓 |
| POST | `/api/prohibitorum/accounts/credentials/delete` | 🔐 |

### Known caveats

- **Key-cache lag (multi-replica).** `Provider.InvalidateKeyCache()` runs on the mutating replica; others pick up a new/activated key within the 5-min cache TTL. Same family as the v0.2 in-process rate-limiter caveat (v0.2-deployment-notes.md §1). The reconcile loop (decommissioning→retired) also doesn't invalidate the cache — an already-non-signing key lingers in JWKS slightly past its `retire_after`, in the safe direction.
- **Upstream IdP crash mid-create.** Insert-then-seal-then-update: a crash between insert and seal leaves a placeholder secret that decrypts to a failure (fails closed). Best-effort cleanup only.
- **Migrations consolidated.** With no deployments yet, the incremental history was flattened into a single `001_initial.sql`, dropping the legacy `signing_key` columns (`active`/`not_before`/`retired_at`) and the plaintext `private_pem` — signing keys are sealed-only.

## v0.7 — RBAC app authorization (done)

A coarse per-app access gate plus first-class groups. An admin marks an OIDC client
/ SAML SP `access_restricted` and controls sign-in via **groups** and/or
**individual accounts**; exposed groups additionally flow downstream as an OIDC
`groups` claim / SAML `groups` attribute. No admin bypass. This evolves — does not
replace — the model: the IdP gates whether you may obtain a token/assertion at all;
the RP still gates in-app policy from claims.

### What shipped

- **Schema** (`015_rbac.sql`, folded into the consolidated set): `user_group`, `group_member`, `oidc_client_access`, `saml_sp_access` (a grant points at exactly one of group/account, enforced by `CHECK num_nonnulls(...) = 1` + partial unique indexes), and `access_restricted boolean NOT NULL DEFAULT false` on `oidc_client` + `saml_sp` (every existing app stays open). Applies cleanly on a populated DB.
- **Authorization predicate** — one sqlc query per protocol (`IsAccountAuthorizedFor{OIDCClient,SAMLSP}`): `NOT access_restricted OR direct grant OR via-group grant`.
- **Admin API** — groups CRUD + membership; per-app `set-restricted` / `grant` / `revoke` + a combined `GET …/access`; `accessRestricted` in app detail views. GETs 🔓, all mutations 🔐 (sudo) + audited (`group` factor; `access_granted`/`access_revoked`/`access_restricted_set`).
- **SPA** — `/admin/groups` list + detail (edit, exposed toggle, member mgmt), a reusable per-app **Access** card (restrict toggle + group/account grants) on both app detail pages, a group-membership card on account-detail.
- **Enforcement** — gate at OIDC `/authorize`, re-checked at the refresh-token grant (denial revokes the family → `invalid_grant`), and at SAML SSO (SP- + IdP-initiated). Denied interactive → IdP `/error?reason=app_access_denied`; OIDC `prompt=none` → `access_denied` to the RP; SAML passive → `RequestDenied`. Denials write an `access_denied` `credential_event`.
- **Group exposure** — two-level opt-in: `exposed_to_downstream` (default true) on the group AND a per-app ask — the OIDC `groups` scope (sorted claim in id_token + `/userinfo`, present-but-empty `[]`) or a SAML attribute-map `source: "groups"` entry (multi-valued, omitted when empty).
- **CLI** — `group create|list|update|delete|add-member|remove-member`; `access` subcommands on `oidc-client`/`saml-sp` (`--access-restricted`, grant/revoke).

### Endpoints introduced in v0.7

See `api.md` → *Groups (RBAC)* and *Per-app access (RBAC)*. Group CRUD +
membership under `/groups`, per-app access under
`/{oidc-applications,saml-applications}/{id}/access{,/set-restricted,/grant,/revoke}`,
and `GET /accounts/{id}/groups`. The `groups` scope is advertised in OIDC
discovery `scopes_supported`.

### Smoke-covered runtime paths

RBAC arc (rbac 1–7, `SMOKE_EXIT=0`): create an exposed group → add the admin as a
member → create a confidential client (`openid profile groups`) → mark it
restricted → authorize as a not-yet-granted user and assert a **302 to
`/error?reason=app_access_denied` with no code** → grant the group (via-group path)
→ authorize again and assert a **code**, then exchange it and assert the `groups`
claim (incl. the slug) in **both** the id_token and `/userinfo`.

### Notes / acknowledged gaps

- The DB-level predicate matrix is exercised by the smoke + protocol handler tests (fake-`Querier`), not a dedicated `pkg/db` integration test (no DB-test harness convention).
- The smoke covers the OIDC restrict→deny→grant→allow + groups-claim arc; the symmetric SAML arc is enforced + unit-tested (`sso_test.go` interactive `/error`, passive `RequestDenied`) but not in the smoke (would need a dedicated assertion verifier — scope creep).
- End-user app launchpad remains out of scope; the authorization predicate is the query it will reuse.

## Optional hardening & known gaps (unscheduled)

The full IdP shipped through v0.6. What remains is optional production hardening, a
compliance gap, and demand-driven features. None scheduled; pick up if a
deployment needs it.

- **HSM/KMS-backed signing.** Keys are DEK-sealed at rest; moving to AWS KMS / GCP KMS / Vault Transit (key never leaves the vault) would additionally defend a combined DB + environment compromise.
- **Password breach-list check.** NIST SP 800-63B-4 §5.1.1.2 SHALL gap for the password fallback (HIBP k-anonymity or a static blocklist).
- **Front-/back-channel logout.** OIDC + SAML sign-out are IdP-local only; coordinated multi-RP/SP sign-out is unbuilt.
- **Audit-log export / SIEM.** The append-only `credential_event` table is the only sink today.

Conditional on external demand (tracked in `AUDIT.md`, not planned): DPoP / PAR /
JAR / mTLS (low-trust clients), SAML assertion/NameID encryption (SP demand),
pairwise `sub`. Permanent non-goals: dynamic client registration and upstream
refresh-token storage (see `ARCHITECTURE.md` → Out of scope).

## Why ship the rescope as a skeleton

The schema and package boundaries are the load-bearing decisions; once committed,
v0.2+ work lands in focused PRs without disturbing them. Splitting "rescope" from
"v0.2 password+TOTP" is reviewable in isolation — the v0.1 commit is purely
structural, and the audit reports in `docs/superpowers/specs/` give the per-layer
authoritative checklist that v0.2+ ticks off.
