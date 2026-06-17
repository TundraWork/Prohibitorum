#!/usr/bin/env bash
# Bring up two local prohibitorum instances (upstream OP + downstream RP) wired
# for OIDC federation behind nginx TLS, for manual end-to-end testing.
#
# Deployment-specific values (hostnames, cert paths) are read from the gitignored
# .dev/dev-federation.env — NEVER hardcode real infra here. See the spec:
# docs/superpowers/specs/2026-06-17-dev-federation-harness-design.md
#
#   mise run dev:federation            # bring it up (Ctrl-C stops both)
#   mise run dev:federation -- --fresh # wipe + recreate both DBs first
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

FRESH=0
[ "${1:-}" = "--fresh" ] && FRESH=1

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

# --- DB: ensure the two federation databases on the dev cluster ------------
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
ensure_db() {
	local name="$1"
	if [ "$FRESH" = "1" ]; then
		psql -d postgres -c "DROP DATABASE IF EXISTS $name WITH (FORCE)" >/dev/null
	fi
	if ! psql -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='$name'" | grep -q 1; then
		createdb "$name"
		echo "created database $name"
	fi
}
ensure_db prohibitorum_upstream
ensure_db prohibitorum_downstream

# --- 3. build once (matches smoke/release) ---------------------------------
BIN_DIR="$(mktemp -d)"
BIN="$BIN_DIR/prohibitorum"
echo "building prohibitorum (-tags nodynamic) ..."
go build -tags nodynamic -o "$BIN" ./cmd/prohibitorum

# --- 4. per-instance seed + admin enrollment -------------------------------
setup_instance() {
	local origin="$1" dburl="$2" label="$3"
	echo "==> [$label] dev-seed"
	PROHIBITORUM_PUBLIC_ORIGIN="$origin" PROHIBITORUM_DATABASE_URL="$dburl" "$BIN" dev-seed
	echo "==> [$label] enroll-admin"
	if ! PROHIBITORUM_PUBLIC_ORIGIN="$origin" PROHIBITORUM_DATABASE_URL="$dburl" "$BIN" enroll-admin; then
		echo "    [$label] admin already enrolled — sign in at $origin"
	fi
}
setup_instance "$UP_ORIGIN" "$UP_DB" upstream
setup_instance "$DOWN_ORIGIN" "$DOWN_DB" downstream

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
	PROHIBITORUM_HOST=127.0.0.1 PROHIBITORUM_PORT="$UP_PORT" PROHIBITORUM_TRUST_PROXY=true \
	"$BIN" >"$UP_LOG" 2>&1 &
UP_PID=$!
PROHIBITORUM_DATABASE_URL="$DOWN_DB" PROHIBITORUM_PUBLIC_ORIGIN="$DOWN_ORIGIN" \
	PROHIBITORUM_HOST=127.0.0.1 PROHIBITORUM_PORT="$DOWN_PORT" PROHIBITORUM_TRUST_PROXY=true \
	PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK=true \
	"$BIN" >"$DOWN_LOG" 2>&1 &
DOWN_PID=$!

TAIL_PID=""
cleanup() { kill "$UP_PID" "$DOWN_PID" ${TAIL_PID:+"$TAIL_PID"} 2>/dev/null || true; rm -rf "$BIN_DIR"; }
trap cleanup INT TERM EXIT

# --- 8. wait for backends, probe nginx, banner -----------------------------
for url in "http://127.0.0.1:$UP_PORT/.well-known/openid-configuration" \
	"http://127.0.0.1:$DOWN_PORT/.well-known/openid-configuration"; do
	for _ in $(seq 1 60); do curl -sf "$url" >/dev/null 2>&1 && break; sleep 1; done
done
if curl -sf "$UP_ORIGIN/.well-known/openid-configuration" >/dev/null 2>&1; then
	NGINX_NOTE="nginx is routing $UP_ORIGIN"
else
	NGINX_NOTE="NOTE: $UP_ORIGIN not reachable — install the nginx vhost (command above) + reload"
fi
cat <<EOF

============================================================
  prohibitorum dev federation harness is UP
  Upstream  (OP): $UP_ORIGIN   (backend 127.0.0.1:$UP_PORT)
  Downstream(RP): $DOWN_ORIGIN (backend 127.0.0.1:$DOWN_PORT)
  $NGINX_NOTE
  Test: open $DOWN_ORIGIN and click "Upstream".
  Logs: $UP_LOG / $DOWN_LOG   |   Ctrl-C stops both.
============================================================
EOF

tail -n +1 -f "$UP_LOG" "$DOWN_LOG" &
TAIL_PID=$!
wait "$UP_PID" "$DOWN_PID"
