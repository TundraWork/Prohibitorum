# Dev federation harness — two wired prohibitorum instances for manual testing

**Date:** 2026-06-17
**Status:** Design (approved in brainstorming)

> **Privacy note.** The operator's real hostnames and TLS cert paths are
> deployment-specific and **must never be committed**. They live only in the
> gitignored `.dev/` tree (see "Local configuration" below). Every committed
> artifact — this spec, `scripts/dev-federation.sh`, the Go command,
> `mise.toml`, `TOOLING.md` — uses **placeholder** names
> (`idp-a.example.test` / `idp-b.example.test`) and reads the real values from
> the local config at runtime.

## Goal

One developer command (`mise run dev:federation`) that brings up **two
prohibitorum instances** — an **upstream** IdP/OP and a **downstream** RP that
federates to it — fully wired so an operator can manually exercise the
important end-to-end flows in a real browser:

- the OIDC OP path on the upstream (`/oauth/authorize` → login → **consent** →
  `/oauth/token` → `/oauth/userinfo`), driven by the downstream acting as a real
  relying party;
- the **federated-login enrollment + identity-confirmation** path on the
  downstream (`auto_provision` → `/welcome` confirm/decline; and an
  `invite_only` variant);
- the OP path driven **directly** by a generic test relying party (for
  authorize/token/userinfo without going through federation), against either
  instance;
- and, because the redirect URIs are registered, **link-identity** and
  **sudo-via-federation** as well.

Reuse the existing `dev-seed` command as much as possible.

## Why two instances (not one)

Federation is a two-party protocol; a single instance cannot be both the OP and
a distinct RP with an independent user session. Two instances also give the
operator **independent simultaneous browser sessions** (logged into the upstream
*and* the downstream at once), which is required to observe the full
cross-instance flow.

### Session isolation requires distinct hostnames

The dev session cookie is the **host-only** `prohibitorum_session` (or
`__Host-prohibitorum_session` over https). **Cookies ignore port**, so two
instances on the same hostname (`localhost:8080` / `localhost:8081`) share one
cookie jar and clobber each other's session. Independent sessions therefore
require **distinct hostnames**, and those hostnames must:

- resolve to loopback in **both** the browser and Go's federation client;
- be valid **WebAuthn RP IDs** in a **secure context** (passkey enrollment is
  central to the test) — which rules out IP literals and, over plain http,
  any non-`localhost` domain.

This is solved with two real DNS names the operator controls, both A-records to
`127.0.0.1`, served over TLS by the host's nginx. Topology (placeholder names;
real values from the local config):

| | Upstream (a) | Downstream (b) |
| --- | --- | --- |
| Public origin | `https://idp-a.example.test` | `https://idp-b.example.test` |
| nginx → backend (loopback http) | `127.0.0.1:18080` | `127.0.0.1:18081` |
| Database (same dev cluster) | `prohibitorum_upstream` | `prohibitorum_downstream` |
| WebAuthn RP ID | `idp-a.example.test` | `idp-b.example.test` |
| `PROHIBITORUM_TRUST_PROXY` | `true` | `true` |
| `PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK` | — | `true` |

The configured names resolve to `127.0.0.1` via real DNS A-records the operator
controls, so they resolve under the pure-Go resolver too (verified for the
operator's real names) and the federation client — which only ever **dials** the
upstream origin — works regardless of `CGO_ENABLED`. The downstream-dial of a
loopback address is permitted by `ALLOW_PRIVATE_NETWORK=true`; the real wildcard
cert chains to a public root in the system pool, so Go's TLS validation passes.

The instances share one DEK (the existing `.dev/encryption-key`); realism does
not matter for a local harness and each instance hashes/seals secrets
independently anyway. The two federation DBs are separate from the operator's
existing `prohibitorum_dev`, so this never touches current dev data.

## Local configuration (never committed to git)

Deployment-specific values live in a gitignored env file the orchestrator
**sources**, `.dev/dev-federation.env` (`/.dev` is already in `.gitignore`):

```sh
# .dev/dev-federation.env — local only, never committed
DEV_FED_UPSTREAM_HOST=idp-a.example.test          # your real upstream hostname
DEV_FED_DOWNSTREAM_HOST=idp-b.example.test        # your real downstream hostname
DEV_FED_TLS_CERT=/etc/nginx/ssl.d/wildcard.cer    # cert nginx serves (fullchain)
DEV_FED_TLS_KEY=/etc/nginx/ssl.d/wildcard.key
# optional overrides (sensible defaults baked into the script):
# DEV_FED_UPSTREAM_BACKEND_PORT=18080
# DEV_FED_DOWNSTREAM_BACKEND_PORT=18081
# DEV_FED_NGINX_DIR=/etc/nginx/hosts.d
```

`DEV_FED_UPSTREAM_HOST` / `DEV_FED_DOWNSTREAM_HOST` / `DEV_FED_TLS_CERT` /
`DEV_FED_TLS_KEY` are **required** (no defaults — the script must not embed real
infra). On first run, if the file is absent the orchestrator **writes a
commented template** (with the `example.test` placeholders above) to
`.dev/dev-federation.env` and exits, telling the operator to fill it in and
re-run. The generated nginx vhost also lands under `.dev/` (see below), so no
real hostname or cert path ever reaches a tracked file.

## TLS / nginx

nginx (already running on the host, with an included config dir and the
wildcard cert installed where nginx — running as root — can read it) **terminates
TLS** and reverse-proxies each hostname to its loopback http backend. The
harness therefore needs **no TLS code and no cert handling** — backends run plain
http on loopback with `TRUST_PROXY=true`, so they emit Secure `__Host-` cookies
and read scheme/IP from `X-Forwarded-*`.

The orchestrator **generates one vhost file** containing **both** server blocks
into `.dev/nginx/prohibitorum-federation.conf` (gitignored; filled in from the
local config so the hostnames/cert/ports stay in sync) and prints a one-time
install command. Shape of the generated file (placeholder values shown):

```nginx
# prohibitorum-federation.conf — generated by scripts/dev-federation.sh from
# .dev/dev-federation.env. Do not hand-edit or commit; re-run regenerates it.

# --- upstream IdP / OP ---------------------------------------------------
server {
    listen 80; listen [::]:80;
    server_name idp-a.example.test;            # = $DEV_FED_UPSTREAM_HOST
    return 301 https://$host$request_uri;
}
server {
    listen 443 ssl; listen [::]:443 ssl; http2 on;
    server_name idp-a.example.test;
    ssl_certificate     /etc/nginx/ssl.d/wildcard.cer;   # = $DEV_FED_TLS_CERT
    ssl_certificate_key /etc/nginx/ssl.d/wildcard.key;   # = $DEV_FED_TLS_KEY
    location / {
        proxy_pass         http://127.0.0.1:18080;
        proxy_http_version 1.1;
        proxy_set_header   Host              $host;
        proxy_set_header   X-Real-IP         $remote_addr;
        proxy_set_header   X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto $scheme;
        proxy_set_header   X-Forwarded-Host  $host;
    }
}

# --- downstream IdP / RP -------------------------------------------------
server {
    listen 80; listen [::]:80;
    server_name idp-b.example.test;            # = $DEV_FED_DOWNSTREAM_HOST
    return 301 https://$host$request_uri;
}
server {
    listen 443 ssl; listen [::]:443 ssl; http2 on;
    server_name idp-b.example.test;
    ssl_certificate     /etc/nginx/ssl.d/wildcard.cer;
    ssl_certificate_key /etc/nginx/ssl.d/wildcard.key;
    location / {
        proxy_pass         http://127.0.0.1:18081;
        proxy_http_version 1.1;
        proxy_set_header   Host              $host;
        proxy_set_header   X-Real-IP         $remote_addr;
        proxy_set_header   X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto $scheme;
        proxy_set_header   X-Forwarded-Host  $host;
    }
}
```

The two `listen 443 ssl` blocks share the host's existing cert and must **not**
set `default_server` (another vhost on the box may already own it). Install
command printed by the harness (`$DEV_FED_NGINX_DIR` defaults to the host's
included dir):

```sh
sudo cp .dev/nginx/prohibitorum-federation.conf "$DEV_FED_NGINX_DIR"/ \
  && sudo nginx -t && sudo systemctl reload nginx
```

After launching the backends the orchestrator probes
`https://$DEV_FED_UPSTREAM_HOST/.well-known/openid-configuration`; if it is
unreachable, it prints the install command as a reminder rather than failing.

## Components

### 1. `cmd/prohibitorum/dev_federation.go` — idempotent two-DB wiring

A new dev-only cobra subcommand `dev-federation`, structured like `dev_seed.go`
(shares its loopback guard; see §4). It connects to **both** databases in one
process and is **fully idempotent** (safe to re-run). Reuses existing library
functions — no new crypto or DB code paths. It is fed hostnames/origins by the
orchestrator as flags, so it embeds no infra.

Flags / env:

- `--upstream-db`, `--downstream-db` — Postgres DSNs.
- `--upstream-origin`, `--downstream-origin` — the https public origins.
- DEK from env (`mustCurrentDEK()`), i.e. the shared `.dev/encryption-key`.

Steps (each idempotent):

1. **Guard:** refuse unless **both** origins resolve entirely to loopback (§4).
2. **Migrate** both DBs (`migrations.UpWithResult`).
3. **Active signing key on each DB:** if `GetActiveSigningKey` returns
   `pgx.ErrNoRows`, `oidc.InsertPendingKey` + `oidc.ActivateSigningKey` (sealed
   with the shared DEK; grace from `config.SAML.MetadataRotationGrace`).
   Without this the OP cannot mint id_tokens and federation token-exchange
   fails. Skip if a key already exists.
4. **Upstream OIDC client `downstream-federation`** (create-or-rotate): if it
   exists, `UpdateOIDCClient` (refresh fields) + `RotateClientSecret` (returns a
   fresh plaintext); else `oidc.BuildClientParams` + `InsertOIDCClient`. Either
   path ends with a **known plaintext secret**. Config:
   - `require_consent = true` (so the consent screen shows in the federation path);
   - redirect URIs (downstream origin):
     - `…/api/prohibitorum/auth/federation/upstream/callback`
     - `…/api/prohibitorum/auth/federation/upstream-invite/callback`
     - `…/api/prohibitorum/me/identities/link/upstream/callback`
     - `…/api/prohibitorum/me/identities/link/upstream-invite/callback`
     - `…/api/prohibitorum/me/sudo/federation/callback`
   - scopes `openid profile email`; post-logout `…/`.
5. **Downstream upstream_idp rows** (UPSERT, never delete — `account_identity`
   FK may reference them after testing). For slugs `upstream` (`auto_provision`)
   and `upstream-invite` (`invite_only`): if the slug exists,
   `UpdateUpstreamIDPConfig`; else `InsertUpstreamIDP` (placeholder secret).
   Then reseal the secret every run: `fedoidc.EncryptClientSecret(dek,
   fedSecret, rowID, keyVer)` + `UpdateUpstreamIDPSecret`. Common fields:
   `issuer_url = upstream-origin`, `client_id = downstream-federation`,
   secret = the plaintext from step 4, scopes `openid email profile`,
   `username_claim=preferred_username`, `display_name_claim=name`,
   `email_claim=email`, `picture_claim=picture`,
   `require_verified_email=false`, `allowed_domains=[]`. (By design, an
   `invite_only` IdP **rejects a bare login button** with `invite_required`;
   it is reachable only via a bound invite — see step 6.)
6. **Federation-bound invitation on the downstream** (the only entry point for
   the `invite_only` path): mint a fresh `IntentInvite` enrollment whose
   `EnrollmentTemplate.ExpectedUpstreamIDPSlug = "upstream-invite"`
   (`enrollment.IssueEnrollment`), and print its `…-b…/enroll/<token>` URL.
   `dev-seed`'s plain (non-federated) invites cannot drive federation
   (`invite_not_federated`), so this binding is required. Minted fresh each run
   (single-use tokens can't be reprinted); a few extra pending invites in dev
   are harmless.
7. **Test RP `test-rp` on each DB** (create-or-rotate, like step 4):
   confidential, `require_consent=true`, scopes `openid profile email`,
   redirect URI `http://127.0.0.1:9876/callback` (the operator reads `code` from
   the browser address bar after the redirect fails to connect — a standard
   manual-testing trick, fully offline).
8. **Print a wiring summary** including, per instance, a ready **PKCE recipe**:
   a freshly generated S256 `code_verifier`/`code_challenge`, the authorize URL
   to open, and the `curl` token + userinfo commands (with the `test-rp` secret
   for that instance). Fresh pair per run → internally consistent.

### 2. `scripts/dev-federation.sh` — orchestrator (process lifecycle)

Runs as the normal user. Single foreground command; `Ctrl-C` stops everything.
Contains **no real hostnames or cert paths** — it sources them from
`.dev/dev-federation.env`.

1. **Local config + preflight:** source `.dev/dev-federation.env` (if absent,
   write the commented template and exit with instructions); assert the four
   required vars are set and both hostnames resolve to loopback; ensure
   `.dev/encryption-key` exists (generate like `dev-env.sh` if not).
2. **Ensure DBs:** verify the dev Postgres is reachable (instruct
   `mise run db:start` if not); `createdb` `prohibitorum_upstream` /
   `prohibitorum_downstream` if missing (guarded by a `pg_database` check, like
   `dev-db.sh`). `--fresh` drops + recreates both first.
3. **Build once:** `go build -tags nodynamic -o "$BIN" ./cmd/prohibitorum`
   (matches the smoke/release build; one binary reused for every subcommand and
   both servers).
4. **Per instance** (upstream then downstream), with env scoped to that
   instance (DSN, shared DEK, https `PUBLIC_ORIGIN` built from the local config):
   run `dev-seed`, then `enroll-admin` — catching the "admin already exists"
   exit and downgrading it to a "sign in at `<origin>`" note.
5. **Wire:** run `dev-federation` with both DSNs + both origins.
6. **nginx:** generate the single `.dev/nginx/prohibitorum-federation.conf`
   (both server blocks, filled in from the local config) and print the
   install/reload command.
7. **Launch backends** (backgrounded), env per instance:
   - upstream: `…_DATABASE_URL=…upstream`, `…_PUBLIC_ORIGIN=https://$UPSTREAM_HOST`,
     `…_HOST=127.0.0.1`, `…_PORT=18080`, `…_TRUST_PROXY=true`.
   - downstream: `…downstream`, `https://$DOWNSTREAM_HOST`, `…_PORT=18081`,
     `…_TRUST_PROXY=true`, `…_FEDERATION_ALLOW_PRIVATE_NETWORK=true`.
   Each server auto-migrates on boot. Logs tee to `.dev/logs/{upstream,downstream}.log`.
8. **Probe + banner:** wait for both backends' discovery endpoints; probe the
   public https origins (remind about nginx if unreachable); print the
   manual-test banner (URLs, admin-enroll links, the click-paths below, and the
   test-RP recipe from `dev-federation`).
9. `trap` `INT`/`TERM`/`EXIT` → kill both backends; `tail -f` both logs.

### 3. mise task `dev:federation`

```toml
[tasks."dev:federation"]
description = "DEV: bring up two prohibitorum instances (upstream + downstream IdP) wired for OIDC federation behind nginx TLS, for manual end-to-end testing. Reads local hostnames/cert from .dev/dev-federation.env (template written on first run). Start the DB with `mise run db:start`."
run = "exec scripts/dev-federation.sh \"$@\""
```

### 4. Loopback-guard change (shared)

`dev_seed.go`'s guard currently accepts only `localhost`/`127.0.0.1`/`::1`. Add
a shared helper `isLoopbackOrigin(origin) bool` that returns true when the
hostname is in that set, **or** is an IP literal that `IsLoopback()`, **or**
resolves (`net.LookupIP`) to a non-empty set whose members are **all**
loopback. Fail-closed (resolution error or any non-loopback IP → false). This
lets the operator's loopback-pinned DNS names pass while still refusing genuine
public origins. Used by both `dev-seed` and `dev-federation`.

### 5. Docs

A `dev:federation` section in `TOOLING.md` (topology, the `.dev/dev-federation.env`
local config, the one-time nginx step, the manual-test paths) and a one-line
pointer in `README.md` — both using the `example.test` placeholders only.

## Idempotency / re-run semantics

Re-runnable without `--fresh`: DBs and signing keys are ensure-if-missing;
OIDC clients are create-or-rotate; upstream_idp rows are UPSERT-and-reseal;
`dev-seed` is already idempotent; `enroll-admin`'s "admin exists" is caught.
Because the fed-client secret is rotated and immediately resealed into the
downstream rows in the same run, the two sides stay consistent every run.
`--fresh` drops + recreates both DBs for a clean slate (loses enrolled
passkeys, so it is off by default).

## Manual-test paths this unlocks

- **Federated login (auto_provision):** open `https://idp-b.example.test`
  → click **Upstream** → authenticate + **consent** on
  `https://idp-a.example.test` → callback → **`/welcome`**
  confirm/decline → downstream session. Exercises the OP end-to-end plus
  enrollment and identity confirmation.
- **Invite-gated federation (invite_only):** open the **federation-bound invite
  URL** printed by `dev-federation` (`…-b…/enroll/<token>`) → choose the
  **Upstream (invite)** federated option → authenticate on the upstream → the
  callback atomically redeems the invite + links the identity → session.
  (Clicking the `invite_only` IdP as a *bare login* is rejected with
  `invite_required` — that gating is the behavior under test.)
- **Direct OP test:** paste the printed `test-rp` authorize URL at either
  instance → consent → read `code` from the address bar → run the printed
  token and userinfo `curl`s.
- **Link identity / sudo-via-federation:** from a signed-in downstream session,
  using the registered link + sudo redirect URIs.

## Verification plan

- `go build -tags nodynamic ./...`, `go vet ./...`, `go test ./...`; targeted
  unit test for `isLoopbackOrigin`.
- Actually run `mise run dev:federation`: confirm both backends boot with an
  active signing key, the wiring summary is correct, cross-instance discovery
  resolves over https, and a re-run is clean (no duplicate keys, consistent
  secrets).
- Browser passkey steps are inherently manual; the banner prints exact
  click-paths and the operator confirms the federation + consent + `/welcome`
  round-trip.
- Confirm `git status` shows no tracked file contains the real hostnames or cert
  paths (they exist only under the gitignored `.dev/`).

## Out of scope

- Changes to production server code (no in-process TLS; nginx terminates).
- Automating the privileged nginx install/reload (printed for the operator).
- SAML flows (federation harness is OIDC-focused).
