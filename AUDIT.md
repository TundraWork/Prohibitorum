# Audit — OAuth 2.1 / OIDC / WebAuthn best-practice checklist

Compliance of `v1` (this codebase) against the relevant current
standards and security BCPs. Items marked **⚠️ deferred** are
intentional v1 omissions with a clear future path; items marked **❌
gap** are unfinished and need work before v1.0.

## Authentication (identity layer — lifted from picotera)

Carried over wholesale; audit findings on the picotera side apply
identically here. See picotera's `feat-user-system` branch history.

| Item | Status | Notes |
|---|---|---|
| WebAuthn `ResidentKey=Required` (discoverable) | ✅ | `auth.RegistrationOptions` |
| WebAuthn `UserVerification=Required` at register, `Preferred` at login | ✅ | Per FIDO Alliance UV split |
| `AttestationPreference=PreferNoAttestation` | ✅ | No fingerprinting |
| Sign-count clone detection | ✅ | go-webauthn `FinishPasskeyLogin` |
| `excludeCredentials` on add-passkey | ✅ | `handle_me.go` |
| Session cookie `HttpOnly` + `Secure` + `SameSite=Lax` | ✅ | `auth.FreshSessionCookie` |
| Live `account.disabled` check per request | ✅ | `auth.LoadSession` middleware |
| Sliding session refresh at TTL/4 | ✅ | `session.Load` |
| Sudo mode for sensitive actions | ✅ | `handle_sudo.go`, `requireFreshSudo` |
| Atomic single-use enrollment-token consume | ✅ | SQL row-level lock via `WHERE consumed_at IS NULL` |
| Rate limiting on auth-sensitive endpoints | ✅ | `auth.RateLimiter` |
| Device pairing without bearer-token URL | ✅ | `pkg/auth/pairing.go` |
| Backfill `SessionID` / `UserAgent` for legacy KV entries | ✅ | `session.Load` + `ListByAccount` |

## OAuth 2.1 / OIDC (new layer)

Reference: [RFC 9700 — OAuth 2.0 Security BCP](https://datatracker.ietf.org/doc/html/rfc9700), [RFC 9068 — JWT Access Token Profile](https://datatracker.ietf.org/doc/html/rfc9068), [RFC 9207 — Issuer Identification](https://datatracker.ietf.org/doc/html/rfc9207), [RFC 8414 — Authorization Server Metadata](https://datatracker.ietf.org/doc/html/rfc8414), [OAuth 2.1 (draft)](https://datatracker.ietf.org/doc/draft-ietf-oauth-v2-1/).

| Item | Status | Notes |
|---|---|---|
| Authorization Code + PKCE only | ✅ design | Implicit + ROPC + Hybrid are not registered as accepted `response_type`s. |
| PKCE required for **all** clients (incl. confidential) | ✅ design | RFC 9700 §2.1.1 |
| `code_challenge_method=S256` only (no `plain`) | ✅ design | |
| `redirect_uri` exact-match (no wildcards / subpath) | ✅ design | `oidc_client.redirect_uris` is an array of full URIs |
| Single-use authorization codes | ✅ design | KV TTL 60s; deleted on first /token exchange |
| Authorization Server issuer in response (RFC 9207) | ✅ design | `iss` query param appended to redirect |
| Discovery doc (RFC 8414 / OIDC Core) | ✅ design | `/.well-known/openid-configuration` |
| JWKS endpoint | ✅ design | `/jwks` |
| ID token signed with **asymmetric** alg | ✅ design | RS256 in v1; ES256/EdDSA future |
| `alg: none` rejected | ✅ design | jwt verification configured for RS256 only |
| ID token claims validated: signature, `iss`, `aud`, `exp`, `nonce` | ✅ design | Documented in INTEGRATION.md |
| Refresh tokens single-use rotation + reuse detection | ⚠️ deferred | Token endpoint stubbed; rotation logic is v0.2 |
| Refresh tokens stored server-side (opaque) | ✅ design | KV-backed |
| Access tokens short-lived (≤ 15 min) | ✅ design | Default 10 min |
| Refresh tokens 30 day default | ✅ design | Configurable |
| Argon2id hashing for client secrets | ✅ design | `golang.org/x/crypto/argon2` |
| Client secret theft → leak detection? | ❌ gap | No anomaly detection. Operator monitors via logs. |
| Pushed Authorization Requests (PAR, RFC 9126) | ⚠️ deferred | Not required for v1 first-party clients; add when integrating with low-trust clients |
| JAR (RFC 9101) | ⚠️ deferred | Same |
| DPoP (RFC 9449) sender-constrained tokens | ⚠️ deferred | Bearer tokens for v1; DPoP when threat model demands |
| Dynamic client registration (RFC 7591) | ⚠️ deferred | Static config via SQL for v1 |
| Pairwise sub identifiers (privacy isolation per client) | ⚠️ deferred | Single global `sub` for v1; first-party-only deployments don't need pairwise |
| Encrypted ID tokens (JWE) | ⚠️ deferred | TLS provides confidentiality on the wire; encrypt only when caching intermediaries are introduced |
| Logout: RP-initiated (OIDC RP-Initiated Logout 1.0) | ⚠️ deferred | `/oidc/logout` endpoint stubbed |
| Logout: front-channel / back-channel | ❌ gap | Add when multi-RP single-sign-out becomes a requirement |
| Token introspection (RFC 7662) | ⚠️ deferred | Documented as Pattern B in INTEGRATION.md; endpoint stubbed |
| Token revocation (RFC 7009) | ⚠️ deferred | Same; refresh-token rotation invalidates by design |
| Mix-up attack resistance | ✅ design | `iss` param in response (RFC 9207) + client-side library does claim validation |
| Open-redirect resistance | ✅ design | exact-match redirect_uris |
| Time skew tolerance | ✅ design | 30s `leeway` on `exp` / `iat` / `nbf` |

## Cryptography

| Item | Status | Notes |
|---|---|---|
| All tokens via `crypto/rand` | ✅ | 32 bytes session / enrollment, 16 bytes pairing id, 64 bytes WebAuthn handle |
| Pairing code: rejection-sampled, unambiguous alphabet | ✅ | 8 chars, ~40 bits |
| JWT signing: RS256 (2048-bit RSA) | ✅ design | Asymmetric, widely supported |
| Key rotation: insert new + flip active flag | ✅ design | Old keys remain in JWKS until `retired_at < now() - max_token_ttl` |
| HSM / KMS integration | ❌ gap | Private keys live in DB column. Future: KMS adapter |
| TLS termination | external | Reverse proxy responsibility; Prohibitorum sets `Secure` cookie when TLS is detected |

## Operational

| Item | Status | Notes |
|---|---|---|
| Migrations forward-only via goose | ✅ | Embedded `.sql` files |
| Structured audit logs | ✅ | `auth.*` events tagged with `event` field |
| Session manager for end users | ✅ | `/me/sessions` |
| Admin can revoke other-user sessions | ✅ | `/accounts/revoke-sessions` |
| Rate limit on `/authorize` and `/token` | ❌ gap | Add similar to `auth.*` |
| `/me/sessions/revoke` with sudo | ⚠️ deferred | Currently no sudo gate on session revoke |
| OpenAPI spec for management API | ✅ | huma-generated |
| Admin UI for accounts | ✅ | Carried from picotera |
| Admin UI for OIDC clients | ❌ gap | Manage via SQL for v1; future page |
| Consent screen | ⚠️ deferred | First-party-only deployments don't need it |
| SAML | ⚠️ out of scope | OIDC + Pattern B cover the use cases |
| Multi-tenancy | ⚠️ out of scope | Single tenant for v1 |

## Web (frontend)

| Item | Status | Notes |
|---|---|---|
| Passkey ceremony popup with focus trap + Esc + backdrop-click | ✅ | Carried from picotera |
| `AbortController` on `navigator.credentials.{create,get}` | ✅ | Same |
| Body scroll lock during ceremony | ✅ | Same |
| WCAG 2.1.2 No Keyboard Trap | ✅ | Same |
| Concurrent ceremony preemption | ✅ | SDK aborts prior |
| CSRF on state-changing `/me/*` | ✅ | `SameSite=Lax` session cookie |
| Conditional UI (passkey autofill) | ⚠️ deferred | Identifier-less login; add if username-first flow is needed |
| Content Security Policy (CSP) | ❌ gap | Reverse-proxy responsibility for v1; bake into static handler later |
| HSTS, X-Frame-Options, etc. | ❌ gap | Same |

## Threat model — what v1 does NOT protect against

- **HSM-tier private key protection.** RSA private keys live in
  `oidc_signing_key.private_pem`. A DB compromise = a complete IdP
  compromise. Move to KMS-backed signing (AWS KMS, GCP KMS, Vault
  Transit) for production use.
- **Insider abuse via direct DB access.** A SQL operator can grant
  themselves any role / permission, mint sessions, or extract signing
  keys. Standard IdP threat — mitigate with DB access controls + audit.
- **Sustained credential-stuffing against `/token`.** Rate limiting on
  OIDC endpoints is gap-flagged above; reverse-proxy WAF is the
  short-term mitigation.

Each gap is tracked in the project's TODO with a target version.
