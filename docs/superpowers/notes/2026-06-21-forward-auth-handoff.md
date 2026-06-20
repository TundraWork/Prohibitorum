# Forward-Auth — Handoff (Phase 1 DONE → Phase 2 to finish)

Date: 2026-06-21
Status: **Phase 1 shipped on `master` (gate-green, runtime-verified). Phase 2 not started.**

This note is self-contained: a fresh session should be able to finish forward-auth
(admin UI + dev harness + sign-out) from here without prior conversation context.

---

## How to resume (new session, first moves)

1. Read this note, then the **spec** `docs/superpowers/specs/2026-06-21-forward-auth-phase1-design.md`
   (the "Phase 2 (deferred)" section) and the **Phase 1 plan**
   `docs/superpowers/plans/2026-06-21-forward-auth-phase1.md`.
2. Skim the operator guide `docs/forward-auth.md` (the live contract) and the
   memory file `…/memory/project_current_state.md` (latest entry = forward-auth + the repo state).
3. Phase 2 is mostly a *frontend admin UI* on a settled backend, plus a dev harness and a
   small sign-out backend bit. The design is largely decided (below), so you can go
   **brainstorm (short) → writing-plans → subagent-driven-development**. The admin-UI
   shape closely mirrors the existing OIDC-client admin pages — reuse them.

### Repo / workflow state (important)
- Branch **`master`**; **~92 commits ahead of `origin/master`, ALL UNPUSHED**. Work continues on master (the established per-cycle pattern here). Never rewrite at/below `origin/master`.
- Gate: `go vet ./... && go build -tags nodynamic ./... && go test ./...`; **frontend** (Phase 2 has UI) adds `cd dashboard && npm run test`, `npx vue-tsc -b`, `node scripts/check-contrast.mjs`, and a **dist rebuild+commit** (`cd dashboard && npm run build` then commit `pkg/webui/dist` — the `ci:frontend` gate fails on stale dist).
- **NEVER** add a `Co-Authored-By` trailer (firm user rule).
- **Runtime verification:** the controller's own Bash servers get killed (exit 144) — launch the verify server from a **subagent** (`setsid /tmp/bin >log 2>&1 </dev/null &`). `mise run db:start` is broken; podman Postgres is already up on **:5432** (DB `prohibitorum_dev`, user/pass `prohibitorum`); `source scripts/dev-env.sh` for the DSN. Playwright not installed but `/usr/bin/chromium --headless=new --no-sandbox --dump-dom <url>` works for SPA DOM checks.
- **Stale LSP:** IDE diagnostics go stale after sqlc-gen / new files — trust `go build`/`go test`, not the panel.
- sqlc: query source `db/queries/*.sql`, regen `sqlc generate` from repo root (sqlc 1.30.0 via mise).

---

## Phase 1 — what already exists (the foundation Phase 2 builds on)

**Model:** native Traefik ForwardAuth (Authentik embedded-outpost style), reusing the OIDC OP.
A protected service **is a normal `oidc_client`** flagged for forward-auth; per-service RBAC
reuses `oidc_client_access` + `access_restricted` (no separate authz system).

**Shipped (commits `296effe`→`d94e162`):**
- **Schema** (migration `018_forward_auth.sql`): `oidc_client.forward_auth_enabled boolean` + `forward_auth_host text` + partial unique index on `forward_auth_host WHERE forward_auth_enabled`.
- **Queries** (`db/queries/oidc.sql` → `pkg/db/oidc.sql.go`): `GetForwardAuthClientByHost(host) → {ClientID, DisplayName, AccessRestricted, Disabled}`; `SetForwardAuthConfig(ClientID, ForwardAuthEnabled, ForwardAuthHost)`.
- **Config:** `configx.ForwardAuthConfig{ SessionTTL }` (default 1h).
- **Handlers** (`pkg/protocol/oidc/forward_auth.go`, methods on `*Provider`):
  - `HandleForwardAuthVerify` — the ForwardAuth target. `200`+identity headers / `302` into `/oauth/authorize` (PKCE S256 + single-use KV state) / `403` unknown host. Live RBAC re-check via `IsAccountAuthorizedForOIDCClient`.
  - `HandleForwardAuthCallback` — in-process `consumeCode` + PKCE/client/redirect binding → mint per-domain KV `fa:session` → host-only cookie (`__Host-prohibitorum_forward_auth`) → 302 to the original URL.
  - building blocks: `mintFASession`/`loadFASession`, `mintFAState`/`popFAState`, `pkceChallengeS256`, `faCookie`/`faCookieName`, `writeIdentityHeaders` (Remote-User/Name/Email/Groups), const `ForwardAuthPathPrefix = "/.prohibitorum-forward-auth"`.
- **Routes** (`pkg/server/server.go`, both **public**): `GET /api/prohibitorum/forward-auth/verify`; `GET /.prohibitorum-forward-auth/callback` (root-level).
- **CLI** (`cmd/prohibitorum/main.go`): `forward-auth-app create --client-id --host [--display-name]` → creates a public+PKCE client (`require_consent=false`, callback redirect_uri) + `SetForwardAuthConfig(true, host)`. RBAC via the existing `oidc-client access --client-id … --access-restricted=true --grant-group …`.
- **Docs:** `docs/forward-auth.md` (Traefik middleware + per-domain router + `authResponseHeaders` + the EntryPoint `trustedIPs`/`trustForwardHeader` + the "overwrite, don't pass through, X-Forwarded-*" security mandate).
- **Reviewed + verified:** opus security review APPROVED (fail-closed verify, server-minted single-use state, full PKCE+client+redirect binding, atomic single-use replay protection, host-only cookie, headers only on 200). Runtime curl 3/3 (unknown→403, registered→302 authorize w/ correct params, bogus-state callback→/error). `forward_auth` 17 unit tests.

**Tests live in** `pkg/protocol/oidc/forward_auth_test.go` (has a `newForwardAuthTestProvider`-style harness + fake querier you can extend).

---

## Phase 2 — scope to finish

Three pieces. The admin UI is the bulk and the user's explicit ask ("admin UI").

### 2A. Admin dashboard UI for forward-auth services (PRIMARY)
Goal: register/list/edit/delete forward-auth services + manage their RBAC from the dashboard
(today this is CLI-only). Present them as their own section — **a forward-auth service is an
`oidc_client` with `forward_auth_enabled=true`**, so filter on that flag rather than mixing them
into the OIDC-applications list.

**Backend (new admin endpoints — mirror `handle_admin_oidc_clients.go` + `handle_admin_app_access.go`):**
- `GET /api/prohibitorum/forward-auth-apps` (admin) — list (filter `oidc_client WHERE forward_auth_enabled`; needs a new sqlc query, e.g. `ListForwardAuthClients`). Return `{clientId, displayName, forwardAuthHost, accessRestricted, disabled}`.
- `GET /api/prohibitorum/forward-auth-apps/{clientId}` (admin) — detail.
- `POST /api/prohibitorum/forward-auth-apps` (admin+sudo) — create: build a public+PKCE client (`require_consent=false`, `redirect_uri=https://<host>/.prohibitorum-forward-auth/callback`) + `SetForwardAuthConfig(true, host)`. (Same as the CLI's `forward-auth-app create`, factored into a shared helper so CLI + HTTP share it.)
- `PUT /api/prohibitorum/forward-auth-apps/{clientId}` (admin+sudo) — edit display name / host (keep `forward_auth_host` ↔ redirect_uri in sync).
- `POST /api/prohibitorum/forward-auth-apps/delete` (admin+sudo) — delete (drops the backing client).
- **RBAC reuse:** the existing `…/oidc-applications/{clientId}/access/{set-restricted,grant,revoke}` endpoints already operate on the backing `client_id` — reuse them directly (no new access endpoints needed). The detail page just calls those.
- Register reads via `registerOpHTTP(admin)`, mutations via `s.registerSudoOpHTTP(admin)` (the sudo wrapper) — **except** any raw/non-JSON body (none expected here; all JSON, so the wrapper is fine).

**Frontend (mirror `AdminOidcClientsView.vue` + `AdminOidcClientDetailView.vue`):**
- `dashboard/src/pages/admin/AdminForwardAuthAppsView.vue` (list + inline create: client-id, host, display-name) and `…DetailView.vue` (edit + the access/RBAC editor — reuse the access UI from the OIDC detail view + the `ScopeSelector`/grant patterns) + delete (ConfirmDialog).
- Route: `/admin/forward-auth-apps` (+ `:clientId`), `meta.requiresAdmin`, `meta.titleKey: 'title.adminForwardAuthApps'` (add the title.* keys en+zh).
- Sidebar: add a "Forward auth" item — likely under the **Applications** group in `AppSidebar.vue` (it's a downstream-app concept). Add `admin.nav.forwardAuthApps` + an `admin.forwardAuth.*` i18n block (en + zh; mind the en.ts apostrophe + `@`-escaping hazards, and `zh` parity).
- A "copy the Traefik config" affordance on the detail page (the middleware + per-domain router snippet, host-substituted) is a nice touch and a strong UX win — reuse the `CodeField`/copy components.

### 2B. Multi-domain dev harness
Goal: exercise the full browser flow locally (a dummy protected app on a second host behind Traefik,
forward-auth → Prohibitorum → callback → 200). Model on the existing **`mise dev:federation`**
harness (`cmd/prohibitorum/dev_federation.go` + `scripts/dev-federation.sh` + nginx/TLS + local
hostnames in gitignored `.dev/dev-federation.env`; committed code uses `example.test` placeholders —
see memory `feedback_dev_federation_local_only`). Add a Traefik (or reuse nginx) front + a static
"whoami"-style backend that echoes the `Remote-*` headers, plus a `forward-auth-app create` seed.
Keep real hostnames/cert paths OUT of git.

### 2C. Sign-out / revocation propagation (small backend)
Goal: a way to clear the per-domain forward-auth cookie (and optionally end the Prohibitorum
session). Add a `GET /.prohibitorum-forward-auth/sign_out` on the protected domain (routed like the
callback) that deletes the KV `fa:session` + clears the host-only cookie, then redirects (to a
configured post-logout URL or the IdP). Mirrors Authentik's `…/sign_out`. Document it in
`docs/forward-auth.md`. (Live RBAC re-check already handles *authorization* revocation per-request;
this is about *session* sign-out.)

**Suggested Phase 2 task order:** 2A backend (endpoints + shared create helper + list query) → 2A frontend (views + route + sidebar + i18n) → 2C sign-out → 2B dev harness → gate + dist rebuild + runtime verify (chromium через subagent server: register an app in the UI, confirm it lists; drive the verify/callback). Each task: build/test, FE gate where relevant, commit; final dist rebuild+commit.

---

## Gotchas carried from Phase 1
- The verify endpoint reads the **forwarded** cookie (Traefik copies the protected-domain `Cookie` header to the auth sub-request) — that's why per-domain cookies work across unrelated hosts.
- `verifyPKCE` already exists in `token.go` (constant-time) — reuse, don't redefine. `pkceChallengeS256` is the new challenge-builder in `forward_auth.go`.
- The backing client's `redirect_uri` MUST stay exactly `https://<forward_auth_host>/.prohibitorum-forward-auth/callback` — the authorize exact-match guard depends on it. If the admin edits the host, update the redirect_uri too.
- Audit factor used for forward-auth/admin mutations: reuse the same pattern as the OIDC-client admin mutations (`audit.FactorOIDCClient` / appropriate event).

## References
- Spec: `docs/superpowers/specs/2026-06-21-forward-auth-phase1-design.md`
- Plan: `docs/superpowers/plans/2026-06-21-forward-auth-phase1.md` (+ `.tasks.json`)
- Operator guide: `docs/forward-auth.md`
- Core code: `pkg/protocol/oidc/forward_auth.go`, `pkg/server/server.go` (routes ~L385), `cmd/prohibitorum/main.go` (`forward-auth-app`), migration `db/migrations/018_forward_auth.sql`
- Admin-UI templates to mirror: `dashboard/src/pages/admin/AdminOidcClient{s,Detail}View.vue`, `pkg/server/handle_admin_oidc_clients.go`, `pkg/server/handle_admin_app_access.go`
- Memory: `…/memory/project_current_state.md` (latest entries) + `feedback_dev_federation_local_only`, `reference_dev_postgres_podman`, `feedback_no_coauthor_commits`.
