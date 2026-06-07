# Handoff — Frontend Spec 2c (Connected accounts + Devices + new-device Pairing) DONE

**Date:** 2026-06-08
**Branch:** `master` (no remote, no worktree — project convention: commit directly to master)
**HEAD:** `93d29df`
**Tree:** clean. Gate GREEN — vitest **91/91** across 24 files, `go build ./... && go vet ./...` exit 0, `cmd/smoke` **SMOKE_EXIT=0** (121 steps).

Read project memory first (`MEMORY.md` + `project_current_state.md`), then this note.

---

## What shipped this session

Spec 2c of the shadcn-vue frontend rebuild — three pages + wiring, all on the established Spec 2a/2b patterns (`useApi` `busy`/`error`/`run`, the sudo gate `withSudo`/`ensureSudo`+`SudoModal`, `errors.<code>` i18n mapping, `Card`/`Alert` structure, `data-test` hooks, `min-w-0`/`truncate` chain).

- **`pages/ConnectedAccountsView.vue` (`/connected`)** — list federated identities (`GET /me/identities`); **unlink** via `ConfirmDialog` + `withSudo(POST /me/identities/{id}/unlink)` (surfaces `last_sign_in_method`/`credential_not_found`); **link** a provider (`GET /auth/federation`, disable already-linked slugs) via **proactive `ensureSudo()` then `hardRedirect('/me/identities/link/{slug}/begin?return_to=%2Fconnected')`** — the begin endpoint is a sudo-gated 302 that `withSudo`'s XHR-retry can't replay.
- **`pages/DevicesView.vue` (`/devices`, approver side)** — plain `Input` for the `XXXX-XXXX` code → `GET /me/devices/pair/lookup?code=` → confirmation card (initiator UA/IP/created/expires + `displayCode` echoed via `CodeField`) → **Approve** (`withSudo(POST .../approve)`) / **Cancel** (`POST .../cancel`). Handles `alreadyBound` (no approve button) and `rate_limited`. Both `approve` and `cancel` gate state-reset on `if (ok)`; approve clears the code field on success.
- **`pages/PairDeviceView.vue` (`/pair`, public new-device side, `CenteredLayout`)** — `POST .../begin` on mount → show `displayCode` + countdown → **poll** `GET .../status?id=` every `POLL_MS=2500` → on `approved` `POST .../complete` (sets session cookie) → **success step** offering a **skippable** local-passkey registration (`register/begin` → `useWebauthn().register()` → `register/complete`), then `router.push('/')`. Poll loop is **mounted-guarded** (no completion fires after navigation), **re-entrancy-guarded** (`polling` flag + `phase!=='pending'`), cleared on unmount/approved/expired, and **resumes polling if `complete()` fails** (no wedge). `expired` → "Generate a new code" re-begins.
- **Wiring** — routes (`/connected`+`/devices` as `requiresAuth` children of `DashboardLayout`; public `/pair`); sidebar `accountItems` order **Profile · Security · Sessions · Connected · Devices** (`Link2`/`TabletSmartphone` icons); `/login` → `/pair` "New device? Pair it" link; i18n `connected.*`/`devices.*`/`pair.*` + `nav.connected`/`nav.devices`/`login.pairDevice` + errors `last_sign_in_method`/`credential_not_found`/`pairing_not_found`/`pairing_expired`/`pairing_not_approved`/`pairing_state` (`rate_limited` already existed).

Spec: `docs/superpowers/specs/2026-06-08-frontend-2c-connected-devices-design.md`. Plan (5 tasks, all DONE): `docs/superpowers/plans/2026-06-08-frontend-2c-connected-devices.md` (+`.tasks.json`).

## Verified backend contracts (canonical — read from the live handlers this session)
- `pkg/server/handle_me_identities.go`: list `[{id, idpSlug, idpDisplayName, upstreamEmail?, linkedAt}]`; `unlink` 204 sudo (rejects `last_sign_in_method`, 404 `credential_not_found`); `link/{slug}/begin` 302 sudo; `link/{slug}/callback` 302 no-sudo (server-side, FE just points `return_to`).
- `pkg/server/handle_pairing.go`: approver `lookup` (20/min, not sudo, foreign/expired→`pairing_not_found`, `alreadyBound`=approved-for-this-account), `approve` (sudo **and** 10/min), `cancel` (not sudo). New-device `begin`→`{pairingId,code,displayCode,expiresAt}`, `status?id=`→`{status:pending|approved|expired}` (not-found→expired), `complete {pairingId}`→`{session}`+cookie (`pairing_not_approved` 428).
- `pkg/server/handle_federation.go`: `GET /auth/federation`→`[{slug,displayName}]`.

## Process / quality trail
Workflow: brainstorming (re-presented the paused 2c design + 2 AskUserQuestion forks — user expanded scope to INCLUDE the new-device flow, chose short "Connected"/"Devices" nav labels, "offer-skippable" post-pair passkey, public `/pair`+login link) → writing-plans (5 bite-sized tasks, test code grounded in the real conventions) → subagent-driven-development (sonnet impl + spec-compliance then code-quality review per task; opus final whole-slice review).

Real issues caught and fixed by review (not cosmetic): a **`cancel()` cleared state on error** (Critical, Task 2 — now `if (ok)`-gated + regression test); the **poll state-machine post-unmount completion + complete-failure wedge** (I1/I2, Task 3 — `mounted` guard + resume-on-failure + 2 regression tests); a **discarded `useWebauthn` error** (M1, Task 3 — folded into `errorText`); `encodeURIComponent` on the link `return_to`; provider-load flash guard; several missing test assertions (`credential_not_found`, the pair-link). Final opus review: **ready to finish**, 0 Critical/Important. The one actionable nit (stale code after approve) was fixed (`93d29df`).

**Recurring hazard recorded to memory ([[reference_en_ts_apostrophe_edit_hazard]]):** the Edit tool corrupted `en.ts` straight-quote string delimiters into curly U+2018 **twice** this slice. Rule now in the memory + baked into dispatch prompts: after any `en.ts` edit, `grep -nP "\x{2018}"` (must be 0) and `grep -nP ":\s*\x{2019}"` (must be 0). Final state verified: 0 / 0, 18 legit in-text U+2019.

## ▶ NEXT WORK
- **Spec 3 — Admin.** Wire the **already-built admin HTTP API** (Admin Management API chunk, `ce2fdf4`..`459c505`; routes in `api.md`) into dashboard pages: Accounts + `/admin/accounts/:id` detail (role/disable PUT, revoke-sessions, reissue, delete), Invitations, and the 5 ex-"Planned" areas (OIDC clients / SAML SPs / signing keys / audit / settings). Per-credential force-revoke is backable via `GET /accounts/{id}/credentials`. Reuse Spec 2a/2b patterns (sudo gate, `StatusBadge`, config-driven nav gated on `auth.isAdmin`, `errors.<code>`+`Alert`). Own brainstorm→spec→plan→subagent-driven build.
- **Open loop — visual review (still not done).** The whole rebuild (Spec 1/2a/2b/2c + polish) was built WITHOUT a screenshot tool. A live reload-and-react pass on `/connected`, `/devices`, `/pair`, the sidebar (now 5 items), and the `/login` pair link is worthwhile. `mise dev-server` + `mise enroll-admin`; **`mise dev-seed`** seeds upstream IdP providers so the federation buttons + connected-accounts link options RENDER (otherwise empty). Note: `/dev` console, dev-seed admin pages, and the old admin surface return with Spec 3.
- Deferred (unchanged): `009` migration after soak; Playwright e2e; v0.7+ hardening; prune any now-unused `nav.*` i18n keys at the i18n pass; zh-CN locale.

## Runtime / conventions / quirks (these bite)
- Frontend tooling from `dashboard/` with `mise exec -- npm …`; **cwd resets to repo root between some tool calls** — `cd` explicitly. Don't pipe `npm run test` to `tail` in a chain that gates a commit (masks failure).
- Binary embeds the **committed** `pkg/webui/dist` via go:embed. **Vite chunk hashes are non-deterministic** → for source-only commits, `git checkout -- pkg/webui/dist && git clean -fq pkg/webui/dist` after a verify build; rebuild + commit dist once at the slice's done-gate.
- Go gate authoritative: `mise exec -- go build ./... && go vet ./...` exit 0 (trust over gopls — see memory for the stale-IDE-gopls root cause).
- Smoke: `setsid bash /tmp/run_v06.sh`; poll `/tmp/v06.result` for `SMOKE_EXIT=0` (full log `/tmp/smoke-v06.log`). The runner resets the **smoke** DB (`postgres`@55432), NOT the dev DB. NEVER bare `pkill -f 'prohibitorum'` (kills the dev PG) — use precise `pkill -f 'go-build.*/prohibitorum'`+`pkill -f 'cmd/prohibitorum'`.
- Backend `AuthError` messages are Chinese → FE maps every reachable code via `errors.<code>` in `locales/en.ts`; keep apostrophes curly (U+2019) and run the U+2018 grep after edits.
- NO git remote (push/PR N/A).
