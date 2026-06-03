# Session handoff — Frontend full-surface scaffold: DONE

> This chunk scaffolded the ENTIRE planned core frontend surface (not just
> implemented features) so the user can drive **UX / design-system** work against a
> complete, navigable skeleton. Frontend-only, no Go changes. All 11 plan tasks done,
> per-task two-stage reviewed, final opus review READY. Committed directly to `master`.

## State
```
HEAD: e45f356 (tracker sync)   feature commits f1d7f7f..fa83105   branch: master   NO git remote
```
Spec: `docs/superpowers/specs/2026-06-03-frontend-full-surface-scaffold-design.md`
Plan: `docs/superpowers/plans/2026-06-03-frontend-full-surface-scaffold.md` (+ `.tasks.json`, all 11 completed)

> NOTE: the working tree also has **unrelated** user-added files (ARCHITECTURE.md/README.md edits, `.agents/ .claude/ .impeccable/ DESIGN.md PRODUCT.md`). They are NOT part of this work — leave them.

## What shipped
A grouped-sidebar dashboard covering the full core surface. Gate GREEN: `go build/vet/test`, frontend **vitest 56/56**, `npm run build` clean, `pkg/webui/dist` rebuilt + committed.

- **Account group:** Profile · **Security** (one page, four cards under `pages/security/`: `PasskeysCard` list/rename/delete + **add-passkey**; `PasswordCard`; `TotpCard` enroll/verify/revoke; `RecoveryCodesCard`) · Sessions · **ConnectedAccountsView** (identities list/unlink/link) · **DevicesView** (pairing lookup/approve/cancel).
- **Admin group:** Accounts (rows link to) **`AccountDetailView` `/admin/accounts/:id`** (role/disable PUT, revoke-sessions, reissue→CopyableUrl, delete) · Invitations.
- **Admin · Planned** (greyed, navigable): `/admin/{oidc-clients,saml-providers,signing-keys,audit,settings}` → one `PlaceholderView` reading `route.meta.title/summary`.
- **Sudo step-up gate:** `lib/sudo.ts` — `withSudo(fn)` (retry-once-after-step-up on `{code:'sudo_required'}`), `ensureSudo()` (proactive, for the redirect-based identity link), module-singleton `sudoState`; `components/SudoModal.vue` mounted ONCE in `DashboardLayout`. Wraps every sudo-gated `/me/*` action (password set, TOTP enroll/verify/revoke, recovery regenerate, identity unlink, device approve). Device **cancel** is intentionally NOT wrapped (test-locked).
- **Shared:** `components/StatusBadge.vue` (planned/stub/beta), `lib/webauthn.passkeyAddCredential`.
- Router: new nested routes + `/credentials`→`/security` redirect; `CredentialsView` retired (deleted). `installGuard` bounces non-admins from every `requiresAdmin` route incl. the planned placeholders. Sidebar + `/dev` console route lists kept in sync with the router.

## Key conventions (match these for any new scaffold pages)
- **English-literal copy** + a `// TODO(i18n): … key + translate after the design-system pass.` header. The shipped login/consent/dashboard pages stay bilingual; the `nav.*` i18n keys are now mostly unused (prune at the i18n pass — only `nav.logout` is still used, in DashboardLayout).
- `busy` re-entrancy guard; `type="button"`; errors via `<p v-if="error" role="alert" aria-live="polite">` (fallback to `e.message`); inline two-step arm→confirm for destructive actions; semantic `<table>`s.
- Tests: vitest, API-mocked; assert real call args + a UI effect (not just "mock called").

## Backend contracts used (confirmed against handlers; all under /api/prohibitorum)
`/me/credentials/register/begin?nickname=` + `/complete` (add passkey, NOT sudo) · `/me/password/set {password}` (sudo) · `/me/totp/begin`→`{secret_base32,otpauth_uri}` + `/me/totp/verify {code}`→204|`{recovery_codes}` (conditional sudo) · `/me/recovery-codes/regenerate`→`{recovery_codes}` (sudo) · `/me/auth/revoke-password-totp` (sudo) · `/me/identities` (GET) + `/{id}/unlink` (sudo) + `/link/{slug}/begin?return_to=` (302, sudo) · `/me/devices/pair/lookup?code=` + `/approve {code}`(sudo) + `/cancel {code}`(not sudo) · `/me/sudo/{methods,begin,complete}` · admin `/accounts/{id}` (GET), `PUT /accounts/{id} {displayName,role,disabled}`, `/accounts/revoke-sessions {id}`→`{revoked}`, `/accounts/reissue-enrollment {id}`→`{url}`.

## Honest limitations / deferred
- **Admin per-credential force-revoke** is a `stub` badge in AccountDetailView — there is NO admin "list an account's credentials" endpoint, so credential IDs can't be enumerated. `POST /accounts/credentials/delete {accountId,credentialId}` exists but is undriveable from UI. TODO(backend): add a list endpoint.
- The **5 Planned admin areas** are placeholder pages only (no backend APIs — those features are CLI-only today: `oidc-client`, `saml-sp`, `signing-key`). Building them out (frontend + backend stubs) is the natural next chunk.
- Identity **link** and **federation login** need a live upstream OP — not standalone-testable (the interactions/buttons render regardless).
- No Playwright e2e; manual click-through is the acceptance.

## Run / manually exercise it (see the whole surface)
```
mise dev-server          # builds SPA + serves :8080 (dedicated prohibitorum_dev DB; auto-migrates)
mise enroll-admin        # prints /enroll/<token>  (same dev key/DB)
```
Register a passkey → dashboard. The **🛠 dev** console at **`/dev`** lists every page (User/Admin/Planned) + a mint-invite button. Try **Security → Set password** to see the `SudoModal` fire. (`mise build` compiles a standalone `./prohibitorum`.)

## Runtime quirks (unchanged — these bite)
- master, direct commits, NO git remote. opus for review/judgment, sonnet mechanical; never haiku.
- **gopls is reliable** (gopls v0.22.0 + go 1.26.3-X:nodwarf5, mise==global). Trust `go build/vet ./...` exit 0; the diagnostics you see (`unusedparams`/`deprecated`/`SplitSeq`) are legit info/style hints, not the old false "no matching files" lies.
- NEVER `pkill -f 'prohibitorum'` bare (kills the dev PG at /tmp/prohibitorum-pg). Precise: `pkill -f 'go-build.*/prohibitorum'` + `pkill -f 'cmd/prohibitorum'`. Smoke (unaffected this chunk): `setsid bash /tmp/run_v06.sh`, poll `/tmp/v06.result` for `DONE`/`SMOKE_EXIT=0`.
- **After ANY Vue edit: `cd dashboard && npm run build` then `git add pkg/webui/dist`** — the binary embeds the COMMITTED dist (deterministic build → no diff if source unchanged).
