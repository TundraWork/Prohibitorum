#!/usr/bin/env bash
# Bring up two local prohibitorum instances (upstream OP + downstream RP) wired
# for OIDC federation behind nginx TLS, for manual end-to-end testing.
#
# Deployment-specific values (hostnames, cert paths) are read from the gitignored
# .dev/dev-federation.env — NEVER hardcode real infra here. See the spec:
# docs/superpowers/specs/2026-06-17-dev-federation-harness-design.md
#
#   mise run dev:federation            # bring it up (Ctrl-C stops both)
#   mise run dev:federation -- --fresh # wipe + reseed both DBs first
#
# By DEFAULT this is idempotent: a database that already exists and is set up is
# REUSED as-is — no drop, no reseed — so manual test state (accounts, passkeys,
# config) survives across runs. Only a missing or un-migrated database is created
# clean and seeded. Pass --fresh to force the old clean-slate behavior (drop +
# recreate + reseed + re-enroll both instances). Federation wiring (idempotent)
# runs every time and re-applies any pending migrations.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# --- 0. args ---------------------------------------------------------------
FRESH=0
for arg in "$@"; do
	case "$arg" in
	--fresh | --clean) FRESH=1 ;;
	-h | --help)
		sed -n '2,17p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	*)
		echo "unknown argument: $arg (try --fresh or --help)" >&2
		exit 1
		;;
	esac
done

LOCAL_ENV=".dev/dev-federation.env"
mkdir -p .dev/nginx .dev/logs

# --- 1. local config (gitignored; never committed) -------------------------
if [ ! -f "$LOCAL_ENV" ]; then
	cat >"$LOCAL_ENV" <<'TEMPLATE'
# .dev/dev-federation.env — LOCAL ONLY, never committed (.dev/ is gitignored).
# Fill in your real values, then re-run `mise run dev:federation`.
DEV_FED_UPSTREAM_HOST=idp-a.example.test
DEV_FED_DOWNSTREAM_HOST=idp-b.example.test
DEV_FED_TLS_CERT=/etc/nginx/ssl.d/wildcard.cer
DEV_FED_TLS_KEY=/etc/nginx/ssl.d/wildcard.key
# Optional overrides:
DEV_FED_UPSTREAM_BACKEND_PORT=18080
DEV_FED_DOWNSTREAM_BACKEND_PORT=18081
DEV_FED_NGINX_DIR=/etc/nginx/hosts.d
TEMPLATE
	echo "Wrote a template to $LOCAL_ENV."
	echo "Fill in your real hostnames + cert paths (both pinned to 127.0.0.1), then re-run."
	exit 1
fi
# shellcheck disable=SC1090
. "$LOCAL_ENV"

: "${DEV_FED_UPSTREAM_HOST:?set DEV_FED_UPSTREAM_HOST in $LOCAL_ENV}"
: "${DEV_FED_DOWNSTREAM_HOST:?set DEV_FED_DOWNSTREAM_HOST in $LOCAL_ENV}"
: "${DEV_FED_TLS_CERT:?set DEV_FED_TLS_CERT in $LOCAL_ENV}"
: "${DEV_FED_TLS_KEY:?set DEV_FED_TLS_KEY in $LOCAL_ENV}"
UP_PORT="${DEV_FED_UPSTREAM_BACKEND_PORT:-18080}"
DOWN_PORT="${DEV_FED_DOWNSTREAM_BACKEND_PORT:-18081}"
NGINX_DIR="${DEV_FED_NGINX_DIR:-/etc/nginx/hosts.d}"
UP_ORIGIN="https://$DEV_FED_UPSTREAM_HOST"
DOWN_ORIGIN="https://$DEV_FED_DOWNSTREAM_HOST"

# --- resolution check (browser + Go both need loopback) --------------------
for h in "$DEV_FED_UPSTREAM_HOST" "$DEV_FED_DOWNSTREAM_HOST"; do
	ip="$(getent hosts "$h" 2>/dev/null | awk '{print $1; exit}')" || true
	case "$ip" in
	127.* | ::1) ;;
	*)
		echo "ERROR: $h does not resolve to loopback (got '${ip:-nothing}'). Point its DNS at 127.0.0.1." >&2
		exit 1
		;;
	esac
done

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

# db_is_seeded NAME — true if the database exists AND is migrated (has the
# `account` table). A bare createdb'd-but-empty DB (e.g. a previous run that
# crashed mid-setup) reports false so it gets recreated clean rather than reused.
db_is_seeded() {
	local name="$1" reg
	pg psql -U prohibitorum -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = '$name'" 2>/dev/null | grep -q 1 || return 1
	reg="$(pg psql -U prohibitorum -d "$name" -tAc "SELECT to_regclass('public.account') IS NOT NULL" 2>/dev/null || true)"
	[ "$(printf '%s' "$reg" | tr -d '[:space:]')" = "t" ]
}

# db_has_admin NAME — true if an active admin account already exists (mirrors the
# backend's HasAnyActiveAdmin). Gates enroll-admin so reuse doesn't abort on the
# "an admin already exists" error.
db_has_admin() {
	local name="$1" out
	out="$(pg psql -U prohibitorum -d "$name" -tAc "SELECT EXISTS(SELECT 1 FROM account WHERE role = 'admin' AND NOT disabled)" 2>/dev/null || true)"
	[ "$(printf '%s' "$out" | tr -d '[:space:]')" = "t" ]
}

# recreate_db NAME — destructive clean slate. WITH (FORCE) terminates any
# lingering connections (e.g. a backend from a previous run).
recreate_db() {
	local name="$1"
	pg psql -U prohibitorum -d postgres -c "DROP DATABASE IF EXISTS $name WITH (FORCE)" >/dev/null
	pg createdb -U prohibitorum "$name"
	echo "recreated database $name (clean slate)"
}

# ensure_db NAME — idempotent unless --fresh. Sets DB_MODE to fresh|reuse so the
# caller knows whether to seed + enroll. Reuse preserves all existing data.
DB_MODE=""
ensure_db() {
	local name="$1"
	if [ "$FRESH" = 1 ]; then
		recreate_db "$name"
		DB_MODE=fresh
		return
	fi
	if db_is_seeded "$name"; then
		echo "reusing existing database $name (data preserved; pass --fresh to wipe)"
		DB_MODE=reuse
	else
		recreate_db "$name"
		DB_MODE=fresh
	fi
}
ensure_db prohibitorum_upstream
UP_MODE="$DB_MODE"
ensure_db prohibitorum_downstream
DOWN_MODE="$DB_MODE"

# --- 3. build once (matches smoke/release) ---------------------------------
BIN_DIR="$(mktemp -d)"
# Arm cleanup of the tempdir immediately (a build failure aborts before the
# full trap below is set); extended to also kill the backends once they start.
trap 'rm -rf "$BIN_DIR"' EXIT
BIN="$BIN_DIR/prohibitorum"
echo "building prohibitorum (-tags nodynamic) ..."
go build -tags nodynamic -o "$BIN" ./cmd/prohibitorum

# --- 4. per-instance seed + admin enrollment -------------------------------
# This federation harness is the designated dev test env, so every start runs
# dev-seed against both instances. dev-seed is idempotent (it skips rows that
# already exist): a reused DB gets any missing demo data topped up without
# wiping anything, and a fresh DB is populated from empty. enroll-admin only
# runs if no admin exists yet (so a reused instance whose admin is already
# registered isn't disturbed). The enroll URL (when issued) is captured into the
# named output var for the banner.
UP_ENROLL_URL=""
DOWN_ENROLL_URL=""
setup_instance() {
	local origin="$1" dburl="$2" dbname="$3" label="$4" outvar="$5"
	echo "==> [$label] dev-seed"
	PROHIBITORUM_PUBLIC_ORIGIN="$origin" PROHIBITORUM_DATABASE_URL="$dburl" "$BIN" dev-seed
	if db_has_admin "$dbname"; then
		echo "==> [$label] admin already enrolled — skipping enroll-admin (re-issue with 'enroll-admin --reset --username NAME', or run with --fresh)"
		printf -v "$outvar" '%s' ""
		return
	fi
	echo "==> [$label] enroll-admin"
	local out
	if out="$(PROHIBITORUM_PUBLIC_ORIGIN="$origin" PROHIBITORUM_DATABASE_URL="$dburl" "$BIN" enroll-admin 2>&1)"; then
		echo "$out"
		printf -v "$outvar" '%s' "$(printf '%s\n' "$out" | grep -oE 'https?://[^ ]+/enroll/[A-Za-z0-9._-]+' | head -1)"
	else
		echo "$out" >&2
		echo "ERROR: [$label] enroll-admin failed" >&2
		exit 1
	fi
}
setup_instance "$UP_ORIGIN" "$UP_DB" prohibitorum_upstream upstream UP_ENROLL_URL
setup_instance "$DOWN_ORIGIN" "$DOWN_DB" prohibitorum_downstream downstream DOWN_ENROLL_URL

# --- 5. wire federation ----------------------------------------------------
echo "==> wiring federation"
PROHIBITORUM_PUBLIC_ORIGIN="$DOWN_ORIGIN" PROHIBITORUM_DATABASE_URL="$DOWN_DB" \
	"$BIN" dev-federation \
	--upstream-db "$UP_DB" --downstream-db "$DOWN_DB" \
	--upstream-origin "$UP_ORIGIN" --downstream-origin "$DOWN_ORIGIN"

# --- 6. generate nginx vhost (single file, both blocks) --------------------
NGINX_CONF=".dev/nginx/prohibitorum-federation.conf"
cat >"$NGINX_CONF" <<EOF
# Generated by scripts/dev-federation.sh — do not commit; re-run regenerates it.
server {
    listen 80; listen [::]:80;
    server_name $DEV_FED_UPSTREAM_HOST;
    return 301 https://\$host\$request_uri;
}
server {
    listen 443 ssl; listen [::]:443 ssl; http2 on;
    server_name $DEV_FED_UPSTREAM_HOST;
    ssl_certificate     $DEV_FED_TLS_CERT;
    ssl_certificate_key $DEV_FED_TLS_KEY;
    location / {
        proxy_pass         http://127.0.0.1:$UP_PORT;
        proxy_http_version 1.1;
        proxy_set_header   Host              \$host;
        proxy_set_header   X-Real-IP         \$remote_addr;
        proxy_set_header   X-Forwarded-For   \$proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto \$scheme;
        proxy_set_header   X-Forwarded-Host  \$host;
    }
}
server {
    listen 80; listen [::]:80;
    server_name $DEV_FED_DOWNSTREAM_HOST;
    return 301 https://\$host\$request_uri;
}
server {
    listen 443 ssl; listen [::]:443 ssl; http2 on;
    server_name $DEV_FED_DOWNSTREAM_HOST;
    ssl_certificate     $DEV_FED_TLS_CERT;
    ssl_certificate_key $DEV_FED_TLS_KEY;
    location / {
        proxy_pass         http://127.0.0.1:$DOWN_PORT;
        proxy_http_version 1.1;
        proxy_set_header   Host              \$host;
        proxy_set_header   X-Real-IP         \$remote_addr;
        proxy_set_header   X-Forwarded-For   \$proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto \$scheme;
        proxy_set_header   X-Forwarded-Host  \$host;
    }
}
EOF
echo
echo "nginx vhost generated: $NGINX_CONF"
echo "  install once (needs root):"
echo "    sudo cp $NGINX_CONF $NGINX_DIR/ && sudo nginx -t && sudo systemctl reload nginx"

# --- 7. launch both backends (loopback http; nginx terminates TLS) ---------
UP_LOG=".dev/logs/upstream.log"
DOWN_LOG=".dev/logs/downstream.log"
PROHIBITORUM_DATABASE_URL="$UP_DB" PROHIBITORUM_PUBLIC_ORIGIN="$UP_ORIGIN" \
	PROHIBITORUM_HOST=127.0.0.1 PROHIBITORUM_PORT="$UP_PORT" \
	"$BIN" >"$UP_LOG" 2>&1 &
UP_PID=$!
PROHIBITORUM_DATABASE_URL="$DOWN_DB" PROHIBITORUM_PUBLIC_ORIGIN="$DOWN_ORIGIN" \
	PROHIBITORUM_HOST=127.0.0.1 PROHIBITORUM_PORT="$DOWN_PORT" \
	"$BIN" >"$DOWN_LOG" 2>&1 &
DOWN_PID=$!

TAIL_PID=""
cleanup() { kill "$UP_PID" "$DOWN_PID" ${TAIL_PID:+"$TAIL_PID"} 2>/dev/null || true; rm -rf "$BIN_DIR"; }
trap cleanup INT TERM EXIT

# --- 8. wait for backends, probe nginx, banner -----------------------------
# Track liveness per backend: if one never answers (port clash, bad DSN, crash),
# surface its log tail and abort rather than print a misleading "UP" banner.
for url in "http://127.0.0.1:$UP_PORT/.well-known/openid-configuration" \
	"http://127.0.0.1:$DOWN_PORT/.well-known/openid-configuration"; do
	ok=0
	for _ in $(seq 1 60); do
		curl -sf "$url" >/dev/null 2>&1 && { ok=1; break; }
		sleep 1
	done
	if [ "$ok" != 1 ]; then
		echo "ERROR: backend $url never came up. Last log lines:" >&2
		tail -n 25 "$UP_LOG" "$DOWN_LOG" >&2
		exit 1
	fi
done
if curl -sf "$UP_ORIGIN/.well-known/openid-configuration" >/dev/null 2>&1; then
	NGINX_NOTE="nginx is routing $UP_ORIGIN"
else
	NGINX_NOTE="NOTE: $UP_ORIGIN not reachable — install the nginx vhost (command above) + reload"
fi
if [ "$UP_MODE" = fresh ] && [ "$DOWN_MODE" = fresh ]; then
	SLATE_NOTE="clean slate; both DBs recreated"
elif [ "$UP_MODE" = reuse ] && [ "$DOWN_MODE" = reuse ]; then
	SLATE_NOTE="reusing existing data on both DBs; pass --fresh to wipe"
else
	SLATE_NOTE="upstream=$UP_MODE, downstream=$DOWN_MODE"
fi
cat <<EOF

============================================================
  prohibitorum dev federation harness is UP ($SLATE_NOTE)
  Upstream  (OP): $UP_ORIGIN   (backend 127.0.0.1:$UP_PORT)
  Downstream(RP): $DOWN_ORIGIN (backend 127.0.0.1:$DOWN_PORT)
  $NGINX_NOTE

  Admin enrollment — open in a browser to register a passkey:
    upstream:   ${UP_ENROLL_URL:-already enrolled (pass --fresh to reset)}
    downstream: ${DOWN_ENROLL_URL:-already enrolled (pass --fresh to reset)}

  Test: open $DOWN_ORIGIN and click "Upstream".
  Logs: $UP_LOG / $DOWN_LOG   |   Ctrl-C stops both.
============================================================
EOF

tail -n +1 -f "$UP_LOG" "$DOWN_LOG" &
TAIL_PID=$!
wait "$UP_PID" "$DOWN_PID"
