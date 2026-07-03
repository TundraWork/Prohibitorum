# Maintenance-mode redirect + dedicated notice page

**Date:** 2026-07-03
**Status:** Design approved

## Problem

Maintenance mode is fully enforced server-side today: `maintenanceLockout`
blocks non-admins at every session-issuance point, `maintenanceGateMW` blocks
non-admin API/SSO traffic, and the forward-auth gateway carries its own check.
A `/maintenance` notice page already exists and authenticated non-admins are
already redirected to it by the router guard.

The remaining gap is the **login page for unauthenticated visitors**. During
maintenance, an unauthenticated normal user can still reach `/login` and sees
the full sign-in form with only a small banner: *"The system is under
maintenance. Only administrators can sign in right now."* That is confusing —
they can attempt sign-in flows that the backend will reject anyway.

## Goal

During maintenance:

- Normal users (unauthenticated, or authenticated non-admins) are funnelled to
  the dedicated `/maintenance` notice page, which shows only the system notice
  and the optional *Message for users* — **no login functionality**.
- Admins can still sign in, via a deliberate **"Administrator sign-in"** link on
  the notice page that opens the normal login form.

## Non-goals

- No backend changes. The backend already guarantees only admins can obtain a
  session during maintenance; the admin-entry flag below is a UX affordance, not
  a security boundary.
- Not preserving `return_to` through the maintenance bounce (see "Optional
  refinement (deferred)").
- No visually distinct admin login page — the existing `LoginView` is reused.

## Approach

Close the gap entirely in the **client-side router guard**
(`dashboard/src/router/index.ts`). The SPA already owns this routing flow, and
the backend already enforces admin-only sign-in, so a client-side flag
(`/login?admin=1`) is sufficient and correct as the entry affordance.

### 1. Router guard (`dashboard/src/router/index.ts`)

Rewrite the maintenance block in `installGuard` so that when maintenance is
**on** and the visitor is **not an authenticated admin**, they are confined to
exactly three destinations — everything else redirects to `/maintenance`:

- route `maintenance` — the notice page (the redirect target itself)
- route `logout` — so a stuck authenticated non-admin can still sign out
- route `login` **with the `admin` query flag present** — the admin entry

Plain `/login` (no `admin` flag) now redirects to `/maintenance`. Authenticated
admins (`auth.me && auth.isAdmin`) bypass the block entirely and fall through to
the normal flow, exactly as today.

Because `/` requires auth → redirects to `/login` → which (no flag) redirects to
`/maintenance`, all normal-user entry points funnel to the notice page. Guard
redirects resolve before the target route renders, so there is no visible flash
of the login form.

The maintenance-**off** branch is unchanged: if someone is on `/maintenance`
while maintenance is off, redirect them to `/login`.

Guard sketch (maintenance-on, non-admin branch):

```ts
if (branding.maintenanceMode) {
  try { await auth.ensureLoaded() } catch { /* treat as unauthenticated */ }
  const isAdmin = !!auth.me && auth.isAdmin
  if (!isAdmin) {
    const allowed =
      to.name === 'maintenance' ||
      to.name === 'logout' ||
      (to.name === 'login' && to.query.admin != null)
    if (!allowed) return { name: 'maintenance' }
    return true
  }
  // admin: fall through to the normal flow below
}
```

### 2. `MaintenanceView.vue`

The page already renders the warm heading, the body (with instance name), the
optional *Message for users* callout (`branding.maintenanceMessage`, shown only
when set), and a "Try again" reload button. It already tracks `hasSession` and
shows a "Sign out" link when authenticated.

Add one affordance to the footer, mirroring the existing `hasSession` split:

- **Unauthenticated** (`v-else`, `!hasSession`) → a subtle "Administrator
  sign-in" link to `{ name: 'login', query: { admin: '1' } }`.
- **Authenticated** (`v-if="hasSession"`) → the existing "Sign out" link
  (unchanged).

The "Try again" button stays for everyone. Style the admin link like the
existing sign-out link (`text-xs text-muted underline underline-offset-4
hover:text-ink`) so it reads as a quiet, secondary affordance.

### 3. Admin login page

No component change. It is the existing `LoginView`, reached via
`/login?admin=1`. Its existing maintenance notice banner
(`login.maintenanceNotice` — *"…Only administrators can sign in right now."*)
stays and now reads correctly in context. `LoginView` does not need to read the
`admin` flag; the guard alone consumes it.

### 4. i18n

Add one key, `maintenance.adminSignIn` (e.g. *"Administrator sign-in"*), to both
`dashboard/src/locales/en.ts` and `dashboard/src/locales/zh.ts`. No other
strings change. Follow the existing `en.ts` apostrophe/`@` hazards
(grep-verify after editing).

## Testing

- **`dashboard/src/router/guard.test.ts`** — add cases:
  - maintenance on + unauthenticated navigating to `/login` → redirected to
    `/maintenance`
  - maintenance on + `/login?admin=1` → allowed through (stays on `login`)
  - maintenance on + authenticated non-admin on an app route → `/maintenance`
  - maintenance on + admin → passes through (no maintenance redirect)
  - (existing non-maintenance cases must still pass; `api.get` mock returns no
    `maintenanceMode`, so those paths are unaffected)
- **`dashboard/src/pages/MaintenanceView.test.ts`** — add cases:
  - unauthenticated → shows the admin-sign-in link (href to `/login?admin=1`),
    no sign-out link
  - authenticated → shows sign-out, no admin-sign-in link
- **Gate:** vitest, `vue-tsc`, `go build -tags nodynamic`/`vet`,
  `check-contrast`, then rebuild + commit `dist`. No new smoke step — this is
  client-side routing; the maintenance toggle is already smoke-covered
  server-side.

## Optional refinement (deferred)

When an unauthenticated user is bounced from `/login?return_to=X` (e.g. arriving
from an OIDC app) to `/maintenance`, we could carry `return_to` through so the
admin-sign-in link preserves it. Deferred: during maintenance, admins typically
go straight to the dashboard, and dropping `return_to` just means re-initiating
the app afterward.

## Affected files

- `dashboard/src/router/index.ts` — maintenance block rewrite
- `dashboard/src/pages/MaintenanceView.vue` — admin-sign-in link
- `dashboard/src/locales/en.ts`, `dashboard/src/locales/zh.ts` — new key
- `dashboard/src/router/guard.test.ts`, `dashboard/src/pages/MaintenanceView.test.ts` — tests
- `dashboard/dist/**` — rebuilt embedded assets (committed at the done-gate)
