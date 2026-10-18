# coss redesign — Phase 3: member pages

> **For agentic workers:** REQUIRED SUB-SKILL: Use minipowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the signed-in member pages — console home, Profile, Security (passkeys, password and authenticator, linked identities, access tokens), Connected applications and Devices (sessions and pairing approval) — on coss in the approved `#security` design, with every dialog, reveal and step-up on coss.

**Architecture:** Each page opens with a `PageHeader` (title, description, optional actions) in the content column; the shell breadcrumb names the page but is no longer its heading. Each block is a `Section` (coss `Frame`: header band with icon, title, description and one action; `FramePanel` body) holding row lists built on `ItemList`/`ItemListRow`, whose secondary actions live in a per-row coss `Menu`. Multi-step flows are coss `Dialog`s with the existing `StepHeader`; one-time secrets use a coss rebuild of `SecretReveal`; step-up stays the coss `SudoDialog` stacked on top.

**Tech Stack:** React 19, TanStack Router/Query/Form, Lingui 6 (en, zh), coss ui (Base UI 1.9) in `dashboard/src/components/ui`, Tailwind 4, Vitest + Testing Library.

**References:**
- Approved mock: `.impeccable/mocks/coss-sandbox/src/scenes/security.tsx` (page title + description, Frame sections with icon titles and a header action, grid rows with badge and last-used columns and a row "More" menu, the step dialog for the authenticator with QR beside the key). Run it with `pnpm exec vite --port 5311 --host 127.0.0.1` in that directory; scene `#security`.
- Scenario research for every member page (data, limits, states, error codes, dialogs, the top-10 hard problems): `.impeccable/research/member.md`. Each task names its section.
- Finished siblings to mirror: the public pages (`pages/Login.tsx`, `pages/public/*`) for StepHeader, TotpSetup, RecoveryCodes, CopyValue and the `withInlineReport` idiom; the shell (`components/custom/ConsoleLayout.tsx`) and the shared console patterns from the foundation phase (`Section`, `AsyncSection`, `ItemList`, `ConsoleCard`, `ChoiceTile`, `DangerZone`, `ConfirmDialog`, `SudoDialog`).
- Screenshot helper: `node .impeccable/scripts/shoot.mjs <outDir> '<json>'` against `pnpm exec vite --port 5292 --host 127.0.0.1` in `dashboard/`. A plain member is `mock.session.role=member&mock.admin.oidcApps=0&mock.admin.samlApps=0&mock.admin.forwardAuthApps=0`; writes need `mock.writes=true`. Other knobs: research §8 and `src/devtools/mock/model.ts`.

## Global Constraints

- Work in `/home/tundra/projects/tundra/prohibitorum-worktrees/develop-coss` on branch `develop-coss`; dashboard commands run in `dashboard/`.
- Nobody uses the dashboard during the migration. Do not build anything whose only purpose is to let HeroUI and coss work together; an unmigrated admin page may render or behave wrongly. `pnpm exec tsc --noEmit` must pass at every commit.
- A file a task rewrites ends with zero imports from `@heroui/react`, `@heroui/styles`, `react-aria-components`, `react-aria` and `tailwind-variants`.
- coss components are the base: restyle one only when the design needs it, building on coss's own style and preserving its details (hover, active, focus-visible and disabled states, dark mode, sizing rhythm, `data-slot` hooks); otherwise compose and pass `className` for layout.
- Console material: content column `max-w-6xl` (shell) with page content at `max-w-3xl`; sections are `Frame`s; rows sit in the `FramePanel`; a well is `rounded-xl bg-muted/60 ring-1 ring-border/60`; never a card inside a card. Flat coss buttons; no gradients, no decorative marks, no keyboard-shortcut hints.
- Type: Cal Sans; Paper Mono only for values a person copies or compares (usernames, subjects, IDs, codes, hints, IPs, scopes). Page titles `text-2xl font-semibold tracking-tight`; section titles are coss `FrameTitle`.
- Passkeys are first-class: the Passkeys section comes first on Security; password and authenticator are second but never collapsed.
- A disabled control that the reader might try says why in visible text next to it (a muted line or the row's details), not only in a tooltip.
- Behaviour is preserved exactly unless a task says otherwise: every route, query, mutation, sudo reason, success toast, error-code message, last-method rule, reveal lock (no dismiss until "I have saved…", navigation guard), deep link (`/devices?code=`), polling interval and focus return described in `.impeccable/research/member.md`.
- One report per failure: a failure shown inline does not also toast (use `withInlineReport` from `src/app/query-client.ts`; a read uses `meta.quiet` or the caller's quiet option); a failure with no inline place keeps its toast.
- User-facing strings are Lingui messages with explicit ids and both `en` and `zh` entries (`pnpm run i18n:extract`, then fill `src/locales/zh/messages.po`; 你 register; the catalogue's terms: 通行密钥, 身份验证器, 恢复码, 访问令牌, 提供商). Where this plan names an existing id, confirm it in `src/locales/en/messages.po`; if it differs, change that id's message rather than adding a duplicate.
- Tests find elements by role, name, label and `data-*`; never by class names. Write tests for the new components' behaviour and keep each page's existing behavioural assertions (rewrite their queries; do not delete a behaviour's test because markup changed).
- Per task checks: `pnpm exec tsc --noEmit`; `pnpm exec biome check <touched files>`; `pnpm exec vitest run <touched and new test files>`; `pnpm run i18n:check` when strings change. No full-suite run inside a task. A task that changes what a page looks like ends with two screenshots per changed state through the helper — 1440×900 light `en` and 390×844 dark `zh` — opened and viewed by the implementer, with every visible defect fixed before the commit and every path listed in the report.
- Dependencies stay pinned to exact versions; no new dependency.
- Commits: Conventional Commits subject, imperative. No `Co-Authored-By` or other attribution trailer (the owner's standing rule). Nothing written may cite this plan, the spec, task numbers or the mock.

---

### Task 1: Page header, row menu and dialog surfaces

**Files:**
- Create: `dashboard/src/components/custom/PageHeader.tsx`, `PageHeader.test.tsx`, `RowMenu.tsx`, `RowMenu.test.tsx`
- Modify: `components/custom/ConsoleLayout.tsx` (breadcrumb's current page is no longer an `h1`), `components/custom/Section.tsx` (header band may carry a leading icon), `components/custom/ItemList.tsx` (row grid with optional badge and meta columns from `sm`), `components/custom/SecretReveal.tsx` (coss `Dialog` instead of HeroUI `Modal`), `components/custom/ImageDropTile.tsx` (native file input + drop, no react-aria), and their tests

**Interfaces:**
- Produces (binding):
  ```ts
  export function PageHeader(props: { title: React.ReactNode; description?: React.ReactNode; actions?: React.ReactNode }): React.ReactElement; // the page's only h1, text-2xl font-semibold tracking-tight; description text-muted-foreground; actions end-aligned from sm
  export function RowMenu(props: { label: string; children: React.ReactNode }): React.ReactElement; // ghost icon-sm "More" trigger (MoreHorizontal) opening a coss Menu; label is the trigger's accessible name, e.g. "More for MacBook Pro"
  // Section gains: icon?: React.ReactNode  (drawn before the FrameTitle text, size-4 muted)
  // ItemListRow gains: badge?: React.ReactNode; meta?: React.ReactNode  (from sm the row is grid-cols-[auto_minmax(0,1fr)_auto_auto_auto]: icon, title+details, badge, meta, actions; below sm badge and meta join the details)
  // SecretReveal: same props minus onSurface/inDialog; inside a coss Dialog it disables Escape, outside-press and the close button until saved; the navigation and beforeunload guard stay
  // ImageDropTile: same props; a visually hidden <input type="file"> behind a button-styled label, plus drag-and-drop on the tile
  ```
- Existing callers of `SecretReveal` and `ImageDropTile` (member, admin, public) keep compiling; admin callers may change behaviour (accepted).
- [ ] **Step 1: Write failing tests:** PageHeader renders the only `h1` with the title and the description; ConsoleLayout's breadcrumb current item is not a heading. RowMenu opens on click and Enter, items run their handlers, the trigger is named by `label`. Section with `icon` shows it in the header; ItemListRow places `badge`/`meta` (assert by `data-slot`). SecretReveal inside a dialog: Escape and the close button do nothing until the checkbox is ticked; Done is disabled until then; navigation away asks to confirm. ImageDropTile: choosing a file through the labelled input calls `onFiles`; a dropped file calls it too; disabled blocks both. Un-skip and rewrite the skipped ImageDropTile test.
- [ ] **Step 2: Run them; failures are the missing modules and new structure.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run the touched tests, tsc, Biome. Screenshots: the Security page with the new header and a section with rows (before its content is restyled) at the two standard shots.**
- [ ] **Step 5: Commit** — `feat(dashboard): add page headers, row menus and coss reveal and drop surfaces`

---

### Task 2: Profile

Research: §3.

**Files:**
- Modify: `dashboard/src/pages/Profile.tsx`, `pages/profile/AvatarPanel.tsx`, `AvatarPanel.test.tsx`, `pages/profile/AccountPanel.tsx`, and a new `AccountPanel.test.tsx`

**Interfaces:**
- `PageHeader` "Profile" with a one-line description.
- Avatar section (`Section` with `UserRound` icon, "Picture"): the source picker as a `RadioGroup` of `ChoiceTile`s (media layout, 56px preview, provider label or "Uploaded picture" or "No picture"), then the `ImageDropTile` ("Upload"/"Replace"); the format hint and "Remove uploaded picture" (`destructive-outline` sm) in the section footer band. Saving state, the "one write at a time" rule, the inline type/size warning and the server errors are unchanged.
- Account section: an editable "Display name" field (with the existing validation) whose Save/Discard live in the section footer and appear only when the value is dirty (`Section` `dirty` + footer, the foundation's unsaved-changes idiom); the read-only identifiers (username, OIDC subject with its hint) render as `SettingRow`s with a `CopyValue` each, visually separate from the editable field.
- [ ] **Step 1: Update tests first:** AvatarPanel's existing behaviour assertions with new queries; AccountPanel: Save is hidden until the name changes, Discard restores it, saving shows the success toast and updates the shell name, invalid names show the field error, the identifiers copy.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: profile default, no picture sources, three upstream sources with one active, name validation error, remove-upload confirm — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild the profile page on coss`

---

### Task 3: Security — passkeys

Research: §4 intro, §4.1. Mock: `#security` (Passkeys frame).

**Files:**
- Modify: `dashboard/src/pages/Security.tsx`, `pages/security/PasskeysPanel.tsx`, and a new `PasskeysPanel.test.tsx` (move any passkey assertions from existing tests into it)

**Interfaces:**
- `PageHeader` "Security", description "How you sign in to {instance}." (new id). Sections in order: Passkeys, Password and authenticator, Linked identities, Access tokens.
- Passkeys `Section`: `Fingerprint` icon, title, description "The way in. Your device keeps the key, so there's nothing to type or leak." (new id), header action "Add a passkey" (`default` sm with `Plus`).
- Row: device-kind icon (`KeyRound` for security keys/unknown, `Laptop` otherwise is not knowable — use `KeyRound` for all unless `transports` says `internal`/`hybrid`, then `Laptop`/`Smartphone`), title = nickname or muted "Unnamed passkey", details "Added {relative} · …{suffix}" (suffix mono), `badge` = "Synced" (`success` Badge) or "This device only" (`outline`), `meta` = "Used {relative}" / "Not used yet", actions = `RowMenu` with Rename and Remove (Remove `variant="destructive"` item).
- The last-passkey rule: when only one passkey exists, the Remove item is disabled and the row's details add "Your only passkey" (new id) so the reason is visible; the existing server error mapping stays.
- Rename dialog (coss `Dialog`): Name field with its hint, Cancel / Save name; add failure inline in the section via `withInlineReport` (no double toast).
- [ ] **Step 1: Write failing tests:** rows show nickname/badge/meta; the menu offers Rename and Remove; with one passkey Remove is disabled and "Your only passkey" is visible; add runs the ceremony (mock the WebAuthn helper as existing tests do) and a failure shows once (inline, no toast); rename saves; remove confirms then requires sudo.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: 2 passkeys, 6 passkeys (`mock.factors.passkeys=6`), 1 passkey, 0 passkeys, rename dialog, remove confirm with sudo stacked — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild passkeys on the security page`

---

### Task 4: Security — password and authenticator

Research: §4.2 (the state matrix, set-up dialog, change, replace, regenerate, turn off). Mock: `#security` (the step dialog).

**Files:**
- Modify: `dashboard/src/pages/security/PasswordTotpPanel.tsx` (split into a panel file plus one file per dialog if it helps keep each under ~300 lines), `PasswordTotpPanel.test.tsx`

**Interfaces:**
- `Section`: `Lock` icon, title "Password and authenticator", description by state: neither set "For a device without your passkeys. Not set up yet."; both set "For a device without your passkeys."; one half set "Only part of this is set up. Set it up again to use it." (new ids). Header action: "Set up" (`outline` sm) when not both set; none otherwise.
- Rows when both set: Password ("Set", Change), Authenticator ("Set", Replace), Recovery codes (meta "{n} codes left" with the existing plural; badge "Running low" `warning` at ≤3, "All used" `destructive` at 0; Regenerate). Turn off moves to a separate danger row at the bottom of the panel (`destructive-outline` sm), disabled with the visible reason "This is your only way to sign in. Add a passkey first." when there are no passkeys. Half-set and neither-set rows follow the research matrix with dimmed rows.
- Set-up dialog (coss `Dialog`, `sm:max-w-xl`): `StepHeader` (Password, Authenticator, Recovery codes); step 1 new + repeat password (and the replacing warning); step 2 QR beside the setup key (`CopyValue` with `wrap`) from `sm`, stacked below, then the code `OtpField` (lg), footer "The key was made in this browser and is sent only when you confirm." (new id) beside Back / Turn on; step 3 the recovery codes `SecretReveal` inside the dialog. Sudo stacks on top. Error routing (code error on the field, password error back to step 1) unchanged.
- Change password and Replace authenticator dialogs on coss `Dialog`; rename the replace dialog's OTP label to "Code from the new authenticator" (existing id, new message). Regenerate and Turn off keep their `ConfirmDialog`s; the new codes open in a `SecretReveal` dialog.
- Factor read failure uses `AsyncSection` with Try again (no bare alert).
- [ ] **Step 1: Update tests first** for the new structure; add: the turn-off reason is visible text when there are no passkeys; the set-up dialog's step header shows the current step; the key wraps (CopyValue wrap) — assert by role/text; a read failure offers Try again.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: neither set, half set, both set with 8 / 2 / 0 codes, set-up step 1 (with errors), step 2, step 3 reveal, change password, replace authenticator, regenerate confirm, turn-off disabled with reason — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild password and authenticator on the security page`

---

### Task 5: Security — linked identities and access tokens

Research: §4.3, §4.4.

**Files:**
- Modify: `dashboard/src/pages/security/IdentitiesPanel.tsx`, `pages/security/TokensPanel.tsx`, `TokensPanel.create.test.tsx`, `TokensPanel.test.ts`, and a new `IdentitiesPanel.test.tsx`

**Interfaces:**
- Identities `Section`: `Link2` icon, title "Linked identities", description (new id), header action "Link another identity" (`outline` sm) opening a coss `Menu`: one item per provider with its `EntityAvatar` (sm) and name; already-linked providers disabled with "Linked" as the item's trailing text; with none, one disabled item "No identities available to link". Rows: provider `EntityAvatar` (md, the provider icon when known), provider name, badge protocol (`outline`, "OIDC"/"SAML"), details "email · subject" (subject mono, `wrap-anywhere`), meta "Linked {relative}", actions: Unlink (`destructive-outline` icon-sm with accessible name). The link redirect, sudo, unlink confirm and `last_sign_in_method` mapping unchanged; unlink failure inline once.
- Tokens `Section`: `KeyRound` icon, title "Access tokens", description (new id) mentioning the `X-Prohibitorum-PAT` header only in the reveal (not here), header action "Create a token" (`outline` sm). Rows: name, details "hint (mono) · access summary", badge "Skips confirming it is you" (`warning`) for sudo-level, meta "Expires {date}" / "Never expires" (past expiry: "Expired" `destructive` badge), actions Revoke (`destructive-outline` icon-sm).
- Create dialog (coss `Dialog`, `sm:max-w-xl`): Name, Expires after (coss `Select`), "What it may access" as a `RadioGroup` of `ChoiceTile`s (list layout, title + description, the radio visibly round); "Applications you choose" reveals a well with a checkbox list (scrolls inside the dialog body past ~8 items); the sudo-level option turns the submit `destructive` and adds the existing extra confirm. Reveal: `SecretReveal` dialog with the header hint line. Behaviour, validation and sudo stacking unchanged.
- [ ] **Step 1: Update tests first;** add: link menu disables linked providers with "Linked"; unlink failure reports once; the create dialog's access levels are radios (role radio) with descriptions; picking selected apps shows the checklist; the sudo level requires the extra confirm.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: identities 0 / 4, link menu open, unlink confirm; tokens 0 / 8, create dialog (each access level, selected-apps checklist, no apps), sudo-level confirm, reveal — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild linked identities and access tokens`

---

### Task 6: Connected applications

Research: §5.

**Files:**
- Modify: `dashboard/src/pages/ConnectedApps.tsx`, and a new `ConnectedApps.test.tsx` (move assertions from `console-pages.test.tsx` if they live there)

**Interfaces:**
- `PageHeader` "Connected applications", description "Applications you allowed to sign you in. Removing one makes it ask again next time." (new id).
- One `Section` (no extra title band if the page header already names it; use an untitled `ItemList`) with rows: app `EntityAvatar` (md, logo or initials), name, badge "OpenID Connect"/"SAML" (`outline`), details: the granted scopes humanized with the consent page's scope titles (reuse the consent scope mapping — extract it to a shared module if it lives inside `pages/public/Consent.tsx`) joined with the locale list format, "Basic profile" when none; meta "Allowed {relative}"; action Remove access (`destructive-outline` icon-sm). Load failure via `AsyncSection` with Try again (one report).
- [ ] **Step 1: Update tests first;** add: scopes are humanized (e.g. "Your profile" for `profile`); a load failure shows Try again and no toast.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: 4 apps, 0 apps, remove confirm — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild connected applications`

---

### Task 7: Devices and pairing approval

Research: §6.

**Files:**
- Modify: `dashboard/src/pages/Devices.tsx`, `pages/devices/DeviceSessionList.tsx`, `pages/devices/PairDeviceDialog.tsx`, `Devices.test.tsx`

**Interfaces:**
- `PageHeader` "Devices", description (new id), actions: "Sign in a new device" (`default` sm, `QrCode` icon).
- Sessions list (untitled `ItemList`): device icon from the user agent, title "Browser · OS" (full UA in a tooltip and in the details when unparsed), badge "This device" (`info`) on the current session, details IP (mono) · "Signed in {relative}", action End session (`destructive-outline` icon-sm); on the current row the action is replaced by the muted text "Use sign out to end this one" (no disabled-with-tooltip).
- While the list polls for a newly approved device, a quiet status line above the list: `Spinner` + "Waiting for the new device to finish signing in…" (new id, `aria-live="polite"`), gone when the new session appears or the pairing expires.
- Pair dialog (coss `Dialog`): step 1 the 8-character `OtpField` (lg, 4-4) with its errors and the deep-link prefill; step 2 review as a `dl` in a well (Device, IP address with "Same as this device", Requested, Expires) plus the consequence line, Decline (`outline`) / Approve (`default`) with sudo stacked; expired state flips live with a warning `Alert` and Approve disabled with the visible reason; already-approved state with Close / Revoke approval (`destructive-outline`).
- [ ] **Step 1: Update tests first;** add: the waiting line appears after approve and disappears when a new session arrives; the current row shows its explanatory text instead of an action.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: 4 sessions (with an unparsed UA), pair code step (empty, incomplete error, prefilled), review (other network, same network), expired, already approved, sudo stacked, waiting line — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild devices and pairing approval`

---

### Task 8: Console home

Research: §2.

**Files:**
- Modify: `dashboard/src/pages/Console.tsx`, and a new `Console.test.tsx` (move assertions from `console-pages.test.tsx` if they live there)

**Interfaces:**
- `PageHeader` "Welcome, {displayName}" with description "{instance} account" (new ids) — the stale "administrative pages are still to come" alert is removed.
- An account summary built only from data the API already returns: a `Section` "Sign-in" with three `SettingRow`s linking to Security — Passkeys ("{n} passkeys" or "No passkeys yet"), Password and authenticator ("On" / "Not set up"), Recovery codes ("{n} left", warning text at ≤3, destructive at 0, row hidden when the authenticator is off); a `Section` "Devices" with one `SettingRow` ("{n} signed-in devices") linking to Devices. Data from `GET /me/factors` and `GET /me/sessions` (existing query options), each section an `AsyncSection`.
- [ ] **Step 1: Write failing tests:** the welcome title uses the display name; counts render from the mocked reads; recovery codes ≤3 shows the warning wording; the rows link to /security and /devices.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: default, no passkeys + codes low, admin role — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): turn the console home into an account summary`

---

## Phase end (controller)

`mise run ci:frontend` once; a visual QA pass (1440×900, 1920×1080, 390×844 × light, dark × `en`, `zh`) across every member route and dialog above, viewed at 2×; then the whole-phase review, one fix wave, and the branch question.
