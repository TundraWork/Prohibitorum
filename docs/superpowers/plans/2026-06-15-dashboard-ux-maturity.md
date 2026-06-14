# Dashboard UX-Maturity Improvement Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers-extended-cc:subagent-driven-development. Steps use checkbox tracking.

**Goal:** Raise the whole dashboard frontend to the UX-maturity standard the recent refinements set (consistent list/detail patterns, State-Has-a-Colour status, danger-zone grouping, read-only identifiers, loading/empty/error states, a11y), plus the two backend additions needed for full parity (account + SAML-provider independent disable).

**Architecture:** Vue 3 + Vite + Tailwind v4 + shadcn-vue (Reka UI) SPA embedded in the Go binary. Backend: Go + Huma/chi + sqlc/pgx + goose. Foundations (shared composables/components) land first; per-page work builds on them; backend bits add the dedicated `set-disabled` endpoints (account already has the column; SAML needs a migration).

**Tech stack:** vue-i18n, vitest, vue-tsc; `useApi` composable; `StatusBadge`/`ConfirmDialog`/`FormSection`/`SettingRow`; goose migration `014`; sqlc.

**Source of findings:** a 4-part parallel audit (admin views / account+threshold / custom components / cross-cutting). Highest-leverage themes: no loading states (13+ pages), `errorText` duplicated ~27×, success flags never auto-clear (9×), bare empty states, scattered a11y gaps, and two missing independent-disable flows (account, SAML).

---

## Conventions (verified)
- Commit directly to `master`; user pushes. NO `Co-Authored-By` trailers.
- Do NOT touch `pkg/webui/dist` until the done-gate (Task 12) — rebuild + commit once there.
- en.ts apostrophe hazard: after any en.ts edit, `LC_ALL=C grep -nP "[\xe2\x80\x98\xe2\x80\x99]" src/locales/en.ts` MUST be empty.
- Real FE typecheck is `npx vue-tsc -b`. Backend gate: `CGO_ENABLED=0 go build -tags nodynamic ./... && go vet ./... && go test ./...`.
- `account` HAS a `disabled` column; `saml_sp` does NOT (needs migration 014). The `set-disabled` handler/route pattern is established in `pkg/server/handle_admin_oidc_clients.go` / `handle_admin_upstream_idps.go` (`SetOIDCClientDisabled`, sudo-gated POST, audit, returns view) — mirror it.
- Status colours: `StatusBadge` variants success(green)/caution(amber)/danger(red)/neutral.

---

### Task 1: `useApi` returns `errorText` (kill the 27× duplication)
**Goal:** `useApi()` returns an `errorText` computed (maps `errors.<code>` i18n with message/common.error fallback) so pages stop copy-pasting it. Migrate every call site to use the returned `errorText` and delete the local computed.
**Files:** `dashboard/src/composables/useApi.ts` (+ its test), all ~27 pages/components that declare a local `errorText` computed (admin views, account views, threshold views, dialogs). Keep `PairDeviceView`'s two-source merge working (it ORs `error` + `waError`) — its `errorText` may stay bespoke or compose the returned one.
**Acceptance:** `useApi` exposes `errorText`; no page declares the duplicated 5-line computed anymore (except a documented bespoke multi-source case); behaviour unchanged; vitest + vue-tsc green.
**Verify:** `cd dashboard && npx vitest run && npx vue-tsc -b`
```json:metadata
{"files":["dashboard/src/composables/useApi.ts","dashboard/src/composables/useApi.test.ts","dashboard/src/pages","dashboard/src/components/custom"],"verifyCommand":"cd dashboard && npx vitest run && npx vue-tsc -b","acceptanceCriteria":["useApi returns errorText","duplicated errorText computed removed from call sites","vitest+vue-tsc green"]}
```

### Task 2: `useTransientFlag` composable (auto-clearing success feedback)
**Goal:** New `useTransientFlag(ms=3000)` returning `{ flag, trigger }` that sets true then auto-clears. Replace the 9 persistent success booleans (`saved`/`created`/`rotated`/`done`/`approved`) so "Saved"/"Created" confirmations fade.
**Files:** `dashboard/src/composables/useTransientFlag.ts` (+test); the 9 sites (admin detail views, PasswordCard, AdminInvitationsView, etc.). Use vitest fake timers in the composable test.
**Acceptance:** composable auto-clears; success confirmations use it; existing tests still pass (adjust any that asserted the persistent flag).
**Verify:** `cd dashboard && npx vitest run && npx vue-tsc -b`
```json:metadata
{"files":["dashboard/src/composables/useTransientFlag.ts","dashboard/src/composables/useTransientFlag.test.ts","dashboard/src/pages/admin","dashboard/src/pages/security"],"verifyCommand":"cd dashboard && npx vitest run && npx vue-tsc -b","acceptanceCriteria":["useTransientFlag auto-clears after ms","9 success-flag sites migrated","tests green"]}
```

### Task 3: Loading skeletons on all data-backed pages
**Goal:** Shared `TableSkeleton.vue` (N shimmer rows for list tables) + `CardSkeleton.vue` (for detail/card sections) using the existing `ui/skeleton` primitive. Render them while `busy && empty` on every data page (admin lists + admin detail + Sessions/Connected/Devices + threshold loading where it's currently plain text).
**Files:** `dashboard/src/components/custom/TableSkeleton.vue` + `CardSkeleton.vue` (+tests); wire into AdminAccountsView, AdminInvitationsView, AdminSamlProvidersView, AdminSigningKeysView, AdminAuditView, AdminUpstreamIdpsView, AdminOidcClientsView, the 4 admin detail views, SessionsView, ConnectedAccountsView, DevicesView. Threshold loading text → skeleton where it improves CLS (ConsentView).
**Acceptance:** each data page shows a skeleton (not blank) while first-loading; no layout jump; tests green.
**Verify:** `cd dashboard && npx vitest run && npx vue-tsc -b`
```json:metadata
{"files":["dashboard/src/components/custom/TableSkeleton.vue","dashboard/src/components/custom/CardSkeleton.vue","dashboard/src/pages/admin","dashboard/src/pages"],"verifyCommand":"cd dashboard && npx vitest run && npx vue-tsc -b","acceptanceCriteria":["TableSkeleton/CardSkeleton exist","data pages show skeleton while busy&&empty","tests green"]}
```

### Task 4: `EmptyState` + `BackLink` shared components
**Goal:** `EmptyState.vue` (icon + title + description + CTA slot, `role="status"`) replacing bare `<p class="text-muted">{{ ...empty }}</p>` across lists, with a primary action where one exists (Invite / Create / Generate). `BackLink.vue` (`← label`, shared class) for the 4 admin detail pages.
**Files:** `dashboard/src/components/custom/EmptyState.vue` + `BackLink.vue` (+tests); apply to all list empty states + the 4 detail back-links.
**Acceptance:** empty states are friendly + actionable; back links use the shared component with a leading arrow; tests green.
**Verify:** `cd dashboard && npx vitest run && npx vue-tsc -b`
```json:metadata
{"files":["dashboard/src/components/custom/EmptyState.vue","dashboard/src/components/custom/BackLink.vue","dashboard/src/pages/admin","dashboard/src/pages"],"verifyCommand":"cd dashboard && npx vitest run && npx vue-tsc -b","acceptanceCriteria":["EmptyState+BackLink exist+used","list empties have CTA where applicable","tests green"]}
```

### Task 5: List + detail consistency with the standard
**Goal:** AdminAccountsView header → "Account · Username" (combined, two-line cell already exists). AdminSamlProvidersView → collapse the two columns into one stacked "Provider · Entity ID" cell. AdminAccountDetailView → add a `StatusBadge` near the heading + render `username` as a read-only labeled field with a description. SAML detail → remove the redundant `<div class="flex flex-col gap-4">` double-gap wrapper. `SettingRow.vue` description `text-sm` → `text-xs` for sizing parity. (SAML list status badge is added in Task 11 once it has a `disabled` field.)
**Files:** `dashboard/src/pages/admin/AdminAccountsView.vue`(+test), `AdminSamlProvidersView.vue`(+test), `AdminAccountDetailView.vue`(+test), `AdminSamlProviderDetailView.vue`, `dashboard/src/components/custom/SettingRow.vue`, `dashboard/src/locales/en.ts`.
**Acceptance:** lists/detail match the established two-line/header/read-only/badge standard; tests green.
**Verify:** `cd dashboard && npx vitest run src/pages/admin && npx vue-tsc -b`
```json:metadata
{"files":["dashboard/src/pages/admin/AdminAccountsView.vue","dashboard/src/pages/admin/AdminSamlProvidersView.vue","dashboard/src/pages/admin/AdminAccountDetailView.vue","dashboard/src/pages/admin/AdminSamlProviderDetailView.vue","dashboard/src/components/custom/SettingRow.vue","dashboard/src/locales/en.ts"],"verifyCommand":"cd dashboard && npx vitest run src/pages/admin && npx vue-tsc -b","acceptanceCriteria":["accounts header combined; SAML list stacked cell; account-detail status badge + read-only username; SettingRow text-xs","tests green"]}
```

### Task 6: a11y round-2
**Goal:** Focus-visible rings on bare-`<button>` links (`PasswordTotpForm.vue:~141`, `SudoModal.vue:~140`) — use `Button variant="link"` or add the ring classes. Signing-key KID expander (`AdminSigningKeysView`) → `Button variant="ghost"`. `ListInput` error `<p>` gets an id + `aria-describedby` on the invalid input. EditProfileDialog avatar-picker group gets `role="group"`+`aria-labelledby` (or fieldset/legend); hidden file input `aria-hidden`. AvatarCropper wrapper `aria-label`. NavUser `ChevronsUpDown` `aria-hidden`. `aria-busy` on async confirm buttons (`ConfirmDialog`, `AccountRecovery`, `RecoveryCodesDisplay`). ComboboxTokenInput chip remove `aria-label` includes the value. RecoveryCodesCard badge: `>4 success / >0 caution / 0 danger`.
**Files:** the components listed; `dashboard/src/locales/en.ts` for any new aria strings.
**Acceptance:** every interactive element is keyboard-focusable with a visible ring; invalid inputs are described; decorative icons hidden; async buttons announce busy; recovery badge colour reflects risk; tests green.
**Verify:** `cd dashboard && npx vitest run && npx vue-tsc -b`
```json:metadata
{"files":["dashboard/src/components/custom/PasswordTotpForm.vue","dashboard/src/components/custom/SudoModal.vue","dashboard/src/pages/admin/AdminSigningKeysView.vue","dashboard/src/components/custom/ListInput.vue","dashboard/src/components/custom/EditProfileDialog.vue","dashboard/src/components/custom/AvatarCropper.vue","dashboard/src/components/custom/NavUser.vue","dashboard/src/components/custom/ConfirmDialog.vue","dashboard/src/components/custom/AccountRecovery.vue","dashboard/src/components/custom/RecoveryCodesDisplay.vue","dashboard/src/components/custom/ComboboxTokenInput.vue","dashboard/src/pages/security/RecoveryCodesCard.vue","dashboard/src/locales/en.ts"],"verifyCommand":"cd dashboard && npx vitest run && npx vue-tsc -b","acceptanceCriteria":["focus rings on bare-button links","ListInput aria-describedby","avatar group/cropper labels","aria-busy on async buttons","recovery badge danger at 0","tests green"]}
```

### Task 7: Account-list maturity (Sessions / Devices / Connected)
**Goal:** SessionsView: revoke behind `ConfirmDialog` (names the session), `gap-4`→`gap-6`, UA truncated with `title` (or short-parsed), IP `font-mono`. DevicesView: lookup busy indicator, UA truncate+title. ConnectedAccountsView: `StatusBadge` "Linked" on identities, already-linked provider shown as a badge (not disabled muted text). (Skeletons/EmptyState come from Tasks 3/4.)
**Files:** `dashboard/src/pages/SessionsView.vue`(+test), `DevicesView.vue`(+test), `ConnectedAccountsView.vue`(+test), `dashboard/src/locales/en.ts`.
**Acceptance:** revoke confirms; states badged; long strings safe; tests green.
**Verify:** `cd dashboard && npx vitest run src/pages && npx vue-tsc -b`
```json:metadata
{"files":["dashboard/src/pages/SessionsView.vue","dashboard/src/pages/DevicesView.vue","dashboard/src/pages/ConnectedAccountsView.vue","dashboard/src/locales/en.ts"],"verifyCommand":"cd dashboard && npx vitest run src/pages && npx vue-tsc -b","acceptanceCriteria":["session revoke confirm; identity/provider badges; UA/IP formatting","tests green"]}
```

### Task 8: Security-cards + SecurityView maturity
**Goal:** PasskeysCard empty-state message + rename input/button `aria-label`. TotpCard cancel-out of the QR step (ghost Cancel resets state). Password/Totp card badge: avoid width flicker before factors load (neutral placeholder). SecurityView: surface a non-destructive Alert if `loadFactors` fails.
**Files:** `dashboard/src/pages/security/PasskeysCard.vue`(+test), `TotpCard.vue`(+test), `PasswordCard.vue`, `dashboard/src/pages/SecurityView.vue`, `dashboard/src/locales/en.ts`.
**Acceptance:** no blank passkey card; TOTP setup is escapable; factor-load failure is visible; tests green.
**Verify:** `cd dashboard && npx vitest run src/pages/security src/pages/SecurityView.test.ts && npx vue-tsc -b`
```json:metadata
{"files":["dashboard/src/pages/security/PasskeysCard.vue","dashboard/src/pages/security/TotpCard.vue","dashboard/src/pages/security/PasswordCard.vue","dashboard/src/pages/SecurityView.vue","dashboard/src/locales/en.ts"],"verifyCommand":"cd dashboard && npx vitest run src/pages/security && npx vue-tsc -b","acceptanceCriteria":["passkey empty state; TOTP cancel; factor-load error surfaced","tests green"]}
```

### Task 9: Threshold pages + layout maturity
**Goal:** LoginView bootstrap-check + FederationButtons load skeleton (no layout jump); pair-link `cursor-pointer`. LogoutView in-flight indicator. PairDeviceView code skeleton while `begin()` in flight. EnrollView field descriptions + reset-target as mono identifier. WelcomeView avatar overlay `rounded-full`. ConsentView loading skeleton. DashboardLayout header shows the current page title; `SidebarInset` `<main>` gets an `aria-label`.
**Files:** `LoginView.vue`, `FederationButtons.vue`, `LogoutView.vue`, `PairDeviceView.vue`, `EnrollView.vue`, `WelcomeView.vue`, `ConsentView.vue`, `DashboardLayout.vue`, `dashboard/src/components/ui/sidebar/SidebarInset.vue`, `dashboard/src/locales/en.ts` (+ touched tests).
**Acceptance:** no layout jumps; in-flight states shown; descriptions present; header oriented; tests green.
**Verify:** `cd dashboard && npx vitest run && npx vue-tsc -b`
```json:metadata
{"files":["dashboard/src/pages/LoginView.vue","dashboard/src/components/custom/FederationButtons.vue","dashboard/src/pages/LogoutView.vue","dashboard/src/pages/PairDeviceView.vue","dashboard/src/pages/EnrollView.vue","dashboard/src/pages/WelcomeView.vue","dashboard/src/pages/ConsentView.vue","dashboard/src/pages/DashboardLayout.vue","dashboard/src/components/ui/sidebar/SidebarInset.vue","dashboard/src/locales/en.ts"],"verifyCommand":"cd dashboard && npx vitest run && npx vue-tsc -b","acceptanceCriteria":["threshold loading/in-flight states; enroll descriptions; header title; main aria-label","tests green"]}
```

### Task 10: Backend — account `set-disabled` + FE Danger-Zone relocation
**Goal:** Dedicated `POST /api/prohibitorum/accounts/set-disabled` `{id, disabled}` (admin+sudo, `SetAccountDisabled` sqlc query, audit, returns account view) mirroring the OIDC endpoint. FE: move the account disable out of the identity form into the Danger Zone as an independent Disable/Enable button + a labeled `StatusBadge` near the heading; the identity PUT no longer needs to own disable.
**Files:** `db/queries/account.sql`, `pkg/db` (sqlc regen), `pkg/server/handle_account.go` (or the admin-accounts handler), `pkg/server/server.go` (route), `pkg/server/handle_admin_set_disabled_test.go` (extend guard tests), `dashboard/src/pages/admin/AdminAccountDetailView.vue`(+test), `dashboard/src/locales/en.ts`.
**Acceptance:** endpoint flips only `disabled` (admin+sudo), returns updated view; FE button toggles independently + status badge; guard tests; `go build/vet/test` 0; vitest+vue-tsc green.
**Verify:** `CGO_ENABLED=0 go build -tags nodynamic ./... && go vet ./pkg/server && go test ./pkg/server -run 'SetAccountDisabled|Disabled' && cd dashboard && npx vitest run src/pages/admin/AdminAccountDetailView.test.ts && npx vue-tsc -b`
```json:metadata
{"files":["db/queries/account.sql","pkg/server/handle_account.go","pkg/server/server.go","pkg/server/handle_admin_set_disabled_test.go","dashboard/src/pages/admin/AdminAccountDetailView.vue","dashboard/src/pages/admin/AdminAccountDetailView.test.ts","dashboard/src/locales/en.ts"],"verifyCommand":"CGO_ENABLED=0 go build -tags nodynamic ./... && go vet ./pkg/server && go test ./pkg/server -run Disabled && cd dashboard && npx vitest run src/pages/admin/AdminAccountDetailView.test.ts && npx vue-tsc -b","acceptanceCriteria":["account set-disabled endpoint (admin+sudo, audit, view)","FE danger-zone button + status badge","guard tests; gates green"]}
```

### Task 11: Backend — SAML-provider disable (migration + endpoint + FE parity)
**Goal:** Migration `014` adds `disabled boolean NOT NULL DEFAULT false` to `saml_sp`. Include `disabled` in `GetSAMLSPByEntityID`/`ListSAMLSPs`/`GetSAMLSPByID`/`UpdateSAMLSP` projections + a `SetSAMLSPDisabled` query. Contract `SamlApplicationView` gains `disabled`; `POST /api/prohibitorum/saml-applications/set-disabled` `{entityId, disabled}` (admin+sudo, audit). SP-initiated SSO must reject a disabled SP (filter `NOT disabled` on the auth path, mirroring upstream_idp). FE: SAML list status badge + detail Danger-Zone Disable/Enable button + status badge.
**Files:** `db/migrations/014_saml_sp_disabled.sql`, `db/queries/saml_sp.sql`, `pkg/db` (regen), `pkg/contract` (SAML view), `pkg/server/handle_admin_saml_*.go`, `pkg/server/server.go`, `pkg/protocol/saml` (reject disabled SP on SSO), guard test, `dashboard/src/pages/admin/AdminSamlProvidersView.vue`(+test), `AdminSamlProviderDetailView.vue`(+test), `dashboard/src/locales/en.ts`.
**Acceptance:** migration applies; disabled SAML SP rejected on SSO; endpoint + FE parity (list badge, danger-zone button); `go build/vet/test` 0; vitest+vue-tsc green.
**Verify:** `CGO_ENABLED=0 go build -tags nodynamic ./... && go vet ./... && go test ./... && cd dashboard && npx vitest run src/pages/admin && npx vue-tsc -b`
```json:metadata
{"files":["db/migrations/014_saml_sp_disabled.sql","db/queries/saml_sp.sql","pkg/contract","pkg/server","pkg/protocol/saml","dashboard/src/pages/admin/AdminSamlProvidersView.vue","dashboard/src/pages/admin/AdminSamlProviderDetailView.vue","dashboard/src/locales/en.ts"],"verifyCommand":"CGO_ENABLED=0 go build -tags nodynamic ./... && go vet ./... && go test ./... && cd dashboard && npx vitest run src/pages/admin && npx vue-tsc -b","acceptanceCriteria":["014 adds saml_sp.disabled; SSO rejects disabled SP; set-disabled endpoint; FE list badge + danger-zone button","gates green"]}
```

### Task 12: Done-gate (full verification + smoke + dist)
**Goal:** Full gate green and dist rebuilt+committed. Add smoke coverage for the new account + SAML set-disabled endpoints ONLY if it fits the sudo-begin rate budget (10/min/session, one-shot) — otherwise rely on guard tests (document the decision, as with the OIDC set-disabled).
**Files:** `cmd/smoke/main.go` (if budget allows), `pkg/webui/dist`.
**Verify:** `CGO_ENABLED=0 go build -tags nodynamic ./... && go vet ./... && go test ./... && cd dashboard && npm run test && npm run build` + live smoke `SMOKE_EXIT=0` (own server on :8081 vs a fresh `prohibitorum_smoke` DB).
```json:metadata
{"files":["cmd/smoke/main.go","pkg/webui/dist"],"verifyCommand":"CGO_ENABLED=0 go build -tags nodynamic ./... && go vet ./... && go test ./... && cd dashboard && npm run build","acceptanceCriteria":["full gate green; live smoke SMOKE_EXIT=0; dist rebuilt+committed"]}
```

---

## Review follow-ups (tracked during execution)
- (none yet)

## Done-gate
`CGO_ENABLED=0 go build -tags nodynamic ./...` / `go vet` / `go test ./...` (0), `vitest` green, `vue-tsc -b` (0), live smoke `SMOKE_EXIT=0`, rebuild + commit `pkg/webui/dist`.
