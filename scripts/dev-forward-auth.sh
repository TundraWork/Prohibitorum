#!/usr/bin/env bash
# Bring up a protected whoami app behind a Traefik TLS front, wired to
# Prohibitorum forward-auth, for manual end-to-end browser-flow testing.
#
# Traefik is required (the front proxy must forward a non-2xx verify response):
# forward-auth's verify endpoint answers an unauthenticated request with a 302
# into the OIDC login. Traefik's ForwardAuth middleware forwards that 302
# straight to the browser. (A subrequest-auth proxy that only treats 2xx/401/403
# specially turns the 302 into an internal 500, so it cannot bootstrap login.)
# The canonical Traefik config this mirrors is documented in docs/forward-auth.md.
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
#   browser → https://FA_APP_HOST/  → Traefik → forward-auth/verify (Prohibitorum)
#   → login on https://FA_IDP_HOST/ → callback on FA_APP_HOST → 200 + Remote-*
#   → Traefik → whoami (127.0.0.1:FA_WHOAMI_PORT) → text/plain Remote-* dump
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
mkdir -p .dev/traefik .dev/logs

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
# TLS cert + key for the Traefik HTTPS entrypoint (must cover both hostnames).
FA_TLS_CERT=/etc/traefik/ssl.d/wildcard.cer
FA_TLS_KEY=/etc/traefik/ssl.d/wildcard.key
# Optional overrides:
FA_IDP_BACKEND_PORT=18090
FA_WHOAMI_PORT=18091
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
FA_CLIENT="${FA_CLIENT_ID:-dev-fa}"
IDP_ORIGIN="https://$FA_IDP_HOST"
APP_ORIGIN="https://$FA_APP_HOST"
VERIFY_URL="$IDP_ORIGIN/api/prohibitorum/forward-auth/verify"

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

# --- 6. generate Traefik config (static + dynamic) ----------------------------
# Mirrors the canonical config in docs/forward-auth.md:
#   * an HTTPS entryPoint whose forwardedHeaders.trustedIPs trusts the loopback
#     range, so Traefik OVERWRITES (and trusts) the X-Forwarded-* headers;
#   * a forwardAuth middleware → Prohibitorum's verify endpoint, with
#     trustForwardHeader: true + authResponseHeaders for the Remote-* identity;
#   * a router for the app host → whoami (gated by the middleware);
#   * a router for Host && PathPrefix(/.prohibitorum-forward-auth) → Prohibitorum
#     (NOT gated — this is where the OIDC callback + sign_out land).
TRAEFIK_DIR=".dev/traefik"
TRAEFIK_STATIC="$TRAEFIK_DIR/traefik.yml"
TRAEFIK_DYNAMIC="$TRAEFIK_DIR/dynamic.yml"

# Static config: entrypoint + TLS + file provider. trustedIPs covers loopback so
# the harness's own forwarded headers are trusted (docs §3 security guidance).
cat >"$TRAEFIK_STATIC" <<EOF
# Generated by scripts/dev-forward-auth.sh — do not commit; re-run regenerates it.
entryPoints:
  websecure:
    address: ":443"
    forwardedHeaders:
      trustedIPs:
        - "127.0.0.1/32"
        - "::1/128"

providers:
  file:
    filename: "$ROOT/$TRAEFIK_DYNAMIC"
    watch: true

log:
  level: INFO

api:
  dashboard: false
EOF

# Dynamic config: forwardAuth middleware + two routers + the backend services.
cat >"$TRAEFIK_DYNAMIC" <<EOF
# Generated by scripts/dev-forward-auth.sh — do not commit; re-run regenerates it.
tls:
  certificates:
    - certFile: "$FA_TLS_CERT"
      keyFile: "$FA_TLS_KEY"

http:
  middlewares:
    prohibitorum-forward-auth:
      forwardAuth:
        address: "$VERIFY_URL"
        trustForwardHeader: true
        authResponseHeaders:
          - Remote-User
          - Remote-Name
          - Remote-Email
          - Remote-Groups

  routers:
    # The protected whoami app — gated by the forward-auth middleware.
    fa-app:
      rule: "Host(\`$FA_APP_HOST\`)"
      entryPoints: ["websecure"]
      middlewares: ["prohibitorum-forward-auth"]
      service: whoami
      tls: {}

    # The per-domain auth/callback + sign_out path — routed to Prohibitorum, NOT
    # the app, and NOT gated by the forward-auth middleware. This is where the
    # OIDC callback lands so the per-domain cookie is scoped to $FA_APP_HOST.
    fa-app-forward-auth:
      rule: "Host(\`$FA_APP_HOST\`) && PathPrefix(\`/.prohibitorum-forward-auth\`)"
      entryPoints: ["websecure"]
      service: prohibitorum
      tls: {}

    # The Prohibitorum IdP (login UI, OIDC endpoints) on its own host.
    fa-idp:
      rule: "Host(\`$FA_IDP_HOST\`)"
      entryPoints: ["websecure"]
      service: prohibitorum
      tls: {}

  services:
    whoami:
      loadBalancer:
        servers:
          - url: "http://127.0.0.1:$WHOAMI_PORT"
    prohibitorum:
      loadBalancer:
        servers:
          - url: "http://127.0.0.1:$IDP_PORT"
EOF
echo
echo "Traefik config generated:"
echo "  static:  $TRAEFIK_STATIC"
echo "  dynamic: $TRAEFIK_DYNAMIC"

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

# --- 9. start Traefik (best-effort) -------------------------------------------
# If traefik is on PATH we run it pointed at the generated static config and add
# it to the cleanup trap. If it isn't installed we DON'T fail: the backends + DB
# seed still come up so the operator can run Traefik themselves.
TRAEFIK_LOG=".dev/logs/fa-traefik.log"
TRAEFIK_PID=""
TRAEFIK_NOTE=""
if command -v traefik >/dev/null 2>&1; then
	traefik --configFile="$TRAEFIK_STATIC" >"$TRAEFIK_LOG" 2>&1 &
	TRAEFIK_PID=$!
	TRAEFIK_NOTE="Traefik started (log: $TRAEFIK_LOG)"
else
	TRAEFIK_NOTE="Traefik NOT found on PATH — start it yourself:
      traefik --configFile=$TRAEFIK_STATIC
    (install Traefik: https://doc.traefik.io/traefik/getting-started/install-traefik/)"
fi

TAIL_PID=""
cleanup() { kill "$IDP_PID" "$WHOAMI_PID" ${TRAEFIK_PID:+"$TRAEFIK_PID"} ${TAIL_PID:+"$TAIL_PID"} 2>/dev/null || true; rm -rf "$BIN_DIR"; }
trap cleanup INT TERM EXIT

# --- 10. wait for backends, probe the Traefik front, banner -------------------
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

# Probe the TLS front (cert may be self-signed in dev, so -k).
if curl -skf "$IDP_ORIGIN/.well-known/openid-configuration" >/dev/null 2>&1; then
	FRONT_NOTE="Traefik is routing $IDP_ORIGIN"
else
	FRONT_NOTE="NOTE: $IDP_ORIGIN not reachable yet — $TRAEFIK_NOTE"
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
  Front proxy: Traefik   static=$TRAEFIK_STATIC
  $FRONT_NOTE

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
