#!/usr/bin/env bash
# Shared dev environment sourced by the `mise run dev:*` and `mise run db:*` tasks.
# SOURCE this — it only exports vars, it execs nothing:
#   . ./scripts/dev-env.sh
#
# Assumes the dev Postgres (compose.yaml) is running on localhost:5432 with user
# prohibitorum and a prohibitorum_dev database. Start it with `mise run db start`
# (podman or docker; see scripts/db.sh).
#
# Exports:
# - PROHIBITORUM_DATA_ENCRYPTION_KEY_V1 — generated once into .dev/encryption-key
#   (gitignored, stable across runs so persisted data stays decryptable).
# - PROHIBITORUM_PUBLIC_ORIGIN and PROHIBITORUM_DATABASE_URL — only if unset, so
#   you can override either to point at a different origin or database.
mkdir -p .dev
[ -f .dev/encryption-key ] || openssl rand -base64 32 > .dev/encryption-key
export PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(cat .dev/encryption-key)"
export PROHIBITORUM_PUBLIC_ORIGIN="${PROHIBITORUM_PUBLIC_ORIGIN:-http://localhost:8080}"
export PROHIBITORUM_DATABASE_URL="${PROHIBITORUM_DATABASE_URL:-postgres://prohibitorum:prohibitorum@localhost:5432/prohibitorum_dev?sslmode=disable}"
