# Prohibitorum

> Index Librorum Prohibitorum — the list of who's allowed and what they can do.

A homegrown identity provider for small orgs. Single-tenant, first-party,
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

v0.2 shipped — **WebAuthn (v0.1) + Password+TOTP+recovery codes (v0.2)**
are smoke-verified end-to-end against a live dev server (45/45 steps +
DB-state assertions). Sudo step-up accepts all three methods;
`POST /me/auth/revoke-password-totp` lets users drop the fallback once
their passkey is confirmed working.

Still ahead: v0.3 upstream OIDC federation, v0.4 OIDC OP, v0.5 SAML IdP,
v0.6 frontend, v0.7+ hardening. See `STATUS.md` for the roadmap and
`AUDIT.md` for the spec-compliance checklist.

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
# Prints an enrollment URL. v0.6's dashboard will drive the in-browser
# passkey ceremony; until then, drive the API directly:
#   POST /api/prohibitorum/enrollments/{token}/register/begin
#   POST /api/prohibitorum/enrollments/{token}/register/complete
# (See STATUS.md "WebAuthn smoke without a frontend" for options.)

# 5. Run the server
mise run server
# Mounted in v0.1: WebAuthn enrollment/login + /me + /.well-known/openid-configuration
# + /oauth/jwks (JWKS returns empty `keys` until v0.4).
# Mounted in v0.2: /auth/{password/begin,totp/verify,recovery-code/verify}
# + /me/{password/set,totp/{begin,verify},recovery-codes/regenerate,auth/revoke-password-totp,sudo/methods}
# + extended /me/sudo/{begin,complete} dispatching on `method`.

# 6. Dashboard dev (v0.6+; dashboard/ is empty until then)
mise run web
```

If `mise run db:up` fails because `goose` isn't installed, see
`STATUS.md` for the `aqua:pressly/goose` workaround.

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

`zitadel/oidc` (the Go library, not the service) **is** used as the
OIDC OP toolkit; `crewjam/saml` for the SAML IdP side. Reimplementing
either by hand is a known antipattern.

## Docs

- [`DESIGN.md`](./DESIGN.md) — architecture, methods, protocols,
  threat model, scope.
- [`STATUS.md`](./STATUS.md) — what's done in v0.1 / v0.1.1 / v0.2, what's
  coming in v0.3 / v0.4 / v0.5 / v0.6 / v0.7+.
- [`INTEGRATION.md`](./INTEGRATION.md) — three integration patterns
  for relying parties (OIDC Code+PKCE, cookie+introspect, SAML SP).
- [`AUDIT.md`](./AUDIT.md) — per-layer compliance checklist with
  ✅ / ⚠️ deferred / ❌ gap labels per item.
