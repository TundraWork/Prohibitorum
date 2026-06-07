# Spec 3a — Admin shell + Accounts + Invitations (frontend)

**Date:** 2026-06-08
**Status:** approved — ready for writing-plans
**Predecessors:** Frontend rebuild Spec 1 (threshold), 2a (shell+sudo), 2b (Security), 2c (Connected+Devices+Pairing) — all shipped on `master`.

This is the **first** of three admin slices. It builds the admin section of the dashboard
(the rebuild's Task 1 deleted the entire prior admin surface) and wires the **already-built
backend Admin Management API** (`api.md`; chunk `ce2fdf4`..`459c505`) for **Accounts** and
**Invitations**. It follows the patterns established by 2a/2b/2c: `useApi` (`busy`/`error`/`run`),
the sudo gate (`withSudo`), `errors.<code>` i18n mapping, `Card`/`Alert`/`StatusBadge`/
`ConfirmDialog`/`CodeField`, `data-test` hooks, the per-slice done-gate.

---

## Context — the admin gap and its decomposition

A code-verified survey (2026-06-08) of the 111-endpoint backend vs the rebuilt `dashboard/`
found: **user self-service is complete**; the **entire admin surface is unimplemented** (7
resource areas, ~14 reads + ~31 sudo-gated mutations, zero frontend), plus one non-admin gap
(recovery-code login). The admin surface is too large for one spec, so it is sliced:

- **Spec 3a (this doc)** — admin shell + **Accounts** + **Invitations**.
- **Spec 3b** — relying parties: **OIDC clients** + **SAML service providers**.
- **Spec 3c** — **upstream IdPs** (federation config) + **signing keys** + **audit events**.
- **Deferred, tracked separately** — recovery-code login flow (`/auth/recovery-code/verify`
  + `/auth/recovery/totp/{begin,verify}`): a lost-authenticator user with no passkey is locked
  out of the UI. Threshold-side, independent of admin work. Not in any admin slice.

## Backend contracts (authoritative — read from `pkg/contract/auth.go` + `pkg/server/handle_account.go` on 2026-06-08)

**`api.md` is STALE for these routes** — the mutations are POST-with-body, not REST `/{id}` paths.
The exact Huma `Operation` method+path are canonical:

Accounts (all `registerOp` = **admin-role, NO sudo** unless noted):
- `GET /api/prohibitorum/accounts` → `[]AccountView`.
- `GET /api/prohibitorum/accounts/{id}` → `AccountView`. 404 `account_not_found`.
- `PUT /api/prohibitorum/accounts/{id}` → `AccountView`. Body `{username:"" (immutable — reject if non-empty), displayName (required), role:"admin"|"user" (required), attributes?:map, disabled (required)}`.
  Errors: `invalid_role`, `username_immutable`, `invalid_display_name`, `admin_cannot_be_disabled` (409, role=admin && disabled), `last_admin` (409, demote/disable the only active admin), `account_not_found`.
  **⚠ `Attributes` is REPLACED on every PUT** (`encodeAttributes(in.Body.Attributes)`), so the FE MUST send the account's existing `attributes` back unchanged or they are cleared.
- `POST /api/prohibitorum/accounts/delete` → 204. Body `{id:int32}`. Errors: `cannot_delete_self` (409), `last_admin` (409), `account_not_found`. Side effect: revokes the account's sessions.
- `GET /api/prohibitorum/accounts/{id}/credentials` → `[]CredentialView` `{id, credentialIdSuffix, nickname?, transports[], backupState, attestationType, createdAt, lastUsedAt?}`. 404 `account_not_found`.
- `POST /api/prohibitorum/accounts/credentials/delete` → 204. **admin + SUDO** (the one sudo-gated op in 3a). Body `{accountId:int32, credentialId:int32}`. Errors: `sudo_required`, `bad_request`, `account_not_found`, `credential_not_found`.
- `POST /api/prohibitorum/accounts/revoke-sessions` → 200 `{revoked:int}`. Body `{id:int32}`. 404 `account_not_found`.
- `POST /api/prohibitorum/accounts/reissue-enrollment` → 200 `{url:string, expiresAt:RFC3339}` (reset-intent enrollment URL; forces passkey re-enroll). Body `{id:int32}`. 404 `account_not_found`.

`AccountView` = `{id:int32, username, displayName, role:"admin"|"user", attributes?:map, disabled:bool, createdAt:RFC3339, updatedAt:RFC3339, lastSignInAt?:RFC3339}`.

Invitations (all `registerOp` = **admin-role, NO sudo**):
- `POST /api/prohibitorum/invitations` → 200 `{url:string, expiresAt:RFC3339}`. Body `{role:"admin"|"user" (required), attributes?:map}`. Intent = `invite`. Error: `invalid_role`, `bad_request`.
- `GET /api/prohibitorum/invitations` → `[]InvitationView` `{token, url, role, attributes?, createdAt:RFC3339, expiresAt:RFC3339}` (pending only; **the full `url` is returned in the list**, so it is re-retrievable — not strictly reveal-once).
- `POST /api/prohibitorum/invitations/revoke` → 204. Body `{token:string}`. Error: `invitation_not_found` (404), `bad_request`.

i18n: only `errors.invalid_display_name` exists today. ADD: `last_admin`, `admin_cannot_be_disabled`,
`cannot_delete_self`, `invalid_role`, `username_immutable`, `account_not_found`, `invitation_not_found`.

---

## Shell (foundation)

- **Vendor the shadcn-vue `Table` primitive** into `components/ui/table/` (Table, TableHeader,
  TableBody, TableRow, TableHead, TableCell, + index). Token-styled only (capability-floor rule).
  No Table exists in the vendored kit yet.
- **AppSidebar.vue** — add an **Admin** `SidebarGroup` (the file already reserves "admin group
  rendered only when `auth.isAdmin`"), rendered `v-if="auth.isAdmin"`. Items: **Accounts**
  (`/admin/accounts`, `Users` icon), **Invitations** (`/admin/invitations`, `Ticket` icon). Active-state
  via the existing `isActive` helper.
- **Router** — three `requiresAdmin` children of the existing `DashboardLayout` (`/`) route:
  `admin/accounts` (`AdminAccountsView`), `admin/accounts/:id` (`AdminAccountDetailView`),
  `admin/invitations` (`AdminInvitationsView`). `installGuard` already enforces `requiresAdmin`
  (non-admin → `/error?error=forbidden`) — reuse unchanged. `errors.forbidden` already maps
  (verify; add if missing).

## Page 1 — `pages/admin/AdminAccountsView.vue` (`/admin/accounts`)

`GET /accounts` → a **Table**: columns **User** (displayName over `@username`, `min-w-0`/`truncate`),
**Role** (`StatusBadge` neutral/admin-accent), **State** (`StatusBadge` success "Active" / caution
"Disabled"), **Last seen** (relative or `—` when null). Whole row is a link to `/admin/accounts/:id`
(`data-test="account-row-{id}"`). Header title + an **Invite** button → `router push /admin/invitations`.
Loading skeleton, empty state, `errorText` Alert.

## Page 2 — `pages/admin/AdminAccountDetailView.vue` (`/admin/accounts/:id`)

On mount, `GET /accounts/{id}` and `GET /accounts/{id}/credentials` (in parallel; 404 → not-found
state). A back link to the list. Card stack:

1. **Identity & role** — form: `displayName` (Input), `role` (select admin/user), `disabled` (toggle/checkbox);
   username shown read-only. Save → `withSudo(PUT /accounts/{id}, {username:'', displayName, role, disabled, attributes: <existing, unchanged>})`.
   Surface `last_admin`, `admin_cannot_be_disabled`, `invalid_role`, `invalid_display_name` via `errors.<code>`.
   If `attributes` present, render them read-only (key/value list); **editing attributes is out of scope**.
2. **Passkeys** — list from `/accounts/{id}/credentials` (suffix `····{suffix}`, nickname, transports,
   created, last used). Per row **Force-revoke** → `ConfirmDialog` → `withSudo(POST /accounts/credentials/delete, {accountId, credentialId})` → refresh list. Surface `credential_not_found`. Empty state when none.
3. **Sessions** — **Revoke all sessions** button → `ConfirmDialog` → `POST /accounts/revoke-sessions {id}` →
   show `{revoked:N}` as a status line.
4. **Reset access** — **Reissue enrollment link** button → `POST /accounts/reissue-enrollment {id}` →
   reveal returned `{url, expiresAt}` in a `CodeField` + an expiry note. (Reset intent: the user re-enrolls passkeys.)
5. **Danger zone** (rose, like Security revoke) — **Delete account** → `ConfirmDialog` (itemized
   consequences) → `POST /accounts/delete {id}` → on success `router push /admin/accounts`. Surface
   `cannot_delete_self`, `last_admin`.

All mutations wrapped in `withSudo` (no-op unless the server returns `sudo_required`; only credential
force-revoke does today — but wrapping future-proofs and is harmless).

## Page 3 — `pages/admin/AdminInvitationsView.vue` (`/admin/invitations`)

`GET /invitations` → a **Table** (consistent with Accounts) of outstanding
invitations: **Role** badge, **Created**, **Expires**, and the enrollment **URL** in a `CodeField`
(copyable; the list returns the full URL). **Create invitation**: a `ConfirmDialog`/inline form with a
role select (admin/user) → `POST /invitations {role}` → on success refresh the list (the new invite
appears with its copyable URL; optionally highlight it). **Revoke** per row → `ConfirmDialog` →
`POST /invitations/revoke {token}` → refresh. Empty/loading/error states. (Attributes on invitations
are out of scope for create — role only.)

## Cross-cutting

- New i18n `admin.*` namespace: `admin.nav.{accounts,invitations}`, `admin.accounts.*`,
  `admin.account.*` (detail), `admin.invitations.*` + the `errors.*` additions above. Curly U+2019
  apostrophes; **run the U+2018 grep after every en.ts edit** (see the en.ts-apostrophe hazard memory).
- A small **relative-time** helper (`lib/`) for "last seen" (e.g. "2h ago", "—" when null) — admin
  lists read better relative than absolute. Created/Expires on invitations may use the same helper.
- Reuse `StatusBadge` (role/state), `ConfirmDialog`, `CodeField`, `useApi`, `errorText`, `withSudo`.
- **Tests:** each view (`*.test.ts`) — list render, the mutation flows (PUT/delete/revoke/reissue/
  invite/revoke-invite), error mapping, the attributes round-trip on PUT (assert the existing
  attributes are sent back), the sudo path on credential force-revoke. Extend `AppSidebar.test.ts`
  for the admin group (rendered when `isAdmin`, absent otherwise). A router-guard test:
  `requiresAdmin` + non-admin → redirect to `error`.
- **Done-gate (identical to prior slices):** `mise exec -- npm run test` green; `go build ./... &&
  go vet ./...` exit 0; smoke `SMOKE_EXIT=0` (the smoke's step-5b SPA-shell check still serves
  `/admin/*` via the Go `NotFound` fallback); rebuild + commit `pkg/webui/dist` once at the gate.

## Plan shape (~5 tasks)
1. Shell — vendor `Table` primitive + AppSidebar admin group + the three `requiresAdmin` routes + guard/forbidden test.
2. `AdminAccountsView` + tests (table list, row→detail nav, states).
3. `AdminAccountDetailView` + tests (identity PUT w/ attribute round-trip, passkey force-revoke [sudo], revoke-all-sessions, reissue, delete).
4. `AdminInvitationsView` + tests (list, create reveal-URL, revoke).
5. Done-gate: full vitest, go build/vet, smoke, rebuild+commit dist.

## Out of scope
- Attribute *editing* (display read-only in 3a).
- OIDC clients / SAML SPs (Spec 3b); upstream IdPs / signing keys / audit (Spec 3c).
- Recovery-code login flow (deferred, tracked).
- No backend changes — all endpoints exist.
