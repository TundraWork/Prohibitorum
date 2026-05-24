# Prohibitorum

> Index Librorum Prohibitorum — the list of who's allowed and what they can do.

Homegrown identity & authorization service. WebAuthn-only sign-in,
OpenID Connect for relying-party integration. Extracted from
[picotera](https://github.com/oott123/picotera)'s `feat-user-system`
branch into a standalone single-tenant IdP.

## Status

WIP. See `DESIGN.md` for the architecture and `AUDIT.md` for the
OIDC / OAuth 2.1 compliance checklist.

## Quickstart

Requires Postgres (any 14+), Redis-compatible KV (or in-process memory).

```bash
# 1. Toolchain via mise
mise install

# 2. Apply migrations
PROHIBITORUM_DATABASE_URL="postgres://prohibitorum:prohibitorum@localhost:5432/prohibitorum?sslmode=disable" \
  mise run db:up

# 3. Bootstrap the first admin
go run ./cmd/prohibitorum enroll-admin
# Copy the enrollment URL it prints; open in browser; register a passkey.

# 4. Run the server
PROHIBITORUM_PUBLIC_ORIGIN=http://localhost:8080 \
PROHIBITORUM_DATABASE_URL=... \
  mise run server

# 5. Dashboard dev
mise run web
```

## Architecture in one paragraph

A user signs in at Prohibitorum with a WebAuthn passkey, gets a
session cookie. Relying parties integrate via OIDC Authorization Code
+ PKCE: redirect the user to `/authorize`, get back a code, exchange
it at `/token` for an ID token + access token. RP back-ends validate
the ID token's signature against the JWKS endpoint, then trust its
claims (`sub`, `username`, `role`, `permissions`). Prohibitorum owns
the account directory; RPs enforce authorization using the claims.

## Why not …?

- **Keycloak / Authelia / Authentik** — bigger than needed for a small
  org; come with operational overhead (admin UIs, themes, plugins) we
  don't want to staff.
- **Ory Hydra** — token-issuance only, doesn't own user state. We need
  both.
- **Zitadel (the service)** — full IdP, but operational complexity
  similar to Keycloak. We're extracting from existing code, not
  shopping for a vendor.
- **Auth0 / Clerk / Stytch (SaaS)** — not self-hosted.

`zitadel/oidc` (the Go library, not the service) **is** used as the
OIDC OP toolkit because reimplementing OIDC verification by hand is a
known antipattern.

## Docs

- [`DESIGN.md`](./DESIGN.md) — architecture, threat model, scope.
- [`INTEGRATION.md`](./INTEGRATION.md) — how an RP integrates.
- [`AUDIT.md`](./AUDIT.md) — OAuth 2.1 / OIDC best-practice checklist.
