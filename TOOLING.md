# Tooling & dependency architecture

Opinionated, unified tooling for **dev**, **CI**, and **prod-build**. One front
door (`mise`), one task interface (`mise run`), one lockfile, one JS package
manager. This document is the source of truth for *how the project is built and
its dependencies are managed* — distinct from `CONFIG.md` (runtime env vars) and
`ARCHITECTURE.md` (what the software does).

## Principle

> **`mise` is the single front door.** `mise install` provisions every pinned,
> checksummed tool; `mise run <task>` is the one interface humans, CI, and the
> release build all call. No second tool manager, no per-environment drift.

## Current state audit (the chaos this replaces)

| Area | Was | Problem |
|------|-----|---------|
| Go version | mise pins `go = "1.26"` **and** a goenv `.go-version` (`1.26.1`) exists; `go` resolved to the **goenv shim** (1.26.x) while mise has 1.26.4 | Two managers, disagreeing even on patch — ambiguous source of truth |
| Go toolchain | `GOTOOLCHAIN` unset | Go may silently auto-download a different compiler — an unmanaged supply-chain surface |
| JS package mgr | mise pinned `pnpm = "10"`, but the real tool is **npm** (`package-lock.json`, `npm ci`) | Dead pin; intent undocumented (no `packageManager`/`engines`) |
| Tool pinning | no `mise.lock` | Versions resolved live each install → not reproducible; `mise install` hits the GitHub API (rate-limit risk for the prebuilt-postgres pin) |
| CI | none (`.github` absent) | Nothing enforces lockfiles, the toolchain, tests, formatting, or embedded-`dist` freshness (we shipped a stale `dist` + a gofmt miss before this audit) |
| Prod packaging | none — "`go build` a binary + env vars" | No reproducible artifact, no image, no SBOM/provenance, no release pipeline |
| Lint/format | gofmt unenforced; no FE linter | Formatting drift slips in |

## Target architecture

### Tool inventory — pinned in `mise.toml`, locked in `mise.lock`

| Tool | Pin | Backend | Notes |
|------|-----|---------|-------|
| Go | `go = "1.26"` | core | Language floor; mise owns the exact patch. `GOTOOLCHAIN=local` forbids auto-download |
| Node | `node = "24"` | core | Provides **npm** (no Corepack — removed in Node 25+) |
| sqlc | `sqlc = "1.30.0"` | registry | `sqlc generate` → `pkg/db` (config `sqlc.yaml`) |
| goose | `aqua:pressly/goose = "3.27.0"` | aqua | DB migrations (`db/migrations`) |
| Postgres | `github:theseus-rs/postgresql-binaries = "18.3.0"` | github | **Prebuilt** (NOT the source-building default that fails on macOS); checksum + SLSA verified by mise; feeds `mise db:start` |

`mise.lock` (enabled via `[settings] lockfile = true`) pins exact versions +
checksums + provenance for every tool, cross-language. It also pre-resolves
download URLs, so `mise install` becomes hermetic and stops calling the GitHub
API (which removes the prebuilt-postgres rate-limit failure mode). Commit it.
See <https://mise.jdx.dev/dev-tools/mise-lock.html>.

> **Multi-platform:** `mise lock` recorded **all** platforms (linux / macos /
> windows × x64 / arm64, incl. musl — 35 entries), so `mise install --locked` is
> already hermetic on Linux CI runners, not just macOS dev — no per-platform
> follow-up needed.

### Go

- **mise is the single Go source of truth.** No goenv `.go-version` (deleted +
  gitignored).
- `GOTOOLCHAIN=local` (set in mise `[env]`) so Go never auto-downloads a
  compiler behind mise's back — closes the auto-download supply-chain surface
  (<https://go.dev/doc/toolchain>). `go.mod`'s `go 1.26` remains the language floor.
- Reproducible build flags everywhere binaries are produced:
  `CGO_ENABLED=0 go build -tags nodynamic -trimpath -ldflags="-s -w"`.

### Frontend — standardize on npm

The dashboard is a **single package**, not a monorepo, so npm is the right fit;
pnpm's advantages are monorepo-shaped (<https://blog.openreplay.com/switch-npm-pnpm/>).

- `pnpm` pin removed from mise.
- `package.json` gains `"packageManager": "npm@11.13.0"` and `"engines"`
  (`node >=24`, `npm >=11`) to document + enforce intent.
- `npm ci` (frozen lockfile) for installs everywhere; `package-lock.json` is the
  lockfile. mise's node provides npm — no Corepack.

### Dev

- One-stop: `mise install` provisions everything, **including a prebuilt
  Postgres** — no container runtime and no system Postgres install required.
- Dev DB: **`mise db:start`** (self-contained local cluster in `.dev/pgdata`
  from mise's Postgres binaries; `scripts/dev-db.sh`) is the default;
  `podman compose up -d` (`compose.yaml`) is the container alternative.
- Env: `scripts/dev-env.sh` exports the dev `PROHIBITORUM_*` vars + a stable
  `.dev/encryption-key`. (Could later move the static vars into mise `[env]`.)

### Prod — OCI image via GoReleaser + ko

The server is a single Go binary with the SPA embedded via `go:embed`
(`pkg/webui/dist`), so the runtime image is **just the binary**. Build it with
**GoReleaser + [ko](https://goreleaser.com/customization/ko/)** for multi-arch
images, SBOMs, checksums, and signed artifacts from one config — fitting for an
IdP's supply-chain posture.

`.goreleaser.yaml` (committed) shape:
- `before.hooks`: build the SPA (`mise run build:web` → `pkg/webui/dist`)
  so ko's Go build embeds a fresh bundle.
- `builds`: `env: [CGO_ENABLED=0]`, `flags: [-trimpath, -tags=nodynamic]`,
  `ldflags: [-s -w]`, `mod_timestamp: {{.CommitTimestamp}}`, `goos: [linux]`,
  `goarch: [amd64, arm64]`.
- `kos`: `repositories: [ghcr.io/tundrawork/prohibitorum]`, `bare: true`,
  `platforms: [linux/amd64, linux/arm64]`, `sbom: spdx`, base image
  distroless/static (ko default) `:nonroot`.
- Image signing + checksums via cosign (keyless, CI OIDC).

Triggered on tag by `.github/workflows/release.yml`, which runs
`mise run prod:release` after a GHCR login; cosign signing uses CI's OIDC
(`id-token: write`). goreleaser 2.16.0 + cosign 3.1.1 are pinned in mise.
Dry-run locally with `goreleaser release --snapshot --clean`.

### CI — GitHub Actions running `mise run ci`

The unifier: CI runs the **same** tasks humans run, via
[`jdx/mise-action@v3`](https://github.com/jdx/mise-action) (or the
[step-security hardened fork](https://github.com/step-security/mise-action)).
With `mise.lock` present the action auto-applies `--locked` (hermetic, no rate
limits). `.github/workflows/ci.yml` has two jobs:

- **gate** runs `mise run ci` = `mise run ci:go` (`go vet ./...` →
  `go build -tags nodynamic ./...` → `go test ./...`) + `mise run ci:frontend`
  (`npm ci` → `npm test` → `npm run build` → **dist-freshness guard**: fails if
  `pkg/webui/dist` drifts from the committed bundle — would have caught the
  stale dist this audit found). (No `gofmt` gate yet — see deferred.)
- **smoke** runs `mise run ci:smoke` (`db:start` → server → `cmd/smoke`); the smoke
  talks to the DB via pgx, so it needs no extra services.

### Embedded `dist` drift

`pkg/webui/dist` stays committed (so `go run` / `mise run dev:run` work without
node), and CI's dist-freshness guard prevents it going stale. Locally, mise task
`sources`/`outputs` can skip unnecessary SPA rebuilds.

## Implementation status

**Done:**
- One tool source of truth: `mise.lock` (all-platform pins) + `[settings]
  lockfile = true`; `pnpm` pin removed; `packageManager` + `engines` in
  `dashboard/package.json`; `GOTOOLCHAIN = "local"`; goenv `.go-version` deleted
  + gitignored.
- Tasks: `mise run ci` (`ci:go` + `ci:frontend` w/ dist guard) and `mise run ci:smoke`.
- CI: `.github/workflows/ci.yml` (gate + smoke jobs, `jdx/mise-action@v3`).
- Prod: `.goreleaser.yaml` + `mise run prod:release` + `.github/workflows/release.yml`
  (GoReleaser + ko → multi-arch OCI on GHCR + SBOMs + checksums + cosign);
  goreleaser 2.16.0 + cosign 3.1.1 pinned in mise.

**Validated locally:** `mise install` (locked), `GOTOOLCHAIN=local`, `mise run
ci` green, `mise run ci:smoke` green (clean teardown), `goreleaser check` +
`goreleaser build --snapshot` green. The ko image build + cosign signing run
only in CI/release (they need a container runtime + OIDC).

**Deferred (optional):**
- A formatting gate (`gofmt`/strict `vue-tsc`) — needs a one-time
  `gofmt -w ./...` first, since the existing tree isn't gofmt-clean.
- Fold `dev-env.sh` static vars into mise `[env]`.
- Switch to the step-security `mise-action` fork for a hardened CI action.

## Task namespaces

Tasks are namespaced by **context** so a newcomer can tell dev from prod at a
glance (`mise tasks` lists them grouped):

| Namespace | Context | Examples |
|-----------|---------|----------|
| `dev:*`   | local development | `dev:server`, `dev:web`, `dev:run`, `dev:enroll-admin`, `dev:seed` |
| `db:*`    | local Postgres lifecycle (dev + smoke) | `db:start`, `db:stop`, `db:reset`, `db:up`, `db:status` |
| `build:*` | artifacts shared by dev + prod | `build:web`, `build:openapi` |
| `ci:*`    | the checks CI runs | `ci`, `ci:go`, `ci:frontend`, `ci:smoke` |
| `prod:*`  | **production** build + release | `prod:build`, `prod:release` |

## Quick reference

```bash
mise install                       # provision the locked toolchain (Go, Node/npm, sqlc, goose, Postgres, …)
mise run db:start                  # local Postgres dev cluster (or: podman compose up -d)
mise run dev:server                # build SPA + run server on :8080 (auto-migrates)
mise run dev:enroll-admin -- --new # bootstrap an admin
mise run ci                        # the full fast gate (what CI runs)
mise run prod:build                # SPA -> pkg/webui/dist, then compile ./prohibitorum
mise run prod:release              # release: binaries + OCI images (on a git tag; --snapshot to dry-run)
mise lock                          # refresh mise.lock after changing [tools]
```

> Run tasks with `mise run <task>` (not the `mise <task>` shorthand) — the
> shorthand can be shadowed by future mise subcommands.

### `mise run dev:federation` — two-instance OIDC federation harness

Brings up two local instances for manual end-to-end testing: an **upstream** OP
(`https://idp-a.example.test`) and a **downstream** RP
(`https://idp-b.example.test`) that federates to it. Distinct hostnames give
each its own cookie jar (independent sessions); nginx terminates TLS and proxies
each to a loopback http backend (`127.0.0.1:18080` / `:18081`); the two
databases (`prohibitorum_upstream` / `prohibitorum_downstream`) are separate
from your `prohibitorum_dev`.

**Local config (never committed).** Real hostnames + cert paths live in the
gitignored `.dev/dev-federation.env`. First run writes a commented template
(`example.test` placeholders) and exits — fill in your real values (DNS names
pinned to `127.0.0.1`, plus the wildcard cert nginx serves) and re-run.

**Setup:**

1. `mise run db:start` (the dev Postgres).
2. `mise run dev:federation` — first run writes `.dev/dev-federation.env`; edit it.
3. `mise run dev:federation` again — it seeds, wires, generates
   `.dev/nginx/prohibitorum-federation.conf`, and prints a one-time
   `sudo cp … && sudo nginx -t && sudo systemctl reload nginx` command. Run it.
4. Open the printed admin-enrollment URLs (shown in the final banner) to
   register a passkey on each.

**Manual-test paths** (see the spec for detail):

- Federated login (auto_provision): open the downstream → **Upstream** →
  consent on the upstream → `/welcome` confirm → session.
- Invite-gated (invite_only): open the federation-bound invite URL the harness
  prints → **Upstream (invite)** → invite redeemed + identity linked.
- Direct OP test: paste the printed `test-rp` authorize URL → consent → read the
  `code` from the address bar → run the printed token + userinfo `curl`s.

**Every run starts from a clean slate** — both DBs are dropped + recreated and a
fresh admin enroll link is printed for each instance (so enrolled passkeys do
not persist across runs; that is intentional for repeatable manual testing).
