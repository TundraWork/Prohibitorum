#!/usr/bin/env bash
# Shared dev environment for the `mise dev-*` and `mise db:*` tasks.
# SOURCE this — it only exports vars, it execs nothing:
#   . ./scripts/dev-env.sh
#
# Assumes the dev Postgres is running (see compose.yaml):
#   podman compose up -d
# which exposes Postgres on localhost:5432 with user/password prohibitorum and
# a prohibitorum_dev database.
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
