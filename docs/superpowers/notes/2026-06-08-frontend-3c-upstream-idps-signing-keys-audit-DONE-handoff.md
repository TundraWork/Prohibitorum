# Handoff — Frontend 3c: Upstream IdPs + Signing keys + Audit log DONE

**Date:** 2026-06-08
**Branch:** `master` (no remote, no worktree — commit directly to master).
**HEAD:** `af986a7` (last impl/chore); feature commits `1795364`..`4cbb6d3` + finishing commits (not-found fix + dist rebuild).
**Tree:** clean. Done-gate GREEN — vitest **177/177** (38 files), `go build/vet ./...` exit 0, `vue-tsc --noEmit` exit 0, `cmd/smoke` **SMOKE_EXIT=0** (121 steps), `pkg/webui/dist` rebuilt + committed (all four 3c chunks present).

Read project memory first (`MEMORY.md` + `project_current_state.md`), then this note.

---

## What shipped — the LAST admin slice of the frontend rebuild
Spec 3c closes the final three admin areas, reaching admin parity with the backend. **Frontend-only — no Go changes.** All backends were already complete (Admin Management API). New views are `requiresAdmin` children of the existing `DashboardLayout`; new nav items extend `adminItems` in `AppSidebar.vue`. Admin group is now: Accounts · Invitations · OIDC clients · SAML providers · **Upstream IdPs · Signing keys · Audit log**.

5 tasks via subagent-driven dev (sonnet impl + spec-then-quality review per task + a final opus whole-cycle review). Reviews caught + fixed real issues (see below).

- **Task 1 — Foundation** (`1795364`): i18n `admin.upstream/signingKeys/audit.*` + 3 nav labels + 3 error codes (`upstream_idp_not_found`, `upstream_idp_already_exists`, `active_key_no_replacement`); 4 `requiresAdmin` routes; 3 sidebar items (`Network`/`KeySquare`/`ScrollText`); guard regression test. **Created 4 minimal stub view files** so Vite import-analysis resolves the router's lazy imports during vitest (overwritten in Tasks 2–5).
- **Task 2 — `AdminUpstreamIdpsView`** (`479a8c6`): list + inline create (sudo, write-only `clientSecret` password input, `mode` select). `export interface UpstreamIdp` (imported by Task 3). Review fixes: `autocomplete=off` on displayName, `mode` literal-union type, empty-state-hidden-while-create test.
- **Task 3 — `AdminUpstreamIdpDetailView`** (`3b5ff36`): edit (PUT, **no clientSecret**, +disabled), rotate-secret (POST 204, no reveal), delete (ConfirmDialog). Review fixes: hard `clickConfirm` helper (removed an if/else fallback that could silently pass), RouterLink stub (warn noise), +2 coverage tests (generic load-error, no-nav-on-delete-failure).
- **Task 4 — `AdminSigningKeysView`** (`5d89e78`): list with status badges (pending→neutral, active→success, decommissioning→caution, retired→neutral) + expandable public JWK; lifecycle generate (no body) / activate (pending only) / retire (decommissioning only) via sudo-gated ConfirmDialogs; surfaces `active_key_no_replacement` (409). **Deviation:** inline `@update:open` arrows assigning `''` hit a Vue template-parser error → extracted named close-handlers. Review fixes: retired-row gating coverage, static `colspan`, removed 3 dead i18n keys.
- **Task 5 — `AdminAuditView`** (`4cbb6d3`): filterable (factor/event/accountId/since/until), keyset-paginated (newest-first, **load-more via `before=<lastId>`**, append, hide when `< limit`), per-row expandable pretty-JSON detail. Review fixes: **`hasMore` reset in `reload()`** (a real bug — stale Load More after a failed Apply could send `before=<old id>` with new filters), append-assertion test, row `role=button`/`aria-expanded`/`aria-label` (a11y; used the otherwise-dead `audit.expand` key).
- **Final opus whole-cycle review** caught the **not-found double-render** in `AdminUpstreamIdpDetailView` (both the red Alert AND the muted paragraph rendered on `upstream_idp_not_found`) → guarded the Alert with `&& !notFound`, converging on the `AdminSamlProviderDetailView` idiom. Fixed before the gate.

Spec: `docs/superpowers/specs/2026-06-08-frontend-3c-upstream-idps-signing-keys-audit-design.md`. Plan (5 tasks, DONE): `docs/superpowers/plans/2026-06-08-frontend-3c-upstream-idps-signing-keys-audit.md` (+`.tasks.json`). Spec `77c0c3e`, plan `33d33ed`.

## Verified contracts (authoritative — `pkg/contract/auth.go` + handlers; `api.md` STALE)
- **Upstream IdPs** (`/api/prohibitorum/upstream-idps`): `GET`→`UpstreamIDPView[]`; `GET /{slug}`→one (404 `upstream_idp_not_found`); `POST` (sudo) `{slug,displayName,issuerUrl,clientId,clientSecret,mode,scopes[],allowedDomains[],usernameClaim,displayNameClaim,emailClaim,requireVerifiedEmail}`→201 (409 `upstream_idp_already_exists`); `PUT /{slug}` (sudo) **same minus clientSecret, plus disabled**→200; `POST /rotate-secret` (sudo) `{slug,clientSecret}`→**204**; `POST /delete` (sudo) `{slug}`→204. View never carries a secret. `mode ∈ {auto_provision,invite_only,link_only}` (`pkg/federation/oidc/modes.go`).
- **Signing keys** (`/api/prohibitorum/signing-keys`): `GET`→`SigningKeyView[]`; `POST /generate` (sudo, **no body**)→201 pending; `POST /{kid}/activate` (sudo)→200 (404 `credential_not_found`); `POST /{kid}/retire` (sudo)→200 (404 `credential_not_found`; **409 `active_key_no_replacement`**). `SigningKeyView = {kid,algorithm,use,status,publicJwk,notBefore?,activatedAt?,decommissionedAt?,retireAfter?}`; `status ∈ {pending,active,decommissioning,retired}`; **no `privatePem`**.
- **Audit** (`/api/prohibitorum/audit-events`, admin-read, **NO sudo**): query `factor?,event?,accountId?(int),since?(RFC3339),until?(RFC3339),before?(int64 keyset),limit?(default 50,1..200)`→`AuditEventView[]` **newest-first**, **no nextCursor** (client uses last row's `id` as next `before`). `AuditEventView = {id,at,accountId?,factor,event,ip?,userAgent?,detail?}`.

## ▶ NEXT WORK
With 3c done, the rebuilt dashboard has **full admin parity with the backend**. Remaining items (all pre-existing/tracked):
- **D — OTP/password invitations** (deferred, BACKEND): enrollment is passkey-only; "invite requires password/TOTP setup" needs a credential-requirements column on `enrollment` + new ceremony endpoints + frontend. Medium-high.
- **E — SAML-as-login subsystem** (deferred, MAJOR backend): no SAML *relying-party* login flow (we're only the SAML IdP); federation-as-login is OIDC-only. Needs ACS callback + assertion validation + account linking + upstream-SAML config. Enables SAML invites.
- **Live visual review:** FOLDED INTO this cycle's done-gate — the dev env was set up (`mise dev-server` + `mise dev-seed` + `mise enroll-admin --new`) and a reload-and-react checklist handed to the user for the three 3c pages + a sweep of the earlier rebuilt surface. (No screenshot tool — the user is the visual verifier.) Record the outcome / any follow-ups when known.
- Pre-existing, out-of-cycle: `AdminOidcClientDetailView.vue` has the SAME not-found double-render the final review fixed in the upstream view — fix opportunistically. The `lines()` helper is duplicated across admin views (extract a shared `lib/text.ts` when convenient). The `credential_not_found` i18n copy ("That connection no longer exists.") reads slightly off for a missing signing-key kid (near-unreachable edge case; shared code — not changed).
- Deferred infra: `009` migration (drop legacy signing-key columns) after a soak; Playwright e2e; v0.7+ hardening (HSM/KMS, SAML front-channel SLO, assertion encryption, upstream refresh-token storage, password breach-list, audit SIEM).

## Process notes / quirks (these bit this cycle)
- **Vite import-analysis vs lazy routes:** adding routes that lazy-`import()` not-yet-created view files breaks vitest transform (unlike `vue-tsc`, which ignores dynamic imports). Fix used: commit minimal stub views in the foundation task, overwrite them in the per-view tasks.
- **Vue template-parser quirk:** an inline `@event` arrow whose body assigns an EMPTY STRING (`x = ''`) fails to compile. Use a named handler (Task 4 did this). Assigning `false`/other literals is fine.
- **ConfirmDialog teleports to `document.body`** — tests must query `document.body` (not the wrapper) for its confirm button; the confirm button carries the `bg-destructive` class. (A reviewer suggested wrapper-scoped `w.findAll` — that would NOT find the teleported button; rejected.)
- **Dist rebuild is the done-gate step, not per-task** — Vite chunk hashes are non-deterministic, so dist is rebuilt + committed ONCE at the end. The final review correctly flagged the (then-)stale dist as Critical; resolved at the gate. A verify-only `npm run build` dirties dist — `git checkout -- pkg/webui/dist` to discard.
- **en.ts apostrophe guard** ran after every en.ts edit (Tasks 1, 4) — clean each time.
- **Coordinator `.tasks.json` sync** committed as small `chore(plan)` commits to keep the tree clean for each reviewer's `git status` check.
- gopls staleness, NO git remote, never bare `pkill -f prohibitorum` — unchanged (see project memory).
