# Dashboard redesign on coss ui

**Date:** 2026-10-18
**Status:** Approved (mock approved 2026-10-18)
**Branch:** `develop-coss`, forked from `origin/develop` at `07e4b126`

## Summary

Replace the dashboard's HeroUI component layer with [coss ui](https://coss.com/ui) (Base UI + Tailwind v4) and redesign every surface — the public sign-in flows, the member console and the admin console — in one visual language: layered surfaces with concentric arced edges, built only from coss components and coss tokens. The routing, data, form, i18n and API layers stay as they are; this is a presentation and information-architecture change plus a short list of small backend additions the design needs.

The approved reference is a working mock built from the real coss registry components: `.impeccable/mocks/coss-sandbox/` (gitignored; run `pnpm install && pnpm exec vite` there, scenes at `#signin`, `#consent`, `#group`, `#oidc`, `#idp`, `#idp-test`, `#security`, `#appearance`). The scenario research behind it — every complex screen's real data, limits, states and current defects, read from the code and the devtools mock — is in `.impeccable/research/{public,admin,member}.md`.

## Goals

- One component vocabulary: coss components everywhere, no HeroUI, no react-aria-components, no OverlayScrollbars.
- Layers that mean something: page canvas → inset console panel → section frame (muted) → raised panel (white). Bands such as headers, footers and consequential-save notices live inside a frame's own arcs; nothing crosses a rounded edge.
- Pages that serve the real scenarios the code supports: unbounded identity-provider lists, text-only target-app labels, long admin pages with independently saved sections, nested rules, connection-test stages.
- No regression in behaviour, keyboard access, i18n (`en`, `zh`), dark mode, CSP or test coverage.

## Non-goals

- Changing routes, query/mutation options, the API client, TanStack Form wiring, Lingui setup or the devtools mock.
- New product features beyond those listed under *Backend additions*.
- Fixing the defects in *Found defects* inside the redesign tasks; they are separate tasks so each fix is reviewable on its own.

## Visual language

### Material

| Layer | Treatment (coss) |
|---|---|
| Canvas | `bg-sidebar`; the console sidebar sits directly on it |
| Console panel | `SidebarInset` (sidebar `variant="inset"`): rounded, hairline, `shadow-sm/5` |
| Section | `Frame` (`bg-muted/72`, `rounded-2xl`, `p-1`) with `FrameHeader` / `FrameFooter` on the muted band |
| Panel | `FramePanel` (`rounded-xl`, border, `shadow-xs/5`, 1px bottom bevel) |
| Well | a recessed region inside a panel: `bg-muted/60` + `ring-1 ring-border/60`, `rounded-xl` — for nested rule groups, endpoint sets, callback addresses; never a card inside a card |
| Overlay | coss `Dialog`, `Drawer variant="inset"`, `Popover`, `Menu`, `Toast` unchanged |

Radii are concentric: an inner surface's radius is the outer radius minus the gap. The sign-in stage follows the same rule at a larger scale (frame 28px, gap 6px, card 22px).

Buttons, inputs, selects, checkboxes, switches and badges use coss variants unmodified: flat fills, a 1px inner highlight, no gradients. Segmented choices of a form value use the coss segmented-control recipe on `RadioGroup` (`@/lib/segmented-control`); Tabs only switch panels.

### Type

- Cal Sans (variable) for everything, Paper Mono for exact values — the coss bundled faces, self-hosted (`/fonts/*.woff2`, same-origin under the production CSP). Chinese falls back to `Noto Sans SC` / the system CJK face.
- Monospace only for identifiers a person copies or compares: client IDs, slugs, scopes, claim names, URIs, subjects, codes. Human values (names, email shown as a person's address in prose) stay in the sans.
- Page titles `text-2xl font-semibold tracking-tight`; section titles are coss `FrameTitle`.

### Colour

coss default tokens (neutral) in both themes. Colour carries meaning only: `success`, `warning`, `destructive`, `info` badges and notices; the brand is the instance's own icon and, on public pages, the admin-chosen background.

### Width and density

Console content is centred with `max-w-6xl` (≈1152px) so a 1920px display shows one composed column, not a stretched form. Object pages use a main column plus a 200px "On this page" rail.

## Shared patterns

These become components in `src/components/custom` (the existing convention), each built on coss primitives:

| Pattern | Use | Reference scene |
|---|---|---|
| `ObjectHeader` | icon tile, name, status badges (`Enabled`/`Disabled`, `Restricted`, `Sign-in on`), copyable identifier, protocol/kind, one key fact, actions | `#oidc`, `#idp`, `#group` |
| `Section` (Frame) | title, description, optional header action, panel, optional footer; a section with unsaved edits owns its own footer with Discard / Save | all console scenes |
| Consequential save | when a pending edit has a consequence past the save (scope removal, subject claim/source change, NameID, Remote-User), the section footer states the consequence and the primary button reads as the action ("Remove scope and save", "Review and save") before the existing confirm dialog | `#oidc`, `#idp` |
| `OutlineRail` | sticky "On this page" links; a warning dot on sections with unsaved edits; danger section last in destructive colour; a small facts list beneath | `#oidc`, `#idp` |
| `UnsavedBar` | floating "Unsaved changes in {section} · Jump to it" when the edited section is off-screen | `#idp` |
| `SettingRow` | title + explanation on the left, control on the right, hairline between rows | `#idp` |
| `ValueSet` | a short persisted set of literal values (scopes, email domains): raised mono chips with an inner remove button, a dashed "Add …" chip opening a popover input; suggestions only when the source actually publishes them; an empty set states the policy ("Every domain is accepted.") | `#idp` |
| `UriList` | ordered addresses (redirect / post-logout URIs): one mono input per row, per-row error, "Add address" | `#oidc` |
| `CopyValue` | read-only mono input with a copy button in an `InputGroup` | `#idp`, `#security` |
| `DangerZone` | a destructive-tinted Frame of `SettingRow`s, each stating its consequence | `#idp` |
| `PublicStage` | the sign-in stage: wallpaper, toolbar capsules, centred flexible-height frame with an optional header strip and footer strip, inner card | `#signin`, `#consent` |
| `AppStrip` | the requesting application in the stage's header strip: tile from the app's icon or its label's first letter, name, host | `#signin`, `#consent` |
| `ProviderGrid` | 1 provider: full-width button. 2–6: two columns. 7+: five plus a "+N More providers" button opening a searchable popover; fallback tiles use the name's first letters | `#signin` |
| `ScopeRows` | consent rows with icon, title and detail; new scopes first; "Already allowed" uses the same rows inside a collapsed `Collapsible` | `#consent` |
| `StepHeader` | numbered steps for multi-step dialogs (password → authenticator → recovery codes) | `#security` |
| `StageTimeline` | vertical stage list (passed / failed / skipped / pending) with mono facts | `#idp-test` |

## Surfaces

The approved scenes cover the hardest cases; every other page is composed from the same patterns.

### Public (`PublicLayout`)

- Stage centred by default; the admin's card position (left / centre / right), opacity, blur and theme settings keep working.
- Sign-in: the requesting app is the stage's header strip ("You're signing in to · {app} · {host}"); the card is "Sign in with {instance}", passkey first, username and password visible, then providers through `ProviderGrid`, then "Sign in with your phone or another device"; "Stay signed in on this device" sits on the footer strip.
- Consent and SAML consent: `AppStrip` header, signed-in account with "Switch account", `ScopeRows`, decision buttons on the footer strip.
- TOTP, recovery, enrollment, welcome, setup-signin, federation flow, VRChat proof, require-identity, pair, error and maintenance pages use `PublicStage` with the same header/footer strip rules.

### Console shell (`ConsoleLayout`)

coss `Sidebar variant="inset"`: instance switcher header, role-gated groups (account; Directory; Applications; System), counts as `SidebarMenuBadge`, account menu in the footer; header row with `SidebarTrigger`, `Breadcrumb` (current entity name, not the section), search `InputGroup` with `Kbd`; mobile uses the sidebar's sheet mode. `SudoDialog`, toasts and `RequestDetailsDialog` move to coss `Dialog` / `Toast`.

### Member pages

Security (`#security`): Passkeys section first, password and authenticator second (not collapsed), linked identities, access tokens; setup flows in step dialogs. Profile, Connected apps and Devices follow the same section and row patterns.

### Admin pages

- Applications (OIDC `#oidc`, SAML, forward-auth): `ObjectHeader` + main column + `OutlineRail`; sections Sign-in, Access, Managers, Identity, Appearance, Credentials, Disable or delete; access shows the evaluation order and each selected group's kind.
- Identity providers (`#idp`): the application object-page frame (provider name as the title, slug, protocol, readiness). Sections: Sign-in (allow sign-in, provisioning as described cards), Connection (issuer, client ID, client authentication with described options, client-secret row, sign-in response, scopes `ValueSet`, endpoints discover / by hand with override rows, JWKS-none stated explicitly, avatar token switch, callback addresses), Accounts and claims (domains `ValueSet`, verified-email switch, claim mapping table with the latest test's sources), Diagnostics, Appearance (name, icon), Disable or delete. Secrets (OIDC client secret, Steam Web API key) are set from their own rows, not the danger zone. Steam and VRChat show their own sections (Web API key; Operator session) in place of the OIDC ones.
- User groups (`#group`): rule builder with the existing grammar (fact `Select`, `is` / `is not` segmented control, value `Select`, nested any/all well, add condition / group), error marked on the row, Save blocked while the editor knows the rule is invalid; side column with match count, account explanation and "Used by".
- Users, invitations, logs, settings (`#appearance` for the sign-in page editor with live preview) use list frames on coss `Table` and the shared patterns.

## Backend additions

Small, separately scoped tasks the design depends on; until each lands the UI degrades gracefully as noted.

1. **Target-app host on the sign-in page.** PHB-136 returns a text label only. Add the app's host (and icon URL when one is set) to the sign-in context. Until then `AppStrip` shows the label and a letter tile.
2. **Latest connection-test result.** Diagnostics shows "Passed 2 min ago" and the claim table's "Last test returned" column. Add a read of the provider's most recent finished test run: a value-free summary (status, time, per-field source/claim/presence, staleness) kept 30 days, because the tested identity is the running administrator's own upstream account and every administrator can read the summary. Until then the status line and column are omitted.
3. **Readability check** on the sign-in page editor is client-side (sample the image under the card) and needs no backend; it ships as its own frontend task after the editor.

## Found defects (separate tasks)

From the research (`.impeccable/research/admin.md` §9, `member.md`, `public.md`):

- Rule editor: deleting a nested group's last condition turns it into `login_method = passkey`; deleting every root condition still saves that rule; the root combinator has no control; save is not blocked by known problems.
- App managers: assign/remove do not await the POST (`api/mutations.ts`), so failures are swallowed and success is always toasted.
- Manual-group allow/deny decisions have API and mutations but no UI.
- Failures shown twice (inline alert and toast) on several public and member forms.
- Smaller copy and alignment defects listed in the research files.

## Conventions to rewrite on this branch

`AGENTS.md`, `dashboard/AGENTS.md` and `DESIGN.md` currently mandate HeroUI and its Theme Builder configuration. They are rewritten to: coss registry components in `src/components/ui` (copied, not imported from a package, with the registry's own import paths rewritten to `@/components/ui`, `@/lib`, `@/hooks`); shared patterns in `src/components/custom`; the material and type rules above; verification at 1440, 1920 and 390 widths, both themes, both locales. `DESIGN.md` is regenerated from the built result at the end.

## Migration strategy

HeroUI and coss define the same CSS custom properties (`--background`, `--accent`, …), so they cannot be styled side by side. Nobody uses the dashboard while the branch is being migrated (owner decision, 2026-10-19), so intermediate states only need to compile, not to work:

1. **Foundation** (done). Base UI, the coss registry, stylesheet and fonts; the shared layer in `src/components/custom` rebuilt on coss; the console shell.
2. **Pages by area**: public flows, member pages, admin applications, identity providers, groups, users/invitations/logs, settings. A page is rewritten directly on coss components. Shared components may change their API to coss's own vocabulary (`onClick`, `disabled`, coss variant names); callers are updated mechanically in the same task so `tsc` stays green. No new compatibility layer between HeroUI and coss is added; an unmigrated page may render or behave wrongly until its phase.
3. **Removal.** Delete `@heroui/react`, `react-aria-components`, `react-aria`, `overlayscrollbars*`, `tailwind-variants` and the interim bridges from the foundation phase (host-overlay container, react-aria top layer); rewrite conventions docs; regenerate `DESIGN.md`.

`origin/develop` is merged into `develop-coss` at each phase boundary; conflicts in page files are resolved by porting the incoming behaviour change into the coss version.

## Verification

Per task: `pnpm exec tsc --noEmit`, Biome on touched files, the touched and new test files, `pnpm run i18n:check` when strings change, and for a visual change a Playwright pass against the devtools mock (`?mock`) at 1440×900 light `en` and 390×844 dark `zh`, reviewed at 2×. Tests are written for the migrated component's behaviour, not for interim states. Per phase: `mise run ci:frontend` once, and the browser pass at 1440×900, 1920×1080 and 390×844, light and dark, `en` and `zh`, across the phase's routes. `mise run ci:smoke` and the full gate run at the end of the migration.
