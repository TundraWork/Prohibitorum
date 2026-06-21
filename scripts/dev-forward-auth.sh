#!/usr/bin/env bash
# Bring up a protected whoami app behind nginx TLS, wired to Prohibitorum
# forward-auth, for manual end-to-end browser-flow testing.
#
# Deployment-specific values (hostnames, cert paths) are read from the gitignored
# .dev/dev-forward-auth.env — NEVER hardcode real infra here.
#
#   mise run dev:forward-auth            # bring it up (Ctrl-C stops all)
#   mise run dev:forward-auth -- --fresh # wipe + reseed the DB first
#
# By DEFAULT this is idempotent: an already-set-up database is REUSED as-is —
# no drop, no reseed — so manual test state (accounts, passkeys, FA client)
# survives across runs. Only a missing or un-migrated database is created clean
# and seeded. Pass --fresh to force the old clean-slate behaviour.
#
# Flow exercised:
#   browser → https://FA_APP_HOST/  → nginx → forward-auth/verify (Prohibitorum)
#   → login on https://FA_IDP_HOST/ → callback on FA_APP_HOST → 200 + Remote-*
#   → nginx → whoami (127.0.0.1:FA_WHOAMI_PORT) → text/plain Remote-* dump
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# --- 0. args -------------------------------------------------------------------
FRESH=0
for arg in "$@"; do
	case "$arg" in
	--fresh | --clean) FRESH=1 ;;
	-h | --help)
		sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	*)
		echo "unknown argument: $arg (try --fresh or --help)" >&2
		exit 1
		;;
	esac
done

LOCAL_ENV=".dev/dev-forward-auth.env"
mkdir -p .dev/nginx .dev/logs

# --- 1. local config (gitignored; never committed) -----------------------------
if [ ! -f "$LOCAL_ENV" ]; then
	cat >"$LOCAL_ENV" <<'TEMPLATE'
# .dev/dev-forward-auth.env — LOCAL ONLY, never committed (.dev/ is gitignored).
# Fill in your real values, then re-run `mise run dev:forward-auth`.
#
# FA_IDP_HOST   — the Prohibitorum IdP hostname (where the login UI lives).
# FA_APP_HOST   — the protected app hostname (where the whoami server lives).
# Both must resolve to 127.0.0.1 (add to /etc/hosts or local DNS).
FA_IDP_HOST=auth.example.test
FA_APP_HOST=app.example.test
FA_TLS_CERT=/etc/nginx/ssl.d/wildcard.cer
FA_TLS_KEY=/etc/nginx/ssl.d/wildcard.key
# Optional overrides:
FA_IDP_BACKEND_PORT=18090
FA_WHOAMI_PORT=18091
FA_NGINX_DIR=/etc/nginx/hosts.d
FA_CLIENT_ID=dev-fa
TEMPLATE
	echo "Wrote a template to $LOCAL_ENV."
	echo "Fill in your real hostnames + cert paths (both pinned to 127.0.0.1), then re-run."
	exit 1
fi
# shellcheck disable=SC1090
. "$LOCAL_ENV"

: "${FA_IDP_HOST:?set FA_IDP_HOST in $LOCAL_ENV}"
: "${FA_APP_HOST:?set FA_APP_HOST in $LOCAL_ENV}"
: "${FA_TLS_CERT:?set FA_TLS_CERT in $LOCAL_ENV}"
: "${FA_TLS_KEY:?set FA_TLS_KEY in $LOCAL_ENV}"
IDP_PORT="${FA_IDP_BACKEND_PORT:-18090}"
WHOAMI_PORT="${FA_WHOAMI_PORT:-18091}"
NGINX_DIR="${FA_NGINX_DIR:-/etc/nginx/hosts.d}"
FA_CLIENT="${FA_CLIENT_ID:-dev-fa}"
IDP_ORIGIN="https://$FA_IDP_HOST"
APP_ORIGIN="https://$FA_APP_HOST"

# --- resolution check (browser + Go both need loopback) -----------------------
for h in "$FA_IDP_HOST" "$FA_APP_HOST"; do
	ip="$(getent hosts "$h" 2>/dev/null | awk '{print $1; exit}')" || true
	case "$ip" in
	127.* | ::1) ;;
	*)
		echo "ERROR: $h does not resolve to loopback (got '${ip:-nothing}'). Point its DNS at 127.0.0.1." >&2
		exit 1
		;;
	esac
done

# --- 2. shared encryption key (stable across restarts) -----------------------
[ -f .dev/encryption-key ] || openssl rand -base64 32 >.dev/encryption-key
export PROHIBITORUM_DATA_ENCRYPTION_KEY_V1
PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(cat .dev/encryption-key)"

# --- DB: ensure the FA database on the dev cluster ---------------------------
export PGHOST=localhost PGPORT=5432 PGUSER=prohibitorum PGPASSWORD=prohibitorum
FA_DB="postgres://prohibitorum:prohibitorum@localhost:5432/prohibitorum_fa?sslmode=disable"

if ! psql -d postgres -tAc 'SELECT 1' >/dev/null 2>&1; then
	echo "ERROR: Postgres not reachable on localhost:5432 — start the podman Postgres with 'podman compose up -d'." >&2
	exit 1
fi

db_is_seeded() {
	local name="$1" reg
	psql -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = '$name'" 2>/dev/null | grep -q 1 || return 1
	reg="$(psql -d "$name" -tAc "SELECT to_regclass('public.account') IS NOT NULL" 2>/dev/null || true)"
	[ "$(printf '%s' "$reg" | tr -d '[:space:]')" = "t" ]
}

db_has_admin() {
	local name="$1" out
	out="$(psql -d "$name" -tAc "SELECT EXISTS(SELECT 1 FROM account WHERE role = 'admin' AND NOT disabled)" 2>/dev/null || true)"
	[ "$(printf '%s' "$out" | tr -d '[:space:]')" = "t" ]
}

recreate_db() {
	local name="$1"
	psql -d postgres -c "DROP DATABASE IF EXISTS $name WITH (FORCE)" >/dev/null
	createdb "$name"
	echo "recreated database $name (clean slate)"
}

DB_MODE=""
if [ "$FRESH" = 1 ]; then
	recreate_db prohibitorum_fa
	DB_MODE=fresh
elif db_is_seeded prohibitorum_fa; then
	echo "reusing existing database prohibitorum_fa (data preserved; pass --fresh to wipe)"
	DB_MODE=reuse
else
	recreate_db prohibitorum_fa
	DB_MODE=fresh
fi

# --- 3. build once (matches smoke/release) ------------------------------------
BIN_DIR="$(mktemp -d)"
trap 'rm -rf "$BIN_DIR"' EXIT
BIN="$BIN_DIR/prohibitorum"
echo "building prohibitorum (-tags nodynamic) ..."
go build -tags nodynamic -o "$BIN" ./cmd/prohibitorum

# --- 4. seed + admin enrollment -----------------------------------------------
ENROLL_URL=""
if [ "$DB_MODE" = fresh ]; then
	echo "==> dev-seed"
	PROHIBITORUM_PUBLIC_ORIGIN="$IDP_ORIGIN" PROHIBITORUM_DATABASE_URL="$FA_DB" "$BIN" dev-seed
else
	echo "==> reusing existing data — skipping dev-seed"
fi

if db_has_admin prohibitorum_fa; then
	echo "==> admin already enrolled — skipping enroll-admin"
else
	echo "==> enroll-admin"
	enroll_out=""
	if enroll_out="$(PROHIBITORUM_PUBLIC_ORIGIN="$IDP_ORIGIN" PROHIBITORUM_DATABASE_URL="$FA_DB" "$BIN" enroll-admin 2>&1)"; then
		echo "$enroll_out"
		ENROLL_URL="$(printf '%s\n' "$enroll_out" | grep -oE 'https?://[^ ]+/enroll/[A-Za-z0-9._-]+' | head -1)"
	else
		echo "$enroll_out" >&2
		echo "ERROR: enroll-admin failed" >&2
		exit 1
	fi
fi

# --- 5. register the forward-auth app client (idempotent via create-or-rotate) -
echo "==> registering forward-auth app client '$FA_CLIENT' for host $FA_APP_HOST"
# `forward-auth-app create` is idempotent when the client already exists:
# it prints an error and exits 1. We suppress that on reuse.
if ! PROHIBITORUM_PUBLIC_ORIGIN="$IDP_ORIGIN" PROHIBITORUM_DATABASE_URL="$FA_DB" \
	"$BIN" forward-auth-app create \
	--client-id "$FA_CLIENT" \
	--host "$FA_APP_HOST" \
	--display-name "Dev ForwardAuth Whoami" 2>/dev/null; then
	echo "    (client already exists — skipped create)"
fi

# --- 6. generate nginx vhost --------------------------------------------------
NGINX_CONF=".dev/nginx/prohibitorum-forward-auth.conf"
# NOTE: The FA verify endpoint is on the IdP origin. nginx's auth_request module
# is used for the forward-auth check: it makes a sub-request to
# /api/prohibitorum/forward-auth/verify, copying back the Remote-* headers on 200.
# The per-domain callback path on FA_APP_HOST is proxied to Prohibitorum.
cat >"$NGINX_CONF" <<EOF
# Generated by scripts/dev-forward-auth.sh — do not commit; re-run regenerates it.
server {
    listen 80; listen [::]:80;
    server_name $FA_IDP_HOST;
    return 301 https://\$host\$request_uri;
}
server {
    listen 443 ssl; listen [::]:443 ssl; http2 on;
    server_name $FA_IDP_HOST;
    ssl_certificate     $FA_TLS_CERT;
    ssl_certificate_key $FA_TLS_KEY;
    location / {
        proxy_pass         http://127.0.0.1:$IDP_PORT;
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
    server_name $FA_APP_HOST;
    return 301 https://\$host\$request_uri;
}
server {
    listen 443 ssl; listen [::]:443 ssl; http2 on;
    server_name $FA_APP_HOST;
    ssl_certificate     $FA_TLS_CERT;
    ssl_certificate_key $FA_TLS_KEY;

    # --- Forward-auth sub-request (nginx auth_request module) ----------------
    # nginx contacts Prohibitorum's verify endpoint on behalf of every request.
    # Prohibitorum returns 200 (+ Remote-* headers) or 302 (login redirect).
    # nginx's auth_request cannot follow redirects, so the redirect is surfaced
    # as a 401; @login_redirect handles it. Remote-* headers from the 200 are
    # copied into the upstream request.
    location = /_auth {
        internal;
        proxy_pass              http://127.0.0.1:$IDP_PORT/api/prohibitorum/forward-auth/verify;
        proxy_pass_request_body off;
        proxy_set_header        Content-Length "";
        proxy_set_header        Host              $FA_APP_HOST;
        proxy_set_header        X-Forwarded-Host  $FA_APP_HOST;
        proxy_set_header        X-Forwarded-Proto https;
        proxy_set_header        X-Real-IP         \$remote_addr;
        proxy_set_header        X-Forwarded-For   \$proxy_add_x_forwarded_for;
        proxy_set_header        X-Forwarded-Uri   \$request_uri;
    }

    # --- Per-domain callback/sign-out path — routed to Prohibitorum -----------
    # NOT guarded by auth_request; this is where the OIDC callback lands.
    location /.prohibitorum-forward-auth/ {
        proxy_pass         http://127.0.0.1:$IDP_PORT;
        proxy_http_version 1.1;
        proxy_set_header   Host              $FA_APP_HOST;
        proxy_set_header   X-Real-IP         \$remote_addr;
        proxy_set_header   X-Forwarded-For   \$proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto https;
        proxy_set_header   X-Forwarded-Host  $FA_APP_HOST;
    }

    # --- Protected whoami app — gated by forward-auth -------------------------
    location / {
        auth_request /_auth;
        auth_request_set \$remote_user  \$upstream_http_remote_user;
        auth_request_set \$remote_name  \$upstream_http_remote_name;
        auth_request_set \$remote_email \$upstream_http_remote_email;
        auth_request_set \$remote_groups \$upstream_http_remote_groups;

        error_page 401 = @login_redirect;
        error_page 403 = @login_redirect;

        proxy_pass         http://127.0.0.1:$WHOAMI_PORT;
        proxy_http_version 1.1;
        proxy_set_header   Host         \$host;
        proxy_set_header   Remote-User   \$remote_user;
        proxy_set_header   Remote-Name   \$remote_name;
        proxy_set_header   Remote-Email  \$remote_email;
        proxy_set_header   Remote-Groups \$remote_groups;
    }

    location @login_redirect {
        # Return the 302 from Prohibitorum's verify endpoint to the browser.
        # auth_request swallows the Location header — re-fetch it from the
        # sub-request response header set via add_header (requires nginx >=1.19).
        # Simpler: capture the Location from the 302 and issue our own redirect.
        # NOTE: This approach works when Prohibitorum responds with X-Auth-Redirect
        # on 401. For nginx < 1.19 without that header, adapt as needed.
        return 302 \$upstream_http_location;
    }
}
EOF
echo
echo "nginx vhost generated: $NGINX_CONF"
echo "  install once (needs root):"
echo "    sudo cp $NGINX_CONF $NGINX_DIR/ && sudo nginx -t && sudo systemctl reload nginx"

# --- 7. start the whoami server -----------------------------------------------
WHOAMI_LOG=".dev/logs/fa-whoami.log"
"$BIN" forward-auth-whoami --addr "127.0.0.1:$WHOAMI_PORT" >"$WHOAMI_LOG" 2>&1 &
WHOAMI_PID=$!

# --- 8. start the Prohibitorum backend ----------------------------------------
IDP_LOG=".dev/logs/fa-idp.log"
PROHIBITORUM_DATABASE_URL="$FA_DB" PROHIBITORUM_PUBLIC_ORIGIN="$IDP_ORIGIN" \
	PROHIBITORUM_HOST=127.0.0.1 PROHIBITORUM_PORT="$IDP_PORT" PROHIBITORUM_TRUST_PROXY=true \
	"$BIN" >"$IDP_LOG" 2>&1 &
IDP_PID=$!

TAIL_PID=""
cleanup() { kill "$IDP_PID" "$WHOAMI_PID" ${TAIL_PID:+"$TAIL_PID"} 2>/dev/null || true; rm -rf "$BIN_DIR"; }
trap cleanup INT TERM EXIT

# --- 9. wait for backend, probe nginx, banner ---------------------------------
for url in "http://127.0.0.1:$IDP_PORT/.well-known/openid-configuration" \
	"http://127.0.0.1:$WHOAMI_PORT/"; do
	ok=0
	for _ in $(seq 1 60); do
		curl -sf "$url" >/dev/null 2>&1 && { ok=1; break; }
		sleep 1
	done
	if [ "$ok" != 1 ]; then
		echo "ERROR: backend $url never came up. Last log lines:" >&2
		tail -n 25 "$IDP_LOG" "$WHOAMI_LOG" >&2
		exit 1
	fi
done

if curl -sf "$IDP_ORIGIN/.well-known/openid-configuration" >/dev/null 2>&1; then
	NGINX_NOTE="nginx is routing $IDP_ORIGIN"
else
	NGINX_NOTE="NOTE: $IDP_ORIGIN not reachable — install the nginx vhost (command above) + reload"
fi

if [ "$DB_MODE" = fresh ]; then
	SLATE_NOTE="clean slate; DB recreated"
else
	SLATE_NOTE="reusing existing data; pass --fresh to wipe"
fi

cat <<EOF

============================================================
  prohibitorum dev forward-auth harness is UP ($SLATE_NOTE)
  IdP (Prohibitorum): $IDP_ORIGIN   (backend 127.0.0.1:$IDP_PORT)
  App (whoami):       $APP_ORIGIN   (backend 127.0.0.1:$WHOAMI_PORT)
  $NGINX_NOTE

  Admin enrollment — open in a browser to register a passkey:
    ${ENROLL_URL:-already enrolled (pass --fresh to reset)}

  Test the full forward-auth flow:
    1. Open $APP_ORIGIN in a browser.
    2. You should be redirected to $IDP_ORIGIN to log in.
    3. After logging in you should land back on $APP_ORIGIN
       and see the Remote-* headers echoed, e.g.:
         Remote-User:   alice
         Remote-Name:   Alice Example
         Remote-Email:  alice@example.test
         Remote-Groups: staff,admin

  Sign out:
    https://$FA_APP_HOST/.prohibitorum-forward-auth/sign_out

  Logs: $IDP_LOG / $WHOAMI_LOG   |   Ctrl-C stops all.
============================================================
EOF

tail -n +1 -f "$IDP_LOG" "$WHOAMI_LOG" &
TAIL_PID=$!
wait "$IDP_PID" "$WHOAMI_PID"
