# mise command audit & consolidation — design

- **Date:** 2026-06-24
- **Status:** Approved design, pending implementation plan
- **Scope:** `mise.toml` tasks, the scripts they wrap, `compose.yaml`, and every
  in-repo reference to a task name (CI workflows, `.goreleaser.yaml`,
  `TOOLING.md`). No application code.

## Motivation

`mise run dev:federation` hung forever with no output. Root cause: the dev
Postgres container was down, and the script's first DB step was a host
`psql … 'SELECT 1'` probe with **no connect timeout**. Against a down rootless
container, `127.0.0.1:5432` black-holes the SYN (no RST), and libpq's default
connect timeout is infinite — so the probe (meant to fail fast and say "start
the DB") waited indefinitely, before printing anything.

That bug exposed broader debt the command surface had accumulated:

- **Two DB backends** on the same port — the podman `compose.yaml` container and
  the podman-free mise-binary cluster (`scripts/dev-db.sh` / `db:start`). Two
  mental models, and the duplication is *why* auto-recovery was unsafe (starting
  one when you use the other clashes on `:5432`).
- **21 visible tasks across 5 namespaces** (`dev`/`db`/`build`/`ci`/`prod`) — the
  `db:*` and `build:*` namespaces are cross-cutting (used by dev *and* CI *and*
  the release path), so the dev/CI/prod boundary is blurry.
- **Redundant/fuzzy commands:** `dev:run` vs `dev:server`; `dev:web` (vague
  name); `db:up`/`db:status` overlap with auto-migrate-on-boot.
- **Self-containment gaps:** tasks assume hidden prior setup (a sourced env file,
  a running DB) and fail badly when that assumption breaks (the hang).

## Goals

1. **Self-contained commands** — each task sets up its own preconditions (env,
   deps, DB) or fails fast with a clear, actionable message. No silent hangs, no
   "you forgot to run X first."
2. **Fewer commands** — remove the unnecessary, merge the rest, so a newcomer
   sees a short, legible list.
3. **A clear dev / CI / prod boundary**, with a concise, educationally-named
   command list per category — a good first impression.

## Decisions (locked with the user)

| # | Decision |
|---|----------|
| D1 | **4 namespaces:** `dev:` / `db:` / `ci:` / `prod:`. `db:` stays as an explicit shared-infrastructure namespace (dev + CI both use it); `build:*` is folded away. |
| D2 | **One DB backend: containers via `compose.yaml`**, runnable on Linux *and* macOS through either `podman compose` or `docker compose`. Delete `scripts/dev-db.sh` and the `github:theseus-rs/postgresql-binaries` mise pin. |
| D3 | **Exactly one container-definition file** (`compose.yaml`). The engine is auto-detected (a tiny prefix function); only portable compose flags are used, so no per-engine branching and no second compose file. If engines ever forced divergent handling we'd hardcode `docker compose` — but they don't here. |
| D4 | **Approach B (Consolidated):** arg-driven `db` task; dev tasks auto-start the DB when down; `dev:run` merged into `dev:server`; educational renames. Target ~13 visible tasks. |
| D5 | **No data migration** — the existing compose container + named volume are kept as-is. |

## Section 1 — Command catalog (21 visible → 13 visible)

### `dev:*` — developer commands (run locally) — 8

| Task | From | Change |
|------|------|--------|
| `dev:server` | `dev:server` + `dev:run` | **Merged.** Auto-starts DB if down → installs npm deps only when a build is needed → rebuilds the SPA only if `dashboard/**` changed (mise `sources`/`outputs`) → runs the server on `:8080`. The "no-rebuild / no-node" fast path `dev:run` provided is now automatic, so `dev:run` is deleted. |
| `dev:dashboard` | `dev:web` | **Renamed** ("dashboard" is the product term; "web" was vague). Vite hot-reload against a running backend. |
| `dev:demo` | `dev:demo` | Kept (zero-backend launcher preview with in-memory fixtures). |
| `dev:enroll-admin` | `dev:enroll-admin` | Kept (name already descriptive). Auto-ensures DB. |
| `dev:seed` | `dev:seed` | Kept. Auto-ensures DB. |
| `dev:federation` | `dev:federation` | Kept; DB probe fixed, `psql` routed through the container. |
| `dev:forward-auth` | `dev:forward-auth` | Kept; same fixes. |
| `dev:openapi` | `build:openapi` | **Moved + renamed** — it's dev codegen, not a ship artifact. |

### `db:*` — shared DB infra (dev + CI) — 5 → 1 visible

| Task | Replaces | Change |
|------|----------|--------|
| `db` | `db:start` `db:stop` `db:reset` `db:up` `db:status` | **One arg-driven task:** `mise run db start\|stop\|reset\|migrate\|status` (bare `mise run db` prints usage). Backed by `compose.yaml` via the auto-detected engine. `migrate` = the old `db:up` (goose). |

### `ci:*` — the gate CI runs — 4 → 2 visible

| Task | Change |
|------|--------|
| `ci` | Full fast gate (Go vet/build/test + frontend install/test/typecheck + embedded-`dist` freshness guard). Behavior unchanged; `depends` on the now-hidden `ci:go` + `ci:frontend`. |
| `ci:smoke` | e2e smoke; brings the DB up via `db start` (compose) instead of the deleted mise cluster. |
| ~~`ci:go`, `ci:frontend`~~ | **Hidden** (`hide = true`) — still runnable by name for targeted local runs, off the main list. |

### `prod:*` — the ship path — 2 (unchanged)

| `prod:build` (local release binary) · `prod:release` (signed multi-arch OCI images + SBOMs) |

### Hidden / internal

- `build:web` — kept under its **existing name** but `hide = true`. A
  `sources`/`outputs`-gated dependency task shared by `dev:server`, `prod:build`,
  and the `.goreleaser.yaml` before-hook (which calls `mise run build:web`
  unchanged — zero hook churn). Off the user-facing `mise tasks` list, so it does
  not count against the "4 visible namespaces" goal.

**Net visible:** `dev` 8 + `db` 1 + `ci` 2 + `prod` 2 = **13** (down from 21).

## Section 2 — DB mechanics (one container backend, no more hangs)

**New `scripts/db.sh`** replaces `scripts/dev-db.sh` and backs the `db` task. It
owns every DB operation and stays concise (smaller than the 100-line
`dev-db.sh`, doing more):

- **Engine auto-detect.** A small function picks `podman compose` (project
  default) → falls back to `docker compose`, overridable via an env var
  (`PROHIBITORUM_COMPOSE`). The same `compose.yaml` is used either way (D3).
- **`db start`** → `compose up -d`, then a bounded readiness poll of
  `compose exec -T db pg_isready -U prohibitorum` (portable across both engines,
  avoiding `--wait` flag divergence). Returns only once Postgres is *accepting*,
  not merely "port open."
- **`db ensure`** (internal subcommand) → the bounded `pg_isready` check; if not
  healthy, calls `db start`. This is the auto-start, invoked at the top of
  `dev:server`, `dev:seed`, `dev:enroll-admin`, and both harness scripts.
- **`db stop`** → `compose stop` (keeps the volume).
- **`db reset`** → `compose down -v` then `db start` (wipes the DB — clearly
  labeled destructive in help + output).
- **`db migrate`** → goose up against `PROHIBITORUM_DATABASE_URL`.
- **`db status`** → `compose ps` + goose migration version.
- **No host `psql`/`createdb` needed.** The harness scripts' DB admin (exists /
  create / drop the `prohibitorum_upstream` + `prohibitorum_downstream` DBs)
  routes through a `pg()` helper = `compose exec -T db psql -U prohibitorum …`.
  goose still runs on the host against the published `:5432` (pure-Go binary, no
  libpq).

**Why the hang cannot recur:** nothing ever runs a host `psql` with libpq's
infinite connect timeout against a maybe-down port. Reachability is the bounded
container `pg_isready` check, and `db ensure` brings the DB up rather than
blocking.

**CI implication:** `ci:smoke` switches from the deleted mise cluster to
`db start` (compose). GitHub `ubuntu-latest` ships both `podman` and Docker, and
`podman compose`'s provider can be flaky on the runner, so CI pins the engine
explicitly — `ci.yml` sets `PROHIBITORUM_COMPOSE="docker compose"` for the smoke
job (local stays auto-detect, podman-first). The smoke runs against a throwaway
`prohibitorum_smoke` database that it drops + recreates each run (it bootstraps an
admin and registers OIDC/SAML clients, so it needs a clean slate — and this keeps
it from ever polluting or colliding with `prohibitorum_dev`).

## Section 3 — Self-containment & env

- Keep **one** dev env helper, `scripts/dev-env.sh` (the encryption-key
  generation into `.dev/encryption-key` is dynamic, so it cannot be pure mise
  `[env]`). It is sourced *internally* by the dev tasks — the user never sources
  anything manually.
- `sources`/`outputs` on the hidden `build:web` task let `dev:server` skip the
  SPA rebuild **and npm entirely** when `dashboard/**` is unchanged, preserving
  the old `dev:run` "no-node, instant" speed. The `npm ci` install moves *inside*
  the build task, gated by the same freshness check, so it only runs when a build
  actually runs.
- Result: `mise run dev:server` from a cold checkout (DB down, no `node_modules`,
  committed `dist` fresh) works end-to-end with no hidden prerequisite steps.

## Section 4 — Reference updates & breaking changes

In-repo references updated so nothing dangles:

- **`.github/workflows/ci.yml`** — `ci:smoke` no longer `depends=["db:start"]`;
  it runs `db start` (compose). The smoke job sets
  `PROHIBITORUM_COMPOSE="docker compose"` so the engine is deterministic on the
  runner (Docker present on `ubuntu-latest`; podman's compose provider can be
  flaky there).
- **`.goreleaser.yaml`** — before-hook keeps calling `mise run build:web`
  (now hidden); no change needed.
- **`mise.toml`** — remove the `github:theseus-rs/postgresql-binaries` pin;
  refresh `mise.lock` (`mise lock`); rewrite the `[tasks.*]` table per Section 1.
- **`TOOLING.md`** — rewrite the dev-DB section (container-only), the
  task-namespace table, and the quick reference.
- **`compose.yaml`** — header updated: this is now *the* dev DB, not "an
  alternative."
- **Delete** `scripts/dev-db.sh`; **add** `scripts/db.sh`.
- **`scripts/dev-federation.sh`, `scripts/dev-forward-auth.sh`** — DB probe →
  `db ensure`; host `psql`/`createdb` → container `pg()` helper.

**Breaking changes (muscle memory; documented in `TOOLING.md`):**
`dev:run`→`dev:server` · `dev:web`→`dev:dashboard` ·
`db:start`/`db:stop`/`db:reset`→`db start`/`db stop`/`db reset` ·
`db:up`→`db migrate` · `db:status`→`db status` ·
`build:openapi`→`dev:openapi` · `ci:go`/`ci:frontend` hidden.

**No data migration** — the existing compose container + named volume are kept.

## Section 5 — Verification (done-gate)

- `mise tasks` shows exactly the 13 visible tasks in 4 clean groups
  (`dev`/`db`/`ci`/`prod`).
- `mise run ci` green.
- `mise run ci:smoke` green using the compose DB.
- `mise run dev:server` from a state of **DB down + no `node_modules`** comes up
  on `:8080` (auto-DB, deps installed, SPA built).
- `mise run db start|stop|reset|migrate|status` each behave; bare `mise run db`
  prints usage.
- **Re-run the exact `mise run dev:federation` scenario that hung** (DB down) and
  confirm it auto-starts / fails fast instead of hanging.
- Engine override env var is respected; exactly one `compose.yaml`;
  `scripts/db.sh` is concise; `scripts/dev-db.sh` is gone; `grep` finds no
  dangling references to removed task names.

## Non-goals / out of scope

- No application/runtime code changes.
- No change to the prod release artifacts or the `prod:release` pipeline (only
  the `build:web` hook reference, which is unchanged).
- Not folding `dev-env.sh` static vars into mise `[env]` (would leak dev defaults
  into the prod task context; the single sourced helper is the cleaner
  self-containment).
- Not reorganizing into single-dispatcher verbs (rejected Approach C).
