# Frontend full-surface scaffold — design

**Goal:** Make every planned user interaction *visible and navigable* in the dashboard SPA, so the UX/design-system work can proceed against a complete surface — instead of only the handful of fully-implemented features. Build out the frontend for the **core** account + admin surface (wiring real endpoints where they exist), and expose the **deferred** protocol/system-admin areas as greyed "planned" placeholder pages.

**Decided in brainstorming (2026-06-03):**
- **Scope:** *Core now, protocol-admin deferred.* Fully scaffold Account self-service + Admin (accounts incl. detail, invitations). Protocol/system admin (OIDC clients, SAML providers, signing keys, audit log, settings) appear as greyed, navigable **placeholder** pages only.
- **Fidelity:** *Functional where the backend exists.* The core features wire to the real `/me/*` and admin endpoints (which mostly already exist), including the sudo step-up ceremony they require.
- **IA:** grouped sidebar with a **consolidated Security page**.
- **i18n:** new scaffold pages use **English literal copy** (no i18n keys) with a `TODO(i18n)` marker; the already-shipped login/dashboard/consent pages stay bilingual. Final copy + translation is a design-phase follow-up.
- **Admin account detail** page is **included**.

**Non-goals (this phase):** building the deferred protocol-admin features or their backend APIs (placeholders only); a backend `enroll-admin`-style API; Playwright e2e; re-styling/visual design (that's the downstream UX work this scaffold enables); changing any backend behavior beyond what already exists.

**Builds on:** the Vue 3 + Nuxt UI v4 SPA (`dashboard/`), embedded via `pkg/webui` `go:embed`, and the prior admin+user dashboard (`docs/superpowers/specs/2026-05-31-admin-user-dashboard-design.md`). Frontend-only — **no Go changes** this phase (the deferred areas are placeholders, not stubbed endpoints; the "backend stub + TODO" approach applies only when those areas are later built).

---

## Information architecture & routes

`AppSidebar` gains two grouped sections. All routes are children of `DashboardLayout` (`requiresAuth`); admin routes add `requiresAdmin`.

**Account**
| Route | Page | Status |
|---|---|---|
| `/` | `ProfileView` | exists |
| `/security` | `SecurityView` | new (consolidated) |
| `/sessions` | `SessionsView` | exists (regrouped under Account) |
| `/connected` | `ConnectedAccountsView` | new |
| `/devices` | `DevicesView` | new |

**Admin** (`requiresAdmin`)
| Route | Page | Status |
|---|---|---|
| `/admin/accounts` | `AccountsView` (list) | exists |
| `/admin/accounts/:id` | `AccountDetailView` | new |
| `/admin/invitations` | `InvitationsView` | exists |

**Admin · planned** (`requiresAdmin`, greyed in nav, navigable)
| Route | Renders |
|---|---|
| `/admin/oidc-clients` | `PlaceholderView` — "OIDC clients" |
| `/admin/saml-providers` | `PlaceholderView` — "SAML service providers" |
| `/admin/signing-keys` | `PlaceholderView` — "Signing keys" |
| `/admin/audit` | `PlaceholderView` — "Audit log" |
| `/admin/settings` | `PlaceholderView` — "Settings" |

The existing `CredentialsView` (`/credentials`) is **folded into** `SecurityView`'s Passkeys section; the `/credentials` route is kept as a **redirect to `/security`** (safe for any bookmarks/links), and `CredentialsView.vue` is retired once its logic moves into the Passkeys card. Router guard (`installGuard`) and the dev-mode `/dev` gating are unchanged in mechanism; the `/dev` console's route list is extended to include the new pages.

---

## Components & pages

### New shared components
- **`StatusBadge.vue`** — small pill for `planned` / `stub` / `beta`. Used on greyed nav items and placeholder/partial pages so the design clearly distinguishes maturity.
- **`PlaceholderView.vue`** — props `{ title, summary }`; renders a centered card: title + `StatusBadge planned` + a one-line description of the intended feature + a muted "Not yet implemented." Used by all five deferred admin routes (data-driven via route meta).
- **`SudoModal.vue`** + **`useSudo()`** composable — the step-up gate (see below).
- (Reuse existing `CopyableUrl`, the inline two-step confirm pattern, the `te()`/`role="alert"` error + `busy` guard conventions.)

### `SecurityView` (`/security`) — wired
Card sections, each its own focused sub-component under `src/pages/security/`:
- **Passkeys** (`PasskeysCard`): list (`GET /me/credentials`), rename, delete (existing logic moved from `CredentialsView`), **+ Add passkey** — `POST /me/credentials/register/begin` → `@simplewebauthn/browser` `startRegistration` → `POST /me/credentials/register/complete`. (Add a `passkeyAddCredential()` helper to `lib/webauthn.ts` mirroring `passkeyRegister`.) Not sudo-gated.
- **Password** (`PasswordCard`): set/change form → `POST /me/password/set` (sudo-gated → `useSudo`).
- **Two-factor / TOTP** (`TotpCard`): enroll — `POST /me/totp/begin` (returns otpauth/secret) → render QR + secret → `POST /me/totp/verify`; plus **Revoke password & TOTP** → `POST /me/auth/revoke-password-totp` (both sudo-gated).
- **Recovery codes** (`RecoveryCodesCard`): **Regenerate** → `POST /me/recovery-codes/regenerate` → **show-once** list with copy/download (sudo-gated).

### `ConnectedAccountsView` (`/connected`) — wired
- `GET /me/identities` → list linked upstream identities (provider, subject, linked-at).
- **Link** → fetch available providers (`GET /api/prohibitorum/auth/federation`) → per provider, navigate to `GET /me/identities/link/{slug}/begin` (redirect to upstream; sudo-gated — `useSudo` before navigating). Completes only with a live upstream OP (interaction/buttons present regardless).
- **Unlink** → `POST /me/identities/{id}/unlink` (confirm).

### `DevicesView` (`/devices`) — wired
- `GET /me/devices/pair/lookup` → pending device-pairing request(s).
- **Approve** (`POST /me/devices/pair/approve`, sudo-gated) / **Cancel** (`POST /me/devices/pair/cancel`).
- Empty state explains the device-initiated pairing flow (the new device drives `auth/devices/pair/*`).

### `AccountDetailView` (`/admin/accounts/:id`) — wired
- `GET /accounts/{id}` → account fields.
- **Edit role** + **enable/disable** → `PUT /accounts/{id}` (`{displayName, role, disabled}`; username immutable).
- **Credentials** for the account → force-revoke a credential (`POST /accounts/credentials/delete`).
- **Revoke all sessions** → `POST /accounts/revoke-sessions`.
- **Reissue enrollment** → `POST /accounts/reissue-enrollment` → `CopyableUrl`.
- **Delete account** (confirm; surface last-admin error).
- Rows in `AccountsView` link here.

### `ProfileView`, `SessionsView`, `AccountsView`, `InvitationsView`, `EnrollView`
Unchanged behavior; `SessionsView` is regrouped under Account in the sidebar.

---

## Sudo step-up (the reusable gate)

`useSudo()` exposes `withSudo(fn)`: it runs `fn()`; if it rejects with `{code:'sudo_required'}`, it opens `SudoModal`, runs the ceremony, and retries `fn()` **once**.

`SudoModal` flow:
1. `GET /me/sudo/methods` → `{methods: [...]}` (subset of `webauthn`, `password_totp`).
2. User picks a method.
   - **webauthn:** `POST /me/sudo/begin {method:'webauthn'}` → `startAuthentication` → `POST /me/sudo/complete` (assertion).
   - **password_totp:** `POST /me/sudo/begin {method:'password_totp'}` → form → `POST /me/sudo/complete {current_password, totp_code}`.
3. On success the server stamps a one-shot fresh-sudo window; `withSudo` retries the gated call. On no available methods, show guidance (enroll a factor first).

Gated callers this phase: Password set, TOTP enroll, Revoke-pwd-TOTP, Recovery-codes regenerate, Connected-account link, Device approve. (Exact sudo begin/complete wire shapes are confirmed against `pkg/server/handle_sudo.go` during plan-writing.)

---

## Conventions

- **i18n:** new pages/sections use English literal strings with a top-of-file `// TODO(i18n): key + translate after the design-system pass`. Do **not** add new keys to `locales/{zh,en}.ts` for these pages. Existing bilingual pages are untouched.
- **Errors / busy / a11y:** reuse the established `te('errors.'+code) ? t() : message` → `role="alert" aria-live="polite"` pattern and the `busy` re-entrancy guard. (For English-literal pages, the error helper still surfaces backend `{code,message}` — fall back to `message`.)
- **Destructive actions:** inline two-step confirm (matches existing views), `type="button"` everywhere.
- **Dev console (`/dev`):** extend its `routeGroups` to list every new page (Security, Connected, Devices, Account detail, the five planned admin pages) so the hub stays a complete map.
- **Embedded dist:** after Vue changes, `cd dashboard && npm run build` then `git add pkg/webui/dist` (unchanged rule).

---

## Data flow (representative)

1. User opens `/security` → Passkeys/Password/TOTP/Recovery cards each fetch/seed their state.
2. User clicks "Set password" → `withSudo(() => api.post('/me/password/set', …))` → `sudo_required` → `SudoModal` (passkey or password+TOTP) → retry → success toast/inline.
3. Admin opens `/admin/accounts` → clicks a row → `/admin/accounts/:id` → edits role → `PUT` → refetch.

## Error handling / edge cases
- `sudo_required` anywhere → step-up modal, then retry once; if the retry still fails, surface the error.
- No sudo methods enrolled → modal explains the user must enroll a factor first.
- Last-admin / last-passkey / last-factor guards → backend rejects; surfaced inline.
- Connected-account link with no live upstream → the begin redirect won't complete; the list/unlink/link affordances still render. Document as a known dev limitation.
- Planned routes → always render the placeholder (no data fetch).

## Testing
- **Vitest (API-mocked), per new unit:** `SecurityView` cards (add-passkey wiring; password/TOTP/recovery happy path incl. a mocked `sudo_required`→step-up→retry); `useSudo` (the retry-after-step-up logic) + `SudoModal` (method selection); `ConnectedAccountsView` (list + unlink); `DevicesView` (lookup + approve); `AccountDetailView` (role edit PUT, revoke-sessions, reissue); `PlaceholderView`/`StatusBadge`.
- **Gate:** `go build/vet ./...` (no Go changes, must stay green), `npm run build` + `vitest`, `pkg/webui/dist` rebuilt + committed. No new smoke assertions (frontend-only); existing smoke unaffected.
- **Honest limitation:** the manual acceptance is a click-through of the new surface in `mise dev-server`; sudo-gated and federation-link flows need enrolled factors / a live upstream to fully exercise.

## Out of scope (restated)
Protocol/system-admin features + their APIs (placeholders only); backend changes; Playwright; visual/design-system styling (the downstream goal this scaffold serves); copy finalization + translation.
