# Frontend Rebuild — Spec 2a: Authenticated Shell + Sudo Gate + Profile + Sessions

> Scope: frontend. Slice **2a of 3** of the Spec 2 (authenticated dashboard)
> chunk of the from-scratch frontend rebuild. Stands up the authenticated
> dashboard shell on shadcn-vue, the reusable sudo step-up gate, and the first
> two real pages (Profile, Sessions). Security / Connected / Devices are 2b/2c;
> Admin is Spec 3.

## 1. Context

The frontend rebuild's **Spec 1** (foundation + auth threshold) is DONE: the
fresh Vite + Vue 3 + TS-strict + Tailwind v4 + shadcn-vue/Reka UI scaffold,
design tokens, core libs/store/router/i18n, the vendored `ui/` kit, and the
threshold pages (`/login`, `/consent`, `/logout`, `/error`, `/enroll/:token`).

**Important:** the rebuild's Task 1 deleted the *entire* old Nuxt UI `dashboard/`,
including the whole authenticated dashboard/admin surface built in prior chunks.
So today `dashboard/` contains only the foundation + the Spec-1 threshold pages;
the authenticated routes (`/`, `/sessions`, …) have **no Vue route** (the
catch-all sends them to `/error`). This spec rebuilds the shell + the first two
authenticated pages on shadcn-vue.

The old `dashboard/` (git `e45f356`) is **advisory only** — re-derive logic
cleanly. Backend endpoints + security properties + `DESIGN.md`/`PRODUCT.md` are
canonical. All backend shapes below were read from the handlers, not the old FE.

## 2. Goal & scope

**Goal:** restore a working `login → dashboard → logout` loop on the new stack,
and build the **sudo step-up gate** that every later sensitive action (2b/2c,
Spec 3) depends on.

**In scope (2a):**
- The authenticated **shell**: `DashboardLayout` + a config-driven `AppSidebar`
  over the vendored shadcn-vue `Sidebar` primitive.
- The **sudo gate**: `lib/sudo.ts` (`withSudo`/`ensureSudo`) + `SudoModal.vue`.
- Signature component **`StatusBadge`** (used by Sessions; the others come when
  first needed — YAGNI).
- **`ProfileView`** (`/`, read-only) and **`SessionsView`** (`/sessions`).

**Out of scope (this slice):**
- Security (passkeys/password/TOTP/recovery) — 2b. Connected accounts + Devices
  — 2c. Admin accounts/invitations + the five API-backed admin areas — Spec 3.
- `CopyableUrl` / `CodeField` signature components (first needed in 2b/Spec 3).
- Dark mode (reserved in tokens; later polish).
- Any backend HTTP change. New authenticated routes ride the existing chi
  `NotFound` SPA fallback, exactly like Spec 1.

## 3. Decomposition (Spec 2 is sliced 2a → 2b → 2c)

- **2a (this doc):** shell + sudo gate + `StatusBadge` + Profile + Sessions.
- **2b (later):** Security — Passkeys, Password, TOTP, Recovery cards (+
  `CodeField`), all behind the sudo gate.
- **2c (later):** Connected accounts (link/unlink via `/me/identities*`) +
  Devices (pairing via `/me/devices/pair/*`).

Each slice is its own spec → plan → implementation cycle.

## 4. Research-backed shell principles (and how they map to us)

Current dashboard-shell best practice, filtered to our warm-but-restrained
"Welcoming Vault" system:

- **IA first; cap primary nav at 5–7 items.** Our Account group is exactly 5
  (Profile, Security, Sessions, Connected, Devices) — no nesting needed.
- **Three visual tiers:** primary nav (clear active state) · nested (only when
  parent active) · **utility at the bottom** (account + **logout**, de-emphasized,
  divided off). Logout lives in the sidebar footer, not on the Profile page.
- **Anatomy:** sticky header (brand mark) · scrollable content (nav) · sticky
  footer (identity + logout). Expanded width ~240–260px; collapse to an
  icon-rail with hover tooltips; persist the collapse choice; off-canvas drawer
  below the mobile breakpoint (not a squeezed sidebar).
- **Warmth without clutter:** warm neutral surface (our Sunken, teal-cast — not
  cold gray), rounded corners + subtle shadow only on elevation
  (Flat-Until-It-Acts), humanist type (Hanken Grotesk), generous whitespace, and
  a single scarce warm accent (Ember) marking the brand moment.
- **Config-driven, role-filtered nav:** nav as a config array; `auth.isAdmin`
  gates the admin group (lands in Spec 3); sidebar state high in the tree so it
  survives route changes.

**shadcn-vue is the capability floor.** We adopt the vendored `Sidebar`
primitive and keep everything it provides (collapse-to-rail, mobile drawer,
tooltips, ARIA/keyboard, persisted state); we may **enhance upward** (warmth,
polish, product affordances) but never strip below it.

## 5. Architecture

### Routing (nested layout route)

Add a `/` **layout route** to `router/index.ts`; threshold routes stay flat &
public; the catch-all → `/error` is unchanged.

```ts
{
  path: '/',
  component: () => import('../pages/DashboardLayout.vue'),
  meta: { requiresAuth: true },
  children: [
    { path: '', name: 'profile', component: () => import('../pages/ProfileView.vue') },
    { path: 'sessions', name: 'sessions', component: () => import('../pages/SessionsView.vue') },
  ],
}
```

`installGuard` (already present) enforces `requiresAuth`: it lazy-loads the auth
store, calls `ensureLoaded()`, and redirects unauthenticated users to
`/login?return_to=<fullPath>`. `App.vue` stays `<RouterView/>`.

### Component taxonomy

- **`components/ui/` (vendored, CLI-fenced — `aliases.ui`):** add `sidebar` and
  its dependencies (`sheet`, `tooltip`, `separator`, `skeleton`). Token-reconcile
  any `:root`/`.dark` vars the CLI appends into the existing `@theme` mapping
  (keep our OKLCH values + shadcn's variable names); no markup hand-edits.
- **`components/custom/`:**
  - `AppSidebar.vue` — thin, **config-driven** composition over `ui/sidebar`.
    Sticky header = brand mark (the one Ember moment). Content = the Account nav
    group, built links only for 2a (Profile, Sessions). Footer = user identity
    (`auth.me.displayName`) + a logout item. An admin nav group is structurally
    anticipated but lands in Spec 3 (rendered only when `auth.isAdmin`).
  - `SudoModal.vue` — the step-up ceremony (§6), mounted once in `DashboardLayout`.
  - `StatusBadge.vue` — small token-styled status pill (Sage/Amber/Rose/neutral
    by a `variant` prop); used for the "current session" marker now, reused widely
    later.
- **`lib/sudo.ts`** — `sudoState` module singleton + `ensureSudo()` + `withSudo(fn)`
  + `_resolveSudo` test hook (§6).
- **`pages/`:** `DashboardLayout.vue`, `ProfileView.vue`, `SessionsView.vue`.

## 6. The sudo step-up gate

**Why:** a stolen session cookie (XSS / malicious extension) must not suffice for
sensitive `/me` mutations. Sudo forces a fresh proof of an enrolled factor for a
short window; the grant is **one-shot** (consumed per gated action).

**Backend contract (verified — `pkg/server/handle_sudo.go`):**
- `GET /api/prohibitorum/me/sudo/methods` → `{ "methods": [...] }` where methods ⊆
  `{"webauthn","password_totp"}` in priority order (empty = admin-recovery-only;
  `recovery_code` is intentionally NOT a sudo factor).
- `POST /api/prohibitorum/me/sudo/begin { method }` — webauthn → `200` with the
  WebAuthn request options (+ server-side ceremony stash); password_totp → `204`
  (no challenge). Rate-limited 10/min per session.
- `POST /api/prohibitorum/me/sudo/complete` — webauthn → body = assertion;
  password_totp → body `{ current_password, totp_code }`. `204` on success
  (stamps `SudoUntil`); `bad_credentials` / `ceremony_expired` /
  `sudo_method_unavailable` / `rate_limited` otherwise.
- Gated endpoints return `sudo_required` (`401`) when no fresh sudo.

**`lib/sudo.ts` (re-derived):**
```ts
export const sudoState = ref<{ open: boolean; resolve: ((ok: boolean) => void) | null }>(
  { open: false, resolve: null })
export function ensureSudo(): Promise<boolean>            // opens modal, resolves true/false
export function withSudo<T>(fn: () => Promise<T>): Promise<T>  // run; on sudo_required → ensureSudo → retry once
export function _resolveSudo(ok: boolean): void           // test/internal: resolve + close
```
`withSudo`: run `fn()`; if it throws `{code:'sudo_required'}`, `await ensureSudo()`;
if the user completed it, retry `fn()` **once**; otherwise rethrow. Any non-sudo
error rethrows immediately. (Proactive `ensureSudo()` is for actions that can't be
retried, e.g. a redirect — first used in 2c.)

**`SudoModal.vue`** (mounted once in `DashboardLayout`, watches `sudoState`):
1. On open: `GET /me/sudo/methods`. Empty → show a "no elevation method available"
   terminal message; closing resolves `false`.
2. Passkey (primary, when available): `POST begin{method:'webauthn'}` →
   `passkeyGet(options)` (reuse `lib/webauthn`) → `POST complete` (assertion) →
   `204` → `resolve(true)`.
3. Password + TOTP (secondary): a small form (current password + 6-digit code) →
   `POST begin{method:'password_totp'}` (`204`) → `POST complete{current_password,
   totp_code}` → `204` → `resolve(true)`.
4. Cancel → `resolve(false)`. Errors render via `errors.<code>` (busy guards
   re-entrancy; `aria-live` alert region).

2a builds the gate but neither Profile nor Sessions triggers `sudo_required`
(revoke is not gated) — so the gate is proven by the `withSudo` unit test +
`SudoModal` flow tests, not a contrived page action. It exists here because it is
shell-level and every 2b/2c/Spec-3 action consumes it.

## 7. Pages

**`ProfileView.vue` (`/`)** — read-only. There is **no self-profile-update
endpoint** (only admin `PUT /accounts/{id}`), so this view displays
`auth.me` (username, displayName, role) in a definition card. No edit affordance,
no invented behavior.

**`SessionsView.vue` (`/sessions`)**:
- `GET /api/prohibitorum/me/sessions` → `SessionListItem[]` (top-level array):
  `{ id, isCurrent, issuedAt, expiresAt, lastSeenIp, userAgent? }`.
- Render rows (device/UA, IP, issued/expires); `StatusBadge` marks the current
  session; the current row has **no** revoke control.
- Revoke: `POST /api/prohibitorum/me/sessions/revoke { id }` (NOT sudo-gated),
  then refresh the list. Errors via `errors.<code>`.

**Logout** is a sidebar-footer utility item that routes to `/logout` — reusing
the Spec-1 `LogoutView` (which POSTs `/auth/logout` + `auth.clear()` + landing).
One logout path, no duplicated logic.

## 8. Backend contracts used (all verified, no new endpoints)

| Method | Path | Shape |
|---|---|---|
| GET | `/api/prohibitorum/me` | `SessionView { id, username, displayName, role, attributes? }` (auth store) |
| GET | `/api/prohibitorum/me/sessions` | `SessionListItem[]` |
| POST | `/api/prohibitorum/me/sessions/revoke` | body `{ id }` → 204; cannot target current |
| GET | `/api/prohibitorum/me/sudo/methods` | `{ methods: string[] }` |
| POST | `/api/prohibitorum/me/sudo/begin` | `{ method }` → 200 (webauthn opts) / 204 (password_totp) |
| POST | `/api/prohibitorum/me/sudo/complete` | webauthn: assertion / pwd: `{current_password,totp_code}` → 204 |
| POST | `/api/prohibitorum/auth/logout` | 204 (via `/logout` page) |

Error envelope `{message, code, details}` (backend messages are Chinese → the FE
maps codes via `errors.<code>`; new sudo codes added to `en.ts`).

## 9. i18n (English-first; `locales/en.ts`)

New namespaces: `nav` (Profile, Sessions, Account, sign-out), `profile`
(title, username, displayName, role), `sessions` (title, current, revoke,
issued, expires, lastSeen, empty), `sudo` (title, prompt, passkeyButton,
passwordLabel, codeLabel, verify, cancel, noMethod). Add the `sudo_method_unavailable`
error code to the `errors.*` map (Spec 1 mapped login/consent/enroll/federation
codes but not the sudo ones); `bad_credentials`, `ceremony_expired`, `rate_limited`,
and the client-synthesized `webauthn_error` are already mapped from Spec 1.
`sudo_required` is consumed by `withSudo` and never shown. zh deferred.

## 10. Testing

- **`lib/sudo.test.ts`** — `withSudo`: passthrough on success; `sudo_required` →
  `_resolveSudo(true)` → retry once → resolves; `_resolveSudo(false)` → rethrows;
  non-sudo error rethrows without opening the modal.
- **`SudoModal.test.ts`** — passkey path (mock api + `lib/webauthn`) → resolves
  true; password+TOTP path → resolves true; cancel → resolves false; empty methods
  → terminal message.
- **`SessionsView.test.ts`** — list renders; revoke posts `{id}` and refreshes;
  current row has no revoke control.
- **`ProfileView.test.ts`** — fields render from a seeded store; logout navigates
  to `/logout`.
- **`AppSidebar.test.ts`** — built nav links (Profile, Sessions) + footer
  logout render; admin group absent when `!isAdmin`.

## 11. Embed / CSP / done-gate

- After any Vue change: `cd dashboard && npm run build` then `git add
  pkg/webui/dist` (the binary embeds the committed dist; Vite chunk hashes are
  non-deterministic — commit deliberately).
- **CSP unchanged.** The `Sidebar` primitive sets width via inline style
  *attributes* (already allowed by `style-src-attr 'unsafe-inline'`) and persists
  state via cookie/localStorage; it emits no inline `<style>` elements. Verify the
  built `dist/index.html` still has zero `<style>` tags.
- **Done-gate:** `go build/vet/test ./...` exit 0; `npm run test` green; `npm run
  build` clean + `pkg/webui/dist` committed; `cmd/smoke` `SMOKE_EXIT=0` (step 5b:
  `/` and `/sessions` are now real routes and still serve the shell). Manual:
  `mise dev-server` + `mise enroll-admin` → login → `/` (Profile) → `/sessions`
  (revoke a second session) → logout.

## 12. Out of scope (restated)

Security/Connected/Devices pages, the admin nav group + admin pages,
`CopyableUrl`/`CodeField`, dark mode, and any backend change. The sidebar's
collapse/responsive/a11y behavior comes from the vendored primitive — we do not
re-implement or simplify it.
