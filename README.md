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

v0.6 shipped — the full IdP is live and smoke-verified end-to-end against a live
dev server, and a pre-ship logic-correctness / security-invariant audit of the
auth-critical cores has been completed and remediated
(`docs/superpowers/notes/2026-06-11-preship-logic-correctness-audit.md`).

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

# 3. Migrations. (The server and every DB-backed CLI command also auto-apply
#    migrations on start, so this step is optional.)
mise db:up

# 4. Mint an OIDC signing key — /oauth/jwks and signed tokens need one.
go run ./cmd/prohibitorum signing-key generate

# 5. Bootstrap the first admin. Prints an enrollment URL; open it in a browser
#    to run the passkey-enrollment ceremony (the EnrollView page).
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

The `mise dev-*` tasks give you a self-contained loop with no manual env setup.
They source `scripts/dev-env.sh`, which:

- generates a stable data-encryption key once into `.dev/encryption-key` (gitignored),
- points at a dedicated **`prohibitorum_dev`** database, isolated from the smoke's
  `postgres` DB, and auto-creates it when `psql` is available. The default URL is
  `postgres://tundra@localhost:55432/prohibitorum_dev?sslmode=disable` — override
  `PROHIBITORUM_DATABASE_URL` (and `PROHIBITORUM_PUBLIC_ORIGIN`) to use a different
  cluster, in which case DB auto-create is skipped.

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
SPA (no Nuxt UI). Use `mise web` for a hot-reloading dev server against the
running backend. The shipped UI is embedded via `go:embed` from the **committed**
`pkg/webui/dist`, so after any change that should land in the binary, rebuild and
commit the bundle:

```bash
mise build                 # builds dashboard/dist -> pkg/webui/dist, then compiles ./prohibitorum
git add pkg/webui/dist     # Vite chunk hashes are non-deterministic; commit dist deliberately
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
| `mise web` | Dashboard dev server with hot reload (`pnpm --dir dashboard dev`). |
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

## Configuration — environment variables

Config is read from `PROHIBITORUM_*` env vars (an optional `config.yaml` in the
working directory is also honored). Nested keys map by upper-casing and joining
with `_` (e.g. `oidc.access_token_ttl` → `PROHIBITORUM_OIDC_ACCESS_TOKEN_TTL`).
Durations use Go syntax (`10m`, `8h`, `720h`, `60s`). Defaults below are the
in-code defaults; **only the data-encryption key is strictly required** (boot
fails without one).

### Core

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>` | — (**required**) | base64-encoded AES-256 key (32 bytes). The highest version `<n>` is used for new writes; lower versions stay available for decryption. Set at least one (e.g. `_V1`). |
| `PROHIBITORUM_DATABASE_URL` | — | Postgres connection string. Required for the server and every DB-backed CLI command. |
| `PROHIBITORUM_PUBLIC_ORIGIN` | `http://localhost:8080` | Comma-separated public origin(s). Seeds the OIDC issuer, SAML EntityID + endpoint URLs, and the WebAuthn RP ID/origins when those aren't set explicitly. |
| `PROHIBITORUM_HOST` | `""` (all interfaces) | Bind interface; set e.g. `127.0.0.1` to listen loopback-only behind a reverse proxy. |
| `PROHIBITORUM_PORT` | `8080` | Bind port. |
| `PROHIBITORUM_SESSION_TTL` | `8h` | Session lifetime (cookie + KV). |
| `PROHIBITORUM_TRUST_PROXY` | `false` | Honor `X-Forwarded-For` / `X-Forwarded-Proto`. Enable only behind a trusted reverse proxy. |

### KV store

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROHIBITORUM_KV_DRIVER` | `memory` | `memory` (single-process dev) or `redis`. |
| `PROHIBITORUM_KV_REDIS_URL` | `localhost:6379` | Redis address. |
| `PROHIBITORUM_KV_REDIS_USERNAME` | `""` | Redis 6+ ACL username (optional). |
| `PROHIBITORUM_KV_REDIS_PASSWORD` | `""` | Redis password. |
| `PROHIBITORUM_KV_REDIS_TLS` | `false` | Connect to Redis over TLS. |

### OIDC OP

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROHIBITORUM_OIDC_ISSUER` | `PublicOrigins[0]` | Issuer string embedded in tokens + discovery. |
| `PROHIBITORUM_OIDC_ACCESS_TOKEN_TTL` | `10m` | Access-token lifetime. |
| `PROHIBITORUM_OIDC_ID_TOKEN_TTL` | `10m` | ID-token lifetime. |
| `PROHIBITORUM_OIDC_REFRESH_TOKEN_TTL` | `720h` (30d) | Refresh-token / family lifetime (slides forward on rotation). |
| `PROHIBITORUM_OIDC_AUTHORIZATION_CODE_TTL` | `60s` | Authorization-code lifetime (single-use). |
| `PROHIBITORUM_OIDC_JWKS_CACHE_MAX_AGE` | `5m` | `Cache-Control: max-age` on `/oauth/jwks` + discovery. |

### WebAuthn

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROHIBITORUM_WEBAUTHN_RP_ID` | host of `PublicOrigins[0]` | WebAuthn Relying Party ID. Override when the RP ID differs from the origin hostname. |
| `PROHIBITORUM_WEBAUTHN_RP_DISPLAY_NAME` | `Prohibitorum` | RP display name shown by authenticators (also the TOTP issuer fallback). |
| `PROHIBITORUM_WEBAUTHN_RP_ORIGINS` | `PublicOrigins` | Comma-separated allowed WebAuthn origins. |

### Upstream OIDC federation

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROHIBITORUM_FEDERATION_STATE_TTL` | `10m` | Lifetime of the single-use federation state blob. |
| `PROHIBITORUM_FEDERATION_DEFAULT_SCOPES` | `openid,profile,email` | Scopes requested from an upstream when none are set per-IdP. (List value — prefer `config.yaml`.) |
| `PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK` | `false` | Disable the outbound federation client's SSRF dial-screen. Set `true` only for a trusted internal upstream IdP (or the loopback mock OP in tests). |

### TOTP

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROHIBITORUM_TOTP_DEFAULT_PERIOD` | `30` | TOTP period (seconds). |
| `PROHIBITORUM_TOTP_DEFAULT_DIGITS` | `6` | TOTP digit count. |
| `PROHIBITORUM_TOTP_DEFAULT_ALGORITHM` | `SHA1` | RFC 6238 HMAC algorithm. |
| `PROHIBITORUM_TOTP_DRIFT_STEPS` | `1` | Accepted ± step drift on verify. |
| `PROHIBITORUM_TOTP_RECOVERY_CODE_COUNT` | `10` | Recovery codes minted per enrollment. |
| `PROHIBITORUM_TOTP_ISSUER` | `webauthn.rp_display_name` | Label in the `otpauth://` URI. |

### Cross-factor auth

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROHIBITORUM_AUTH_SUDO_TTL` | `5m` | Window a step-up (sudo) grant stays valid. |
| `PROHIBITORUM_AUTH_PARTIAL_SESSION_TTL` | `5m` | Window a password-only partial session has to complete the TOTP step. |
| `PROHIBITORUM_AUTH_THROTTLE_SCHEDULE` | `0,0,1s,2s,4s,8s,16s,32s,1m,2m,4m,8m,15m` | Per-failure lockout ladder (last entry clamps). List value — prefer `config.yaml`. |

### SAML IdP

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROHIBITORUM_SAML_ENTITY_ID` | `PublicOrigins[0]` | Stable IdP SAML EntityID. Choose one that never changes — changing it breaks every registered SP. |
| `PROHIBITORUM_SAML_DEFAULT_NAMEID_FORMAT` | `urn:oasis:names:tc:SAML:1.1:nameid-format:persistent` | Default NameID format. |
| `PROHIBITORUM_SAML_SESSION_LIFETIME` | `8h` | Default `SessionNotOnOrAfter` horizon. |
| `PROHIBITORUM_SAML_METADATA_ROTATION_GRACE` | `168h` (7d) | Signing-key decommission grace advertised in metadata. |
| `PROHIBITORUM_SAML_METADATA_VALIDITY` | `24h` | `validUntil` on published IdP metadata. |

### Password hashing (argon2id)

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROHIBITORUM_PASSWORD_HASH_MEMORY_KIB` | `65536` (64 MiB) | argon2id memory cost. |
| `PROHIBITORUM_PASSWORD_HASH_ITERATIONS` | `3` | argon2id time cost. |
| `PROHIBITORUM_PASSWORD_HASH_PARALLELISM` | `1` | argon2id lanes. |

## Deployment hardening

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
IdP that legitimately lives on a private/internal network, opt in explicitly
with `PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK=true`.

Behind a TLS-terminating reverse proxy, set `PROHIBITORUM_TRUST_PROXY=true` so
client-IP and scheme are read from the forwarded headers, and keep
`PROHIBITORUM_PUBLIC_ORIGIN` on `https://…` so secure cookies are issued.

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
- [`STATUS.md`](./STATUS.md) — what's done per version and what's coming.
- [`api.md`](./api.md) — the HTTP surface (runtime protocol endpoints + admin API).
- [`INTEGRATION.md`](./INTEGRATION.md) — three integration patterns for relying
  parties (OIDC Code+PKCE, cookie+introspect, SAML SP).
- [`DESIGN.md`](./DESIGN.md) / [`PRODUCT.md`](./PRODUCT.md) — design tokens and
  product framing for the dashboard.
- [`AUDIT.md`](./AUDIT.md) — per-layer compliance checklist with ✅ / ⚠️ deferred
  / ❌ gap labels per item.
</content>
