# Pre-ship logic-correctness + security-invariant audit

**Date:** 2026-06-11
**Trigger:** gate before exposing the IdP to insider users.
**Scope:** auth-critical cores only — OIDC OP (authorize/token/refresh/id_token), SAML IdP
(AuthnRequest/assertion/SLO/XML-sig), WebAuthn + credential ceremonies (passkey/enrollment/password/TOTP),
session & sudo/step-up/CSRF, upstream OIDC federation, and a cross-cutting authz/IDOR/mass-assignment pass.
Admin CRUD plumbing, pure UI, and the config surface were out of scope (covered by the 2026-06-10/06-11
config + FE-surface audits).
**Method:** 6 parallel domain auditors (opus, full code-trace per domain), each finding then independently
re-checked by 2 refute-by-default skeptics (sonnet). 36 agents, ~1.84M tokens. The 1 HIGH and both MEDIUM
findings were additionally re-verified by hand against the code (see file:line below). Distinct from prior
rounds: this targeted **logic correctness and security invariants in the protocol flows**, not config
completeness or hygiene.

## Headline

Overall posture is **strong** — clearly the product of the prior hardening cycles. The protocol cores get
the hard parts right (see "Verified correct" below). The audit surfaced **1 HIGH, 2 MEDIUM, 5 LOW, 4 INFO**
confirmed issues and **3 disputed** items (adjudicated below; 2 are non-issues, 1 is latent defense-in-depth).
No auth-bypass, token-forgery, replay, XSW, open-redirect, or crypto-misuse defect was confirmed.

**One ship-blocker: WACER-1 (the sudo/login user-verification downgrade).** Everything else is fix-soon or
optional.

## Ranked findings (confirmed)

| # | Sev | Domain | Title | Verify |
|---|-----|--------|-------|--------|
| WACER-1 | 🔴 HIGH | WebAuthn/sudo | UV not enforced on login or sudo step-up — UV-bound passkey usable with user-presence only | 2/2 ✅ (+hand) |
| AUTHZ-1 | 🟠 MED | Authz | Unthrottled argon2id on public `/auth/password/begin` — CPU/RAM DoS amplifier | 2/2 ✅ (+hand) |
| OIDC-1 | 🟠 MED | OIDC OP | Superseded (rotated-away) refresh token reports `active:true` at `/introspect` | 2/2 ✅ (+hand) |
| SESS-1 | 🟡 LOW | Session | Sudo one-shot consume is best-effort; a KV `Save` failure degrades it to unlimited-within-TTL | 2/2 ✅ |
| SESS-2 | 🟡 LOW | Session | Ceremony cookie `Secure` derived per-request; can drop `Secure` behind a TLS-terminating proxy | 2/2 ✅ |
| OIDC-3 | 🟡 LOW | OIDC OP | Auth code not re-bound to a live session at token exchange (logout-before-exchange window) | 2/2 ✅ |
| SAML-1 | 🟡 LOW | SAML IdP | AuthnRequest `IssueInstant` freshness never validated; replay window = 5-min replay-key TTL only | 2/2 ✅ |
| OIDCFED-1 | 🟡 LOW | Federation | Link-flow browser-binding generated + persisted but never enforced (dead anti-forgery control) | 2/2 ✅ |
| SESS-3 | ⚪ INFO | Session | No per-IP rate limit on the unauthenticated WebAuthn login ceremony | 2/2 ✅ |
| WACER-3 | ⚪ INFO | WebAuthn | Enrollment ceremony stash keyed on the plaintext enrollment token in KV | 2/2 ✅ |
| OIDCFED-2 | ⚪ INFO | Federation | `ConsumeEnrollment` not intent-restricted (single begin-time gate) | 2/2 ✅ |
| AUTHZ-2 | ⚪ INFO | Authz | Admin credential delete has no last-passkey guard (asymmetric with self-delete) | 2/2 ✅ |

Disputed (adjudicated, no action / optional): **OIDC-2**, **SAML-2**, **WACER-2** — see end.

---

## Detail

### 🔴 WACER-1 — User verification (UV) is not enforced on login or the sudo step-up

- **Where:** `pkg/credential/webauthn/webauthn.go:24-39` (`NewWebAuthn` leaves `Config.AuthenticatorSelection`
  unset → `UserVerification == ""`); `:122-126` (`LoginOptions` = `VerificationPreferred`);
  `pkg/server/handle_sudo.go:178` (`BeginLogin(wu)` with **no** options → inherits `""`);
  completion paths `handle_sudo.go:236-292` and `handle_auth.go` login-complete never re-check the asserted UV flag.
- **What:** Every credential is **registered** with `UserVerification=Required` (`webauthn.go:108-111`), so all
  passkeys are UV-capable and the row stores `uv_initialized`. But at **assertion** time go-webauthn only
  verifies the UV flag when `session.UserVerification == VerificationRequired` (`login.go:354`). Login passes
  `Preferred`; sudo passes nothing (empty). In both cases `shouldVerifyUser = false`, so an assertion with
  `UP=1, UV=0` is accepted. No handler compares `credential.Flags.UserVerified` against the stored
  `uv_initialized` afterward.
- **Impact:** The sudo gate's documented promise — "fresh biometric/PIN proof, cookie theft alone no longer
  suffices" — **does not hold**. Anyone with physical possession of the authenticator (or a malware/cooperating
  authenticator returning `UV=0`) can satisfy sudo with presence only, and sudo gates the security-critical
  mutations (add backup passkey, approve device pairing, set password, re-enroll TOTP). Same downgrade on
  primary login. This is the one issue I would block ship on.
- **Fix:** For sudo, `BeginLogin(wu, webauthn.WithUserVerification(protocol.VerificationRequired))` (or set
  `Config.AuthenticatorSelection.UserVerification = Required` globally). For login, either request `Required`
  or, after finish, reject when the matched credential row has `uv_initialized=true` but the assertion's
  `UserVerified` flag is false (per-credential no-downgrade). Add a regression test asserting `UV=0` is
  rejected on both surfaces.

### 🟠 AUTHZ-1 — Unthrottled argon2id on public `/auth/password/begin` (DoS amplifier)

- **Where:** `pkg/server/handle_auth_password.go:50-106` (`publicReq`, `server.go:322`);
  `pkg/credential/password/password.go:159-165` (`VerifyAgainstDummy`).
- **What:** The endpoint has **no** per-IP rate limit (`s.rateLimit` is never called here; it's only on
  pairing/sudo) and **no** password length cap (unlike `/me/password/set`, which caps at 1024 bytes). The
  unknown-username (`:63`) and disabled (`:80`) branches run a full `argon2.IDKey` (default **64 MiB**, 3
  iterations) over the attacker-supplied password — correctly, for timing-enumeration defense — but unthrottled
  and uncapped. The per-account throttle can't engage on the unknown-username path (no account row to key on).
- **Impact:** An unauthenticated client can force one 64 MiB memory-hard hash per request by POSTing random
  usernames; a modest request rate exhausts CPU/RAM and denies service. Real pre-ship availability risk.
- **Fix:** Per-IP fixed-window limit at the top of the handler (mirror pairing/sudo, keyed on
  `sessstore.ClientIP`), and reject `len(body.Password) > 1024` before any verify. Apply the same IP limit to
  `/auth/login/begin` (see SESS-3).

### 🟠 OIDC-1 — Superseded refresh token introspects as `active:true`

- **Where:** `pkg/protocol/oidc/refresh.go:207-217` (rotate success never `Del`s the old token mapping),
  `:226-232` (`lookupRefresh` resolves any token to the live family by design), consumed at
  `pkg/protocol/oidc/introspect.go:83-98`.
- **What:** After rotation the old token→family mapping survives at full TTL, and `/introspect` reports it
  `active:true` (with scope + sub). Note: the same token presented at `/token` correctly trips reuse detection
  and revokes the family — so this is **not** a token-exchange bypass, only an introspection-correctness
  (RFC 7662 §2.2) violation. The retained mapping is needed for `/revoke` (RFC 7009), so the fix belongs in
  introspect, not in deleting the mapping.
- **Impact:** A resource server / gateway using introspection to gate on a refresh token's standing gets a
  false-positive "valid" for a token that is effectively dead.
- **Fix:** In `introspect` (or a variant of `lookupRefresh`), report `active:false` unless
  `presented == fam.CurrentToken` or (`presented == fam.PreviousToken` and within `PreviousValidUntil`).

### 🟡 SESS-1 — Sudo one-shot consume fails open on KV write error

`handle_sudo.go:388-402` (`consumeFreshSudo`): zeroes `SudoUntil` in memory, `Save`s it; **on `Save` error it
logs and returns `true` anyway**. Because every request re-reads `SudoUntil` from KV, an un-persisted clear
means `HasFreshSudo()` keeps returning true for the rest of the 5-min `SudoTTL` — i.e. one elevation authorizes
*all* sudo-gated mutations for the window (the code comment's "one extra action" understates it). **Fix:** fail
closed — if the clear can't be persisted, deny the action.

### 🟡 SESS-2 — Ceremony cookie `Secure` flag is per-request, not deployment-stable

`pkg/session/session.go` (middleware): `FreshSessionCookie` and `FedStateCookie` derive `Secure` from
`secureCookies(cfg)` (stable, from the https public origin), but `CeremonyCookie` uses `isSecure(r, TrustProxy)`
— which returns false behind a TLS-terminating proxy when `TrustProxy` is at its default (off). So the
short-lived login ceremony cookie ships **without `Secure`** on an otherwise-https deployment. Session cookie
itself is unaffected. **Fix:** derive the ceremony cookie's `Secure` from `secureCookies(cfg)` for consistency.

### 🟡 OIDC-3 — Auth code not re-bound to a live session at token exchange

`token.go` `grantAuthorizationCode` reloads the account and checks `Disabled`, but never re-loads the session
(`ac.SessionID`) to check `revoked_at` — unlike `/authorize` (`authorize.go:156`). If the user logs out (or the
session is revoked) within the ≤60 s single-use code window, the code still exchanges, and any `offline_access`
refresh token outlives the revoked session (re-checked only against account-disable on each refresh).
**Fix (if in threat model):** re-load the session at exchange and reject when `revoked_at` is set; otherwise
document as an accepted limitation given the 60 s code TTL.

### 🟡 SAML-1 — AuthnRequest `IssueInstant` freshness never validated

`authnreq.go:296-316` validates `@ID` and `Version` but never checks `IssueInstant`, and AuthnRequest carries
no `NotOnOrAfter`. The only bound on a signed request's age is the 5-min replay-key TTL — once it expires, the
same signed AuthnRequest is accepted again (mints a fresh assertion). SLO *does* enforce freshness
(`slo.go:128`), so the asymmetry is clearly an omission. Exploitation needs a captured signed SP request + a
live victim IdP session + browser-drive, and the assertion still only goes to the registered ACS. **Fix:**
reject `IssueInstant` older than `AuthnRequestTTL ± skew` in `parseAuthnRequestXML`.

### 🟡 OIDCFED-1 — Link-flow browser-binding is a dead control

`federation.go` `begin()` always sets `BrowserBinding`, but the link flow
(`handle_me_identities.go` link-begin) never sets `FedStateCookie`, and `LinkCallback` never calls
`browserBindingOK`. So the persisted binding is unreachable. **No takeover risk** — `LinkCallback` enforces
`state.LinkingAccountID == currentAccountID` behind an authenticated session — but it's a populated-but-unchecked
security field (the `state.go` doc comment even says link flows shouldn't carry one). **Fix:** either wire the
cookie + check through, or stop populating `BrowserBinding` for the link flow and document the account-ID match
as the link-flow CSRF control.

### ⚪ INFO

- **SESS-3** — `/auth/login/begin` + `/complete` have no per-IP rate limit (the rate-limit package doc says
  they should). Availability/abuse only; WebAuthn is challenge-response so it's not a credential bypass. Fold
  into the AUTHZ-1 fix.
- **WACER-3** — enrollment WebAuthn stash keyed on the plaintext enrollment token
  (`handle_enrollment.go:292/311/507`), inconsistent with the add-passkey/sudo hardening that keys on
  `SessionID`. Low practical exposure (the token is already a URL bearer). **Fix:** key on a hash of the token.
- **OIDCFED-2** — `ConsumeEnrollment` SQL has no `intent='invite'` predicate; the federation invite path relies
  on a single begin-time intent gate (compensated by the sole-writer invariant + schema CHECK). **Fix:** add
  `AND intent='invite'` to the consume for defense-in-depth.
- **AUTHZ-2** — admin credential delete (`handle_account.go:437-503`) has no last-passkey guard, unlike
  self-delete; an admin can strand an account with zero passkeys (recoverable via reissue-enrollment). Likely
  intentional; add a count check or a warning if not.

---

## Disputed — adjudicated

- **OIDC-2 (JWKS publishes pending keys)** — **No action / by design.** Publishing a pending public key before
  activation is a standard pre-rotation announcement so RPs can pre-cache it; it's asserted by a passing test
  and a handler comment. Only public material is exposed and the OP never signs with a pending key. The
  refuting verifier is correct.
- **SAML-2 (RelayState 80-byte cap on decoded vs on-wire bytes)** — **No action.** Info severity with zero
  security impact by both verifiers' own analysis (RelayState is only ever HTML-escaped or `QueryEscape`d,
  never a sink), and the spec arguably bounds the *value* not the wire encoding — so the current code may
  already be conformant. Not worth changing.
- **WACER-2 (password+TOTP step-2 doesn't assert `FactorCompleted=="password"`)** — **Optional hardening.**
  Not exploitable today (the only writer of `partial_session:` tokens always sets the factor after a successful
  password verify). Worth a one-line guard for self-validating state, but not a ship issue.

---

## Verified correct (high-value, on record)

- OIDC: exact-match `redirect_uri` at authorize **and** token; PKCE S256-enforced, no plain downgrade, verified
  at token; auth codes single-use via atomic KV `Pop` + family revoke on reuse; refresh rotation with reuse
  detection + idempotency window + per-token lock; constant-time client-secret compare with unknown-client
  timing equalizer; RS256 alg-pinned JWS by kid, no private leak; RFC 9207 `iss` on success + error (incl.
  consent-deny).
- SAML: redirect detached-sig reconstructed from exact query octets; POST enveloped sig with positive
  RSA-SHA256 allowlist + Reference-URI==rootID tie + nested-signature (XSW) guard; DTD/entity + duplicate-ID
  rejected pre-parse; AuthnRequest replay atomic single-use on the issue path; ACS resolved only to a
  registered Location; opaque per-(account,sp) NameID; SLO signature required before any session mutation and
  redirect LogoutResponse is detached-signed; IdP-initiated is per-SP opt-in.
- WebAuthn/cred: server-generated single-use challenges (`Pop`); enrollment token consumed atomically inside
  the credential-insert TX; TOTP secret AES-256-GCM with account+keyver AAD, bounded ±1 drift, replay-guarded
  in Go and by conditional SQL UPDATE; recovery codes argon2id-hashed + single-use; password verify
  constant-time + transparent rehash.
- Session/authz: opaque CSPRNG session tokens hashed before KV keying; role always re-fetched live (no
  privilege snapshot); revocation immediate (no in-memory cache); every `/me/*` derives account id from the
  session (no IDOR); mass-assignment prevented (PUT /me mutates only displayName); single vs bulk variants
  gated identically; admin mutations through one admin+sudo chokepoint with a regression test
  (`admin_route_policy_test.go`); secrets excluded from every wire projection.
- Federation: state/nonce/PKCE server-side + single-use `Pop`; constant-time browser-binding cookie on
  login/invite; RFC 9207 + mix-up resistance (per-flow expected_iss/token_endpoint re-check); aud/azp/exp/sig/
  iss/nonce all verified, RS256/ES256/EdDSA allowlist (no HS256/none); outbound client screens resolved IPs per
  hop (anti-SSRF/rebinding); secrets AES-256-GCM with (idp_id,key_version) AAD; (iss,sub) re-login scoped to the
  binding upstream row; provisioning modes + require_verified_email/allowed_domains enforced; link flow
  session-swap-protected by account-id match.

## Recommended remediation order

1. **WACER-1** (ship-blocker) — enforce UV on sudo + login.
2. **AUTHZ-1 + SESS-3** — per-IP rate limit + password length cap on `/auth/password/begin` and
   `/auth/login/*`.
3. **OIDC-1** — introspect superseded refresh tokens as inactive.
4. **SESS-1** (fail closed on sudo clear), **SESS-2** (ceremony cookie Secure).
5. Remaining LOW/INFO as hygiene: OIDC-3, SAML-1, OIDCFED-1, WACER-3, OIDCFED-2, AUTHZ-2, WACER-2.

---

## Remediation applied (2026-06-12) — "blocker + 2 mediums"

Fixed via TDD (failing test → fix → green), then full gate re-run.

- **WACER-1 (HIGH)** — `LoginOptions()` now returns `UserVerification=Required`
  (`pkg/credential/webauthn/webauthn.go`); the sudo step-up passes those options to
  `BeginLogin` (`pkg/server/handle_sudo.go`). Both login and sudo now make
  go-webauthn verify the asserted UV flag and reject a presence-only (UV=0)
  assertion. Test: `pkg/credential/webauthn/webauthn_test.go`.
- **AUTHZ-1 (MED) + SESS-3 (INFO)** — per-IP fixed-window caps + a 1024-byte
  password length cap added ahead of the work: `/auth/password/begin` (30/min/IP
  + cap), `/auth/login/{begin,complete}` (60/min/IP, shared budget). Consts in
  `pkg/server/handle_auth_ratelimit.go`; handlers in `handle_auth_password.go` /
  `handle_auth.go`. Tests: `TestPasswordBeginRateLimitsByIP`,
  `TestPasswordBeginRejectsOversizePassword`, `TestLoginCompleteRateLimitsByIP`.
- **OIDC-1 (MED)** — `/introspect` now reports `active:false` for a refresh token
  that is not the family's current (or in-window previous) token
  (`refreshFamily.isActiveToken`, `pkg/protocol/oidc/{refresh,introspect}.go`).
  Test: `TestIntrospectSupersededRefreshTokenInactive`.

## Remediation applied (2026-06-12) — deferred LOW + INFO batch

Fixed via TDD, full gate re-run green. All confirmed findings are now resolved
except AUTHZ-2 (left by decision) and the two by-design disputed items.

- **SESS-1 (LOW)** — `consumeFreshSudo` now FAILS CLOSED: on a KV `Save` error it
  returns false (denies the gated action) instead of authorizing, so the
  one-shot grant can't silently become "every action for the TTL window"
  (`handle_sudo.go`). Test: `TestConsumeFreshSudoFailsClosedOnSaveError`.
- **SESS-2 (LOW)** — `CeremonyCookie` derives `Secure` from the deployment-stable
  `secureCookies(cfg)` (matching session/fed cookies), not a per-request
  `isSecure(r)` probe; removed the now-dead `isSecure` (`pkg/session/middleware.go`).
  Test: `TestCeremonyCookieSecureFromConfig`.
- **OIDC-3 (LOW)** — `grantAuthorizationCode` re-loads the session by
  `ac.SessionID` (`GetSession` filters `revoked_at IS NULL`) and rejects with
  `invalid_grant` when it's gone — closing the logout-before-exchange window
  (`token.go`). Test: `TestTokenRejectsRevokedSession`.
- **SAML-1 (LOW)** — `parseAuthnRequestXML` rejects an `IssueInstant` outside
  ±`AuthnRequestTTL` of now (`ErrStaleRequest`), so the accept window matches the
  replay-key TTL (`authnreq.go`). Test:
  `TestParseAuthnRequestXMLRejectsStaleIssueInstant`.
- **OIDCFED-1 (LOW)** — `begin()` leaves `BrowserBinding` empty for the link flow
  (`linkingAccountID != nil`), matching the documented intent — no
  populated-but-unchecked field (`federation.go`). Test:
  `TestFederator_LinkBegin_NoBrowserBinding`.
- **WACER-3 (INFO)** — enrollment ceremony KV key derived from a SHA-256 of the
  token (`enrollCeremonyKey`), keeping the bearer out of the keyspace
  (`handle_enrollment.go`). Test: `TestEnrollCeremonyKeyHashesToken`.
- **OIDCFED-2 (INFO)** — new intent+expiry-scoped `ConsumeInviteEnrollment` query
  (`intent='invite'`) used by `applyInviteOnly`; the shared `ConsumeEnrollment`
  stays for the all-intent WebAuthn ceremony (`db/queries/enrollment.sql`,
  `modes.go`). Real-DB intent restriction verified by the smoke's invite_only arc.
- **WACER-2 (INFO/optional)** — both password+TOTP step-2 handlers reject unless
  `partial.FactorCompleted == "password"`, making the MFA state machine
  self-validating (`handle_auth_password.go`). Test:
  `TestTOTPVerify_RejectsWrongCompletedFactor`.

**Still open by decision:** **AUTHZ-2** (admin can delete an account's last
passkey) — left as-is: deleting a compromised sole credential is a legitimate
admin action and is recoverable via reissue-enrollment; a hard block could be
wrong. Flag for a product decision. **OIDC-2** and **SAML-2** remain no-action
(by design).

### Incidental: the smoke gate was broken on master (pre-existing)

Running the gate surfaced `cmd/smoke` failing on **clean HEAD** (confirmed by
stashing my changes) at step 16: add-passkey became fresh-sudo gated in commit
`9de0008`, but the smoke still called `/me/credentials/register/begin` before its
first sudo. **Implication: master has not been smoke-verified since `9de0008`.**
Fixed by priming a sudo grant before step 16 (`cmd/smoke/main.go`). The smoke also
needs `PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK=true` in the runner env (the
in-process mock OP is on loopback; the SSRF dial-screen is correct production
behavior, opted out only for the test).

### Gate result

Both remediation batches share this gate result (re-run after the LOW+INFO
batch): `go build ./...` ✓ · `go vet ./...` ✓ · `go test ./...` rc=0 (incl. all
new unit tests) · smoke **SMOKE_EXIT=0, 120 steps** (sudo UV=Required;
invite_only federation exercises the intent-scoped consume; introspect/PKCE/SAML
AuthnRequest arcs unaffected). `sqlc generate` was run for the OIDCFED-2 query.
Frontend untouched — no vitest/dist rebuild. Changes are uncommitted, pending
review.

