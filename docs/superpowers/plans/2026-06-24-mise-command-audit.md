# mise Command Audit & Consolidation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Consolidate the mise command surface from 21 visible tasks to 13 across a clean dev/db/ci/prod boundary, standardize on a single container DB backend (compose, podman-or-docker), and make every task self-contained — no more silent hangs.

**Architecture:** A new `scripts/db.sh` owns all Postgres lifecycle via `compose.yaml` (engine auto-detected; sourceable so the harness scripts reuse its `pg()` container helper). `mise.toml` is rewritten to the new catalog with hidden internal tasks (`build:web`, `ci:go`, `ci:frontend`) and an arg-driven `db` task. The podman-free mise Postgres cluster (`dev-db.sh` + the `theseus-rs` pin) is removed. All in-repo references (CI, goreleaser hook, docs) are synced.

**Tech Stack:** mise tasks, bash, `compose.yaml` (Postgres 18), goose, GitHub Actions, GoReleaser.

**Spec:** `docs/superpowers/specs/2026-06-24-mise-command-audit-design.md`

---

## File Structure

| File | Responsibility | Change |
|------|----------------|--------|
| `scripts/db.sh` | DB lifecycle (`start/stop/reset/migrate/status/ensure`) + sourceable helpers (`compose_cmd`, `pg`, `db_ensure`) | **Create** |
| `scripts/dev-db.sh` | (old podman-free mise cluster) | **Delete** |
| `mise.toml` | Tool pins + task catalog | **Rewrite** `[tools]` (drop Postgres) + `[tasks.*]` |
| `mise.lock` | Locked tool versions | **Regenerate** via `mise lock` |
| `scripts/dev-federation.sh` | Two-instance OIDC harness | **Modify** DB section (source db.sh, `db_ensure`, container `pg()`) |
| `scripts/dev-forward-auth.sh` | Forward-auth harness | **Modify** DB section (same) |
| `.github/workflows/ci.yml` | CI gate + smoke | **Modify** smoke job (engine pin) |
| `TOOLING.md` | Tooling docs | **Modify** dev-DB section, namespace table, quick ref |
| `compose.yaml` | Container DB definition | **Modify** header comment |

**mise behavior verified during planning (relied on by this plan):**
- Single-line `run = "scripts/db.sh"` + `mise run db start` → executes `scripts/db.sh start` (trailing positional appended).
- Multi-line `run` ending in `… "$@"` + `mise run dev:enroll-admin -- --new` → forwards `--new` as `$@`.
- `hide = true`, `sources`/`outputs`, and `depends` are supported (mise 2026.5.15).

---

### Task 0: Create `scripts/db.sh` (container DB backend + sourceable helpers)

**Goal:** One concise script that brings the dev Postgres up/down via `compose.yaml` on either podman or docker, exposes a bounded readiness check (killing the infinite-hang failure mode), and can be sourced by the harness scripts to run `psql`/`createdb` inside the container.

**Files:**
- Create: `scripts/db.sh`

**Acceptance Criteria:**
- [ ] `scripts/db.sh start` brings the compose DB up and returns only once `pg_isready` succeeds (≤30s) or fails with a clear error.
- [ ] `scripts/db.sh ensure` is a no-op when the DB is already accepting; starts it otherwise.
- [ ] `scripts/db.sh status` shows container state + goose migration version.
- [ ] `scripts/db.sh stop` stops without wiping; `scripts/db.sh reset` wipes (`down -v`) then restarts.
- [ ] Sourcing the file (`. scripts/db.sh`) defines `compose_cmd`, `pg`, `db_ensure` **without** running a subcommand.
- [ ] Engine is `PROHIBITORUM_COMPOSE` if set, else `podman compose`, else `docker compose`; `--help`/usage works even with no engine installed.

**Verify:**
```
scripts/db.sh start    # → "waiting for postgres… — ready"
scripts/db.sh ensure   # → returns immediately (already ready), no output churn
scripts/db.sh status   # → compose ps table + goose status
scripts/db.sh stop
```

**Steps:**

- [ ] **Step 1: Write `scripts/db.sh`**

```bash
#!/usr/bin/env bash
# Local Postgres for development + the smoke, via compose.yaml — the single
# container definition. Works on Linux and macOS with podman OR docker.
#
#   scripts/db.sh start     # bring the DB up; waits until it accepts connections
#   scripts/db.sh stop      # stop, keep the data volume
#   scripts/db.sh reset     # DESTROY the data volume + recreate
#   scripts/db.sh migrate   # apply goose migrations
#   scripts/db.sh status    # container + migration status
#   scripts/db.sh ensure    # start only if not already accepting (dev tasks call this)
#
# Sourcing this file (`. scripts/db.sh`) defines the helpers (compose_cmd, pg,
# db_ensure) WITHOUT running a subcommand — the dev:federation / dev:forward-auth
# harnesses source it to reuse the engine detection + in-container psql/createdb.
#
# Engine: $PROHIBITORUM_COMPOSE if set, else `podman compose`, else `docker
# compose`. The SAME compose.yaml is used either way (one definition file).

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

_CE=""
compose_cmd() {
	if [ -z "$_CE" ]; then
		if [ -n "${PROHIBITORUM_COMPOSE:-}" ]; then
			_CE="$PROHIBITORUM_COMPOSE"
		elif command -v podman >/dev/null 2>&1; then
			_CE="podman compose"
		elif command -v docker >/dev/null 2>&1; then
			_CE="docker compose"
		else
			echo "error: neither podman nor docker found; set PROHIBITORUM_COMPOSE" >&2
			return 1
		fi
	fi
	printf '%s' "$_CE"
}

# Run a command inside the db container — psql/createdb/pg_isready live there, so
# no host Postgres client is required. -T disables TTY allocation (scriptable).
pg() {
	# shellcheck disable=SC2046,SC2086 -- compose_cmd is "podman compose" (2 words)
	( cd "$ROOT" && $(compose_cmd) exec -T -e PGPASSWORD=prohibitorum db "$@" )
}

# True when Postgres is up AND accepting. Bounded by compose exec — never blocks
# on a black-holed port the way a host `psql` connect would.
db_ready() { pg pg_isready -U prohibitorum -d prohibitorum_dev >/dev/null 2>&1; }

db_start() {
	# shellcheck disable=SC2046,SC2086
	( cd "$ROOT" && $(compose_cmd) up -d )
	printf 'waiting for postgres'
	local i
	for i in $(seq 1 30); do
		if db_ready; then printf ' — ready\n'; return 0; fi
		printf '.'; sleep 1
	done
	printf ' — TIMEOUT\n' >&2
	echo "error: postgres did not become ready in 30s; check '$(compose_cmd) logs db'" >&2
	return 1
}

db_ensure() {
	if db_ready; then return 0; fi
	echo "dev Postgres not ready — starting it (compose)"
	db_start
}

db_stop() {
	# shellcheck disable=SC2046,SC2086
	( cd "$ROOT" && $(compose_cmd) stop )
}

db_reset() {
	echo "WIPING the dev database (compose down -v)…"
	# shellcheck disable=SC2046,SC2086
	( cd "$ROOT" && $(compose_cmd) down -v )
	db_start
}

db_migrate() {
	# shellcheck disable=SC1091
	. "$ROOT/scripts/dev-env.sh"
	goose -dir "$ROOT/db/migrations" postgres "$PROHIBITORUM_DATABASE_URL" up
}

db_status() {
	# shellcheck disable=SC2046,SC2086
	( cd "$ROOT" && $(compose_cmd) ps )
	# shellcheck disable=SC1091
	. "$ROOT/scripts/dev-env.sh"
	goose -dir "$ROOT/db/migrations" postgres "$PROHIBITORUM_DATABASE_URL" status || true
}

_usage() { sed -n '2,16p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; }

_main() {
	case "${1:-}" in
	start)   db_start ;;
	stop)    db_stop ;;
	reset)   db_reset ;;
	migrate) db_migrate ;;
	status)  db_status ;;
	ensure)  db_ensure ;;
	"" | -h | --help) _usage ;;
	*) echo "unknown subcommand: $1" >&2; _usage >&2; exit 2 ;;
	esac
}

# Dispatch only when executed; sourcing just defines the helpers above.
if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
	set -euo pipefail
	_main "$@"
fi
```

- [ ] **Step 2: Make it executable**

Run: `chmod +x scripts/db.sh`

- [ ] **Step 3: Verify lifecycle against the real compose DB**

Run:
```
scripts/db.sh start
scripts/db.sh ensure
scripts/db.sh status
```
Expected: `start` prints `waiting for postgres… — ready`; `ensure` returns immediately (already ready); `status` prints the `compose ps` table for service `db` plus a goose status listing. (Leave the DB running for later tasks; do **not** run `reset` here — it would wipe your `prohibitorum_*` databases.)

- [ ] **Step 4: Verify the sourced-helper path (no subcommand runs on source)**

Run: `bash -c '. scripts/db.sh && type pg db_ensure compose_cmd >/dev/null && echo OK'`
Expected: `OK` (functions defined, no lifecycle action taken).

- [ ] **Step 5: Verify usage works with no engine**

Run: `PROHIBITORUM_COMPOSE= PATH=/usr/bin scripts/db.sh --help 2>&1 | head -3` (or simply `scripts/db.sh --help`)
Expected: the usage block prints (the `--help` path must not require an engine).

- [ ] **Step 6: Commit**

```bash
git add scripts/db.sh
git commit -m "feat(tooling): add scripts/db.sh — single container DB backend + sourceable helpers"
```

---

### Task 1: Rewrite `mise.toml` catalog, drop the Postgres pin, delete `dev-db.sh`

**Goal:** Switch the whole command surface to the new 13-visible catalog (Section 1 of the spec): merged `dev:server`, renamed `dev:dashboard`/`dev:openapi`, arg-driven `db`, hidden `build:web`/`ci:go`/`ci:frontend`, DB-ensure in dev tasks. Remove the now-unused mise Postgres cluster.

**Files:**
- Modify: `mise.toml` (full `[tools]` + `[tasks.*]` rewrite)
- Regenerate: `mise.lock` (`mise lock`)
- Delete: `scripts/dev-db.sh`

**Acceptance Criteria:**
- [ ] `mise tasks` lists exactly 13 visible tasks: `dev:server dev:dashboard dev:demo dev:enroll-admin dev:seed dev:federation dev:forward-auth dev:openapi db ci ci:smoke prod:build prod:release`.
- [ ] `build:web`, `ci:go`, `ci:frontend` are hidden but still runnable by name.
- [ ] `mise run db start|stop|status|migrate` work; bare `mise run db` prints usage.
- [ ] `mise run dev:openapi` regenerates `openapi.yaml`.
- [ ] `mise.lock` no longer contains a `postgresql-binaries` entry; `scripts/dev-db.sh` is gone.

**Verify:**
```
mise tasks                       # 13 visible rows, grouped dev/db/ci/prod
mise run db status               # works (Task 0 helper)
mise run dev:openapi && git checkout -- openapi.yaml   # regenerates cleanly
grep -rn "postgresql-binaries" mise.lock || echo "pin removed OK"
```

**Steps:**

- [ ] **Step 1: Replace the entire contents of `mise.toml` with:**

```toml
[tools]
go = "1.26"
node = "24"
sqlc = "1.30.0"
"aqua:pressly/goose" = "3.27.0"
# Release tooling (used by `mise run prod:release` and the release workflow):
# GoReleaser drives the build + multi-arch OCI images via its built-in ko
# integration + SBOMs; cosign signs the artifacts. See TOOLING.md.
"aqua:goreleaser/goreleaser" = "2.16.0"
"aqua:sigstore/cosign" = "3.1.1"

[settings]
# Pin exact tool versions + checksums + provenance in mise.lock (committed), so
# `mise install` is reproducible and hermetic — no live GitHub/registry API
# calls at install time.
lockfile = true

[env]
# mise is the single Go source of truth (no goenv). Forbid Go's transparent
# toolchain auto-download so it can't fetch a different compiler behind mise's
# back — `go.mod`'s `go 1.26` stays the language floor. See TOOLING.md.
GOTOOLCHAIN = "local"

# =============================================================================
# Tasks — namespaced by CONTEXT so dev, CI, and prod can't be mixed up.
# Run with `mise run <task>`, e.g. `mise run dev:server`. (`mise tasks` lists
# the 13 visible tasks grouped by namespace.) See TOOLING.md for the full picture.
#
#   dev:*  local development — run the app, hot reload, seed, enroll, harnesses
#   db     local Postgres lifecycle (dev + the smoke) — `mise run db <subcommand>`
#   ci:*   the checks GitHub Actions runs (also runnable locally)
#   prod:* PRODUCTION build + release (release binary, OCI images) — the ship path
#
# Hidden internal tasks (off `mise tasks`, still runnable): build:web (SPA bundle,
# sources/outputs-gated), ci:go, ci:frontend.
# =============================================================================

# ---- dev: local development -------------------------------------------------

[tasks."dev:server"]
description = "DEV: start the dev DB if needed, rebuild the SPA only if it changed, and run the server at http://localhost:8080 (dev env: prohibitorum_dev DB + a stable .dev/encryption-key; auto-migrates on boot). Bootstrap an admin with `mise run dev:enroll-admin -- --new`."
depends = ["build:web"]
run = """
set -e
scripts/db.sh ensure
. ./scripts/dev-env.sh
exec go run ./cmd/prohibitorum
"""

[tasks."dev:dashboard"]
description = "DEV: dashboard dev server with hot reload (Vite) against a running backend (`mise run dev:server`). Installs npm deps on first run."
run = """
set -e
cd dashboard
[ -d node_modules ] || npm ci
exec npm run dev
"""

[tasks."dev:demo"]
description = "DEV: preview the end-user launcher (LauncherLayout + MyAppsView) with NO backend — the dashboard dev server runs against in-memory fixtures from dashboard/vite.demo.config.ts. No DB, no Go server; open http://localhost:5173/. Installs npm deps on first run."
run = """
set -e
cd dashboard
[ -d node_modules ] || npm ci
exec npm run dev -- --config vite.demo.config.ts
"""

[tasks."dev:enroll-admin"]
description = "DEV: issue an admin passkey-enrollment URL against the dev DB; open the printed http://localhost:8080/enroll/<token>. Errors if an admin exists; pass flags after --, e.g. `mise run dev:enroll-admin -- --new` or `... -- --reset --username alice`."
run = """
set -e
scripts/db.sh ensure
. ./scripts/dev-env.sh
exec go run ./cmd/prohibitorum enroll-admin "$@"
"""

[tasks."dev:seed"]
description = "DEV: seed the dev database with example providers/accounts/invitations so the dashboard's data-driven views render. Idempotent; refuses non-localhost."
run = """
set -e
scripts/db.sh ensure
. ./scripts/dev-env.sh
exec go run ./cmd/prohibitorum dev-seed
"""

[tasks."dev:federation"]
description = "DEV: bring up two prohibitorum instances (upstream + downstream IdP) wired for OIDC federation behind nginx TLS, for manual end-to-end testing. Reads local hostnames/cert from .dev/dev-federation.env (a template is written on first run). Idempotent by default (existing DBs reused); pass `-- --fresh` to wipe + reseed both. Starts the dev DB automatically."
run = "exec scripts/dev-federation.sh \"$@\""

[tasks."dev:forward-auth"]
description = "DEV: bring up a protected whoami app behind a Traefik TLS front wired to Prohibitorum forward-auth, for manual end-to-end testing. Reads local hostnames/cert from .dev/dev-forward-auth.env (a template is written on first run). Idempotent by default; pass `-- --fresh` to wipe + reseed. Starts the dev DB automatically."
run = "exec scripts/dev-forward-auth.sh \"$@\""

[tasks."dev:openapi"]
description = "DEV: regenerate openapi.yaml from the humacli."
run = "go run ./cmd/prohibitorum openapi > openapi.yaml"

# ---- db: local Postgres lifecycle (dev + the smoke) -------------------------

[tasks.db]
description = "DB: local Postgres lifecycle via compose.yaml — `mise run db start|stop|reset|migrate|status` (bare prints usage). Linux + macOS; podman or docker (override with PROHIBITORUM_COMPOSE). `reset` WIPES the database."
run = "scripts/db.sh"

# ---- ci: the checks GitHub Actions runs (also runnable locally) -------------

[tasks.ci]
description = "CI: the full fast gate (ci:go + ci:frontend) — the same task the CI `gate` job runs."
depends = ["ci:go", "ci:frontend"]

[tasks."ci:go"]
hide = true
description = "CI (internal): Go vet + build (-tags nodynamic) + full test suite."
run = """
set -e
go vet ./...
go build -tags nodynamic ./...
go test ./...
"""

[tasks."ci:frontend"]
hide = true
description = "CI (internal): dashboard install + unit tests + typecheck (via build), then assert the committed pkg/webui/dist matches a fresh build (no stale-dist drift)."
run = """
set -e
cd dashboard
npm ci
npm test
npm run build
cd ..
if ! git diff --quiet -- pkg/webui/dist; then
  echo 'ERROR: pkg/webui/dist is stale — run `mise run prod:build` and commit the rebuilt bundle.' >&2
  git --no-pager diff --stat -- pkg/webui/dist >&2
  exit 1
fi
"""

[tasks."ci:smoke"]
description = "CI: end-to-end smoke — start the compose Postgres + the server and run cmd/smoke against it (expects exit 0). The CI `smoke` job; also runnable locally."
run = """
set -e
scripts/db.sh start
export PROHIBITORUM_DATABASE_URL="postgres://prohibitorum:prohibitorum@localhost:5432/prohibitorum_dev?sslmode=disable"
export PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(openssl rand -base64 32)"
export PROHIBITORUM_PUBLIC_ORIGIN="http://localhost:8080"
export PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK="true"
# Build once and run the binary directly so the trap kills the real server
# (backgrounding `go run` leaves its compiled child orphaned, holding :8080).
BIN="$(mktemp -d)/prohibitorum"
go build -tags nodynamic -o "$BIN" ./cmd/prohibitorum
"$BIN" & SERVER_PID=$!
trap 'kill "$SERVER_PID" 2>/dev/null || true; rm -rf "$(dirname "$BIN")"' EXIT
for _ in $(seq 1 60); do
  curl -sf http://localhost:8080/.well-known/openid-configuration >/dev/null 2>&1 && break
  sleep 1
done
go run ./cmd/smoke --base-url http://localhost:8080
"""

# ---- build: SPA bundle (hidden — shared by dev:server, prod:build, release) --

[tasks."build:web"]
hide = true
description = "BUILD (internal): compile the dashboard SPA into pkg/webui/dist (the go:embed bundle). sources/outputs-gated, so it's skipped when dashboard/** is unchanged."
sources = [
  "dashboard/src/**/*",
  "dashboard/public/**/*",
  "dashboard/index.html",
  "dashboard/package.json",
  "dashboard/package-lock.json",
  "dashboard/vite.config.ts",
  "dashboard/vite.demo.config.ts",
  "dashboard/tsconfig.json",
  "dashboard/tsconfig.app.json",
  "dashboard/tsconfig.node.json",
  "dashboard/components.json",
]
outputs = ["pkg/webui/dist/**/*"]
run = """
set -e
cd dashboard
[ -d node_modules ] || npm ci
npm run build
"""

# ---- prod: production build + release (the ship path) -----------------------

[tasks."prod:build"]
description = "PROD: build the SPA + compile the ./prohibitorum release binary (-tags nodynamic, embeds the SPA), matching the released artifact. Run it with `. ./scripts/dev-env.sh && ./prohibitorum` or set PROHIBITORUM_* yourself."
depends = ["build:web"]
run = """
set -e
go build -tags nodynamic -o prohibitorum ./cmd/prohibitorum
echo "built ./prohibitorum"
"""

[tasks."prod:release"]
description = "PROD: build + publish a release — binaries + multi-arch OCI images (GoReleaser + ko) + SBOMs + checksums + cosign signatures. Runs on a git tag in the release workflow; dry-run locally with `goreleaser release --snapshot --clean`."
run = "goreleaser release --clean"
```

> **Note on `dashboard/` source globs:** the `sources` list above assumes the standard Vite layout. Before committing, run `ls dashboard/*.ts dashboard/*.json dashboard/tsconfig*.json` and adjust the globs so every config that affects the build is listed (a missing entry only means an over-eager skip — fix if you spot drift; the `ci:frontend` dist-freshness guard is the backstop).

- [ ] **Step 2: Delete the obsolete cluster script**

Run: `git rm scripts/dev-db.sh`

- [ ] **Step 3: Refresh the lockfile (drops the Postgres pin)**

Run: `mise lock`
Then confirm: `grep -c "postgresql-binaries" mise.lock` → expected `0`.

- [ ] **Step 4: Verify the catalog**

Run: `mise tasks`
Expected: 13 visible tasks in the groups above; `build:web`, `ci:go`, `ci:frontend` absent from the list.

Run: `mise run ci:go --help >/dev/null 2>&1; mise tasks --hidden 2>/dev/null | grep -E 'build:web|ci:go|ci:frontend'` (or `mise tasks -a`)
Expected: the three hidden tasks appear in the all/hidden listing.

- [ ] **Step 5: Verify `db` arg dispatch + dev:openapi**

Run: `mise run db` → expected: usage block. `mise run db status` → expected: compose ps + goose status.
Run: `mise run dev:openapi && git diff --stat openapi.yaml` → expected: regenerates with no diff (or an expected diff); then `git checkout -- openapi.yaml`.

- [ ] **Step 6: Commit**

```bash
git add mise.toml mise.lock
git commit -m "refactor(tooling): consolidate mise catalog to 13 tasks; container-only DB; drop dev-db.sh + Postgres pin"
```

---

### Task 2: Route the harness scripts through `db.sh` (kill the hang)

**Goal:** `dev:federation` and `dev:forward-auth` no longer probe the DB with a host `psql` (the infinite-hang bug). They source `db.sh`, call `db_ensure` to auto-start the container, and run all `psql`/`createdb` admin through the in-container `pg()` helper — so no host Postgres client is needed.

**Files:**
- Modify: `scripts/dev-federation.sh` (DB section ~lines 85–98 + the `db_is_seeded`/`db_has_admin`/`recreate_db` helpers ~lines 104–126)
- Modify: `scripts/dev-forward-auth.sh` (DB section ~lines 105–132)

**Acceptance Criteria:**
- [ ] Neither script contains a bare `psql -d postgres -tAc 'SELECT 1'` reachability probe.
- [ ] Both source `scripts/db.sh` and call `db_ensure` before touching the DB.
- [ ] All `psql`/`createdb` calls go through `pg …` (container).
- [ ] With the DB **stopped**, `mise run dev:federation` auto-starts it instead of hanging.

**Verify:**
```
grep -n "psql -d postgres -tAc 'SELECT 1'" scripts/dev-federation.sh scripts/dev-forward-auth.sh   # → no matches
bash -n scripts/dev-federation.sh && bash -n scripts/dev-forward-auth.sh   # syntax OK
scripts/db.sh stop && mise run dev:federation   # auto-starts DB, proceeds (Ctrl-C to stop)
```

**Steps:**

- [ ] **Step 1: `dev-federation.sh` — replace the encryption-key + DB-probe block**

Find (around lines 85–98):
```bash
# --- 2. shared encryption key (reuse dev-env.sh's) -------------------------
[ -f .dev/encryption-key ] || openssl rand -base64 32 >.dev/encryption-key
export PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(cat .dev/encryption-key)"

# PGPASSWORD matches the dev credential (compose.yaml / dev-env.sh). The
# podman-free dev-db.sh cluster uses trust auth and ignores it; the compose
# Postgres requires it — setting it works for both.
export PGHOST=localhost PGPORT=5432 PGUSER=prohibitorum PGPASSWORD=prohibitorum
UP_DB="postgres://prohibitorum:prohibitorum@localhost:5432/prohibitorum_upstream?sslmode=disable"
DOWN_DB="postgres://prohibitorum:prohibitorum@localhost:5432/prohibitorum_downstream?sslmode=disable"

if ! psql -d postgres -tAc 'SELECT 1' >/dev/null 2>&1; then
	echo "ERROR: Postgres not reachable on localhost:5432 — run 'mise run db:start'." >&2
	exit 1
fi
```
Replace with:
```bash
# --- 2. shared encryption key (reuse dev-env.sh's) -------------------------
[ -f .dev/encryption-key ] || openssl rand -base64 32 >.dev/encryption-key
export PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(cat .dev/encryption-key)"

# --- DB: ensure the dev Postgres (container) is up. psql/createdb run INSIDE
# the container via the pg() helper from db.sh — no host Postgres client needed.
# shellcheck disable=SC1091
. "$ROOT/scripts/db.sh"
db_ensure
UP_DB="postgres://prohibitorum:prohibitorum@localhost:5432/prohibitorum_upstream?sslmode=disable"
DOWN_DB="postgres://prohibitorum:prohibitorum@localhost:5432/prohibitorum_downstream?sslmode=disable"
```

- [ ] **Step 2: `dev-federation.sh` — route the DB helpers through `pg`**

Find (around lines 104–126):
```bash
db_is_seeded() {
	local name="$1" reg
	psql -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = '$name'" 2>/dev/null | grep -q 1 || return 1
	reg="$(psql -d "$name" -tAc "SELECT to_regclass('public.account') IS NOT NULL" 2>/dev/null || true)"
	[ "$(printf '%s' "$reg" | tr -d '[:space:]')" = "t" ]
}
```
(and the two functions after it). Replace the three functions with:
```bash
db_is_seeded() {
	local name="$1" reg
	pg psql -U prohibitorum -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = '$name'" 2>/dev/null | grep -q 1 || return 1
	reg="$(pg psql -U prohibitorum -d "$name" -tAc "SELECT to_regclass('public.account') IS NOT NULL" 2>/dev/null || true)"
	[ "$(printf '%s' "$reg" | tr -d '[:space:]')" = "t" ]
}

db_has_admin() {
	local name="$1" out
	out="$(pg psql -U prohibitorum -d "$name" -tAc "SELECT EXISTS(SELECT 1 FROM account WHERE role = 'admin' AND NOT disabled)" 2>/dev/null || true)"
	[ "$(printf '%s' "$out" | tr -d '[:space:]')" = "t" ]
}

recreate_db() {
	local name="$1"
	pg psql -U prohibitorum -d postgres -c "DROP DATABASE IF EXISTS $name WITH (FORCE)" >/dev/null
	pg createdb -U prohibitorum "$name"
	echo "recreated database $name (clean slate)"
}
```

- [ ] **Step 3: `dev-forward-auth.sh` — replace the DB-probe block**

Find (around lines 106–112):
```bash
# --- DB: ensure the FA database on the dev cluster ---------------------------
export PGHOST=localhost PGPORT=5432 PGUSER=prohibitorum PGPASSWORD=prohibitorum
FA_DB="postgres://prohibitorum:prohibitorum@localhost:5432/prohibitorum_fa?sslmode=disable"

if ! psql -d postgres -tAc 'SELECT 1' >/dev/null 2>&1; then
	echo "ERROR: Postgres not reachable on localhost:5432 — start the podman Postgres with 'podman compose up -d'." >&2
	exit 1
fi
```
Replace with:
```bash
# --- DB: ensure the dev Postgres (container) is up. psql/createdb run INSIDE
# the container via the pg() helper from db.sh — no host Postgres client needed.
# shellcheck disable=SC1091
. "$ROOT/scripts/db.sh"
db_ensure
FA_DB="postgres://prohibitorum:prohibitorum@localhost:5432/prohibitorum_fa?sslmode=disable"
```

- [ ] **Step 4: `dev-forward-auth.sh` — route the DB helpers through `pg`**

Find (around lines 114–132) the `db_is_seeded`, `db_has_admin`, and `recreate_db` functions (identical bodies to the federation script's originals) and apply the **exact same** `pg psql -U prohibitorum …` / `pg createdb -U prohibitorum …` replacement shown in Step 2.

- [ ] **Step 5: Syntax check both scripts**

Run: `bash -n scripts/dev-federation.sh && bash -n scripts/dev-forward-auth.sh && echo "syntax OK"`
Expected: `syntax OK`.

- [ ] **Step 6: Verify the original hang scenario is fixed**

Run: `scripts/db.sh stop` (simulate the down DB that caused the hang), then `mise run dev:federation`.
Expected: it prints `dev Postgres not ready — starting it (compose)`, brings the DB up, then proceeds through seed/wire/banner (or reaches the `.dev/dev-federation.env` template step on a fresh checkout). It must **not** hang. Ctrl-C to stop once you see it progress past the DB step.

- [ ] **Step 7: Commit**

```bash
git add scripts/dev-federation.sh scripts/dev-forward-auth.sh
git commit -m "fix(tooling): harnesses auto-start DB via db.sh + in-container psql (no more hang)"
```

---

### Task 3: Point CI at the container DB deterministically

**Goal:** `ci:smoke` now starts the compose DB (the mise cluster is gone). The smoke job in `ci.yml` pins `PROHIBITORUM_COMPOSE="docker compose"` so the engine is deterministic on GitHub runners (podman's compose provider can be flaky there), while local `mise run ci:smoke` keeps auto-detecting.

**Files:**
- Modify: `.github/workflows/ci.yml` (smoke job)

> The `ci:smoke` task body was already rewritten in Task 1 (uses `scripts/db.sh start` + the compose `DATABASE_URL`). This task only wires the CI engine pin.

**Acceptance Criteria:**
- [ ] The smoke job sets `PROHIBITORUM_COMPOSE: docker compose` for the `mise run ci:smoke` step.
- [ ] No workflow references a removed task name (`db:start`, etc.).
- [ ] `mise run ci:smoke` passes locally (uses the developer's auto-detected engine).

**Verify:**
```
grep -n "PROHIBITORUM_COMPOSE" .github/workflows/ci.yml   # present on the smoke step
grep -rn "db:start\|db:up\|build:web\|dev:run\|dev:web" .github/workflows/   # → no matches
mise run ci:smoke   # green locally (SMOKE exit 0)
```

**Steps:**

- [ ] **Step 1: Edit the `smoke` job in `.github/workflows/ci.yml`**

Find:
```yaml
  smoke:
    name: End-to-end smoke
    runs-on: ubuntu-latest
    timeout-minutes: 20
    steps:
      - uses: actions/checkout@v6
      - uses: jdx/mise-action@v3
      - run: mise run ci:smoke
```
Replace with:
```yaml
  smoke:
    name: End-to-end smoke
    runs-on: ubuntu-latest
    timeout-minutes: 20
    steps:
      - uses: actions/checkout@v6
      - uses: jdx/mise-action@v3
      # Bring the dev Postgres up via compose. ubuntu-latest ships both podman and
      # docker; pin docker compose here because podman's compose provider can be
      # flaky on the runner (local `mise run ci:smoke` stays auto-detect).
      - run: mise run ci:smoke
        env:
          PROHIBITORUM_COMPOSE: docker compose
```

- [ ] **Step 2: Verify no stale task names remain in workflows**

Run: `grep -rn "db:start\|db:stop\|db:reset\|db:up\|db:status\|build:web\|build:openapi\|dev:run\|dev:web" .github/workflows/`
Expected: no matches.

- [ ] **Step 3: Run the smoke locally**

Run: `mise run ci:smoke`
Expected: server boots, `cmd/smoke` runs, exits 0 (the task's final command). Uses your local engine (podman) — the CI pin does not affect local runs.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci(tooling): smoke pins docker compose engine; container DB via db.sh"
```

---

### Task 4: Update docs — `TOOLING.md` + `compose.yaml`

**Goal:** Documentation matches the new reality: container-only dev DB, the 4-namespace catalog, and the new quick-reference commands. No doc points at a removed command or the deleted mise Postgres cluster.

**Files:**
- Modify: `TOOLING.md` (tool inventory table, the "Dev" + "CI" sections, the Task-namespaces table, the Quick reference)
- Modify: `compose.yaml` (header comment)

**Acceptance Criteria:**
- [ ] `TOOLING.md` describes the dev DB as container-only (`mise run db start`), with no `dev-db.sh`/`theseus-rs`/"podman-free cluster" as a current path.
- [ ] The Task-namespaces table lists `dev` / `db` / `ci` / `prod` with the new command names.
- [ ] The Quick reference uses `mise run db start` and `mise run dev:server` (not `db:start`).
- [ ] `grep -rn "db:start\|db:up\|build:web\|build:openapi\|dev:run\|dev:web\|dev-db.sh\|theseus-rs" TOOLING.md compose.yaml` → no matches (except any intentional "renamed from" note).

**Steps:**

- [ ] **Step 1: `TOOLING.md` — remove the Postgres-binaries row from the tool inventory table**

Delete the table row beginning `| Postgres | \`github:theseus-rs/postgresql-binaries…` in the "Tool inventory" section. Leave Go/Node/sqlc/goose rows.

- [ ] **Step 2: `TOOLING.md` — rewrite the "Dev" section**

Replace the "### Dev" bullet list with:
```markdown
### Dev

- One-stop: `mise install` provisions every pinned tool (Go, Node/npm, sqlc,
  goose, GoReleaser, cosign).
- Dev DB: **`mise run db start`** — a Postgres container from `compose.yaml`,
  via `scripts/db.sh`, which auto-detects `podman compose` or `docker compose`
  (override with `PROHIBITORUM_COMPOSE`). Works on Linux and macOS. The dev
  tasks (`dev:server`, `dev:seed`, `dev:enroll-admin`, the harnesses) call
  `scripts/db.sh ensure` to start it automatically when it's down.
- Env: `scripts/dev-env.sh` exports the dev `PROHIBITORUM_*` vars + a stable
  `.dev/encryption-key`, sourced internally by the dev tasks.
```

- [ ] **Step 3: `TOOLING.md` — fix the "CI" section's smoke bullet**

Replace the smoke bullet under "### CI" with:
```markdown
- **smoke** runs `mise run ci:smoke` (`scripts/db.sh start` → server →
  `cmd/smoke`). The smoke job pins `PROHIBITORUM_COMPOSE=docker compose` so the
  container engine is deterministic on the runner.
```

- [ ] **Step 4: `TOOLING.md` — replace the "Task namespaces" table**

Replace the namespaces table with:
```markdown
| Namespace | Context | Commands |
|-----------|---------|----------|
| `dev:*` | local development | `dev:server`, `dev:dashboard`, `dev:demo`, `dev:enroll-admin`, `dev:seed`, `dev:federation`, `dev:forward-auth`, `dev:openapi` |
| `db` | local Postgres lifecycle (dev + smoke) | `mise run db start\|stop\|reset\|migrate\|status` |
| `ci:*` | the checks CI runs | `ci`, `ci:smoke` (internal: `ci:go`, `ci:frontend`) |
| `prod:*` | **production** build + release | `prod:build`, `prod:release` |

The SPA bundle build is the hidden, `sources`/`outputs`-gated `build:web` task,
shared by `dev:server`, `prod:build`, and the GoReleaser before-hook.
```

- [ ] **Step 5: `TOOLING.md` — update the Quick reference block**

Replace the quick-reference code block with:
```bash
mise install                       # provision the locked toolchain (Go, Node/npm, sqlc, goose, …)
mise run db start                  # start the dev Postgres (compose; podman or docker)
mise run dev:server                # start DB if needed + build SPA if changed + run server on :8080
mise run dev:enroll-admin -- --new # bootstrap an admin
mise run ci                        # the full fast gate (what CI runs)
mise run ci:smoke                  # end-to-end smoke against a real server + DB
mise run prod:build                # SPA -> pkg/webui/dist, then compile ./prohibitorum
mise run prod:release              # release: binaries + OCI images (on a git tag; --snapshot to dry-run)
mise lock                          # refresh mise.lock after changing [tools]
```

- [ ] **Step 6: `TOOLING.md` — fix the federation subsection's DB setup line**

In the "### `mise run dev:federation`" subsection, change the setup step that says `1. \`mise run db:start\`` to `1. (optional) \`mise run db start\` — the harness auto-starts it otherwise.` Leave the rest of that subsection intact.

- [ ] **Step 7: `compose.yaml` — update the header comment**

Replace the top comment block (lines 1–9) with:
```yaml
# The dev/test Postgres for Prohibitorum — the single container definition used
# by `mise run db …` (scripts/db.sh), the dev tasks, and `mise run ci:smoke`.
# Works with podman or docker (set PROHIBITORUM_COMPOSE to force one).
#
#   mise run db start           # start Postgres on localhost:5432
#   mise run db stop            # stop (keeps data in the named volume)
#   mise run db reset           # stop and WIPE the database
#
# Postgres only: the dev KV defaults to the in-process "memory" driver
# (configx: kv.driver=memory), so no Redis container is needed locally.
```

- [ ] **Step 8: Verify no stale references**

Run: `grep -rn "db:start\|db:up\|db:status\|build:web\|build:openapi\|dev:run\|dev:web\|dev-db.sh\|theseus-rs\|podman-free" TOOLING.md compose.yaml`
Expected: no matches (a single "renamed from" note, if you add one, is acceptable).

- [ ] **Step 9: Commit**

```bash
git add TOOLING.md compose.yaml
git commit -m "docs(tooling): container-only dev DB + new mise catalog in TOOLING.md/compose"
```

---

### Task 5: Final verification & done-gate

**Goal:** Prove the whole redesign works end-to-end and nothing dangles.

**Files:** none (verification only)

**Acceptance Criteria:**
- [ ] `mise tasks` shows exactly 13 visible tasks in 4 groups.
- [ ] `mise run ci` is green.
- [ ] `mise run ci:smoke` is green on the compose DB.
- [ ] A cold `dev:server` (DB down) comes up on :8080.
- [ ] The federation hang scenario is fixed.
- [ ] No repo reference to a removed task name or `dev-db.sh`.

**Steps:**

- [ ] **Step 1: Catalog shape**

Run: `mise tasks | wc -l` and `mise tasks`
Expected: 13 visible task rows across `dev`/`db`/`ci`/`prod`.

- [ ] **Step 2: Full gate**

Run: `mise run ci`
Expected: Go vet/build/test pass; frontend install/test/build pass; dist-freshness guard passes (clean tree).

- [ ] **Step 3: Smoke**

Run: `mise run ci:smoke`
Expected: exits 0.

- [ ] **Step 4: Cold dev:server**

Run: `scripts/db.sh stop` then `mise run dev:server`
Expected: prints the auto-start line, builds the SPA if needed, serves on :8080 (`curl -sf http://localhost:8080/.well-known/openid-configuration` returns JSON in another shell). Ctrl-C to stop.

- [ ] **Step 5: Repo-wide dangling-reference sweep**

Run:
```
grep -rn --exclude-dir=.git --exclude-dir=node_modules --exclude-dir=dist \
  "db:start\|db:stop\|db:reset\|db:up\|db:status\|build:web\|build:openapi\|dev:run\|dev:web\|dev-db.sh" \
  . | grep -v "docs/superpowers/"
```
Expected: no matches outside the `docs/superpowers/` spec+plan (which legitimately mention old names in the rename table).

- [ ] **Step 6: Confirm the deleted script and pin are gone**

Run: `test ! -e scripts/dev-db.sh && echo "dev-db.sh removed"` and `grep -c postgresql-binaries mise.toml mise.lock`
Expected: `dev-db.sh removed`; counts `0`.

- [ ] **Step 7: Final commit (if any verification fixups were made)**

```bash
git add -A
git commit -m "chore(tooling): mise command audit — final verification fixups" || echo "nothing to commit"
```

---

## Self-Review (completed during planning)

- **Spec coverage:** Section 1 catalog → Task 1; Section 2 DB mechanics → Tasks 0+2; Section 3 self-containment → Tasks 0+1 (db_ensure, sources/outputs); Section 4 reference updates → Tasks 1–4; Section 5 done-gate → Task 5. ✓
- **Placeholder scan:** every code/step block contains concrete content; no TBD/TODO. The one judgment note (dashboard source globs) is explicit and bounded. ✓
- **Type/name consistency:** `compose_cmd`, `pg`, `db_ready`, `db_ensure`, `db_start`, `db_stop`, `db_reset`, `db_migrate`, `db_status` defined in Task 0 and used consistently in Tasks 1–2; `PROHIBITORUM_COMPOSE` env name consistent across Tasks 0/1/3/4; the 13 task names match between Task 1 and Task 5. ✓
