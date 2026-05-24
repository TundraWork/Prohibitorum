# Prohibitorum — Design

> Index Librorum Prohibitorum. The list of who's allowed and what they can do.

A homegrown identity & authorization service. WebAuthn-only sign-in,
OpenID Connect for relying-party integration.

## Why

`picotera` grew an in-house user/account/session/passkey layer because
no commodity IdP fit its constraints (no email channel, admin-driven
recovery, passkey-only, single-org). The same layer is useful to other
services in the org. Rather than copy-paste the auth code into each new
project, extract it into a standalone service and have each RP integrate
via standard protocols.

## What this is (and isn't)

**Is:** a single-tenant identity provider for a small org. Owns the
account directory, runs passkey ceremonies, issues sessions and OIDC
tokens. Acts as the source of truth for "who is this user, are they
disabled, what can they do?"

**Is not:**
- A multi-tenant SaaS IdP (no per-tenant separation; future scope).
- A SAML IdP (OIDC + introspection only).
- A social-login proxy (no upstream IdPs to federate from).
- An authorization policy engine (no OPA/Rego; only RBAC + permission
  flags carried in tokens — RP enforces).

## Architecture

```
                ┌─────────────────────────────────────┐
                │           Prohibitorum              │
                │                                     │
   browser ────►│  /login   /enroll/{token}   /me     │
                │  /accounts            (Vue SPA)     │
                │                                     │
                │  /authorize  /token  /userinfo      │
   RP backend ─►│  /introspect  /.well-known/...      │
                │  /jwks                              │
                │                                     │
                │  ┌────────┐  ┌────────┐  ┌───────┐  │
                │  │ pgx    │  │ KV     │  │ jose  │  │
                │  │postgres│  │keydb/  │  │JWT    │  │
                │  │        │  │memory  │  │signer │  │
                │  └────────┘  └────────┘  └───────┘  │
                └─────────────────────────────────────┘
```

**Identity layer** (lift-and-shift from picotera):
- WebAuthn registration + assertion via go-webauthn
- KV-backed sessions (sliding refresh, live `account.disabled` check)
- Enrollment tokens (bootstrap / invite / reset / add-device pairing)
- Device pairing via short code (no bearer-token transfer)
- Sudo mode (fresh assertion for sensitive actions)
- Per-IP / per-account rate limiting
- 5 v1 permissions; RBAC via `role ∈ {admin, user}`

**OIDC layer** (new):
- Authorization Code flow with PKCE — only flow supported (RFC 9700 / OAuth 2.1).
- Discovery + JWKS endpoints.
- Tokens signed with RS256 (rotation via DB-stored signing keys; one active key, multiple verifying keys until expiry).
- ID token claims: `sub`, `iss`, `aud`, `exp`, `iat`, `nonce`, plus
  `username`, `displayName`, `role`, `permissions` (the 5 booleans).
- Access token: JWT per RFC 9068 with `scope` claim.
- Refresh tokens: opaque, server-side stored, rotated on use.
- Static client config in DB for v1 (no dynamic registration).
- Introspection endpoint for RP back-ends that prefer opaque-token
  semantics.

## Authentication ceremony

1. RP redirects user to `GET /authorize?client_id=...&response_type=code&scope=openid&code_challenge=...&code_challenge_method=S256&state=...&redirect_uri=...&nonce=...`.
2. Prohibitorum checks session cookie. If absent, redirect to `/login?return_to=/authorize?...`.
3. Login page runs WebAuthn assertion ceremony (the existing Passkey SDK). Server verifies, issues session cookie.
4. Browser returns to `/authorize`, which now sees the session, mints an
   authorization code, stores `(code → {account_id, client_id, scope,
   nonce, code_challenge, redirect_uri, expires_at})` in KV with 60s TTL.
5. Redirects browser to `redirect_uri?code=...&state=...`.
6. RP back-end POSTs to `/token` with `grant_type=authorization_code`,
   the code, the original `code_verifier`, and its client credentials.
7. Prohibitorum verifies the code (single-use; PKCE; client identity;
   redirect_uri match), mints ID + access tokens, optionally refresh
   token, returns them.
8. RP validates ID token signature via JWKS, extracts `sub`, etc.

## Authorization model

- Each account has `role` (`admin` | `user`) and 5 boolean
  permissions: `view_own_usage`, `manage_own_api_keys`, `view_models`,
  `view_own_traces`, `manage_own_projects`. Admins implicitly hold
  every permission.
- Token claims:
  - ID token: `role: "admin"|"user"`, `permissions: {…booleans…}`.
  - Access token: standard claims + `scope`. (Same `permissions` is
    available; RPs can either rely on it or call `/userinfo` /
    `/introspect`.)
- RPs **enforce** authorization themselves using the token claims;
  Prohibitorum doesn't decide whether user X can perform action Y on
  resource Z. (No OPA/Rego in v1; RBAC + the 5 flags only.)

## Data layout

**Postgres** — durable identity state:
- `account` — id, username, display_name, webauthn_user_handle, role,
  5 permission booleans, `disabled`, timestamps.
- `webauthn_credential` — id, account_id, credential_id, public_key,
  sign_count, transports, AAGUID, attestation_type, backup_*,
  nickname, timestamps.
- `enrollment` — token, intent (bootstrap/invite/reset/add_device),
  target_account_id, template_*, created/expires/consumed timestamps.
- `oidc_client` — client_id, client_secret_hash, redirect_uris[],
  allowed_scopes[], require_pkce (always true), display_name,
  timestamps.
- `oidc_signing_key` — kid, algorithm (RS256), public_jwk, private_pem,
  active, created_at, retired_at.

**KV** (KeyDB/Redis or in-process) — ephemeral state:
- `session:<acct>:<token>` → `SessionData`.
- `webauthn_ceremony:{login,enroll,add,sudo}:<token>` → `webauthn.SessionData`.
- `pairing:id:<id>` + `pairing:code:<code>` → device pairing state.
- `oidc:code:<random>` → `AuthCodeData` (account_id, client_id, scope,
  nonce, code_challenge, redirect_uri).
- `oidc:refresh:<random>` → `RefreshTokenData` (account_id, client_id,
  scope, family, rotated_from).

## RP integration patterns

Two supported patterns; pick by RP capability:

**Pattern A — OIDC Authorization Code + PKCE (preferred)**
Best for any RP with a back-end (web apps, mobile apps with a server,
CLI tools using a loopback redirect). Uses standard OIDC libraries.

```
RP                                         Prohibitorum
─┬─                                        ─┬─
 │  302 → /authorize?client_id=...&PKCE      │
 │ ─────────────────────────────────────────►│
 │                                           │ (login if needed; WebAuthn)
 │  302 → redirect_uri?code=...&state=...    │
 │ ◄─────────────────────────────────────────│
 │  POST /token (code + code_verifier)       │
 │ ─────────────────────────────────────────►│
 │  { id_token, access_token, refresh_token} │
 │ ◄─────────────────────────────────────────│
 │  Trust id_token claims after JWKS verify  │
```

**Pattern B — Cookie + Introspection (for legacy / co-located RPs)**
Only viable when the RP shares a parent domain with Prohibitorum (so
the session cookie is sent). RP back-end posts the cookie to
`/oidc/introspect` to look up identity + permissions per request. Less
secure than Pattern A — exposed to cookie theft within the parent
domain — but simpler when both parties live behind one reverse proxy.

## Cryptography

- Random tokens: 32 bytes (256 bits) from `crypto/rand`, base64url.
- Pairing codes: 8 chars from 30-char unambiguous alphabet
  (rejection-sampled) ≈ 40 bits.
- WebAuthn user handle: 64 bytes random.
- JWT signing: RS256 (2048-bit RSA). Easy ecosystem support, asymmetric
  so RPs verify with public key only. Keys stored as PEM in
  `oidc_signing_key`. Rotation: add new key, mark active, keep old
  verifying for `> max(token lifetime)`.
- Access token lifetime: 10 min (RFC 9700 recommendation: short).
- Refresh token lifetime: 30 days, single-use rotation; reuse detection
  invalidates the family.
- Session lifetime: 8 h sliding refresh (carried over from picotera).
- Sudo grant: 5 min, single-use.

## Threat model (delta from picotera)

Identity-layer threats — same as picotera, all mitigations carried over:
- Stolen session cookie → live `account.disabled` check + sudo for
  sensitive actions
- Cloned authenticator → go-webauthn sign-count regression detection
- Bearer-token URL leak → device pairing avoids it; admin-issued
  recovery is the only bearer-token surface (small, gated)
- Phishing → origin-bound WebAuthn, server-verified challenges
- Brute force on enrollment tokens / pairing codes → rate limiting +
  high-entropy tokens + short TTLs

New OIDC-layer threats:
- **Token theft** → short access-token lifetimes, refresh-token
  rotation with reuse detection, JWT verification mandatory.
- **Authorization code interception** → PKCE required (no plain code
  flow accepted; redirect_uri must match exactly).
- **Mix-up attacks** → RFC 9207 `iss` parameter in authorization
  response.
- **Open redirect** → `redirect_uri` must exactly match a value in the
  client's registered list; no wildcards.
- **Client secret leak** → public clients use PKCE without secret;
  confidential clients use secret. Treat `client_secret` like a
  password (hashed at rest with argon2id).
- **`alg: none` confusion** → JWT verification configured for `RS256`
  only.

## Out of scope (for v1)

- Multi-tenancy
- SAML
- Dynamic client registration (RFC 7591)
- Consent screen (assumed first-party)
- DPoP / sender-constrained tokens
- Pushed Authorization Requests (PAR), JAR
- Pairwise sub identifiers (single sub per user across all clients)
- Social login federation
- Account linking
- Email/SMS channels of any kind
- Self-service account recovery (admin-issued recovery link remains
  the only path)
- Hardware security keys provisioning workflow
- Audit log export / SIEM integration

Each is a clean future addition without breaking the v1 surface.
