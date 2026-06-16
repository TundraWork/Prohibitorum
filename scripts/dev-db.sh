#!/usr/bin/env bash
# Podman-free local Postgres for development — for macOS (or any host without a
# working container runtime), where `podman compose up -d` from compose.yaml is
# not an option.
#
# Manages a self-contained cluster under .dev/pgdata that is a DROP-IN
# REPLACEMENT for the compose.yaml container: same port (5432), superuser role
# (prohibitorum) and database (prohibitorum_dev) that scripts/dev-env.sh — and
# therefore `mise run db:up`, `mise run dev:server`, `mise run dev:enroll-admin`,
# `mise run dev:seed`, and `cmd/smoke` — expect. Nothing else changes; the DATABASE_URL is
# identical to the containerised one.
#
# The Postgres binaries come from mise (`github:theseus-rs/postgresql-binaries`
# in mise.toml, prebuilt — not the source-building default), so `mise db:start`
# runs this with initdb/pg_ctl on PATH on any platform, no system Postgres
# install needed. (Run it directly, outside mise, only if initdb/pg_ctl are
# already on your PATH.) Data persists across start/stop in .dev/pgdata
# (gitignored), like the compose named volume; use `reset` to wipe.
# Local connections use trust auth (no password) — fine for a localhost dev DB;
# the password in the dev-env.sh URL is simply ignored.
#
#   scripts/dev-db.sh start    # initdb on first run, start, ensure prohibitorum_dev
#   scripts/dev-db.sh stop
#   scripts/dev-db.sh status
#   scripts/dev-db.sh reset    # stop + delete the data directory
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PGDATA="${PGDATA:-$ROOT/.dev/pgdata}"
PGPORT="${PGPORT:-5432}"
SUPERUSER="prohibitorum"
DBNAME="prohibitorum_dev"
LOG="$PGDATA/server.log"

need() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "error: '$1' not on PATH — run via 'mise db:start' (mise provides Postgres), or put initdb/pg_ctl on your PATH" >&2
		exit 1
	}
}

running() { pg_ctl -D "$PGDATA" status >/dev/null 2>&1; }

start() {
	need initdb
	need pg_ctl
	need createdb
	need psql
	if [ ! -d "$PGDATA/base" ]; then
		echo "initdb $PGDATA (superuser=$SUPERUSER, trust auth)"
		mkdir -p "$PGDATA"
		initdb -D "$PGDATA" -U "$SUPERUSER" --auth=trust >/dev/null
	fi
	if running; then
		echo "postgres already running (PGDATA=$PGDATA)"
	else
		echo "starting postgres on :$PGPORT"
		pg_ctl -D "$PGDATA" -o "-p $PGPORT -k $PGDATA" -l "$LOG" -w start
	fi
	# initdb only creates postgres/template{0,1}; ensure the dev database exists.
	if ! psql -h localhost -p "$PGPORT" -U "$SUPERUSER" -d postgres -tAc \
		"SELECT 1 FROM pg_database WHERE datname='$DBNAME'" | grep -q 1; then
		echo "creating database $DBNAME"
		createdb -h localhost -p "$PGPORT" -U "$SUPERUSER" "$DBNAME"
	fi
	echo "ready: postgres://$SUPERUSER@localhost:$PGPORT/$DBNAME (matches scripts/dev-env.sh)"
}

stop() {
	need pg_ctl
	if running; then
		pg_ctl -D "$PGDATA" -m fast -w stop
	else
		echo "postgres not running (PGDATA=$PGDATA)"
	fi
}

status() {
	need pg_ctl
	pg_ctl -D "$PGDATA" status || true
}

reset() {
	stop || true
	echo "wiping $PGDATA"
	rm -rf "$PGDATA"
	echo "reset done — run 'scripts/dev-db.sh start' to recreate"
}

case "${1:-}" in
start) start ;;
stop) stop ;;
status) status ;;
reset) reset ;;
*)
	echo "usage: $0 {start|stop|status|reset}" >&2
	exit 2
	;;
esac
