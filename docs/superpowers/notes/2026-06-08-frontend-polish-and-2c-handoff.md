# Handoff — Frontend design-polish pass done; Spec 2c (Connected + Devices) paused mid-brainstorm

**Date:** 2026-06-08
**Branch:** `master` (no remote, no worktree — project convention: commit directly to master)
**HEAD:** `5b522ce`
**Tree:** clean. Gate GREEN — vitest 68/68, `go build/vet/test ./...` exit 0, `cmd/smoke` `SMOKE_EXIT=0`.

Read project memory first (`MEMORY.md` + `project_current_state.md`), then this note.

---

## Where the frontend rebuild stands

The from-scratch shadcn-vue rebuild has shipped, on `master`:
- **Spec 1** (threshold pages): `/login` (passkey + password/TOTP + federation), `/consent`, `/logout`, `/error`, `/enroll/:token`.
- **Spec 2a** (auth shell): `DashboardLayout` + vendored Sidebar + config `AppSidebar`; the reusable **sudo gate** (`lib/sudo` `withSudo`/`ensureSudo` + `SudoModal` over `/me/sudo/*`); read-only `/` (Profile) + `/sessions`.
- **Spec 2b** (Security): `/security` — Passkeys / Password / TOTP(QR) / Recovery cards + coarse revoke, all behind the sudo gate; building blocks `CodeField` / `RecoveryCodesDisplay` / `ConfirmDialog` / `TotpQr`.
- **Design-polish pass** (this session, commits `2d528b5`..`5b522ce`) — see below.

Specs/plans live in `docs/superpowers/{specs,plans}/2026-06-07-frontend-rebuild-*`.

## The design-polish pass (this session)

**Root-cause fix (`2d528b5`) — the important one.** The UI looked like a flat "figma sketch" because the shadcn semantic color utilities (`bg-primary`, `bg-card`, `bg-popover`, `text-primary-foreground`, `ring-ring`, `border-input`, `bg-sidebar*`) **were never generated**: the semantic vars lived only in `:root`, never bridged into Tailwind v4's `@theme`. Buttons had no fill, dialogs were transparent, inputs had no borders/focus ring. Fix = the canonical `@theme inline` bridge mapping the COMPLETE token set (incl. `chart-1..5` → brand hues) in `dashboard/src/assets/main.css`, plus a base `border-color` default (v4 defaults plain `border` to currentColor). **Full structure + two CSS-lexer gotchas (`*/` and apostrophes in comments near `@theme`) are recorded in the `reference-tailwind-shadcn-token-bridge` memory — read it before touching main.css.**

**Then richer polish, all within the calm/warm brand:**
- `317ad86` — SessionsView row overflow (the `min-w-0` truncation-chain bug the user spotted; the rule: every flex ancestor of a `truncate` child needs `min-w-0`, fixed sibling needs `shrink-0`). Also added the missing OR divider above federation (`OrDivider`).
- `02c6774` — confident layered "Drenched" `AuthBackdrop` + lighter scrim; title hierarchy (dashboard `text-2xl`, threshold card titles `text-xl`).
- `529066e` — `prefers-reduced-motion` guard (a11y mandate, was missing), Tide `::selection`, sidebar hover/active Tide wash (was gray), richer brand lockup + footer identity (name+role), sticky header, roomier padding.
- `c609fea` — Security revoke card as a rose **danger zone**; Profile role as a `StatusBadge`.
- `5b522ce` — sudo modal Tide ShieldCheck lockup + Fingerprint passkey button; **dist rebuilt + committed**.

**Caveat I worked under:** no browser-screenshot tool available — the user is the visual verifier. They confirmed the foundational fix looked "all good," authorized "polish + push richer," and the polish above was done blind-but-careful (build + vitest + reasoning). **Next session: ask the user to reload (`mise dev-server` embeds the committed dist; Vite dev = HMR) and react; tighten precisely what they flag.** PRODUCT.md is deliberately restrained ("warmth through space/tone, not decoration") — the user wants richer than that but it's still a calm IdP, not a consumer app.

## ▶ NEXT WORK: resume Spec 2c (Connected accounts + Devices) — paused mid-brainstorm

Spec 2c was being brainstormed when the user pivoted to the polish pass. **Status: contracts fully explored, design proposed to the user, but NOT yet approved / written to a spec doc.** Resume by re-presenting the design (below) for approval → write spec → writing-plans → subagent-driven build. No spec/plan/tasks file exists yet for 2c.

**Verified backend contracts (canonical — already read from handlers):**
- Connected accounts (`pkg/server/handle_me_identities.go`):
  - `GET /api/prohibitorum/me/identities` → `[{id, idpSlug, idpDisplayName, upstreamEmail?, linkedAt}]`
  - `POST /api/prohibitorum/me/identities/{id}/unlink` → 204, **sudo-gated**, rejects `last_sign_in_method`.
  - `GET /api/prohibitorum/me/identities/link/{slug}/begin?return_to=` → **302 to upstream, sudo-gated** (a redirect can't be retried → FE must `ensureSudo()` PROACTIVELY, then `hardRedirect`).
  - `GET …/link/{slug}/callback` → 302 back to `return_to` (no sudo; completes the begin ceremony).
  - Available providers to link: reuse public `GET /api/prohibitorum/auth/federation` → `[{slug,displayName}]`; disable already-linked slugs.
- Devices / pairing (authed approver side, `pkg/server/handle_pairing.go`):
  - `GET /api/prohibitorum/me/devices/pair/lookup?code=` → `{pairingId, displayCode, initiatorUa, initiatorIp, createdAt, expiresAt, alreadyBound}` (rate-limited 20/min, not sudo).
  - `POST /api/prohibitorum/me/devices/pair/approve {code}` → 204, **sudo-gated** (binds a device).
  - `POST /api/prohibitorum/me/devices/pair/cancel {code}` → 204 (not sudo).
  - No persistent device list — pairing issues a session (shows under `/sessions`). Spec 2c = the "approve a device by code" flow only; the anonymous new-device begin/status/complete side is separate (not in 2c).
  - Add error i18n: `last_sign_in_method`, `pairing_not_found`, `pairing_not_approved`, `pairing_expired`.

**Proposed design (re-present for approval):** two pages, one slice.
- `pages/ConnectedAccountsView.vue` (`/connected`): list linked identities (provider, optional upstream email, linked date — truncate long values); **unlink** via `ConfirmDialog` + `withSudo` (XHR retry); `last_sign_in_method` surfaced. **Link** = providers from `/auth/federation` (disable already-linked) → `ensureSudo()` then `hardRedirect('/me/identities/link/{slug}/begin?return_to=/connected')`.
- `pages/DevicesView.vue` (`/devices`): explainer → code input → `lookup` → a confirmation card showing the initiator device/IP/created/expires + echoed code → **Approve** (`withSudo` approve) / **Cancel**. The lookup→approve flow IS the confirmation (sudo adds friction); no extra dialog.
- Shell: add `/connected` + `/devices` children to `DashboardLayout` (requiresAuth); add **Connected accounts** + **Devices** to `AppSidebar` `accountItems` (order: Profile · Security · Sessions · Connected · Devices). i18n `connected.*` + `devices.*`. Done-gate identical to prior slices.
- Plan shape (~3 tasks): ConnectedAccountsView, DevicesView, routes+nav+done-gate.

**Then:** Spec 3 (Admin) — accounts/detail/invitations + wire the 5 ex-Planned admin pages to the already-built admin HTTP API (OIDC clients / SAML SPs / signing keys / audit / settings). The admin HTTP API exists (Admin Management API chunk, `ce2fdf4`..`459c505`).

## Runtime / conventions / quirks (these bite)
- Run frontend tooling from `dashboard/` with `mise exec -- npm …`. **Watch your cwd** — it resets to repo root between some tool calls; `npm` fails at root (no package.json). Don't pipe `npm run test` to `tail` in a chain that gates a commit (masks failure).
- Binary embeds the COMMITTED `pkg/webui/dist` via go:embed. **Vite chunk hashes are non-deterministic** → for source-only commits, `git checkout -- pkg/webui/dist && git clean -fq pkg/webui/dist` after a verify build; rebuild + commit dist once at a slice's done-gate.
- Go gate authoritative: `mise exec -- go build ./... && go vet ./...` exit 0 (trust over gopls).
- Smoke: `setsid bash /tmp/run_v06.sh`; poll `/tmp/v06.result` for `SMOKE_EXIT=0` (full log `/tmp/smoke-v06.log`). NEVER bare `pkill -f 'prohibitorum'` (kills dev PG). The runner resets the dev DB schema.
- Backend `AuthError` messages are Chinese → FE maps every reachable code via `errors.<code>` in `locales/en.ts`; keep `en.ts` apostrophes curly (U+2019) and avoid them in CSS comments.
- Workflow: brainstorming → writing-plans → subagent-driven-development (sonnet impl + 2-stage review per task + opus final review) → finishing-a-development-branch. NO git remote (push/PR N/A).
