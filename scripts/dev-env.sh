#!/usr/bin/env bash
# Shared dev environment for the `mise dev-server` / `mise enroll-admin` tasks.
# SOURCE this (it only exports vars + ensures the dev DB exists; it execs nothing):
#   . ./scripts/dev-env.sh
#
# - Data-encryption key: generated once into .dev/encryption-key (gitignored,
#   stable across runs so persisted data stays decryptable).
# - Dedicated 'prohibitorum_dev' database, isolated from the smoke's 'postgres'
#   DB (auto-created when psql is available). The connection honors libpq's
#   PGUSER / PGHOST / PGPORT, defaulting to the current OS user on localhost:5432.
#   Override PROHIBITORUM_DATABASE_URL (or PGPORT etc.) to point elsewhere — when
#   PROHIBITORUM_DATABASE_URL is set, DB auto-create is skipped (point it at an
#   existing, migratable DB). Override PROHIBITORUM_PUBLIC_ORIGIN similarly.
mkdir -p .dev
[ -f .dev/encryption-key ] || openssl rand -base64 32 > .dev/encryption-key
export PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(cat .dev/encryption-key)"
export PROHIBITORUM_PUBLIC_ORIGIN="${PROHIBITORUM_PUBLIC_ORIGIN:-http://localhost:8080}"
if [ -z "${PROHIBITORUM_DATABASE_URL:-}" ]; then
  _pguser="${PGUSER:-$USER}"
  _pghost="${PGHOST:-localhost}"
  _pgport="${PGPORT:-5432}"
  export PROHIBITORUM_DATABASE_URL="postgres://${_pguser}@${_pghost}:${_pgport}/prohibitorum_dev?sslmode=disable"
  _psql="$(command -v psql || mise which psql 2>/dev/null || true)"
  if [ -n "$_psql" ]; then
    _maint="postgres://${_pguser}@${_pghost}:${_pgport}/postgres?sslmode=disable"
    "$_psql" "$_maint" -tAc "SELECT 1 FROM pg_database WHERE datname='prohibitorum_dev'" 2>/dev/null | grep -q 1 \
      || "$_psql" "$_maint" -c "CREATE DATABASE prohibitorum_dev" >/dev/null 2>&1 || true
  fi
fi
