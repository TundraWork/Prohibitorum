# Prohibitorum

> *Index Librorum Prohibitorum*

[![ci](https://github.com/TundraWork/Prohibitorum/actions/workflows/ci.yml/badge.svg)](https://github.com/TundraWork/Prohibitorum/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/TundraWork/Prohibitorum)](https://github.com/TundraWork/Prohibitorum/releases)
[![license](https://img.shields.io/badge/license-Apache--2.0-blue)](./LICENSE)

**A self-hosted identity provider for a small organization.** One binary holds
the account directory, sign-in, single sign-on for your applications, and the
web dashboard that members and admins use. It needs PostgreSQL, plus Redis or
the built-in in-memory store.

## Features

- **Sign-in** — passkeys first, with password + authenticator app and recovery
  codes as a fallback. Members can also sign in through upstream OpenID Connect
  and Steam providers. A VRChat profile verifies identity for invitations and
  linked accounts, and an admin can also turn it on for sign-in.
- **Single sign-on** — an OpenID Connect provider, a SAML 2.0 identity provider
  (GitHub Enterprise Server compatible), and Traefik forward-auth for
  applications without a login of their own. Personal access tokens give
  scripts and tools access as their owner.
- **Access control** — an application is open to every member or restricted to
  groups: manual allow and deny lists, or rules evaluated from live account
  facts such as verified connections and sign-in method. Groups can be sent to
  applications as claims, and a member can be assigned to manage specific
  applications without becoming an admin.
- **Dashboard** — English and Chinese, light and dark, on desktop and mobile.
  Members get an application launcher, their profile and avatar, sign-in
  methods, sessions, connected applications, and device pairing. Admins manage
  users, groups, invitations, identity providers, applications, signing keys,
  the sign-in page, and the audit log.
- **Operations** — signing keys rotate with a grace window and are encrypted at
  rest. Releases are signed with cosign and ship with SBOMs and build
  provenance.

Account recovery uses recovery codes or an admin-issued enrollment link. Email
is optional and only verifies addresses; it is never a way back into an
account.

## Quick start

Run the signed multi-arch image from GHCR, or a binary from the
[releases page](https://github.com/TundraWork/Prohibitorum/releases).
PostgreSQL 14 or newer is required.

```bash
export PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(openssl rand -base64 32)"  # keep it stable; boot fails without it
export PROHIBITORUM_PUBLIC_ORIGIN="https://auth.example.com"
export PROHIBITORUM_DATABASE_URL="postgres://user:pass@db:5432/prohibitorum?sslmode=disable"

IMG=ghcr.io/tundrawork/prohibitorum:1.0.0   # pin a release, or use :latest
docker run --rm -e PROHIBITORUM_DATABASE_URL -e PROHIBITORUM_DATA_ENCRYPTION_KEY_V1 "$IMG" signing-key generate
docker run --rm -e PROHIBITORUM_DATABASE_URL -e PROHIBITORUM_DATA_ENCRYPTION_KEY_V1 -e PROHIBITORUM_PUBLIC_ORIGIN "$IMG" enroll-admin
docker run -p 8080:8080 -e PROHIBITORUM_DATABASE_URL -e PROHIBITORUM_DATA_ENCRYPTION_KEY_V1 -e PROHIBITORUM_PUBLIC_ORIGIN "$IMG"
```

The server migrates the database on start and listens on `:8080`. Open the URL
that `enroll-admin` prints to register the first admin's passkey; everything
else can be set up from the dashboard. For production, read
[`docs/config.md`](./docs/config.md): it covers every setting, Redis, TLS, and
hardening.

To check the image signature before running it:

```bash
cosign verify "$IMG" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github.com/TundraWork/Prohibitorum/[.]github/workflows/release[.]yml@'
```

The same binary is also a command-line tool for scripting setup:
`oidc-client`, `saml-sp`, `forward-auth-app`, `upstream-idp`, `signing-key`,
and `enroll-admin`. Run `prohibitorum <command> --help` for details.

## Why Prohibitorum

- **Keycloak / Authentik / Authelia** — more than a small organization needs:
  larger admin surfaces, themes, and plugin systems to maintain.
- **Ory Hydra** — issues tokens but does not own user accounts.
- **Zitadel** — a full identity provider with Keycloak-level operational
  complexity.
- **Auth0 / Clerk / Stytch** — not self-hosted.

Not yet available: single sign-out across applications, KMS/HSM-backed signing,
and audit-log export. See the roadmap in [`docs/status.md`](./docs/status.md).

## Documentation

- [`docs/config.md`](./docs/config.md) — settings and deployment hardening.
- [`docs/integration.md`](./docs/integration.md) — connecting applications.
- [`docs/forward-auth.md`](./docs/forward-auth.md) — protecting applications with Traefik.
- [`docs/api.md`](./docs/api.md) — the HTTP API.
- [`docs/architecture.md`](./docs/architecture.md) — design, protocols, and threat model.
- [`docs/audit.md`](./docs/audit.md) — standards compliance checklist.
- [`docs/status.md`](./docs/status.md) — current state, known limits, and the roadmap.
- [`docs/tooling.md`](./docs/tooling.md) — development, CI, and release builds.

## Development

You need [`mise`](https://mise.jdx.dev) and podman or docker. The backend is Go;
the dashboard in `dashboard/` is React and TypeScript, built into the binary.

```bash
mise install                         # toolchain pinned in mise.lock
mise run dev:server                  # starts a dev PostgreSQL and serves http://localhost:8080
mise run dev:enroll-admin -- --new   # prints an enrollment URL for a dev admin
mise run dev:dashboard               # dashboard with hot reload
mise run ci                          # the CI checks; `mise run ci:smoke` runs the end-to-end test
```

`mise tasks` lists every task. See [`docs/tooling.md`](./docs/tooling.md) for the rest.

## License

Prohibitorum is licensed under the [Apache License 2.0](./LICENSE).
[`NOTICE`](./NOTICE) carries the project's copyright and the notices of the
third-party code it includes.
