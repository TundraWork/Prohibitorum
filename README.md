# Prohibitorum

> *Index Librorum Prohibitorum*

[![ci](https://github.com/TundraWork/Prohibitorum/actions/workflows/ci.yml/badge.svg)](https://github.com/TundraWork/Prohibitorum/actions/workflows/ci.yml)
![go](https://img.shields.io/badge/go-1.26-00ADD8?logo=go&logoColor=white)
![deploy](https://img.shields.io/badge/deploy-single%20binary-blue)

**A self-hosted, single-binary identity provider for small orgs.** Single-tenant,
first-party, no email channel — admin-issued enrollment is the only recovery path.
The dashboard SPA is embedded in the binary: one `./prohibitorum` process is the
whole IdP plus its frontend.

**Frontend rewrite (M2):** the embedded UI shows bilingual component and public API
previews. Login, enrollment, self-service, application management and admin pages
are temporarily unavailable. Backend authentication and protocol APIs are unchanged.

- **Sign-in** — WebAuthn passkeys (preferred), Password + TOTP fallback, or federation through upstream OIDC, Steam, and VRChat providers.
- **Downstream** — OIDC provider for modern apps, SAML 2.0 IdP for GitHub Enterprise Server and other legacy SaaS, and a forward-auth gateway.
- **Authorization** — restricted apps use app-bound manual decisions and live calculated rules. The same service controls token/assertion issuance and app-aware group claims; a policy-management assignment never grants app usage.

## Status

**Authentication**
- [x] WebAuthn passkey sign-in
- [x] Password + TOTP sign-in (fallback)
- [x] Upstream OIDC federation
- [x] Steam OpenID 2.0 federation
- [x] VRChat profile-proof federation with reusable operator session
- [x] Step-up re-authentication
- [x] Personal access tokens — user-owned credentials for programmatic gateway access

**Downstream protocols**
- [x] OIDC provider
- [x] SAML 2.0 IdP (GHES-compatible)
- [x] Forward-auth gateway (Traefik-compatible)
- [ ] ~~Coordinated single sign-out~~

**Authorization & delegated management**
- [x] App-bound access policy — one optional manual group and calculated rule groups per app, evaluated from live verified-connection, login-method, and avatar facts
- [x] Assigned-application management — any active account can manage an assigned app; assignment does not grant app access
- [x] App-aware group claims — exposed manual allow and every exposed matching rule group for the owning app only

**Dashboard — M2 rewrite**
- [x] React/HeroUI foundation preview with English and Chinese, persisted language preference, and interactive component samples
- [x] Typed OpenAPI client, shared Query cache, route loading feedback and reusable mutation/form integration
- [ ] Login and enrollment UI
- [ ] Admin console and permission-aware application management
- [ ] End-user self-service and app launchpad

**Keys & operations**
- [x] Signing-key lifecycle — rotation, grace windows, sealed at rest
- [x] Signed, provenance-tracked releases
- [ ] KMS/HSM-backed signing
- [ ] Audit-log export to SIEM

On-demand extras (DPoP, PAR, mTLS, SAML assertion encryption, pairwise `sub`) and
non-goals are tracked in `AUDIT.md` / `ARCHITECTURE.md`.

## Quickstart

Deploy as one self-contained artifact: the signed multi-arch OCI image (its
entrypoint runs both the server and the bootstrap subcommands) or a release
binary. Requires Postgres 14+ and a Redis-compatible KV (or the in-process
`memory` driver).

```bash
export PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(openssl rand -base64 32)"  # keep stable; boot fails without it
export PROHIBITORUM_PUBLIC_ORIGIN="https://auth.example.com"
export PROHIBITORUM_DATABASE_URL="postgres://user:pass@db:5432/prohibitorum?sslmode=disable"

IMG=ghcr.io/tundrawork/prohibitorum:0.7.0   # pin a release; or :latest
docker run --rm -e PROHIBITORUM_DATABASE_URL -e PROHIBITORUM_DATA_ENCRYPTION_KEY_V1 "$IMG" signing-key generate
docker run --rm -e PROHIBITORUM_DATABASE_URL -e PROHIBITORUM_DATA_ENCRYPTION_KEY_V1 -e PROHIBITORUM_PUBLIC_ORIGIN "$IMG" enroll-admin   # prints an enroll URL
docker run -p 8080:8080 -e PROHIBITORUM_DATABASE_URL -e PROHIBITORUM_DATA_ENCRYPTION_KEY_V1 -e PROHIBITORUM_PUBLIC_ORIGIN "$IMG"        # auto-migrates; serves :8080
```

Verify the image's keyless signature before running:

```bash
cosign verify "$IMG" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github.com/TundraWork/Prohibitorum/[.]github/workflows/release[.]yml@'
```

Endpoints: [`api.md`](./api.md). Config + hardening: [`CONFIG.md`](./CONFIG.md).
Release artifacts (binaries, SBOMs, signatures): [`TOOLING.md`](./TOOLING.md).

## Development

Prerequisites: [`mise`](https://mise.jdx.dev) and a container runtime (podman or
docker). `mise install` pins the toolchain; the dev DB is a `compose.yaml`
Postgres container.

```bash
mise install
mise run dev:server                # auto-starts DB + builds SPA if changed + serves :8080
mise run dev:enroll-admin -- --new # prints an /enroll/<token> URL
mise run ci                        # the full gate; ci:smoke for the e2e smoke
```

`mise tasks` lists every task (grouped `dev:` / `db` / `ci:` / `prod:`); full
tooling reference in [`TOOLING.md`](./TOOLING.md). The frontend (`dashboard/`,
React + TypeScript + HeroUI + Tailwind v4 + Lingui) uses pnpm and hot-reloads via `mise run dev:dashboard`;
the shipped UI is `pkg/webui/dist` (go:embed), generated by
`mise run prod:build` rather than committed.
`dashboard-old/` is archived reference material and is excluded from builds.

## CLI

`./prohibitorum <command>` (no subcommand runs the server); every DB-backed
command auto-migrates first. Verbs: `enroll-admin`, `signing-key`, `oidc-client`,
`saml-sp`, `forward-auth-app`, `upstream-idp`, `openapi`, `dev-seed` — run
`<command> --help` for flags. The admin HTTP API uses role names
(`oidc-applications`, `saml-applications`, `identity-providers`); CLI verbs
remain protocol-named. The admin UI is temporarily unavailable during the rewrite.

Access-policy commands are scoped beneath the owning app command:
`manager list|assign|remove`, `access set-restricted`,
`group list|select|preview`, and `decision list|set`. Use `--client-id` for
`oidc-client` and `forward-auth-app`, or `--entity-id` for `saml-sp`.
Global group definitions are managed through the admin API during the rewrite. Application
commands identify them by ID because slugs may be shared.

```bash
# The account is active; assignment grants management authority, not app access.
./prohibitorum oidc-client manager assign --client-id docs --username policy-manager
./prohibitorum oidc-client group select --client-id docs --group-id 12 --group-id 18
./prohibitorum oidc-client decision set --client-id docs --group-id 12 --username alice --effect=allow
./prohibitorum oidc-client group preview --client-id docs --group-id 18
./prohibitorum oidc-client access set-restricted --client-id docs --restricted=true
```

## Architecture

Three layers, acyclic imports: an **identity store** (accounts, credentials,
federation links), an **authentication subsystem** that turns WebAuthn,
Password + TOTP, recovery codes, or an upstream provider (OIDC, Steam, VRChat)
into a session, and a **protocol subsystem** that turns that session into an
OIDC OP response or a signed SAML assertion. The `session` package is the
contract between them — RPs see only the resulting claims. `zitadel/oidc` (the
library) is the OIDC OP toolkit; `crewjam/saml` the SAML side.

## Why not …?

- **Keycloak / Authelia / Authentik** — bigger than needed; admin UIs, themes, and plugins we don't want to staff.
- **Ory Hydra** — token issuance only; doesn't own user state.
- **Zitadel (the service)** — full IdP, but Keycloak-level operational complexity.
- **Auth0 / Clerk / Stytch** — not self-hosted.

## Docs

- [`ARCHITECTURE.md`](./ARCHITECTURE.md) — architecture, methods, protocols, threat model, scope.
- [`STATUS.md`](./STATUS.md) — done-per-version + roadmap.
- [`api.md`](./api.md) — the HTTP surface.
- [`CONFIG.md`](./CONFIG.md) — env-var reference + deployment hardening.
- [`TOOLING.md`](./TOOLING.md) — mise, lockfile, dev/CI/prod build.
- [`INTEGRATION.md`](./INTEGRATION.md) — relying-party integration patterns.
- [`DESIGN.md`](./DESIGN.md) / [`PRODUCT.md`](./PRODUCT.md) — dashboard design + product framing.
- [`AUDIT.md`](./AUDIT.md) — per-layer compliance checklist.
