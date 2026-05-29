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
  realistic deployment; sweep tooling is v0.7+).
- OIDC `auth_time` vs sudo semantics (decision needed before the v0.4
  token endpoint goes live; default-safe option named in the notes).
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
| Full attestation-object retention for MDS3 validation | ⚠️ deferred (v0.7+) | credentials/Optional |
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
| Client secret AES-GCM encrypted with versioned DEK | ✅ smoke-verified | `pkg/federation/oidc/secret.go`; smoke step 46/65 seeds via `oidc.SealClientSecret` |
| AAD bound to row identity (`'upstream_idp:'||id||':'||key_version`) | ✅ implemented | `pkg/federation/oidc/secret.go` AAD format; 5/5 unit tests in `secret_test.go` including a cross-row-paste rejection case |
| JWT alg allowlist (RS256/ES256/EdDSA only; `HS256` / `none` rejected) | ✅ implemented | `pkg/federation/oidc/client.go` `DefaultAllowedAlgs()`; library-level enforcement + post-decode re-check |
| Three provisioning modes (`auto_provision` / `invite_only` / `link_only`) | ✅ all smoke-verified | `auto_provision` (steps 47–50/69), `link_only` (step 59/69), `invite_only` (steps 65–66/69) |
| `auto_provision` gated by `require_verified_email` + `allowed_domains` + username collision | ✅ smoke-verified | `pkg/federation/oidc/modes.go` `applyAutoProvision`; steps 53/69 (email_not_verified), 54/69 (username_collision) |
| `invite_only` mode end-to-end (token-bearing redemption) | ✅ smoke-verified | `pkg/federation/oidc/modes.go` `applyInviteOnly` + `pkg/federation/oidc/federation.go` `BeginInviteRedemption` + `pkg/server/handle_invite_federation.go`; step 65/69 drives `GET /enrollments/{token}/start-federation` → upstream `/authorize` → callback → 302 `/me`; step 66/69 DB-asserts `enrollment.consumed_at`, account + identity rows, and `credential_event[register reason=invite_only_redemption]` |
| `invite_only` rejects consumed token | ✅ smoke-verified | step 67/69: re-driving the consumed token through `/start-federation` returns 403 `invite_required` pre-flight (no upstream hop); `failNoAccount("invite_already_consumed")` audited |
| `invite_only` rejects expired token | ✅ smoke-verified | step 68/69: enrollment seeded with `expires_at = now() - interval '1 second'` returns 403 `invite_required`; `failNoAccount("invite_expired")` audited |
| Invite redemption is single atomic transaction (consume+account+identity+audit) | ✅ implemented | `pkg/federation/oidc/modes.go` `applyInviteOnly` wraps via `runInviteTx` + `pkg/server/server.go` passes `pgxpool`. Audit `Writer` is tx-scoped (`audit.NewWriter(qtx)`) so `credential_event.account_id` FK resolves against the just-inserted account row and rollback reverts audit too. Smoke step 66/69 is the regression gate for the audit-FK bug fixed in stage 3 |
| `account_identity` keyed on `(upstream_iss, upstream_sub)` (OIDC Core §2) | ✅ smoke-verified | UNIQUE constraint in migration 004; step 51/65 DB-asserts row insertion; step 62/65 asserts ownership |
| Federation state snapshots `expected_iss` + `expected_token_endpoint` | ✅ implemented | `pkg/federation/oidc/federation.go:190` populates `ExpectedIss`; library uses it on `client.Exchange` (mix-up resistance) |
| Single-use federation state via atomic Pop | ✅ implemented | `pkg/federation/oidc/federation.go:220` `kvStore.Pop(LoginKey(stateToken))`; unit-tested |
| Cross-namespace defense (LoginKey != LinkKey) | ✅ implemented | `pkg/federation/oidc/federation.go:202–204`; unit-tested — a `LoginKey`-stashed state cannot be redeemed via the link callback |
| RFC 9207 `iss` callback param validated against `state.ExpectedIss` | ✅ implemented | `pkg/federation/oidc/federation.go:231` (HandleCallback) + 317 (LinkCallback); unit-tested |
| Strict issuer + audience + nonce validation on upstream ID token | ✅ implemented | `pkg/federation/oidc/client.go` via `zitadel/oidc/v3 v3.47.5`; nonce threaded via context-key |
| Disabled-account re-check after Resolve | ✅ implemented | `pkg/federation/oidc/federation.go:269–278` — returns `authn.ErrBadCredentials()` (enumeration-safe, same path as password login) |
| `email_verified` gating per IdP (`require_verified_email` column) | ✅ smoke-verified | migration `006_federation_v03.sql`; `modes.go:110`; step 53/65 |
| AMR pass-through from upstream + backfill to `["federated"]` when omitted | ✅ implemented | `pkg/server/handle_federation.go:127–130` (RFC 8176 §2 rationale) |
| Per-IdP claim-name overrides (`username_claim` / `display_name_claim` / `email_claim`) | ⚠️ schema only | columns exist in `upstream_idp` (migration 004) but `pkg/federation/oidc/modes.go` reads `tokens.PreferredUsername` / `tokens.Name` / `tokens.Email` directly and never consults the per-IdP override. Benign for OPs that use the default claim names; closing the gap requires plumbing overrides through `client.Exchange` or `modes.go` |
| RP flow implementation (BeginLogin / HandleCallback / LinkBegin / LinkCallback) | ✅ smoke-verified | `pkg/federation/oidc/federation.go`; steps 47–49, 61, 64/65 |
| Local-username collision policy on JIT auto-provision | ✅ smoke-verified | `modes.go:135–142`; step 54/65 |
| Link-flow session-swap defense | ✅ implemented | `pkg/federation/oidc/federation.go:307–312` — `state.LinkingAccountID` must equal current session's account; unit-tested |
| Last-sign-in-method check on unlink | ✅ implemented | `pkg/server/handle_me_identities.go:121–145`; computes post-unlink method set via `authn.AvailableMethods`; unit-tested (smoke-untested because the federated-only sudo path is unreachable) |
| Refresh-token storage for upstream tokens | ❌ gap | not implemented; federated users re-authenticate via `/login` each time. Revisit if `/me` ever needs to refresh upstream profile claims out-of-band |

## OIDC OP downstream (RFC 6749 / OIDC Core / RFC 9068 / RFC 9700 / RFC 9207 / RFC 8414 / RFC 7636 / RFC 7009 / RFC 7662 / RP-Initiated Logout 1.0)

| Item | Status | Notes / source |
|---|---|---|
| Authorization Code + PKCE only | ✅ schema | `oidc_client.require_pkce` defaults true; CHECK forbids `plain` via allowlist |
| PKCE required for **all** clients (incl. confidential) | ✅ schema | `require_pkce` default true; admin can not turn off in v0.6+ CHECK |
| `code_challenge_method` allowlist rejects `plain` | ✅ schema | oidc/R2; `allowed_code_challenge_methods text[]` default `{S256}` |
| `redirect_uri` exact-match (no wildcards) | ✅ schema | `oidc_client.redirect_uris text[]` |
| `post_logout_redirect_uris` exact-match list | ✅ schema | oidc/C1; `oidc_client.post_logout_redirect_uris` |
| Single-use authorization codes with replay revocation | ✅ design | oidc/C8; spec §"Authorization-code lifecycle" — `consumed_at`, revoke family on replay, audit |
| `iss` parameter in authorization response (RFC 9207) | ✅ design | spec §"HTTP surface"; discovery advertises support |
| Discovery doc (RFC 8414 / OIDC Core) | ✅ stub | `/.well-known/openid-configuration` mounted; advertises planned v0.4 endpoints; `claims_supported` lists `sub/iss/aud/exp/iat/nonce/auth_time/amr/acr/username/displayName/role/attributes` |
| JWKS endpoint | ✅ stub | `/oauth/jwks` mounted; returns empty `keys` array until v0.4 mints signing keys |
| ID token signed with asymmetric alg | ✅ design | RS256; ES256 / EdDSA possible via `signing_key.algorithm` |
| `alg: none` rejected | ✅ design | jwt verification configured for RS256 only |
| ID token claims: signature, `iss`, `aud`, `exp`, `nonce` validated | ✅ design | INTEGRATION.md |
| ID token `auth_time` claim (OIDC Core §2) | ✅ schema | oidc/C2; `session.auth_time` populated on every `sessionStore.Issue` (writer wired; smoke-verified ≥3 rows per account); ID-token mint that emits the claim lands in v0.4 |
| ID token `amr` / `acr` claims | ✅ schema | oidc/C2; `session.amr` populated (WebAuthn → `["hwk"]`; smoke-verified all rows); `session.acr` reserved for v0.2+; ID-token mint v0.4 |
| ID token `azp` when `aud` is multi-valued | ✅ design | oidc/C5; spec §"Access-token issuance" |
| ID token `at_hash` (defense in depth) | ✅ design | oidc/C5; spec |
| `sid` claim sourced from `session.id` | ✅ schema | `session.id` PK; rows inserted on `Issue` (smoke-verified); revoked on `Revoke` (smoke-verified), `RevokeBySessionID` (smoke-verified), `RevokeAllForAccount` (wired, smoke-untested — admin endpoint); claim emission v0.4 |
| RFC 9068 access token `typ: at+jwt` | ✅ design | oidc/C4 |
| RFC 9068 required claims (`iss`, `sub`, `aud`, `exp`, `iat`, `jti`, `client_id`, `scope`) | ✅ design | oidc/C3 |
| `jti` revocation via denylist | ✅ schema | oidc/C3; `revoked_jti` |
| Refresh tokens single-use rotation + reuse detection | ⚠️ deferred (v0.4) | family-revocation logic |
| Refresh tokens stored server-side (opaque) | ✅ design | KV-backed |
| Access tokens short-lived (≤ 15 min) | ✅ design | `configx.OIDC.AccessTokenTTL` default 10m |
| Refresh tokens 30 day default | ✅ design | `configx.OIDC.RefreshTokenTTL` |
| `offline_access` scope gates refresh issuance (OIDC Core §11) | ⚠️ deferred (v0.4) | oidc/R3 |
| argon2id hashing for `client_secret_hash` | ✅ design | `golang.org/x/crypto/argon2` |
| `token_endpoint_auth_method` (`client_secret_basic` default, `none` for public) | ✅ schema | oidc/R1; `oidc_client.token_endpoint_auth_method` |
| `id_token_signed_response_alg` per client | ✅ schema | oidc/R1 |
| `subject_type` (`public` / `pairwise`) | ✅ schema | oidc/R1 |
| `application_type` (`web` / `native`) | ✅ schema | oidc/R1 |
| `default_max_age` / `require_auth_time` per client | ✅ schema | oidc/R1 |
| `contacts` / `logo_uri` / `tos_uri` / `policy_uri` | ✅ schema | oidc/R1 |
| Token introspection (RFC 7662) — `active`, `sub`, `scope`, `client_id`, `exp` | ⚠️ deferred (v0.4) | oidc/R6; endpoint stubbed |
| Token revocation (RFC 7009) | ✅ schema; ⚠️ endpoint v0.4 | `revoked_jti` writes |
| Pushed Authorization Requests (PAR, RFC 9126) | ⚠️ deferred (v0.7+) | not required for v1 first-party clients |
| JAR (RFC 9101) | ⚠️ deferred (v0.7+) | same |
| DPoP (RFC 9449) sender-constrained tokens | ⚠️ deferred (v0.7+) | bearer for v1 |
| mTLS (RFC 8705) | ⚠️ deferred (v0.7+) | bearer for v1 |
| Dynamic client registration (RFC 7591) | ⚠️ deferred (v0.7+) | static config in v0.x |
| Pairwise sub identifiers | ⚠️ deferred (v0.7+) | `subject_type='pairwise'` column reserved |
| Encrypted ID tokens (JWE) | ⚠️ deferred | TLS provides confidentiality on the wire |
| RP-Initiated Logout 1.0 | ⚠️ deferred (v0.4) | `oidc/C1` schema; endpoint stubbed |
| Front-channel / back-channel logout | ⚠️ deferred (v0.7+) | multi-RP coordinated sign-out |
| Mix-up attack resistance | ✅ design | `iss` param (RFC 9207) + federation state snapshots |
| Refresh-token family forensics table | ⚠️ deferred (v0.4) | oidc/R7; KV-only for v0.4 |
| Rate limit on `/oauth/authorize` and `/oauth/token` | ❌ gap | flagged by audit-oidc.md indirectly; tracked for v0.4 |

## SAML IdP (SAML 2.0 Core / Bindings / Metadata / Profiles)

| Item | Status | Notes / source |
|---|---|---|
| SP registry with entity ID, NameID format, attribute map | ✅ schema | `saml_sp` |
| Multi-endpoint ACS (Metadata §2.4.4) | ✅ schema | saml/C1; `saml_sp_acs` child table |
| ACS URL validated by exact match → index lookup → is_default | ✅ design | saml/C1; spec §"SAML assertion construction" |
| Multiple SP signing/encryption certs per SP (rotation-friendly) | ✅ schema | saml/C3; `saml_sp_key (sp_id, use)` |
| `require_signed_authn_request` per SP | ✅ schema | saml/C3; `saml_sp.require_signed_authn_request` |
| `want_assertions_signed` / `authn_requests_signed` mirror SP metadata | ✅ schema | saml/R4 |
| Both `<Response>` and `<Assertion>` signed | ✅ design | saml/GHES-1; spec §"SAML assertion construction" |
| `Destination` on `<Response>` = chosen ACS URL | ✅ design | saml/GHES-2 |
| `<SubjectConfirmationData Recipient>` = chosen ACS URL | ✅ design | Profiles §4.1.4.2 |
| `<Audience>` = `saml_sp.entity_id` verbatim | ✅ design | saml/C2 |
| Stable pairwise NameID (Core §8.3.7) | ✅ schema | saml/C5; `saml_subject_id (account_id, sp_id)` |
| Persistent 1.1-namespace NameID default | ✅ schema | saml/C4; `saml_sp.name_id_format` default `urn:oasis:names:tc:SAML:1.1:nameid-format:persistent` |
| Attribute map as ordered JSONB array (multi + URI NameFormat) | ✅ schema | saml/R1; `saml_sp.attribute_map jsonb` |
| Per-SP `session_lifetime` for `SessionNotOnOrAfter` | ✅ schema | saml/GHES-8 |
| Metadata freshness fields (`metadata_*`) | ✅ schema | saml/R3 |
| `AuthnContextClassRef` per spec (`PasswordProtectedTransport` / `unspecified`) | ✅ design | saml/R5 |
| IdP metadata publishes all live + grace-period signing keys | ✅ design | saml/R6; `configx.SAML.MetadataRotationGrace` |
| GHES `sp_kind='ghes'` auto-sets `require_signed_authn_request=true` | ✅ design | saml/GHES-10 |
| GHES `emails` / `public_keys` / `gpg_keys` multi-valued | ✅ schema | saml/GHES-6; `attribute_map.multi=true` |
| GHES `public_keys` URI NameFormat support | ✅ schema | saml/GHES-7; `attribute_map.name_format='uri'` |
| GHES `administrator` attribute literal | ✅ design | saml/GHES-5; documented in INTEGRATION.md |
| XML signature wrapping (XSW) defense | ✅ planned | crewjam/saml post-canonicalization verification; v0.5 |
| Assertion replay (NotBefore / NotOnOrAfter / InResponseTo / one-use Assertion ID) | ✅ planned | crewjam/saml enforces; v0.5 |
| `saml_session` populated from day one for SLO forward-compat | ✅ schema | spec §"db/migrations/005_saml.sql" |
| Single Logout (SLO) endpoint | ⚠️ deferred (v0.5) | `/saml/slo` stubbed |
| SLO endpoint binding child table (`saml_sp_slo`) | ⚠️ deferred (v0.5) | saml/R2 |
| IdP-initiated SSO | ⚠️ out of scope | saml/Optional |
| AttributeQuery / NameIDMapping / Artifact binding | ⚠️ out of scope | saml/Optional |
| `default_relay_state` per SP (only if IdP-initiated lands) | ⚠️ out of scope | saml/Optional |
| Encrypted assertions (`saml_sp_key.use='encryption'`) | ⚠️ deferred (v0.7+) | schema room ready |
| Implementation | ⚠️ deferred (v0.5) | `pkg/protocol/saml` stubbed |

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
| HSM / KMS integration | ⚠️ deferred (v0.7+) | private keys in DB column |
| TLS termination | external | reverse-proxy responsibility; Prohibitorum sets `Secure` cookie when TLS detected |
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
| Admin UI for OIDC clients / SAML SPs / upstream IdPs | ⚠️ deferred (v0.6) | manage via SQL until then |
| Consent screen | ⚠️ deferred | first-party-only deployments don't need it |
| Audit-log export / SIEM | ⚠️ deferred (v0.7+) | append-only PG table for now |
| Versioned DEK rotation procedure documented | ✅ | spec §"DEK compromise / rotation" |

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
| Conditional UI (passkey autofill) | ⚠️ deferred (v0.7+) | identifier-less login |
| Content Security Policy (CSP) | ❌ gap | reverse-proxy responsibility for v0.x; bake into static handler in v0.7+ |
| HSTS, X-Frame-Options, etc. | ❌ gap | same |

## Threats this codebase does NOT protect against (v0.x)

- **HSM-tier private key protection.** RSA private keys live in
  `signing_key.private_pem`. A DB compromise = a complete IdP
  compromise (signing + DEK-encrypted secrets). Move to KMS-backed
  signing (AWS KMS / GCP KMS / Vault Transit) for production — v0.7+.
- **Loss of all DEK versions.** TOTP secrets and upstream-OIDC
  client secrets become undecryptable; users must re-enroll TOTP and
  re-link upstream IdPs. Operator responsibility: keep at least two
  consecutive `PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>` versions
  available during rotation.
- **Insider abuse via direct DB access.** A SQL operator can grant
  themselves any role / attributes, mint sessions, or extract signing
  keys. Standard IdP threat — mitigate with DB access controls +
  audit-log monitoring.
- **Sustained credential-stuffing against `/oauth/token`.** Rate
  limiting on OIDC endpoints is gap-flagged above; reverse-proxy WAF
  is the short-term mitigation.
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
