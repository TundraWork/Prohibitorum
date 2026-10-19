# coss redesign — Phase 8: settings

> **For agentic workers:** REQUIRED SUB-SKILL: Use minipowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the instance settings — general (name, icon), maintenance mode, client IP, signing keys, and the sign-in page editor with its live preview — on coss in the approved `#appearance` design, and add the client-side readability check the design calls for.

**Architecture:** `/admin/settings` becomes an object-style page of independently saved sections with the "On this page" rail (General, Sign-in page summary, Maintenance mode, Client IP, Signing keys). The sign-in page editor moves to its own route, `/admin/settings/sign-in-page`, because the approved design gives it a two-column workspace (controls on the left, a sticky live preview on the right) that does not fit inside a section; the settings page shows a summary row linking to it.

**Tech Stack:** React 19, TanStack Router/Query/Form, Lingui 6 (en, zh), coss ui (Base UI 1.9), Tailwind 4, Vitest + Testing Library.

**References:**
- Approved mock: `.impeccable/mocks/coss-sandbox/src/scenes/appearance.tsx` (scene `#appearance`: the sign-in page editor — controls column with Background (source as a segmented choice, image tiles with an upload tile, change cadence, credit), Card (position, theme, opacity and blur sliders with their values), a footer "Readable · Text contrast 9.4 : 1 on this image"; a sticky Preview frame with light/dark and desktop/phone toggles and a footer "2 unsaved changes · Discard · Save"). Run it with `pnpm exec vite --port 5311 --host 127.0.0.1` in `.impeccable/mocks/coss-sandbox`. Invented data (the photo credit, the exact ratio) is not to be copied.
- Scenario research: `.impeccable/research/admin.md` §6 (every field, limit, message, instant-vs-batched write, the Unsplash key rules, the images limits, the hidden-field save blockers, maintenance confirm, client-IP rules, signing-key lifecycle and sudo points), §7–§9.
- Spec: the "Backend additions" item 3 — the readability check is client-side (sample the image under the card) and needs no backend.
- Finished siblings and shared components to mirror: the application, identity-provider, group and account pages (`AppDetailLayout`, `ObjectHeader`, `Section`, `ConsequenceFooter`, `SettingRow`, `ItemList`/`RowMenu`, `DangerZoneFrame`, `ConfirmDialog`, `SudoDialog`/`runWithSudo`, `SecretRevealDialog`, `ChoiceTile`, `SegmentedControl`, `ImageDropTile`/`ImageUploadControl`, `EntityIconControl`, `CopyableValue`, `DataTable`, `InlineFailure`/`useInlineFailure`/`SectionFailure`, `withInlineReport`, `quietQuery`, `RelativeTime`, `JsonBlock`, `useUnsavedReserve`/`UnsavedBar`), and the public sign-in stage (`components/custom/PublicStage.tsx`, `components/custom/login-appearance/*`) which the preview must render faithfully. `dashboard/AGENTS.md` describes the idioms.
- Screenshot helper: `node .impeccable/scripts/shoot.mjs <outDir> '<json>'` against `pnpm exec vite --port 5292 --host 127.0.0.1` in `dashboard/`; viewport shots (`"full": false`) scrolled with `steps`; hide the devtools button as in earlier phases. Admin `mock.session.role=admin`; writes `mock.writes=true`; sudo prompt `mock.sudo.fresh=false`; appearance, maintenance, network and key states via the knobs in `dashboard/src/devtools/mock/model.ts` (`instance.loginAppearance.*` and others — validate every path; add typed knobs with tests where a state cannot be reached).

## Global Constraints

These come from the owner's decisions and from defects earlier phases found. Anything not listed here is a default the implementer may depart from with a stated reason.

- Work in `/home/tundra/projects/tundra/prohibitorum-worktrees/develop-coss` on branch `develop-coss`; dashboard commands run in `dashboard/`.
- Nobody uses the dashboard during the migration: no HeroUI↔coss coexistence layers; `pnpm exec tsc --noEmit` passes at every commit. A file a task rewrites ends with zero imports from `@heroui/*`, `react-aria*` and `tailwind-variants` (tests included).
- coss components are the base; restyle one when the design needs it, preserving its details (hover, active, focus-visible, disabled, dark mode, rhythm, `data-slot` hooks).
- Console material: sections are `Frame`s; a well is `rounded-xl bg-muted/60 ring-1 ring-border/60`; never a card in a card; no gradients or decoration in the console chrome (the sign-in preview shows the instance's own chosen background, which may be a gradient — that is content, not chrome). These pages read as siblings of the other console pages.
- Consequential saves state the consequence in the section footer and name the action on the primary button, then confirm (one grammar, `ConsequenceFooter`): turning maintenance mode on; changing the client-IP source or trusted proxies.
- A disabled control the reader might try says why in visible text next to it.
- Behaviour is preserved unless a task says otherwise: every field, limit, validation message, the instant writes (icon, uploaded images) vs batched saves (everything else), the Unsplash key checks, hidden-field save blockers, sudo on generate/activate/retire keys, the maintenance confirm, and every read and mutation.
- One report per failure: inline-reported failures use `withInlineReport`/`useInlineFailure`; reads under a section are quiet and show their failure in place.
- Show only data the API returns; never present a guessed value as fact (the readability check states what it measured and how).
- Strings: Lingui explicit ids, `en` and `zh` (你 register, catalogue terms, complete sentences, no invisible characters); update the en msgstr when an existing id's English changes.
- Tests by role, name, label and `data-*`, not class names; tests for new behaviour; keep existing behavioural assertions; prove one report per failure with a spy `notifyError`.
- New dependencies are fine when they suit the design (exact versions; say why in the report).
- Per task: `pnpm exec tsc --noEmit`; Biome on touched files; vitest on touched and new tests plus `src/locales`; `pnpm run i18n:check`. Visual tasks end with screenshots of every changed state at 1440×900 light `en` and 390×844 dark `zh` (dialogs viewport-only), taken after the last code change and each one viewed.
- Commits: Conventional Commits; no `Co-Authored-By` or other attribution trailer; nothing written cites this plan, the spec, task numbers or the mock.

---

### Task 1: Settings page frame, general and maintenance

Research: §6.1, §6.2, §6.4.

**Files:**
- Move the settings page into `dashboard/src/pages/admin/settings/` as `AdminSettings.tsx` on the object-page layout with the rail; rebuild `GeneralPanel.tsx` (name, icon) and `MaintenancePanel.tsx`; leave the network, keys and sign-in panels mounted unchanged for later tasks; tests; update the route.

**Interfaces:**
- `PageHeader`-style title "Settings" with a short description; sections in order: **General** (instance name with its limit and hint; icon via `EntityIconControl`/`ImageUploadControl`, instant write as today), **Sign-in page** (in this task, keep the existing editor mounted in place; Task 3 replaces it with a summary row), **Maintenance mode**, **Client IP**, **Signing keys**; the rail lists them.
- **Maintenance mode**: switch and notice (≤500 characters, hint kept). Turning it on is a consequential save: footer "Nobody except administrators can sign in or use applications until it is turned off.", action "Turn on maintenance and save", then the existing confirm. While maintenance is on, the console shows a persistent, quiet banner at the top of every console page ("Maintenance mode is on — only administrators can sign in.") with a link to this section (research: no persistent banner exists today).
- [ ] **Step 1: Write failing tests** — name save and limit message; icon instant write; maintenance consequence + action + confirm; the console banner while on; one report per failure.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: settings top (name, icon), maintenance off and pending-on, the console banner on another page — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild the settings page frame, general and maintenance`

---

### Task 2: Client IP and signing keys

Research: §6.5, §6.6.

**Files:**
- Rebuild `dashboard/src/pages/admin/settings/NetworkPanel.tsx` (titled "Client IP") and `SigningKeysPanel.tsx`, tests.

**Interfaces:**
- **Client IP**: "Read the address from" as described `ChoiceTile`s (The connection / X-Forwarded-For / Another header), header name (mono, rules kept), trusted proxies (one CIDR per line, ≤64, both messages kept), the hidden-invalid-list save blocker kept with a visible reason. Changing the source or the proxies is a consequential save: footer "Audit logs and rate limits use the new address for every request from now on. A wrong setting can lock everyone into one shared limit.", action "Change the client address and save", then a short confirm.
- **Signing keys**: heading action "Generate key" (sudo). The keys read as a lifecycle, not a table of chips: the signing key first (Key ID mono via `CopyableValue`, algorithm, "Signing since {relative}"), then pending keys with the next step stated ("Published, not yet signing" + Activate (sudo, existing confirm copy) and Retire (sudo, existing confirm copy)), then retiring keys ("Still verifying until {date}"), then retired keys collapsed behind "Show {n} retired keys". A short description explains the lifecycle in one or two sentences (generate → publish → activate → retire after the grace period). View public key (JWK dialog with Copy) on every key.
- [ ] **Step 1: Write failing tests** — client IP fields, messages, blocker, consequence + confirm; keys grouped by state with the right actions only on pending keys; sudo on generate/activate/retire; JWK dialog copy; retired collapse; one report per failure.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: client IP each source, invalid list, change pending; keys in a normal state (1 signing, 1 pending, 1 retiring, several retired) and with retired expanded; activate confirm + sudo; JWK dialog — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild client IP and signing keys`

---

### Task 3: Sign-in page editor

Research: §6.3.

**Files:**
- Create the route `/admin/settings/sign-in-page` and `dashboard/src/pages/admin/settings/sign-in-page/` (`SignInPageEditor.tsx`, `BackgroundControls.tsx`, `SurfaceControls.tsx`, `SignInPreview.tsx`, plus the images control rebuilt), tests; delete the old `SignInPagePanel.tsx`, `SignInPageControls.tsx`, `LoginImagesControl.tsx`, `LoginPreview.tsx` once replaced; on the settings page replace the editor with a **Sign-in page** summary row (current background source and card position in words, a small static thumbnail of the preview if cheap, and "Edit the sign-in page" linking to the route).

**Interfaces:**
- Layout as the approved mock: page title "Sign-in page" with its description and a back link to Settings; a ~380px controls column (Frames: **Background**, **Card**, **Toolbar**) and a sticky **Preview** frame with light/dark and desktop/phone toggles and the unsaved-changes footer ("{n} unsaved changes · Discard · Save"); below `lg` the preview stacks above the controls.
- **Background**: source as a segmented choice (Default / Color / Gradient / Bing / Unsplash / Images) with each source's controls and rules exactly as today (color hue + hex + swatches; gradient presets; Bing region + caption toggle; Unsplash key saved/replace/remove with the remove-while-in-use reason, keyword, order, interval; Images drop zone with thumbnails, limits and counts, instant upload/remove with the "uploads apply right away" note, order and interval with ≥2 images). The hidden-invalid-field blockers keep their messages.
- **Card**: position (Left / Center / Right), theme (Visitor's choice / Always light / Always dark), translucent switch → opacity slider (value shown) and frosted glass switch. **Toolbar**: the same three. Defaults kept.
- **Preview** renders the real public stage components at the chosen settings (as the current `LoginPreview` does — keep its scaling approach), in the chosen preview theme and size.
- One Save for all batched settings (consequence-free), Discard, and the unsaved bar/guard idiom.
- [ ] **Step 1: Write failing tests** — every source's controls and rules, the blockers, instant image writes vs batched save, surfaces, preview reflects edits, save body, discard, one report per failure.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: each background source; Unsplash key states; images (0, 2, 10 of 10); card and toolbar surfaces; preview light/dark and desktop/phone; unsaved footer; the settings-page summary row — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): give the sign-in page its own editor with a live preview`

---

### Task 4: Readability check

Spec: Backend additions item 3.

**Files:**
- Create `dashboard/src/pages/admin/settings/sign-in-page/readability.ts` (+ test) and wire a readability line into the Card frame footer of the editor.

**Interfaces:**
- Measure the contrast of the card's text against what is actually behind the card in the preview: sample the rendered background under the card's rectangle (for images: draw the image region into a canvas — same-origin uploads and proxied wallpapers; for colors/gradients: compute from the colors), combine with the card's surface (opacity, frosted glass approximated as a blur of the sampled region) and compute the WCAG contrast ratio of the card's foreground against the worst-case sampled area.
- Show "Readable · Text contrast {ratio} : 1" (success) at ≥ 4.5, "Hard to read · Text contrast {ratio} : 1 — raise the card's opacity or pick another background" (warning) below; when the background cannot be sampled (cross-origin image without CORS, not yet loaded) say "Readability can't be measured for this background." — never guess.
- Recompute when the background, surface or preview theme changes; cheap enough not to block typing (debounce, offscreen canvas, downsampled region).
- [ ] **Step 1: Write failing tests** — the contrast math (known pairs), the surface composition, the three outcomes, recomputation triggers.
- [ ] **Step 2: Run them.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run tests, tsc, Biome, i18n:check. Screenshots: readable, hard to read, cannot measure — both standard shots each.**
- [ ] **Step 5: Commit** — `feat(dashboard): tell administrators whether the sign-in card is readable`
