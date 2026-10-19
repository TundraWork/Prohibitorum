# coss redesign — Phase 5: identity providers

> **For agentic workers:** REQUIRED SUB-SKILL: Use minipowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the identity-provider pages — list, create, and the OIDC, Steam and VRChat detail pages — on coss in the approved `#idp` / `#idp-test` design, as siblings of the application pages finished in the previous phase, and add the one backend read the design depends on (the latest connection-test outcome).

**Architecture:** The detail page reuses the application object-page frame: `AppDetailLayout` (renamed or generalised only if a neutral name is needed) with `ObjectHeader`, the "On this page" rail and the unsaved bar; each section is a `Section` with its own save and `ConsequenceFooter` for saves with a consequence. Protocol decides which sections exist. Diagnostics keeps its dialogs so the page shape never changes. A small backend addition stores a value-free summary of each provider's latest finished connection test.

**Tech Stack:** Go (huma, the existing KV store), React 19, TanStack Router/Query/Form, Lingui 6 (en, zh), coss ui (Base UI 1.9), Tailwind 4, Vitest + Testing Library.

**References:**
- Approved mock: `.impeccable/mocks/coss-sandbox/src/scenes/idp.tsx` (scene `#idp`: General with described provisioning cards, Connection with described client-authentication options, scopes and domains as `ValueSet`, endpoints discover / by hand with override rows, callback addresses, claim mapping table with a "Last test returned" column, Diagnostics rows) and `idptest.tsx` (`#idp-test`: the test result dialog). Run with `pnpm exec vite --port 5311 --host 127.0.0.1` in `.impeccable/mocks/coss-sandbox`. Invented data in the mock (GitHub branding, counts, the "Last result" button) is not to be copied.
- Scenario research: `.impeccable/research/admin.md` §3 (every field, rule, message, protocol difference, dialog and mock outcome), §7–§9.
- Finished siblings to mirror: the application pages (`pages/admin/oidc-applications/*`, `saml-applications/*`, `forward-auth-apps/*`) and shared components `AppDetailLayout`, `ObjectHeader`, `CopyableValue`/`CopyStatus`, `ConsequenceFooter`, `Section`, `SettingRow`, `ItemList`/`RowMenu` (with `stackActions`), `DangerZone`/`DangerZoneFrame`, `ConfirmDialog`, `SudoDialog`, `SecretRevealDialog`, `ValueSet`, `UriList`, `ChoiceTile`, `DataTable`/`EntityCell`/`APP_DATA_COLUMN`, `InlineFailure`/`useInlineFailure`, `withInlineReport`, `quietQuery`, `JsonBlock`, `RelativeTime`. `dashboard/AGENTS.md` describes the idioms.
- Screenshot helper: `node .impeccable/scripts/shoot.mjs <outDir> '<json>'` against `pnpm exec vite --port 5292 --host 127.0.0.1` in `dashboard/`; viewport shots (`"full": false`) scrolled with `steps`; hide the devtools button as in earlier phases. Admin `mock.session.role=admin`; writes `mock.writes=true`; sudo prompt `mock.sudo.fresh=false`; provider protocols, readiness, linked counts and test outcomes via the knobs in `dashboard/src/devtools/mock/model.ts` (validate every path there; add typed knobs where a state cannot be reached). Routes `/admin/identity-providers`, `/admin/identity-providers/$slug`, `/admin/identity-providers/new`.

## Global Constraints

- Work in `/home/tundra/projects/tundra/prohibitorum-worktrees/develop-coss` on branch `develop-coss`; dashboard commands run in `dashboard/`, Go from the repository root.
- Nobody uses the dashboard during the migration. No HeroUI↔coss coexistence layers; unmigrated pages may break; `pnpm exec tsc --noEmit` must pass at every commit.
- A file a task rewrites ends with zero imports from `@heroui/react`, `@heroui/styles`, `react-aria-components`, `react-aria` and `tailwind-variants`.
- coss components are the base: restyle one only when the design needs it, preserving its details (hover, active, focus-visible, disabled, dark mode, rhythm, `data-slot` hooks).
- Console material and layout: object pages use the shell column as a main column plus the 200px rail from `lg`; sections are `Frame`s; a well is `rounded-xl bg-muted/60 ring-1 ring-border/60`; never a card in a card; flat coss buttons; no gradients or decoration. The identity-provider pages read as siblings of the application pages: same header, section grammar, danger zone, list and create idioms.
- Type: Cal Sans; Paper Mono only for values a person copies or compares (issuer, client ID, slugs, scopes, claim names, endpoints, callback addresses, domains, error codes, request IDs). Page title is the `ObjectHeader` h1 and is the provider's name.
- Consequential saves state the consequence in the section footer band and name the action on the primary button, then the existing confirm runs (one grammar, `ConsequenceFooter`): turning sign-in off ("Turn off sign-in and save"), changing the subject claim while accounts are linked ("Change the subject claim and save").
- A disabled control the reader might try says why in visible text next to it (never only in a tooltip): enable while not ready, "Use what discovery reports" with manual endpoints, "Creates accounts" while sign-in is off.
- Behaviour is preserved exactly unless a task says otherwise: every route, query, mutation, full-record PUT merge, validation message, write-only secret (saved on its own endpoint first, never shown), sudo-guarded delete, rate limits, the connection-test round trip (leave for the provider, return to `?test=<id>`, poll, complete once), and the VRChat operator sign-in flow, as described in `.impeccable/research/admin.md` §3.
- One report per failure: inline-reported failures use `withInlineReport`/`useInlineFailure`; reads under a section boundary are quiet; anything with no inline place keeps its toast.
- Keep every piece of information the old pages showed unless a task removes it explicitly.
- User-facing strings are Lingui messages with explicit ids and both `en` and `zh` entries (`pnpm run i18n:extract`, then fill `src/locales/zh/messages.po`; 你 register; catalogue terms — check existing msgstr before inventing a term; one verb across a flow's title, footer and button). Where this plan names an existing id, confirm it; if its text changes, update the en msgstr too (extract does not refresh it).
- Tests find elements by role, name, label and `data-*`; never by class names. Tests for every new behaviour; keep each page's existing behavioural assertions (rewrite queries, do not delete). Prove one report per failure with a spy `notifyError`.
- Per task checks: `pnpm exec tsc --noEmit`; `pnpm exec biome check <touched files>`; `pnpm exec vitest run <touched and new test files>`; `pnpm run i18n:check` when strings change; Go tasks `go build ./... && go vet ./... && go test <touched packages>`. No full-suite run inside a task. A visual task ends with screenshots of every changed state — 1440×900 light `en` and 390×844 dark `zh` (dialogs viewport-only) — taken after the last code change, every one opened and viewed, every visible defect fixed, every path listed in the report.
- Dependencies stay pinned; no new dependency.
- Commits: Conventional Commits subject, imperative. No `Co-Authored-By` or other attribution trailer. Nothing written may cite this plan, the spec, task numbers or the mock.

---

### Task 1: Latest connection-test outcome (backend + mock)

Research: §3 Diagnostics. The design shows "Passed 2 min ago" on the Diagnostics row and a "Last test returned" column in the claim table. Today a test run lives 10 minutes in the KV store, bound to the browser and the admin who started it, so nothing survives for the page to show.

**Files:**
- Modify: `pkg/federation/providers/oidc/diagnostics.go` (+ test), `pkg/server/handle_oidc_diagnostics.go` (+ test), `pkg/server/server.go` (route), `openapi.yaml` (regenerate with `mise run dev:openapi`), the dashboard API types generated from it (follow the repository's existing generation step), `dashboard/src/api/queries.ts`, `dashboard/src/devtools/mock/*` (+ tests)

**Interfaces:**
- When a run reaches a terminal status (`succeeded` or `failed`, including the `configuration_changed` failure), store a summary under a per-provider KV key (keyed by provider ID, overwritten by each finished run) with a 30-day TTL: `status`, `finishedAt`, `providerIdentity` (the same value `providerIdentity(provider)` the run was started with), and for each mapped identity field (`issuer`, `subject`, `username`, `displayName`, `email`, `emailVerified`, `picture`) its `source` and `claim` and whether a value was `present`. **No claim values and no documents** — the test identity is the running admin's own upstream account, and the summary is readable by every administrator.
- `GET /api/prohibitorum/identity-providers/{slug}/tests/latest` (admin, same guard as the other test routes, no rate limit beyond the shared admin read limits) answers `{ "latest": null }` or `{ "latest": { status, finishedAt, stale, fields: { <field>: { source, claim, present } } } }`, where `stale` is true when the provider's current identity differs from the stored `providerIdentity` (the configuration changed since the test).
- A failure to write the summary never fails the test itself (log and continue).
- Dashboard: `latestConnectionTestQueryOptions(slug)` (quiet by default) and the mock endpoint with a typed knob for none / passed / failed / stale and per-field presence.
- [ ] **Step 1: Write failing Go tests** — a succeeded run writes a summary with sources/claims/presence and no values; a failed run writes `failed`; a configuration change makes the read `stale`; an unknown provider is 404; a non-admin is refused; the summary write failing leaves the run result intact.
- [ ] **Step 2: Run them; they fail for the right reason.**
- [ ] **Step 3: Implement** the store write, the read, the route, the OpenAPI regeneration and the dashboard types/query/mock (mock test for the knob).
- [ ] **Step 4: Run `go build ./... && go vet ./... && go test ./pkg/federation/... ./pkg/server/...` (the server suite can flake under parallel shared-DB runs; re-run a failing test in isolation before treating it as real), plus the dashboard checks.**
- [ ] **Step 5: Commit** — `feat: remember the latest identity-provider connection test`

---

### Task 2: Provider page frame, sign-in, appearance, credentials and danger

Research: §3 (Provider, Steam specifics, Danger zone), §9.

**Files:**
- Modify: `dashboard/src/pages/admin/identity-providers/AdminIdentityProvider.tsx`, `ProviderGeneralSection.tsx`, `ProvisioningModeField.tsx`, `ProviderDangerSection.tsx`, `ProviderActions.tsx`, their tests; create `ProviderAppearanceSection.tsx` and `ProviderCredentialsSection.tsx` (+ tests) if the split helps.

**Interfaces:**
- Frame on `AppDetailLayout`: `ObjectHeader` with the provider's icon, its **name as the h1** (the old page said "Federation"), statuses (Enabled/Disabled; "Sign-in off" when it only verifies; "Not ready" with its reason as visible text below the header), the copyable slug (mono; it appears in callback addresses and the `identity:<slug>` scope), and the protocol fact (OpenID Connect / Steam / VRChat). The rail lists the sections with Disable or delete last.
- Sections by protocol, in this order:
  - OIDC: **Sign-in**, **Connection** (Task 3), **Accounts and claims** (Task 4), **Diagnostics** (Task 5), **Appearance**, **Disable or delete**.
  - Steam: **Sign-in**, **Web API key**, **Appearance**, **Disable or delete**.
  - VRChat: **Sign-in**, **Operator session** (Task 5), **Appearance**, **Disable or delete**.
  - Until Tasks 3–5 land, render their existing section components unchanged in those slots.
- **Sign-in** section: "Allow sign-in" switch with its hint (VRChat's weaker-proof warning as a separate sentence — fix the glued "…linked to it.When…" fragments) and Provisioning as described `ChoiceTile` cards (Creates accounts / Invitation only / Links existing accounts) with the existing descriptions; "Creates accounts" is disabled with a visible reason while sign-in is off, and switching sign-in off still resets provisioning to invitation-only. Turning sign-in off is a consequential save: footer "Anyone who signs in only through this provider will be locked out until an administrator sends them a reset link." (existing copy), action "Turn off sign-in and save", then the existing confirm.
- **Appearance**: name and icon (as on the application pages: name field, `EntityIconControl`), saved with the full-record merge.
- **Secrets leave the danger zone** — setting a secret is not destructive. Create `ProviderSecretRow` (a `SettingRow`-style row: label, status "Stored" / "Not set", Set or Replace opening the existing write-only dialog "Stored encrypted and never shown again."; the mutation stays at row level and the dialog ignores dismissal while saving). Steam gets a **Web API key** section holding that row. For OIDC confidential clients the row belongs in Connection: in this task mount it directly above the existing Connection section component in that slot; Task 3 moves it inside the rebuilt Connection section. Public OIDC clients show no secret row.
- **Disable or delete** (`DangerZoneFrame`, same ids/labels idiom as the application pages): Enable/Disable (disable confirms with the existing copy; enable runs straight through but is disabled while not ready, with the protocol-specific reason as visible text); Delete (sudo) confirm listing the existing consequences with the linked-account count, unfinished-invitation count and the `identity:<slug>` scope note.
- [ ] **Step 1: Update tests first**, adding: header shows the name, slug copy and protocol; sections per protocol in order with the danger link last; "Creates accounts" disabled with its reason while sign-in is off; turning sign-in off shows the consequence and the action label, then confirms; enable disabled with its visible reason when not ready; Steam key set/replace from its own section; delete lists the counts and goes through sudo when the window is closed; one report per failure for each save.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: OIDC, Steam and VRChat headers + rails; sign-in off pending; disabled not-ready provider; Steam key dialog; delete confirm + sudo — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild the identity-provider page frame and its common sections`

---

### Task 3: OIDC connection

Research: §3 Connection.

**Files:**
- Modify: `ProviderConnectionSection.tsx`, `ClientAuthMethodField.tsx`, `ResponseTypeField.tsx`, `EndpointsField.tsx`, `CallbackPreview.tsx`, `provider-validation.ts`, their tests.

**Interfaces:**
- One section, one save, full-record merge; the write-only client secret is saved first on its own endpoint, then the configuration (unchanged order).
- Fields: Issuer URL (mono, existing validation, server `bad_request` mapped onto it); Client ID (mono); Client authentication as described options (use what discovery reports — unavailable with manual endpoints with the visible reason; `client_secret_basic`; `client_secret_post`; public client with its PKCE note); **Client secret** — `ProviderSecretRow` (from the previous task) moved inside this section; when the chosen method needs a secret and none is stored, the inline write-only secret field appears as today and is saved first on its own endpoint; Sign-in response as described options with the existing hints and the Keycloak/Microsoft note; Scopes as `ValueSet` (existing rules: no spaces, quotes or backslashes; "The provider must support every scope listed here."); Endpoints: Discover automatically (with "Override an endpoint" rows, each removable) / Enter the endpoints (authorization and token required, JWKS required or explicitly "None — skip the ID token", UserInfo required when JWKS is none) — the JWKS-none choice is stated as its own explicit option, not an empty field; "Send the access token when fetching the avatar" switch with its hint; **Callback addresses** (sign-in, link an account, connection test) as copyable mono values using `CopyableValue`, with "Register these with the provider." guidance.
- [ ] **Step 1: Update tests first**, adding: discovery option disabled with its reason under manual endpoints; JWKS none as an explicit option and the UserInfo requirement it triggers; secret saved before the configuration and never echoed; callback addresses copyable; one report per failure; existing validation messages kept.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: discovery with two overrides, manual endpoints with JWKS none, public client, secret not set / stored, validation errors — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild the identity-provider connection section`

---

### Task 4: Accounts and claims

Research: §3 Accounts and claims.

**Files:**
- Modify: `ProviderClaimsSection.tsx`, `ClaimMappingTable.tsx`, their tests.

**Interfaces:**
- Allowed email domains as `ValueSet` (empty means every domain; domain shape check); "Require a verified email address" switch (default on).
- Claim mapping table on coss: Account field | Provider claim (mono input) | **Last test returned** (from Task 1's summary: the source and claim that produced the field, e.g. "ID token · preferred_username", or "Missing" in warning text when the field was absent; the column header notes "From the test {relative time}" and states "Configuration changed since" when stale; the column is omitted when there is no summary). The dotted-name note is kept. Subject row keeps "Accounts are linked by this value."
- Changing the subject claim while `linkedAccountCount > 0` is a consequential save: footer states the existing consequence ("{n} accounts are linked to {name} by their {current} claim. After switching to {next}, those links no longer match…"), action "Change the subject claim and save", then the existing confirm. With no linked accounts it saves normally.
- [ ] **Step 1: Update tests first**, adding: last-test column from the summary (present, missing, stale, absent); subject change consequence + confirm only when accounts are linked; one report per failure.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: table with a passed summary, with missing fields, stale, no summary; subject change pending — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild accounts and claims for identity providers`

---

### Task 5: Diagnostics and the VRChat operator session

Research: §3 Diagnostics, Operator session.

**Files:**
- Modify: `ProviderDiagnosticsSection.tsx` (split into `EffectiveConfigDialog.tsx` and `ConnectionTestDialog.tsx` if it helps), `ProviderOperatorSection.tsx`, `StageRow` and their tests.

**Interfaces:**
- Diagnostics section: two `ItemList` rows. **Effective configuration** → "Read configuration" opening the dialog (read time, field | value | source badge Discovered / Overridden / Manual, special values stated in words, the test callback address via `CopyableValue`, footer Close / Read again). **Connection test** → status from Task 1 as a badge beside the title ("Passed {relative}" success / "Failed {relative}" destructive / "Configuration changed since" when stale / nothing when none) and "Start a test" (outline, external-link icon) which leaves for the provider exactly as today.
- The result dialog (opened on return with `?test=<id>`): run status line, ordered stage list (stage name, Passed/Failed/Skipped/Pending icon + word, duration, HTTP status, mono error code, endpoint, request ID, "Provider message:" wrapped), then tabs Mapped fields (value + source badge; subjects beyond JS integer precision shown as the string received) / ID token (`JsonBlock`) / UserInfo (`JsonBlock`) with the existing empty-state copy; polling every second for 30 s then the "taking longer" warning with Check again; complete once; footer Close / Test again. A start failure keeps its toast. Rate-limit refusals report once.
- VRChat **Operator session** section: status badge (Session valid / Session expired / Not set up) with "Checked {relative}", Check and Sign in / Sign in again; the sign-in dialog with username/password, then the verification-method choice and code, "Waiting for VRChat…", password cleared after use, the old session kept until the new one completes. Mutations at section level; the dialog ignores dismissal while a request is pending.
- [ ] **Step 1: Update tests first**, adding: last-test badge states; effective-config dialog values and sources; result dialog for succeeded / fallback (big-integer subject as string) / failed outcomes; polling timeout warning and Check again; operator sign-in with a verification step; one report per failure.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: Diagnostics rows (passed, failed, stale, none); effective-config dialog; result dialog for each outcome; taking-longer warning; operator session states and the sign-in + verification dialog — both standard shots each (dialogs viewport-only).**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild identity-provider diagnostics and the operator session`

---

### Task 6: Identity-provider list and create

Research: §3 List, Create page.

**Files:**
- Modify: `AdminIdentityProviders.tsx`, `AdminIdentityProviderNew.tsx`, `CallbackPreview.tsx` (shared with Task 3), their tests.

**Interfaces:**
- List on the application-list idiom: `PageHeader` "Identity providers" with description and "Add a provider"; `DataTable` rows: provider cell (`EntityCell`: icon, name, mono slug, Disabled badge, a "Sign-in off" mark), Protocol (word) with the provisioning mode as a second line, Accounts (linked count, tabular numerals), readiness ("Not ready" with its short reason as visible text, nothing when ready), row action. Column widths from the shared data-column constant so the list reads as a sibling of the application lists. Quiet list read + `InlineFailure`; empty state with one line of help (create action only in the header).
- Create page on the application-create idiom: `PageHeader` in `PageBody`; Sections with icons — **Protocol** (`ChoiceTile` cards with the existing descriptions), **Provider** (name, slug with its rules), **Callback addresses** (the preview once the slug is valid, copyable, with "Register these with the provider." guidance); footer Create + Cancel (`admin.cancel`); `withInlineReport` create; slug conflict on the field; list invalidation on success with its own spy test.
- [ ] **Step 1: Update tests first**, adding: list columns and states (disabled, sign-in off, not ready with reason); one report per failure for the list read and the create; slug conflict on the field; list invalidation.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: list (8 providers across protocols and states), empty list, create for each protocol, slug error, callback preview — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild the identity-provider list and create page`
