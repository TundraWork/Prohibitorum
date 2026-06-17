# Prohibitorum

> *Index Librorum Prohibitorum*

[![ci](https://github.com/TundraWork/Prohibitorum/actions/workflows/ci.yml/badge.svg)](https://github.com/TundraWork/Prohibitorum/actions/workflows/ci.yml)
![go](https://img.shields.io/badge/go-1.26-00ADD8?logo=go&logoColor=white)
![deploy](https://img.shields.io/badge/deploy-single%20binary-blue)

**A self-hosted, single-binary identity provider for small orgs.** Single-tenant,
first-party, no email channel — admin-issued enrollment is the only recovery path.
The dashboard SPA is embedded in the binary, so one `./prohibitorum` process is the
entire IdP plus its admin UI.

- **Sign-in** — WebAuthn passkeys (phishing-resistant, preferred), Password + TOTP fallback, or federation to any upstream OIDC IdP (Google, Entra, Keycloak).
- **Downstream** — OIDC provider for modern apps; SAML 2.0 IdP for GitHub Enterprise Server and other legacy SaaS.
- **Authorization** — a per-account `attributes` map flows verbatim into ID-token claims and SAML attributes. RPs enforce policy; Prohibitorum answers *who is this, and what do we know about them?*

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

Deploy the IdP as a single self-contained artifact — the SPA is embedded in the
binary, so there's nothing to host separately. Use the signed multi-arch **OCI
image** the release pipeline publishes (the binary is the image entrypoint, so
the same image runs the server *and* the bootstrap subcommands), or a release
binary. (For the local dev loop — hot reload, a throwaway DB, seeded data — see
[Development](#development).)

Requires Postgres 14+ and a Redis-compatible KV (or the in-process `memory`
driver). Config comes from the environment:

```bash
export PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(openssl rand -base64 32)"  # keep it stable; boot fails without it
export PROHIBITORUM_PUBLIC_ORIGIN="https://auth.example.com"
export PROHIBITORUM_DATABASE_URL="postgres://user:pass@db:5432/prohibitorum?sslmode=disable"
```

**Run the image** (`docker` or `podman`):

```bash
IMG=ghcr.io/tundrawork/prohibitorum:0.7.0           # release version (git tag v0.7.0 → image :0.7.0); or :latest

# (optional) verify the keyless cosign signature before running
cosign verify "$IMG" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github.com/TundraWork/Prohibitorum/[.]github/workflows/release[.]yml@'

# one-time bootstrap: mint a signing key + the first admin (prints an enrollment URL)
docker run --rm -e PROHIBITORUM_DATABASE_URL -e PROHIBITORUM_DATA_ENCRYPTION_KEY_V1 "$IMG" signing-key generate
docker run --rm -e PROHIBITORUM_DATABASE_URL -e PROHIBITORUM_DATA_ENCRYPTION_KEY_V1 -e PROHIBITORUM_PUBLIC_ORIGIN "$IMG" enroll-admin

# run the server (auto-applies migrations on boot; listens on :8080)
docker run -p 8080:8080 \
  -e PROHIBITORUM_DATABASE_URL -e PROHIBITORUM_DATA_ENCRYPTION_KEY_V1 -e PROHIBITORUM_PUBLIC_ORIGIN \
  "$IMG"
```

**Or a binary** — download a release archive from GitHub Releases, or build one
from source with `mise install && mise run prod:build` (→ `./prohibitorum`, SPA
embedded). Then `./prohibitorum signing-key generate`, `./prohibitorum
enroll-admin`, and `./prohibitorum` to serve.

Releases (image + binaries + SBOMs + checksums + signatures) are produced by the
GoReleaser + ko pipeline on a git tag — see [`TOOLING.md`](./TOOLING.md).

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

The local dev loop — hot reload, a throwaway DB, dev defaults, seeded data — via
`mise run dev:*` / `db:*`. (To deploy against your own config, see the
[Quickstart](#quickstart).)

The only prerequisite is [`mise`](https://mise.jdx.dev): `mise install` pins and
installs the whole toolchain — Go, Node/npm, sqlc, goose, **and prebuilt Postgres
18** — so no container runtime and no system Postgres are needed. The dashboard's
npm deps install on first run.

### Local development

```bash
mise install                       # toolchain: Go, Node/npm, sqlc, goose, prebuilt Postgres (pinned + locked)
mise run db:start                  # local Postgres on :5432 from the mise binaries (or: podman compose up -d)
mise run dev:server                # build the SPA + run the server on :8080 (auto-migrates)
mise run dev:enroll-admin -- --new # create an admin; prints an /enroll/<token> URL
# open http://localhost:8080
```

The `dev:*` tasks source `scripts/dev-env.sh` — a stable `.dev/encryption-key`
(gitignored) and `PROHIBITORUM_DATABASE_URL` pointed at the local
`prohibitorum_dev`. Export your own to override.

```bash
mise run dev:seed                  # optional: seed example providers/accounts/invitations
mise run db:stop                   # stop the local cluster, data persists (or: podman compose down)
mise run db:reset                  # stop and wipe the local cluster (or: podman compose down -v)
```

**Frontend.** `dashboard/` is a Vue 3 + Vite + Tailwind v4 + shadcn-vue/Reka UI
SPA; `mise run dev:web` runs it with hot reload against the backend. The shipped
UI is embedded via `go:embed` from the **committed** `pkg/webui/dist`, so rebuild
and commit the bundle after any change that should ship in the binary:

```bash
mise run build:web         # rebuild the SPA into pkg/webui/dist
git add pkg/webui/dist     # Vite chunk hashes change each build; commit the bundle
```

(CI's `ci:frontend` guard fails if the committed bundle drifts from a fresh build.)

**Tests.**

```bash
go build ./... && go vet ./... && go test ./...   # backend
cd dashboard && npm ci && npm test                # frontend unit tests (vitest)
cd dashboard && npm run build                     # FE typecheck (vue-tsc -b) + production build
```

**End-to-end smoke** (`cmd/smoke`) drives a real server over HTTP and bootstraps
its own admin, using the throwaway `postgres` maintenance database (isolated from
`prohibitorum_dev`). The one-command form is **`mise run ci:smoke`** (starts a
local Postgres + the server, runs the smoke, tears down). Equivalent manual steps
(the federation arc runs an in-process mock OP on loopback, so the server must opt
the outbound federation client out of the SSRF dial-screen):

```bash
mise run db:start                                             # local Postgres (or: podman compose up -d)
export PROHIBITORUM_DATABASE_URL="postgres://prohibitorum:prohibitorum@localhost:5432/postgres?sslmode=disable"
export PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(openssl rand -base64 32)"
export PROHIBITORUM_PUBLIC_ORIGIN="http://localhost:8080"
export PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK="true"   # in-process mock OP is on loopback
go run ./cmd/prohibitorum &                                   # auto-migrates on boot
go run ./cmd/smoke --base-url http://localhost:8080
```

## mise tasks

Run with `mise run <task>` (the `mise <task>` shorthand can be shadowed by future
mise subcommands). Tasks are **namespaced by context** so dev and prod commands
can't be confused — `mise tasks` lists them grouped:

| Namespace | Purpose |
|-----------|---------|
| `dev:*`   | local development — run the app, hot reload, seed, enroll an admin |
| `db:*`    | local Postgres lifecycle (dev + smoke; never prod) |
| `build:*` | build artifacts shared by dev and prod (SPA bundle, openapi) |
| `ci:*`    | the checks GitHub Actions runs (also runnable locally) |
| `prod:*`  | **production** build + release (release binary, OCI images) |

| Task | What it does |
|------|--------------|
| `mise install` | Install the pinned, lockfile-verified toolchain (go 1.26, node 24, sqlc 1.30.0, goose 3.27.0, prebuilt Postgres 18, goreleaser, cosign). |
| `mise run dev:server` | Build the SPA + run the embedded server on `:8080` against the dev DB + stable `.dev/encryption-key`. Auto-migrates. |
| `mise run dev:web` | Dashboard dev server with hot reload (`npm run dev`). |
| `mise run dev:run` | Run the server against your current env using the committed SPA (no rebuild). |
| `mise run dev:enroll-admin [-- FLAGS]` | Issue an admin enrollment URL against the dev DB (e.g. `-- --new`, `-- --reset --username alice`). |
| `mise run dev:seed` | Seed the dev DB with example data (idempotent, loopback-only). |
| `mise run dev:federation` | Two wired instances (upstream OP + downstream RP) for manual OIDC/enrollment/consent testing (see TOOLING.md). |
| `mise run db:start` / `db:stop` / `db:reset` | Start / stop / wipe the local Postgres cluster (`.dev/pgdata`, port 5432). |
| `mise run db:up` / `db:status` | Apply / show goose migrations on the dev DB. |
| `mise run build:web` | Build the SPA into `pkg/webui/dist` (the embedded bundle). |
| `mise run build:openapi` | Regenerate `openapi.yaml` from the running humacli. |
| `mise run ci` | The full fast gate (`ci:go` + `ci:frontend` with a dist-freshness guard) — same as CI's gate job. |
| `mise run ci:smoke` | End-to-end smoke (DB + server + `cmd/smoke`) — same as CI's smoke job. |
| `mise run prod:build` | Build the SPA + compile the `./prohibitorum` release binary (`-tags nodynamic`). |
| `mise run prod:release` | GoReleaser + ko: multi-arch OCI images, SBOMs, checksums, cosign signing (runs on a git tag). |

See [`TOOLING.md`](./TOOLING.md) for the full tooling architecture.

## CLI commands

Invoke as `go run ./cmd/prohibitorum <command>` (dev) or `./prohibitorum <command>`
(after `mise run prod:build`). With no subcommand, the binary runs the server. Every
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
- [`TOOLING.md`](./TOOLING.md) — tooling & dependency architecture (mise, lockfile, dev/CI/prod build).
- [`INTEGRATION.md`](./INTEGRATION.md) — three integration patterns for relying
  parties (OIDC Code+PKCE, cookie+introspect, SAML SP).
- [`DESIGN.md`](./DESIGN.md) / [`PRODUCT.md`](./PRODUCT.md) — design tokens and
  product framing for the dashboard.
- [`AUDIT.md`](./AUDIT.md) — per-layer compliance checklist with ✅ / ⚠️ deferred
  / ❌ gap labels per item.
