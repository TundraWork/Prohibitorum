# Session handoff — Admin+User Dashboard: SPEC DONE, ready to plan

> Future Claude: this session BRAINSTORMED + SPEC'd the admin+user dashboard. The
> spec is committed; nothing is implemented yet. **Resume by: (1) let the user
> review the spec if they haven't, then (2) `superpowers-extended-cc:writing-plans`
> on the spec → (3) `subagent-driven-development`** (same rhythm as the prior two
> chunks). Do NOT re-brainstorm — decisions are locked below.

## State
```
HEAD: 0de7b3c   branch: master   working tree: clean   (NO git remote)
```
Prior chunk (Login+Consent UI) is DONE: `docs/superpowers/notes/2026-05-31-login-consent-ui-handoff.md`.

## What this chunk is
A **basic admin+user dashboard** making the whole **login → dashboard → logout**
flow manually browser-testable from a fresh DB. **Spec (LOCKED, D1–D10):**
`docs/superpowers/specs/2026-05-31-admin-user-dashboard-design.md`.

## Locked decisions (don't relitigate)
- **Frontend-only chunk** — every section consumes EXISTING backend APIs; the new
  routes are SPA paths served by the existing `NotFound`→`index.html` fallback. **No Go changes** (except a `cmd/smoke` shell-route assertion + a `mise dev-server` task).
- **Sidebar layout** (`DashboardLayout.vue`): left nav + header (LocaleSwitcher + user menu + Logout); admin nav group renders only when `role==='admin'`.
- **Routes:** `/` ProfileView, `/sessions`, `/credentials` (requiresAuth); `/admin/accounts`, `/admin/invitations` (requiresAuth+requiresAdmin); public `/enroll/:token` (no layout) + existing `/login /consent /logout /error`. Catch-all → `/` (guard bounces to /login if unauth).
- **Router `beforeEach` guard:** requiresAuth → ensure session (Pinia `ensureLoaded()`=cached fetchMe); none → `/login?return_to=<fullpath>`. requiresAdmin → also `role==='admin'` else `/`.
- **Enrollment page** `/enroll/:token`: `GET /api/prohibitorum/enrollments/{token}` (EnrollmentPreview) → "Register passkey" → `register/begin`→`startRegistration`→`register/complete`. **complete SETS the session cookie + returns SessionView (auto-login)** → on success `window.location.assign('/')`. Passkey only (no federation enroll). Add `passkeyRegister(token)` to `lib/webauthn.ts`.
- **Sections (existing APIs):** Profile (`/me`), Sessions (`/me/sessions` + revoke), Credentials (`/me/credentials` + rename/delete; no add-passkey), Admin Accounts (`ListAccounts` + UpdateAccount disable/enable + DeleteAccount + ReissueEnrollment→URL), Admin Invitations (`ListInvitations` + CreateInvitation→URL + RevokeInvitation).
- **NO sudo step-up needed** — verified `requireFreshSudo` guards only password/TOTP setup, identity linking, revoke-pwd-totp, device pairing (none in this dashboard).
- **Run/manual-test:** `mise dev-server` (npm run build → pkg/webui/dist, then `go run ./cmd/prohibitorum`) → http://localhost:8080. WebAuthn works over http://localhost (secure context). Manual script: `enroll-admin` → open /enroll/<token> → register → auto-login → dashboard → logout → /login → passkey login.

## Key backend facts already confirmed (so the plan is accurate)
- Enrollment `register/complete` auto-logins (`pkg/server/handle_enrollment.go:506` sets FreshSessionCookie; returns `{session: SessionView}`).
- Contracts: `AccountView{id,username,displayName,role,disabled,createdAt,updatedAt,lastSignInAt}`, `SessionListItem{id,isCurrent,issuedAt,expiresAt,lastSeenIp,userAgent}`, `CredentialView{id,credentialIdSuffix,nickname,transports,backupState,attestationType,createdAt,lastUsedAt}`, `EnrollmentPreview{intent,target{username,displayName},expiresAt}` (`pkg/contract/auth.go`).
- Admin huma ops registered in `pkg/server/server.go` ~lines 328–337 (ListAccounts/GetAccount/UpdateAccount/DeleteAccount/RevokeAccountSessions/ReissueEnrollment/CreateInvitation/ListInvitations/RevokeInvitation, `admin` requirement); `/me/*` raw-chi at ~287–325. Plan should confirm exact HTTP paths of the huma admin ops (they're under the `/api/prohibitorum` mgmt group).

## SPA infra to REUSE (from the login+consent chunk — read its handoff)
`dashboard/` = Vue 3 + Vite + Nuxt UI v4.8.1 + vue-i18n (zh+en) + Pinia + vue-router. `lib/api.ts` (`api.get/post`, `{code,message}`), `lib/returnTo.ts` (`safeReturnTo`), `stores/session.ts` (`fetchMe`), `lib/webauthn.ts` (`passkeyLogin`), `vitest.config.ts` has the `@nuxt/ui/vite` plugin for component tests. Conventions: `te('errors.'+code)?t():message`; explicit `type="button"`; `role="alert" aria-live="polite"`; `busy` re-entrancy guard.
**Embed: after ANY Vue change, `cd dashboard && npm run build` (→ pkg/webui/dist) and `git add pkg/webui/dist` — the binary embeds the COMMITTED dist.**

## Runtime quirks (unchanged — these bite)
- master, direct commits, NO git remote. opus for judgment-heavy/critical, sonnet for mechanical; never haiku.
- Trust `go build ./...` exit 0 + `go vet`, NOT gopls `<new-diagnostics>` (FALSE "no matching files" on `//go:embed all:dist`, sqlc "undefined", `DeleteExpiredSAMLSessions"). 
- NEVER `pkill -f 'prohibitorum'` bare (kills the PG at /tmp/prohibitorum-pg). Smoke: `setsid bash /tmp/run_v06.sh`, poll `/tmp/v06.result` for `DONE`/`SMOKE_EXIT=0`. Precise kills: `pkill -f 'go-build.*/prohibitorum'` + `pkill -f 'cmd/prohibitorum'`. Node via mise; npm (dashboard has package-lock).
- Visual-companion brainstorm artifacts live in `.superpowers/` (gitignored).

## Resume checklist
1. (Optional) confirm the user is happy with the spec.
2. `superpowers-extended-cc:writing-plans` on `docs/superpowers/specs/2026-05-31-admin-user-dashboard-design.md`.
3. `subagent-driven-development` to execute; per-task two-stage review; rebuild+commit `pkg/webui/dist`; final gate (go build/vet/test + vitest + smoke SMOKE_EXIT=0).
4. The deliverable's acceptance is the MANUAL browser walkthrough (D9 script) — surface it to the user to run.
