# Dashboard Content-Aware UX Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers-extended-cc:subagent-driven-development.

**Goal:** Fix the experiential UX gaps a content-aware re-audit surfaced (the product owner's 6 reported issues + all P1/P2 finds) — redundancy, content overflow, ambiguous affordances, same-task inconsistency, and missing modern patterns — across the whole dashboard, plus the small backend touch the consent logo/policy needs (none — all data already in the contract).

**Architecture:** Vue 3 + Vite + Tailwind v4 + shadcn-vue SPA embedded in the Go binary. This cycle is frontend-only EXCEPT where noted. Shared helpers/components land first; per-screen work builds on them.

**Tech stack:** vue-i18n, vitest, vue-tsc; existing `useApi`/`useTransientFlag`/`StatusBadge`/`ConfirmDialog`/`FormSection`/`SettingRow`/`ComboboxTokenInput`/`EmptyState`/`BackLink`/`SudoModal`/`lib/sudo`.

**Audit source:** two opus content-aware passes (admin screens; account/threshold/dialogs) — reasoning about lived task flow, not structure. Research-backed scope-picker decision: ≤5–6 fixed options → checkboxes WITH per-option descriptions; extensible/unknown → combobox with tags+descriptions ([NN/G listbox-vs-dropdown], [oauth.com scope UI], [Keycloak FGAP 2025]).

---

## Conventions (verified)
- Commit directly to `master`; user pushes. NO `Co-Authored-By` trailers.
- Do NOT touch `pkg/webui/dist` until the done-gate (final task).
- en.ts apostrophe hazard: prefer apostrophe-free copy; after any en.ts edit run `cd dashboard && npm run build 2>&1 | tail -3` (must succeed — a curly DELIMITER breaks esbuild) AND `LC_ALL=C grep -nP "[\xe2\x80\x98\xe2\x80\x99]" src/locales/en.ts` (legit in-string apostrophes OK; build is the authoritative check).
- Real FE typecheck: `npx vue-tsc -b`. Per-task verify: `cd dashboard && npx vitest run && npx vue-tsc -b`.
- This cycle is FRONTEND-ONLY (no Go/migrations). Existing backend contracts already carry everything needed (consent `logoUri`/`policyUri`/`tosUri`; session `isCurrent`; etc.).

---

### Task 1: shared `formatUserAgent()` + consistent relative-time
**Goal:** `lib/userAgent.ts` `formatUserAgent(ua)` → a short human label ("Chrome on Windows", "Safari on iPhone", fallback to a trimmed UA). Apply it wherever a raw/truncated UA is the identity signal: `SessionsView`, `DevicesView` (initiator UA), `AdminAccountDetailView` sessions. Keep the full UA in a `title`/tooltip. Also standardize account-surface timestamps on ONE cadence (relative time, e.g. extend `lib/time.ts` with `relativeTime` already used in admin — apply consistently to Sessions/Connected/Devices/Passkeys instead of the current mix of `toLocaleString`/`toLocaleDateString`).
**Files:** `dashboard/src/lib/userAgent.ts`(+test), `dashboard/src/lib/time.ts`, `SessionsView.vue`, `DevicesView.vue`, `AdminAccountDetailView.vue`, `ConnectedAccountsView.vue`, security `PasskeysCard.vue`.
**Verify:** `cd dashboard && npx vitest run && npx vue-tsc -b`
```json:metadata
{"files":["dashboard/src/lib/userAgent.ts","dashboard/src/lib/userAgent.test.ts","dashboard/src/lib/time.ts","dashboard/src/pages/SessionsView.vue","dashboard/src/pages/DevicesView.vue","dashboard/src/pages/admin/AdminAccountDetailView.vue","dashboard/src/pages/ConnectedAccountsView.vue","dashboard/src/pages/security/PasskeysCard.vue"],"verifyCommand":"cd dashboard && npx vitest run && npx vue-tsc -b","acceptanceCriteria":["formatUserAgent humanizes UA + full UA in title","timestamps consistent (relative) on account surfaces","tests green"]}
```

### Task 2: sudo foreshadowing — action context + reversible path
**Goal:** `withSudo`/`ensureSudo` (`lib/sudo`) accept an optional `reason` string; `SudoModal` renders it in the dialog description so step-up says WHAT it's confirming. Add a reciprocal "Use a passkey instead" link in the password fallback (currently one-way), and group the passkey / password / federation options with a divider like LoginView. Thread a short reason from the main sudo-gated call sites (delete passkey, set password, regenerate codes, unlink, approve device, revoke, rotate, disable, etc.) — at minimum the highest-traffic ones.
**Files:** `dashboard/src/lib/sudo.ts`, `dashboard/src/components/custom/SudoModal.vue`(+test), `dashboard/src/locales/en.ts`, + the call sites passing a reason.
**Verify:** `cd dashboard && npx vitest run && npx vue-tsc -b`
```json:metadata
{"files":["dashboard/src/lib/sudo.ts","dashboard/src/components/custom/SudoModal.vue","dashboard/src/locales/en.ts"],"verifyCommand":"cd dashboard && npx vitest run && npx vue-tsc -b","acceptanceCriteria":["sudo modal shows the action reason","reciprocal use-passkey path","options grouped","tests green"]}
```

### Task 3: admin list cleanup — kill duplicate CTAs + title create forms + account filter
**Goal:** Remove the empty-state CTA that duplicates the always-visible top-right create button on all 7 list pages (empty state = friendly message + icon only; the top button is the single create affordance). Give the inline create-form `Card`s a `CardHeader`/title ("New OIDC application", etc.). Add a client-side search/filter `Input` to `AdminAccountsView` (filter rows by username/displayName) so the accounts table is usable at scale.
**Files:** `AdminAccountsView.vue`, `AdminInvitationsView.vue`, `AdminOidcClientsView.vue`, `AdminSamlProvidersView.vue`, `AdminUpstreamIdpsView.vue`, `AdminSigningKeysView.vue`, `AdminAuditView.vue` (empty-state CTA removals as applicable), `dashboard/src/locales/en.ts` (+ touched tests).
**Verify:** `cd dashboard && npx vitest run src/pages/admin && npx vue-tsc -b`
```json:metadata
{"files":["dashboard/src/pages/admin","dashboard/src/locales/en.ts"],"verifyCommand":"cd dashboard && npx vitest run src/pages/admin && npx vue-tsc -b","acceptanceCriteria":["no duplicate empty-state CTA","create-form cards titled","accounts list has a filter","tests green"]}
```

### Task 4: shared scope picker with descriptions
**Goal:** Extract ONE OIDC scope picker — checkboxes WITH a plain-language description per scope (openid required+disabled; profile/email/offline_access) — and use it in BOTH `AdminOidcClientsView` (create) and `AdminOidcClientDetailView` (edit), ending the verbatim duplication. Add per-scope descriptions to en.ts (reuse/extend `lib/scopes.ts`). The upstream-IdP `ComboboxTokenInput` already pairs scopes with descriptions — leave it (extensible set), but ensure both read as the same "pick scopes, each explained" pattern.
**Files:** `dashboard/src/components/custom/OidcScopePicker.vue`(+test) (or a shared list), `dashboard/src/lib/scopes.ts`, `AdminOidcClientsView.vue`, `AdminOidcClientDetailView.vue`, `dashboard/src/locales/en.ts`.
**Verify:** `cd dashboard && npx vitest run src/pages/admin src/components/custom && npx vue-tsc -b`
```json:metadata
{"files":["dashboard/src/components/custom/OidcScopePicker.vue","dashboard/src/lib/scopes.ts","dashboard/src/pages/admin/AdminOidcClientsView.vue","dashboard/src/pages/admin/AdminOidcClientDetailView.vue","dashboard/src/locales/en.ts"],"verifyCommand":"cd dashboard && npx vitest run src/pages/admin && npx vue-tsc -b","acceptanceCriteria":["one shared OIDC scope picker w/ per-scope descriptions used in create+detail","no duplicated checkbox block","tests green"]}
```

### Task 5: danger-zone consistent 3-row layout
**Goal:** Across all 4 detail views (upstream IdP, OIDC, SAML, account), make every danger-zone sub-action a consistent **title → description → control** unit (a small `<h4>`/medium label, a muted description line, then the input/button), separated by `<Separator/>`. Today only "disable" has a title; rotate/delete/etc. don't. Keep the status badge with the disable sub-action.
**Files:** `AdminUpstreamIdpDetailView.vue`, `AdminOidcClientDetailView.vue`, `AdminSamlProviderDetailView.vue`, `AdminAccountDetailView.vue`, `dashboard/src/locales/en.ts` (sub-action title keys).
**Verify:** `cd dashboard && npx vitest run src/pages/admin && npx vue-tsc -b`
```json:metadata
{"files":["dashboard/src/pages/admin/AdminUpstreamIdpDetailView.vue","dashboard/src/pages/admin/AdminOidcClientDetailView.vue","dashboard/src/pages/admin/AdminSamlProviderDetailView.vue","dashboard/src/pages/admin/AdminAccountDetailView.vue","dashboard/src/locales/en.ts"],"verifyCommand":"cd dashboard && npx vitest run src/pages/admin && npx vue-tsc -b","acceptanceCriteria":["every danger-zone sub-action: title+description+control, separated","tests green"]}
```

### Task 6: account-detail correctness — Save scope + session revoke confirm
**Goal:** (a) Scope the identity-card **Save** so it no longer silently owns `disabled` (the danger-zone toggle is the source of truth — drop `disabled` from the identity PUT, or make the form not the disable control) and make the attributes editor's save boundary clear (so editing the name can't silently re-commit/clobber attributes). (b) Per-row session revoke must go through a `ConfirmDialog` (parity with revoke-all) and the row must show "(this device)" for `isCurrent`, with the humanized UA from Task 1. (c) Show "Never used" instead of "last used —".
**Files:** `AdminAccountDetailView.vue`(+test), `dashboard/src/locales/en.ts`.
**Verify:** `cd dashboard && npx vitest run src/pages/admin/AdminAccountDetailView.test.ts && npx vue-tsc -b`
```json:metadata
{"files":["dashboard/src/pages/admin/AdminAccountDetailView.vue","dashboard/src/locales/en.ts"],"verifyCommand":"cd dashboard && npx vitest run src/pages/admin/AdminAccountDetailView.test.ts && npx vue-tsc -b","acceptanceCriteria":["identity Save no longer owns disabled; attributes save boundary clear","per-row revoke confirms + (this device) + humanized UA","tests green"]}
```

### Task 7: signing-key JWK out of the table row
**Goal:** Move the expanded JWK out of the inline `colspan` table row into a contained region (a dialog/sheet, or a properly width-bounded panel below the table) so it no longer blows out the row width; pretty-print + wrap. Clarify the kid affordance (it's both identifier and expander — make the expand a distinct control or clearly a disclosure). Add a short lifecycle/status legend (pending→active→decommissioning→retired) and surface the next action; indicate which key is currently signing.
**Files:** `AdminSigningKeysView.vue`(+test), `dashboard/src/locales/en.ts`.
**Verify:** `cd dashboard && npx vitest run src/pages/admin/AdminSigningKeysView.test.ts && npx vue-tsc -b`
```json:metadata
{"files":["dashboard/src/pages/admin/AdminSigningKeysView.vue","dashboard/src/locales/en.ts"],"verifyCommand":"cd dashboard && npx vitest run src/pages/admin && npx vue-tsc -b","acceptanceCriteria":["JWK in a contained panel/dialog, no row blowout","kid affordance clear","status legend/next-action","tests green"]}
```

### Task 8: SAML attribute-map row editor + detail grouping
**Goal:** Replace the raw JSON `<textarea>` attribute-map editor with a row editor (name / name_format / source / multi) like the ACS editor; keep the same persisted shape. Group the SAML detail security flags under `FormSection`/`SettingRow` (consistent with create); format ACS URLs with mono+truncate+tooltip instead of `break-all`. Also group the upstream-IdP DETAIL form into the same `FormSection`s the create form uses (Connection / Provisioning / Claims).
**Files:** `AdminSamlProviderDetailView.vue`(+test), `AdminSamlProvidersView.vue` (ACS create row if needed), `AdminUpstreamIdpDetailView.vue`, `dashboard/src/locales/en.ts`.
**Verify:** `cd dashboard && npx vitest run src/pages/admin && npx vue-tsc -b`
```json:metadata
{"files":["dashboard/src/pages/admin/AdminSamlProviderDetailView.vue","dashboard/src/pages/admin/AdminUpstreamIdpDetailView.vue","dashboard/src/locales/en.ts"],"verifyCommand":"cd dashboard && npx vitest run src/pages/admin && npx vue-tsc -b","acceptanceCriteria":["attribute-map row editor (same payload shape)","SAML security in FormSection; ACS URL formatted","upstream detail grouped into FormSections","tests green"]}
```

### Task 9: audit log — presets + real pagination + enumerated filters
**Goal:** Add quick time-range presets (Last 15m / 1h / 24h / 7d / custom) that fill since/until. Replace the append "load more" with real pagination (keyset prev/next page that REPLACES the page, with page context — not an infinitely-growing list). Make `factor`/`event` enumerated (select/combobox from the known taxonomy) instead of free-text. Show active filters as removable pills; contain the row-expand detail (don't blow the row).
**Files:** `AdminAuditView.vue`(+test), `dashboard/src/locales/en.ts`, possibly a small `lib/audit.ts` for the factor/event taxonomy.
**Verify:** `cd dashboard && npx vitest run src/pages/admin/AdminAuditView.test.ts && npx vue-tsc -b`
```json:metadata
{"files":["dashboard/src/pages/admin/AdminAuditView.vue","dashboard/src/locales/en.ts"],"verifyCommand":"cd dashboard && npx vitest run src/pages/admin/AdminAuditView.test.ts && npx vue-tsc -b","acceptanceCriteria":["time presets; real prev/next pagination; enumerated factor/event; active-filter pills; contained expand","tests green"]}
```

### Task 10: EditProfileDialog — divide zones + scope Save + Close
**Goal:** Visually divide the Avatar zone (applies immediately) from the Display-name zone (deferred Save) with a `<Separator/>` + section headings; make the footer Save clearly scoped to the name ("Save name") and replace the vague "Cancel" with "Close"/"Done" (the dialog has no atomic transaction). Move the format/size hint under the Upload button; give the clicked source-card pending feedback + surface selection errors near the picker; disambiguate "No avatar" card vs "Remove" upload.
**Files:** `dashboard/src/components/custom/EditProfileDialog.vue`(+test), `dashboard/src/locales/en.ts`.
**Verify:** `cd dashboard && npx vitest run src/components/custom/EditProfileDialog.test.ts && npx vue-tsc -b`
```json:metadata
{"files":["dashboard/src/components/custom/EditProfileDialog.vue","dashboard/src/locales/en.ts"],"verifyCommand":"cd dashboard && npx vitest run src/components/custom/EditProfileDialog.test.ts && npx vue-tsc -b","acceptanceCriteria":["avatar/name zones divided; Save scoped to name; Cancel→Close; hint placement; per-card feedback","tests green"]}
```

### Task 11: SecurityView + cards normalization
**Goal:** Normalize the 4 factor cards (Passkeys/Password/TOTP/Recovery) to ONE template (status badge in header → one help line → primary control in a consistent place). Foreshadow sudo on sudo-gated card actions (use Task 2's reason). Hoist TOTP's sudo to `setup` (begin) so the modal can't interrupt mid-verify and expire the code. Guard RecoveryCodes regenerate when no TOTP (don't let the user click into a `bad_request`). Consistent primary-action placement.
**Files:** `SecurityView.vue`, security `PasskeysCard.vue`/`PasswordCard.vue`/`TotpCard.vue`/`RecoveryCodesCard.vue`, `dashboard/src/locales/en.ts` (+ tests).
**Verify:** `cd dashboard && npx vitest run src/pages/security && npx vue-tsc -b`
```json:metadata
{"files":["dashboard/src/pages/SecurityView.vue","dashboard/src/pages/security","dashboard/src/locales/en.ts"],"verifyCommand":"cd dashboard && npx vitest run src/pages/security && npx vue-tsc -b","acceptanceCriteria":["cards one template; sudo foreshadowed; TOTP sudo hoisted; recovery no-TOTP guard","tests green"]}
```

### Task 12: Sessions / Devices / Connected experiential
**Goal:** Sessions: revoke verb disambiguated from account sign-out, IP labeled correctly (not "Last seen: <ip>"), humanized UA (Task 1) + "this device". Devices: humanize + make the initiator UA prominent (it's the security-decision signal); fix the already-bound terminal state ("Done"/dismiss, not "Cancel"); flag the destructive server-side cancel. Connected: unify vocabulary on "Connected" (not "Linked"+"Connected" mixed), split the "Linked: date" label from the status badge, add link-redirect expectation copy.
**Files:** `SessionsView.vue`(+test), `DevicesView.vue`(+test), `ConnectedAccountsView.vue`(+test), `dashboard/src/locales/en.ts`.
**Verify:** `cd dashboard && npx vitest run src/pages && npx vue-tsc -b`
```json:metadata
{"files":["dashboard/src/pages/SessionsView.vue","dashboard/src/pages/DevicesView.vue","dashboard/src/pages/ConnectedAccountsView.vue","dashboard/src/locales/en.ts"],"verifyCommand":"cd dashboard && npx vitest run src/pages && npx vue-tsc -b","acceptanceCriteria":["UA humanized+device markers; IP labeled; devices terminal state; connected vocabulary unified+date label split+redirect copy","tests green"]}
```

### Task 13: threshold screens — Welcome / Consent / Login / Enroll / Pair
**Goal:** Welcome: stop gating Continue on the avatar fetch (let it settle async); drop the duplicate "setting up picture" messages; make copy about confirming the account. Consent: render the client `logoUri` + `policyUri`/`tosUri` links (already in the contract); add the scope count to Allow; frame unknown scopes as "Access: <scope>". Login: collapse the doubled "or" divider to one authority. Enroll: show a federation interstitial before the silent IdP redirect; foreshadow the passkey ceremony on Register; render the reset-target as a plain read-only value (not a copy CodeField). Pair: present the code as a large legible display (not a copy field — it's typed on another device); add a "skipping is safe on shared devices" note.
**Files:** `WelcomeView.vue`, `ConsentView.vue`, `ConsentScopeList.vue`, `LoginView.vue`, `FederationButtons.vue`, `EnrollView.vue`, `PairDeviceView.vue`, `dashboard/src/locales/en.ts` (+ tests).
**Verify:** `cd dashboard && npx vitest run && npx vue-tsc -b`
```json:metadata
{"files":["dashboard/src/pages/WelcomeView.vue","dashboard/src/pages/ConsentView.vue","dashboard/src/components/custom/ConsentScopeList.vue","dashboard/src/pages/LoginView.vue","dashboard/src/components/custom/FederationButtons.vue","dashboard/src/pages/EnrollView.vue","dashboard/src/pages/PairDeviceView.vue","dashboard/src/locales/en.ts"],"verifyCommand":"cd dashboard && npx vitest run && npx vue-tsc -b","acceptanceCriteria":["Welcome Continue not avatar-gated; Consent logo/policy/scope-count; Login single divider; Enroll interstitial+passkey foreshadow+plain reset target; Pair code-as-display+skip note","tests green"]}
```

### Task 14: misc copy + P3 polish
**Goal:** AccountRecovery: set the "each code is single-use; a wrong code sends you back to sign in" expectation BEFORE submit + clearer restart message + re-enroll heads-up that recovery codes regenerate. RecoveryCodesDisplay: surface a "couldn't copy — copy manually" message on clipboard failure. Misc P3s surfaced by the audit that are pure copy/format (logout "signed out of <IdP>" acknowledgement; any remaining vocabulary/label nits). Keep scope tight to copy/format — no new mechanics.
**Files:** `AccountRecovery.vue`, `RecoveryCodesDisplay.vue`, `PasswordTotpForm.vue`, `LogoutView.vue`, `dashboard/src/locales/en.ts` (+ tests).
**Verify:** `cd dashboard && npx vitest run && npx vue-tsc -b`
```json:metadata
{"files":["dashboard/src/components/custom/AccountRecovery.vue","dashboard/src/components/custom/RecoveryCodesDisplay.vue","dashboard/src/components/custom/PasswordTotpForm.vue","dashboard/src/pages/LogoutView.vue","dashboard/src/locales/en.ts"],"verifyCommand":"cd dashboard && npx vitest run && npx vue-tsc -b","acceptanceCriteria":["recovery expectation-setting + copy-fail message; threshold copy clarifications","tests green"]}
```

### Task 15: done-gate (verify + smoke + dist)
**Goal:** Full gate green + dist rebuilt/committed. This cycle is frontend-only, so the live smoke is a regression confirmation (server still boots, flows unaffected).
**Verify:** `CGO_ENABLED=0 go build -tags nodynamic ./... && go vet ./... && go test ./... && cd dashboard && npm run test && npm run build` + live smoke `SMOKE_EXIT=0`.
```json:metadata
{"files":["pkg/webui/dist"],"verifyCommand":"CGO_ENABLED=0 go build -tags nodynamic ./... && go vet ./... && go test ./... && cd dashboard && npm run build","acceptanceCriteria":["full gate green; live smoke SMOKE_EXIT=0; dist rebuilt+committed"]}
```

---

## Review follow-ups (tracked during execution)
- (none yet)

## Done-gate
`go build -tags nodynamic ./...`/`vet`/`go test ./...` (0), `vitest` green, `vue-tsc -b` (0), live smoke `SMOKE_EXIT=0`, rebuild+commit `pkg/webui/dist`.
