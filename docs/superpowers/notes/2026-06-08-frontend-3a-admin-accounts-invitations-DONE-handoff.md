# Handoff — Frontend Spec 3a (Admin shell + Accounts + Invitations) DONE

**Date:** 2026-06-08
**Branch:** `master` (no remote, no worktree — project convention: commit directly to master)
**HEAD:** `5c64724`
**Tree:** clean. Gate GREEN — vitest **121/121** across 29 files, `go build/vet ./...` exit 0, `cmd/smoke` **SMOKE_EXIT=0** (121 steps).

Read project memory first (`MEMORY.md` + `project_current_state.md`), then this note.

---

## Context — why this slice, and the gap analysis behind it

A code-verified survey (2026-06-08) of the 111-endpoint backend vs the rebuilt `dashboard/` found: **user self-service complete**; the **entire admin surface unimplemented** (the rebuild's Task 1 had deleted all admin pages); plus one non-admin gap (recovery-code login). The admin surface was sliced into three specs:
- **Spec 3a (this — DONE):** admin shell + **Accounts** + **Invitations**.
- **Spec 3b (next):** relying parties — **OIDC clients** + **SAML service providers**.
- **Spec 3c:** **upstream IdPs** (federation config) + **signing keys** + **audit events**.
- **Deferred (tracked, not in any admin slice):** recovery-code login (`/auth/recovery-code/verify` + `/auth/recovery/totp/{begin,verify}`) — a TOTP-only user with no passkey is locked out of the UI.

## What shipped this session (Spec 3a)

Six tasks via subagent-driven development (sonnet impl + spec-then-quality review per task + an opus final whole-slice review). All on the 2a/2b/2c patterns (`useApi`/`errorText`, `withSudo`, `ConfirmDialog`, `StatusBadge`, `CodeField`, `data-test`).

- **Foundation:** vendored shadcn **`Table`** primitive (`components/ui/table/`, token-styled `text-muted`/`bg-sunken`); **`lib/time.ts`** (`relativeTime` past-relative + future-clamp, `formatDateTime` absolute — use formatDateTime for FUTURE times like expiries); `admin.*` i18n namespace + 8 admin error codes.
- **`AdminAccountsView`** (`/admin/accounts`): accounts **Table** (User · Role · State · Last seen), **keyboard-activatable rows** (tabindex + Enter/Space) → detail; Invite → invitations.
- **`AdminAccountDetailView`** (`/admin/accounts/:id`): edit identity/role/disabled (**PUT round-trips existing `attributes`** — backend REPLACES them); **force-revoke passkeys** (the ONE sudo-gated op — retires the old "stub"); revoke-all-sessions; reissue-enrollment (reveals URL in CodeField); delete (danger zone); not-found + **load-error** states.
- **`AdminInvitationsView`** (`/admin/invitations`): list (copyable enrollment URLs), inline **create** (role select; stays open on failure), **revoke** (ConfirmDialog).
- **Wiring:** 3 `requiresAdmin` children of `DashboardLayout`; **`isAdmin`-gated Admin SidebarGroup** (Users/Ticket icons); guard regression test.

Spec: `docs/superpowers/specs/2026-06-08-frontend-3a-admin-accounts-invitations-design.md`. Plan (6 tasks, DONE): `docs/superpowers/plans/2026-06-08-frontend-3a-admin-accounts-invitations.md` (+`.tasks.json`).

## Verified backend contracts (authoritative — `pkg/contract/auth.go`; **`api.md` was STALE**)
Mutations are POST-with-body, NOT REST `/{id}` paths:
- `GET /accounts` → `[]AccountView{id,username,displayName,role,attributes?,disabled,createdAt,updatedAt,lastSignInAt?}`; `GET /accounts/{id}`; `PUT /accounts/{id}` `{username:'' (immutable), displayName, role, disabled, attributes}` (**REPLACES attributes — round-trip them**); `POST /accounts/delete {id}`; `GET /accounts/{id}/credentials`; `POST /accounts/credentials/delete {accountId,credentialId}` (**sudo**); `POST /accounts/revoke-sessions {id}`→`{revoked}`; `POST /accounts/reissue-enrollment {id}`→`{url,expiresAt}`.
- `GET /invitations` → `[]{token,url,role,attributes?,createdAt,expiresAt}` (full url in list); `POST /invitations {role}`→`{url,expiresAt}`; `POST /invitations/revoke {token}`.
- Only `accounts/credentials/delete` is `registerSudoOpHTTP` (sudo); everything else is `registerOp` (admin-role, no sudo). FE wraps all mutations in `withSudo` anyway (no-op unless server demands it).
- Errors: `last_admin`, `admin_cannot_be_disabled`, `cannot_delete_self`, `invalid_role`, `username_immutable`, `account_not_found`, `invitation_not_found`, `forbidden`.

## Review-caught issues (all fixed)
- **`relativeTime` year-bucket bug** (Critical, Task 1): 360–364-day-old timestamps rendered "0y ago" (month branch exits at 30×12=360 but year used floor(d/365)). Fixed: year exit guarded by `d < 365`; added month/year/boundary tests.
- **AccountsView rows mouse-only** (Task 2 a11y): added tabindex + Enter/Space handlers + a keyboard test.
- **DetailView load-error invisible** (blocker, Task 3): the error Alert was inside `v-else-if="account"`, so a non-404 load failure showed a blank page. Hoisted the Alert above the conditional + load-error test. Also cleared the `saved` banner on other mutations.
- **InvitationsView create-form closed on failure** (Task 4): moved `createOpen=false` inside `if (ok)` + a failure-keeps-form-open test; removed a no-op `min-w-0` on a `<td>`.
- en.ts Edit-tool delimiter corruption caught twice by the mandatory U+2018 grep (see [[reference_en_ts_apostrophe_edit_hazard]]) — the grep is baked into dispatch prompts now.

## Known minor follow-ups (non-blocking, from the final review)
- **Account-row screen-reader semantics are thin** — rows are keyboard-operable but a `<tr>` has no role/accessible-name conveying it's actionable. Proper fix (row-as-link) is a cross-cutting pattern with no sibling precedent; revisit if admin a11y becomes a focus.
- **`errorText` is duplicated across ~17 views** — the codebase norm (not slice drift); a composable extraction is a reasonable standalone cleanup ticket, not a blocker.
- `relativeTime` shows "12mo ago" for 360–364 days (codified, internally consistent).

## ▶ NEXT WORK
- **Spec 3b — relying parties: OIDC clients + SAML service providers.** Wire the existing admin API (`api.md`; backend `ce2fdf4`..`459c505`): OIDC clients (list/get/create reveal-once secret/update PUT/rotate-secret/delete) + SAML SPs (list/get/create+metadata-XML-ingest/update/reingest-metadata/delete). All mutations `registerSudoOpHTTP` (sudo). Add to the Admin SidebarGroup. Reuse the 3a patterns (Table, sudo gate, ConfirmDialog, CodeField for reveal-once secrets). **Verify exact contracts in `pkg/contract/auth.go` — api.md is stale.** Own brainstorm→spec→plan→subagent build.
- **Then Spec 3c** (upstream IdPs + signing keys + audit) and the **deferred recovery-code login**.
- **Open loop — visual review (still not done across the whole rebuild):** built without a screenshot tool. A live reload-and-react pass on the admin section (`/admin/accounts`, detail, `/admin/invitations`, the admin sidebar group) + the input-fill change is worthwhile. `mise dev-server` + `mise enroll-admin`; **`mise dev-seed`** seeds accounts/invitations/providers so lists render.

## Runtime / conventions / quirks (these bite)
- Frontend tooling from `dashboard/` with `mise exec -- npm …`; **cwd resets to repo root between some tool calls** — `cd` explicitly.
- Binary embeds the **committed** `pkg/webui/dist`; Vite hashes non-deterministic → source-only commits do `git checkout -- pkg/webui/dist && git clean -fq pkg/webui/dist` after a verify build; rebuild + commit dist once at the slice gate. (Reviewers running `npm run build` dirty the dist — discard before the next commit.)
- Go gate authoritative: `mise exec -- go build ./... && go vet ./...` exit 0.
- Smoke: `setsid bash /tmp/run_v06.sh`; poll `/tmp/v06.result` for `SMOKE_EXIT=0`. Resets the SMOKE DB (`postgres`@55432), NOT the dev DB. NEVER bare `pkill -f 'prohibitorum'` — use precise `pkill -f 'go-build.*/prohibitorum'`+`pkill -f 'cmd/prohibitorum'`.
- Backend `AuthError` messages are Chinese → FE maps every reachable code via `errors.<code>` in `locales/en.ts`; keep apostrophes curly (U+2019) and **run `grep -nP "\x{2018}" en.ts` after every edit**.
- NO git remote (push/PR N/A).
