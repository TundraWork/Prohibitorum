# Admin + User Dashboard — design

**Goal:** Deliver a basic admin+user dashboard that makes the whole
**login → dashboard → logout** flow manually testable in a browser, from a fresh
database. Adds the authenticated dashboard landing, a passkey-enrollment page (the
missing bootstrap), and a router auth-guard — all consuming the backend APIs that
already exist.

**Scope:** Frontend-only. Every section is backed by an existing endpoint, and
the new SPA routes (`/`, `/sessions`, `/credentials`, `/admin/accounts`,
`/admin/invitations`, `/enroll/:token`) are served by the existing
`NotFound`→`index.html` SPA fallback — so there are **no Go changes**. Builds on
the Vue 3 + Nuxt UI v4 SPA from the login+consent chunk
(`docs/superpowers/specs/2026-05-31-login-consent-ui-design.md`).

**Out of scope:** sudo step-up flow (none of the chosen actions are sudo-gated —
verified: `requireFreshSudo` guards only password/TOTP setup, identity linking,
revoke-password-TOTP, device pairing — none in this dashboard); add-passkey on the
credentials page; federation enrollment (`start-federation`); `/me` device-pairing
and identities UI; a headless-browser e2e (Playwright — future).

**Context (existing APIs this consumes):**
- User: `GET /api/prohibitorum/me` (`SessionView{id,username,displayName,role}`);
  `GET /me/sessions` + `POST /me/sessions/revoke {id}` (`SessionListItem`);
  `GET /me/credentials` + `POST /me/credentials/rename {id,nickname}` +
  `POST /me/credentials/delete {id}` (`CredentialView`; last-passkey guarded).
- Enrollment: `GET /api/prohibitorum/enrollments/{token}` (`EnrollmentPreview{intent,
  target{username,displayName},expiresAt}`); `POST …/register/begin` +
  `POST …/register/complete` (WebAuthn; **complete sets the session cookie + returns
  `{session: SessionView}` → auto-login**).
- Admin (role-gated): `ListAccounts` (`AccountView{id,username,displayName,role,
  disabled,createdAt,updatedAt,lastSignInAt}`), `UpdateAccount`, `DeleteAccount`
  (last-admin guarded), `ReissueEnrollment` (→ enrollment URL), `CreateInvitation`
  (→ enroll URL), `ListInvitations` (`InvitationView`), `RevokeInvitation`.
- SPA infra already present: `lib/api.ts` (`api.get/post`, `{code,message}` errors),
  `lib/returnTo.ts` (`safeReturnTo`), Pinia `stores/session.ts` (`fetchMe`),
  vue-i18n (zh+en), `lib/webauthn.ts` (`passkeyLogin` via `@simplewebauthn/browser`),
  the `te()`-localized error pattern, `role="alert"`/`type="button"` conventions.

---

## Decisions

- **D1 — Sidebar layout.** A `DashboardLayout.vue` shell: a persistent left
  **sidebar** of nav links + a header (app name, `LocaleSwitcher`, a user menu
  showing `displayName` + a **Logout** action that navigates to `/logout`) +
  `<RouterView>` for the active section. The **Admin nav group renders only when
  `session.me?.role === 'admin'`**. (Chosen over top-tabs / single-page; it is the
  conventional admin-dashboard shape and grows cleanly.)

- **D2 — Routes & meta.**
  - In-layout, `meta.requiresAuth`: `/` → `ProfileView`, `/sessions` →
    `SessionsView`, `/credentials` → `CredentialsView`.
  - In-layout, `meta.requiresAuth` + `meta.requiresAdmin`: `/admin/accounts` →
    `AccountsView`, `/admin/invitations` → `InvitationsView`.
  - Public, no layout: `/enroll/:token` → `EnrollView`; existing `/login`,
    `/consent`, `/logout`, `/error`. Remove the old catch-all `→ /login` redirect;
    catch-all now `→ /` (the guard redirects to `/login` if unauthenticated).

- **D3 — Auth guard (`router.beforeEach`).** For a `requiresAuth` route: ensure a
  session via the Pinia store (`await session.ensureLoaded()` — `fetchMe()` cached;
  only fetches once). No session → `next('/login?return_to=' +
  encodeURIComponent(to.fullPath))` (login already returns there, same-origin
  guarded). For `requiresAdmin`: additionally require `role === 'admin'`; otherwise
  `next('/')`. Public routes pass through. The guard never blocks `/enroll/:token`.

- **D4 — Session store.** Extend `stores/session.ts` with `ensureLoaded()`
  (idempotent `fetchMe` — returns cached `me` if already fetched, else fetches once;
  treats 401 as `me=null`), an `isAdmin` getter, and a `clear()` (used after logout
  is initiated). The guard and the layout read this single source of truth.

- **D5 — Enrollment page (`/enroll/:token`).** On mount: `GET …/enrollments/{token}`
  → render "Set up your passkey for **{target.displayName}**" + a **Register
  passkey** button; invalid/expired/GET-error → `/error?code=…`. Click →
  `passkeyRegister(token)`: `POST …/register/begin` → `startRegistration({optionsJSON})`
  → `POST …/register/complete`. Complete sets the session cookie + returns the
  SessionView (auto-login) → on success `window.location.assign('/')` (full
  navigation so the freshly-set cookie is in play and the store reloads). Passkey
  only (no federation enroll this chunk).

- **D6 — `lib/webauthn.ts` extension.** Add `passkeyRegister(token: string):
  Promise<void>` using `@simplewebauthn/browser`'s `startRegistration`, mirroring
  the existing `passkeyLogin`. The begin response shape (flat options vs nested
  `publicKey`) is handled the same way the login helper does; verify against the
  enrollment handler during implementation.

- **D7 — Sections (each its own SFC under `src/pages/`, rendered in the layout):**
  - **`ProfileView` (`/`)**: a `<UCard>` with username, displayName, role; Logout
    (also in the header).
  - **`SessionsView` (`/sessions`)**: `GET /me/sessions` → table (current badge,
    issuedAt, lastSeenIp, userAgent, expiresAt); **Revoke** on non-current rows
    (`POST /me/sessions/revoke {id}`) → refetch. The current session has no revoke
    button (backend refuses; UI hides it).
  - **`CredentialsView` (`/credentials`)**: `GET /me/credentials` → list (nickname,
    `credentialIdSuffix`, transports, createdAt, lastUsedAt); **Rename** (inline,
    `POST /me/credentials/rename {id,nickname}`) + **Delete** (confirm; `POST
    /me/credentials/delete {id}`; surface the last-passkey error) → refetch.
  - **`AccountsView` (`/admin/accounts`)**: `ListAccounts` → table (username,
    displayName, role, disabled, lastSignInAt). Row actions: **Disable/Enable**
    (`UpdateAccount`), **Delete** (confirm; surface last-admin error),
    **Reissue enrollment** (`ReissueEnrollment` → show a copyable `/enroll/<token>`
    URL) → refetch.
  - **`InvitationsView` (`/admin/invitations`)**: `ListInvitations` → table;
    **Create invitation** (`CreateInvitation` → copyable enroll URL); **Revoke**
    (`RevokeInvitation`) → refetch.

- **D8 — Mutations UX.** Destructive actions (delete account, delete credential,
  revoke) use a confirm step (Nuxt UI modal or a native confirm). All actions
  surface backend `{code,message}` via the `te('errors.'+code) ? t(...) : message`
  pattern in an `aria-live` region; non-submit buttons carry `type="button"`. Add
  any new `errors.*`, `nav.*`, section-label, and table-header i18n keys to BOTH
  `locales/zh.ts` and `en.ts`.

- **D9 — Run & manual test.** Add a `mise` task `dev-server` that runs
  `npm run build` (→ `pkg/webui/dist`) then `go run ./cmd/prohibitorum` against the
  dev PG, serving the embedded SPA at `http://localhost:8080`. Browsers treat
  `http://localhost` as a secure context, so the WebAuthn enroll + login ceremonies
  work over plain HTTP locally. The spec/README ships the manual-test script:
  `prohibitorum enroll-admin` → open the printed `/enroll/<token>` URL → Register
  passkey → auto-login → `/` dashboard → Sessions/Credentials → (admin)
  Accounts/Invitations → Logout → `/login` → log in with the passkey → back to `/`.

- **D10 — Embedded dist.** The binary embeds the committed `pkg/webui/dist`. After
  any Vue change: `cd dashboard && npm run build` then `git add pkg/webui/dist` in
  the same commit. (Unchanged from the prior chunk.)

---

## Components

`dashboard/src/`:
- `pages/DashboardLayout.vue` — sidebar + header shell with `<RouterView>`.
- `pages/{ProfileView,SessionsView,CredentialsView,AccountsView,InvitationsView,EnrollView}.vue`.
- `components/AppSidebar.vue` (nav + role-gated admin group), `components/CopyableUrl.vue` (reused for reissue/invite URLs), optional `components/ConfirmButton.vue` (confirm wrapper).
- `router.ts` — new routes + `meta` + the `beforeEach` guard.
- `stores/session.ts` — `ensureLoaded()`, `isAdmin`, `clear()`.
- `lib/webauthn.ts` — add `passkeyRegister(token)`.
- `lib/api.ts` — unchanged (reused).
- `locales/{zh,en}.ts` — new keys (nav, section titles, table headers, errors).
- Tests: `*.test.ts` for the guard logic + a representative view (Sessions revoke / Accounts role-gating), API-mocked.

`mise.toml` — add the `dev-server` task. `cmd/smoke/main.go` — add the SPA-route shell assertions (see Testing). No other backend files.

---

## Data flow (the manual-test path)
1. `prohibitorum enroll-admin` → prints `http://localhost:8080/enroll/<token>`.
2. Browser opens `/enroll/<token>` → `EnrollView` previews → **Register passkey** →
   begin/ceremony/complete → session cookie set → `window.location.assign('/')`.
3. `/` loads → guard `ensureLoaded()` finds the session → `ProfileView` renders;
   sidebar shows admin group (the bootstrap account is admin).
4. Navigate Sessions/Credentials/Accounts/Invitations — each fetches its API and
   renders; actions mutate + refetch.
5. **Logout** → `/logout` (POST `/auth/logout`, store `clear()`) → landing → user
   goes to `/login`.
6. Visit `/` again → guard finds no session → `/login?return_to=%2F` → passkey
   login → returns to `/`.

## Error handling / edge cases
- Unauthenticated access to any `requiresAuth` route → `/login?return_to=<path>`.
- Non-admin reaching `/admin/*` → redirected to `/` (admin nav also hidden).
- Expired/invalid enrollment token → `/error?code=…`.
- Last-passkey delete / last-admin delete → backend rejects; surfaced inline.
- Revoke-current-session → not offered (backend refuses).
- `fetchMe` 401 → store `me=null` → treated as unauthenticated.

## Testing
- **Frontend (Vitest, API-mocked):** the `beforeEach` guard (unauth→login w/
  return_to; non-admin→`/`; admin passes), `SessionsView` revoke, `AccountsView`
  role-gated render, `EnrollView` ceremony wiring. Component-level, not a browser.
- **Smoke (`cmd/smoke`):** assert `/`, `/enroll/<token>`, `/admin/accounts` return
  the SPA shell (`id="app"`) and are not shadowed (mirrors the Task-8/12 checks).
- **Manual (the explicit goal):** the D9 walkthrough, run by a human in a browser.
- **Gate:** `go build/vet/test ./...`, `npm run build` + `vitest`, full smoke
  `SMOKE_EXIT=0`; `pkg/webui/dist` rebuilt + committed.
- **Honest limitation:** no headless-browser e2e (no harness); the click-through is
  the manual verification. A Playwright suite is the future follow-up.

## Out of scope (restated)
Sudo step-up; add-passkey; federation enrollment; identities/device-pairing UI;
account create-from-scratch (use enrollment/invitations); Playwright e2e.
