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

The dashboard SPA (`/`, `/admin/*`) is embedded into the server binary, so a
single `./prohibitorum` process serves the whole IdP plus its admin UI.

## Status

The full IdP is live and smoke-verified end-to-end. Done and planned (no fixed
schedule):

**Authentication**
- [x] WebAuthn passkeys — enrollment + login
- [x] Password + TOTP fallback — recovery codes + forced re-enrollment ceremony
- [x] Sudo step-up — WebAuthn, password+TOTP, and federated OIDC re-auth
- [x] Upstream OIDC federation — auto-provision / link-only / invite-only
- [ ] Password breach-list check on set (HIBP k-anonymity / blocklist)

**Downstream protocols**
- [x] OIDC OP — Authorization Code + PKCE, RFC 9068 access tokens, refresh rotation + reuse detection, introspection, revocation
- [x] RP-Initiated Logout; forced re-auth (`prompt=login` / `max_age`)
- [x] SAML 2.0 IdP — SP- and IdP-initiated SSO, IdP-local SLO, signed metadata, GHES profile
- [ ] Coordinated sign-out — OIDC front-/back-channel + SAML multi-SP SLO

**Dashboard & UX**
- [x] Admin console — accounts, invitations, OIDC clients, SAML SPs, upstream IdPs, signing keys, audit log
- [x] End-user area — passkeys, password/TOTP, active sessions, connected accounts, device pairing
- [ ] Logged-in landing is a launchpad of the user's linked apps — one click into each
- [ ] Users can manage their own linked apps (review / revoke access)
- [ ] Profile moves into a popover under the corner username + logout
- [ ] Security becomes the default view inside the account dashboard
- [ ] RBAC — per-provider (up/downstream) management of authorized users

**Operations & hardening**
- [x] Signing keys sealed at rest (AES-256-GCM, versioned DEK)
- [x] HTTP security headers from the embedded SPA (CSP, X-Frame-Options, X-Content-Type-Options)
- [x] Pre-ship logic-correctness / security-invariant audit of the auth cores
- [ ] HSM/KMS-backed signing (key never leaves the vault)
- [ ] Audit-log export / SIEM

Conditional / on-demand extras (DPoP, PAR, mTLS, SAML assertion encryption,
pairwise `sub`) and explicit non-goals are tracked in `AUDIT.md` and
`ARCHITECTURE.md`.

## Quickstart (production-style)

Requires Postgres 14+ and a Redis-compatible KV (or the in-process memory
driver). The toolchain is pinned in `mise.toml`.

```bash
# 1. Toolchain (go 1.26, node 24, pnpm 10, sqlc, goose)
mise install

# 2. Required config: a data-encryption key (boot fails without one) + origin.
export PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(openssl rand -base64 32)"
export PROHIBITORUM_PUBLIC_ORIGIN="https://auth.example.com"
export PROHIBITORUM_DATABASE_URL="postgres://prohibitorum:prohibitorum@localhost:5432/prohibitorum?sslmode=disable"

# 3. Migrations (optional — the server and DB-backed CLI commands auto-apply on boot).
mise db:up

# 4. Mint an OIDC signing key — /oauth/jwks and signed tokens need one.
go run ./cmd/prohibitorum signing-key generate

# 5. Bootstrap the first admin. Prints an enrollment URL; open it in a browser
#    to run the passkey-enrollment ceremony.
go run ./cmd/prohibitorum enroll-admin

# 6. Run the server (defaults to :8080; see PROHIBITORUM_HOST / _PORT).
mise server
```

What gets mounted (all on one origin):

- **Upstream auth** (`/api/prohibitorum/*`): WebAuthn enroll/login, password+TOTP,
  recovery ceremony, upstream OIDC federation, `/me` + sudo step-up.
- **OIDC OP** (issuer root): `/oauth/{authorize,token,userinfo,introspect,revoke,jwks}`,
  `/oidc/logout`, `/.well-known/openid-configuration`.
- **SAML IdP** (issuer root): `/saml/{metadata,sso,slo,sso/init}`.
- **Admin API** (`/api/prohibitorum/*`): `oidc-applications`, `saml-applications`,
  `identity-providers`, `signing-keys`, `audit-events`, `accounts`, `invitations`.
- **Dashboard SPA**: served as the router fallback (embedded from `pkg/webui/dist`).

See [`api.md`](./api.md) for the full HTTP surface and [`INTEGRATION.md`](./INTEGRATION.md)
for relying-party integration patterns.

## Development

**Prerequisites:** `mise install` (toolchain) and a reachable local Postgres.
The dashboard's npm dependencies install automatically on the first
`mise dev-server` / `mise build` / `mise web`.

The `mise dev-*` tasks give you a self-contained loop with no manual env setup.
They source `scripts/dev-env.sh`, which:

- generates a stable data-encryption key once into `.dev/encryption-key` (gitignored),
- points at a dedicated **`prohibitorum_dev`** database, isolated from the smoke's
  `postgres` DB, and auto-creates it when `psql` is available. The connection
  honors libpq's `PGUSER` / `PGHOST` / `PGPORT`, defaulting to
  `postgres://$USER@localhost:5432/prohibitorum_dev?sslmode=disable`. Point at a
  different cluster by setting `PGPORT` etc., or set `PROHIBITORUM_DATABASE_URL`
  directly (which also skips the auto-create — use an existing, migratable DB).

```bash
# Terminal 1 — build the SPA, run the embedded server on :8080, auto-migrate.
mise dev-server

# Terminal 2 — bootstrap an admin against the same dev DB/key. Prints an
# /enroll/<token> URL; open it to register a passkey and sign in.
mise enroll-admin -- --new

# Optional — seed example providers/accounts/invitations so data-driven
# dashboard elements render (idempotent; refuses to run off-loopback).
mise dev-seed
```

**Frontend.** `dashboard/` is a Vue 3 + Vite + Tailwind v4 + shadcn-vue/Reka UI
SPA. Use `mise web` for a hot-reloading dev server against the
running backend. The shipped UI is embedded via `go:embed` from the **committed**
`pkg/webui/dist`, so after any change that should land in the binary, rebuild and
commit the bundle:

```bash
mise build                 # builds dashboard/dist -> pkg/webui/dist, then compiles ./prohibitorum
git add pkg/webui/dist     # Vite chunk hashes change each build; commit the bundle
```

**Tests.**

```bash
go build ./... && go vet ./... && go test ./...   # backend
cd dashboard && npm ci && npm test                # frontend unit tests (vitest)
cd dashboard && npm run build                     # FE typecheck (vue-tsc -b) + production build
```

**End-to-end smoke** (`cmd/smoke`) drives a real server over HTTP and bootstraps
its own admin. Point a server at a throwaway DB and run it against that origin.
Because the smoke's federation arc runs an in-process mock OP on loopback, the
server must opt the outbound federation client out of the SSRF dial-screen:

```bash
export PROHIBITORUM_DATABASE_URL="postgres://tundra@localhost:55432/postgres?sslmode=disable"
export PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(cat .dev/encryption-key)"
export PROHIBITORUM_PUBLIC_ORIGIN="http://localhost:8080"
export PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK="true"   # in-process mock OP is on loopback
go run ./cmd/prohibitorum &                                   # auto-migrates on boot
go run ./cmd/smoke --base-url http://localhost:8080
```

## mise tasks

Run as `mise <task>` (or `mise run <task>`). Tasks marked **dev** source
`scripts/dev-env.sh` and target the loopback `prohibitorum_dev` DB.

| Task | What it does |
|------|--------------|
| `mise install` | Install the pinned toolchain (go 1.26, node 24, pnpm 10, sqlc 1.30.0, goose 3.27.0). |
| `mise server` | Run the Go server (`go run ./cmd/prohibitorum/main.go`) using your current env. |
| `mise web` | Dashboard dev server with hot reload (`npm run dev`; installs deps on first run). |
| `mise frontend-build` | Install + build the SPA into `dashboard/dist` (`npm ci && npm run build`). |
| `mise build` | Build the SPA into `pkg/webui/dist`, then compile the `./prohibitorum` binary (which embeds it). |
| `mise openapi` | Regenerate `openapi.yaml` from the running humacli. |
| `mise db:up` | Apply goose migrations against `$PROHIBITORUM_DATABASE_URL`. |
| `mise db:status` | Show migration status. |
| `mise dev-server` | **dev** — build the SPA + run the embedded server on `:8080` with dev defaults (auto-created `prohibitorum_dev` DB + stable `.dev/encryption-key`). Auto-migrates on boot. |
| `mise enroll-admin [-- FLAGS]` | **dev** — issue an admin enrollment URL against the dev DB. Pass flags after `--`, e.g. `-- --new` or `-- --reset --username alice`. |
| `mise dev-seed` | **dev** — seed `prohibitorum_dev` with example providers/accounts/invitations (idempotent, loopback-only). |

## CLI commands

Invoke as `go run ./cmd/prohibitorum <command>` (dev) or `./prohibitorum <command>`
(after `mise build`). With no subcommand, the binary runs the server. Every
DB-backed command auto-applies migrations first. For day-to-day management the
admin dashboard (`/admin/*`) covers the same surface; the CLI is for
bootstrapping and automation.

| Command | Purpose |
|---------|---------|
| `enroll-admin [--new] [--reset --username NAME]` | Issue a passkey-enrollment URL for an admin. Default errors if an admin already exists; `--new` adds another; `--reset` recovers a named admin. |
| `signing-key generate [--activate] [--retire KID]` | Mint an RSA-2048 OIDC signing key. The first key (or any `--activate`) becomes active; `--retire KID` decommissions a key (refused for the active key). |
| `oidc-client create \| list \| update \| rotate-secret \| delete` | Manage downstream OIDC clients (relying parties). `create`/`rotate-secret` print a confidential secret exactly once. |
| `saml-sp create \| list \| update \| delete` | Manage downstream SAML service providers. `create` ingests `--metadata-file`/`--metadata-url` or manual `--entity-id`/`--acs-url`; `--kind ghes` installs the GHES profile. |
| `upstream-idp create \| list \| update \| rotate-secret \| delete` | Manage upstream OIDC IdPs for federation. The client secret is AES-GCM sealed at rest. |
| `openapi` | Print the OpenAPI spec to stdout. |
| `dev-seed` | Seed the dev database (loopback-only). |

Run `<command> --help` for the full flag list. Note the CLI verbs keep their
protocol-oriented names (`oidc-client`, `saml-sp`, `upstream-idp`) while the
admin HTTP API uses the role-oriented names (`oidc-applications`,
`saml-applications`, `identity-providers`).

## Configuration

Configuration comes from `PROHIBITORUM_*` env vars (or an optional `config.yaml`
in the working directory); nested keys upper-case and join with `_`
(`oidc.access_token_ttl` → `PROHIBITORUM_OIDC_ACCESS_TOKEN_TTL`). Only the
data-encryption key is strictly required — boot fails without it.

See [`CONFIG.md`](./CONFIG.md) for the full environment-variable reference and
deployment-hardening guidance (network-isolated Redis over TLS, `TRUST_PROXY`
behind a reverse proxy, the outbound-federation SSRF dial-screen).

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

`zitadel/oidc` (the Go library, not the service) is the OIDC OP toolkit;
`crewjam/saml` handles the SAML IdP side.

## Docs

- [`ARCHITECTURE.md`](./ARCHITECTURE.md) — architecture, methods, protocols,
  threat model, scope.
- [`STATUS.md`](./STATUS.md) — what's done per version and what's coming.
- [`api.md`](./api.md) — the HTTP surface (runtime protocol endpoints + admin API).
- [`CONFIG.md`](./CONFIG.md) — environment-variable reference + deployment hardening.
- [`INTEGRATION.md`](./INTEGRATION.md) — three integration patterns for relying
  parties (OIDC Code+PKCE, cookie+introspect, SAML SP).
- [`DESIGN.md`](./DESIGN.md) / [`PRODUCT.md`](./PRODUCT.md) — design tokens and
  product framing for the dashboard.
- [`AUDIT.md`](./AUDIT.md) — per-layer compliance checklist with ✅ / ⚠️ deferred
  / ❌ gap labels per item.
