# Frontend audit backlog — Cool Slate dashboard

**Date:** 2026-06-19
**Source:** `/impeccable audit` (4 parallel read-only audit agents + synthesis)
**Health score at audit time:** **12/20 — Acceptable (significant work needed)**
**Status:** **Steps 1–3 (the P1 root causes) DONE — 2026-06-19, commits `5bcc4ee`→`47b79eb`** (colorize source, typeset source, harden source, dist rebuild). Steps 4–6 (responsive/touch-targets, spacing, polish) + a full independent re-audit + the human visual pass in both themes REMAIN. Per-fix gate green throughout: vitest 475/475, vue-tsc 0, `check-contrast` 31/31 (was 23/23 — folded in the missing status-text-on-card/canvas + dark-alias-icon pairs), `npm run build` clean, `go build -tags nodynamic ./...` + `go vet` 0.

**What Steps 1–3 delivered (verified):**
- **Step 1 (colorize):** migrated 23 status-text sites `text-sage`/`text-amber` → AA `-700` tokens; deleted the now-unused sage/amber/rose aliases (footgun removed); added `.dark` overrides for the 3 in-use aliases (tide/tide-strong/ember); hardened `check-contrast.mjs` with the previously-missing pairs (`sage-700`/`amber-700` on canvas+card, both themes; dark alias icons). The 3.44:1 / 2.5:1 fails are now 6.79:1 / 5.88:1.
- **Step 2 (typeset):** new `SectionTitle` component (16px/600 title tier); promoted the 11 danger-zone `<h4 text-sm>` sub-headings, the SAML create-form section spans, and `RecoveryCodesDisplay`'s off-scale `<h3 text-sm>` to it; fixed `text-foreground`→`text-ink` drift in `EditProfileDialog`.
- **Step 3 (harden):** `TableRow` focus-visible outline ring (6 list pages); demoted the redundant header `<h1>`→`<p>` + focus-to-content on navigation; new `StatusMessage` persistent `role=status` live region (19 confirmations migrated); replaced the invalid `role=button` on `<tr>` in AdminAudit with a real `<button>`; `<Label for>` on the display-name input + `aria-label` on the ScopeSelector custom input + fixed the self-referencing `aria-labelledby`; sidebar `<nav aria-label>` landmark (`nav.primaryLabel`, en+zh).

**Re-audit + Steps 4–6 (all 5 recommendations) DONE — 2026-06-19/20, commits `90c873c`→`89cb803`.** A fresh independent re-audit (4 parallel auditors + the augmented component-consistency/affordance lens) scored **15/20** (up from 12) and surfaced new consistency findings the dimension-only scan had missed (bespoke audit pills, AppAccessCard label drift, focus-ring idiom split). High-severity/contested findings were **runtime-verified in chromium**, which caught **3 agent false positives** (documented below). All 5 recommendation groups then executed + per-fix gate:
- **adapt (rec 1):** AttributeMapEditor stacks card-per-row below `sm` (verified 1-col@360/5-col@800); Checkbox/Radio/Switch restructured to a 24px clickable Root with the visible control at 16/20px (verified hit-box 24×24 / visual 16×16, corner-margin click registers); ScopeSelector + AdminAudit ×-buttons → real `size-6` boxes; AdminAudit preset-pill active `bg-ink`→`bg-primary` (Tide selection language).
- **harden (rec 2):** `CardTitle`→`<h2>` (fixes the systemic h1→h3 skip; 13 SectionTitles cascade h4→h3; FormSection gains `as`); Select-trigger/`<input>` label associations via `aria-label` (`<label for>` doesn't bind a Reka button trigger).
- **layout+optimize (rec 3):** dropped the dual icon lib (`@lucide/vue` → `lucide-vue-next`, removed from package.json); `hover:bg-muted`→`bg-accent`; `::selection` tokenized (color-mix on `--color-tide`, theme-tracking); LocaleSwitcher glassmorphism (`backdrop-blur`) removed.
- **typeset+distill (rec 4):** AppAccessCard username→`font-mono` (matches the slug; same-component drift); PasskeysCard ad-hoc empty → `EmptyState`.
- **polish (rec 5):** AdminAudit focus-ring idioms unified to `ring-[3px]`; ScopeSelector Add button → `<Button variant=outline>`; LoginView link underlined at rest; UserAvatar `loading="lazy"`.

**3 agent false positives caught by runtime verification (the audit's own quality check):**
1. "Keyboard-focusable rows have no focus ring" — FALSE; `cn()` merges TableRow's outline ring and Playwright confirmed `outline: solid 2px` renders on a focused `<tr>` (with a screenshot).
2. "~30 Alert sites have conflicting role=alert+aria-live" — overstated; `Alert.vue` bakes in `role=alert`, the call-site `role` is harmless redundancy and `aria-live=polite` is an intentional override (P3 cleanup, not P1).
3. "Invitations table overflows the page" — FALSE; `Table.vue` already wraps every table in `overflow-x-auto`.
4. (Process note) the `::before` hit-area technique was attempted then DISCARDED — `elementFromPoint` proved an overflowing empty pseudo does NOT extend the hit region; the real fix is the 24px Root restructure.

**Deliberately left (judgment, documented in commits):** sidebar `width`-collapse animation (vendored, user-initiated, marginal); broad list-vs-detail `font-mono` split (intentional convention); destructive-action variant treatment (quiet-trigger + red-confirm is coherent); ErrorView centered message (page content, calm per brand — a red Alert would be louder); contextual secret-reveal/pair-success notes (not transient flags); redundant call-site `role=alert` (harmless, 30-file churn).

**RESUME POINT:** `git push` the unpushed range (~49 ahead) + the still-pending **human visual pass in both themes (en + zh)**. Optionally re-run a full `/impeccable audit` to confirm the climb past 15 (evidence-based estimate ~17/20: Responsive 2→3, Accessibility/Theming/Perf each +~0.5, Anti-Patterns held at 4). The original execution-order notes follow (now all addressed).

| Dimension | Score | Headline |
|---|---|---|
| Accessibility | 2/4 | `text-sage` status text fails AA (3.44:1); the contrast gate missed it |
| Performance | 3/4 | Lean; only minor gaps |
| Theming | 2/4 | 6 alias tokens have no `.dark` override → ~28 sites low-contrast in dark |
| Responsive | 2/4 | Touch targets sub-spec; `AttributeMapEditor` grid unusable on mobile |
| Anti-Patterns | 3/4 | No AI tells, but heading==label collision recurs ~10× |

**Anti-Patterns verdict:** PASS the "does this look AI-generated" test (distinctive Tide/Ember identity, no slop tells). The debt is **craft + consistency**, not slop.

> **Why this matters / how it happened:** the type-hierarchy collision the operator caught (FormSection title == field label) was ONE instance of a systemic gap. The debt is almost all systematic — a handful of root causes — so a few targeted fixes move the score a lot. The `text-sage` contrast fail is a "tested the badge pair, not the plain status-text usage" blind spot in `check-contrast.mjs` that must be folded back into the gate.

---

## Root causes (the leverage points — fix these and most findings collapse)

1. **Brand/state ALIAS tokens are the weak link.** `--color-tide / -strong / -sage / -amber / -rose / -ember` (the un-numbered aliases, `main.css:24–52`) are (a) NOT AA as text in light mode (`--color-sage` 0.62 = 3.44:1 on white; `--color-amber` 0.76 = 2.5:1), (b) NOT re-declared in the `.dark` block (`main.css:157–216`) so they stay light-toned on dark surfaces, and (c) bypass the DESIGN.md `-500/-700` rule. **Fix once:** migrate every status-text usage to `-700` (and/or darken aliases + add the 6 `.dark` overrides). Resolves AA fails in BOTH themes.
2. **The type scale is under-enforced.** DESIGN.md defines 5 tiers; components instantiate ~3, so any heading outside `FormSection`/`CardTitle` falls back to the 14px/500 label tier. **Fix once:** a shared `SectionTitle`/sub-section component at the title tier (`text-base font-semibold`) + migrate the stragglers.
3. **No touch-target floor.** No control inflates its hit area; small icon = small target everywhere. Establish ≥24px (AA 2.5.8) minimum, ideally 44px (2.5.5).
4. **`v-if` on status messages** defeats `role="status"` (~15 sites). **Fix once** in `useTransientFlag` (or a shared `<StatusAnnouncement>`).

---

## Work items, grouped by execution step

### Step 1 — `/impeccable colorize` (P1: color tokens / contrast)
- **Status-text contrast (light).** `text-sage`/`text-amber` used as plain text fail AA. Migrate to `-700` tones (DESIGN.md `-500/-700` rule). Sites incl. `SecurityView.vue:77`, `DevicesView.vue:74`, `AdminGroupsView.vue:103`, `AdminOidcClientDetailView.vue:179`, `AdminUpstreamIdpDetailView.vue:172`, `AdminAccountDetailView.vue:424` (`text-amber` → `text-amber-700`), and the rest of the `text-sage` "Saved/Created/Approved/Revoked" spans (~20 total). WCAG 1.4.3.
- **Dark-mode aliases.** Add `.dark` overrides for the 6 aliases (`--color-tide`, `--color-tide-strong`, `--color-sage`, `--color-amber`, `--color-rose`, `--color-ember`) at lightened dark tones (mirror the existing `-700` dark values). Worst current: `text-tide-strong` in `SudoModal.vue:123` (~2.2:1 on dark). ~28 sites total. WCAG 1.4.3.
- **Gate fix (do this here):** add the missing pairs to `dashboard/scripts/check-contrast.mjs` — `text-sage`/`text-amber` on `card`/`canvas` (light) AND the dark-alias-on-dark pairs — so this class can never regress. This closes the blind spot that let the original issue ship.

### Step 2 — `/impeccable typeset` (P1: type hierarchy)
The heading==label collision (same bug the operator caught), remaining instances:
- Danger-zone sub-labels `<h4 class="text-sm font-medium">` (label tier): `AdminAccountDetailView.vue:418,432`; `AdminOidcClientDetailView.vue:192,205,219`; `AdminSamlProviderDetailView.vue:256,269`; `AdminUpstreamIdpDetailView.vue:183,196,208`; `AdminGroupDetailView.vue:255`.
- `DashboardLayout.vue:55` header page-title `<h1 class="text-sm font-medium">` — a 14px `<h1>` (also the double-h1 a11y bug, see Step 3).
- Bare `<span>/<p>` acting as section headers: `AdminOidcClientDetailView.vue:170`; `AdminSamlProvidersView.vue:204,234`; `EditProfileDialog.vue:185,273` (also uses `text-foreground` instead of `text-ink` — token drift).
- `RecoveryCodesDisplay.vue:53` `<h3 class="text-sm font-semibold">` (in-between tier not in the scale).
- **Leverage:** introduce a shared `SectionTitle` component (`text-base font-semibold text-ink`) so all of these + future ones are enforced. Already-fixed reference: `FormSection.vue` (16/600) + `CardTitle.vue` (18/600).

### Step 3 — `/impeccable harden` (P1: a11y correctness)
- **Focus ring on table rows.** `ui/table/TableRow.vue:7` — add `focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:rounded-sm`. Fixes all 6 admin list pages at once (`AdminAccountsView:65`, `AdminOidcClientsView:181`, `AdminSamlProvidersView:260`, `AdminUpstreamIdpsView:177`, `AdminAuditView:266`, `AdminGroupsView:145`). WCAG 2.4.7 / 2.4.11; breaks keyboard-first today.
- **Double `<h1>`.** `DashboardLayout.vue:55` → make it a `<p>`/`<span>` (page body already has the real `<h1>`). WCAG 1.3.1.
- **Missing labels.** `EditProfileDialog.vue:273` (display-name input — replace the `<p>` with `<Label for="edit-displayName">`); `ScopeSelector.vue:160` (custom-scope `<input>` — add `aria-label`). WCAG 1.3.1.
- **Self-referencing `aria-labelledby`.** `EditProfileDialog.vue:188` — move `id="avatar-picker-label"` to the visible `<p>` at line 185. WCAG 4.1.2.
- **Invalid `role="button"` on `<tr>`.** `AdminAuditView.vue:267` — put a real `<button>` (with `aria-expanded`/`aria-label`) inside the first cell; remove `role`/`aria-expanded` from the row. WCAG 4.1.2.
- **`v-if` status regions.** ~15 sites (e.g. `SecurityView.vue:77`, `AdminGroupsView.vue:103`, `DevicesView.vue:74`) — switch to `v-show` or a persistent `aria-live` container; centralize in `useTransientFlag`. WCAG 4.1.3.
- **Sidebar `<nav>` landmark.** `AppSidebar.vue` / `ui/sidebar` — wrap nav groups in `<nav aria-label="…">` (account + admin).
- **Heading-level skip h1→h4** in danger zones — resolved together with Step 2 (h4 → title-tier `<h3>`-equivalent) + a `<h2>` "Danger zone" or proper nesting. WCAG 1.3.1.
- **Router focus management.** Add a `router.afterEach` that focuses the page `<h1>` (`tabindex="-1"`) on navigation. WCAG 2.4.3.

### Step 4 — `/impeccable adapt` (P0/P1: responsive + touch targets)
- **P0:** `AttributeMapEditor.vue:97,108` 5-col grid (`grid-cols-[1fr_1fr_1fr_3rem_2rem]`) has no mobile breakpoint → SAML attr editing unusable < ~360px. Stack to card-per-row below `sm:`.
- **P1 touch targets < 24px (WCAG 2.5.8):** `Checkbox.vue:24` (16px), `RadioGroupItem.vue:23` (16px), `Switch.vue:23` (20px tall) — expand hit area to ≥24px (wrapper padding or larger control).
- **P1:** claims grid `grid-cols-[minmax(7rem,auto)_1fr]` (`AdminUpstreamIdpsView.vue:148`, `AdminUpstreamIdpDetailView.vue:158`) — add `grid-cols-1 sm:grid-cols-[…]` to stack on mobile.
- **P2 (sub-44 but AA-passing):** `SidebarTrigger.vue:21` and `ThemeToggle.vue:58` are 28px — bump toward 44px (2.5.5 best practice). `ScopeSelector.vue:153` chip-remove ~12px; `LocaleSwitcher.vue:36` select ~28px composite; `AttributeMapEditor.vue:162` `icon-sm` 32px in a cramped grid.
- **P2:** 6-col invitations table on mobile (`AdminInvitationsView.vue:118–136`) — hide non-essential columns below `sm:` or stack.

### Step 5 — `/impeccable layout` (P2: spacing/consistency drift)
- CardContent rhythm drift: `gap-5` on the IdP cards (`AdminUpstreamIdpsView.vue:99`, `AdminUpstreamIdpDetailView.vue:113`) where peers use `gap-4`; `py-4` applied inconsistently across SessionsView/ConnectedAccountsView/DevicesView vs siblings. Normalize.
- `bg-muted` misused as a hover background: `EditProfileDialog.vue:206,226` → `bg-accent` (the hover-well token). `--color-muted` is the secondary-TEXT token, not a bg.
- `OrDivider.vue:9` uses `uppercase tracking-wide` "or" — the eyebrow pattern DESIGN.md bans. Drop the case transform.
- `Input.vue:27` uses `bg-sunken` (recessed) where DESIGN.md specs white-fill inputs — inputs read as read-only wells. Consider `bg-background`/`bg-card`.

### Step 6 — `/impeccable polish` (P3: final pass)
- `AttributeMapEditor.vue:164` dup `aria-label` ("Remove") — append the row name.
- `LoginView.vue:102` link underline only on hover — underline at rest (WCAG 1.4.1).
- `LocaleSwitcher.vue:31` decorative `backdrop-blur-sm` — set `bg-surface` fully opaque, drop blur.
- `main.css:279–282` `::selection` hardcoded `oklch(...)` + no dark variant — tokenize + add `.dark ::selection`.
- `UserAvatar.vue:38` coupled to `sidebar-accent` tokens (used site-wide) — introduce `--color-avatar-*` or use `bg-accent`.
- `UserAvatar.vue:40` avatar `<img>` no `loading="lazy"`.
- `SidebarRail.vue:21` `transition-all` → `transition-colors` (vendored; only if touched).

---

## Carried-over follow-ups from the prior two cycles (fold in while here)
- **Color cycle:** F-1 alias dark overrides (now ALSO a P1 above — same root), F-2 dark-mode FOUC (accepted), F-3 `EditProfileDialog` `hover:bg-muted` (now in Step 5). See `docs/superpowers/plans/2026-06-15-...`? → actually `2026-06-18-color-system-redesign.md` "Review follow-ups".
- **i18n cycle:** F-1 `useLocale` `flush:'sync'`/test-watcher cleanup; F-2 `locales.params.test.ts` nested-brace regex. See `docs/superpowers/plans/2026-06-19-cn-i18n.md` "Review follow-ups".

## Per-fix verification gate (run after each step)
```
cd dashboard && npx vitest run && npx vue-tsc -b && node scripts/check-contrast.mjs && npm run build
cd .. && go build -tags nodynamic ./... && go vet ./...
```
Then rebuild + commit `pkg/webui/dist` at the done-gate. Final acceptance = a human visual pass in BOTH themes (still pending from the prior cycles). Re-run `/impeccable audit` after fixes to confirm the score climbs.

## Suggested execution shape
Brainstorm is unnecessary (this backlog IS the spec). Either run the `/impeccable` commands in order (Steps 1→6), or drive it via `writing-plans` → `subagent-driven-development` against this doc. Steps 1–3 (the P1 root causes) deliver the most score-per-effort.
