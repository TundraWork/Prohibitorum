# Audit — OAuth 2.1 / OIDC / WebAuthn / SAML / NIST best-practice checklist

Compliance of the current codebase against authoritative standards.
Status labels:

- **✅** — implemented end-to-end (code enforces the item today)
- **✅ schema** — DB column / table exists; no Go path reads/writes it yet
- **✅ design** — architectural decision locked in spec; no schema or code yet
- **✅ stub** — handler exists and is mounted; returns 501 or partial output
- **✅ planned** — target version named; tracked
- **⚠️ deferred** — intentional v0.x omission with a clear target version
- **❌ gap** — unfinished and needs work before v1.0
- **❌ explicitly forbidden** — the standard forbids this (NIST §3.1.1.2 etc.)

When a bare **✅** appears, read the Notes column: it may still be
schema-only. Suffix labels above qualify what's actually in v0.1.

The full spec-vs-design audits that drove the v0.1 schema decisions
live in:

- `docs/superpowers/specs/2026-05-24-audit-oidc.md` — OIDC OP + RP
  federation (8 critical / 7 recommended findings).
- `docs/superpowers/specs/2026-05-24-audit-credentials.md` —
  WebAuthn / Password / TOTP / Recovery codes (5 critical / 8
  recommended).
- `docs/superpowers/specs/2026-05-24-audit-saml.md` — SAML IdP +
  GHES interop (5 critical / 6 recommended + 10 GHES call-outs).

This file is the running checklist of "what we comply with right now."
Each item below is annotated with the audit-report ID (e.g. "credentials/C1")
when it traces to one of those reports.

---

## Post-implementation audit (2026-05-28)

After v0.2 shipped, a three-bundle security-audit fix sequence closed
the Critical, High, and Medium findings flagged by the standards/spec
audit pass:

- **Bundle 1 (Critical / High):** atomic recovery-code mint + audit-revoke
  on wipe (commit `bc1fb97`); atomic single-use tokens, TOTP race fix,
  throttle race fix, step-2 disabled-account check, revoke ordering, and
  enum-oracle close (commit `8f6b4fd`).
- **Bundle 2 (Medium):** spec/code mismatches and audit-doc anchoring
  (folded into the bundle-1 commits per the picotera-decoupling pattern).
- **Bundle 3 (Low + deployment notes — this commit):** `factor_locked`
  audit-event on throttle transitions; `ErrTOTPCorrupt` sentinel collapse
  on `/me/totp/verify`; PHC params lower-bound validation;
  `VerifyAgainstDummy` params-upgrade timing-variance doc; deployment
  notes covering the 5 known posture caveats.

The remaining items at the audit's Open-Question and Informational
tiers are documented as known caveats in
`docs/superpowers/notes/2026-05-28-v0.2-deployment-notes.md`:

- In-process rate limiter (multi-replica multiplier — operator
  mitigation via LB affinity or external WAF).
- AES-GCM DEK rotation budget (comfortably out of reach for any
  realistic deployment; a batch sweep tool is unscheduled).
- OIDC `auth_time` vs sudo semantics (resolved in v0.4: the ID token's
  `auth_time` is sourced from `session.auth_time` — the original
  authentication moment — not from the last sudo step-up; smoke step 73
  asserts `auth_time` is present on the id_token).
- Password breach-list check (NIST SHALL gap, deferred; viable
  approaches named).
- `auth_throttle` shared across login + sudo surfaces (intentional
  defense-in-depth; documented for operator visibility).

---

## WebAuthn (W3C Level 3)

| Item | Status | Notes / source |
|---|---|---|
| `ResidentKey=Required` (discoverable) | ✅ | `pkg/credential/webauthn` |
| `UserVerification=Required` at register, `Preferred` at login | ✅ | FIDO Alliance UV split |
| `AttestationPreference=PreferNoAttestation` | ✅ | No fingerprinting |
| `excludeCredentials` on add-passkey | ✅ | `handle_me.go` |
| Sign-count clone detection → `clone_warning_at` | ✅ | credentials/R8; `webauthn_credential.clone_warning_at` |
| `user_handle` persisted (L3 §4) | ✅ | credentials/R2; `webauthn_credential.user_handle` indexed |
| `cose_alg` persisted | ✅ | credentials/R1; `webauthn_credential.cose_alg`; extracted from the COSE_Key CBOR by `pkg/credential/webauthn.COSEAlg`; smoke-verified at both insert sites (`handle_enrollment.go:531` initial enrollment, `handle_me.go:201` add-second-passkey) |
| `uv_initialized` persisted (L3 §4) | ✅ | credentials/C5; `webauthn_credential.uv_initialized` |
| `backup_eligible` / `backup_state` persisted | ✅ | `webauthn_credential.backup_eligible/state` |
| Full attestation-object retention for MDS3 validation | ⚠️ deferred (unscheduled) | credentials/Optional |
| `created_via` provenance (registration / add / recovery) | ⚠️ deferred (v0.2) | credentials/Optional |

## Password (NIST SP 800-63B-4 draft)

| Item | Status | Notes / source |
|---|---|---|
| argon2id PHC string at rest (self-describing params) | ✅ smoke-verified | credentials/R5; `password_credential.hash` carries `$argon2id$v=19$…` (smoke step 19 set + step 20 DB assert) |
| Per-row salt embedded in PHC | ✅ smoke-verified | argon2id PHC format; salt visible in the stored hash from step 20 |
| `password_changed_at` distinct from `updated_at` | ✅ smoke-verified | credentials/R6; written by `handle_me_password.go` on every set (steps 19, indirectly via revoke at 42) |
| Configurable params (`PasswordHashParams`) with re-hash on verify | ✅ implemented; smoke-untested | configx defaults `m=65536KiB, t=3, p=1` (OWASP current); re-hash branch in `pkg/credential/password.Verify` is unit-tested; smoke runs one param set |
| Persistent failed-attempt counter (cross-restart) | ✅ smoke-verified | credentials/R4; `auth_throttle (account_id, factor='totp')` populated by wrong-code drive in step 34, asserted at step 35 |
| Verify endpoint with throttle enforcement | ✅ smoke-verified | `/auth/password/begin` + `/auth/totp/verify` (steps 25–26) and lockout observed via sudo path (steps 34–35); 429 + Retry-After confirmed |
| Username-enumeration defense (dummy argon2id verify on missing account) | ✅ implemented; smoke-untested; ✅ doc-anchored (Bundle 3) | spec D3; `pkg/credential/password.VerifyAgainstDummy` runs argon2id at the store's current params; unit-tested in `handle_auth_password_test.go`. Params-upgrade timing-variance caveat (Bundle-3 Low-2) is documented on the function itself — old rows take longer until next rehash; deployment notes §2 / §4 background |
| Disabled-account rejected at `/auth/password/begin` after dummy verify | ✅ implemented; smoke-untested | `handle_auth_password.go:70`; unit-tested; smoke account never disabled |
| Breach-corpus check (k-anonymity-style) on set | ⚠️ deferred (v0.2+) | NIST SP 800-63B-4 §5.1.1.2 SHALL gap; viable approaches (HIBP k-anonymity + static blocklist) documented in `docs/superpowers/notes/2026-05-28-v0.2-deployment-notes.md` §4 |
| Periodic rotation forced | ❌ explicitly forbidden | NIST §3.1.1.2 — do not add |
| Password history | ❌ explicitly forbidden | NIST §3.1.1.2 — do not add |
| Composition rules (uppercase / digit / symbol) | ❌ explicitly forbidden | NIST §3.1.1.2 — do not add |
| No email channel for reset; admin enrollment-token only | ✅ design | enrollment intent `reset` |

## TOTP (RFC 6238 / RFC 4226)

| Item | Status | Notes / source |
|---|---|---|
| Secret entropy ≥ 160 bits | ✅ smoke-verified | `pkg/credential/totp` generates 160-bit secret; smoke decodes the base32 secret returned by `/me/totp/begin` and computes a valid code (steps 21–22) |
| AES-256-GCM at rest | ✅ smoke-verified; ✅ audit-hardened (Bundle 3) | credentials/C3+C4; `secret_enc` + `secret_nonce` populated on enrollment (step 21); decrypts on verify (step 22). Decrypt failure collapses to `ErrTOTPCorrupt` (Bundle-3 Crypto-6) so `/me/totp/verify` does not leak AES-GCM authentication-failure detail to clients; server-side `credential_event` keeps `event=fail, detail.reason=decrypt_failed` for forensics |
| Versioned DEK (`key_version` per row) | ✅ smoke-verified | credentials/C3; `totp_credential.key_version` written to 1 by `/me/totp/begin`; ciphertext readable on subsequent verifies (steps 22, 26, 37) |
| AAD bound to row identity (`'totp:'||account_id||':'||key_version`) | ✅ smoke-verified | credentials/C4; the verify path at step 22 would fail GCM auth if the AAD weren't constructed identically on encrypt and decrypt |
| Per-row nonce (12 bytes from `crypto/rand`) | ✅ smoke-verified | `totp_credential.secret_nonce`; written on enrollment, consumed on verify |
| 30-second period, 6 digits | ✅ smoke-verified | `waitForNextTOTPStep` and the working RFC 6238 verify at steps 22, 26, 37 confirm period and digit count |
| SHA1 default (Google Authenticator interop) | ✅ smoke-verified | credentials/R3; smoke's HMAC-SHA1-based `ComputeCodeForTesting` produces codes the server accepts |
| ±1 period drift tolerance | ✅ implemented; smoke-untested | `configx.TOTP.DriftSteps=1`; `pkg/credential/totp.Verify` checks T-1, T, T+1; unit-tested. Smoke computes the current step's code |
| `last_step` defeats same-step replay (RFC 6238 §5.2) | ✅ smoke-verified | credentials/C1; the smoke's `waitForNextTOTPStep` exists precisely because the server rejected a replay; absence of that wait causes step 26 or 37 to fail |
| `confirmed_at` gates the credential until first verify | ✅ smoke-verified | step 23 DB assert: `confirmed_at IS NOT NULL` after `/me/totp/verify` |
| Persistent throttle (RFC 4226 §7.3) | ✅ smoke-verified | credentials/R4; step 34 drives wrong codes until 429; step 35 asserts `auth_throttle (account_id, 'totp').failed_attempts>=3, locked_until>now` |
| Exponential backoff schedule `[0,0,1s,2s,...,15m]` | ✅ implemented; smoke-untested timings | `pkg/authn/throttle` per spec D2; the schedule is unit-tested. Smoke confirms lockout fires and Retry-After is non-empty but doesn't sleep through the curve |
| TOTP issuer / label format in QR codes | ✅ implemented; smoke captures URI | `pkg/credential/totp` emits `otpauth://totp/{Issuer}:{username}?secret=…&issuer=…`; smoke at step 21 receives `otpauth_uri` and logs the first 40 chars |
| Single TOTP credential per account | ✅ smoke-verified | step 23 DB assert: exactly 1 row in `totp_credential` for the account |

## Recovery codes

| Item | Status | Notes / source |
|---|---|---|
| argon2id PHC at rest, per-row salt | ✅ smoke-verified | credentials/C2; `recovery_code.hash` populated by `/me/totp/verify` at step 22 and `/me/recovery-codes/regenerate` at step 38 |
| Single-use (`used_at` enforced) | ✅ smoke-verified | step 31 DB assert after `/auth/recovery-code/verify` (step 30); recovery_code is no longer a sudo method, so the post-2026-05-28 smoke asserts the redeem at step 31 and the ceremony's atomic wipe at step 32d |
| Shown exactly once at enrollment | ✅ implemented | `/me/totp/verify` returns codes in the response body (step 22); `/me/recovery-codes/regenerate` (step 38) and the recovery ceremony's `/auth/recovery/totp/verify` (step 32c) all return cleartext exactly once — server never persists |
| Redemption context captured (session id, IP) | ✅ implemented | credentials/R7; `used_session_id` + `used_ip` written by the consume query; not asserted by smoke beyond `used_at IS NOT NULL` |
| Mint count: 10 per account | ✅ smoke-verified | step 22 + step 23 DB assert (initial 10) + step 32d (10 fresh after recovery ceremony) + step 38 (regenerate returns 10) |
| Recovery code as one-shot recovery bootstrap (not continuous sudo factor) | ✅ smoke-verified (2026-05-28 hardening) | post 2026-05-28 the only redeem path is `/auth/recovery-code/verify` → `recovery_session_token` → forced TOTP re-enrollment at `/auth/recovery/totp/{begin,verify}`; sudo-via-recovery-code is dropped. NIST SP 800-63B-4 §5.2 rationale (no knowledge-factor reauthentication). Steps 30–32f exercise the full ceremony; steps 39–40 assert recovery_code is NOT surfaced or accepted at `/me/sudo/*`. |
| Recovery codes redeemable independently of TOTP | ✅ smoke-verified | `/auth/recovery-code/verify` consumed after `/auth/password/begin` at steps 29–30 (no TOTP involvement). The user then re-enrolls TOTP via the ceremony at steps 32a–32c — `/begin` preserves the unredeemed recovery codes so a mid-ceremony abandon doesn't brick the account. |
| Code redemption logic | ✅ smoke-verified | `pkg/credential/totp.VerifyRecoveryCode` exercised at step 30 |
| 80-bit entropy, formatted `XXXX-XXXX-XXXX-XXXX` | ✅ implemented | `pkg/credential/totp.GenerateRecoveryCodes` per spec D4; format observed in response bodies at steps 22, 32c, 38 |
| Regeneration invalidates the prior set | ✅ smoke-verified | step 38 returns 10 fresh codes; the ceremony at step 32c likewise wipes the surviving 9 atomically before minting 10 new (audit: 9× `recovery_code:revoke` reason=`recovery_complete`) |

## Recovery ceremony (2026-05-28 hardening)

| Item | Status | Notes / source |
|---|---|---|
| `/auth/recovery-code/verify` returns `recovery_session_token`, NOT a session | ✅ smoke-verified | breaking change vs the pre-2026-05-28 surface; `pkg/server/handle_auth_password.go:172`; step 30 asserts no session cookie + a non-empty token |
| `recovery_session_token` is a narrow bearer scoped to two endpoints | ✅ smoke-verified | KV namespace `recovery_session:<tok>`, 10-min TTL, not accepted by `/me/*` or `/auth/totp/verify`; `pkg/server/handle_auth_recovery.go` |
| `/auth/recovery/totp/begin` wipes old TOTP but preserves recovery codes | ✅ smoke-verified | step 32a + step 32b DB assert (unconfirmed TOTP + 9 codes intact). Rationale: a user who abandons mid-ceremony must still be able to retry with another recovery code. `pkg/credential/totp.Store.BeginPreservingRecovery` |
| `/auth/recovery/totp/verify` atomically consumes the token (kv.Pop) | ✅ unit-test | `TestRecoveryTOTPVerify_ParallelAtomic` (8-way race; at most one consumer); `pkg/server/handle_auth_recovery.go:popRecoverySession` |
| `/auth/recovery/totp/verify` first-confirm wipes prior recovery codes + mints fresh batch in one tx | ✅ smoke-verified | step 32c verify → step 32d DB assert (exactly 10 codes); `pkg/credential/totp.Store.VerifyAndCommitRecovery` shares its body with `Verify` via a private `verify(…, purgePriorRecoveryOnFirstConfirm)` helper (no wrapper layer per `feedback_picotera_decoupling.md`) |
| Disabled-account re-check on both ceremony endpoints | ✅ unit-test | `TestRecoveryTOTPBegin_AccountDisabledMidFlow` / `TestRecoveryTOTPVerify_AccountDisabledMidFlow`; an admin disable mid-ceremony collapses to `recovery_session_invalid` |
| Failed `/verify` consumes the token (single-use, restart-on-failure) | ✅ unit-test | `TestRecoveryTOTPVerify_WrongCodeConsumesToken`; deliberate UX caveat documented in the design and in `handle_auth_recovery.go` to avoid the re-stash race |
| Audit trail (begin: `totp:revoke reason=recovery`; verify: 9× `recovery_code:revoke reason=recovery_complete`, `totp:register`, 10× `recovery_code:register`) | ✅ smoke-verified | step 45 (`credential_event` counts: `totp:revoke>=2`, `recovery_code:revoke>=9`, `recovery_code:register>=10`) |
| recovery_code NOT a sudo factor | ✅ smoke-verified | steps 39–40 assert both surfaces (methods list + dispatch rejection); `pkg/server/handle_sudo.go` package doc captures the NIST SP 800-63B-4 §5.2 rationale |

## Upstream OIDC federation (OIDC Core / RFC 9700)

| Item | Status | Notes / source |
|---|---|---|
| Per-IdP `upstream_idp` row with issuer + client + scopes | ✅ schema | `upstream_idp` (migration 004); model in `pkg/db/models.go` |
| Client secret AES-GCM encrypted with versioned DEK | ✅ smoke-verified | `pkg/federation/oidc/secret.go`; smoke step 46/69 seeds via `oidc.SealClientSecret` |
| AAD bound to row identity (`'upstream_idp:'||id||':'||key_version`) | ✅ implemented | `pkg/federation/oidc/secret.go` AAD format; 5/5 unit tests in `secret_test.go` including a cross-row-paste rejection case |
| JWT alg allowlist (RS256/ES256/EdDSA only; `HS256` / `none` rejected) | ✅ implemented | `pkg/federation/oidc/client.go` `DefaultAllowedAlgs()`; library-level enforcement + post-decode re-check |
| Three provisioning modes (`auto_provision` / `invite_only` / `link_only`) | ✅ all smoke-verified | `auto_provision` (steps 47–50/69), `link_only` (step 59/69), `invite_only` (steps 65–66/69) |
| `auto_provision` gated by `require_verified_email` + `allowed_domains` + username collision | ✅ smoke-verified | `pkg/federation/oidc/modes.go` `applyAutoProvision`; steps 53/69 (email_not_verified), 54/69 (username_collision) |
| `invite_only` mode end-to-end (token-bearing redemption) | ✅ smoke-verified | `pkg/federation/oidc/modes.go` `applyInviteOnly` + `pkg/federation/oidc/federation.go` `BeginInviteRedemption` + `pkg/server/handle_invite_federation.go`; step 65/69 drives `GET /enrollments/{token}/start-federation` → upstream `/authorize` → callback → 302 `/me`; step 66/69 DB-asserts `enrollment.consumed_at`, account + identity rows, and `credential_event[register reason=invite_only_redemption]` |
| `invite_only` rejects consumed token | ✅ smoke-verified | step 67/69: re-driving the consumed token through `/start-federation` returns 403 `invite_required` pre-flight (no upstream hop); `failNoAccount("invite_already_consumed")` audited |
| `invite_only` rejects expired token | ✅ smoke-verified | step 68/69: enrollment seeded with `expires_at = now() - interval '1 second'` returns 403 `invite_required`; `failNoAccount("invite_expired")` audited |
| Invite redemption is single atomic transaction (consume+account+identity+audit) | ✅ implemented | `pkg/federation/oidc/modes.go` `applyInviteOnly` wraps via `runInviteTx` + `pkg/server/server.go` passes `pgxpool`. Audit `Writer` is tx-scoped (`audit.NewWriter(qtx)`) so `credential_event.account_id` FK resolves against the just-inserted account row and rollback reverts audit too. Smoke step 66/69 is the regression gate for the audit-FK bug fixed in stage 3 |
| `account_identity` keyed on `(upstream_iss, upstream_sub)` (OIDC Core §2) | ✅ smoke-verified | UNIQUE constraint in migration 004; step 51/69 DB-asserts row insertion; step 62/69 asserts ownership |
| Federation state snapshots `expected_iss` + `expected_token_endpoint` | ✅ implemented | `pkg/federation/oidc/federation.go:190` populates `ExpectedIss`; library uses it on `client.Exchange` (mix-up resistance) |
| Single-use federation state via atomic Pop | ✅ implemented | `pkg/federation/oidc/federation.go:220` `kvStore.Pop(LoginKey(stateToken))`; unit-tested |
| Cross-namespace defense (LoginKey != LinkKey) | ✅ implemented | `pkg/federation/oidc/federation.go:202–204`; unit-tested — a `LoginKey`-stashed state cannot be redeemed via the link callback |
| RFC 9207 `iss` callback param validated against `state.ExpectedIss` | ✅ implemented | `pkg/federation/oidc/federation.go:231` (HandleCallback) + 317 (LinkCallback); unit-tested |
| Strict issuer + audience + nonce validation on upstream ID token | ✅ implemented | `pkg/federation/oidc/client.go` via `zitadel/oidc/v3 v3.47.5`; nonce threaded via context-key |
| Disabled-account re-check after Resolve | ✅ implemented | `pkg/federation/oidc/federation.go:269–278` — returns `authn.ErrBadCredentials()` (enumeration-safe, same path as password login) |
| `email_verified` gating per IdP (`require_verified_email` column) | ✅ smoke-verified | migration `006_federation_v03.sql`; `pkg/federation/oidc/modes.go` `applyAutoProvision` email_verified gate; step 53/69 |
| AMR pass-through from upstream + backfill to `["federated"]` when omitted | ✅ implemented | `pkg/server/handle_federation.go:113–119`. RFC 8176 §2 explicitly lists `federated` as a registered AMR value — backfilling is spec-compliant. The v0.3 spec draft language "empty array when upstream omits" was tightened in implementation to the named value; the array is never empty in the local session. |
| Per-IdP claim-name overrides (`username_claim` / `display_name_claim` / `email_claim`) | ✅ implemented | commit `45083bc` (audit fix M4): `pkg/federation/oidc/modes.go:133–135` (applyAutoProvision), `modes.go:518–519` (syncClaims drift sync), `modes.go:383` (applyInviteOnly email), and `pkg/federation/oidc/federation.go:453` (LinkCallback email) all route through the shared `ClaimString(tokens.Raw, idp.<Claim>)` helper. Override-key coverage in `modes_test.go`. Schema defaults match OIDC standard claim names (`preferred_username` / `name` / `email`) so the smoke's default-claim mock OP still exercises the helper |
| RP flow implementation (BeginLogin / HandleCallback / LinkBegin / LinkCallback) | ✅ smoke-verified | `pkg/federation/oidc/federation.go`; steps 47–49, 61, 64/69 |
| Local-username collision policy on JIT auto-provision | ✅ smoke-verified | `pkg/federation/oidc/modes.go` `applyAutoProvision` collision check; step 54/69 |
| Concurrent-callback 23505 mapping (auto_provision + invite_only) | ✅ implemented | commit `9ee15a4` (audit fix C1 + H3-di + H4-di): both apply paths share `runProvisionTx`; unique-violation on `account.username` surfaces as `ErrUsernameCollision`; unique-violation on `(upstream_iss, upstream_sub)` collapses onto `ErrInviteRequired` (anti-enumeration parity with `link_conflict`). Tx rollback un-does the partial account row. See `modes.go:198–230` (auto-provision) and `modes.go:367–402` (invite). Unit-tested in `modes_test.go` |
| Federation-bound invites reject WebAuthn enrollment path | ✅ implemented | commit `9ed0b1b` (audit fix M1-int): `/enrollments/{token}/register/{begin,complete}` reject any invite whose `expected_upstream_idp_slug` is set. `pkg/server/handle_enrollment.go:181–189` (begin guard) and `:383–392` (complete belt-and-suspenders); returns `ErrEnrollmentFederationRequired()` so the invitee is forced through `/start-federation` |
| `expected_token_endpoint` snapshot validated at callback | ✅ implemented | commit `4576a05` (audit fix H3-sch / spec D7): FedState already snapshotted `TokenEndpoint` at BeginLogin; `pkg/federation/oidc/federation.go:285–294` (HandleCallback) and `:402–410` (LinkCallback) reject when live discovery drifts from the snapshot. Audited `reason=token_endpoint_drift`. RFC 9700 §4.4.2.1 mix-up defense |
| Link-flow session-swap defense | ✅ implemented | `pkg/federation/oidc/federation.go` LinkCallback `state.LinkingAccountID` check — must equal current session's account; unit-tested |
| Last-sign-in-method check on unlink | ✅ implemented | `pkg/server/handle_me_identities.go` `handleMeIdentitiesUnlinkHTTP` last-method computation via `authn.AvailableMethods`; unit-tested (smoke-untested because the federated-only sudo path is unreachable) |
| Foreign / already-deleted unlink target returns 404 (no audit) | ✅ implemented | commit `5cd1f07` (audit fix M1-di): `db/queries/account_identity.sql:15–19` converted `DeleteAccountIdentity` to `:one` with `RETURNING id`; `pkg/server/handle_me_identities.go:177–192` maps `pgx.ErrNoRows` to `ErrCredentialNotFound` (404) and skips the audit emission. Prevents audit-log pollution from no-op unlinks of foreign rows |
| Refresh-token storage for upstream tokens | ❌ gap | not implemented; federated users re-authenticate via `/login` each time. Revisit if `/me` ever needs to refresh upstream profile claims out-of-band |

## OIDC OP downstream (RFC 6749 / OIDC Core / RFC 9068 / RFC 9700 / RFC 9207 / RFC 8414 / RFC 7636 / RFC 7009 / RFC 7662 / RP-Initiated Logout 1.0)

| Item | Status | Notes / source |
|---|---|---|
| Authorization Code + PKCE only | ✅ smoke-verified | `response_type=code` only; PKCE S256 enforced at `/oauth/authorize`; smoke step 72 (S256 happy path) + step 82 (PKCE mismatch → invalid_grant) |
| PKCE required for **all** clients (incl. confidential) | ✅ smoke-verified | the confidential smoke client supplies `code_challenge`/`code_verifier`; step 72→73 happy path, step 82 mismatch reject |
| `code_challenge_method` allowlist rejects `plain` | ✅ smoke-verified (v0.6, smoke 103) | oidc/R2, v0.6 D6; `plain` is excluded ENTIRELY by a DB CHECK on `oidc_client.allowed_code_challenge_methods` (migration 002); `/oauth/authorize` consults per-client `require_pkce` + `allowed_code_challenge_methods`. Smoke step 103 drives `code_challenge_method=plain` → redirect `error=invalid_request` |
| `redirect_uri` exact-match (no wildcards) | ✅ smoke-verified | exact-match against `oidc_client.redirect_uris`; smoke step 81 drives an unregistered `redirect_uri` → DIRECT 400 (no redirect to the bad URI) |
| `post_logout_redirect_uris` exact-match list | ✅ smoke-verified | oidc/C1; `/oidc/logout` exact-matches `post_logout_redirect_uris`; smoke step 84 redirects to the registered URI with `state` echoed |
| Single-use authorization codes with replay revocation | ✅ smoke-verified | oidc/C8; code is KV `Pop`-consumed; replay → family revoke + `code_replay` audit. Single-use enforced by the happy path (steps 73, 79, 80 each consume a fresh code); refresh-family reuse → revoke is steps 77–78 |
| `iss` parameter in authorization response (RFC 9207) | ✅ smoke-verified | discovery `authorization_response_iss_parameter_supported:true`; step 72 asserts `iss` on the 302 redirect |
| Discovery doc (RFC 8414 / OIDC Core) | ✅ smoke-verified | `/.well-known/openid-configuration` serves the live OP surface (introspection/revocation/end_session endpoints, `scopes_supported` incl `offline_access`, `code_challenge_methods_supported [S256]`, `token_endpoint_auth_methods_supported [client_secret_basic,client_secret_post,none]`); `claims_supported` lists `sub/iss/aud/exp/iat/nonce/auth_time/amr/acr/sid/at_hash/username/displayName/role/attributes`. `pkg/protocol/oidc/oidc.go:67–97`; exercised by every smoke `verifyIDToken`/JWKS fetch |
| JWKS endpoint | ✅ smoke-verified | `/oauth/jwks` serves the active+cached signing keys as RFC 7517 RSA JWKs; smoke step 70 asserts exactly 1 key with the minted `kid`; every token verify resolves the key by `kid` from JWKS |
| ID token signed with asymmetric alg | ✅ smoke-verified | RS256; smoke verifies the id_token signature against JWKS at step 73 |
| `alg: none` rejected | ✅ implemented; smoke-untested | verify resolves keys by `kid` and parses with `[]SignatureAlgorithm{RS256}` only; the `alg:none`/wrong-alg reject is unit-tested in `pkg/protocol/oidc/jwt_test.go`, not driven by the smoke |
| ID token claims: signature, `iss`, `aud`, `exp`, `nonce` validated | ✅ smoke-verified | step 73 asserts `iss`, `aud`, `sub`, `nonce` after JWKS signature verification |
| ID token `auth_time` claim (OIDC Core §2) | ✅ smoke-verified | sourced from `session.auth_time`; smoke step 73 asserts `auth_time` present on the id_token |
| ID token `amr` / `acr` claims | ✅ smoke-verified (amr) | `amr` sourced from `session.amr` (WebAuthn → `["hwk"]`); step 73 asserts `amr` present. `acr` is emitted when present on the session (reserved/sparse today) — not asserted by the smoke |
| ID token `azp` when `aud` is multi-valued | ✅ implemented; smoke-untested | oidc/C5; single-client today so `aud` is single-valued and `azp` is not emitted in the smoke. Multi-`aud` is not a v0.4 deployment shape |
| ID token `at_hash` (defense in depth) | ✅ smoke-verified | oidc/C5; left-half SHA-256 of the access token; step 73 asserts `at_hash` non-empty |
| `sid` claim sourced from `session.id` | ✅ smoke-verified | step 73 asserts `sid` present; step 85 confirms `/oidc/logout` revoked exactly that `sid`'s session (`/me` → 401) |
| RFC 9068 access token `typ: at+jwt` | ✅ smoke-verified | oidc/C4; step 73 asserts the access token's JOSE `typ` is `at+jwt` |
| RFC 9068 required claims (`iss`, `sub`, `aud`, `exp`, `iat`, `jti`, `client_id`, `scope`) | ✅ smoke-verified | oidc/C3; step 73 asserts `jti` present; `client_id`/`sub`/`scope` confirmed via introspection at step 75 |
| `jti` revocation via denylist | ✅ smoke-verified | oidc/C3; step 80 revokes an access token → introspect `active:false`; step 86 DB-asserts the `revoked_jti` row |
| Refresh tokens single-use rotation + reuse detection | ✅ smoke-verified | rotation step 76 (new ≠ old); superseded-token replay → `invalid_grant` step 77; family revocation on reuse step 78 |
| Refresh tokens stored server-side (opaque) | ✅ smoke-verified | KV-backed opaque tokens; rotation/reuse behavior (steps 76–78) is observable only because the family record is server-side |
| Access tokens short-lived (≤ 15 min) | ✅ implemented | `configx.OIDC.AccessTokenTTL` default 10m; smoke asserts `expires_in > 0` at step 73 (does not sleep to expiry) |
| Refresh tokens 30 day default | ✅ implemented | `configx.OIDC.RefreshTokenTTL`; not time-asserted by the smoke |
| `offline_access` scope gates refresh issuance (OIDC Core §11) | ✅ smoke-verified | oidc/R3; the smoke client requests `offline_access` and step 73 asserts a refresh token is present |
| argon2id hashing for `client_secret_hash` | ✅ smoke-verified | `oidc-client create` argon2id-hashes the secret (printed once); step 83 (wrong secret → 401 `invalid_client`) + step 73 (correct secret authenticates) exercise verify against the stored hash |
| `token_endpoint_auth_method` (`client_secret_basic` default, `none` for public) | ✅ smoke-verified (basic); ✅ implemented (post/none) | oidc/R1; smoke uses `client_secret_basic` (steps 73–80). `client_secret_post` and `none` (public client) are implemented + unit-tested but not driven end-to-end by the smoke |
| `id_token_signed_response_alg` per client | ✅ schema | oidc/R1 |
| `subject_type` (`public` / `pairwise`) | ✅ schema | oidc/R1 |
| `application_type` (`web` / `native`) | ✅ schema | oidc/R1 |
| `default_max_age` / `require_auth_time` per client | ✅ schema | oidc/R1 |
| `contacts` / `logo_uri` / `tos_uri` / `policy_uri` | ✅ schema | oidc/R1 |
| Token introspection (RFC 7662) — `active`, `sub`, `scope`, `client_id`, `exp` | ✅ smoke-verified | oidc/R6; `/oauth/introspect` client-authenticated, per-client ownership; step 75 (`active:true` + `token_type:access_token` + `client_id` + `sub`) and step 80 (revoked token → `active:false`) |
| Introspection requires a confidential client; public clients rejected | ✅ smoke-verified (v0.6, smoke 104) | v0.6 D7; RFC 7662 §2.1 — a public (`none`-auth) client calling `/oauth/introspect` → `invalid_client` (401). **Behavior change** from v0.4 (which allowed public-client introspection of own tokens). Smoke step 104: public introspect → 401, confidential introspect of own token → `active:true`, public `/oauth/revoke` of own token → 200 (RFC 7009 unchanged) |
| Token revocation (RFC 7009) | ✅ smoke-verified | `/oauth/revoke` client-authenticated, per-client ownership, always 200; access → `revoked_jti` denylist (step 80 + DB assert step 86), refresh → family revoke (step 79); outstanding access tokens self-expire ≤ `AccessTokenTTL` |
| Pushed Authorization Requests (PAR, RFC 9126) | ⚠️ conditional | not needed for first-party clients; add only if a low-trust client requires it |
| JAR (RFC 9101) | ⚠️ conditional | same |
| DPoP (RFC 9449) sender-constrained tokens | ⚠️ conditional | bearer is fine for first-party; add for a low-trust client |
| mTLS (RFC 8705) | ⚠️ conditional | bearer is fine for first-party; add for a low-trust client |
| Dynamic client registration (RFC 7591) | ⚠️ out of scope | first-party static config (ARCHITECTURE → Out of scope) |
| Pairwise sub identifiers | ⚠️ conditional | `subject_type='pairwise'` column reserved; only if RP correlation-resistance is needed |
| Encrypted ID tokens (JWE) | ⚠️ deferred | TLS provides confidentiality on the wire |
| RP-Initiated Logout 1.0 | ✅ smoke-verified | `/oidc/logout` validates `id_token_hint` sig + `iss` (tolerates expiry), revokes the `sid`'s session, exact-match `post_logout_redirect_uri`; step 84 (302 + `state`) + step 85 (`sid` session revoked → `/me` 401) |
| `prompt=login` forced re-auth | ✅ smoke-verified (v0.6, smoke 100) | v0.6 D1/D2; full fresh re-login + single-use KV nonce (`pkg/authn` `DemandReauth`/`ConsumeReauth`, prefix `oidc:reauth:`). A stale session does NOT issue (its `auth_time` predates the demand); a fresh login + the nonce issues. Step 100 |
| `max_age` forced re-auth | ✅ smoke-verified (v0.6, smoke 101) | v0.6 D3; `max_age=0` always demands (bounces even the just-minted session); a large `max_age` is satisfied by a recent session. Step 101 |
| `prompt=none` + re-auth demand → `login_required` | ✅ smoke-verified (v0.6, smoke 102) | v0.6 D4; no bounce — a redirect carrying `error=login_required`. Step 102 (`prompt=login`+`none` → `invalid_request` is also implemented but unit-tested only, not step 102) |
| Front-channel / back-channel logout | ⚠️ deferred (unscheduled) | multi-RP coordinated sign-out; IdP-local logout ships |
| Mix-up attack resistance | ✅ implemented | `iss` param (RFC 9207) emitted at `/authorize` (step 72) + federation state snapshots (v0.3) |
| Refresh-token family forensics table | ⚠️ deferred (unscheduled) | oidc/R7; KV-only in v0.4 — reuse-detection + family revocation work end-to-end (steps 77–78) without a forensics table |
| Rate limit on `/oauth/authorize` and `/oauth/token` | ✅ implemented (per-identity, NOT per-IP — D3) | INTENTIONAL policy, not a per-IP limiter: `/authorize` keyed per `account_id`; `/token`/`/introspect`/`/revoke` per `client_id`; `/userinfo` per `sub` (keys `oidc:authorize:acct:<id>`, `oidc:token:client:<id>`, `oidc:userinfo:sub:<sub>`). Reuses the v0.2/v0.3 account/session-keyed limiter. This both closes the original "rate limit `/authorize` + `/token`" gap AND respects the v0.3 M5 decision that client IP is untrustworthy behind NAT/CDN — no per-IP buckets were reintroduced. Edge DoS remains the reverse-proxy/WAF's job. See "Rate limiting policy (v0.3 audit)" below. Caps are unit-/manually verified; the smoke does not flood to trip them |

## SAML IdP (SAML 2.0 Core / Bindings / Metadata / Profiles)

**v0.5 shipped — SP-initiated SSO + IdP-local SLO + metadata + CLI,
smoke-verified end-to-end (steps 88–99). v0.6 added — `ForceAuthn`,
`NameIDPolicy/@Format`, POST-binding AuthnRequest, signed metadata, and
IdP-initiated SSO, smoke-verified end-to-end (steps 105–111).** All against
a live PG + dev server + in-process mock SP. Handlers are `IdP` methods in
`pkg/protocol/saml`; routes mounted at `pkg/server/server.go` (incl. the
new `GET /saml/sso/init`).

| Item | Status | Notes / source |
|---|---|---|
| Implementation | ✅ smoke-verified | `pkg/protocol/saml` (`idp.go`/`metadata.go`/`authnreq.go`/`assertion.go`/`attributes.go`/`subjectid.go`/`sso.go`/`slo.go`/`xmlsec.go`); 3 routes mounted; smoke steps 88–99 |
| IdP metadata endpoint (`/saml/metadata`) — `EntityDescriptor` with ≥1 signing `KeyDescriptor` | ✅ smoke-verified (step 89) | step 89 asserts the `EntityDescriptor` carries an `IDPSSODescriptor` with ≥1 signing `KeyDescriptor` |
| `/saml/metadata` SSO/SLO bindings + `NameIDFormat` + `WantAuthnRequestsSigned` | ✅ unit (`metadata_test.go`) | emitted by `metadata.go`; covered by `metadata_test.go`; not asserted by the smoke |
| SP-initiated SSO (`/saml/sso`) | ✅ smoke-verified (step 91) | HTTP-Redirect AuthnRequest in → signed Response auto-POSTed to ACS; SP-side `ParseXMLResponse` verifies it |
| SP registry with entity ID, NameID format, attribute map | ✅ schema; ✅ smoke (step 90) | `saml_sp`; `saml-sp create --kind ghes` registers + ingests metadata |
| Multi-endpoint ACS (Metadata §2.4.4) | ✅ schema | saml/C1; `saml_sp_acs` child table; CLI ingests all ACS from metadata |
| ACS URL validated by exact match → index lookup → is_default | ✅ smoke-verified (step 97) | saml/C1; bad/unregistered ACS rejected (open-redirect guard) |
| Multiple SP signing/encryption certs per SP (rotation-friendly) | ✅ schema | saml/C3; `saml_sp_key (sp_id, use)` |
| `require_signed_authn_request` per SP | ✅ smoke-verified (step 96) | saml/C3; unsigned AuthnRequest to a `require_signed` GHES SP → rejected |
| `want_assertions_signed` / `authn_requests_signed` mirror SP metadata | ✅ schema | saml/R4; CLI honors `--want-assertions-signed` + metadata `AuthnRequestsSigned` |
| Both `<Response>` and `<Assertion>` signed (RSA-SHA256, exclusive C14N) | ✅ smoke-verified (step 91); ✅ unit | saml/GHES-1, D4; smoke verifies the Response signature SP-side; sign-both + alg are unit-tested in `assertion_test.go` |
| `Destination` on `<Response>` = chosen ACS URL | ✅ smoke-verified (step 91) | saml/GHES-2; asserted by SP-side parse |
| `<SubjectConfirmationData Recipient>` = chosen ACS URL | ✅ smoke-verified (step 91) | Profiles §4.1.4.2; asserted by SP-side parse |
| `<Audience>` = `saml_sp.entity_id` verbatim | ✅ smoke-verified (step 91) | saml/C2; asserted by SP-side parse |
| `InResponseTo` echoed on Response + SubjectConfirmationData | ✅ smoke-verified (step 91) | the mock SP calls crewjam `ParseXMLResponse(respXML, []string{requestID}, …)` which validates `InResponseTo` against that request-ID list (steps 91/92); also asserted in `assertion_test.go` |
| Stable pairwise NameID (Core §8.3.7) | ✅ smoke-verified (steps 92–93) | saml/C5, D6; identical NameID across 2 SSOs; DB assert 1 `saml_subject_id` row, stable `name_id` |
| Persistent 1.1-namespace NameID default (Format URI) | ✅ schema; ✅ unit (`assertion_test.go`) | saml/C4; `saml_sp.name_id_format` default `urn:oasis:names:tc:SAML:1.1:nameid-format:persistent`; steps 91/92 verify the NameID *value* (presence + stability), not the Format URI — the format default is unit-tested |
| Attribute map as ordered JSONB array (multi + URI NameFormat) | ✅ smoke-verified (step 91, USERNAME) | saml/R1; `attributes.go` projects the GHES map; smoke asserts the `USERNAME` attribute, full map unit-tested |
| Per-SP `session_lifetime` for `SessionNotOnOrAfter` | ✅ schema; ✅ unit | saml/GHES-8; `SessionNotOnOrAfter` set from `session_lifetime` in `assertion.go` (unit-tested) |
| Metadata freshness fields (`metadata_*`) | ✅ schema | saml/R3 |
| `AuthnContextClassRef` (`PasswordProtectedTransport`) | ✅ unit | saml/R5; emitted in `assertion.go`, unit-tested; not separately asserted by the smoke |
| IdP metadata publishes all non-retired (live + grace) signing keys | ✅ smoke-verified (step 89); ✅ unit | saml/R6, D7; smoke asserts ≥1 KeyDescriptor; the multi-key / grace-window selection is unit-tested in `keys_saml_test.go` |
| Signing-key reuse: same `signing_key` signs OIDC + SAML | ✅ smoke-verified (step 91) | D7; step-70 OIDC key reused to sign the step-91 SAML Response |
| Issuer/EntityID = `PublicOrigins[0]` | ✅ unit | D8; `saml.go` `entityID()`/`ssoURL()`/`sloURL()` derive from `PublicOrigins[0]` (unit-tested). Step 89 logs the EntityID but does not assert it equals `PublicOrigins[0]`; step 91's `Audience` check round-trips but is circular (the mock SP is built from the same metadata) |
| GHES `sp_kind='ghes'` auto-sets `require_signed_authn_request=true` | ✅ smoke-verified (step 90→96) | saml/GHES-10; CLI forces it for `--kind ghes`; step 96 proves enforcement |
| GHES `emails` / `public_keys` / `gpg_keys` multi-valued | ✅ schema; ✅ unit | saml/GHES-6; `attribute_map.multi=true`; multi projection unit-tested in `attributes_test.go` |
| GHES `public_keys` URI NameFormat (`Name=urn:oid:1.2.840.113549.1.1.1`) | ✅ unit | saml/GHES-7; emitted with URI NameFormat + OID Name; unit-tested |
| GHES `administrator` attribute literal | ✅ unit | saml/GHES-5; emitted only as `"true"` when `role=='admin'`/`attributes.administrator` truthy; unit-tested |
| Single Logout (SLO) endpoint (`/saml/slo`) — IdP-local | ✅ smoke-verified (steps 94–95) | D2; signed LogoutRequest → signed LogoutResponse; bound session revoked, a different session survives |
| SLO LogoutRequest signature verify + LogoutResponse sign | ✅ smoke-verified (step 95); ✅ unit | smoke drives the redirect-binding round trip; the LogoutResponse signature is verified in `slo_test.go` (unit), not re-verified by the smoke |
| `saml_session` populated + consumed by SLO | ✅ smoke-verified (steps 93–95) | DB assert ≥2 `saml_session` rows (step 93); SLO revokes exactly the bound one (step 95) |
| `credential_event` (factor `saml_sp`) for SSO + SLO | ✅ smoke-verified (step 99) | DB assert: `use` for SSO + `session_end` for SLO |
| **XSW defense** (signature Reference ties to the processed element's own ID) | ✅ unit | saml/XSW; `xmlsec.go` `parseXMLSecure` + reference-tie check; XSW/duplicate-assertion negatives in `xmlsec_test.go` |
| **XXE / DTD-off parsing + duplicate-ID rejection** | ✅ unit | `xmlsec.go` `parseXMLSecure`; DTD-bearing + duplicate-ID payloads rejected (unit) |
| **SHA-1 rejected** (signature alg + digest) | ✅ unit | RSA-SHA256 only; SHA-1 sig/digest rejected on verify (unit) |
| **SP-signature cert-pinning** (verify against `saml_sp_key`, never message-embedded cert) | ✅ design; ✅ unit | D5; verification cert-pinned to the registered `saml_sp_key`; unit-tested (sidesteps crewjam/saml#384) |
| **AuthnRequest replay single-use** (KV) | ✅ smoke-verified (step 98) | replayed AuthnRequest ID → 2nd rejected; marker written on the issue path (so the login bounce can re-drive once) |
| **DEFLATE decompression-bomb bound (10 MB)** | ✅ unit | `xmlsec.go` caps redirect-binding inflation at 10 MB |
| **ACS open-redirect guard** (only DB-registered ACS locations) | ✅ smoke-verified (step 97) | bad/unregistered ACS → reject; unknown SP → direct error, never a redirect |
| **AuthnRequest `ID` required (NCName)** | ✅ unit | missing/invalid request `ID` rejected (unit) |
| `IsPassive` honored → `NoPassive` Response | ✅ smoke-verified (v0.6, smoke 106) | v0.5 D3/D5; `ForceAuthn`+`IsPassive` (with session; IsPassive wins) → `NoPassive` status Response, no assertion. Smoke step 106 drives this. The no-session+`IsPassive` path remains unit-tested only (sso_test.go; the smoke holds a live session) |
| POST-binding AuthnRequest + POST-binding LogoutRequest | ✅ AuthnRequest smoke-verified (v0.6, smoke 108); LogoutRequest ✅ unit | v0.6 D9: POST-binding AuthnRequest intake smoke-verified at step 108; the POST-binding LogoutRequest parse/verify path is unit-tested (the smoke exercises the REDIRECT binding for SLO) |
| No-stored-SLO-endpoint fallback → 200 `text/xml` LogoutResponse | ✅ unit | `slo.go` fallback; unit-tested only |
| No-session SSO → 302 to `Issuer+/login?return_to=<SSO URL>` | ✅ unit | the smoke holds a live session, so the login-bounce branch is unit-tested only |
| SLO response location resolution | ✅ smoke (round-trip, step 95); ✅ unit (location resolution, `slo_test.go`) | saml/R2; the SP's `SingleLogoutService` location is parsed from the stored SP metadata at request time (`parseSPSLOEndpoint` — `ResponseLocation` else `Location`), NOT a `saml_sp_slo` child table (that table does not exist); request-supplied locations are never trusted. Step 95 asserts the SLO round-trip (302 + decodable Success `LogoutResponse` + session revoked) but does NOT assert the response `Location` host matches the SP's registered SLO location — the location-resolution logic is unit-tested in `slo_test.go` |
| `ForceAuthn` (forced re-auth) | ✅ smoke-verified (v0.6, smoke 105–106) | v0.6 D1/D2/D5 (closes v0.5 D3 deferral); `ForceAuthn` triggers the re-auth bounce + single-use nonce (`pkg/authn` `DemandReauth`/`ConsumeReauth`, prefix `saml:reauth:`) — a stale session does NOT issue, a fresh login + nonce → assertion with a fresh `AuthnInstant` (step 105). `ForceAuthn` + `IsPassive` → `NoPassive` status Response, no assertion (IsPassive wins, step 106) |
| `NameIDPolicy/@Format` honored | ✅ smoke-verified (v0.6, smoke 107) | v0.6 D8 (closes the v0.5 "@Format not honored" deferral); a requested concrete Format that we can't produce (≠ persistent, ≠ `unspecified`) → `InvalidNameIDPolicy` status, no assertion; `unspecified`/absent/matching → normal assertion. Step 107 (`Format=emailAddress` → `InvalidNameIDPolicy`) |
| POST-binding AuthnRequest intake (`POST /saml/sso`) | ✅ smoke-verified (v0.6, smoke 108) | v0.6 D9 (closes the v0.5 "POST-binding AuthnRequest unimplemented" deferral); enveloped-signed AuthnRequest accepted (base64, no inflate, verified against `saml_sp_key`); POST SSO binding re-advertised in metadata. Step 108 |
| Signed IdP metadata + `validUntil`/`cacheDuration` | ✅ smoke-verified (v0.6, smoke 109) | v0.6 D10 (closes the v0.5 "metadata unsigned, omits validUntil/cacheDuration" deferral); `EntityDescriptor` signed, verifies against its own cert; `validUntil` + `cacheDuration` from `configx.SAML.MetadataValidity`; fails OPEN to unsigned if no active signing key (fail-open branch: unit-tested only, TestMetadataNoActiveKeyUnsigned). Step 109 |
| IdP-initiated SSO | ✅ smoke-verified (v0.6, smoke 110–111) | v0.6 D11 (closes the v0.5 out-of-scope item); `GET /saml/sso/init?sp=<entity_id>&RelayState=<deep-link>` emits an UNSOLICITED Response (no `InResponseTo`) to the SP's DEFAULT ACS, gated by per-SP `saml_sp.allow_idp_initiated` (default false; non-opted-in → 403); `RelayState` verbatim; rate-limited per-account + per-SP; audit `reason=idp_initiated`. `saml-sp create --allow-idp-initiated`. Steps 110–111 |
| Front-channel multi-SP SLO propagation | ⚠️ out of scope (D2) | SLO is IdP-LOCAL only — revokes the bound Prohibitorum session, no propagation to the user's other SPs |
| AttributeQuery / NameIDMapping / Artifact binding | ⚠️ out of scope | saml/Optional |
| `default_relay_state` per SP (only if IdP-initiated lands) | ⚠️ out of scope | saml/Optional |
| Encrypted assertions / NameID (`saml_sp_key.use='encryption'`) | ⚠️ conditional | column exists but unused; add only on SP demand (GHES does not require it) |

**Accepted / deferred (tracked, not blocking v0.6):**
- IdP-initiated SSO — ✅ shipped in v0.6 (D11; per-SP opt-in, default ACS,
  smoke 110–111). No longer deferred.
- `ForceAuthn` / `NameIDPolicy/@Format` / POST-binding AuthnRequest / signed
  metadata — ✅ all shipped in v0.6 (D5/D8/D9/D10; smoke 105–109). No longer
  deferred.
- Front-channel multi-SP SLO — STILL out of scope; SLO is IdP-local
  (revokes the bound session only, no propagation) (D2).
- Assertion / NameID encryption — conditional (SP-demand); the
  `saml_sp_key.use='encryption'` column is reserved but unused (GHES
  doesn't require it) (D2).
- No-stored-SLO-endpoint fallback returns a 200 `text/xml`
  LogoutResponse (unit-tested only).

### Post-implementation audit (2026-05-30) — done

After all 14 v0.5 tasks shipped (smoke green end-to-end), a parallel
4-lens audit ran — crypto/XML-DSig + protocol-standards + race-logic +
a deep second pass (integration / data-integrity / schema-drift), focus
on XSW/XXE, signature verification, NameID stability, and replay.
**No Critical findings.** The deep + race passes earned their keep
(as in v0.4): they found two High-class issues the schema-resetting
smoke structurally cannot catch (a live-session-only SLO + a fresh DB
each run). All Highs and the security/interop Mediums were fixed across
three batches; the remainder are documented as accepted/deferred below.

- **Batch A (crypto / interop) `3305ac9`:** `<ds:Signature>` is now
  relocated to immediately after `<Issuer>` in every signed element —
  goxmldsig appends it last, which violates the SAML XSD element order
  and is **rejected by strict schema-validating SPs** (Shibboleth / ADFS
  / OpenSAML); crewjam's lenient parser hid it from the interop test, so
  this was the v0.5 analog of "an interop break the lenient test missed."
  Also: SP cert validity (NotBefore/NotAfter) is now checked on the
  HTTP-Redirect signature path (parity with the POST path); the enveloped
  verify now requires a **positive** RSA-SHA256 + SHA-256 allowlist (not
  just a SHA-1 denylist); and an XSW subtree assertion rejects any second
  `<Signature>` claiming the processed element's own ID.
- **Batch B (SAML conformance) `e5432cf`:** `resolveACS` now honors
  `AssertionConsumerServiceIndex` and the lowest-`index` implicit-default
  ACS (Web SSO Profile §4.1.4.1 / Metadata §2.4.4.1) — the open-redirect
  guard (only registered Locations) is intact; the persistent NameID now
  carries `NameQualifier` (IdP entityID) + `SPNameQualifier` (SP entityID)
  per Core §8.3.7; inbound `Version == "2.0"` is enforced on AuthnRequest
  + LogoutRequest (Core §3.2.1).
- **Batch C (saml_session lifecycle) `87bc8c8`:** the two High-class
  data-integrity findings — **(1)** SLO orphaned a `saml_session` row
  whenever the bound IdP session was already revoked (`GetSession` filters
  `revoked_at IS NULL` → the old code skipped `DeleteSAMLSessionsBySession`):
  the row delete is now **unconditional** (the binding is removed whether
  or not the underlying session is still live; the signature gate still
  precedes all mutation); **(2)** re-SSO inserted duplicate rows and the
  `ON DELETE CASCADE` was dead code (sessions are soft-revoked, never
  hard-deleted): added `UNIQUE (session_id, sp_id, session_index)` + an
  upsert (refresh `not_on_or_after`, no dup) and a background
  `pruneExpiredSAMLSessionsLoop` reaper (mirrors `pruneRevokedJTILoop`).
  SLO partial-revoke failures are now surfaced in the audit record
  (`detail.partial=true`) instead of silently swallowed. (Re-ran the
  full suite + the end-to-end smoke green after the schema amend.)

**Accepted / deferred (tracked, not blocking v0.5):**
- AuthnRequest-ID replay is a non-atomic KV Get→SetEx (a `SetNX` primitive
  isn't on the `kv.Store` interface). Real-world impact is low: a replayed
  AuthnRequest yields an identical assertion to the **same registered ACS**
  for the same subject (the SP de-dupes by `InResponseTo`), and it requires
  a live IdP session. Documented limitation.
- SLO↔SSO resurrection race: a concurrent SSO that already passed the
  session gate can mint one assertion for a session being logged out
  (bounded to one in-flight request, same authenticated user).
- `NameIDPolicy/@Format` is not honored (no `InvalidNameIDPolicy` status) —
  **CLOSED in v0.6** (D8; smoke 107). A genuine unproducible format now
  returns `InvalidNameIDPolicy`; `unspecified`/absent/matching proceeds.
- POST-binding **AuthnRequest** intake is unimplemented —
  **CLOSED in v0.6** (D9; smoke 108). `POST /saml/sso` accepts an
  enveloped-signed AuthnRequest and the POST SSO binding is re-advertised in
  metadata. Front-channel SLO propagation and assertion/NameID **encryption**
  remain out of scope.
- IdP metadata is unsigned and omits `validUntil`/`cacheDuration` —
  **CLOSED in v0.6** (D10; smoke 109). The `EntityDescriptor` is signed and
  carries `validUntil`/`cacheDuration` (fails open to unsigned if no active
  key).

## Cryptography

| Item | Status | Notes |
|---|---|---|
| All tokens via `crypto/rand` | ✅ | 32 bytes session / enrollment, 16 bytes pairing id, 64 bytes WebAuthn user handle |
| Pairing code: rejection-sampled, unambiguous alphabet, ~40 bits | ✅ | 8 chars from 30-char alphabet |
| JWT signing: RS256 (2048-bit RSA) | ✅ design | asymmetric, widely supported |
| Unified `signing_key` for OIDC + SAML (use sig|enc, kid rotation) | ✅ schema | spec §"db/migrations/002_oidc.sql"; oidc/R4 |
| Key rotation: insert new, flip active, retire old after grace | ✅ design | `signing_key.not_before` + `retired_at` |
| `not_before` on signing keys (oidc/R4) | ✅ schema | `signing_key.not_before` |
| AES-256-GCM at rest with versioned DEK | ✅ design | credentials/C3; `PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>`. DEK rotation budget (~2^32 ciphertexts per key, NIST SP 800-38D §8.3) and re-encrypt-sweep plan documented in `docs/superpowers/notes/2026-05-28-v0.2-deployment-notes.md` §2 |
| AAD binds ciphertext to row identity | ✅ design | credentials/C4 |
| 12-byte per-row nonce, unique per row | ✅ design | NIST SP 800-38D §5 |
| argon2id PHC for password / recovery / client_secret hashes | ✅ design; ✅ audit-hardened (Bundle 3) | credentials/R5 + credentials/C2. `pkg/credential/password.PHCDecode` enforces a lower-bound floor on `m`/`t`/`p` (Bundle-3 Crypto Open-Q-5) as defense-in-depth against tampered/injected stored hashes; floor is intentionally well below OWASP minimum (m≥8 MiB) — it's a sanity check, not a config gate |
| HSM / KMS-backed signing | ⚠️ deferred (unscheduled) | private keys are DEK-sealed at rest (`private_pem_enc`); KMS would additionally defend a DB+env compromise |
| TLS termination | external | reverse-proxy responsibility. Session cookie's `Secure` + `__Host-` derive from the public-origin scheme (`PUBLIC_ORIGIN`=https → hardened), deployment-stable; the ceremony cookie's `Secure` is still per-request TLS-detected |
| Time skew tolerance on JWT verification | ✅ design | 30s leeway on `exp` / `iat` / `nbf` |

## Operational

| Item | Status | Notes |
|---|---|---|
| Forward-only migrations via goose | ✅ | embedded `.sql` files; goose installation quirk documented in STATUS.md |
| Structured audit logs via `credential_event` | ✅ smoke-verified; ✅ audit-hardened (Bundle 3) | credentials/New tables; `pkg/audit.Writer` writes `register`/`use`/`fail`/`revoke` rows for password / totp / recovery_code + `session:sudo_granted` + `factor_locked` (Bundle 3 Low-1: emitted by `pkg/authn/throttle.RegisterFailure` on the unlocked→locked transition); step 45 DB assert checks the union of (factor, event) counts |
| Audit-log fields: who, what, when, IP, UA, detail | ✅ smoke-verified | `credential_event.{account_id, factor, event, credential_ref, ip, user_agent, detail jsonb, at}`; populated by every v0.2 handler that touches a credential |
| Session manager for end users (`/me/sessions`) | ✅ | carried from v0.1 skeleton |
| Admin can revoke other-user sessions | ✅ | `/accounts/revoke-sessions` |
| Live `account.disabled` check per request | ✅ | `session.LoadSession` middleware |
| Sudo mode for sensitive actions | ✅ smoke-verified | `pkg/server/handle_sudo.go`; post 2026-05-28 hardening sudo accepts 2 methods (`webauthn` / `password_totp`). Steps 18, 37, 41 exercise each method end-to-end. Steps 39–40 assert that `recovery_code` is REJECTED as a sudo method (rationale: NIST SP 800-63B-4 §5.2 — knowledge factor MUST NOT be used for reauthentication). |
| Sudo discovery endpoint (`GET /me/sudo/methods`) | ✅ smoke-verified | priority order `webauthn` → `password_totp` from `pkg/authn/flow.AvailableMethods` (recovery_code is intentionally excluded); step 39 of the smoke asserts recovery_code is not surfaced |
| WebAuthn-preferred factor policy (revoke-password-totp) | ✅ smoke-verified | `/me/auth/revoke-password-totp` deletes password + TOTP + recovery rows transactionally (step 42); DB assert at step 43; post-revoke `/auth/password/begin` returns 401 at step 44 |
| Rate limit on auth-sensitive endpoints (`/auth/*`) | ✅ smoke-verified; ✅ audit-revised (v0.3) | `pkg/authn/ratelimit` + per-account `auth_throttle` (steps 34–35). v0.3 audit M5: IP-keyed buckets removed project-wide — see "Rate limiting policy (v0.3 audit)" below. Multi-replica caveat (in-process limiter) documented in `docs/superpowers/notes/2026-05-28-v0.2-deployment-notes.md` §1; cross-surface coupling (login↔sudo share `auth_throttle`) documented §5 |
| OpenAPI spec for management API | ✅ | huma-generated |
| Admin UI for accounts | ⚠️ deferred (v0.6) | dashboard scaffold empty in v0.1 |
| Admin HTTP API for OIDC clients / SAML SPs / upstream IdPs / signing keys | ✅ smoke-verified | `registerSudoOpHTTP` centralises gate (admin auth + sudo + body-size + content-type); OIDC client create/update/rotate-secret/delete smoke steps 114–116; signing-key generate/activate smoke steps 117–119; audit-events viewer smoke step 120; credential list/force-revoke smoke step 121. Audit events written for every mutation (`factor` ∈ oidc_client/saml_sp/upstream_idp/signing_key). See `api.md` for the full route table. |
| Admin dashboard UI pages for OIDC clients / SAML SPs / upstream IdPs | ✅ | implemented — `Admin{OidcClients,SamlProviders,UpstreamIdps,SigningKeys,Accounts,Invitations,Audit}View` with CRUD wired to the admin API |
| Consent screen | ⚠️ deferred | first-party-only deployments don't need it |
| Audit-log export / SIEM | ⚠️ deferred (unscheduled) | append-only PG `credential_event` table for now |
| Versioned DEK rotation procedure documented | ✅ | spec §"DEK compromise / rotation" |
| Sudo-gating posture: mutations gated, reads are admin-role only | ✅ smoke-verified; ✅ unit-tested | `hasFreshSudo` (`pkg/server/handle_sudo.go`) is the single chokepoint for all 🔐 admin mutations, applied via `registerSudoOpHTTP` (raw HTTP) and `registerSudoOp` (typed Huma). The gate is a pure read against the session — it checks the recent-auth window and `SudoUntil` without consuming anything (multi-use). The latter gates the account/invitation lifecycle ops — incl. `UpdateAccount` (user→admin escalation), which were previously admin-role-only. Guarded by `TestAdminMutationRoutesRequireSudo` (`pkg/server/admin_route_policy_test.go`), which serves the REAL `registerOperations()` routes and asserts `sudo_required` (401) on every mutation when the session has no fresh sudo grant. |
| Signing-key lifecycle states (`pending`→`active`→`decommissioning`→`retired`) | ✅ smoke-verified | migration `008_signing_key_lifecycle.sql`; partial unique index `one_active_signing_key (use) WHERE status='active'`; publish set = pending+active+decommissioning; activate demotes prior active→decommissioning + promotes target; background reconcile loop flips decommissioning→retired. Smoke steps 117 (snapshot 1 active key), 118 (generate→pending; JWKS publishes both; old key still signs), 119 (activate→new active + old decommissioning; prior-key tokens STILL verify in grace). Legacy `active` bool + `retired_at` retained; `009` (drop legacy columns) is a deferred follow-up. |
| No secret / key material in audit `detail` JSONB | ✅ smoke-verified | write-site invariant: every admin mutation handler constructs `detail` from redacted fields only (client_id, display_name, kid, etc — never hash, secret_enc, private_pem). Smoke step 120 calls `assertAuditDetailNoSecret` on every returned event. |

## Rate limiting policy (v0.3 audit)

IP-keyed rate limits were removed from all auth/federation/enrollment/pairing
HTTP handlers in v0.3 (audit finding M5). Rationale:
`sessstore.ClientIP(r, TrustProxy)` cannot reliably identify a client behind
NAT, CDN, or corporate egress — the resulting per-IP buckets created both
false positives (legitimate users sharing an IP locked out) and false
negatives (an attacker rotating IPs trivially bypasses the cap). What
remains:

- **Account/session-keyed rate limits** — preserved: `pair_lookup:acct:`,
  `pair_approve:acct:` (handle_pairing.go), and `sudo:acct:` (handle_sudo.go,
  2 spots). Keyed on `sess.Account.ID` or `sess.Data.SessionID`; immune to IP
  rotation.
- **`auth_throttle` table** — preserved: per-(account, factor) DB-backed
  lockout state machine for password / TOTP / recovery-code attempts.
  Protects against password-spray once the attacker has a target username.

Public surfaces without account context (`/auth/password/begin`,
`/auth/login/{begin,complete}`, `/auth/federation/<slug>/login`,
`/auth/federation/<slug>/callback`, `/auth/enrollment/<token>/begin`,
`/auth/devices/pair/begin`, `/auth/recovery/totp/{begin,verify}`) now rely
on PKCE + state-token single-use + KV TTL for replay protection, and on
`auth_throttle` once a credential failure occurs against a known account.
No DoS protection at the HTTP edge — that belongs to the deployment's
reverse proxy or WAF.

## Web (frontend, v0.6)

| Item | Status | Notes |
|---|---|---|
| Passkey ceremony popup with focus trap + Esc + backdrop-click | ⚠️ deferred (v0.6) | dashboard not yet ported |
| `AbortController` on `navigator.credentials.{create,get}` | ⚠️ deferred (v0.6) | same |
| Body scroll lock during ceremony | ⚠️ deferred (v0.6) | same |
| WCAG 2.1.2 No Keyboard Trap | ⚠️ deferred (v0.6) | same |
| Concurrent ceremony preemption | ⚠️ deferred (v0.6) | SDK aborts prior |
| Method-selection login UX | ⚠️ deferred (v0.6) | WebAuthn vs password+TOTP vs federation |
| CSRF on state-changing `/me/*` | ✅ | `SameSite=Lax` session cookie + same-origin |
| Conditional UI (passkey autofill) | ⚠️ deferred (unscheduled) | identifier-less login |
| Content Security Policy (CSP) | ✅ | set in `pkg/webui/webui.go` (CSP + `X-Frame-Options: DENY` + `X-Content-Type-Options: nosniff`) |
| HSTS | ⚠️ deferred | TLS-layer header; set at the reverse proxy that terminates HTTPS |

## Threats this codebase does NOT protect against (v0.x)

- **Combined DB + environment compromise of signing keys.** Signing
  private keys are DEK-sealed at rest (`signing_key.private_pem_enc`), so a
  DB-only compromise does not yield them — but the DEK lives in the
  environment, so an attacker holding BOTH the database and the env (or
  process memory) can still decrypt them. KMS/HSM-backed signing (AWS KMS /
  GCP KMS / Vault Transit), where the key never leaves the vault, is
  optional production hardening (unscheduled).
- **Loss of all DEK versions.** TOTP secrets and upstream-OIDC
  client secrets become undecryptable; users must re-enroll TOTP and
  re-link upstream IdPs. Operator responsibility: keep at least two
  consecutive `PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>` versions
  available during rotation.
- **Insider abuse via direct DB access.** A SQL operator can grant
  themselves any role / attributes, mint sessions, or extract signing
  keys. Standard IdP threat — mitigate with DB access controls +
  audit-log monitoring.
- **Sustained credential-stuffing against `/oauth/token`.** v0.4 adds
  per-`client_id` rate limiting at `/token` (and per-`account_id` /
  per-`sub` at `/authorize` / `/userinfo`) — but these are
  per-identity, in-process buckets, not edge DoS protection. A
  reverse-proxy / WAF still owns volumetric defense, and the
  multi-replica in-process-limiter caveat from the v0.2 deployment
  notes applies to the OIDC buckets too.
- **Phishing of federated upstream credentials.** Prohibitorum can
  only validate the assertion the upstream IdP returns; it doesn't
  control how the upstream IdP authenticates the user. Pick upstream
  IdPs whose phishing-resistance matches your threat model.
- **Compromise of an SP signing cert (Pattern C).** If a SAML SP's
  signing cert leaks, an attacker can forge AuthnRequests from that
  SP — but they can't forge our signed `<Response>` without our
  signing key, so the blast radius is "spoof the SP's identity to us"
  rather than "log in as a user."

Each gap is tracked in `STATUS.md` with a target version.

## v0.4 post-implementation audit (done)

After all 17 v0.4 tasks landed and the smoke went green end-to-end, the
v0.4 OIDC OP surface (`pkg/protocol/oidc/*`, the two CLIs, server wiring)
was put through a parallel **3-lens audit** (crypto / protocol-standards /
race-logic) plus a **deep second pass** (integration / data-integrity /
schema-drift). **No Critical issues.** The core machinery was independently
confirmed sound: RS256 alg-allowlist (rejects `alg:none`/HS256/confusion),
RFC 7638 thumbprint-bound `kid` resolution, constant-time PKCE S256 verify,
constant-time argon2id client-secret verify, 256-bit `crypto/rand` for all
tokens/codes/jti/secrets, correct `at_hash`, leak-free JWKS, race-free key
cache, atomic single-use codes (`Pop`), refresh rotation + reuse→family-revoke
(self-healing, no resurrection path), account-bound logout, correct
schema↔sqlc↔code types, shared db/kv instances, collision-free `oidc:*` KV
namespacing, and consistent `factor=oidc_client` auditing.

**Fixed during the audit** (commits `63fe605`, `fef913b`):
- **[High]** `/oauth/authorize` nil-pointer panic for a disabled-mid-session
  account — the bare route mount skipped `authn.Check`, so the disabled
  sentinel session (`Data==nil`) passed the `sess==nil` guard and the
  `sess.Data` deref panicked. Guard widened to `sess==nil || sess.Data==nil ||
  (sess.Account!=nil && sess.Account.Disabled)` → login bounce. (unit-tested)
- **[High]** RFC 6749 §5.2 — `invalid_client` 401 now carries
  `WWW-Authenticate` when Basic auth was used.
- **[High]** rate-limit 429s no longer use the misleading `server_error`
  OAuth code → `temporarily_unavailable`.
- **[Medium]** `validateAccessToken` now asserts `aud == issuer` (was a latent
  confused-deputy hole, masked by the single-audience design).
- **[Medium]** `revoked_jti` denylist is now pruned hourly by a background
  goroutine in `Serve()` (`PruneExpiredRevokedJTI` previously had no caller →
  unbounded growth).
- **[Low]** `/oidc/logout` rejects an access token (`typ:at+jwt`) presented as
  an `id_token_hint`.
- **[Low/availability]** refresh grant fails closed (revokes the family) if
  token minting fails after a rotation, instead of locking the client out.

**Accepted / deferred (tracked, not blocking v0.4):**
- `prompt=login` / `max_age` are not honored (silently ignored) — no step-up /
  forced-reauth yet. **CLOSED in v0.6** (D1–D4; smoke 100–102): both are now
  honored via a full fresh re-login + a single-use KV nonce freshness gate.
  Consent UI is still deferred (`require_consent` fails closed with
  `consent_required`).
- `oidc_client.require_pkce` and `allowed_code_challenge_methods` columns are
  stored but not consulted — **CLOSED in v0.6** (D6; smoke 103):
  `/oauth/authorize` now consults both, and `plain` is excluded by a DB CHECK.
- `none` is advertised for the introspection/revocation auth methods; public
  clients can introspect/revoke **their own** tokens. **CLOSED in v0.6** (D7;
  smoke 104): public clients may NO LONGER introspect (→ `invalid_client`, RFC
  7662); they may still revoke their own tokens (RFC 7009, unchanged).
- Client-id **timing oracle**: the unknown-client path returns before the
  argon2id verify, leaking client-id existence via latency (client-ids are
  semi-public; secrets are safe). Equalize with a dummy verify when hardened.
- The code-replay→family-revoke marker is written after minting and is
  best-effort, so a *concurrent* replay during the mint window (PKCE still
  protects passive interceptors) escapes family revocation — single-use itself
  still holds. The refresh concurrent-rotation race is non-immortalizing
  (self-heals via reuse detection); a fully atomic fix needs a KV
  compare-and-swap the `Store` interface doesn't expose.

## v0.6 post-implementation audit

### Post-implementation audit (2026-05-31) — done

A parallel 4-lens audit ran after all 11 v0.6 tasks shipped — crypto/XML-DSig +
protocol-standards + race/logic + deep integration/data/schema-drift, focus on
the re-auth freshness gate, introspection auth, NameIDPolicy, and IdP-initiated
guardrails. **No Critical, no real High in v0.6's own code.** The crypto lens
confirmed every new signature path reuses the hardened `xmlsec.go` primitives
(cert-pinned, RSA-SHA256-allowlisted, XSW-defended) rather than rolling its own;
the protocol lens confirmed the two most interop-sensitive choices —
`Requester`/`InvalidNameIDPolicy` and the unsolicited-Response shape — are
correct against real IdP/SP behavior. Findings fixed across two batches:

- **Batch A `c1523a0` — re-auth gate hardening (race + deep lenses converged):**
  the KV marker now binds to the **account** (`<accountID>|<instant>`, not just
  the instant) and `ConsumeReauth` rejects an account mismatch + uses an atomic
  `Pop` (was a non-atomic Get→Del). Removes the footgun where a leaked nonce +
  any fresh session could satisfy a demand, and matches the codebase's existing
  single-use `Pop` pattern.
- **Batch B `5643e35` — five independent fixes:** (1) **deep-H1** — `oidc-client
  create` without `--post-logout-redirect-uri` crashed on the
  `post_logout_redirect_uris` NOT NULL (affected ALL clients, not just
  `--public`; `BuildClientParams` now defaults to `[]string{}`); (2) **proto-M1**
  — SAML `NoPassive` top-level status changed `Requester`→`Responder` (Google/
  SAML-community norm; SPs key on the 2nd-level `NoPassive`); (3) `sloParseError`
  now maps `errBadSigAlg`→400 (was 500; matches `ssoParseError`); (4)
  `SAMLConfig.MetadataValidity<=0` falls back to 24h (no born-stale metadata);
  (5) the token endpoint gates `verifyPKCE` on a stored challenge so a
  `require_pkce=false` no-PKCE code can exchange (a `require_pkce=true` client
  always has a challenge — no PKCE weakening). Full suite + the end-to-end smoke
  re-ran green after both batches.

**Accepted / deferred (tracked, not blocking v0.6):**
- `max_age` freshness is evaluated WITHOUT clock-skew leniency (fails *stricter*,
  never looser; the id_token `auth_time` the RP validates is the real value) —
  documentation-vs-D3 drift, no defect.
- `prompt=consent` / `prompt=select_account` are parsed but ignored (consent UI
  is out of scope); `prompt=none` is only rejected when combined with `login`,
  not with the other (unimplemented) interaction prompts. Cosmetic.
- Signed-metadata uses two unsynchronized signing-key cache reads; a key rotation
  landing exactly between them could (transiently, operator-controlled) advertise
  a cert set excluding the signer. Extremely narrow; next fetch is consistent.
- `ForceAuthn` + POST-binding AuthnRequest: the re-auth bounce rebuilds
  `return_to` from the query string, but a POST AuthnRequest body isn't in the
  query → the post-login return has no `SAMLRequest` → fails SAFE (400, no
  wrong-issue, no panic). Degenerate combination; documented limitation.
- Front-channel SLO propagation + assertion/NameID encryption remain out of scope
  (carried from v0.5).

### ✅ Architectural finding RESOLVED (2026-05-31) — session-cookie scoping

The session cookie was scoped `Path=/api/prohibitorum` while the OIDC/SAML
protocol routes are root-level (`/oauth/authorize`, `/saml/sso`, `/saml/sso/init`,
`/saml/slo`), so a real browser never attached the cookie to those paths and the
session gate (`HandleAuthorize` / `HandleSSO` / `HandleIdPInitiated`) looped to
`/login`. **Fixed:** the session cookie is now `Path=/` with a deployment-stable
identity — `__Host-prohibitorum_session` + `Secure` in HTTPS deployments, plain
`prohibitorum_session` (no `Secure`) in HTTP dev so `cookiejar`-based clients can
still send it. `SameSite=Lax`, `HttpOnly`, no `Domain` unchanged; no
route/issuer/metadata changes; ceremony cookie untouched. Name resolution is
centralized in `pkg/session/middleware.go` (`SessionCookieNameFor`) and reused at
the logout-read and the OpenAPI security scheme. Spec:
`docs/superpowers/specs/2026-05-31-session-cookie-scoping-design.md` (D1–D5).

**Verification:** attribute-level unit tests in `pkg/session/middleware_test.go`
(both deployment modes; clear-matches-set; name resolution incl. empty-origin),
and `cmd/smoke` now DROPS its manual cookie re-attach — all six OIDC/SAML helpers
let the jar auto-send the `Path=/` cookie to the root-mounted endpoints
(browser-equivalent), with `assertSessionCookieAtRoot` proving the scoping (it
matches either the plain or `__Host-` name, so it cannot pass vacuously). Full
suite incl. steps 100–111 green, `SMOKE_EXIT=0`. A real-browser HTTPS end-to-end
run is out of scope (no browser harness); the verification is the unit tests plus
the dev-mode behavioral smoke, and production browser behavior follows from
`Path=/` + `SameSite=Lax` per the web-platform spec.

**Carried-forward limitation (D2):** a logged-in user hitting a SAML
**HTTP-POST-binding** AuthnRequest is a cross-site POST, so the `SameSite=Lax`
cookie is not sent and the user bounces through `/login` once — same family as the
deferred `ForceAuthn`+POST-binding item. `SameSite=None` was rejected (broader
cross-site exposure, requires always-`Secure`, increasingly browser-restricted).

All 12 v0.6 smoke steps (100–111) are green end-to-end (`SMOKE_EXIT=0`).
The behaviors closed by v0.6 are flipped to ✅ in the OIDC OP and SAML IdP
tables above, each carrying its smoke-step reference.

**Mechanisms recorded for audit:**

- **Forced-re-auth freshness gate (D1/D2/D5).** Shared `pkg/authn`
  helper (`DemandReauth`/`ConsumeReauth`): on a re-auth demand it stamps a
  single-use KV marker `<proto>:reauth:<nonce> = <demand_instant>` (10m TTL,
  prefixes `oidc:reauth:` / `saml:reauth:`), embeds the nonce in the
  `/login?return_to=…&reauth=<nonce>` bounce, and on return requires the
  marker to still exist AND `session.auth_time >= demand_instant`, then
  consumes it (single-use). A stale pre-existing session's `auth_time`
  post-dates nothing — it predates the demand — so it structurally cannot
  satisfy `prompt=login` / `ForceAuthn`. Unit-tested in
  `pkg/authn/reauth_test.go` (stale session rejected; nonce single-use;
  expired marker re-demands; empty/never-issued nonce rejected).
- **IdP-initiated SSO guardrails (D11).** Per-SP opt-in via
  `saml_sp.allow_idp_initiated` (default false) — a non-opted-in SP → 403;
  delivery only to the SP's registered DEFAULT ACS (open-redirect guard,
  same as SP-initiated); rate-limited per-account + per-SP; `RelayState`
  passed verbatim as the deep-link target; audited `reason=idp_initiated`.
  The inherently weaker login-CSRF posture (an unsolicited Response has no
  `InResponseTo`, SAML Profiles §4.1.5) is the documented trade-off, mitigated
  by the short assertion validity window + SessionIndex + AudienceRestriction
  and the default-off posture (mirrors GHES).

**Accepted / deferred (tracked during v0.6 implementation, not blocking v0.6):**

- **`require_pkce=false` + no `code_challenge` cannot complete token exchange.**
  A pre-existing v0.4 behavior surfaced during Task 3: `verifyPKCE`
  (`pkg/protocol/oidc/token.go`) rejects an empty challenge, so a
  `require_pkce=false` client that sends NO PKCE gets `invalid_grant` at
  `/oauth/token`. Only affects non-default clients (default `require_pkce=true`).
  Deferred — the fix is to skip PKCE verification when no challenge was stored.
- **`sloParseError` omits `errBadSigAlg`.** A SLO POST LogoutRequest with a
  non-SHA256/non-SHA1 sig alg maps to 500 instead of 400 (the SSO path's
  `ssoParseError` was fixed to include it; SLO's was not). Cosmetic — still
  rejects. Deferred.
- **`ForceAuthn` + POST-binding AuthnRequest.** The re-auth bounce rebuilds
  `return_to` from the query string, but a POST-binding AuthnRequest body is
  not in the query, so after the login bounce the return GET has no
  `SAMLRequest` and fails safe with an error. Degenerate combination
  (POST-binding SPs rarely also set ForceAuthn). Deferred / documented
  limitation.
- **`oidc-client create --public` requires `--post-logout-redirect-uri`.** The
  public path passes `nil` post-logout URIs, violating the NOT NULL column;
  workaround is to supply one (the smoke does this at
  `cmd/smoke/main.go:4064` `createPublicOIDCClient`). Deferred CLI ergonomics
  fix (default to an empty array).
- **Front-channel multi-SP SLO** and **assertion / NameID encryption** remain
  out of scope (carried from v0.4/v0.5; `saml_sp_key.use='encryption'` reserved
  but unused).
