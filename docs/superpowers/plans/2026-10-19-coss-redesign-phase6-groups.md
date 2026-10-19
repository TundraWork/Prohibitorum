# coss redesign — Phase 6: user groups

> **For agentic workers:** REQUIRED SUB-SKILL: Use minipowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the user-group pages — list, create, the rule-group page with its rule builder and side column, and the manual-group page with the allow/deny decisions it has never had — on coss in the approved `#group` design, as siblings of the application and identity-provider pages, and fix the rule-editor defects the research found.

**Architecture:** A group page uses the object-page header (`ObjectHeader`) over a main column of independently saved sections and, from `lg`, a 300px side column of read-only context (matches, account explanation, used by) instead of the outline rail; below `lg` the side column follows the main column. The rule builder is rebuilt as sentence rows (fact · is / is not · value) inside nested any/all wells, with errors marked on the row they belong to and Save blocked while the editor knows the rule is invalid. Manual groups get a decisions section.

**Tech Stack:** React 19, TanStack Router/Query/Form, Lingui 6 (en, zh), coss ui (Base UI 1.9), Tailwind 4, Vitest + Testing Library.

**References:**
- Approved mock: `.impeccable/mocks/coss-sandbox/src/scenes/group.tsx` (scene `#group`: header with kind badge and copyable slug; "Who this group matches" frame with Builder / JSON tabs, the root sentence "Accounts that meet [all/any] of these", condition rows `[fact] [is | is not] [value] [×]` with human labels, a nested well "Match [any] of these", "+ Condition" / "+ Group", an error footer that names the problem and disables "Save rule"; side column "Matches right now", "Check an account", "Used by"). Run with `pnpm exec vite --port 5311 --host 127.0.0.1` in `.impeccable/mocks/coss-sandbox`. Invented data in the mock (avatars, "of 42 accounts" unless the API returns a total, app names) is not to be copied.
- Scenario research: `.impeccable/research/admin.md` §1 (list, form, rule grammar and limits, the preview, every validation message, the editor defects, the decisions API, edge cases), §7–§9.
- Finished siblings to mirror: application and identity-provider pages (`pages/admin/oidc-applications/*`, `pages/admin/identity-providers/*`) and shared components `ObjectHeader`, `CopyableValue`, `Section`, `ConsequenceFooter`, `SettingRow`, `ItemList`/`RowMenu` (`stackActions`), `DangerZoneFrame`, `ConfirmDialog`, `EntityPicker`, `EntityAvatar`, `DataTable`/`EntityCell`/`APP_DATA_COLUMN`, `ChoiceTile`, `InlineFailure`/`useInlineFailure`, `withInlineReport`, `quietQuery`, `RelativeTime`, `JsonBlock`, the coss `Tabs`, `Select`, segmented control idiom used by the mock (`Seg`). `dashboard/AGENTS.md` describes the idioms.
- Screenshot helper: `node .impeccable/scripts/shoot.mjs <outDir> '<json>'` against `pnpm exec vite --port 5292 --host 127.0.0.1` in `dashboard/`; viewport shots (`"full": false`) scrolled with `steps`; hide the devtools button as in earlier phases. Admin `mock.session.role=admin`; writes `mock.writes=true`; group counts, kinds, rules, decisions, preview and explain outcomes via the knobs in `dashboard/src/devtools/mock/model.ts` (validate every path; add typed knobs where a state cannot be reached). Routes `/admin/groups`, `/admin/groups/new`, `/admin/groups/$groupId`.

## Global Constraints

- Work in `/home/tundra/projects/tundra/prohibitorum-worktrees/develop-coss` on branch `develop-coss`; dashboard commands run in `dashboard/`.
- Nobody uses the dashboard during the migration. No HeroUI↔coss coexistence layers; unmigrated pages may break; `pnpm exec tsc --noEmit` must pass at every commit.
- A file a task rewrites ends with zero imports from `@heroui/react`, `@heroui/styles`, `react-aria-components`, `react-aria` and `tailwind-variants` (test files included).
- coss components are the base: restyle one only when the design needs it, preserving its details (hover, active, focus-visible, disabled, dark mode, rhythm, `data-slot` hooks).
- Console material and layout: the group page uses the shell column as a main column plus, from `lg`, a 300px side column (`grid-cols-[minmax(0,1fr)_300px]`, side column sticky under the console header); sections are `Frame`s; a well is `rounded-xl bg-muted/60 ring-1 ring-border/60`; never a card in a card (a nested rule group is a well inside the frame panel, and a deeper group is a well inside a well only because the grammar nests — keep each nesting level visually light and indented, never a bordered card); flat coss buttons; no gradients or decoration. Group pages read as siblings of the application and identity-provider pages.
- Type: Cal Sans; Paper Mono only for values a person copies or compares (slugs, provider slugs in rules, JSON). Page title is the `ObjectHeader` h1 (the group's name).
- Rule builder grammar is the server's, unchanged: `{"version":1,"condition":<node>}`; nodes `all`/`any` (1–32 children), `not` wrapping a leaf only, leaves `connection.provider` (provider slug), `connection.protocol` (`oidc`, `steam`, `vrchat`), `login_method` (`passkey`, `password_totp`, `federation`), `avatar` (`any`, `user_uploaded`); depth ≤ 8, nodes ≤ 64. Every client limit message from the research is kept.
- A disabled control the reader might try says why in visible text next to it (Save blocked by a known rule problem; Delete blocked while applications use the group; "+ Group" at maximum depth).
- Behaviour is preserved exactly unless a task says otherwise: routes, queries, mutations, validation messages, the slug conflict landing on the field, the debounced server preview (400 ms, newest response wins, existing groups only), the kind being fixed after creation, and create navigating to the new group.
- One report per failure: inline-reported failures use `withInlineReport`/`useInlineFailure`; reads under a section boundary are quiet; anything with no inline place keeps its toast.
- Show only data the API returns; never present a guessed value as fact.
- User-facing strings are Lingui messages with explicit ids and both `en` and `zh` entries (`pnpm run i18n:extract`, then fill `src/locales/zh/messages.po`; 你 register; catalogue terms — check existing msgstr first; one verb across a flow; complete sentences, never stitched fragments). When an existing id's English text changes, update its en msgstr too.
- Tests find elements by role, name, label and `data-*`; never by class names. Tests for every new behaviour; keep existing behavioural assertions (rewrite queries, do not delete). Prove one report per failure with a spy `notifyError`.
- Per task checks: `pnpm exec tsc --noEmit`; `pnpm exec biome check <touched files>`; `pnpm exec vitest run <touched and new test files>`; `pnpm run i18n:check` when strings change. No full-suite run inside a task. A visual task ends with screenshots of every changed state — 1440×900 light `en` and 390×844 dark `zh` (dialogs viewport-only) — taken after the last code change, every one opened and viewed, every visible defect fixed, every path listed in the report.
- Dependencies stay pinned; no new dependency.
- Commits: Conventional Commits subject, imperative. No `Co-Authored-By` or other attribution trailer. Nothing written may cite this plan, the spec, task numbers or the mock.

---

### Task 1: Group page frame, details and deletion

Research: §1.3, §1.4 "Missing but available in the API" (delete, applications).

**Files:**
- Split `dashboard/src/pages/admin/AdminGroupForm.tsx` (1222 lines, create + edit + rule editor) into a folder `dashboard/src/pages/admin/groups/`: `AdminGroup.tsx` (the edit page), `GroupDetailsSection.tsx`, `GroupDangerSection.tsx`, `group-validation.ts` (shared with the create page later), plus tests. Leave the rule editor code in place (moved as-is into `groups/RuleEditorLegacy.tsx` or kept importable) for Task 2 to rebuild; update the route files' imports.

**Interfaces:**
- `ObjectHeader`: kind icon tile (rule: list-filter; manual: hand/user-check — pick fitting lucide icons), the group's name as h1, kind badge ("Rule group" info / "Manual group" outline), copyable slug (`CopyableValue`), and a "Sent to applications" / "Not sent to applications" status from `exposedToDownstream` (one verb with the switch).
- Layout: main column + side column from `lg` (side column content arrives in Tasks 3–4; this task renders a "Used by" frame there listing the applications from `groupApplicationsQueryOptions` — icon, name linking to the app page, kind badge — with an empty line "No application uses this group." and a quiet read).
- **Details** section: name, slug (rules and messages unchanged; `group_slug_conflict` on the field), description; "Send to applications" switch for `exposedToDownstream` with truthful copy — it controls whether the group's slug is sent to applications that selected it (in the `groups` claim, the SAML `groups` attribute and `Remote-Groups`), not whether the group can be selected; the kind shown read-only with "A group's kind is set when it is created and cannot be changed." One save, `ConsequenceFooter` when turning the switch off states that applications stop receiving the slug ("Applications that selected {name} stop receiving {slug} at the next sign-in."), action "Stop sending and save", then a short confirm.
- **Disable or delete** → for groups only **Delete** (`DangerZoneFrame`, same idiom as the other pages): confirm naming the group; when applications still use the group the Delete action is disabled with the visible reason "{n} applications use this group. Remove it from their access first." (from the applications read); a server refusal (still in use) is reported once inline. Success navigates to the list.
- [ ] **Step 1: Write failing tests** — header (name, kind badge, slug copy, sending status); details save with full body and slug conflict on the field; switching sending off shows the consequence + action label, then confirms; delete disabled with its reason while used, enabled otherwise, confirm, navigate; one report per failure for each write and quiet reads.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: rule group and manual group headers + details; sending-off pending; used-by list (0 and several apps); delete blocked and allowed + confirm — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild the group page frame, details and deletion`

---

### Task 2: Rule builder

Research: §1.4 (grammar, editor model, validation, server preview, behaviour bugs, edge cases).

**Files:**
- Create: `dashboard/src/pages/admin/groups/RuleBuilder.tsx` (the builder), `rule-model.ts` (pure tree model: parse/serialize the document, add/remove/update nodes, limits, path ↔ node mapping), `rule-labels.ts` (human labels for facts and values), `RuleJsonTab.tsx`, `GroupRuleSection.tsx` (the section on the group page), tests for each (`rule-model.test.ts` covers the model exhaustively). Delete the legacy editor.

**Interfaces:**
- Section "Who this group matches" with Builder / JSON tabs in the header (coss `Tabs`).
- Root sentence: "Accounts that meet [all ▾ / any ▾] of these" — the root combinator now has a control (fixes the defect).
- Condition row (one line at ≥ `sm`, wrapping cleanly on narrow screens): fact `Select` with human labels ("Signs in with", "Connected to", "Connected through", "Profile picture"), segmented `is` / `is not` (`not` wraps the leaf; "is not" tinted destructive like the mock), value `Select` with human labels ("A passkey", "Password and authenticator", "A linked provider"; providers by display name with "· disabled" / "· sign-in off" notes from the provider catalogue; "Any picture", "Uploaded by them"; protocols "OpenID Connect", "Steam", "VRChat"), remove (×, `aria-label` names the condition). A saved provider slug that no longer exists shows "Unknown provider ({slug})" in mono and is marked invalid.
- Nested group: a well "Match [all/any] of these" with its own remove, indented children, "+ Condition" / "+ Group" (ghost xs). "+ Group" is disabled at depth 8 with the visible reason "Groups nest at most 8 deep."
- Defect fixes (binding): removing the last condition of a nested group removes that group (never turns it into a condition); a rule with no conditions cannot be saved — the root shows "Add a condition to say who this group matches." and Save is disabled with that reason; new conditions start empty-valued where a choice is required (provider) and default sensibly otherwise.
- Validation: client limits (depth, nodes, children, missing provider) and server preview `invalid_group_rule {path, reason}` are mapped to the **row or well** the path names (`aria-invalid` + a visible message on that row) and summarised in the section footer ("Choose a provider for the highlighted condition."); Save ("Save rule") is disabled while the editor knows the rule is invalid, with the footer reason visible. Unknown server failures: "The server did not accept this rule." once, inline.
- Live preview while editing (existing group only, debounced 400 ms, newest wins): the footer shows "{n} accounts match the edited rule" (pluralised) next to Discard / Save rule; saving is a consequential save — the footer states "Saving changes who is in this group right away." and the action reads "Save rule".
- JSON tab: mono textarea of the serialised document; applies on blur only if it parses and is a document (existing two messages kept); a single-leaf root is wrapped in a root `all` as today.
- [ ] **Step 1: Write failing tests** — model: parse/serialize round trip, every mutation, limits, path mapping, the two defect cases; builder: human labels, is/is not, root combinator, nested add/remove (last child removes the group), depth limit reason, provider unknown/disabled notes, error marked on the leaf row from a server path, Save disabled with reason, empty rule cannot save, preview count debounced and newest-wins, JSON apply on blur with both messages; one report per failure.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: a three-level rule with any/all and a negated provider; an invalid row (missing provider) with the footer reason; unknown provider; empty rule; depth limit; JSON tab with each error; narrow layout of a nested rule — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild the group rule builder`

---

### Task 3: Rule-group side column

Research: §1.4 "Missing but available in the API" (preview, explain).

**Files:**
- Create: `dashboard/src/pages/admin/groups/GroupMatchesFrame.tsx`, `GroupExplainFrame.tsx`, tests; wire them into the side column above "Used by" for rule groups.

**Interfaces:**
- **Matches right now** (from the saved rule, `groupPreviewQueryOptions`): the count in large type with the unit; a total only if the API returns one; up to five avatars (`EntityAvatar`) of matched accounts and "See all" opening a dialog listing the matched accounts the endpoint returns (name, @username mono; the page fixed to what the API serves, stating "Showing the first {n}" when it caps); quiet read with an inline failure line.
- **Check an account**: an account search (`EntityPicker` / account search query) and, once chosen, the explain tree from `groupExplainQueryOptions` — the verdict line "In this group" (success) / "Not in this group" (destructive) and the tree rendered as nested rows with check/cross icons and the server's labels (indent per level; complete sentences from the server's `label` — map known label keys to translated sentences, show the raw label in mono otherwise); quiet read.
- Below `lg` these frames follow the main column in the order Matches, Check an account, Used by.
- [ ] **Step 1: Write failing tests** — count and avatars from the preview; See all dialog; explain verdict and nested reasons for an in and an out account; quiet failures; no invented totals.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: side column with matches (0, a few, many) and an explained in/out account; See all dialog; narrow stacking — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): show who a rule group matches and why`

---

### Task 4: Manual-group decisions

Research: §1.5.

**Files:**
- Create: `dashboard/src/pages/admin/groups/GroupDecisionsSection.tsx`, `AddDecisionDialog.tsx`, tests; mount on manual group pages (main column, after Details); side column for manual groups shows a summary frame ("{allow} allowed · {deny} denied" only if counts are known from the loaded pages — otherwise just "Used by").

**Interfaces:**
- Section "Who this group decides" with description "Accounts allowed here can use the applications that select this group; accounts denied here cannot, whatever else matches. Everyone else is neutral." and header action "Add an account".
- Decision rows (`ItemList`): avatar, name, @username mono, badge Allow (success) / Deny (destructive), meta "Updated {relative}", actions — a segmented control or `RowMenu` to switch Allow ↔ Deny and "Clear" (back to neutral; confirm only for clearing a deny, since that may let the account through: "Clear the deny for {name}? They may get in through another group or rule."). Cursor-paged with "Load more", in the order the API returns.
- Add dialog: account search (`EntityPicker`), effect choice Allow / Deny (`ChoiceTile` or segmented), submit "Allow {name}" / "Deny {name}"; an account that already has a decision shows its current one and the dialog changes it; mutation at section level; dialog ignores dismissal while pending; mobile submit full width.
- Empty state: "No decisions yet. Nobody is allowed or denied through this group." with the add action in the header only.
- Every write reports once; decisions list refreshes after each write.
- [ ] **Step 1: Write failing tests** — list rows with badges and relative time; paging; add allow and add deny; switch allow ↔ deny; clear allow (no confirm) and clear deny (confirm); empty state; one report per failure; dialog dismissal blocked while pending.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: decisions with allows and a deny; empty; add dialog (search open, effect chosen); clear-deny confirm; narrow layout — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): let administrators allow and deny accounts in manual groups`

---

### Task 5: Group list and create page

Research: §1.2, §1.3.

**Files:**
- Modify: `dashboard/src/pages/admin/AdminGroups.tsx` (move into `groups/AdminGroups.tsx`), create `groups/AdminGroupNew.tsx` (the create half of the old form), tests; update routes.

**Interfaces:**
- List on the application-list idiom: `PageHeader` "User groups" with description and "New group"; `DataTable` rows — group cell (`EntityCell`: kind icon, name, mono slug, a "Not sent to applications" mark when `exposedToDownstream` is off), Kind (word), Description (muted, truncated with title, "—" when empty), Applications (count, tabular numerals, "—" when absent), pinned open action. Column widths from the shared data-column constant. Quiet list read + `InlineFailure`; empty state "No user groups yet" with one help line (create action in the header only).
- Create page on the application-create idiom: `PageHeader` "New user group" in `PageBody`; Sections with icons — **Kind** (`ChoiceTile` cards Rule / Manual with the existing note split per option, stating the kind cannot change later), **Details** (name, slug, description, "Send to applications" switch default on with the truthful copy), **Who this group matches** (the Task 2 builder, rule kind only; no live preview for a new group, as today — say so: "Matching accounts appear once the group is saved."), and for manual kind a line "You choose who this group allows or denies on its page after you create it."; footer Create + Cancel (`admin.cancel`); `withInlineReport` create; slug conflict on the field; Save blocked while the rule is known invalid (same rule as Task 2); navigate to the new group; list invalidation with its own spy test.
- [ ] **Step 1: Update tests first**, adding: list columns and the not-sent mark; quiet list failure once; create rule group with a built rule (body asserted); create manual group; slug conflict on the field; invalid rule blocks Create with its reason; list invalidation.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: list (several groups incl. not-sent), empty list, create rule (with builder), create manual, slug error, invalid-rule blocked — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild the group list and create page`
