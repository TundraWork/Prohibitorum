# Prohibitorum

> Index Librorum Prohibitorum

Prohibitorum is a homegrown identity provider for small orgs. Single-tenant, first-party,
no email channel; admin-issued enrollment is the only recovery path.

- **Upstream auth methods:** WebAuthn (preferred, phishing-resistant),
  Password + TOTP (fallback for users without passkey-capable devices),
  upstream OIDC federation (Google / Entra / Keycloak / any OIDC IdP).
- **Downstream protocols:** OIDC OP (for modern apps), SAML IdP (for
  GitHub Enterprise Server and other legacy SaaS).
- **Authorization model:** opaque `attributes` map per account that
  flows verbatim into ID-token claims and SAML AttributeStatement.
  Relying parties enforce policy; Prohibitorum just answers "who is
  this and what do you know about them?"

## Status

v0.6 shipped — the full IdP is live and smoke-verified end-to-end against a
live dev server (121 steps + DB-state assertions):

- **v0.1 / v0.2** — WebAuthn enrollment/login + `/me` + sessions; Password +
  TOTP + recovery codes. Sudo step-up accepts two methods (`webauthn` /
  `password_totp` — recovery codes route through a dedicated re-enrollment
  ceremony, not sudo); `POST /me/auth/revoke-password-totp` drops the
  fallback once a passkey is confirmed working.
- **v0.3** — upstream OIDC federation (`auto_provision` / `link_only` /
  `invite_only`).
- **v0.4** — downstream OIDC OP (Authorization Code + PKCE, RFC 9068 access
  tokens, refresh rotation + reuse detection, introspection, revocation,
  RP-Initiated Logout).
- **v0.5** — SAML 2.0 IdP (SP-initiated SSO + IdP-local SLO + metadata),
  GHES-compatible profile.
- **v0.6** — forced re-auth (`prompt=login` / `max_age` / `ForceAuthn`),
  `NameIDPolicy/@Format`, POST-binding AuthnRequest, signed SAML metadata,
  IdP-initiated SSO, and the admin + end-user dashboard.

Still ahead: v0.7+ hardening (HSM/KMS-backed signing, front-channel SLO,
DPoP/PAR, SIEM export). See `STATUS.md` for the roadmap and `AUDIT.md` for
the spec-compliance checklist.

## Quickstart

Requires Postgres 14+ and a Redis-compatible KV (or in-process memory).

```bash
# 1. Toolchain via mise
mise install

# 2. Environment — DEK (required at boot) and origin (required by enroll-admin)
export PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(openssl rand -base64 32)"
export PROHIBITORUM_PUBLIC_ORIGIN="http://localhost:8080"

# 3. Apply migrations
export PROHIBITORUM_DATABASE_URL="postgres://prohibitorum:prohibitorum@localhost:5432/prohibitorum?sslmode=disable"
mise run db:up

# 4. Bootstrap the first admin
go run ./cmd/prohibitorum enroll-admin
# Prints an enrollment URL (http://localhost:8080/enroll/<token>). Open it in
# a browser to run the passkey-enrollment ceremony in the dashboard
# (EnrollView), or drive the API directly:
#   POST /api/prohibitorum/enrollments/{token}/register/begin
#   POST /api/prohibitorum/enrollments/{token}/register/complete

# 5. Run the server
mise run server
# The full IdP surface is mounted:
#   Upstream auth (/api/prohibitorum): WebAuthn enrollment/login, password+TOTP,
#     recovery ceremony, upstream OIDC federation, /me + sudo.
#   OIDC OP (issuer root): /oauth/{authorize,token,userinfo,introspect,revoke,jwks},
#     /oidc/logout, /.well-known/openid-configuration. /oauth/jwks serves the
#     active signing key once one is provisioned (`prohibitorum signing-key generate`).
#   SAML IdP (issuer root): /saml/{metadata,sso,slo,sso/init}.
#   Admin API (/api/prohibitorum): oidc-clients, saml-providers, upstream-idps,
#     signing-keys, audit-events, accounts.
#   Dashboard SPA: served as the router fallback (embedded from pkg/webui/dist).

# 6. Dashboard dev server (hot-reload). The built SPA is already embedded into
#    the server binary (pkg/webui/dist), so this is only needed for frontend
#    work; rebuild the embedded bundle with `mise run build`.
mise run web
```

If `mise run db:up` fails because `goose` isn't installed, see
`STATUS.md` for the `aqua:pressly/goose` workaround.

### Deployment hardening

The KV store backs session lookups, single-use auth codes / federation state,
PKCE verifiers, and enrollment tokens. Session secrets are stored hashed (the
KV key is `session:<id>:<SHA-256(token)>`, never the raw cookie token), but the
flow secrets above still live in the KV, so in any non-loopback deployment the
Redis backend **must be network-isolated and reached over an authenticated,
encrypted channel**:

```bash
export PROHIBITORUM_KV_DRIVER="redis"
export PROHIBITORUM_KV_REDIS_URL="redis.internal:6379"
export PROHIBITORUM_KV_REDIS_TLS="true"
export PROHIBITORUM_KV_REDIS_USERNAME="prohibitorum"   # Redis 6+ ACL (optional)
export PROHIBITORUM_KV_REDIS_PASSWORD="$REDIS_PASSWORD"
```

Outbound upstream-OIDC federation fetches (discovery / JWKS / token exchange)
run on an SSRF-hardened HTTP client that refuses to connect to loopback,
private (RFC1918 / ULA), or link-local / cloud-metadata addresses, and the
admin API rejects non-`https` or IP-literal issuer URLs. If you federate to an
IdP that legitimately lives on a private/internal network, opt in explicitly:

```bash
export PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK="true"  # default false
```

## Architecture in one paragraph

Three layers, acyclic import graph: an **identity store** (accounts,
credentials, federation links), an **authentication subsystem** that
turns one of four upstream methods (WebAuthn / Password / TOTP /
upstream OIDC) into a session, and a **protocol subsystem** that turns
that session into an OIDC OP response or a signed SAML assertion. The
`session` package is the contract between authentication and protocol —
RPs don't see how the user signed in, only the resulting claims.

## Why not …?

- **Keycloak / Authelia / Authentik** — bigger than needed; come with
  operational overhead (admin UIs, themes, plugins) we don't want to
  staff.
- **Ory Hydra** — token-issuance only, doesn't own user state. We need
  both.
- **Zitadel (the service)** — full IdP, but operational complexity
  similar to Keycloak.
- **Auth0 / Clerk / Stytch (SaaS)** — not self-hosted.

`zitadel/oidc` (the Go library, not the service) is used as the
OIDC OP toolkit; `crewjam/saml` for the SAML IdP side. Reimplementing
either by hand is a known antipattern.

## Docs

- [`ARCHITECTURE.md`](./ARCHITECTURE.md) — architecture, methods, protocols,
  threat model, scope.
- [`STATUS.md`](./STATUS.md) — what's done in v0.1 / v0.1.1 / v0.2, what's
  coming in v0.3 / v0.4 / v0.5 / v0.6 / v0.7+.
- [`INTEGRATION.md`](./INTEGRATION.md) — three integration patterns
  for relying parties (OIDC Code+PKCE, cookie+introspect, SAML SP).
- [`AUDIT.md`](./AUDIT.md) — per-layer compliance checklist with
  ✅ / ⚠️ deferred / ❌ gap labels per item.
