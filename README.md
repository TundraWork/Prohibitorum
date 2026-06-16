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

The full IdP is live, smoke-verified end-to-end, and security-audited. Done and
planned:

**Authentication**
- [x] WebAuthn passkey sign-in
- [x] Password + TOTP sign-in (fallback)
- [x] Upstream OIDC federation (sign in with an external provider)
- [x] Step-up re-authentication for sensitive actions

**Downstream protocols**
- [x] OIDC provider for apps
- [x] SAML 2.0 identity provider (GHES-compatible)
- [ ] Coordinated single sign-out across apps

**Dashboard**
- [x] Admin console — accounts, apps, providers, signing keys, audit log
- [x] End-user self-service — credentials, sessions, devices, linked accounts
- [ ] End-user app launchpad — launch authorized apps and self-manage access
- [x] RBAC — control which users each app is authorized for

**Keys & operations**
- [x] Signing-key lifecycle — rotation, grace windows, sealed at rest
- [ ] KMS/HSM-backed signing
- [ ] Audit-log export to SIEM

Conditional / on-demand extras (DPoP, PAR, mTLS, SAML assertion encryption,
pairwise `sub`), smaller hardening items, and explicit non-goals are tracked in
`AUDIT.md` and `ARCHITECTURE.md`.

## Quickstart

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

**Prerequisites:** [`mise`](https://mise.jdx.dev) and [Podman](https://podman.io)
(for the dev database). `mise install` provides the rest of the toolchain — Go,
Node, pnpm, sqlc, goose, and the `psql` client. The dashboard's npm dependencies
install automatically on the first `mise dev-server` / `mise build` / `mise web`.

### Runtime

`./prohibitorum` is a single Go binary. It requires a Postgres database
(`PROHIBITORUM_DATABASE_URL`), a data-encryption key
(`PROHIBITORUM_DATA_ENCRYPTION_KEY_V1`; the server refuses to start without it),
and a KV store (Redis, or the default in-process `memory` driver). On startup it
applies any pending migrations, then listens on `:8080`, serving the upstream-auth
API, the OIDC OP, the SAML IdP, and the embedded dashboard SPA on a single origin.
`go run ./cmd/prohibitorum` runs the same server without building a binary.

The dev database runs in a container (`compose.yaml`); the rest runs on the host
via `mise`.

### Local development

```bash
mise install                  # toolchain: Go, Node, pnpm, sqlc, goose, psql client
podman compose up -d          # Postgres on localhost:5432 (see compose.yaml)
mise dev-server               # build the SPA and run the server on :8080 (auto-migrates)
mise enroll-admin -- --new    # create an admin; prints an /enroll/<token> URL
# open http://localhost:8080
```

The `dev-server`, `enroll-admin`, and `dev-seed` tasks source
`scripts/dev-env.sh`, which generates a stable `.dev/encryption-key` (gitignored)
and sets `PROHIBITORUM_DATABASE_URL` to the compose database
(`postgres://prohibitorum:prohibitorum@localhost:5432/prohibitorum_dev`). Set it
explicitly to target a different database.

```bash
mise dev-seed                 # optional: seed example providers/accounts/invitations
podman compose down           # stop the database (data persists in the volume)
podman compose down -v        # stop and wipe the database
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
its own admin. It runs against the compose Postgres, using the throwaway
`postgres` maintenance database (isolated from `prohibitorum_dev`). Because the
smoke's federation arc runs an in-process mock OP on loopback, the server must
opt the outbound federation client out of the SSRF dial-screen:

```bash
podman compose up -d                                          # if not already running
export PROHIBITORUM_DATABASE_URL="postgres://prohibitorum:prohibitorum@localhost:5432/postgres?sslmode=disable"
export PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(openssl rand -base64 32)"
export PROHIBITORUM_PUBLIC_ORIGIN="http://localhost:8080"
export PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK="true"   # in-process mock OP is on loopback
go run ./cmd/prohibitorum &                                   # auto-migrates on boot
go run ./cmd/smoke --base-url http://localhost:8080
```

## mise tasks

Run as `mise <task>` (or `mise run <task>`). The dev database is **not** a mise
task — start it with `podman compose up -d` (see `compose.yaml`). Tasks marked
**dev** source `scripts/dev-env.sh` and target the `prohibitorum_dev` database it
provides (override by exporting `PROHIBITORUM_DATABASE_URL`).

| Task | What it does |
|------|--------------|
| `mise install` | Install the pinned toolchain (go 1.26, node 24, pnpm 10, sqlc 1.30.0, goose 3.27.0, and the `psql` client). |
| `mise server` | Run the Go server (`go run ./cmd/prohibitorum`) using your current env. |
| `mise web` | Dashboard dev server with hot reload (`npm run dev`; installs deps on first run). |
| `mise frontend-build` | Install + build the SPA into `dashboard/dist` (`npm ci && npm run build`). |
| `mise build` | Build the SPA into `pkg/webui/dist`, then compile the `./prohibitorum` binary (which embeds it). |
| `mise openapi` | Regenerate `openapi.yaml` from the running humacli. |
| `mise db:up` | **dev** — apply goose migrations to `prohibitorum_dev` (or to `$PROHIBITORUM_DATABASE_URL` if you export one). |
| `mise db:status` | **dev** — show migration status for the dev database. |
| `mise dev-server` | **dev** — build the SPA + run the embedded server on `:8080` against the compose `prohibitorum_dev` DB + stable `.dev/encryption-key`. Auto-migrates on boot. |
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
