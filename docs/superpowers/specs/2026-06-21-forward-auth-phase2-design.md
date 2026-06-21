# Forward-Auth (Phase 2) — Design

Date: 2026-06-21
Status: Approved (brainstorming) — pending spec review

Builds on **Phase 1** (`docs/superpowers/specs/2026-06-21-forward-auth-phase1-design.md`,
shipped on `master`). Phase 1 delivered the native Traefik ForwardAuth verify +
callback handlers, the `oidc_client.forward_auth_enabled`/`forward_auth_host`
schema, the per-domain cookie + KV `fa:session`/`fa:state` machinery, the
`forward-auth-app create` CLI, and `docs/forward-auth.md`. Phase 2 adds the
operator-facing surface that Phase 1 deferred.

## Problem

A forward-auth service today can only be registered and managed from the CLI.
There is no dashboard UI to create/list/edit/delete forward-auth services or
manage their RBAC, no way to sign out of a forward-auth session, and no local
harness to exercise the full multi-domain browser flow.

## Goals

- **2A — Admin UI:** register/list/edit/delete forward-auth services and manage
  their per-service RBAC from the dashboard, presented as their own section.
- **2C — Sign-out:** a sign-out path on the protected domain that clears the
  per-domain forward-auth cookie/session and ends the Prohibitorum SSO session.
- **2B — Dev harness:** a local multi-domain harness to exercise the full
  browser flow (protected app behind Traefik → verify → callback → 200).

## Non-goals

- No new authorization model — per-service RBAC reuses the existing
  `oidc_client_access` + `access_restricted` endpoints unchanged.
- Not a true "kill every app session instantly" global logout (see §2C
  limitation — residual per-domain cookies expire at their TTL).
- No changes to the Phase 1 verify/callback contract.

## Key decisions (from brainstorming)

1. **Exclusive listing.** A forward-auth app is an `oidc_client` with
   `forward_auth_enabled=true`. It is shown **only** under the new Forward-auth
   section and **excluded** from the OIDC-applications list/detail, so the same
   app never appears in two places and its FA contract (the fixed callback
   `redirect_uri`) can't be broken by editing it from the OIDC side.
2. **Sign-out = per-app + SSO logout** (two-hop bounce; see §2C).
3. **Include the host-substituted Traefik config snippet** on the detail page.
4. **Dev harness built last** in the cycle (it is not gate-verifiable in CI /
   the controller environment — it needs real TLS/Traefik and local hostnames).

---

## §2A — Admin UI for forward-auth services

### Backend

New file `pkg/server/handle_admin_forward_auth_apps.go`, mirroring
`handle_admin_oidc_clients.go` (typed huma handlers for reads; raw
sudo-gated handlers for mutations; audit via `audit.FactorOIDCClient`).

| Route | Auth | Handler / behavior |
|---|---|---|
| `GET /api/prohibitorum/forward-auth-apps` | admin (no sudo) | `handleListForwardAuthApps` — new query `ListForwardAuthClients` (`WHERE forward_auth_enabled`). Returns `[]ForwardAuthAppView`. |
| `GET …/forward-auth-apps/{clientId}` | admin (no sudo) | `handleGetForwardAuthApp` — new query `GetForwardAuthAppByID` (selects FA fields, guards `forward_auth_enabled = true`; `pgx.ErrNoRows` → `client_not_found`). |
| `POST …/forward-auth-apps` | admin + sudo | `handleCreateForwardAuthAppHTTP` — body `{clientId, host, displayName?}`; calls the shared `RegisterForwardAuthApp` helper. Unique-host violation → `client_already_exists` (409). |
| `PUT …/forward-auth-apps/{clientId}` | admin + sudo | `handleUpdateForwardAuthAppHTTP` — body `{displayName, host}`; updates display-name + host **and** re-derives `redirect_uris=[https://<host>/.prohibitorum-forward-auth/callback]` in one statement (new query `UpdateForwardAuthApp`). Unique-host violation → 409. |
| `POST …/forward-auth-apps/set-disabled` | admin (no sudo) | `handleSetForwardAuthAppDisabledHTTP` — mirrors the OIDC set-disabled (reuses `SetOIDCClientDisabled`). |
| `POST …/forward-auth-apps/delete` | admin + sudo | `handleDeleteForwardAuthAppHTTP` — reuses `DeleteOIDCClient` (drops the backing client + its access rows by FK). |

Registration in `pkg/server/server.go` alongside the OIDC-application routes:
reads via `registerOpHTTP(..., admin, ...)`, sudo mutations via
`s.registerSudoOpHTTP(..., admin, ...)`, `set-disabled` via `registerOpHTTP(...,
admin, ...)` (matching the OIDC set-disabled precedent).

**Shared create helper.** Factor the body of the CLI's `forward-auth-app create`
(`cmd/prohibitorum/main.go`) into an exported
`oidc.RegisterForwardAuthApp(ctx, q *db.Queries, clientID, host, displayName string) (db.OidcClient, error)`
in `pkg/protocol/oidc/forward_auth.go`: builds a public PKCE client
(`BuildClientParams`, `require_consent=false`, scopes `openid email groups`,
`redirect_uri=https://<host>/.prohibitorum-forward-auth/callback`), inserts it,
then `SetForwardAuthConfig(clientID, true, host)`. The CLI and the HTTP handler
both call it (single source of truth for the FA client shape).

**RBAC reuse.** No new access endpoints. The detail page drives the existing
`/oidc-applications/{clientId}/access/{,set-restricted,grant,revoke}` endpoints
(they operate on the backing `client_id` regardless of the FA flag) via the
existing `AppAccessCard kind="oidc"` component — unchanged.

**Exclusive-listing chokepoints** (enforce decision 1):
- Add `WHERE NOT forward_auth_enabled` to the admin `ListOIDCClients` query (used
  only by `handleListOIDCApplications` — verify during planning; if shared
  elsewhere, add a new `ListNonForwardAuthOIDCClients` query instead).
- Guard `handleGetOIDCApplication` to return `client_not_found` when the loaded
  row has `forward_auth_enabled=true` (reads the flag off the `GetOIDCClientAny`
  row; that row already carries the column after migration 018 + sqlc regen).
- Guard the OIDC `PUT` and `rotate-secret` handlers the same way (the FA contract
  must not be mutable from the OIDC side; both already pre-load or can pre-load
  the client). The shared `/oidc-applications/{clientId}/access/*` endpoints are
  intentionally **not** guarded — the FA detail page depends on them.
- `set-disabled` and `delete` on the OIDC side are left unguarded (harmless:
  delete drops the same backing client the FA delete would; disable is also
  offered on the FA detail page). Noted explicitly so it isn't read as an
  oversight.

**Contract.** New `contract.ForwardAuthAppView{ ClientID, DisplayName,
ForwardAuthHost, AccessRestricted, Disabled, CreatedAt }` in `pkg/contract`.

### Frontend

Mirroring `AdminOidcClientsView.vue` / `AdminOidcClientDetailView.vue`.

- **`dashboard/src/pages/admin/AdminForwardAuthAppsView.vue`** — list (table:
  `displayName · clientId`, `forwardAuthHost`, status badge) + inline create card
  with **only** client-id, host, display-name fields (everything else is derived
  server-side). `TableSkeleton` loading state + `EmptyState`. Create goes through
  `withSudo`.
- **`dashboard/src/pages/admin/AdminForwardAuthAppDetailView.vue`**:
  - Config card: client-id (read-only, `font-mono`), display-name (editable),
    host (editable). Save via `withSudo` PUT.
  - **Traefik snippet card**: a `CodeField` with a ready-to-paste, host-substituted
    Traefik dynamic-config snippet (the `forwardAuth` middleware whose `address`
    is `${window.location.origin}/api/prohibitorum/forward-auth/verify`, the
    per-domain router for `/.prohibitorum-forward-auth/*` + the callback/sign_out
    routing, and `authResponseHeaders`). The IdP origin is taken from
    `window.location.origin` (the dashboard is served from the Prohibitorum
    domain); the protected host is the app's `forwardAuthHost`.
  - `<AppAccessCard kind="oidc" :app-id="clientId" />` — RBAC, reused unchanged.
  - Danger zone (last): disable/enable toggle (via `set-disabled`) + delete
    (ConfirmDialog). **No** rotate-secret (FA clients are public).
- **Router** (`dashboard/src/router/index.ts`): two routes mirroring the OIDC
  pair — `/admin/forward-auth-apps` (`title.adminForwardAuthApps`) and
  `/admin/forward-auth-apps/:clientId` (`title.adminForwardAuthAppDetail`), both
  `meta.requiresAdmin`. Extend the `guard.test.ts` requiresAdmin list.
- **Sidebar** (`AppSidebar.vue`): add a "Forward auth" item to the existing
  `applicationItems` (Applications group), icon `Waypoints` (lucide), label
  `admin.nav.forwardAuthApps`.
- **i18n**: `title.adminForwardAuthApps` + `title.adminForwardAuthAppDetail`,
  `admin.nav.forwardAuthApps`, and an `admin.forwardAuth.*` block, in **both**
  `en.ts` and `zh.ts` (en/zh key parity is gate-checked). Mind the en.ts
  apostrophe hazard (grep-verify after edits) and `@`-escaping (`{'@'}`) for any
  literal `@` — guarded by `en.compile.test.ts`.

---

## §2C — Sign-out (per-app + SSO logout)

The Phase 1 cross-domain constraint applies: a request to
`https://<apphost>/.prohibitorum-forward-auth/sign_out` carries only app-domain
cookies — never the Prohibitorum session cookie (on the IdP domain). So sign-out
is a **two-hop bounce** (the Authentik model):

1. **`GET /.prohibitorum-forward-auth/sign_out`** — Provider method
   (`HandleForwardAuthSignOut`), routed on the protected domain like the
   callback. Reads the FA cookie; deletes its KV `fa:session`; clears the
   host-only FA cookie (expired Set-Cookie). Then `302` to
   `<issuer>/api/prohibitorum/forward-auth/sso-logout?rd=<scheme>://<apphost>/`
   (the `rd` host is the trusted `X-Forwarded-Host`).
2. **`GET /api/prohibitorum/forward-auth/sso-logout`** — server handler
   (`handleForwardAuthSSOLogoutHTTP`), public, on the IdP domain. Terminates the
   current Prohibitorum session (reuse the existing logout/session-termination
   path — verify in planning whether `handleLogoutHTTP` exposes a reusable
   function or the session manager is called directly) and clears the session
   cookie. **Open-redirect guard:** the `rd` host must resolve via
   `GetForwardAuthClientByHost` (i.e. be a registered FA host) — same fail-closed
   model as verify; otherwise redirect to a safe default (the SPA login/root).
   Then `302` to the validated `rd` (the app, now unauthenticated → bounces to
   login).

**Limitation (documented, accepted for v1):** killing the Prohibitorum SSO
session logs out the dashboard and *this* app and prevents future silent
re-auth, but FA cookies **already planted on other app domains** are independent
KV sessions keyed by random token (not indexed by account), so they persist
until their TTL (`ForwardAuth.SessionTTL`, default 1h) or the next live-RBAC
denial. True instant global revocation would require a per-account FA-session
index (extra KV bookkeeping); deferred unless needed. Documented in
`docs/forward-auth.md`.

**Docs:** add the `sign_out` route to the Traefik per-domain router section of
`docs/forward-auth.md` and to the detail-page snippet.

---

## §2B — Dev harness (built last)

Model on the existing `mise dev:federation` harness (`scripts/dev-federation.sh`
+ `cmd/prohibitorum/dev_federation.go` + nginx/TLS + gitignored
`.dev/dev-federation.env`; committed code uses `example.test` placeholders — see
memory `feedback_dev_federation_local_only`). Add:

- A front proxy (reuse the nginx pattern, or Traefik) terminating TLS for a
  second protected host, with the ForwardAuth middleware pointed at
  Prohibitorum's verify endpoint and the `/.prohibitorum-forward-auth/*` prefix
  routed to Prohibitorum.
- A static "whoami"-style backend on the protected host that echoes the
  `Remote-User/Name/Email/Groups` headers (so a successful 200 is visible).
- A seed step that runs `forward-auth-app create` for the protected host + grants
  the dev admin access.
- A `mise dev:forward-auth` task wrapping the script; local hostnames/cert paths
  read from a gitignored env file (template written on first run);
  `example.test` placeholders committed.

**Caveat:** not gate-verifiable in the controller environment (needs real
TLS/Traefik). It is a manual-testing aid; correctness is verified locally by the
operator. The script itself (shell/Go) is built and `shellcheck`/`go build`
clean.

---

## Testing & verification

- **Go unit tests** (`pkg/protocol/oidc/forward_auth_test.go` +
  `pkg/server/...`):
  - `sign_out` deletes the KV `fa:session` and clears the cookie; redirects to
    the sso-logout URL with a correct `rd`.
  - `sso-logout` terminates the session; accepts a registered `rd` host; rejects
    an unregistered/spoofed `rd` host (redirects to the safe default, never an
    arbitrary URL).
  - `RegisterForwardAuthApp` produces a public PKCE client with the exact
    callback `redirect_uri`, `require_consent=false`, and the FA flag/host set.
  - Admin handlers: list returns only FA apps; the OIDC list **excludes** FA
    apps; `GET`/`PUT`/`rotate-secret` on the OIDC side 404 for an FA client;
    create (incl. host-conflict 409), edit (host change re-derives redirect_uri),
    set-disabled, delete.
- **Frontend** vitest for the two views (list renders + create; detail edit,
  snippet renders host-substituted, delete confirm) + the full FE gate:
  `npm run test`, `npx vue-tsc -b`, `node scripts/check-contrast.mjs`, and a
  **dist rebuild + commit** (`npm run build` → commit `pkg/webui/dist`).
- **Runtime** (subagent-launched server on a free port; chromium `--dump-dom`
  for SPA DOM): register an FA app in the UI → it lists, and the OIDC list does
  not show it; curl the verify → authorize → callback → verify → sign_out →
  sso-logout sequence with simulated Traefik headers + the per-domain cookie.
- **Gate** per task: `go vet ./... && go build -tags nodynamic ./... && go test
  ./...`, plus the FE gate where UI changed. No `Co-Authored-By` trailer. Work
  continues on `master`.

## Task order

2A backend (queries + shared helper + handlers + routes + contract + OIDC
exclusion guards) → 2A frontend (views + route + sidebar + i18n) → 2C sign-out
(two endpoints + docs) → runtime-verify 2A+2C → 2B dev harness → final gate +
dist rebuild + commit.

## References

- Phase 1 spec: `docs/superpowers/specs/2026-06-21-forward-auth-phase1-design.md`
- Phase 1 handoff: `docs/superpowers/notes/2026-06-21-forward-auth-handoff.md`
- Operator guide: `docs/forward-auth.md`
- Core code: `pkg/protocol/oidc/forward_auth.go`, `pkg/server/server.go`,
  `cmd/prohibitorum/main.go` (`forward-auth-app`), migration
  `db/migrations/018_forward_auth.sql`
- Mirror templates: `pkg/server/handle_admin_oidc_clients.go`,
  `pkg/server/handle_admin_app_access.go`,
  `dashboard/src/pages/admin/AdminOidcClient{s,Detail}View.vue`,
  `dashboard/src/components/custom/AppAccessCard.vue`
- Dev-harness model: `scripts/dev-federation.sh`,
  `cmd/prohibitorum/dev_federation.go`, memory `feedback_dev_federation_local_only`
</content>
</invoke>
