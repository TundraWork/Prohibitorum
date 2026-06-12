# Account dropdown + Security as default dashboard page

**Date:** 2026-06-12
**Status:** Approved (design) — pending implementation plan
**Scope:** Frontend SPA only (`dashboard/`). No backend changes.

## Problem

The authenticated dashboard's index route (`/`) is the **Profile** page, which displays
only three fields: `username` (read-only), `displayName` (inline-editable → `PUT /me`),
and `role` (badge). It is the thinnest page in the app yet occupies the default landing
slot. Meanwhile **Security** (`/security`) — passkeys, password, TOTP, recovery codes,
revoke-all — is the most function-rich surface and the one users actually come to change.

## Goals

1. Move the sparse profile information into the bottom-left **account area** of the
   sidebar, surfaced as a **dropdown menu** (shadcn-vue `NavUser` pattern), with
   display-name editing in a small dialog.
2. Make **Security** the default page: `/` lands on `/security`.
3. Consolidate **Sign out** into the new account dropdown (remove the standalone footer
   button).
4. Remove the now-redundant Profile page and its sidebar nav link.

## Non-goals

- No toast/notification system. The app has none today; feedback stays inline (reactive
  trigger update + inline `Alert` on error). Introducing a global toast system is out of
  scope.
- No avatar **image** support. `SessionView` carries no image URL; we render an
  initials-based avatar only.
- No backend changes. `PUT /me` (self display-name, sudo-free) already exists and is the
  only API this touches.
- No change to `returnTo` defaults or pairing-flow redirects (see Routing — they flow
  through the `/` → `/security` redirect untouched).

## Confirmed constraints (from code)

- **Server `displayName` validation** (`pkg/account/account.go::ValidateDisplayName`,
  via `pkg/server/handle_me.go::handleUpdateMe`): length **1–128**, **rejects control
  chars** (`r < 0x20 || r == 0x7f`), **no server-side trimming**. Violation →
  error code `invalid_display_name`, HTTP 400.
- **Error surfacing**: `lib/api.ts` throws `ApiError { code, message }` on 4xx;
  `composables/useApi.ts` captures it into `error.value`; the established display pattern
  (in `ProfileView`) is `const key = 'errors.' + e.code; return te(key) ? t(key) : e.message`.
  The `errors.invalid_display_name` i18n key already exists.
- **Sidebar state**: `useSidebar()` returns `{ state: 'expanded'|'collapsed', isMobile,
  ... }`; `SidebarMenuButton` accepts a `tooltip` prop (shown only when collapsed and not
  mobile). This lets `NavUser` adapt the dropdown side and show a name tooltip when
  collapsed.
- **No toast system** confirmed (no `sonner`/`toast`/`notify` in the codebase).

## Architecture

Five units, each with one clear responsibility:

### 1. Vendored `DropdownMenu` primitive — `components/ui/dropdown-menu/`

Manually vendored to match the existing `dialog/` vendoring style (Reka UI wrappers +
`index.ts` barrel), adapted to this repo's token conventions (sidebar tokens, the
`@theme inline` bridge). Components needed:
`DropdownMenu` (Root), `DropdownMenuTrigger`, `DropdownMenuContent` (in a `DropdownMenuPortal`),
`DropdownMenuItem`, `DropdownMenuLabel`, `DropdownMenuSeparator`, `DropdownMenuGroup`.

We do **not** vendor `Avatar` — it exists for image-with-fallback, and there is no image
source. (If avatar images are added later, swap `UserAvatar` for the vendored primitive.)

### 2. `UserAvatar.vue` (new) — `components/custom/`

Small presentational component reused by the dropdown trigger and the menu header.
- Props: `displayName?`, `username?`, `size?` (`'sm' | 'md'`, default `md` = `size-8`).
- Renders initials (1–2 chars) derived from `displayName`; falls back to the first
  char of `username`; falls back to a generic `User` lucide icon if both are empty.
- Themed via sidebar/accent tokens. `aria-hidden="true"` — the visible name is the
  accessible label.

### 3. `NavUser.vue` (new) — `components/custom/`, replaces the `SidebarFooter` inner block

Owns **both** the dropdown and the edit dialog as **siblings**. The dialog is NOT nested
inside the dropdown content — nesting triggers the known Reka/Radix focus +
`pointer-events: none` lingering-on-body bug.

States:
- **Loading** (`auth.me === null`): render a `Skeleton` shaped like the trigger, not an
  empty footer.
- **Trigger**: `SidebarMenuButton` (size `lg`) used as `DropdownMenuTrigger` (`as-child`).
  Contents: `UserAvatar` + `displayName` + `role` (muted) + `ChevronsUpDown` icon.
  `min-w-0` / `truncate` on the text column. When collapsed
  (`state === 'collapsed' && !isMobile`): icon-only with `tooltip = displayName`.
- **Content**: `side = isMobile ? 'bottom' : 'right'`, `align = 'end'`, `:side-offset="4"`,
  `min-w-56`. Layout: header label (`displayName`, `@username`, role badge) → separator →
  **"Edit display name…"** item → separator → **"Sign out"** item
  (`LogOut` icon → `router.push('/logout')`; an action item, not an anchor, so logout
  cannot be opened in a new tab).

**Dropdown → Dialog handoff**: the Edit item's `@select` sets `editOpen = true` on
`nextTick` (allowing the menu to finish closing and restoring focus to the trigger
first). The dialog's `v-model:open` then traps focus; on close, focus returns to the
trigger. This is the robust pattern that avoids the lingering `pointer-events: none` bug.

Data: reads `auth.me` from the Pinia auth store.

### 4. `EditDisplayNameDialog.vue` (new) — `components/custom/`

Vendored `Dialog` wrapping a `<form @submit.prevent="save">` (so Enter submits).
- Props/model: `v-model:open`.
- **Draft reset**: `watch` on open → when opening, `draft = auth.me.displayName`. No stale
  carry-over between opens.
- **Client validation mirrors the server exactly**: length 1–128, reject control chars
  (`< 0x20 || === 0x7f`), **no auto-trim** (stored value equals what the user sees;
  whitespace-only is the server's call). Save is disabled when the value is empty, >128,
  contains control chars, or is **unchanged** (no-op guard).
- **Submit**: `useApi.run` → `api.put<SessionView>('/api/prohibitorum/me', { displayName })`
  → `auth.setDisplayName(draft)` → close. `busy` disables Save and shows a spinner
  (prevents double-submit).
- **Error**: dialog stays open, input preserved; inline `Alert` shows the mapped message
  via the `errors.${code}` → `te ? t : message` pattern (so `invalid_display_name` renders
  the friendly string).
- **No sudo** — `PUT /me` self-edit is sudo-free.

### 5. `AppSidebar.vue` (modified)

- Remove the **Profile** entry from the Account nav list (Security becomes the first
  Account item).
- Replace the `SidebarFooter` inner block (static `displayName`/`role` text + standalone
  Sign out `RouterLink`) with `<NavUser />`.

## Routing (`router/index.ts`)

- Remove the `profile` index child and its `ProfileView` lazy import.
- Add `{ path: '', redirect: { name: 'security' } }` as the index child; `/security`
  remains the canonical route and component mount (no duplicate mount).
- Delete `pages/ProfileView.vue`.

The `/` → `/security` redirect means existing landings on `/` keep working **without
changes**:
- `lib/returnTo.ts` defaults `return_to` to `/` → redirects to `/security`.
- `pages/PairDeviceView.vue` `router.push('/')` after pair/enroll → redirects to `/security`.

These are deliberately left untouched to minimize blast radius.

## i18n (`locales/en.ts`)

- **Add** an `account.*` group: edit-item label, dialog title + description, and any aria
  labels for the trigger.
- **Reuse** existing keys: `nav.signOut`, `profile.displayName`, `profile.username`,
  `profile.role`, `errors.invalid_display_name`.
- **Prune** only the page-only `profile.*` strings that nothing else references after
  `ProfileView` is deleted (e.g. a page title/description if present). Keep the label
  strings the dropdown/dialog reuse.
- Grep-verify apostrophes after editing (`en.ts` curly-quote Edit hazard).

## Data flow

```
auth store (Pinia) ──auth.me──▶ NavUser (trigger + menu header) ──▶ UserAvatar
                                     │
                              "Edit display name…" (@select, nextTick)
                                     ▼
                          EditDisplayNameDialog (v-model:open)
                                     │ save()
                                     ▼
              useApi.run → api.put('/api/prohibitorum/me', { displayName })
                                     │ success
                                     ▼
                       auth.setDisplayName(draft)  → trigger updates reactively
```

## Error handling

| Condition | Behavior |
|---|---|
| `auth.me === null` (loading) | NavUser renders a Skeleton trigger |
| Empty / >128 / control char / unchanged draft | Save disabled (client) |
| `invalid_display_name` (400) | Dialog stays open, input preserved, inline `Alert` with mapped message |
| Other API error | Same inline `Alert`, mapped via `errors.${code}` → fallback to `message` |
| Dialog closed (Esc / Cancel / overlay) | Draft discarded; focus returns to trigger |

## Testing

- **Delete** `pages/ProfileView.test.ts` (component removed).
- **`UserAvatar.test.ts`** (light): initials from `displayName`; `username` fallback;
  generic-icon fallback when both empty.
- **`NavUser.test.ts`**: Skeleton when `me` is null; renders name/role + avatar when
  loaded; opening the menu shows the header + "Edit display name…" + "Sign out"; Sign out
  triggers `router.push('/logout')`; selecting Edit opens the dialog. Reka idioms
  (`click` / `mousedown` / `$emit`, not `setValue`).
- **`EditDisplayNameDialog.test.ts`**: prefills current name; empty / >128 / unchanged
  disable Save; success calls `PUT /me` + `auth.setDisplayName` + closes; an
  `invalid_display_name` error keeps the dialog open, shows the mapped message, and
  preserves the input.
- **`AppSidebar.test.ts`** (update): no Profile link; Security present; footer renders the
  NavUser trigger (not the old static text / standalone sign-out row).

## Files

**New**
- `dashboard/src/components/ui/dropdown-menu/` (vendored wrappers + `index.ts`)
- `dashboard/src/components/custom/UserAvatar.vue`
- `dashboard/src/components/custom/NavUser.vue`
- `dashboard/src/components/custom/EditDisplayNameDialog.vue`
- `dashboard/src/components/custom/UserAvatar.test.ts`
- `dashboard/src/components/custom/NavUser.test.ts`
- `dashboard/src/components/custom/EditDisplayNameDialog.test.ts`

**Modified**
- `dashboard/src/router/index.ts`
- `dashboard/src/components/custom/AppSidebar.vue`
- `dashboard/src/components/custom/AppSidebar.test.ts`
- `dashboard/src/locales/en.ts`

**Deleted**
- `dashboard/src/pages/ProfileView.vue`
- `dashboard/src/pages/ProfileView.test.ts`

## Done-gate

`go build` / `go vet` / `go test` (0 failures), `vitest` (all green), `vue-tsc -b` (0),
smoke `SMOKE_EXIT=0`, then rebuild the embedded SPA and **commit `dist`** (Vite hashes are
non-deterministic, so the committed `dist` must be regenerated).
