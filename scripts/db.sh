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

# Drop + recreate ONE named database (the smoke uses this for a clean slate).
# Exposed as a subcommand so callers that can't source this file — e.g. a mise
# task body, which may run under plain sh — get it by EXECUTING db.sh instead.
db_recreate() {
	local name="${1:?usage: db.sh recreate-db <dbname>}"
	pg psql -U prohibitorum -d postgres -c "DROP DATABASE IF EXISTS $name WITH (FORCE)" >/dev/null
	pg createdb -U prohibitorum "$name"
	echo "recreated database $name"
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
	recreate-db) db_recreate "${2:-}" ;;
	"" | -h | --help) _usage ;;
	*) echo "unknown subcommand: $1" >&2; _usage >&2; exit 2 ;;
	esac
}

# Dispatch only when executed; sourcing just defines the helpers above.
if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
	set -euo pipefail
	_main "$@"
fi
