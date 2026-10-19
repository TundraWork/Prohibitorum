# coss redesign — Phase 7: users, invitations and logs

> **For agentic workers:** REQUIRED SUB-SKILL: Use minipowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the directory pages that remain on HeroUI — the account detail page, the account list, invitations (list and form), the audit log and the administration landing page — on coss, as siblings of the application, identity-provider and group pages, and fix the defects the research found on them.

**Architecture:** The account page becomes an object page on `AppDetailLayout` (identity header, independently saved sections, the "On this page" rail, Disable or delete last) instead of three URL-synced tabs; an old `?tab=` link scrolls to the matching section. Lists use the shared `DataTable` idiom with filters in a toolbar above the frame. The audit log keeps its server-side, URL-held filters and cursor paging and gains a consistent expandable row.

**Tech Stack:** React 19, TanStack Router/Query/Form, Lingui 6 (en, zh), coss ui (Base UI 1.9), Tailwind 4, Vitest + Testing Library.

**References:**
- Scenario research: `.impeccable/research/admin.md` §4 (account detail and list: every field, rule, message, sudo point, confirm, the identity filter), §5 (logs: time ranges, filters, categories, events, columns, row expansion, states, volumes), §7 (cross-cutting patterns), §9 (defects: username never shown, `font-mono` leaking onto the Attributes label, Disabled duplicated in Profile and Danger, Logs Time column misaligned, mock role `member` not matching `user`/`admin`). Invitations: the current `AdminInvitations.tsx` / `AdminInvitationForm.tsx` and their tests are the behavioural source; `.impeccable/research/public.md` §5 describes what an invitation does on the public side (bound provider, link reveal).
- Finished siblings to mirror: `pages/admin/oidc-applications/*` (object page, Credentials reveal, danger zone), `pages/admin/identity-providers/*`, `pages/admin/groups/*` (list and create idioms, `EntityPicker`, decisions rows), and member pages (`pages/security/*`, `pages/devices/*` for passkey, session and token rows). Shared components: `AppDetailLayout`, `ObjectHeader`, `CopyableValue`, `Section`, `ConsequenceFooter`, `SettingRow`, `ItemList`/`RowMenu` (`stackActions`, `iconBare`), `DangerZoneFrame`, `ConfirmDialog`, `SudoDialog`, `SecretRevealDialog`, `EntityAvatar`, `EntityPicker`, `DataTable`/`EntityCell`/`APP_DATA_COLUMN`, `PageHeader`/`PageBody`, `CancelLink`, `TableEmptyState`, `InlineFailure`/`useInlineFailure`, `withInlineReport`, `quietQuery`, `RelativeTime`, `JsonBlock`, `SegmentedControl`, `invalidateAfterDelete`. `dashboard/AGENTS.md` describes the idioms.
- No approved mock scene exists for these pages; the material, layout and component rules below and the finished sibling pages are the design source.
- Screenshot helper: `node .impeccable/scripts/shoot.mjs <outDir> '<json>'` against `pnpm exec vite --port 5292 --host 127.0.0.1` in `dashboard/`; viewport shots (`"full": false`) scrolled with `steps`; hide the devtools button as in earlier phases. Admin `mock.session.role=admin`; writes `mock.writes=true`; sudo prompt `mock.sudo.fresh=false`; account, invitation and audit volumes and states via the knobs in `dashboard/src/devtools/mock/model.ts` (validate every path; add typed knobs with tests where a state cannot be reached). Routes `/admin/users`, `/admin/users/$id`, `/admin/invitations`, `/admin/invitations/new` (check the route files), `/admin/logs`, `/admin`.

## Global Constraints

- Work in `/home/tundra/projects/tundra/prohibitorum-worktrees/develop-coss` on branch `develop-coss`; dashboard commands run in `dashboard/`.
- Nobody uses the dashboard during the migration. No HeroUI↔coss coexistence layers; unmigrated pages may break; `pnpm exec tsc --noEmit` must pass at every commit.
- A file a task rewrites ends with zero imports from `@heroui/react`, `@heroui/styles`, `react-aria-components`, `react-aria` and `tailwind-variants` (test files included).
- coss components are the base: restyle one only when the design needs it, preserving its details (hover, active, focus-visible, disabled, dark mode, rhythm, `data-slot` hooks).
- Console material and layout: object pages use `AppDetailLayout` (main column + 200px rail from `lg`); lists and the log use the shell column with `PageHeader`; sections are `Frame`s; a well is `rounded-xl bg-muted/60 ring-1 ring-border/60`; never a card in a card; flat coss buttons; no gradients or decoration. These pages read as siblings of the application, identity-provider and group pages.
- Type: Cal Sans; Paper Mono only for values a person copies or compares (usernames as `@username`, subjects, token hints, key IDs, IPs, request IDs, JSON). The page title is the `ObjectHeader` h1 (the account's display name, falling back to the username).
- Consequential saves use `ConsequenceFooter` and then the existing confirm (one grammar): changing an account's role (the save names the action, e.g. "Make admin and save" / "Make user and save", then sudo as today).
- A disabled control the reader might try says why in visible text next to it (e.g. disabling an admin: "An admin has to be made a user before they can be disabled.").
- Behaviour is preserved exactly unless a task says otherwise: routes, queries, mutations, the full-record PUT (attributes replace the stored object), validation messages, sudo-guarded writes (role change, delete account, issue a registration link), reveal-once links, server-side filters held in the URL, cursor paging, and delete navigating to the list.
- One report per failure: inline-reported failures use `withInlineReport`/`useInlineFailure`; reads under a section boundary are quiet; anything with no inline place keeps its toast.
- Show only data the API returns; never present a guessed value as fact. Keep every piece of information the old pages showed unless a task removes it explicitly.
- User-facing strings are Lingui messages with explicit ids and both `en` and `zh` entries (`pnpm run i18n:extract`, then fill `src/locales/zh/messages.po`; 你 register; catalogue terms — check existing msgstr first; one verb across a flow; complete sentences, never stitched fragments; no invisible characters). When an existing id's English text changes, update its en msgstr too.
- Tests find elements by role, name, label and `data-*`; never by class names. Tests for every new behaviour; keep existing behavioural assertions (rewrite queries, do not delete). Prove one report per failure with a spy `notifyError`.
- Per task checks: `pnpm exec tsc --noEmit`; `pnpm exec biome check <touched files>`; `pnpm exec vitest run <touched and new test files>`; `pnpm run i18n:check` when strings change. No full-suite run inside a task. A visual task ends with screenshots of every changed state — 1440×900 light `en` and 390×844 dark `zh` (dialogs viewport-only) — taken after the last code change, every one opened and viewed, every visible defect fixed, every path listed in the report.
- Dependencies stay pinned (exact versions). A new dependency is fine when it suits the design; say why in the report.
- Commits: Conventional Commits subject, imperative. No `Co-Authored-By` or other attribution trailer. Nothing written may cite this plan, the spec, task numbers or a mock.

---

### Task 1: Account page frame, profile and danger zone

Research: §4 (header, Profile tab, Danger tab), §7, §9.

**Files:**
- Split `dashboard/src/pages/admin/AdminUser.tsx` (997 lines) into a folder `dashboard/src/pages/admin/users/`: `AdminUser.tsx` (the page on `AppDetailLayout`), `AccountProfileSection.tsx`, `AccountRegistrationSection.tsx`, `AccountDangerSection.tsx`, plus tests; keep the Access content importable from its current place for Task 2 (or move it as-is into `users/AccountAccessLegacy.tsx`) so `tsc` stays green; update the route file.

**Interfaces:**
- `ObjectHeader`: the account's avatar (`EntityAvatar` person, image when the account has one), the display name as h1 (username when empty), `@username` copyable in mono (the username is shown — research defect), statuses Admin/User and Enabled/Disabled, facts "Joined {date}" and "Last signed in {relative}" / "Never signed in" when the API returns them.
- An old `?tab=profile|access|danger` link scrolls to the matching section; the route keeps accepting the parameter.
- **Profile** section: display name, email (with a "Verified" / "Not verified" note when `emailVerified` is known), role (`SegmentedControl` or `Select` User / Admin; a stored role outside these two shows as its raw value with a note instead of an empty "Select an item"), attributes (JSON, mono only on the textarea — not its label or hint; both validation messages kept; empty saves `{}`; a note that saving replaces the whole object), the OIDC subject as `CopyableValue` with its existing hint. One save, full-record PUT. Changing the role is a consequential save: footer "{name} gets / loses access to the administration pages.", action "Make admin and save" / "Make user and save", then sudo as today. The duplicated Disabled switch is removed from Profile (it lives in Disable or delete).
- **Registration link** section: "Issue a registration link" (sudo) → the existing non-dismissable reveal with the expiry line; the description says an earlier link stops working.
- **Disable or delete** (`DangerZoneFrame`): Disable/Enable with the existing confirms (disable copy kept); for an admin the Disable action is disabled with the visible reason; the server refusal still reports once inline. Delete: confirm, then sudo, then navigate to the list, using `invalidateAfterDelete`.
- [ ] **Step 1: Write failing tests** — header facts and `@username` copy; `?tab=` scroll targets; profile save body (full record, attributes replace); the role consequence + action label + sudo; raw unknown role shown; attributes validation messages; disable blocked for an admin with its reason; enable/disable confirms; registration link sudo + reveal; delete confirm → sudo → navigate with no 404 toast; one report per failure.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: header (admin and user, enabled and disabled, with and without last sign-in); profile with attributes; role change pending; attributes error; registration link reveal; disable blocked for admin; delete confirm + sudo — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild the account page frame, profile and danger zone`

---

### Task 2: Account access

Research: §4 (Access tab).

**Files:**
- Create: `dashboard/src/pages/admin/users/AccountIdentitiesSection.tsx`, `AccountPasskeysSection.tsx`, `AccountSessionsSection.tsx`, `AccountTokensSection.tsx`, tests; mount them in `AdminUser.tsx` between Profile and Registration link; delete the legacy access code.

**Interfaces:**
- Four sections, each with its own quiet read, empty state and inline failure, rows as `ItemList` mirroring the member pages' passkey, session and token rows (`pages/security/*`, `pages/devices/*`):
  - **Connected identities**: provider name, protocol word ("OpenID Connect", "Steam", "VRChat"; SAML if present), mono subject (wrapping at sensible points); read-only, with a description saying administrators cannot unlink them.
  - **Passkeys**: nickname or "Unnamed passkey", "Added {relative} · Last used {relative}" / "Not used yet", the credential id tail in mono; "Remove" behind the existing confirm.
  - **Sessions**: a readable device line from the user agent (as the member Devices page does; full string available), IP in mono, "Started {relative}"; per-session "End" with the confirm; "Sign out everywhere" (with the confirm) shown when there is more than one session.
  - **Access tokens**: name, access badge, mono hint, scope summary, expiry, last used; "Revoke" with the confirm.
- Every write reports once inline and refreshes its list.
- [ ] **Step 1: Write failing tests** — each section's rows, empty and failure (quiet, once); remove passkey, end session, sign out everywhere (only with >1), revoke token, each with confirm and one report per failure.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: all four sections populated; all empty; a long user agent and a long subject at 390; each confirm — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): show an account's identities, passkeys, sessions and tokens on coss`

---

### Task 3: Account list

Research: §4 (List).

**Files:**
- Modify: move `dashboard/src/pages/admin/AdminUsers.tsx` to `users/AdminUsers.tsx` (+ `user-filters.ts` if it belongs there), tests; update the route.

**Interfaces:**
- `PageHeader` "Users" with description and "Invite a user" (to the invitation form).
- Filter toolbar above the table (sibling idiom: compact controls in a row, wrapping on narrow screens): the existing filters kept and held in the URL, including the identity filter (Provider → Field `email`/`subject` → Match `eq`/`contains` → value) with its validation; an active-filter count and "Clear filters".
- `DataTable`: account cell (`EntityCell`: avatar, display name, mono `@username`, marks "Admin" and "Disabled"), Last signed in (relative, "Never" when absent), pinned open action; column widths from the shared data-column constant. Quiet list read + `InlineFailure`; empty states distinguish "No accounts yet" from "No account matches these filters" (with "Clear filters"); cursor "Load more".
- [ ] **Step 1: Update tests first** — columns and marks; filters in the URL; identity filter validation; empty vs no-match; one report per failure.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: list (mixed roles and states), filters open with an identity filter, no-match, empty, failure — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild the account list`

---

### Task 4: Invitations

**Files:**
- Modify: move `dashboard/src/pages/admin/AdminInvitations.tsx` and `AdminInvitationForm.tsx` into `dashboard/src/pages/admin/invitations/`, tests; update the routes.

**Interfaces:**
- Read the current files and tests first; every field, rule, message and state they have is kept (bound provider, role, expiry, reveal-once link, revoke, statuses).
- List: `PageHeader` "Invitations" with description and "Invite a user"; `DataTable` rows — invitee cell (name or email as the old list shows it), status words with tone (pending / used / expired / revoked as the API reports), bound provider when set, created/expires relative, row action (revoke behind a confirm where the old page allowed it). Quiet read + `InlineFailure`; empty state with one help line; cursor paging if the API pages.
- Form: the application-create idiom (`PageHeader` in `PageBody`, Sections with icons, `ChoiceTile` where a choice exists, footer Create + `CancelLink`, `withInlineReport`), then the existing reveal-once link dialog (`SecretRevealDialog` idiom) with its expiry line; list invalidation with its own spy test.
- [ ] **Step 1: Update tests first** — keep every existing behavioural assertion; add list states, revoke confirm, form sections, reveal, invalidation, one report per failure.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: list (each status), empty, form (with and without a bound provider), validation errors, link reveal, revoke confirm — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild invitations`

---

### Task 5: Audit log

Research: §5.

**Files:**
- Modify: move `dashboard/src/pages/admin/AdminLogs.tsx` (+ `audit-filters.ts`, `audit-vocabulary.ts`) into `dashboard/src/pages/admin/logs/`, tests; update the route.

**Interfaces:**
- `PageHeader` "Logs" with description. Toolbar: time range (`Select`: Last hour / Last 24 hours (default) / Last 7 days / Last 30 days / All time / Custom range; custom shows a date range control with both existing messages), a Filter popover with the active-count chip (Category, Event, Account via `EntityPicker`, "Clear filters"), and Refresh (icon button with an accessible name). The Event options are narrowed to the events that occur under the chosen Category when the vocabulary knows that mapping; otherwise they stay flat (do not invent a mapping — derive it from `audit-vocabulary.ts` or the server's catalogue).
- Table: Time (absolute to the second, tabular numerals), Account (link, or "System" for system events instead of "—"), Category, Event (failure events in the destructive tone as a badge), IP (mono); every row has the same leading expander column (a row with nothing to expand gets an inert spacer) so Time always aligns (research defect). Expanded: a definition list with User-Agent (full, wrapping), IP and Details (`JsonBlock`).
- Unknown categories/events show raw in mono. Filters stay in the URL and server-side; presets re-anchor on Refresh as today; cursor "Load more"; states for empty ("No events in this range"), incomplete custom range and failure (quiet, once).
- [ ] **Step 1: Update tests first** — keep existing behavioural assertions; add aligned rows with and without detail, failure-tone events, system rows, category-narrowed events (only if the mapping exists), URL round-trip, one report per failure.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: log (mixed rows), expanded row with JSON, filters open, custom range with each message, empty, failure, narrow layout — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild the audit log`

---

### Task 6: Administration landing page

**Files:**
- Modify: `dashboard/src/pages/admin/AdminHome.tsx`, its test (create if none).

**Interfaces:**
- `PageHeader` "Administration" with the existing summary as its description, in `PageBody`; no invented counts. If `ConsoleCard` has no other users after this phase's tasks, delete it (and its tests); same for `ConsoleTabs` once the account page no longer uses it.
- [ ] **Step 1: Update or write the test.**
- [ ] **Step 2: Run it.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: the landing page — both standard shots.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild the administration landing page`
