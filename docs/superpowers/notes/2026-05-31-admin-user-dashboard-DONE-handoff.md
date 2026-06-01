# Session handoff — Admin+User Dashboard: DONE (implemented, reviewed, gate green)

> Future Claude: this session IMPLEMENTED the admin+user dashboard (the chunk that
> the earlier `2026-05-31-admin-user-dashboard-handoff.md` had only SPEC'd). All 12
> plan tasks are done, per-task two-stage reviewed, a final whole-feature opus review
> passed, and the full gate is green. Committed directly to `master` (no remote).

## State
```
HEAD: fd89b0a   branch: master   working tree: clean   (NO git remote)
```
Plan: `docs/superpowers/plans/2026-05-31-admin-user-dashboard.md` (+ `.tasks.json`, all 12 marked completed).
Spec it implemented: `docs/superpowers/specs/2026-05-31-admin-user-dashboard-design.md` (D1–D10).

## What shipped
A sidebar admin+user dashboard SPA (Vue 3 + Nuxt UI v4, bilingual zh+en), embedded
same-origin in the Go binary via `pkg/webui/dist` (committed). **Frontend-only** — no
backend HTTP changes; new routes ride the existing `NotFound`→`index.html` SPA
fallback. The only Go-side additions are a `mise dev-server` task and a smoke
SPA-shell assertion (step 5b).

- **Session store** (`stores/session.ts`): `ensureLoaded()` (idempotent cached fetchMe via a `loaded` flag), `isAdmin`, `clear()` (now called by LogoutView). `lib/api.ts` gained `put`.
- **`passkeyRegister(token, fields)`** in `lib/webauthn.ts` — enrollment ceremony (begin→startRegistration→complete), returns `result.session`.
- **Views** (`pages/`): ProfileView `/`, SessionsView `/sessions` (revoke), CredentialsView `/credentials` (rename + two-step delete + Last-used col), AccountsView `/admin/accounts` (disable/enable PUT, delete, reissue→CopyableUrl), InvitationsView `/admin/invitations` (create {role} + revoke + Created col), EnrollView `/enroll/:token`.
- **`DashboardLayout` + `AppSidebar`** — sidebar shell, header (app name, LocaleSwitcher, displayName, Logout), admin nav group gated on `session.isAdmin`.
- **Router** (`router.ts`): nested DashboardLayout routes (meta `requiresAuth`/`requiresAdmin`); pathless `CenteredLayout` parent for the public auth pages (`/login /consent /logout /error`); `/enroll/:token` standalone; catch-all → `/`. `installGuard(router)` `beforeEach`: requiresAuth→`ensureLoaded`→`/login?return_to=`; requiresAdmin→`isAdmin` else `/`. Exported for tests.
- **Chrome refactor**: `App.vue` reduced to `<UApp><RouterView/></UApp>`; the old centered chrome moved into the new `CenteredLayout.vue` (so the sidebar isn't trapped in a max-w-md card).
- **`CopyableUrl.vue`** shared component (readonly input + clipboard copy). No `ConfirmButton` — destructive actions use inline two-step confirms (so `data-test` lands on real buttons).

## Spec corrections made during implementation (the plan documents these up top)
- `/me/*` + admin ops are **huma** (not raw-chi); list endpoints return **top-level arrays**.
- `UpdateAccount` is **PUT /accounts/{id}** with body `{displayName, role, disabled}` — username immutable; disable/enable resends current displayName+role. (Added `api.put`.)
- **Enrollment begin REQUIRES username+displayName for bootstrap & invite** (only reset uses empty body + has a target). The manual-test path (`enroll-admin`) is **bootstrap** with no target → EnrollView collects username+displayName inputs. (Spec D5 had assumed a target always exists — corrected.)
- Enrollment `register/complete` returns `{session, newCredentialId}` (not a bare SessionView).

## Verification (gate — all green)
- `go build ./... && go vet ./...` exit 0; `go test ./...` all pass.
- Frontend: `cd dashboard && npx vitest run` → **35 tests, 14 files, all pass**.
- Full smoke (`setsid bash /tmp/run_v06.sh`): **SMOKE_EXIT=0**; new step 5b asserts `/ /sessions /credentials /admin/accounts /enroll/<token>` all return 200 + `id="app"` (see `/tmp/smoke-v06.log:26-27`).
- `pkg/webui/dist` rebuilt + committed (commit 51dbd57 first build; e8b5ef6 final rebuild after review fixes).

## Commits (877d4a0 baseline → fd89b0a)
15 task/fix commits (43c33d9 store … 51dbd57 router+dist … e8b5ef6 final-review fixes) + fd89b0a (plan docs). See `git log --oneline 877d4a0..HEAD`.

## What is NOT covered (honest limitations / candidates next)
- **No headless-browser e2e** — the click-through is manual (the D9 walkthrough). A **Playwright suite** is the natural follow-up and closes the real-browser gap.
- Out of scope this chunk (unchanged): sudo step-up, add-passkey on credentials, federation enrollment, identities/device-pairing UI, account create-from-scratch.
- Minor, non-blocking (from the final review, deliberately left): no HTML5 `required` on EnrollView identity inputs (backend validates); `RouteMeta` not type-augmented (guard truthiness works); empty `<script setup>` in App.vue.

## The deliverable's acceptance — the MANUAL browser walkthrough (D9)
Needs a human + a real passkey ceremony (WebAuthn works over http://localhost). From a fresh DB:
1. `mise dev-server` → serves :8080. (As of commit b18b070 it's **self-contained**: dev defaults for `PROHIBITORUM_DATABASE_URL` (localhost:55432 dev PG) + `PROHIBITORUM_PUBLIC_ORIGIN=http://localhost:8080`, and a stable data-encryption key auto-generated into `.dev/encryption-key` (gitignored). Override the DB/origin via env if needed. Server auto-migrates on boot. Caveat: defaults to the same dev PG the smoke wipes.)
2. `go run ./cmd/prohibitorum enroll-admin` → open the printed `/enroll/<token>`.
3. Type username + display name → Register passkey → auto-login → `/` Profile (admin sidebar group visible).
4. Exercise Sessions/Passkeys + Admin Accounts/Invitations (revoke/rename/disable/reissue/create-invite).
5. Logout (header) → `/login` → sign in with the passkey → back to `/`.

## Runtime quirks (unchanged — these bite)
- master, direct commits, NO git remote. opus for judgment-heavy/review, sonnet for mechanical; never haiku.
- Trust `go build ./...` exit 0 + `go vet`, NOT gopls `<new-diagnostics>` (false positives on `//go:embed all:dist`, sqlc, arg counts).
- NEVER `pkill -f 'prohibitorum'` bare (kills the dev PG at /tmp/prohibitorum-pg). Smoke: `setsid bash /tmp/run_v06.sh`, poll `/tmp/v06.result` for `DONE`/`SMOKE_EXIT=0`; full step log in `/tmp/smoke-v06.log`. Node via mise; dashboard uses **npm** (package-lock).
- **After ANY Vue edit: `cd dashboard && npm run build` then `git add pkg/webui/dist`** — the binary embeds the COMMITTED dist.
