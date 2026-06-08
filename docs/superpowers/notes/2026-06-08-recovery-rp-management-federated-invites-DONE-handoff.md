# Handoff — Account recovery + RP management + Federated invites (combined cycle) DONE

**Date:** 2026-06-08
**Branch:** `master` (no remote, no worktree — commit directly to master).
**HEAD:** `9a18962`
**Tree:** clean. Gate GREEN — vitest **152/152** across 34 files, `go build/vet ./...` exit 0, `cmd/smoke` **SMOKE_EXIT=0** (121 steps), dist rebuilt+committed.

Read project memory first (`MEMORY.md` + `project_current_state.md`), then this note.

---

## Why this was one cycle
The user asked for less spec→plan→build ceremony per slice. We **batched three backend-ready areas** into one spec/plan/cycle (one brainstorm, one spec, one plan, one done-gate/handoff) while keeping the per-task TDD + two-stage review. The non-amortizable cost (per-task review) is unchanged; the amortizable cost (brainstorm/spec/finishing) was paid once instead of three times. Two backend-heavy areas were deliberately **deferred** (D, E below).

## What shipped (10 tasks, subagent-driven: sonnet impl + spec-then-quality review/task + opus final)
- **A — Account recovery (frontend; backend was already complete).** `components/custom/AccountRecovery.vue` (props `partialToken`; emits `success`/`restart`): recovery code → `/auth/recovery-code/verify` → re-enroll TOTP (`/auth/recovery/totp/{begin,verify}` via `TotpQr`+`CodeField`) → `RecoveryCodesDisplay` → session. Reached via a **"Lost your authenticator?"** link in `PasswordTotpForm`'s TOTP step (reuses the captured `partial_session_token`). A failed recovery code emits `restart` (token spent); a failed `begin` shows a retry. **Only password+TOTP accounts can recover** (passkey/federation-only → admin reissue-enrollment).
- **B — Relying-party management (frontend; backend complete).** `AdminOidcClientsView` (list + create reveal-once secret) + `AdminOidcClientDetailView` (edit PUT[**`allowedScopes`**, create used `scopes`], rotate-secret reveal, delete); `AdminSamlProvidersView` (list + create with **metadata-paste AND manual-ACS** modes — repeatable ACS rows, mutually-exclusive default, contiguous indices) + `AdminSamlProviderDetailView` (edit flags PUT, reingest-metadata, read-only ACS/certs, delete).
- **C — Federated (OIDC) invites.** Backend: `POST /invitations` accepts optional `expectedUpstreamIdpSlug` → enrollment template; `InvitationView` returns it (+ a test seam `invitationOverride` mirroring `listFedOverride`; 5 Go tests). Frontend: `AdminInvitationsView` create form gains a **"Require sign-up via [upstream IdP]"** select (from `GET /upstream-idps`, disabled filtered, try/catch) + a **Method** column. Invitee redemption already worked (EnrollView handles `enrollment_federation_required`).
- **Foundation/wiring:** vendored `Textarea` primitive (`components/ui/textarea/`, `aria-invalid`+`dark:bg-input/30` parity with Input); `recovery.*`/`admin.oidc.*`/`admin.saml.*` i18n + error codes (`client_not_found`, `oidc_client_already_exists`, `saml_provider_already_exists`); 4 `requiresAdmin` routes + Admin sidebar items (OIDC clients · SAML providers; `AppWindow`/`Building2`). Admin group is now Accounts · Invitations · OIDC clients · SAML providers.

Spec: `docs/superpowers/specs/2026-06-08-recovery-rp-management-federated-invites-design.md`. Plan (10 tasks, DONE): `docs/superpowers/plans/2026-06-08-recovery-rp-management-federated-invites.md` (+`.tasks.json`). Commits `ff4e8f1`..`9a18962` (spec `14eb517`, plan `7bc9ca2`).

## Verified contracts (authoritative — `pkg/contract/auth.go` + handlers; `api.md` stale)
- OIDC clients: `GET /oidc-clients` `[]{clientId,displayName,redirectUris[],postLogoutRedirectUris[],allowedScopes[],tokenEndpointAuthMethod,requireConsent,disabled,createdAt}`; `POST {…,scopes[],public,requireConsent}`→ view+`secret`(confidential only); `PUT {…,allowedScopes[],…}` (**allowedScopes on PUT, scopes on create**); `POST /oidc-clients/rotate-secret {clientId}`→`{secret}`; `POST /oidc-clients/delete {clientId}`. Errors `client_not_found`,`oidc_client_already_exists`.
- SAML SPs: `GET /saml-providers` (list omits acs/keys); `GET /{id}` full (`acs:[{binding,location,index,isDefault}]`,`keys:[{use,notAfter?}]`); `POST` metadata `{metadataXml,…flags}` OR manual `{displayName,entityId,nameIdFormat,…flags,acs:[…]}`; `PUT /{id} {displayName,nameIdFormat,…flags,sessionLifetimeSecs?}`; `POST /{id}/reingest-metadata {metadataXml}`; `POST /saml-providers/delete {id}`. Not-found code = `credential_not_found`. Binding URNs HTTP-POST/HTTP-Redirect.
- Recovery: `/auth/recovery-code/verify {partial_session_token,code}`→`{recovery_session_token}`; `/auth/recovery/totp/begin {recovery_session_token}`→`{secret_base32,otpauth_uri}`; `/auth/recovery/totp/verify {recovery_session_token,code}`→`{recovery_codes[]}`+cookie.
- `GET /upstream-idps` `[]{slug,displayName,…,disabled}` (filter disabled for the picker).

## ▶ NEXT WORK
- **D — OTP/password invitations** (deferred, BACKEND feature): enrollment is passkey-only today; supporting "invite requires password/TOTP setup" needs a credential-requirements column on `enrollment` + new enrollment ceremony endpoints (`/enrollments/{token}/password|totp/...`) + frontend. Medium-high.
- **E — SAML-as-login subsystem** (deferred, MAJOR backend): there is NO SAML *relying-party* login flow (we're only the SAML IdP for downstream SPs; federation-as-login is OIDC-only). "Invite/sign-in via SAML" requires building ACS callback + assertion validation + account linking + upstream-SAML config. A v0.x-scale effort. Enables SAML invites.
- **Open loop — live visual review (STILL not done across the whole rebuild):** built without a screenshot tool. Worth a reload-and-react pass on `/login` recovery, the OIDC/SAML admin pages, the invite picker, + the earlier input-fill change. `mise dev-server` + `mise enroll-admin`; **`mise dev-seed`** seeds accounts/invitations/upstream-IdPs so lists + the picker render.
- Minor follow-ups (non-blocking, from final review): `lines()` helper duplicated across OIDC list/detail (extract to a shared util when convenient); SAML manual-create with zero ACS rows surfaces a generic backend `bad_request` (a client-side "add at least one ACS" hint would be friendlier).

## Process notes / quirks (these bit this cycle)
- **en.ts Edit-tool corruption struck again** (Tasks 1, 3, 5, 6 touched en.ts) — caught every time by the mandatory `grep -nP "\x{2018}"` + `grep -nP ":\s*\x{2019}"` guard baked into dispatch prompts ([[en.ts apostrophe hazard]]). Keep doing this.
- **Run git/dist commands from the REPO ROOT, not `dashboard/`** — a subagent ran them from `dashboard/` once, so `git checkout -- pkg/webui/dist` silently no-op'd (wrong relative path) and a fix didn't get staged. Always `cd /home/tundra/projects/tundra/prohibitorum` first.
- **A stray uncommitted `PairDeviceView.vue` `POLL_MS 2500→2000`** drift was caught by a spec reviewer's `git status` and reverted — review `git status` for unrelated dirt, not just the task's files.
- **gopls false-positive** (`ReconcileRetiredSigningKeys undefined`) appeared after the Go change; `go build ./... && go vet ./...` exit 0 is authoritative (the method exists, pkg/db untouched) — see the gopls root-cause in project memory.
- Reviewers' `npm run build` dirties `pkg/webui/dist`; discard before the next commit. Dist committed once at the done-gate (Vite hashes non-deterministic).
- NEVER bare `pkill -f 'prohibitorum'` (kills dev PG). NO git remote (push/PR N/A).
