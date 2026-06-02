#!/usr/bin/env bash
# Shared dev environment for the `mise dev-server` / `mise enroll-admin` tasks.
# SOURCE this (it only exports vars + ensures the dev DB exists; it execs nothing):
#   . ./scripts/dev-env.sh
#
# - Data-encryption key: generated once into .dev/encryption-key (gitignored,
#   stable across runs so persisted data stays decryptable).
# - Dedicated 'prohibitorum_dev' database, isolated from the smoke's 'postgres'
#   DB (auto-created when psql is available). Override PROHIBITORUM_DATABASE_URL
#   or PROHIBITORUM_PUBLIC_ORIGIN to point elsewhere (then DB auto-create is
#   skipped — point at an existing, migratable DB).
mkdir -p .dev
[ -f .dev/encryption-key ] || openssl rand -base64 32 > .dev/encryption-key
export PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(cat .dev/encryption-key)"
export PROHIBITORUM_PUBLIC_ORIGIN="${PROHIBITORUM_PUBLIC_ORIGIN:-http://localhost:8080}"
if [ -z "${PROHIBITORUM_DATABASE_URL:-}" ]; then
  export PROHIBITORUM_DATABASE_URL="postgres://tundra@localhost:55432/prohibitorum_dev?sslmode=disable"
  _psql="$(command -v psql || mise which psql 2>/dev/null || true)"
  if [ -n "$_psql" ]; then
    _maint="postgres://tundra@localhost:55432/postgres?sslmode=disable"
    "$_psql" "$_maint" -tAc "SELECT 1 FROM pg_database WHERE datname='prohibitorum_dev'" 2>/dev/null | grep -q 1 \
      || "$_psql" "$_maint" -c "CREATE DATABASE prohibitorum_dev" >/dev/null 2>&1 || true
  fi
fi
