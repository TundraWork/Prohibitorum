# Launcher ↔ dashboard navigation — design

> How users cross between the "My apps" launcher home and the secondary
> settings/admin dashboard, and back. A focused follow-up to the end-user app
> launchpad (`2026-06-22-end-user-app-launchpad-design.md`), replacing a stopgap
> patch with a designed affordance.

Status: approved (brainstorm 2026-06-22).

## Context

The launchpad made `/` a minimal **launcher** (top bar + tile grid) and moved
settings/admin behind the existing sidebar shell. A code review caught that the
launcher had **no UI path** to settings/admin; the gap was patched by a one-shot
subagent that crammed Settings/Admin into the shared `NavUser` dropdown (which is
built on sidebar primitives and renders wrong in a top bar) with arbitrary
landing pages. The *gap* was a bug; *how* to cross between the two areas is an
IA/UX decision, so it's designed here.

## Decision

The launcher is **primary**; settings/admin are a **secondary dip-in area**
(per the launchpad's framing). Crossing is via a compact **account avatar menu**;
returning is via an explicit **"Apps"** entry. No new overview pages.

1. **Launcher account menu (top-bar treatment).** The launcher top bar's account
   control is a top-bar-appropriate **avatar/initial + caret** dropdown:
   **Edit profile · Settings (→ `/security`) · Admin (admins only → `/admin/accounts`) · Sign out**.
   It must look like a top-bar control, not a sidebar menu row (the review's
   visual finding).
2. **Sidebar account menu stays minimal.** In the dashboard sidebar the account
   dropdown keeps just **Edit profile · Sign out** — Settings/Admin are redundant
   there because the sidebar already lists every settings/admin page.
3. **"Apps" return.** The dashboard sidebar gets an **"Apps"** item at the very
   top (above the "Account" group), grid icon, → `/`. The brand mark is already a
   clickable link to `/` in both shells as a secondary affordance.

## Implementation

- **`dashboard/src/components/custom/NavUser.vue`** gains a `variant?: 'sidebar'
  | 'topbar'` prop (default `'sidebar'`):
  - `sidebar` → the current `SidebarMenuButton` trigger; items: Edit profile, Sign out.
  - `topbar` → a compact avatar/initial + caret `DropdownMenuTrigger`; items: Edit
    profile, Settings (`router.push('/security')`), Admin (`v-if="auth.isAdmin"`,
    `router.push('/admin/accounts')`), Sign out.
  - The edit-profile dialog + sign-out stay shared (no duplication) — this is why
    a `variant` prop is preferred over a separate component.
  - This reverts the stopgap that put Settings/Admin in the sidebar variant.
- **`dashboard/src/pages/LauncherLayout.vue`** uses `<NavUser variant="topbar" />`.
- **`dashboard/src/components/custom/AppSidebar.vue`** adds an "Apps" item (→ `/`,
  grid icon) at the top, above the Account group. (Brand → `/` already in place.)
- **i18n** (`en.ts` + `zh.ts`, at parity): add `nav.apps` (en "Apps" / zh "应用");
  reuse existing `nav.settings` / `nav.admin` / `nav.signOut` / edit-profile keys.

Landing targets: Settings → `/security`, Admin → `/admin/accounts` (no new pages).

## Testing

- vitest (`NavUser.test.ts`): `topbar` variant shows Settings + Admin (Admin gated
  by `auth.isAdmin`) and routes to `/security` / `/admin/accounts`; `sidebar`
  variant shows only Edit profile + Sign out. `AppSidebar` exposes an "Apps"
  item → `/`. Locale parity passes.
- `npm run build` typechecks; rebuild + commit `pkg/webui/dist`.
- Visual check: run the dev server and screenshot the launcher top bar + the open
  account menu to confirm the top-bar treatment reads correctly.
- No backend change → `cmd/smoke` unaffected.

## Out of scope

A settings/admin overview/index page; reworking the sidebar's grouping; any
backend change.
