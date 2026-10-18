# coss redesign — Phase 4: applications

> **For agentic workers:** REQUIRED SUB-SKILL: Use minipowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the three application kinds — OIDC, SAML and forward-auth — on coss in the approved `#oidc` design: an object page with an identity header, a main column of independently saved sections with consequential saves, an "On this page" rail, an access panel that shows the evaluation order, and lists and create pages to match.

**Architecture:** A shared `AppDetailLayout` composes the foundation's `ObjectHeader`, `SectionRegistryProvider`, `OutlineRail` and `UnsavedBar` around the page's sections. Each section is a `Section` whose footer band carries Discard / Save while dirty; a save with a consequence past the save states it in that footer band and names the action on the primary button, then confirms. The access panel, managers, icon card, URI list and rows field are shared across kinds and rebuilt once.

**Tech Stack:** React 19, TanStack Router/Query/Form, Lingui 6 (en, zh), coss ui (Base UI 1.9) in `dashboard/src/components/ui`, Tailwind 4, Vitest + Testing Library.

**References:**
- Approved mock: `.impeccable/mocks/coss-sandbox/src/scenes/oidc.tsx` (identity header with status badges, copyable client ID, kind and one fact; sections with Frame header/panel/footer; the consequential-save footer "Clients still asking for `email` will be refused until they stop." with "Remove scope and save"; the access evaluation-order strip; group rows with kind badges; the 200px "On this page" rail with an unsaved dot and the danger link last). Run it with `pnpm exec vite --port 5311 --host 127.0.0.1` in that directory; scene `#oidc`. The mock's invented data (last sign-in time, per-group account counts, the Managers description) is NOT to be copied — use only data the API returns and truthful copy.
- Scenario research: `.impeccable/research/admin.md` §2 (access panel, OIDC, SAML, forward-auth, delegated managers), §7 (cross-cutting patterns), §8–§9 (hard problems, defects).
- Finished siblings: the member pages (`pages/Security.tsx`, `pages/security/*`, `pages/Devices.tsx`) for page, section, row, dialog, error and test idioms; foundation components `ObjectHeader`, `OutlineRail`, `UnsavedBar`, `SettingRow`, `ValueSet`, `DangerZone`, `ConfirmDialog`, `SudoDialog`, `SecretReveal`/`SecretRevealDialog`, `InlineFailure`/`useInlineFailure`, `withInlineReport`, `quietQuery`, `ItemList`/`RowMenu`, `EntityAvatar`, `DataTable`.
- Screenshot helper: `node .impeccable/scripts/shoot.mjs <outDir> '<json>'` against `pnpm exec vite --port 5292 --host 127.0.0.1` in `dashboard/`. Admin: `mock.session.role=admin`; delegated manager: `mock.session.role=member&mock.session.managedApps.oidc=2` (see research §2.5; check `src/devtools/mock` for the knobs that actually drive manager visibility); writes `mock.writes=true`; sudo prompt `mock.sudo.fresh=false`. Routes `/admin/oidc-applications`, `/admin/oidc-applications/$clientId`, `/admin/oidc-applications/new`, and the SAML (`/admin/saml-applications`) and forward-auth (`/admin/forward-auth-apps`) equivalents.

## Global Constraints

- Work in `/home/tundra/projects/tundra/prohibitorum-worktrees/develop-coss` on branch `develop-coss`; dashboard commands run in `dashboard/`.
- Nobody uses the dashboard during the migration. No HeroUI↔coss coexistence layers; unmigrated pages may break; `pnpm exec tsc --noEmit` must pass at every commit.
- A file a task rewrites ends with zero imports from `@heroui/react`, `@heroui/styles`, `react-aria-components`, `react-aria` and `tailwind-variants`.
- coss components are the base: restyle one only when the design needs it, preserving its details (hover, active, focus-visible, disabled, dark mode, rhythm, `data-slot` hooks).
- Console material and layout: object pages use the full shell column (`max-w-6xl`) as a main column plus a 200px rail from `lg` (rail hidden below `lg`); sections are `Frame`s; a well is `rounded-xl bg-muted/60 ring-1 ring-border/60`; never a card in a card; flat coss buttons; no gradients or decoration.
- Type: Cal Sans; Paper Mono only for values a person copies or compares (client IDs, entity IDs, URIs, scopes, claim names, slugs, hosts, URNs, headers). Page title is the `ObjectHeader` h1.
- Consequential saves: a save with a consequence past the save (OIDC scope removal, OIDC subject source, SAML NameID format, forward-auth Remote-User, removing the last selected group from a restricted app) states the consequence in the section footer band and names the action on the primary button ("Remove scope and save", "Change subject and save"), then the existing confirm runs. One grammar for all of them.
- A disabled control the reader might try says why in visible text next to it.
- Behaviour is preserved exactly unless a task says otherwise: every route, query, mutation, full-record PUT merge, validation message, sudo-guarded write (rotate secret, delete app), reveal-once secret, manager rules, and the delegated-manager scope described in `.impeccable/research/admin.md` §2.
- One report per failure: inline-reported failures use `withInlineReport`/`useInlineFailure`; reads under a section boundary are quiet; anything with no inline place keeps its toast.
- Keep every piece of information the old pages showed unless the task removes it explicitly.
- User-facing strings are Lingui messages with explicit ids and both `en` and `zh` entries (`pnpm run i18n:extract`, then fill `src/locales/zh/messages.po`; 你 register; catalogue terms). Where this plan names an existing id, confirm it; if it differs, change that id's message rather than adding a duplicate.
- Tests find elements by role, name, label and `data-*`; never by class names. Tests for every new behaviour; keep each page's existing behavioural assertions (rewrite queries, do not delete). Prove one report per failure with a spy `notifyError`.
- Per task checks: `pnpm exec tsc --noEmit`; `pnpm exec biome check <touched files>`; `pnpm exec vitest run <touched and new test files>`; `pnpm run i18n:check` when strings change. No full-suite run inside a task. A visual task ends with screenshots of every changed state — 1440×900 light `en` and 390×844 dark `zh` (dialogs with `"full": false`) — taken after the last code change, every one opened and viewed, every visible defect fixed, every path listed in the report.
- Dependencies stay pinned; no new dependency.
- Commits: Conventional Commits subject, imperative. No `Co-Authored-By` or other attribution trailer. Nothing written may cite this plan, the spec, task numbers or the mock.

---

### Task 1: Application page frame and shared fields

**Files:**
- Create: `dashboard/src/components/custom/AppDetailLayout.tsx`, `AppDetailLayout.test.tsx`, `components/custom/ConsequenceFooter.tsx` (or a `Section` prop — pick one and use it everywhere), its test
- Modify: `components/custom/ObjectHeader.tsx` (only if the header needs a launch action or kind fact it lacks), `OutlineRail.tsx`, `UnsavedBar.tsx`, `EntityIconCard.tsx`, `RowsField.tsx`, `PrincipalSourceField.tsx`, `pages/admin/oidc-applications/UriListField.tsx` (move to `components/custom/UriList.tsx` since SAML/forward-auth do not use it but it is a shared pattern), and their tests

**Interfaces:**
- Produces (binding):
  ```ts
  export function AppDetailLayout(props: {
    header: React.ReactNode;           // an ObjectHeader
    facts?: React.ReactNode;           // rail facts under the outline (e.g. issuer, entity ID)
    children: React.ReactNode;         // the sections, in order; the danger section last
  }): React.ReactElement;              // SectionRegistryProvider + header + grid main/rail (rail from lg) + UnsavedBar
  // Consequential save: a Section footer variant that shows a warning icon, the consequence text and Discard / <action label> buttons
  export function ConsequenceFooter(props: { consequence?: React.ReactNode; actionLabel: React.ReactNode; onDiscard: () => void; pending?: boolean; disabled?: boolean }): React.ReactElement;
  export function UriList(props: { /* TanStack Form field array binding as the current UriListField takes */ }): React.ReactElement; // one mono Input per row, per-row error under it, remove button per row (disabled on the last required row with the visible reason), "Add address"
  ```
  `EntityIconCard`, `RowsField`, `PrincipalSourceField` keep their props but render with coss components only.
- [ ] **Step 1: Write failing tests:** AppDetailLayout renders the header h1, the sections in order, the rail with one link per registered section (danger last, unsaved dot on a dirty section) from `lg`, and the facts; ConsequenceFooter shows the consequence and names the primary button with `actionLabel`, Discard calls `onDiscard`; UriList adds and removes rows, shows the per-row error, keeps the last required row and says why; EntityIconCard/RowsField/PrincipalSourceField keep their existing behaviour assertions.
- [ ] **Step 2: Run them; failures are the new modules.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: one existing OIDC detail page wrapped in AppDetailLayout with a dirty section (rail dot, UnsavedBar) at the two standard shots.**
- [ ] **Step 5: Commit** — `feat(dashboard): add the application page frame and shared fields`

---

### Task 2: Access and managers

Research: §2.1, §2.5, §9 (manager mutation defect).

**Files:**
- Modify: `dashboard/src/components/custom/AppAccessPanel.tsx` (split into `AppAccessSection.tsx` and `AppManagersSection.tsx` if it helps), its tests, `dashboard/src/api/mutations.ts` (manager mutations)

**Interfaces:**
- Access `Section` ("Access"): header action is the Restricted switch with its label; description states the order ("Checked in this order. The first step that matches someone decides." — new id). Body when restricted:
  - an evaluation-order strip in a well: 1 Denied by hand → 2 Allowed by hand → 3 Matches a rule → 4 Everyone else ("Can't sign in"), each step listing the selected groups that take part (manual groups for 1–2, rule groups for 3) by name, or "None selected"; no account counts (not in the API).
  - the selected groups as `ItemList` rows: kind icon, name (link for admins, text for managers), mono slug, badge "Chosen by hand" (manual, `outline`) or "Rule" (`info`), remove action.
  - "Add a group" (`outline` sm) opening the existing picker dialog on coss (multi-select with search, slug description, kind shown per option).
  Open: the state row "Every account can use this" with the kept groups dimmed and the existing note.
- Removing a group: no confirm normally; removing the LAST selected group of a restricted app confirms ("Remove the last group? Nobody can use {app} until you select one." — new ids) — a consequence the old UI did not guard.
- The restrict/open confirms, load errors and picker candidate rules are unchanged; failures report once.
- Managers `Section` (admins only): description states the real scope truthfully ("Managers can change everything on this page except who manages it." — new id); rows with avatar, name, `@username` mono, meta "Assigned {relative}", remove (`destructive-outline` icon-sm) with the existing confirm; "Assign a manager" header action opening the account picker dialog on coss with the disabled reasons.
- For a delegated manager, the page shows a quiet note under the header ("You manage this application." — new id) and no Managers section.
- Defect fix: `assignAppManagerMutationOptions` / `removeAppManagerMutationOptions` must await the POST and throw on failure so errors reach the form and the success toast fires only on success.
- [ ] **Step 1: Write failing tests:** the order strip lists the selected groups under the right steps; group rows show kind badges; removing the last group of a restricted app confirms first and removing another group does not; a failed manager assign shows its error and no success toast (defect); a delegated manager sees the note and no Managers section; one report per failure.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots (on an OIDC detail page): restricted with 2 groups (manual + rule), restricted with none, open with kept groups, add-group dialog, last-group confirm, managers 0 / 2, assign dialog, delegated-manager view.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild application access and managers`

---

### Task 3: OIDC application detail

Research: §2.2. Mock: `#oidc`.

**Files:**
- Modify: `dashboard/src/pages/admin/oidc-applications/AdminOidcApplication.tsx`, `ClientConfigSection.tsx`, `SignInRulesSection.tsx`, `AppearanceSection.tsx`, `IdentityProjectionSection.tsx`, `AliasTableField.tsx`, `OidcScopesField.tsx`, `DangerSection.tsx`, and their tests (create a test file per section where none exists)

**Interfaces:**
- `ObjectHeader`: app icon tile, name, statuses (Enabled/Disabled, Restricted/Open), copyable client ID, kind fact (Confidential/Public), and an "Open {app}" outline action only when a launch URL is set. Rail facts: the issuer (instance origin) copyable.
- Sections in order: **Sign-in** (redirect URIs `UriList`, post-logout URIs `UriList`, scopes as checkbox rows with descriptions and identity scopes, Require PKCE, Require consent), **Access**, **Managers** (Task 2), **Identity** (subject source + claim aliases), **Appearance** (name, launch URL, icon card), **Credentials** (confidential only: rotate secret with sudo and reveal; public: a short line saying a public client has no secret), **Disable or delete** (`DangerZone`, last).
- Consequential saves: scope removal (footer: "Clients still asking for {scopes} will be refused until they stop.", action "Remove scope and save"), subject source change (footer states everyone is signed out of every client, action "Change subject and save"); a removed scope row shows "Will be removed" badge in the list until saved; a scope whose provider is gone shows the warning text.
- The full-record PUT merge, validation and all confirms/sudo/reveal behaviour unchanged.
- [ ] **Step 1: Update tests first**, adding: header statuses and launch action; rail lists the sections with the danger link last; scope removal shows the consequence and the action label; subject change likewise; Credentials hidden content for public clients; one report per failure for each section save.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: confidential + restricted + disabled app, public app, scope removal pending, subject change pending, alias table (incl. errors), rotate secret confirm + reveal, delete with sudo, delegated-manager view — both standard shots each (dialogs viewport-only).**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild the OIDC application page`

---

### Task 4: OIDC list and create

Research: §2.2 (list, create page).

**Files:**
- Modify: `dashboard/src/pages/admin/oidc-applications/AdminOidcApplications.tsx`, `AdminOidcApplicationNew.tsx`, and their tests

**Interfaces:**
- List: `PageHeader` "OIDC applications" with description and "New application" (admins only); the `DataTable` rows: application cell (icon, name, mono client ID; disabled dimmed with a "Disabled" badge rather than only dimming; restricted with a lock and an accessible label), type, redirects (first URI mono + "+n", "None" flagged as a warning text), row action; cursor "load more" unchanged.
- Create page: `PageHeader` "New OIDC application" inside `PageBody`; one `Section` per group of fields (Identity: name + client ID; Type: Confidential/Public as `ChoiceTile` list with descriptions; Sign-in: redirect URIs + scopes + PKCE (locked on for public with the visible reason) + consent; Access: restrict switch with its note); footer Create / Cancel. Conflict on Client ID, confidential reveal before navigating, public navigates directly — unchanged.
- [ ] **Step 1: Update tests first**; add: disabled badge; "None" redirects flagged; PKCE locked reason visible for public.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: list (admin, 10 apps incl. disabled/restricted/no-redirect), list (delegated manager), create confidential, create public, conflict error, secret reveal.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild the OIDC list and create page`

---

### Task 5: SAML applications

Research: §2.3, §9 (NameID confirm, placeholder-as-value, danger styling).

**Files:**
- Modify: `dashboard/src/pages/admin/saml-applications/*` (detail, sections, list, create, AcsRowFields, AttributeMapField) and their tests

**Interfaces:**
- Detail on `AppDetailLayout`: header (icon, name, statuses, copyable entity ID, a fact for the signing-certificate state: "Certificate expires {date}" / "Certificate expired" / "No signing certificate"). Sections: **Sign-in** (session lifetime in minutes, signed AuthnRequest, IdP-initiated), **Access**, **Managers**, **Identity** (NameID format with a consequential save: footer states every service provider sees a new account, action "Change NameID and save", confirm; attribute map as a table on coss — truncation-free name-format display, no placeholder that looks like a value), **Metadata** (ACS endpoints and certificates as read-only rows; "Re-import metadata" dialog now confirms that it replaces endpoints and certificates), **Appearance** (name, icon), **Disable or delete** (same `DangerZone` styling as OIDC).
- List: same idiom as the OIDC list (application cell with entity ID, certificate expiry column with visible wording, access marks). Create page: same idiom as the OIDC create page.
- [ ] **Step 1: Update tests first**; add: NameID change consequence + confirm; re-import confirm; attribute-map validations kept; certificate fact in the header.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: detail (with 0 / 12 attributes, expired cert), NameID pending, re-import dialog + confirm, list, create — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild SAML applications`

---

### Task 6: Forward-auth applications

Research: §2.4.

**Files:**
- Modify: `dashboard/src/pages/admin/forward-auth-apps/*` (detail, sections, list, create) and their tests

**Interfaces:**
- Detail on `AppDetailLayout`: header (icon, name, statuses, copyable client ID, mono host fact). Sections: **Service** (name, hostname), **Headers** (a read-only table on coss: Remote-User with the principal source select — consequential save "Change the user identifier and save" with the existing confirm — plus the stated Remote-Name/Email/Groups rows), **Proxy configuration** (the generated Traefik YAML in a read-only mono block with Copy in the section header; the note), **Access**, **Managers**, **Appearance** (icon), **Disable or delete**.
- List and create on the same idiom as OIDC.
- [ ] **Step 1: Update tests first**; add: Remote-User consequence + confirm; Copy feedback on the snippet.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: detail, Remote-User pending, list, create — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild forward-auth applications`

---

## Phase end (controller)

`mise run ci:frontend` once; a visual QA pass (1440×900, 1920×1080, 390×844 × light, dark × `en`, `zh`) across every application route and dialog above for admin and delegated manager, viewed at 2×; then the whole-phase review, one fix wave, and the branch question.
