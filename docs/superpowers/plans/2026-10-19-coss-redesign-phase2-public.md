# coss redesign — Phase 2: public flows

> **For agentic workers:** REQUIRED SUB-SKILL: Use minipowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild every public page (sign-in and its second steps, pairing, consent, SAML consent, enrollment, welcome, setup-signin, federation flow, VRChat proof, require-identity, maintenance, error landing, public route errors) on coss in the approved `#signin` / `#consent` design, and give the sign-in page the requesting app's host and icon.

**Architecture:** `PublicLayout` draws the wallpaper, the toolbar capsules and a centred, flexible-height **stage**: a frosted outer frame (28px radius, 6px gap) whose optional header strip and footer strip surround an inner card (22px radius). Pages fill the card through `PublicStep` and put content into the strips through `PublicStep`'s `strip` and `footer` props, which portal into the stage. The shared `Button` moves to coss's own prop vocabulary first, so every page is written once.

**Tech Stack:** React 19, TanStack Router/Query/Form, Lingui 6 (en, zh), coss ui (Base UI 1.9) in `dashboard/src/components/ui`, Tailwind 4, Vitest + Testing Library, Go (one backend task).

**References:**
- Approved mock: `.impeccable/mocks/coss-sandbox/src/scenes/signin.tsx` (`Wall`, `Stage`, `AppStrip`, sign-in body) and `scenes/consent.tsx` (`Rows`, collapsed "Already allowed", decision footer). Run it with `pnpm exec vite --port 5311 --host 127.0.0.1` in that directory; scenes at `#signin`, `#consent`.
- Scenario research for every public page (data, limits, states, error codes, current screenshots): `.impeccable/research/public.md`. Each page task names its section.
- Screenshot helper: `node .impeccable/scripts/shoot.mjs <outDir> '<json>'` against `pnpm exec vite --port 5292 --host 127.0.0.1` in `dashboard/` (JSON fields in the script's header; mock parameters are parsed in `dashboard/src/devtools/mock/query.ts`).

## Global Constraints

- Work in `/home/tundra/projects/tundra/prohibitorum-worktrees/develop-coss` on branch `develop-coss`; dashboard commands run in `dashboard/`.
- Nobody uses the dashboard during the migration. Do not build anything whose only purpose is to let HeroUI and coss work together; an unmigrated page may render or behave wrongly. `pnpm exec tsc --noEmit` must pass at every commit.
- A file a task rewrites ends with zero imports from `@heroui/react`, `@heroui/styles`, `react-aria-components`, `react-aria` and `tailwind-variants`.
- coss components are the base: restyle one only when the design needs it, building on coss's own style and preserving its details (hover, active, focus-visible and disabled states, dark mode, sizing rhythm, `data-slot` hooks); otherwise compose and pass `className` for layout.
- Public material: stage frame `rounded-[28px] p-1.5`, card `rounded-[22px]` (concentric: frame radius minus the gap). Toolbar capsules and the wallpaper credit are `rounded-xl` / `rounded-lg` bars, never fully rounded pills. Buttons are flat coss buttons. No gradients (except the admin-chosen backdrop presets that already exist), no decorative marks, no keyboard-shortcut hints.
- Type: Cal Sans for everything; Paper Mono (`font-mono`) only for values a person copies or compares (hosts, scopes, codes, usernames in `@name` form, URLs, IDs). Page titles `text-2xl font-semibold tracking-tight`.
- Behaviour is preserved exactly unless a task says otherwise: every route, loader, search parameter, mutation, error-code message, busy lock, focus-to-title on step change, `return_to` handling, "keep me signed in" semantics, back-forward-cache restore and polling interval described in `.impeccable/research/public.md`.
- User-facing strings are Lingui messages with explicit ids and both `en` and `zh` entries (`pnpm run i18n:extract`, then fill `src/locales/zh/messages.po`; zh uses the 你 register and corner brackets 「」 for UI nouns as the catalogue already does). Admin-written text (maintenance message, SAML attribute labels, app/provider/instance names, custom scopes) is shown as-is.
- Tests find elements by role, name, label and `data-*`; never by coss or HeroUI class names. Write tests for the new components' behaviour and keep each page's existing behavioural assertions (rewrite their queries to the new structure; do not delete a behaviour's test because markup changed).
- Per task checks: `pnpm exec tsc --noEmit`; `pnpm exec biome check <touched files>`; `pnpm exec vitest run <touched and new test files>`; `pnpm run i18n:check` when strings change. No full-suite run inside a task. A task that changes what a page looks like ends with two screenshots per changed route through the helper — 1440×900 light `en` and 390×844 dark `zh` — viewed at 2× by the implementer, with every visible defect fixed before the commit and the paths listed in the report.
- Dependencies stay pinned to exact versions; no new dependency.
- Where this plan names an existing Lingui id, confirm it in `src/locales/en/messages.po`; if the real id differs, change that id's message rather than adding a duplicate.
- Commits: Conventional Commits subject, imperative; end with the implementing agent's own truthful `Co-Authored-By: <model> <noreply@anthropic.com>` trailer. Nothing written may cite this plan, the spec, task numbers or the mock.

---

### Task 1: Button in coss's vocabulary

**Files:**
- Modify: `dashboard/src/components/custom/Button.tsx`, `Button.test.tsx`, `SubmitButton.tsx`
- Modify: every file importing `@/components/custom/Button` (66 sources, 2 tests) — mechanical prop renames only
- Modify: `dashboard/src/forms/forms.test.tsx` if its pending assertions need the new spelling

**Interfaces:**
- Produces (binding):
  ```ts
  type ButtonVariant = "default" | "secondary" | "outline" | "ghost" | "destructive" | "destructive-outline" | "link";
  type ButtonSize = "xs" | "sm" | "default" | "lg" | "xl" | "icon-xs" | "icon-sm" | "icon" | "icon-lg" | "icon-xl";
  interface ButtonProps extends React.ComponentProps<"button"> {
    variant?: ButtonVariant;   // coss names, default "default"
    size?: ButtonSize;         // coss names, default "default"
    pending?: boolean;         // spinner beside the text (or in an icon button's place), aria-busy and aria-disabled, presses ignored, focus kept
    render?: React.ReactElement; // coss render prop, e.g. a router Link
  }
  ```
  `Button` renders the coss `Button` (`@/components/ui/button`) — no react-aria import. `SubmitButton` keeps its role (form-bound submit that is pending while the form submits) on the new props.
- Codemod mapping for callers: `onPress`→`onClick`; `isDisabled`→`disabled`; `isPending`→`pending`; `fullWidth`→`className="w-full"` (merged with an existing className); `isIconOnly` + `size="sm"`→`size="icon-sm"`, `isIconOnly` alone→`size="icon"`; `size="sm"`→`"sm"`, `size="md"`/none→default, `size="lg"`→`"lg"`; variants `primary`→`default`, `secondary`→`outline`, `outline`→`outline`, `tertiary`→`ghost`, `ghost`→`ghost`, `danger`→`destructive`, `danger-soft`→`destructive-outline`, `warning`→`default`; drop `excludeFromTabOrder` (→`tabIndex={-1}`), `preventFocusOnPress` (drop), `slot` (drop). A handler typed for react-aria's `PressEvent` becomes a plain `() => void` or `MouseEvent` handler.
- A trigger that a still-HeroUI component (`Dropdown`, `Popover`, `Tooltip`, `FileTrigger`) wraps may stop opening; that is accepted (see Global Constraints) — do not add a bridge.

- [ ] **Step 1: Write failing tests** in `Button.test.tsx`: `onClick` fires on click and Enter/Space; `disabled` is native disabled and ignores clicks; `pending` sets `aria-busy="true"` and `aria-disabled="true"`, ignores clicks and keyboard activation, keeps focus when a focused button becomes pending, and keeps a text label beside the spinner (`data-slot="spinner"` present, accessible name still the text); an icon button (`size="icon-sm"`) that is pending shows the spinner instead of its icon; `render={<a href="/x" />}` renders a link with the button's classes. Expected red: props not accepted / behaviours missing.
- [ ] **Step 2: Run** `pnpm exec vitest run src/components/custom/Button.test.tsx` — fails on the new props.
- [ ] **Step 3: Implement** Button and SubmitButton, then apply the codemod to every caller (a script under `/tmp` is fine; review each diff hunk). Run `pnpm exec tsc --noEmit` until clean.
- [ ] **Step 4: Run** Button, SubmitButton, `forms.test.tsx` and the tests of any file whose handler signature changed; Biome on touched files.
- [ ] **Step 5: Commit** — `refactor(dashboard): speak coss's button vocabulary`

---

### Task 2: Public material on coss tokens

**Files:**
- Modify: `dashboard/src/components/custom/login-appearance/surface-style.ts`, `surface-style.test.ts`
- Modify: `dashboard/src/pages/admin/settings/LoginPreview.tsx` only as far as needed to compile

**Interfaces:**
- Produces (binding names, behaviour below): `surfaceStyle(surface: LoginSurface): SurfaceStyle`, `cardSurfaceStyle(surface: LoginSurface): SurfaceStyle`, `capsuleSurfaceStyle(surface: LoginSurface): SurfaceStyle`, `SurfaceStyle { className: string; style?: CSSProperties }` — same names as today.
- Behaviour: today a translucent surface mixes the HeroUI `--surface` token, which no longer exists, so a translucent card has no fill at all. Rebuild on coss tokens:
  - An opaque card: `bg-background` (nothing else).
  - A translucent card: its fill is the theme's background colour at `opacity`% — written from the colour constants coss.css defines `--background` from (`--color-white` in light; the `.dark` value in dark), because a property cannot be redefined from its own value — plus `backdrop-blur-xl backdrop-saturate-150` when `blur`.
  - Controls on a translucent card (coss inputs, outline/secondary buttons, checkboxes, select triggers, separators): redefine the coss tokens they read inside the card subtree (`--background` for input/button fills, `--input` and `--border` for their edges, `--muted`/`--accent` for hover fills) as see-through mixes at half the card's opacity, derived from the same constants, so the picture reads through fields and the public page and the settings preview agree. Popups (menus, popovers, select lists) portal out of the card and stay opaque.
  - A capsule: opaque → `bg-background`; translucent → the card's translucent fill rule with the capsule's own settings.
- [ ] **Step 1: Write failing tests** (`surface-style.test.ts`): opaque card yields `bg-background` and no inline style; translucent card yields a fill class and the opacity custom property `50%` for `opacity: 50`; blur adds the backdrop classes only when translucent; no returned class or style references `--surface`, `--field-background`, `--default`, `--separator` or `--surface-secondary`. Expected red: the HeroUI token names are still emitted.
- [ ] **Step 2: Run** `pnpm exec vitest run src/components/custom/login-appearance/surface-style.test.ts`.
- [ ] **Step 3: Implement.** Keep the doc comments' shape (one summary line, one short paragraph).
- [ ] **Step 4: Verify in the browser** after Task 3 lands (this task has no page of its own yet): note in the report that the visual check is deferred to Task 3. Run tests, tsc, Biome.
- [ ] **Step 5: Commit** — `fix(dashboard): draw translucent sign-in surfaces from coss tokens`

---

### Task 3: The public stage

**Files:**
- Create: `dashboard/src/components/custom/PublicStage.tsx` (stage frame, card, strip slots), `PublicStage.test.tsx`
- Modify: `PublicLayout.tsx`, `PublicLayout.test.tsx`, `AppToolbar.tsx`, `InstanceIdentity.tsx`, `PublicStep.tsx`, `PublicStep.test.tsx`, `PageHeading.tsx`, `OrSeparator.tsx`, `login-appearance/WallpaperCredit.tsx`, `login-appearance/card-position.ts`, `RouteFeedback.tsx` (public and whole-window variants and `PublicPending` only)
- Delete: `PublicCard.tsx` and `PageFrame.tsx` once nothing imports them (`LoginPreview.tsx` may keep compiling against a small export you leave for it; the settings phase rebuilds the preview)

**Interfaces:**
- Produces (binding):
  ```ts
  // PublicStage.tsx
  export function PublicStage(props: {
    width?: "default" | "wide";            // 440px sign-in pages, 480px consent pages
    position?: LoginCardPosition;           // from lg: left, center, right; below lg centred
    frame: "frosted" | "plain";             // frosted over a picture, gradient or colour; plain over the page's own background
    card?: SurfaceStyle;                    // the admin's card surface (Task 2)
    children: React.ReactNode;              // card body
  }): React.ReactElement;
  export function useStageSlots(): { strip: HTMLElement | null; footer: HTMLElement | null; setWidth: (w: "default" | "wide") => void };

  // PublicStep.tsx
  export const PublicStep: React.FC<{
    title: React.ReactNode; titleKey?: string;
    back?: { label: string; onClick: () => void; disabled?: boolean };
    description?: React.ReactNode;
    strip?: React.ReactNode;       // rendered into the stage's header strip (e.g. AppStrip)
    footer?: React.ReactNode;      // rendered into the stage's footer strip (e.g. remember checkbox, decision buttons)
    wide?: boolean;                // asks the stage for the 480px width while mounted
    children?: React.ReactNode; actions?: React.ReactNode;
  }> & { Actions: React.FC<{ layout?: "single" | "decision"; children: React.ReactNode }> };

  // OrSeparator.tsx
  export function OrSeparator(props: { label?: React.ReactNode }): React.ReactElement; // default label "or"
  ```
- Stage geometry (from the mock's `Stage`): frame `rounded-[28px] p-1.5`; frosted frame `bg-background/60 shadow-2xl shadow-black/30 ring-1 ring-white/40 backdrop-blur-2xl`; plain frame `bg-muted/72 ring-1 ring-border shadow-xs/5`. Header strip `flex items-center gap-3 px-3 pt-1.5 pb-3`; footer strip `flex items-center gap-3 px-3 pt-2.5 pb-1.5 text-sm`; a strip with no content is not rendered. Card `rounded-[22px] border bg-clip-padding shadow-xs/5` with the coss 1px bottom bevel (`before:` shadow as in the mock) and the admin card surface class/style; card body padding `p-7 max-sm:p-5`. Width `sm:w-[440px]` or `sm:w-[480px]`, full width minus `mx-2` below `sm`.
- Placement: the stage is centred vertically in the space under the toolbar (`min-h` of the viewport minus the toolbar, `items-center`) and grows with its content; when taller than the window the page scrolls (no inner scroll area). From `lg`, `left`/`right` align the stage's outer edge with the toolbar capsules as today.
- Toolbar (`AppToolbar`): `h-16` row, `px-5`; left capsule = instance mark (`size-6 rounded-md`, icon or first letter) + instance name, `h-9 rounded-xl ps-1.5 pe-3 font-medium text-sm`, truncating; right capsule `h-9 rounded-xl px-1` with icon buttons (`variant="ghost" size="icon-sm"`) for language (coss `Menu` with the `LanguageMenuItems`) and, only when the theme is switchable, theme (coss `Menu` with radio items from `useThemeOptions`). Capsule surface from `capsuleSurfaceStyle` plus `ring-1 ring-black/8 shadow-xs/5` (white ring in dark).
- Wallpaper credit: `h-7 rounded-lg bg-black/45 px-2.5 text-white/90 text-xs backdrop-blur-md` with a leading `ImageIcon` (`size-3.5`), links underlined; placement logic of `card-position.ts` kept (from `lg` the bottom corner away from the stage; below `lg` under the stage).
- `PageHeading`: `text-2xl font-semibold tracking-tight`, still focused on mount and remount, not in the tab order.
- Callers: `PublicStep` loses `media` and spells `back.onPress`/`isDisabled` as `onClick`/`disabled`; update every caller in this task (move an app icon that was `media` into `strip` as a plain `EntityAvatar` for now — Tasks 7 and 10 replace it with `AppStrip`) so `tsc` stays green.
- Route feedback on public pages: `PublicPending` is a centred coss `Spinner`; `PublicRouteError`, `PublicRouteNotFound`, `AppRouteError` and `AppRouteNotFound` render through `PublicStep` inside the stage (the whole-window variants draw a plain stage without toolbar) with the existing single way out and "Details".
- [ ] **Step 1: Write failing tests:** PublicStage — renders the card body; a `PublicStep` with `strip`/`footer` puts that content inside the stage's header/footer regions (`data-slot="stage-strip"` / `"stage-footer"`) and outside the card (`data-slot="stage-card"`); no strip region is rendered when nothing fills it; `wide` sets `data-width="wide"` on the stage while mounted and restores `default` on unmount; `frame` and `position` are reflected as `data-frame` / `data-position`. PublicStep — the title is the only `h1`, takes focus on mount and again when `titleKey` changes; `back` renders a button with its label that calls `onClick`. PublicLayout — the theme control is absent when the theme is forced; the credit renders only for Bing/Unsplash. OrSeparator — default text "or", custom label shown. RouteFeedback — public error shows its title, message, one way out and "Details" inside the stage card.
- [ ] **Step 2: Run them**; failures are the missing module and the new structure.
- [ ] **Step 3: Implement** following the mock's `Wall`/`Stage`. `frame` is `plain` when `loginAppearance.background.source === "none"`, else `frosted`.
- [ ] **Step 4: Run** the touched tests, tsc, Biome. Screenshots: `/login` signed out (`mock.session.signedIn=false`) with the default background, with `gradient` and with `images` (find the mock parameters for the login appearance in `src/devtools/mock/query.ts`), card position right at 1440, a translucent card (verifies Task 2: fill visible, fields see-through, text readable), the public route error, at the two standard shots.
- [ ] **Step 5: Commit** — `feat(dashboard): stage the public pages on coss`

---

### Task 4: Name the requesting app's host and icon (backend)

**Files:**
- Modify: `pkg/protocol/oidc/forward_auth_context.go`, `forward_auth_context_test.go`; the SAML resolver it calls (`p.samlContext`, wired from `pkg/server`) and its tests
- Modify: `dashboard/src/api/raw-paths.ts` (`LoginContext`), `api.md` (the endpoint's response), and the OpenAPI output if the endpoint is described there (`mise run dev:openapi`, then `sh scripts/api-types.sh` — check `mise run ci:api-types` passes)

**Interfaces:**
- Produces: `GET /api/prohibitorum/forward-auth/login-context` answers `{ "application": { "label": string, "host"?: string, "iconUrl"?: string } | null }`.
  - `host`: ordinary OIDC → the host of the request's `redirect_uri` (already exact-matched against the registered list); forward-auth → `forward_auth_host`; SAML → the host of the SP's registered ACS URL used for this request. Omitted when not derivable.
  - `iconUrl`: the application's uploaded icon URL built exactly as other app icon URLs are (see `pkg/server/entity_icon_url.go` and `requireIdentityIconURL` in `pkg/server/handle_require_identity.go`), omitted when the app has no icon.
  - Every existing `none()` / error path is unchanged; the number of database reads must stay equal between an unknown and a known-but-not-forward-auth client (the comment in the handler explains the timing reason) — reading the icon must not add a read only on the known path; if it needs one, do it on both paths or derive it from the already-loaded row.
- [ ] **Step 1: Write failing Go tests:** each branch (ordinary OIDC with/without display name, forward-auth, SAML) returns the expected `host`; a client with an icon returns its `iconUrl`, one without omits it; disabled/unregistered/unknown cases still return `{"application":null}`; the read-count invariant holds (extend the existing test that guards it).
- [ ] **Step 2: Run** `go test ./pkg/protocol/oidc/ ./pkg/server/ -run 'LoginContext|ForwardAuthContext|SAML.*Context'` — fails on the missing fields.
- [ ] **Step 3: Implement**, update `raw-paths.ts` (`LoginContext.application` gains `host?: string; iconUrl?: string`), `api.md`, and regenerate API types if the endpoint appears in the OpenAPI document.
- [ ] **Step 4: Run** the Go tests above, `go vet ./pkg/...`, `pnpm exec tsc --noEmit`.
- [ ] **Step 5: Commit** — `feat: name the requesting app's host and icon on the sign-in page`

---

### Task 5: AppStrip and ProviderGrid

**Files:**
- Create: `dashboard/src/components/custom/AppStrip.tsx`, `AppStrip.test.tsx`, `ProviderGrid.tsx`, `ProviderGrid.test.tsx`
- Delete: `ProviderButtons.tsx`, `ProviderButtons.test.tsx` (their behaviour moves into `ProviderGrid` and its tests); update the imports in `pages/Login.tsx` and `pages/public/Enroll.tsx` to the new component without restyling those pages yet

**Interfaces:**
- Produces (binding):
  ```ts
  export function AppStrip(props: {
    lead: React.ReactNode;      // e.g. "You're signing in to", "Asking for access"
    name: string;               // the app's label, as the server names it
    host?: string;              // mono, muted
    iconUrl?: string;           // else a tile with the name's initials
  }): React.ReactElement;

  export function ProviderGrid(props: {
    providers: readonly FederationProvider[];
    href: (provider: FederationProvider) => string;
    disabled?: boolean;
    onLeavingChange?: (leaving: boolean) => void;
  }): React.ReactElement | null;
  ```
- AppStrip (mock `AppStrip`): `EntityAvatar` tile `size-10 rounded-xl` (icon or initials), then a `min-w-0 leading-tight` block: lead `text-muted-foreground text-xs`, name `truncate font-semibold text-base`, host `truncate font-mono text-muted-foreground text-xs`. The full name is the element's accessible text even when truncated (`title` on the truncated name).
- ProviderGrid:
  - 0 providers → `null`.
  - 1 → one full-width `outline` button: provider tile (`EntityAvatar size="sm"`) + "Continue with {name}" (existing id `federation.continue_with`).
  - 2–6 → `grid grid-cols-2 gap-2` of `outline` buttons, `min-w-0 justify-start`: tile + truncated name; accessible name "Continue with {name}".
  - 7 or more → the first five as in 2–6, then a sixth cell "+N" tile + "More providers" (N = providers − 5) opening a coss `Popover` (`side="top" align="end"`, `w-72 p-1.5`) with an `InputGroup` search field "Find a provider" (filters case-insensitively by display name; an empty result says "No provider matches") and the remaining providers as full-width menu-like buttons (tile + name). Providers stay in the server's order (alphabetical); no reordering.
  - Fallback tiles use the name's initials (`EntityAvatar`'s `initialsOf`), so "University SSO" reads "US".
  - Leaving: pressing a provider calls `loadDocument(href(provider))`; that button shows `pending`, every other provider control (including "More providers") is `disabled`; `onLeavingChange(true)`; a `pageshow` with `persisted` restores everything and calls `onLeavingChange(false)`; `disabled` prop disables all.
  - New strings: `federation.more_providers` "More providers", `federation.find_provider` "Find a provider", `federation.no_provider_match` "No provider matches".
- [ ] **Step 1: Write failing tests:** AppStrip — name, lead and host render; with `iconUrl` an image is shown, without it the initials; the name is reachable by role text even when long. ProviderGrid — 0 → nothing; 1 → one button named "Continue with GitLab"; 4 → four buttons in a grid; 9 → five provider buttons plus "More providers" naming 4 more, the popover lists the 4, search "ste" leaves only "Steam", search "zzz" shows "No provider matches"; pressing a provider calls `loadDocument` with `href`, marks it busy (`aria-busy`) and disables the rest; a persisted `pageshow` re-enables them and calls `onLeavingChange(false)`; `disabled` disables all. Mock `loadDocument` as the deleted ProviderButtons test did.
- [ ] **Step 2: Run them**; fails on missing modules.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the two test files plus `Login.test.tsx` and `Enroll.test.tsx` (provider assertions may need the new names), tsc, Biome, i18n:check. No screenshots (pages restyle in later tasks).
- [ ] **Step 5: Commit** — `feat(dashboard): add the app strip and the provider grid`

---

### Task 6: Sign-in pages

Research: `.impeccable/research/public.md` §0, §1 (all subsections). Mock: `#signin`.

**Files:**
- Modify: `dashboard/src/pages/Login.tsx`, `Login.test.tsx`, `Login.autofill.test.tsx`, `components/custom/LoginShell.tsx`, `RememberCheckbox.tsx`, `TotpSetup.tsx`, `TotpSetup.test.tsx`, `RecoveryCodes.tsx`, `SurfaceAlert.tsx` (only if the sign-in banners need it)

**Interfaces:**
- Consumes: `PublicStep` (`strip`, `footer`), `AppStrip`, `ProviderGrid`, `OrSeparator({label})`, Task 4's `application.host` / `application.iconUrl` (via `useLoginContext`, which now returns `application?: { label: string; host?: string; iconUrl?: string }`).
- Password step layout (binding):
  - Header strip (only when the app is known): `AppStrip lead="You're signing in to" name={label} host={host} iconUrl={iconUrl}`. The title does not mention the app.
  - Title: "Sign in with {instance}" (new id `login.title.instance`; replaces `login.title` and `login.title.application`). Description, only when the app is known: "{application} uses your {instance} account. You'll go back to it after signing in." (new id `login.description.application`).
  - Passkey button: `size="xl"` full width `h-12 justify-start gap-3 ps-2`, a leading icon well `size-8 rounded-md bg-primary-foreground/12 ring-1 ring-primary-foreground/16` holding `Fingerprint`, label "Sign in with a passkey". No keyboard hint. Unsupported → `variant="outline"` and disabled, followed by the existing muted line.
  - `OrSeparator label="or with your password"` (new id `login.or_password`).
  - Username and password as coss `Field` + `Input size="lg"` (same autocomplete, validation and errors), then "Continue" `variant="outline" size="lg"` full width with a trailing `ArrowRight` (`variant="default"` when passkeys are unsupported). Existing id `login.password.submit` changes its message to "Continue".
  - When providers or the device button show: `OrSeparator label="or continue with"` (new id `login.or_continue`), `ProviderGrid`, then a ghost button "Sign in with your phone or another device" (`QrCode` icon; existing behaviour of the device button; message of its existing id changes to this text) aligned start.
  - Footer strip: the remember checkbox "Stay signed in on this device" (existing id `login.remember`, new message) — shared by every method as today.
  - Banners (maintenance admin bypass, uninitialised, link error) render in the card above the title as coss `Alert`s; link error and uninitialised replace the form as today.
- TOTP and recovery steps: same stage; title "Signing in as {username}" with the back button; header strip shows the AppStrip when the app is known; footer strip empty. Recovery reset: `TotpSetup` (QR 200px, "Setup key" `CopyValue`) and `RecoveryCodes` on coss; the reset-complete step keeps its behaviour.
- [ ] **Step 1: Update tests first:** in `Login.test.tsx` rewrite queries to the new names and add: the title reads "Sign in with Prohibitorum" (mock instance name) and an app context renders the strip with label, host and description; the remember checkbox sits in the stage footer (`data-slot="stage-footer"`) and its value still reaches password begin, passkey finish, provider links and pairing; with passkeys unsupported "Continue" is the primary button. Keep every existing behaviour test (busy lock, autofill, failures, second-step restart, recovery reset). Expected red: new texts/structure missing.
- [ ] **Step 2: Run** `pnpm exec vitest run src/pages/Login.test.tsx src/pages/Login.autofill.test.tsx src/components/custom/TotpSetup.test.tsx`.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the tests above, tsc, Biome, i18n:check. Screenshots (both standard shots each): `/login` with no app; with an app context (find how the mock answers login-context — if it never returns an application, add a `mock.login.application` option to the mock with label, host and icon, as other mock options are added, with a test in `query.test.ts`); with 1, 4 and 9 sign-in providers (`mock.federationProviders`) and the "More providers" popover open; passkeys unsupported is not reachable in the mock (state it); maintenance admin bypass; `/login/totp`; `/login/recovery` with "Reset my authenticator" checked.
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild the sign-in pages on the public stage`

---

### Task 7: Consent and SAML consent

Research: §3, §4. Mock: `#consent`.

**Files:**
- Create: `dashboard/src/components/custom/ScopeRows.tsx`, `ScopeRows.test.tsx`
- Modify: `dashboard/src/pages/public/Consent.tsx`, `Consent.test.tsx`, `consent/ConsentRequest.tsx`, `consent/ConsentExpired.tsx`, `SamlConsent.tsx`, `SamlConsent.test.tsx`

**Interfaces:**
- Produces (binding):
  ```ts
  export interface ScopeRow { key: string; icon: React.ReactNode; title: React.ReactNode; detail?: React.ReactNode }
  export function ScopeRows(props: { rows: ScopeRow[] }): React.ReactElement; // one bordered panel, a hairline between rows
  ```
  Row: icon well `size-7 rounded-lg bg-muted text-muted-foreground` (`[&_svg]:size-4`), title `font-medium text-sm` (a custom scope's title is its raw string in `font-mono text-[13px] wrap-anywhere`), detail `text-muted-foreground text-xs`. Panel `divide-y overflow-hidden rounded-xl border bg-background shadow-xs/5`, row `flex items-start gap-3 px-3.5 py-3`.
- OIDC consent layout (`wide`):
  - Header strip: `AppStrip lead="Asking for access" name={client.displayName} iconUrl={client.logoUri}` (no host: the consent request carries none).
  - Title: first time "Allow {app} to access your account?"; incremental "{app} is asking for more access" (existing ids).
  - Account bar: `rounded-xl bg-muted px-3 py-2 text-sm` with the account avatar (`size-6`), "Signed in as {name}" and a ghost `sm` "Switch account" at the end (existing "Use another account" behaviour; message changes to "Switch account").
  - First time: label "{app} will be able to" then `ScopeRows` of all scopes. Incremental: label "New for {app}" with an `info` `Badge` of the count, the new scopes' rows, then a coss `Collapsible` closed by default whose trigger reads "Already allowed" with an `outline` `Badge` of the count and a rotating chevron, and whose panel shows the already-granted scopes as `ScopeRows` — the same row template as the new ones. No date (the API does not provide one).
  - Scope copy and icons are the existing ones (research §3 item 4), unknown `identity:` slug shown raw as today.
  - The revoke footnote and the policy/terms links (three copy variants) render as one `text-muted-foreground text-xs` paragraph under the rows.
  - Footer strip: `grid w-full grid-cols-2 gap-2` with "Deny" (`outline` `lg`) and "Allow" (`default` `lg`); pending/disabled behaviour as today.
- SAML consent: same layout; strip lead "Asking for access", title "Continue to {sp}?", label "{sp} will receive", one row per attribute label with the `Tag` icon and no detail, the zero-attribute fallback row, footer "Not now" / "Continue".
- Expired: `PublicStep` titled "Unable to continue" with a destructive coss `Alert` and the "Try again" action as today.
- [ ] **Step 1: Write failing tests:** ScopeRows renders one row per scope with title and detail. Consent: first-time lists all scopes under "Allow {app} …"; incremental shows "New for {app}" with the count, the "Already allowed" trigger collapsed (`aria-expanded="false"`) and, once opened, the granted scopes as rows; Deny/Allow sit in the stage footer and keep their pending behaviour; "Switch account" signs out and goes to `/login?return_to=…`. SAML: attributes as rows, zero attributes → fallback row, "Not now"/"Continue" in the footer. Keep existing behavioural assertions.
- [ ] **Step 2: Run** the three test files; fails on new structure.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** tests, tsc, Biome, i18n:check. Screenshots: consent first-time, incremental (closed and opened), no logo + only terms + unknown identity slug + long custom scope, expired; SAML consent with 3, 10 and 0 attributes.
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild consent on the public stage`

---

### Task 8: Enrollment

Research: §5. Shares `PasswordTotpSetup` with Task 9's setup-signin.

**Files:**
- Create: `dashboard/src/components/custom/StepHeader.tsx`, `StepHeader.test.tsx`
- Modify: `dashboard/src/pages/public/Enroll.tsx`, `Enroll.test.tsx`, `enroll/PasswordTotpSetup.tsx`, `enroll/PasswordTotpSetup.test.tsx`, `enroll/PasskeyButton.tsx`

**Interfaces:**
- Produces (binding):
  ```ts
  export function StepHeader(props: { steps: React.ReactNode[]; current: number; label: string }): React.ReactElement;
  ```
  An ordered list (`aria-label={label}`) of numbered steps: done steps show a check in a `size-5 rounded-full bg-primary text-primary-foreground` dot, the current step a `ring-1 ring-primary` numbered dot with `aria-current="step"`, later steps a muted numbered dot; step names `text-xs`, hairline connectors between steps; wraps to a vertical list below `sm` only if it does not fit.
- Enroll: titles and descriptions per intent as today (`text-2xl` title through `PublicStep`); username/display name as coss `Field`s; the fixed username as a read-only mono value; the verified identity as a muted bar with a `size-8` avatar ("Verified as {name} by {provider}"); actions: "Create a passkey" (`default`, full width; disabled with an explanatory muted line beneath — not a tooltip — when passkeys are unsupported), "Use a password and an authenticator instead" (`outline`), then `OrSeparator label="or continue with"` and `ProviderGrid` when providers exist; a bound invite shows only its provider (`ProviderGrid` with one provider gives the full-width button).
- PasswordTotpSetup: `StepHeader` with "Password", "Authenticator", "Recovery codes" above the step's title; steps' content, validation, `bad_credentials` handling, the no-going-back recovery step and the username-error jump back are unchanged; `TotpSetup` and `RecoveryCodes` from Task 6.
- [ ] **Step 1: Write failing tests:** StepHeader marks the current step `aria-current="step"` and the earlier ones as done. Enroll/PasswordTotpSetup: the step header shows the right current step on each step; passkey unsupported shows the explanatory line; existing tests keep passing with updated queries.
- [ ] **Step 2: Run** the test files; red on StepHeader and new structure.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** tests, tsc, Biome, i18n:check. Screenshots: invite (open, 3 providers), fixed username + passkey only, bound to a provider, verified identity, reset, bootstrap, password step, authenticator step, recovery codes step, expired.
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild enrollment on the public stage`

---

### Task 9: Pairing, welcome and setup-signin

Research: §2, §6, §7.

**Files:**
- Modify: `dashboard/src/pages/public/Pair.tsx`, `Pair.test.tsx`, `Welcome.tsx`, `Welcome.test.tsx`, `SetupSignin.tsx`, `SetupSignin.test.tsx`, `components/custom/QrCode.tsx` (only if it needs coss styling)

**Interfaces:**
- Pair: the code in a well (`rounded-xl bg-muted/60 ring-1 ring-border/60`) as `font-mono text-3xl tracking-widest` with its screen-reader spelling, the 200px QR beside it from `sm` (stacked below `sm`), the hint line, the status line with a coss `Spinner` and the live countdown; "Stay signed in on this device" in the footer strip (hidden when expired, as today); starting state uses coss `Skeleton`s of the same sizes; expired dims the code and QR and shows a warning `Alert` and "Get a new code"; signed-in step keeps "Add a passkey" / "Not now" and the timed-out fallback; "Back to sign in" as a ghost button with `ArrowLeft` at the start of the actions. Polling, countdown and completion behaviour unchanged.
- Welcome: account preview as a `rounded-xl bg-muted/60 ring-1 ring-border/60 p-4` well with `EntityAvatar shape="round" size="xl"`, display name `font-medium wrap-anywhere`, `@username` in mono and the optional email; actions "Continue" (`default`) and "This isn't me" (`ghost`).
- SetupSignin: actions "Add a passkey" (`default`, `Fingerprint`), "Use a password and an authenticator instead" (`outline`), "Skip for now" (`ghost`), the footnote; the shared `PasswordTotpSetup` flow; timed-out fallback unchanged.
- [ ] **Step 1: Update tests first** to the new queries; add: Pair's remember checkbox sits in the stage footer and is hidden once expired; Welcome's preview shows `@username` and the email when present.
- [ ] **Step 2: Run** the three test files.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** tests, tsc, Biome, i18n:check. Screenshots: pair starting, pending, expired, signed-in (`mock.writes=true` where the research says writes are needed); welcome with and without avatar; setup-signin.
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild pairing and first sign-in steps on the public stage`

---

### Task 10: Federation flow, VRChat proof and require-identity

Research: §8, §9, §10.

**Files:**
- Modify: `dashboard/src/pages/public/FederationFlow.tsx`, `FederationFlow.test.tsx`, `VrchatProof.tsx`, `VrchatProof.test.tsx`, `RequireIdentity.tsx`, `RequireIdentity.test.tsx`

**Interfaces:**
- FederationFlow: header strip `AppStrip lead=… name="VRChat"` is **not** used (VRChat is the provider, not the app); title per intent; identify step: a numbered list (`ol` with `size-5` numbered dots, same dot style as `StepHeader`'s later steps) with the external link, the mono profile field and its warning description, "Continue"; proof step: profile row (label + mono `usr_…` + "Open profile" link), the verification link as `CopyValue`, the three numbered steps, the expiry line, the optional username field, "Verify"; verified step as today. Error handling unchanged.
- VrchatProof: `PublicStep` with the two paragraphs, no actions.
- RequireIdentity: header strip `AppStrip lead="Requested by" name={client.displayName} iconUrl={client.iconUrl}`; title "{app} needs to verify your identity"; providers as a `ScopeRows`-styled panel where each row is provider tile (`EntityAvatar size="md"`) + name + `outline sm` "Verify" button (aria "Verify with {name}"); the sudo step, satisfied auto-return, mismatch state and "Not now, return to {app}" unchanged.
- [ ] **Step 1: Update tests first** to new queries; keep every behavioural assertion; add: require-identity's app strip shows the app name.
- [ ] **Step 2: Run** the three test files.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** tests, tsc, Biome, i18n:check. Screenshots: flow identify, flow proof with username field, VRChat proof page, require-identity with 1 and 3 providers, mismatch, expired.
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild federation and identity checks on the public stage`

---

### Task 11: Maintenance and the error landing

Research: §11, §12.

**Files:**
- Modify: `dashboard/src/pages/public/Maintenance.tsx`, `Maintenance.test.tsx`, `ErrorLanding.tsx`, `ErrorLanding.test.tsx`

**Interfaces:**
- Maintenance: title "{instance} is under maintenance", description, the admin message in a neutral coss `Alert` (unlocalised text), actions "Try again" (`default`), "Administrator sign-in" or "Sign out" (`ghost`); 180s polling unchanged.
- ErrorLanding: title rules unchanged; message in a destructive coss `Alert`; the denied-retry line; the reference as `Reference` + mono value with `select-all`; exactly one action chosen by the existing rules, as an `outline` button. When the landing names an app (`app` parameter) the header strip shows `AppStrip lead="Signing in to" name={app}` (text only; it is attacker-writable, so no host/icon is fetched).
- [ ] **Step 1: Update tests first** to new queries; add: the app strip appears only with an `app` parameter.
- [ ] **Step 2: Run** the two test files.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** tests, tsc, Biome, i18n:check. Screenshots: maintenance signed out and signed in (zh), error denied with a long app name, federation error, SAML unknown signed in, generic, not-found.
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild maintenance and error landing on the public stage`

---

### Task 12: One report per public failure

Research: §0 item 5 ("Many pages also render an inline alert for the same failure") and the spec's found defect "Failures shown twice (inline alert and toast)".

**Files:**
- Modify: the public pages' mutations that render their failure inline (`pages/Login.tsx`, `pages/public/Pair.tsx`, `Enroll.tsx`, `enroll/PasswordTotpSetup.tsx`, `SetupSignin.tsx`, `FederationFlow.tsx`, `RequireIdentity.tsx`, `Welcome.tsx`, `Consent.tsx`, `SamlConsent.tsx` — check each) and their tests; the toast policy in `dashboard/src/api` / `AppNotifications.tsx` only if a per-mutation opt-out does not already exist (the query side already has `meta.quiet`)

**Interfaces:**
- Rule: a failure the page shows inline (alert or field error) does not also raise a toast; a failure with no inline place keeps its toast. Express it with a mutation `meta` flag mirroring the existing `quiet` read flag; no other toast behaviour changes.
- [ ] **Step 1: Write failing tests:** for login password failure, enroll username taken, pair start failure (which has no inline place — keeps its toast) and federation flow identify error: exactly one report (inline xor toast). Expected red: the inline cases also toast.
- [ ] **Step 2: Run** the affected test files.
- [ ] **Step 3: Implement** the flag and mark each inline-reported mutation.
- [ ] **Step 4: Run** the affected tests, tsc, Biome.
- [ ] **Step 5: Commit** — `fix(dashboard): report a public failure once`

---

## Phase end (controller)

`mise run ci:frontend` once; `go test ./pkg/...` for the backend task's packages; the browser matrix (1440×900, 1920×1080, 390×844 × light, dark × `en`, `zh`) across every public route above, reviewed at 2×; then the whole-phase review and the branch question.
