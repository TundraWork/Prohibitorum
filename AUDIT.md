# Audit — OAuth 2.1 / OIDC / WebAuthn / SAML / NIST best-practice checklist

Current codebase vs. authoritative standards. Status labels:

- **✅** — implemented end-to-end (code enforces it today)
- **✅ schema** — DB column/table exists; no Go path reads/writes it yet
- **✅ design** — locked in spec; no schema or code yet
- **✅ stub** — handler mounted; returns 501 or partial output
- **⚠️ deferred** — intentional omission
- **❌ gap** — unfinished; needs work
- **❌ explicitly forbidden** — the standard forbids this (NIST §3.1.1.2 etc.)

A bare **✅** may still be schema-only — read the Notes column; suffix labels qualify the implementation depth.

Items below carry an audit-report ID (e.g. "credentials/C1") when traceable.

Known operational caveats:

- In-process rate limiter (multi-replica multiplier — mitigate via LB affinity or external WAF).
- AES-GCM DEK rotation budget (out of reach for any realistic deployment; batch sweep tool unscheduled).
- OIDC `auth_time` semantics: id_token `auth_time` is `session.auth_time` — the original authentication moment, not the last sudo step-up.
- Password breach-list check (NIST SHALL gap, deferred; viable approaches named).
- `auth_throttle` shared across login + sudo (intentional defense-in-depth; documented for operator visibility).

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
| `cose_alg` persisted | ✅ | credentials/R1; extracted from COSE_Key CBOR by `pkg/credential/webauthn.COSEAlg`; written at both insert sites (`handle_enrollment.go`, `handle_me.go`) |
| `uv_initialized` persisted (L3 §4) | ✅ | credentials/C5; `webauthn_credential.uv_initialized` |
| `backup_eligible` / `backup_state` persisted | ✅ | `webauthn_credential.backup_eligible/state` |
| Full attestation-object retention for MDS3 validation | ⚠️ deferred | credentials/Optional |
| `created_via` provenance (registration / add / recovery) | ⚠️ deferred | credentials/Optional |

## Password (NIST SP 800-63B-4 draft)

| Item | Status | Notes / source |
|---|---|---|
| argon2id PHC string at rest (self-describing params) | ✅ | credentials/R5; `password_credential.hash` carries `$argon2id$v=19$…` |
| Per-row salt embedded in PHC | ✅ | argon2id PHC format; salt visible in the stored hash |
| `password_changed_at` distinct from `updated_at` | ✅ | credentials/R6; written by `handle_me_password.go` on every set |
| Configurable params (`PasswordHashParams`) with re-hash on verify | ✅ | configx defaults `m=65536KiB, t=3, p=1` (OWASP current); re-hash branch in `pkg/credential/password.Verify` unit-tested |
| Persistent failed-attempt counter (cross-restart) | ✅ | credentials/R4; `auth_throttle (account_id, factor='totp')` |
| Verify endpoint with throttle enforcement | ✅ | `/auth/password/begin` + `/auth/totp/verify`; lockout via sudo path; 429 + Retry-After |
| Username-enumeration defense (dummy argon2id verify on missing account) | ✅ | `pkg/credential/password.VerifyAgainstDummy` runs argon2id at store's current params; unit-tested in `handle_auth_password_test.go`. Params-upgrade timing-variance caveat documented on the function — old rows take longer until next rehash |
| Disabled-account rejected at `/auth/password/begin` after dummy verify | ✅ | `handle_auth_password.go`; unit-tested |
| Breach-corpus check (k-anonymity-style) on set | ⚠️ deferred | NIST §5.1.1.2 SHALL gap; viable approaches (HIBP k-anonymity + static blocklist) named |
| Periodic rotation forced | ❌ explicitly forbidden | NIST §3.1.1.2 — do not add |
| Password history | ❌ explicitly forbidden | NIST §3.1.1.2 — do not add |
| Composition rules (uppercase / digit / symbol) | ❌ explicitly forbidden | NIST §3.1.1.2 — do not add |
| No email channel for reset; admin enrollment-token only | ✅ design | enrollment intent `reset` |

## TOTP (RFC 6238 / RFC 4226)

| Item | Status | Notes / source |
|---|---|---|
| Secret entropy ≥ 160 bits | ✅ | `pkg/credential/totp` generates a 160-bit secret, base32-encoded in `/me/totp/begin` |
| AES-256-GCM at rest | ✅ | credentials/C3+C4; `secret_enc` + `secret_nonce` on enrollment; decrypts on verify. Decrypt failure collapses to `ErrTOTPCorrupt` so `/me/totp/verify` doesn't leak GCM auth-failure detail; server-side `credential_event` keeps `event=fail, detail.reason=decrypt_failed` |
| Versioned DEK (`key_version` per row) | ✅ | credentials/C3; `totp_credential.key_version`=1 set by `/me/totp/begin`; ciphertext readable on subsequent verifies |
| AAD bound to row identity (`'totp:'||account_id||':'||key_version`) | ✅ | credentials/C4; verify fails GCM auth if AAD differs between encrypt/decrypt |
| Per-row nonce (12 bytes from `crypto/rand`) | ✅ | `totp_credential.secret_nonce`; written on enrollment, consumed on verify |
| 30-second period, 6 digits | ✅ | RFC 6238 verify |
| SHA1 default (Google Authenticator interop) | ✅ | credentials/R3; HMAC-SHA1 |
| ±1 period drift tolerance | ✅ | `configx.TOTP.DriftSteps=1`; `Verify` checks T-1/T/T+1; unit-tested |
| `last_step` defeats same-step replay (RFC 6238 §5.2) | ✅ | credentials/C1; server rejects a same-step replay |
| `confirmed_at` gates the credential until first verify | ✅ | `confirmed_at IS NOT NULL` after `/me/totp/verify` |
| Persistent throttle (RFC 4226 §7.3) | ✅ | credentials/R4; wrong codes drive `auth_throttle.failed_attempts` to lockout (`locked_until>now`) → 429 |
| Exponential backoff schedule `[0,0,1s,2s,...,15m]` | ✅ | `pkg/authn/throttle`; schedule unit-tested |
| TOTP issuer / label format in QR codes | ✅ | `pkg/credential/totp` emits `otpauth://totp/{Issuer}:{username}?secret=…&issuer=…` |
| Single TOTP credential per account | ✅ | exactly 1 row in `totp_credential` |

## Recovery codes

| Item | Status | Notes / source |
|---|---|---|
| argon2id PHC at rest, per-row salt | ✅ | credentials/C2; `recovery_code.hash` populated by `/me/totp/verify` and `/me/recovery-codes/regenerate` |
| Single-use (`used_at` enforced) | ✅ | enforced on `/auth/recovery-code/verify`; recovery_code is no longer a sudo method |
| Shown exactly once at enrollment | ✅ | `/me/totp/verify`, `/me/recovery-codes/regenerate`, and the recovery ceremony's `/auth/recovery/totp/verify` all return cleartext once; server never persists |
| Redemption context captured (session id, IP) | ✅ | credentials/R7; `used_session_id` + `used_ip` written by the consume query |
| Mint count: 10 per account | ✅ | 10 at enrollment, 10 fresh post-ceremony, 10 on regenerate |
| Recovery code as one-shot recovery bootstrap (not continuous sudo factor) | ✅ | the only redeem path is `/auth/recovery-code/verify` → `recovery_session_token` → forced TOTP re-enrollment at `/auth/recovery/totp/{begin,verify}`; sudo-via-recovery-code dropped (NIST §5.2 — no knowledge-factor reauthentication); recovery_code not surfaced/accepted at `/me/sudo/*` |
| Recovery codes redeemable independently of TOTP | ✅ | `/auth/recovery-code/verify` consumed after `/auth/password/begin` (no TOTP). User re-enrolls TOTP via ceremony; `/begin` preserves unredeemed codes so a mid-ceremony abandon doesn't brick the account |
| Code redemption logic | ✅ | `pkg/credential/totp.VerifyRecoveryCode` |
| 80-bit entropy, formatted `XXXX-XXXX-XXXX-XXXX` | ✅ | `pkg/credential/totp.GenerateRecoveryCodes` |
| Regeneration invalidates the prior set | ✅ | regenerate returns 10 fresh codes; the ceremony wipes the surviving 9 atomically before minting 10 (audit: 9× `recovery_code:revoke` reason=`recovery_complete`) |

## Recovery ceremony

| Item | Status | Notes / source |
|---|---|---|
| `/auth/recovery-code/verify` returns `recovery_session_token`, NOT a session | ✅ | `pkg/server/handle_auth_password.go`; no session cookie + non-empty token |
| `recovery_session_token` is a narrow bearer scoped to two endpoints | ✅ | KV namespace `recovery_session:<tok>`, 10-min TTL, not accepted by `/me/*` or `/auth/totp/verify`; `pkg/server/handle_auth_recovery.go` |
| `/auth/recovery/totp/begin` wipes old TOTP but preserves recovery codes | ✅ | unconfirmed TOTP + 9 codes intact. Lets a user who abandons mid-ceremony retry with another code. `totp.Store.BeginPreservingRecovery` |
| `/auth/recovery/totp/verify` atomically consumes the token (kv.Pop) | ✅ | `TestRecoveryTOTPVerify_ParallelAtomic` (8-way race; at most one consumer); `handle_auth_recovery.go:popRecoverySession` |
| `/auth/recovery/totp/verify` first-confirm wipes prior recovery codes + mints fresh batch in one tx | ✅ | exactly 10 codes after verify; `totp.Store.VerifyAndCommitRecovery` shares its body with `Verify` via a private `verify(…, purgePriorRecoveryOnFirstConfirm)` helper |
| Disabled-account re-check on both ceremony endpoints | ✅ | `TestRecoveryTOTPBegin_AccountDisabledMidFlow` / `...Verify...`; mid-ceremony disable collapses to `recovery_session_invalid` |
| Failed `/verify` consumes the token (single-use, restart-on-failure) | ✅ | `TestRecoveryTOTPVerify_WrongCodeConsumesToken`; deliberate UX caveat (avoids the re-stash race) documented in `handle_auth_recovery.go` |
| Audit trail (begin: `totp:revoke reason=recovery`; verify: 9× `recovery_code:revoke reason=recovery_complete`, `totp:register`, 10× `recovery_code:register`) | ✅ | `credential_event` counts: `totp:revoke>=2`, `recovery_code:revoke>=9`, `recovery_code:register>=10` |
| recovery_code NOT a sudo factor | ✅ | both surfaces reject it (methods list + dispatch); `handle_sudo.go` package doc captures the NIST §5.2 rationale |

## Upstream OIDC federation (OIDC Core / RFC 9700)

| Item | Status | Notes / source |
|---|---|---|
| Per-IdP `upstream_idp` row with issuer + client + scopes | ✅ schema | `upstream_idp`; model in `pkg/db/models.go` |
| Client secret AES-GCM encrypted with versioned DEK | ✅ | `pkg/federation/oidc/secret.go`; sealed via `oidc.SealClientSecret` |
| AAD bound to row identity (`'upstream_idp:'||id||':'||key_version`) | ✅ | `secret.go` AAD format; unit tests in `secret_test.go` incl. cross-row-paste rejection |
| JWT alg allowlist (RS256/ES256/EdDSA only; `HS256`/`none` rejected) | ✅ | `client.go` `DefaultAllowedAlgs()`; library-level + post-decode re-check |
| Three provisioning modes (`auto_provision` / `invite_only` / `link_only`) | ✅ | `auto_provision`, `link_only`, `invite_only` all exercised end-to-end |
| `auto_provision` gated by `require_verified_email` + `allowed_domains` + username collision | ✅ | `modes.go` `applyAutoProvision`; email_not_verified + username_collision rejections |
| `invite_only` mode end-to-end (token-bearing redemption) | ✅ | `modes.go` `applyInviteOnly` + `federation.go` `BeginInviteRedemption` + `handle_invite_federation.go`; `/start-federation` → `/authorize` → callback → 302 `/me`; writes `enrollment.consumed_at`, account + identity rows, `credential_event[register reason=invite_only_redemption]` |
| `invite_only` rejects consumed token | ✅ | re-driving a consumed token → 403 `invite_required` pre-flight (no upstream hop); `failNoAccount("invite_already_consumed")` audited |
| `invite_only` rejects expired token | ✅ | expired token → 403 `invite_required`; `failNoAccount("invite_expired")` audited |
| Invite redemption is single atomic transaction (consume+account+identity+audit) | ✅ | `modes.go` `applyInviteOnly` wraps via `runInviteTx`; `server.go` passes `pgxpool`. Audit `Writer` is tx-scoped (`audit.NewWriter(qtx)`) so `credential_event.account_id` FK resolves against the just-inserted account, and rollback reverts audit |
| `account_identity` keyed on `(upstream_iss, upstream_sub)` (OIDC Core §2) | ✅ | UNIQUE constraint; insertion + ownership asserted |
| Federation state snapshots `expected_iss` + `expected_token_endpoint` | ✅ | `federation.go` populates `ExpectedIss`; library uses it on `client.Exchange` (mix-up resistance) |
| Single-use federation state via atomic Pop | ✅ | `federation.go` `kvStore.Pop(LoginKey(stateToken))`; unit-tested |
| Cross-namespace defense (LoginKey != LinkKey) | ✅ | `federation.go`; unit-tested — a `LoginKey`-stashed state can't be redeemed via the link callback |
| RFC 9207 `iss` callback param validated against `state.ExpectedIss` | ✅ | `federation.go` HandleCallback + LinkCallback; unit-tested |
| Strict issuer + audience + nonce validation on upstream ID token | ✅ | `client.go` via `zitadel/oidc/v3`; nonce threaded via context-key |
| Disabled-account re-check after Resolve | ✅ | `federation.go` returns `authn.ErrBadCredentials()` (enumeration-safe, same path as password login) |
| `email_verified` gating per IdP (`require_verified_email` column) | ✅ | `modes.go` `applyAutoProvision` email_verified gate |
| AMR pass-through from upstream + backfill to `["federated"]` when omitted | ✅ | `handle_federation.go`. RFC 8176 §2 lists `federated` as a registered AMR value — backfilling is compliant. The local session array is never empty |
| Per-IdP claim-name overrides (`username_claim` / `display_name_claim` / `email_claim`) | ✅ | `modes.go` (applyAutoProvision, syncClaims drift, applyInviteOnly email) + `federation.go` (LinkCallback email) all route through `ClaimString(tokens.Raw, idp.<Claim>)`. Override coverage in `modes_test.go`. Schema defaults match OIDC standard claim names (`preferred_username`/`name`/`email`) |
| RP flow implementation (BeginLogin / HandleCallback / LinkBegin / LinkCallback) | ✅ | `federation.go` |
| Local-username collision policy on JIT auto-provision | ✅ | `modes.go` `applyAutoProvision` collision check |
| Concurrent-callback 23505 mapping (auto_provision + invite_only) | ✅ | both apply paths share `runProvisionTx`; unique-violation on `account.username` → `ErrUsernameCollision`; on `(upstream_iss, upstream_sub)` → `ErrInviteRequired` (anti-enumeration parity with `link_conflict`). Rollback un-does the partial account. `modes.go`. Unit-tested |
| Federation-bound invites reject WebAuthn enrollment path | ✅ | `/enrollments/{token}/register/{begin,complete}` reject any invite with `expected_upstream_idp_slug` set. `handle_enrollment.go` begin guard + complete belt-and-suspenders; returns `ErrEnrollmentFederationRequired()` forcing `/start-federation` |
| `expected_token_endpoint` snapshot validated at callback | ✅ | FedState snapshots `TokenEndpoint` at BeginLogin; `federation.go` HandleCallback + LinkCallback reject when live discovery drifts. Audited `reason=token_endpoint_drift`. RFC 9700 §4.4.2.1 mix-up defense |
| Link-flow session-swap defense | ✅ | `federation.go` LinkCallback `state.LinkingAccountID` must equal current session's account; unit-tested |
| Last-sign-in-method check on unlink | ✅ | `handle_me_identities.go` `handleMeIdentitiesUnlinkHTTP` last-method via `authn.AvailableMethods`; unit-tested |
| Foreign / already-deleted unlink target returns 404 (no audit) | ✅ | `DeleteAccountIdentity` is `:one` with `RETURNING id`; `handle_me_identities.go` maps `pgx.ErrNoRows` → `ErrCredentialNotFound` (404) and skips audit. Prevents audit-log pollution from no-op foreign unlinks |
| Refresh-token storage for upstream tokens | ❌ gap | not implemented; federated users re-authenticate via `/login` each time. Revisit if `/me` ever needs out-of-band upstream profile refresh |

## OIDC OP downstream (RFC 6749 / OIDC Core / RFC 9068 / RFC 9700 / RFC 9207 / RFC 8414 / RFC 7636 / RFC 7009 / RFC 7662 / RP-Initiated Logout 1.0)

| Item | Status | Notes / source |
|---|---|---|
| Authorization Code + PKCE only | ✅ | `response_type=code` only; PKCE S256 enforced at `/oauth/authorize`; PKCE mismatch → invalid_grant |
| PKCE required for **all** clients (incl. confidential) | ✅ | confidential clients supply `code_challenge`/`code_verifier`; mismatch rejected |
| `code_challenge_method` allowlist rejects `plain` | ✅ | oidc/R2; `plain` excluded ENTIRELY by a DB CHECK on `oidc_client.allowed_code_challenge_methods`; `/oauth/authorize` consults per-client `require_pkce` + `allowed_code_challenge_methods`. `code_challenge_method=plain` → redirect `error=invalid_request` |
| `redirect_uri` exact-match (no wildcards) | ✅ | exact-match against `oidc_client.redirect_uris`; unregistered `redirect_uri` → DIRECT 400 (no redirect to the bad URI) |
| `post_logout_redirect_uris` exact-match list | ✅ | oidc/C1; `/oidc/logout` exact-matches; redirects to the registered URI with `state` echoed |
| Single-use authorization codes with replay revocation | ✅ | oidc/C8; code is KV `Pop`-consumed; replay → family revoke + `code_replay` audit |
| `iss` parameter in authorization response (RFC 9207) | ✅ | discovery `authorization_response_iss_parameter_supported:true`; `iss` on the 302 |
| Discovery doc (RFC 8414 / OIDC Core) | ✅ | `/.well-known/openid-configuration` serves the live OP surface (introspection/revocation/end_session, `scopes_supported` incl `offline_access`, `code_challenge_methods_supported [S256]`, `token_endpoint_auth_methods_supported [client_secret_basic,client_secret_post,none]`); `claims_supported` lists `sub/iss/aud/exp/iat/nonce/auth_time/amr/acr/sid/at_hash/username/displayName/role/attributes`. `oidc.go` |
| JWKS endpoint | ✅ | `/oauth/jwks` serves active+cached signing keys as RFC 7517 RSA JWKs; every verify resolves by `kid` |
| ID token signed with asymmetric alg | ✅ | RS256; id_token signature verifies against JWKS |
| `alg: none` rejected | ✅ | verify resolves keys by `kid` and parses with `[]SignatureAlgorithm{RS256}` only; `alg:none`/wrong-alg reject unit-tested in `jwt_test.go` |
| ID token claims: signature, `iss`, `aud`, `exp`, `nonce` validated | ✅ | `iss`, `aud`, `sub`, `nonce` validated after JWKS signature verification |
| ID token `auth_time` claim (OIDC Core §2) | ✅ | sourced from `session.auth_time` |
| ID token `amr` / `acr` claims | ✅ | `amr` from `session.amr` (WebAuthn → `["hwk"]`). `acr` emitted when present on the session (reserved/sparse today) |
| ID token `azp` when `aud` is multi-valued | ✅ | oidc/C5; single-client → `aud` single-valued, `azp` not emitted |
| ID token `at_hash` (defense in depth) | ✅ | oidc/C5; left-half SHA-256 of the access token |
| `sid` claim sourced from `session.id` | ✅ | `/oidc/logout` revokes exactly that `sid`'s session (`/me` → 401) |
| RFC 9068 access token `typ: at+jwt` | ✅ | oidc/C4; the access token's JOSE `typ` is `at+jwt` |
| RFC 9068 required claims (`iss`, `sub`, `aud`, `exp`, `iat`, `jti`, `client_id`, `scope`) | ✅ | oidc/C3; `jti` present; `client_id`/`sub`/`scope` confirmed via introspection |
| `jti` revocation via denylist | ✅ | oidc/C3; revoking an access token → introspect `active:false` + a `revoked_jti` row |
| Refresh tokens single-use rotation + reuse detection | ✅ | rotation (new ≠ old); superseded-token replay → `invalid_grant`; family revocation on reuse |
| Refresh tokens stored server-side (opaque) | ✅ | KV-backed opaque tokens; rotation/reuse observable only because the family record is server-side |
| Access tokens short-lived (≤ 15 min) | ✅ | `configx.OIDC.AccessTokenTTL` default 10m |
| Refresh tokens 30 day default | ✅ | `configx.OIDC.RefreshTokenTTL` |
| `offline_access` scope gates refresh issuance (OIDC Core §11) | ✅ | oidc/R3; requesting `offline_access` yields a refresh token |
| argon2id hashing for `client_secret_hash` | ✅ | `oidc-client create` argon2id-hashes the secret (printed once); wrong secret → 401 `invalid_client`, correct secret verifies against the stored hash |
| `token_endpoint_auth_method` (`client_secret_basic` default, `none` for public) | ✅ | oidc/R1; `client_secret_basic` default. `client_secret_post` + `none` (public) implemented + unit-tested |
| `id_token_signed_response_alg` per client | ✅ schema | oidc/R1 |
| `subject_type` (`public` / `pairwise`) | ✅ schema | oidc/R1 |
| `application_type` (`web` / `native`) | ✅ schema | oidc/R1 |
| `default_max_age` / `require_auth_time` per client | ✅ schema | oidc/R1 |
| `contacts` / `logo_uri` / `tos_uri` / `policy_uri` | ✅ schema | oidc/R1 |
| Token introspection (RFC 7662) — `active`, `sub`, `scope`, `client_id`, `exp` | ✅ | oidc/R6; `/oauth/introspect` client-authenticated, per-client ownership; `active:true` + `token_type:access_token` + `client_id` + `sub`; revoked → `active:false` |
| Introspection requires a confidential client; public clients rejected | ✅ | RFC 7662 §2.1 — a public (`none`-auth) client → `invalid_client` (401); confidential introspect of own → `active:true`; public `/oauth/revoke` of own → 200 (RFC 7009 unchanged) |
| Token revocation (RFC 7009) | ✅ | `/oauth/revoke` client-authenticated, per-client ownership, always 200; access → `revoked_jti` denylist, refresh → family revoke; outstanding access tokens self-expire ≤ `AccessTokenTTL` |
| Pushed Authorization Requests (PAR, RFC 9126) | ⚠️ conditional | not needed for first-party; add only if a low-trust client requires it |
| JAR (RFC 9101) | ⚠️ conditional | same |
| DPoP (RFC 9449) sender-constrained tokens | ⚠️ conditional | bearer is fine for first-party; add for a low-trust client |
| mTLS (RFC 8705) | ⚠️ conditional | bearer is fine for first-party; add for a low-trust client |
| Dynamic client registration (RFC 7591) | ⚠️ out of scope | first-party static config (ARCHITECTURE → Out of scope) |
| Pairwise sub identifiers | ⚠️ conditional | `subject_type='pairwise'` column reserved; only if RP correlation-resistance is needed |
| Encrypted ID tokens (JWE) | ⚠️ deferred | TLS provides wire confidentiality |
| RP-Initiated Logout 1.0 | ✅ | `/oidc/logout` validates `id_token_hint` sig + `iss` (tolerates expiry), revokes the `sid`'s session, exact-match `post_logout_redirect_uri`; 302 + `state`; `sid` session revoked → `/me` 401 |
| `prompt=login` forced re-auth | ✅ | full fresh re-login + single-use KV nonce (`pkg/authn` `DemandReauth`/`ConsumeReauth`, prefix `oidc:reauth:`). A stale session does NOT issue (its `auth_time` predates the demand); a fresh login + nonce issues |
| `max_age` forced re-auth | ✅ | `max_age=0` always demands (bounces even a just-minted session); a large `max_age` is satisfied by a recent session |
| `prompt=none` + re-auth demand → `login_required` | ✅ | no bounce — redirect carrying `error=login_required` (`prompt=login`+`none` → `invalid_request` is unit-tested) |
| Front-channel / back-channel logout | ⚠️ deferred | multi-RP coordinated sign-out; IdP-local logout ships |
| Mix-up attack resistance | ✅ | `iss` param (RFC 9207) at `/authorize` + federation state snapshots |
| Refresh-token family forensics table | ⚠️ deferred | oidc/R7; KV-only — reuse-detection + family revocation work end-to-end without a forensics table |
| Rate limit on `/oauth/authorize` and `/oauth/token` | ✅ (per-identity, NOT per-IP) | INTENTIONAL policy: `/authorize` keyed per `account_id`; `/token`/`/introspect`/`/revoke` per `client_id`; `/userinfo` per `sub` (keys `oidc:authorize:acct:<id>`, `oidc:token:client:<id>`, `oidc:userinfo:sub:<sub>`). Reuses the account/session limiter. Respects the no-per-IP-bucket policy (client IP untrustworthy behind NAT/CDN). Edge DoS remains the proxy/WAF's job. See "Rate limiting policy". Caps unit-/manually verified |

## SAML IdP (SAML 2.0 Core / Bindings / Metadata / Profiles)

Handlers are `IdP` methods in `pkg/protocol/saml`; routes mounted in `pkg/server/server.go` (incl. `GET /saml/sso/init`).

| Item | Status | Notes / source |
|---|---|---|
| Implementation | ✅ | `pkg/protocol/saml` (`idp.go`/`metadata.go`/`authnreq.go`/`assertion.go`/`attributes.go`/`subjectid.go`/`sso.go`/`slo.go`/`xmlsec.go`); routes mounted |
| IdP metadata endpoint (`/saml/metadata`) — `EntityDescriptor` with ≥1 signing `KeyDescriptor` | ✅ | the `EntityDescriptor` carries an `IDPSSODescriptor` with ≥1 signing `KeyDescriptor` |
| `/saml/metadata` SSO/SLO bindings + `NameIDFormat` + `WantAuthnRequestsSigned` | ✅ unit (`metadata_test.go`) | emitted by `metadata.go` |
| SP-initiated SSO (`/saml/sso`) | ✅ | HTTP-Redirect AuthnRequest in → signed Response auto-POSTed to ACS; `ParseXMLResponse` verifies it |
| SP registry with entity ID, NameID format, attribute map | ✅ schema | `saml_sp`; `saml-sp create --kind ghes` registers + ingests metadata |
| Multi-endpoint ACS (Metadata §2.4.4) | ✅ schema | saml/C1; `saml_sp_acs` child table; CLI ingests all ACS from metadata |
| ACS URL validated by exact match → index lookup → is_default | ✅ | saml/C1; bad/unregistered ACS rejected (open-redirect guard) |
| Multiple SP signing/encryption certs per SP (rotation-friendly) | ✅ schema | saml/C3; `saml_sp_key (sp_id, use)` |
| `require_signed_authn_request` per SP | ✅ | saml/C3; unsigned AuthnRequest to a `require_signed` GHES SP → rejected |
| `want_assertions_signed` / `authn_requests_signed` mirror SP metadata | ✅ schema | saml/R4; CLI honors `--want-assertions-signed` + metadata `AuthnRequestsSigned` |
| Both `<Response>` and `<Assertion>` signed (RSA-SHA256, exclusive C14N) | ✅ | saml/GHES-1; Response sig verified SP-side; sign-both + alg unit-tested in `assertion_test.go` |
| `Destination` on `<Response>` = chosen ACS URL | ✅ | saml/GHES-2; asserted by SP-side parse |
| `<SubjectConfirmationData Recipient>` = chosen ACS URL | ✅ | Profiles §4.1.4.2; asserted by SP-side parse |
| `<Audience>` = `saml_sp.entity_id` verbatim | ✅ | saml/C2; asserted by SP-side parse |
| `InResponseTo` echoed on Response + SubjectConfirmationData | ✅ | `ParseXMLResponse(respXML, []string{requestID}, …)` validates `InResponseTo` against the request-ID list; also `assertion_test.go` |
| Stable pairwise NameID (Core §8.3.7) | ✅ | saml/C5; identical NameID across 2 SSOs; 1 `saml_subject_id` row, stable `name_id` |
| Persistent 1.1-namespace NameID default (Format URI) | ✅ schema; ✅ unit (`assertion_test.go`) | saml/C4; `saml_sp.name_id_format` default `urn:oasis:names:tc:SAML:1.1:nameid-format:persistent`; the NameID *value* (presence + stability) is verified, the Format URI default is unit-tested |
| Attribute map as ordered JSONB array (multi + URI NameFormat) | ✅ | saml/R1; `attributes.go` projects the GHES map; `USERNAME` attribute verified, full map unit-tested |
| Per-SP `session_lifetime` for `SessionNotOnOrAfter` | ✅ schema; ✅ unit | saml/GHES-8; set from `session_lifetime` in `assertion.go` (unit-tested) |
| Metadata freshness fields (`metadata_*`) | ✅ schema | saml/R3 |
| `AuthnContextClassRef` (`PasswordProtectedTransport`) | ✅ unit | saml/R5; emitted in `assertion.go`, unit-tested |
| IdP metadata publishes all non-retired (live + grace) signing keys | ✅ | saml/R6; ≥1 KeyDescriptor; multi-key/grace-window selection unit-tested in `keys_saml_test.go` |
| Signing-key reuse: same `signing_key` signs OIDC + SAML | ✅ | the OIDC signing key is reused to sign the SAML Response |
| Issuer/EntityID = `PublicOrigins[0]` | ✅ unit | `saml.go` `entityID()`/`ssoURL()`/`sloURL()` derive from `PublicOrigins[0]` (unit-tested) |
| GHES `sp_kind='ghes'` auto-sets `require_signed_authn_request=true` | ✅ | saml/GHES-10; CLI forces it for `--kind ghes`; enforcement proven |
| GHES `emails` / `public_keys` / `gpg_keys` multi-valued | ✅ schema; ✅ unit | saml/GHES-6; `attribute_map.multi=true`; unit-tested in `attributes_test.go` |
| GHES `public_keys` URI NameFormat (`Name=urn:oid:1.2.840.113549.1.1.1`) | ✅ unit | saml/GHES-7; emitted with URI NameFormat + OID Name; unit-tested |
| GHES `administrator` attribute literal | ✅ unit | saml/GHES-5; emitted only as `"true"` when `role=='admin'`/`attributes.administrator` truthy; unit-tested |
| Single Logout (SLO) endpoint (`/saml/slo`) — IdP-local | ✅ | signed LogoutRequest → signed LogoutResponse; bound session revoked, a different session survives |
| SLO LogoutRequest signature verify + LogoutResponse sign | ✅ | redirect-binding round trip; LogoutResponse signature verified in `slo_test.go` (unit) |
| `saml_session` populated + consumed by SLO | ✅ | ≥2 `saml_session` rows; SLO revokes exactly the bound one |
| `credential_event` (factor `saml_sp`) for SSO + SLO | ✅ | `use` for SSO + `session_end` for SLO |
| **XSW defense** (signature Reference ties to the processed element's own ID) | ✅ unit | saml/XSW; `xmlsec.go` `parseXMLSecure` + reference-tie check; XSW/duplicate-assertion negatives in `xmlsec_test.go` |
| **XXE / DTD-off parsing + duplicate-ID rejection** | ✅ unit | `xmlsec.go` `parseXMLSecure`; DTD-bearing + duplicate-ID payloads rejected (unit) |
| **SHA-1 rejected** (signature alg + digest) | ✅ unit | RSA-SHA256 only; SHA-1 sig/digest rejected on verify (unit) |
| **SP-signature cert-pinning** (verify against `saml_sp_key`, never message-embedded cert) | ✅ design; ✅ unit | verification cert-pinned to the registered `saml_sp_key`; unit-tested (sidesteps crewjam/saml#384) |
| **AuthnRequest replay single-use** (KV) | ✅ | replayed AuthnRequest ID → 2nd rejected; marker written on the issue path (so the login bounce can re-drive once) |
| **DEFLATE decompression-bomb bound (10 MB)** | ✅ unit | `xmlsec.go` caps redirect-binding inflation at 10 MB |
| **ACS open-redirect guard** (only DB-registered ACS locations) | ✅ | bad/unregistered ACS → reject; unknown SP → direct error, never a redirect |
| **AuthnRequest `ID` required (NCName)** | ✅ unit | missing/invalid request `ID` rejected (unit) |
| `IsPassive` honored → `NoPassive` Response | ✅ | `ForceAuthn`+`IsPassive` (with session; IsPassive wins) → `NoPassive` status Response, no assertion. The no-session+`IsPassive` path is unit-tested only (`sso_test.go`) |
| POST-binding AuthnRequest + POST-binding LogoutRequest | ✅ AuthnRequest; LogoutRequest ✅ unit | POST-binding AuthnRequest intake; POST-binding LogoutRequest parse/verify unit-tested (the REDIRECT binding is exercised for SLO) |
| No-stored-SLO-endpoint fallback → 200 `text/xml` LogoutResponse | ✅ unit | `slo.go` fallback; unit-tested only |
| No-session SSO → 302 to `Issuer+/login?return_to=<SSO URL>` | ✅ unit | the login-bounce branch is unit-tested only |
| SLO response location resolution | ✅; ✅ unit (`slo_test.go`) | saml/R2; the SP's `SingleLogoutService` location is parsed from the stored SP metadata at request time (`parseSPSLOEndpoint` — `ResponseLocation` else `Location`), NOT a `saml_sp_slo` child table (doesn't exist); request-supplied locations never trusted. The round-trip (302 + decodable Success `LogoutResponse` + session revoked) is verified; that the response `Location` host matches the SP's registered SLO location is unit-tested in `slo_test.go` |
| `ForceAuthn` (forced re-auth) | ✅ | triggers the re-auth bounce + single-use nonce (`pkg/authn` `DemandReauth`/`ConsumeReauth`, prefix `saml:reauth:`) — a stale session does NOT issue, a fresh login + nonce → assertion with a fresh `AuthnInstant`. `ForceAuthn`+`IsPassive` → `NoPassive`, no assertion (IsPassive wins) |
| `NameIDPolicy/@Format` honored | ✅ | a requested concrete Format we can't produce (≠ persistent, ≠ `unspecified`) → `InvalidNameIDPolicy`, no assertion; `unspecified`/absent/matching → normal assertion (`Format=emailAddress` → `InvalidNameIDPolicy`) |
| POST-binding AuthnRequest intake (`POST /saml/sso`) | ✅ | enveloped-signed AuthnRequest accepted (base64, no inflate, verified against `saml_sp_key`); POST SSO binding re-advertised in metadata |
| Signed IdP metadata + `validUntil`/`cacheDuration` | ✅ | `EntityDescriptor` signed, verifies against its own cert; `validUntil` + `cacheDuration` from `configx.SAML.MetadataValidity`; fails OPEN to unsigned if no active signing key (fail-open branch unit-tested only, `TestMetadataNoActiveKeyUnsigned`) |
| IdP-initiated SSO | ✅ | `GET /saml/sso/init?sp=<entity_id>&RelayState=<deep-link>` emits an UNSOLICITED Response (no `InResponseTo`) to the SP's DEFAULT ACS, gated by per-SP `saml_sp.allow_idp_initiated` (default false; non-opted-in → 403); `RelayState` verbatim; rate-limited per-account + per-SP; audit `reason=idp_initiated`. `saml-sp create --allow-idp-initiated` |
| Front-channel multi-SP SLO propagation | ⚠️ out of scope | SLO is IdP-LOCAL only — revokes the bound Prohibitorum session, no propagation to other SPs |
| AttributeQuery / NameIDMapping / Artifact binding | ⚠️ out of scope | saml/Optional |
| `default_relay_state` per SP (only if IdP-initiated lands) | ⚠️ out of scope | saml/Optional |
| Encrypted assertions / NameID (`saml_sp_key.use='encryption'`) | ⚠️ conditional | column exists but unused; add only on SP demand (GHES doesn't require it) |

**Accepted / deferred:**
- Front-channel multi-SP SLO — out of scope; SLO is IdP-local (revokes the bound session only).
- Assertion / NameID encryption — conditional (SP-demand); `saml_sp_key.use='encryption'` reserved but unused (GHES doesn't require it).
- No-stored-SLO-endpoint fallback returns a 200 `text/xml` LogoutResponse (unit-tested only).

**SAML residual limitations (accepted):**
- AuthnRequest-ID replay is a non-atomic KV Get→SetEx (no `SetNX` on `kv.Store`). Low impact: a replayed AuthnRequest yields an identical assertion to the **same registered ACS** for the same subject (SP de-dupes by `InResponseTo`), and requires a live IdP session. Documented limitation.
- SLO↔SSO resurrection race: a concurrent SSO that already passed the session gate can mint one assertion for a session being logged out (bounded to one in-flight request, same user).

## Cryptography

| Item | Status | Notes |
|---|---|---|
| All tokens via `crypto/rand` | ✅ | 32 bytes session/enrollment, 16 bytes pairing id, 64 bytes WebAuthn user handle |
| Pairing code: rejection-sampled, unambiguous alphabet, ~40 bits | ✅ | 8 chars from 30-char alphabet |
| JWT signing: RS256 (2048-bit RSA) | ✅ design | asymmetric, widely supported |
| Unified `signing_key` for OIDC + SAML (use sig\|enc, kid rotation) | ✅ schema | oidc/R4 |
| Key rotation: insert new, flip active, retire old after grace | ✅ design | `signing_key.not_before` + `retired_at` |
| `not_before` on signing keys (oidc/R4) | ✅ schema | `signing_key.not_before` |
| AES-256-GCM at rest with versioned DEK | ✅ design | credentials/C3; `PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>`. DEK rotation budget (~2^32 ciphertexts per key, NIST SP 800-38D §8.3) + a re-encrypt-sweep plan |
| AAD binds ciphertext to row identity | ✅ design | credentials/C4 |
| 12-byte per-row nonce, unique per row | ✅ design | NIST SP 800-38D §5 |
| argon2id PHC for password / recovery / client_secret hashes | ✅ design | credentials/R5 + C2. `pkg/credential/password.PHCDecode` enforces a lower-bound floor on `m`/`t`/`p` against tampered/injected stored hashes; floor is well below OWASP minimum (m≥8 MiB) — a sanity check, not a config gate |
| HSM / KMS-backed signing | ⚠️ deferred | private keys are DEK-sealed at rest (`private_pem_enc`); KMS would additionally defend a DB+env compromise |
| TLS termination | external | reverse-proxy responsibility. Session cookie's `Secure` + `__Host-` derive from the public-origin scheme (`PUBLIC_ORIGIN`=https → hardened), deployment-stable; the ceremony cookie's `Secure` is still per-request TLS-detected |
| Time skew tolerance on JWT verification | ✅ design | 30s leeway on `exp` / `iat` / `nbf` |

## Operational

| Item | Status | Notes |
|---|---|---|
| Forward-only migrations via goose | ✅ | embedded `.sql` files; goose installation quirk documented in STATUS.md |
| Structured audit logs via `credential_event` | ✅ | `pkg/audit.Writer` writes `register`/`use`/`fail`/`revoke` for password/totp/recovery_code + `session:sudo_granted` + `factor_locked` (emitted by `throttle.RegisterFailure` on the unlocked→locked transition) |
| Audit-log fields: who, what, when, IP, UA, detail | ✅ | `credential_event.{account_id, factor, event, credential_ref, ip, user_agent, detail jsonb, at}`; populated by every handler that touches a credential |
| Session manager for end users (`/me/sessions`) | ✅ | |
| Admin can revoke other-user sessions | ✅ | `/accounts/revoke-sessions` |
| Live `account.disabled` check per request | ✅ | `session.LoadSession` middleware |
| Sudo mode for sensitive actions | ✅ | `handle_sudo.go`; sudo accepts 2 methods (`webauthn` / `password_totp`). `recovery_code` REJECTED (NIST §5.2 — knowledge factor MUST NOT be used for reauthentication) |
| Sudo discovery endpoint (`GET /me/sudo/methods`) | ✅ | priority `webauthn` → `password_totp` from `flow.AvailableMethods` (recovery_code excluded) |
| WebAuthn-preferred factor policy (revoke-password-totp) | ✅ | `/me/auth/revoke-password-totp` deletes password + TOTP + recovery rows transactionally; post-revoke `/auth/password/begin` → 401 |
| Rate limit on auth-sensitive endpoints (`/auth/*`) | ✅ | `pkg/authn/ratelimit` + per-account `auth_throttle`. IP-keyed buckets removed project-wide — see "Rate limiting policy". Multi-replica caveat (in-process limiter); cross-surface coupling (login↔sudo share `auth_throttle`) |
| OpenAPI spec for management API | ✅ | huma-generated |
| Admin UI for accounts | ⚠️ deferred | dashboard scaffold |
| Admin HTTP API for OIDC clients / SAML SPs / upstream IdPs / signing keys | ✅ | `registerSudoOpHTTP` centralises the gate (admin auth + sudo + body-size + content-type); OIDC client create/update/rotate-secret/delete; signing-key generate/activate; audit-events viewer; credential list/force-revoke. Audit events for every mutation (`factor` ∈ oidc_client/saml_sp/upstream_idp/signing_key). Full route table in `api.md` |
| Admin dashboard UI pages for OIDC clients / SAML SPs / upstream IdPs | ✅ | `Admin{OidcClients,SamlProviders,UpstreamIdps,SigningKeys,Accounts,Invitations,Audit}View` with CRUD wired to the admin API |
| Consent screen | ⚠️ deferred | first-party-only deployments don't need it |
| Audit-log export / SIEM | ⚠️ deferred | append-only PG `credential_event` table for now |
| Versioned DEK rotation procedure documented | ✅ | DEK compromise / rotation procedure |
| Sudo-gating posture: mutations gated, reads are admin-role only | ✅ | `hasFreshSudo` (`handle_sudo.go`) is the single chokepoint for all admin mutations, applied via `registerSudoOpHTTP` (raw HTTP) + `registerSudoOp` (typed Huma). The gate is a pure read against the session — checks the recent-auth window + `SudoUntil` without consuming anything (multi-use). It gates the account/invitation lifecycle ops — incl. `UpdateAccount` (user→admin escalation). Guarded by `TestAdminMutationRoutesRequireSudo` (`admin_route_policy_test.go`), which serves the REAL `registerOperations()` routes and asserts `sudo_required` (401) on every mutation with no fresh sudo grant |
| Signing-key lifecycle states (`pending`→`active`→`decommissioning`→`retired`) | ✅ | partial unique index `one_active_signing_key (use) WHERE status='active'`; publish set = pending+active+decommissioning; activate demotes prior active→decommissioning + promotes target; background reconcile flips decommissioning→retired. Generating a new key publishes both in JWKS while the old key still signs; after activate, prior-key tokens STILL verify in grace. Legacy `active` bool + `retired_at` retained |
| No secret / key material in audit `detail` JSONB | ✅ | write-site invariant: every admin mutation constructs `detail` from redacted fields only (client_id, display_name, kid — never hash, secret_enc, private_pem); `assertAuditDetailNoSecret` checks every returned event |

## Rate limiting policy

IP-keyed rate limits were removed from all auth/federation/enrollment/pairing HTTP handlers: `sessstore.ClientIP(r, TrustProxy)` can't reliably identify a client behind NAT/CDN/corporate egress — per-IP buckets created both false positives (legitimate users sharing an IP locked out) and false negatives (an attacker rotating IPs bypasses the cap). What remains:

- **Account/session-keyed rate limits** — preserved: `pair_lookup:acct:`, `pair_approve:acct:` (handle_pairing.go), and `sudo:acct:` (handle_sudo.go, 2 spots). Keyed on `sess.Account.ID` or `sess.Data.SessionID`; immune to IP rotation.
- **`auth_throttle` table** — preserved: per-(account, factor) DB-backed lockout state machine for password / TOTP / recovery-code attempts. Protects against password-spray once the attacker has a target username.

Public surfaces without account context (`/auth/password/begin`, `/auth/login/{begin,complete}`, `/auth/federation/<slug>/login`, `/auth/federation/<slug>/callback`, `/auth/enrollment/<token>/begin`, `/auth/devices/pair/begin`, `/auth/recovery/totp/{begin,verify}`) rely on PKCE + state-token single-use + KV TTL for replay protection, and on `auth_throttle` once a credential failure occurs against a known account. No DoS protection at the HTTP edge — that belongs to the deployment's reverse proxy or WAF.

## Web (frontend)

| Item | Status | Notes |
|---|---|---|
| Passkey ceremony popup with focus trap + Esc + backdrop-click | ⚠️ deferred | |
| `AbortController` on `navigator.credentials.{create,get}` | ⚠️ deferred | |
| Body scroll lock during ceremony | ⚠️ deferred | |
| WCAG 2.1.2 No Keyboard Trap | ⚠️ deferred | |
| Concurrent ceremony preemption | ⚠️ deferred | SDK aborts prior |
| Method-selection login UX | ⚠️ deferred | WebAuthn vs password+TOTP vs federation |
| CSRF on state-changing `/me/*` | ✅ | `SameSite=Lax` session cookie + same-origin |
| Conditional UI (passkey autofill) | ⚠️ deferred | identifier-less login |
| Content Security Policy (CSP) | ✅ | set in `pkg/webui/webui.go` (CSP + `X-Frame-Options: DENY` + `X-Content-Type-Options: nosniff`) |
| HSTS | ⚠️ deferred | TLS-layer header; set at the reverse proxy |

## Threats this codebase does NOT protect against

- **Combined DB + environment compromise of signing keys.** Private keys are DEK-sealed at rest (`signing_key.private_pem_enc`), so a DB-only compromise doesn't yield them — but the DEK lives in the environment, so an attacker holding BOTH the database and the env (or process memory) can decrypt them. KMS/HSM-backed signing (AWS KMS / GCP KMS / Vault Transit), where the key never leaves the vault, is optional production hardening.
- **Loss of all DEK versions.** TOTP secrets and upstream-OIDC client secrets become undecryptable; users must re-enroll TOTP and re-link upstream IdPs. Operator responsibility: keep at least two consecutive `PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>` versions available during rotation.
- **Insider abuse via direct DB access.** A SQL operator can grant themselves any role/attributes, mint sessions, or extract signing keys. Standard IdP threat — mitigate with DB access controls + audit-log monitoring.
- **Sustained credential-stuffing against `/oauth/token`.** Per-`client_id` rate limiting at `/token` (and per-`account_id` / per-`sub` at `/authorize` / `/userinfo`) are per-identity, in-process buckets, not edge DoS protection. A reverse-proxy / WAF still owns volumetric defense, and the multi-replica in-process-limiter caveat applies to the OIDC buckets too.
- **Phishing of federated upstream credentials.** Prohibitorum can only validate the assertion the upstream IdP returns; it doesn't control how the upstream authenticates. Pick upstream IdPs whose phishing-resistance matches your threat model.
- **Compromise of an SP signing cert (Pattern C).** If a SAML SP's signing cert leaks, an attacker can forge AuthnRequests from that SP — but can't forge our signed `<Response>` without our signing key, so the blast radius is "spoof the SP's identity to us," not "log in as a user."

Each gap is tracked in `STATUS.md`.

## OIDC OP audit notes

The OIDC OP surface (`pkg/protocol/oidc/*`, the CLIs, server wiring) was independently confirmed sound on: RS256 alg-allowlist (rejects `alg:none`/HS256/confusion), RFC 7638 thumbprint-bound `kid` resolution, constant-time PKCE S256 verify, constant-time argon2id client-secret verify, 256-bit `crypto/rand` for all tokens/codes/jti/secrets, correct `at_hash`, leak-free JWKS, race-free key cache, atomic single-use codes (`Pop`), refresh rotation + reuse→family-revoke (self-healing, no resurrection path), account-bound logout, correct schema↔sqlc↔code types, shared db/kv instances, collision-free `oidc:*` KV namespacing, consistent `factor=oidc_client` auditing.

Hardening applied to this surface:
- **[High]** `/oauth/authorize` nil-pointer guard for a disabled-mid-session account — the disabled sentinel session (`Data==nil`) is rejected by the widened guard `sess==nil || sess.Data==nil || (sess.Account!=nil && sess.Account.Disabled)` → login bounce. (unit-tested)
- **[High]** RFC 6749 §5.2 — `invalid_client` 401 carries `WWW-Authenticate` when Basic auth was used.
- **[High]** rate-limit 429s use `temporarily_unavailable` (not the misleading `server_error` OAuth code).
- **[Medium]** `validateAccessToken` asserts `aud == issuer` (closes a confused-deputy hole masked by the single-audience design).
- **[Medium]** `revoked_jti` denylist pruned hourly by a background goroutine in `Serve()` (`PruneExpiredRevokedJTI`) to bound growth.
- **[Low]** `/oidc/logout` rejects an access token (`typ:at+jwt`) presented as an `id_token_hint`.
- **[Low/availability]** refresh grant fails closed (revokes the family) if token minting fails after a rotation, instead of locking the client out.

**Accepted / deferred:**
- Consent UI deferred (`require_consent` fails closed with `consent_required`).
- Client-id **timing oracle**: the unknown-client path returns before the argon2id verify, leaking client-id existence via latency (client-ids are semi-public; secrets safe). Equalize with a dummy verify when hardened.
- The code-replay→family-revoke marker is written after minting and is best-effort, so a *concurrent* replay during the mint window (PKCE still protects passive interceptors) escapes family revocation — single-use itself still holds. The refresh concurrent-rotation race is non-immortalizing (self-heals via reuse detection); a fully atomic fix needs a KV compare-and-swap the `Store` interface doesn't expose.

## Forced-re-auth and IdP-initiated SSO mechanisms

- **Forced-re-auth freshness gate.** Shared `pkg/authn` helper (`DemandReauth`/`ConsumeReauth`): on a re-auth demand it stamps a single-use KV marker `<proto>:reauth:<nonce> = <accountID>|<demand_instant>` (10m TTL, prefixes `oidc:reauth:` / `saml:reauth:`), embeds the nonce in the `/login?return_to=…&reauth=<nonce>` bounce, and on return requires the marker to still exist, the account to match, AND `session.auth_time >= demand_instant`, then consumes it via an atomic `Pop` (single-use). A stale pre-existing session's `auth_time` predates the demand — so it structurally cannot satisfy `prompt=login` / `ForceAuthn`. Binding to the account prevents a leaked nonce + any fresh session from satisfying a demand. Unit-tested in `pkg/authn/reauth_test.go` (stale session rejected; nonce single-use; expired marker re-demands; empty/never-issued nonce rejected; account mismatch rejected).
- **IdP-initiated SSO guardrails.** Per-SP opt-in via `saml_sp.allow_idp_initiated` (default false) — a non-opted-in SP → 403; delivery only to the SP's registered DEFAULT ACS (open-redirect guard, same as SP-initiated); rate-limited per-account + per-SP; `RelayState` passed verbatim as the deep-link target; audited `reason=idp_initiated`. The inherently weaker login-CSRF posture (an unsolicited Response has no `InResponseTo`, SAML Profiles §4.1.5) is the documented trade-off, mitigated by the short assertion validity window + SessionIndex + AudienceRestriction + the default-off posture (mirrors GHES).

### Session-cookie scoping (RESOLVED)

The session cookie is scoped `Path=/` (was `Path=/api/prohibitorum`, which a real browser never attached to the root-level OIDC/SAML protocol routes `/oauth/authorize`, `/saml/sso`, `/saml/sso/init`, `/saml/slo`, looping the session gate to `/login`). It has a deployment-stable identity — `__Host-prohibitorum_session` + `Secure` in HTTPS, plain `prohibitorum_session` (no `Secure`) in HTTP dev. `SameSite=Lax`, `HttpOnly`, no `Domain`; ceremony cookie untouched. Name resolution is centralized in `pkg/session/middleware.go` (`SessionCookieNameFor`) and reused at the logout-read + the OpenAPI security scheme. Attribute-level unit tests in `pkg/session/middleware_test.go` cover both deployment modes, clear-matches-set, and name resolution incl. empty-origin.

**Carried-forward limitation:** a logged-in user hitting a SAML **HTTP-POST-binding** AuthnRequest is a cross-site POST, so the `SameSite=Lax` cookie is not sent and the user bounces through `/login` once — same family as the `ForceAuthn`+POST-binding item. `SameSite=None` was rejected (broader cross-site exposure, requires always-`Secure`, increasingly browser-restricted).

**Accepted / deferred:**
- `max_age` freshness evaluated WITHOUT clock-skew leniency (fails *stricter*, never looser; the id_token `auth_time` the RP validates is real) — no defect.
- `prompt=consent` / `prompt=select_account` parsed but ignored (consent UI out of scope); `prompt=none` only rejected when combined with `login`, not with the other (unimplemented) interaction prompts. Cosmetic.
- Signed-metadata uses two unsynchronized signing-key cache reads; a key rotation landing exactly between them could (transiently, operator-controlled) advertise a cert set excluding the signer. Extremely narrow; next fetch is consistent.
- **`require_pkce=false` + no `code_challenge` cannot complete token exchange.** `verifyPKCE` (`pkg/protocol/oidc/token.go`) rejects an empty challenge, so a `require_pkce=false` client sending NO PKCE gets `invalid_grant` at `/oauth/token`. Only affects non-default clients (default `require_pkce=true`). Fix is to skip PKCE verification when no challenge was stored.
- **`sloParseError` omits `errBadSigAlg`.** A SLO POST LogoutRequest with a non-SHA256/non-SHA1 sig alg maps to 500 instead of 400 (SSO's `ssoParseError` is fixed; SLO's is not). Cosmetic — still rejects.
- **`ForceAuthn` + POST-binding AuthnRequest.** The re-auth bounce rebuilds `return_to` from the query string, but a POST-binding AuthnRequest body isn't in the query, so after the login bounce the return GET has no `SAMLRequest` and fails safe with an error. Degenerate combination (POST-binding SPs rarely also set ForceAuthn). Documented limitation.
- **`oidc-client create --public` requires `--post-logout-redirect-uri`.** The public path passes `nil` post-logout URIs, violating the NOT NULL column; workaround is to supply one. CLI ergonomics fix (default to an empty array).
- **Front-channel multi-SP SLO** and **assertion / NameID encryption** remain out of scope (`saml_sp_key.use='encryption'` reserved but unused).
