# coss redesign — Phase 1: foundation, shared layer and console shell

> **For agentic workers:** REQUIRED SUB-SKILL: Use minipowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace HeroUI's styling and the shared component layer (`dashboard/src/components/custom`) with coss ui, and rebuild the console shell, so every later phase composes pages from coss-based shared components.

**Architecture:** coss registry components are copied into `dashboard/src/components/ui` (source, not a package). The coss stylesheet replaces `@heroui/styles` and the HeroUI theme in one step; HeroUI components that pages still import keep working unstyled until their phase migrates them. Shared components in `src/components/custom` keep their exported names and props so pages do not change in this phase; their internals move to coss. New layout patterns from the approved mock are added beside them.

**Tech Stack:** React 19, TanStack Router/Query/Form, Lingui 6 (`en`, `zh`), Tailwind CSS 4.3, `@base-ui/react` 1.9.0, coss ui registry (copied from `github.com/cosscom/ui` `apps/ui/registry/default`), `class-variance-authority`, `tailwind-merge`, `clsx`, `lucide-react`, Vitest 5 + Testing Library, Biome 2.5.

**Spec:** `docs/superpowers/specs/2026-10-18-coss-dashboard-redesign-design.md`.
**Approved mock (reference implementation of the look):** `.impeccable/mocks/coss-sandbox/` — run `pnpm install && pnpm exec vite` there and open `#oidc`, `#idp`, `#group`, `#security`, `#signin`. Its `src/shell.tsx`, `src/seg.tsx` and `src/scenes/*.tsx` show exactly how the coss components are composed; copy composition from there, not from memory.

## Global Constraints

- All work is in the worktree `/home/tundra/projects/tundra/prohibitorum-worktrees/develop-coss`, branch `develop-coss`. Dashboard commands run in `dashboard/`.
- Install with `pnpm install --frozen-lockfile` after a lockfile change has been committed by the task that changed it; dependencies are pinned to exact versions (no `^`), matching the existing `package.json` style.
- Gate per task: `pnpm exec vitest run <touched test files>`, `pnpm exec biome check <touched files>`, `pnpm exec tsc --noEmit`. The full `mise run ci:frontend` (Biome, Lingui strict extract/compile, Vitest, build) runs once at the end of each task before its commit.
- No new HeroUI import anywhere. A file this plan rewrites must end with zero imports from `@heroui/react`, `@heroui/styles`, `react-aria-components` and `tailwind-variants`.
- Exported names and prop types of every rewritten file in `src/components/custom` stay the same unless a task's Interfaces block says otherwise, so page files compile unchanged. React-Aria-style props that pages pass today (`onPress`, `isDisabled`, `isPending`, `isRequired`, `isInvalid`, `isReadOnly`) remain accepted by the shared components and are translated inside them.
- coss components are used as published: do not restyle a coss primitive's base classes; compose and pass `className` for layout only. Layers follow the spec's material table (canvas, inset panel, `Frame`, `FramePanel`, well). No gradients, no decorative elements.
- Fonts are same-origin files (production CSP forbids inlined and third-party fonts): `Cal Sans` variable and `Paper Mono` from the coss repo, imported through CSS `@font-face` with `url()` so Vite emits them as hashed assets.
- Dark mode follows coss: the `.dark` class on `<html>` (coss declares `@custom-variant dark (&:is(.dark *))`). `data-theme` keeps being written for anything that still reads it.
- User-facing strings stay Lingui messages with explicit ids; new strings get ids under the component's existing namespace and both `en` and `zh` catalog entries (run `pnpm run i18n:extract` and fill `src/locales/zh/messages.po`).
- Tests find elements by role, name, label and `data-*` state; never by coss or HeroUI class names.
- Commit messages: conventional prefix, imperative, and end with the implementing agent's own truthful `Co-Authored-By: <Model name> <email>` trailer (repository rule in `AGENTS.md`).

---

### Task 1: coss foundation (registry, tokens, fonts, dark class)

**Files:**
- Create: `dashboard/src/components/ui/*.tsx` — every file of the coss registry's `apps/ui/registry/default/ui/` except `composer.tsx`
- Create: `dashboard/src/lib/utils.ts`, `dashboard/src/lib/segmented-control.ts` (from the registry's `lib/`)
- Create: `dashboard/src/hooks/use-media-query.ts`, `dashboard/src/hooks/use-copy-to-clipboard.ts` (from the registry's `hooks/`)
- Create: `dashboard/src/styles/coss.css` (from coss `packages/ui/src/styles/globals.css`)
- Create: `dashboard/src/styles/fonts/CalSansVF.woff2`, `dashboard/src/styles/fonts/PaperMono-Regular.woff2`
- Modify: `dashboard/src/styles/index.css`, delete `dashboard/src/styles/theme.css`
- Modify: `dashboard/src/main.tsx` (drop `@fontsource-variable/inter`)
- Modify: `dashboard/src/app/AppEnvironment.tsx`, `dashboard/src/app/AppEnvironment.test.tsx`
- Modify: `dashboard/index.html` (body classes)
- Modify: `dashboard/package.json`, `dashboard/pnpm-lock.yaml`, `dashboard/biome.json` (only if Step 3 needs it)
- Modify: `AGENTS.md` (section *Dashboard and generated assets*), `dashboard/AGENTS.md` (opening note and *Consistency comes first*): replace the HeroUI-theme rules this task makes false — the Theme Builder configuration and `theme.css` location, "preserve HeroUI default component styles", "read the HeroUI docs" — with: new and migrated UI uses the coss components in `src/components/ui` (vendored, updated only by re-copying) composed per `docs/superpowers/specs/2026-10-18-coss-dashboard-redesign-design.md`; files not yet migrated may still import HeroUI until the removal phase. Leave every rule about routing, data, forms, i18n and API untouched.

**Interfaces:**
- Produces: `cn(...inputs: ClassValue[]): string` from `@/lib/utils`; `segmentedControlRootClassName`, `segmentedControlItemVariants` from `@/lib/segmented-control`; every coss component under `@/components/ui/<name>` with the registry's exports unchanged.
- Produces: CSS custom properties `--font-sans` (`"Cal Sans", "Noto Sans SC", ui-sans-serif, system-ui, sans-serif`) and `--font-mono` (`"Paper Mono", ui-monospace, Menlo, monospace`) on `:root`.

- [ ] **Step 1: Write failing tests** in `AppEnvironment.test.tsx`:
  - `adds the dark class for the dark theme`: with `themeAtom` set to `dark`, `document.documentElement.classList.contains("dark")` is true and `dataset.theme` is `"dark"`. Fails today because only `data-theme` is written.
  - `removes the dark class for the light theme`: switching to `light` removes it.
  - `follows the system preference`: with `system` and a mocked `matchMedia` reporting dark, the class is present; flipping the mock's `change` listener to light removes it.
- [ ] **Step 2: Run them; they fail on the missing class.** `pnpm exec vitest run src/app/AppEnvironment.test.tsx`
- [ ] **Step 3: Implement.**
  - Add exact-pinned dependencies `@base-ui/react@1.9.0`, `tailwind-merge@3.4.0`, `clsx@2.1.1` (the versions the coss registry is built against). Remove `@heroui/styles` and `@fontsource-variable/inter`. Keep `@heroui/react`, `react-aria`, `react-aria-components`, `overlayscrollbars*`, `tailwind-variants` for now: unmigrated files still import them.
  - Source: clone `https://github.com/cosscom/ui` at commit `a597c6306a1c60e9f11e22e38bdf4a15f624b0f2` (the commit the approved mock was built from) into a scratch directory outside the repo. The mock's `.impeccable/mocks/coss-sandbox/src/components/ui` holds the same files with imports already rewritten and may be copied instead; either way the README records that commit.
  - Copy the registry files and rewrite their imports: `@/registry/default/ui/` → `@/components/ui/`, `@/registry/default/lib/` → `@/lib/`, `@/registry/default/hooks/` → `@/hooks/`. Remove `"use client";` lines (no RSC here). Keep the AGPL/MIT notices that the coss repo's `LICENSING.md` applies: add `dashboard/src/components/ui/README.md` stating the source path, the commit copied (`git -C <coss clone> rev-parse HEAD`), and that the files are vendored and updated only by re-copying.
  - `coss.css`: copy `globals.css`; replace its two `@source` lines with `@source "../**/*.{ts,tsx}";` so Tailwind scans `src`. Keep everything else verbatim.
  - `index.css`: `@import "./coss.css";` replaces `tailwindcss`, `@heroui/styles` and `./theme.css`; add the two `@font-face` rules (`font-weight: 300 800` for Cal Sans; `font-display: swap`) and `:root { --font-sans: …; --font-mono: …; }`; keep the OverlayScrollbars import and the `.os-theme-app`, viewport and table-pinned rules that the file already owns, restating colours with coss tokens (`--border`, `--muted-foreground`) where they referenced HeroUI tokens.
  - `index.html` body classes become `bg-sidebar text-foreground antialiased` (the coss canvas).
  - `AppEnvironment`: wherever `dataset.theme` is written, also `classList.toggle("dark", resolved === "dark")`.
  - Run `pnpm exec biome check --write src/components/ui src/lib src/hooks`. If recommended rules still fail on vendored files, add one `overrides` entry in `biome.json` for `src/components/ui/**` disabling only the failing rule names; do not edit vendored logic.
- [ ] **Step 4: Run this task's tests, `tsc --noEmit`, Biome on touched files, then `mise run ci:frontend`; all green.** Open `pnpm dev` with `?mock` and confirm the console renders in Cal Sans on the coss canvas (unmigrated HeroUI parts may look unstyled; that is expected).
- [ ] **Step 5: Commit** — `feat(dashboard): add coss ui foundation and replace HeroUI styles`

### Task 2: Button on coss

**Files:**
- Modify: `dashboard/src/components/custom/Button.tsx`, `Button.test.tsx`
- Modify: the class-name assertions on buttons in `components/custom/SudoDialog.test.tsx` and `pages/Login.test.tsx`

**Interfaces:**
- Produces (binding):
  ```ts
  type ButtonVariant = "primary" | "secondary" | "tertiary" | "ghost" | "outline" | "danger" | "danger-soft" | "warning";
  type ButtonSize = "sm" | "md" | "lg";
  interface ButtonProps extends Omit<React.ComponentProps<"button">, "onClick" | "disabled"> {
    variant?: ButtonVariant;            // default "primary"
    size?: ButtonSize;                  // default "md"
    isIconOnly?: boolean;
    isPending?: boolean;
    isDisabled?: boolean;
    fullWidth?: boolean;
    onPress?: (event: React.MouseEvent<HTMLButtonElement>) => void;
    onClick?: (event: React.MouseEvent<HTMLButtonElement>) => void;
    render?: React.ReactElement;        // coss `useRender` slot, e.g. a router Link
  }
  export function Button(props: ButtonProps): React.ReactElement;
  ```
- Mapping to coss `Button` (`@/components/ui/button`): `primary`→`default`, `secondary`→`outline`, `outline`→`outline`, `tertiary`→`secondary`, `ghost`→`ghost`, `danger`→`destructive`, `danger-soft`→`destructive-outline`, `warning`→`default`. Sizes: `sm`→`sm` (`icon-sm` when icon-only), `md`→`default` (`icon`), `lg`→`lg` (`icon-lg`). `warning` keeps the primary look because the consequence is stated by the section footer (spec, *Consequential save*), not by button colour.
- The rendered element carries `data-variant="<ButtonVariant>"` and `data-size="<ButtonSize>"`, and `aria-busy="true"` while pending.

- [ ] **Step 1: Write failing tests** (replace class assertions in `Button.test.tsx`):
  - `maps each variant` — for each `ButtonVariant`, renders a button with `data-variant` equal to it. Fails: attribute absent.
  - `pending keeps the label beside the spinner` — `isPending` with text: the accessible name still contains the text, `aria-busy="true"`, the button is disabled, a `role="status"` or `data-slot="spinner"` element is present. Icon-only + pending: the icon is not rendered, the spinner is.
  - `isDisabled disables and onPress does not fire`.
  - `onPress fires on click and on Enter/Space` (native button semantics).
  - `render renders the given element` — `render={<a href="/x" />}` produces a link with the button's classes and `data-variant`.
  - In `SudoDialog.test.tsx` and `Login.test.tsx`, replace `toHaveClass("button--primary")` style assertions with `toHaveAttribute("data-variant", "primary")` (and `secondary` respectively).
- [ ] **Step 2: Run them; they fail on the missing attributes.** `pnpm exec vitest run src/components/custom/Button.test.tsx src/components/custom/SudoDialog.test.tsx src/pages/Login.test.tsx`
- [ ] **Step 3: Implement** the interface on top of coss `Button`. Pending uses coss `loading` (which already draws `Spinner` and hides the label visually) except that this project's rule is "a button with text keeps that text beside the spinner": pass `loading={false}`, render `<Spinner />` before the children when `isPending`, set `disabled` and `aria-busy`. `fullWidth` adds `w-full`.
- [ ] **Step 4: Run the three test files, Biome, `tsc`, then `mise run ci:frontend`.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild Button on coss`

### Task 3: Text-entry form fields on coss

**Files:**
- Modify: `components/custom/FormField.tsx`, `FormFields.tsx` (`TextAreaField`, `SwitchField`, `NumberField`, `ReadOnlyField`), `FormMessages.tsx`, `FormError.tsx`, `OtpField.tsx`, `CopyValue.tsx`, `SubmitButton.tsx`
- Test: `components/custom/CopyValue.test.tsx`, `src/forms/forms.test.tsx`, `components/custom/TotpSetup.test.tsx` (OTP slot selector), new `components/custom/FormField.test.tsx`

**Interfaces:**
- Consumes: `Button` (Task 2); coss `Field`, `FieldLabel`, `FieldDescription`, `FieldError`, `Input`, `InputGroup*`, `Textarea`, `Switch`, `NumberField*`, `OTPField`, `OTPFieldInput`, `OTPFieldSeparator`.
- Produces: the same exports and props as today (read each file's current props before editing). Field components keep: label, description, error text from `FormMessages`, `isRequired`, `isDisabled` (also disabled while the form submits), `isReadOnly`, `autoComplete`, `inputMode`, and the `onSurface` / `variant="secondary"` prop — which becomes a no-op kept for compatibility (coss inputs read correctly on a `FramePanel`). `CopyValue` keeps `label`, `value`, optional description and the copy button's accessible name.

- [ ] **Step 1: Write failing tests:**
  - `FormField.test.tsx` — label is associated with the input (`getByLabelText`); description is the input's `aria-describedby`; a field error sets `aria-invalid="true"` and is announced via `FieldError`; disabled while submitting. Fails today only on the parts HeroUI wires differently (record which assertions fail).
  - `CopyValue.test.tsx` — replace the `.input-group` selector assertion: the value is in a read-only textbox with the given label; clicking "Copy" writes to `navigator.clipboard` (mocked) and the button's name changes to "Copied" for its timeout.
  - `TotpSetup.test.tsx` — replace `data-slot="input-otp-slot"` queries with role queries on the OTP inputs (coss `OTPFieldInput` renders one input per slot).
  - `forms.test.tsx` — existing behaviour (focus moves to the first invalid field after a failed submit; duplicate submits blocked) still passes.
- [ ] **Step 2: Run them; the changed assertions fail on the old markup.**
- [ ] **Step 3: Implement** each component on coss primitives, keeping props. `NumberField` uses coss `NumberField` (with its group, decrement, input, increment). `ReadOnlyField` is a read-only `Input`. `SwitchField` is a `SettingRow`-shaped label + description on the left and coss `Switch` on the right (the same layout Task 7's `SettingRow` uses; inline the markup here, Task 7 extracts it). `OtpField` keeps its length and separator position (after the third digit for six digits).
- [ ] **Step 4: Run touched tests, Biome, `tsc`, then `mise run ci:frontend`.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild text form fields on coss`

### Task 4: Choice fields and pickers on coss

**Files:**
- Modify: `components/custom/OptionSelectField.tsx`, `RememberCheckbox.tsx`, `ChoiceTile.tsx`, `EntityPicker.tsx`
- Test: `components/custom/EntityPicker.test.tsx` (replace `data-slot="autocomplete"` queries), new `components/custom/OptionSelectField.test.tsx`

**Interfaces:**
- Consumes: coss `Select*`, `Checkbox`, `RadioGroup`, `Radio`, `Label`, `Combobox*` (coss's select-with-search primitive; `Autocomplete` is for free text with suggestions, which these pickers are not), `Spinner`, `Empty`.
- Produces: unchanged exports `SelectOption`, `OptionSelectField`, `RememberCheckbox`, `ChoiceTile`, `EntityOption`, `EntityPicker`, `AccountPicker`, `GroupPicker`, `IdentityProviderPicker` with today's props.
- `OptionSelectField` options keep a title and an explanation; the explanation renders as a second muted line inside each item, and the selected item's explanation is shown as the field description.
- `ChoiceTile` becomes the described radio card from the approved `#idp` scene (`Label` around `Radio` with title and muted description; checked state `has-data-checked:border-primary/48 has-data-checked:bg-accent/50`).

- [ ] **Step 1: Write failing tests:** `OptionSelectField.test.tsx` — opening the select lists every option with its explanation; choosing one updates the form value and the description; keyboard (Arrow, Enter) works. `EntityPicker.test.tsx` — rewrite slot selectors to `getByRole("combobox")` / `getByRole("option")`; searching filters; a disabled candidate shows its reason and cannot be chosen; the empty result shows the empty message.
- [ ] **Step 2: Run them; failures are the rewritten queries.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run touched tests, Biome, `tsc`, then `mise run ci:frontend`.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild choice fields and pickers on coss`

### Task 5: Notices, dialogs and toasts on coss

**Files:**
- Modify: `components/custom/SurfaceAlert.tsx`, `ConfirmDialog.tsx`, `AppNotifications.tsx`, `RequestDetailsDialog.tsx`, `SecretReveal.tsx`, `SudoDialog.tsx`
- Test: `ConfirmDialog.test.tsx` (replace `alert-dialog__icon--*`), `AppNotifications.test.tsx` (replace `toast--success`), `SudoDialog.test.tsx`, new `SecretReveal.test.tsx`

**Interfaces:**
- Consumes: `Button` (Task 2), Task 3 fields; coss `Alert*`, `AlertDialog*`, `Dialog*`, `Toast*` (`toastManager` / provider as the coss toast docs define), `Checkbox`.
- Produces: unchanged exports: `SurfaceAlert`, `ConfirmDialog`, `notificationQueue`, `notifyError`, `notifySuccess`, `AppNotifications`, `RequestDetailsDialog`, `SecretReveal`, `SudoDialog`. `ConfirmDialog` keeps its `tone` prop (`danger`, `warning`, default) and exposes it as `data-tone` on the popup. Toasts expose `data-type` (`success`, `error`). `SecretReveal` keeps `onSurface` and `inDialog` (both become layout-only: `inDialog` still moves the title into the caller's header and the continue control into its footer).
- `notifyError` / `notifySuccess` keep their signatures; `notificationQueue` stays the object other modules import (wrap the coss toast manager behind it).

- [ ] **Step 1: Write failing tests:** ConfirmDialog — each tone sets `data-tone`; confirm runs the action, shows pending on the confirm button, closes when it settles; Escape cancels. AppNotifications — success and error toasts appear with `data-type`, the error toast's "Details" opens `RequestDetailsDialog`. SudoDialog — keep the existing behavioural tests, change only class assertions. SecretReveal — the secret is shown once in a read-only field, copy and download work, continue is disabled until the "I saved it" checkbox is ticked, the dialog cannot be dismissed by Escape or the backdrop.
- [ ] **Step 2: Run them; failures are the replaced assertions and the new file.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run touched tests, Biome, `tsc`, then `mise run ci:frontend`.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild notices, dialogs and toasts on coss`

### Task 6: Sections and lists on coss frames

**Files:**
- Modify: `components/custom/Section.tsx` (`Section`, `AsyncSection`), `ConsoleCard.tsx`, `ItemList.tsx` (`ItemList`, `ItemListRow`), `DangerZone.tsx` (`DangerZone`, `DangerZoneTitle`), `Detail.tsx`, `ConsoleTabs.tsx` (`ConsoleTab`, `ConsoleTabs`)
- Test: `Section.test.tsx`, `ItemList.test.tsx` (replace `text-muted` / `text-foreground` assertions), `ConsoleTabs.test.tsx`

**Interfaces:**
- Consumes: coss `Frame`, `FrameHeader`, `FrameTitle`, `FrameDescription`, `FramePanel`, `FrameFooter`, `Tabs*`, `Skeleton`.
- Produces: unchanged exports with these additions to `Section` and `AsyncSection` props (binding):
  ```ts
  id?: string;                 // anchor for OutlineRail (Task 7); defaults to a slug of the title
  footer?: React.ReactNode;    // rendered in FrameFooter on the muted band
  dirty?: boolean;             // registers with SectionRegistry (Task 7); no visual effect here
  ```
  `Section` renders: `Frame` > `FrameHeader` (title as `h2` via `FrameTitle`, description, header action aligned end) > `FramePanel` (children) > optional `FrameFooter`. `ItemList` renders its rows inside one `FramePanel` with `divide-y` and `p-0`. `DangerZone` renders a `Frame` tinted `bg-destructive/6` whose rows are title + consequence + action. `ConsoleTabs` keeps URL-driven tabs and per-tab boundaries; the list is coss `TabsList` with the underline variant.
- [ ] **Step 1: Write failing tests:** Section — title is a level-2 heading, header action is inside the section's header region, `footer` renders after the panel, `id` is on the section element; AsyncSection still shows the retry state on error. ItemList — muted secondary text is asserted via `data-slot` or role instead of classes. ConsoleTabs — selecting a tab navigates with `replace: true` and the active tab has `aria-selected="true"`.
- [ ] **Step 2: Run them; failures are new props and replaced assertions.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run touched tests, Biome, `tsc`, then `mise run ci:frontend`.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild sections and lists on coss frames`

### Task 7: Object-page patterns (header, outline, unsaved bar, setting row, value set)

**Files:**
- Create: `components/custom/ObjectHeader.tsx`, `OutlineRail.tsx`, `SectionRegistry.tsx`, `UnsavedBar.tsx`, `SettingRow.tsx`, `ValueSet.tsx`
- Modify: `components/custom/TagListField.tsx` (render through `ValueSet`), `components/custom/Section.tsx` (register with `SectionRegistry`)
- Test: `ObjectHeader.test.tsx`, `OutlineRail.test.tsx`, `UnsavedBar.test.tsx`, `ValueSet.test.tsx`, `TagListField.test.tsx` (updated)

**Interfaces (binding):**
```ts
// ObjectHeader.tsx
type ObjectStatus = "enabled" | "disabled" | "restricted" | "open" | "sign-in-on" | "sign-in-off";
interface ObjectHeaderProps {
  icon: React.ReactNode;              // the entity's icon tile (EntityAvatar size "lg")
  title: React.ReactNode;             // rendered as the page h1
  statuses?: ObjectStatus[];          // rendered as coss Badges, in this order
  identifier?: { value: string; label: string };   // mono, with a copy button named by label
  facts?: React.ReactNode[];          // short facts separated by hairlines
  actions?: React.ReactNode;          // aligned end
}
export function ObjectHeader(props: ObjectHeaderProps): React.ReactElement;

// SectionRegistry.tsx
interface RegisteredSection { id: string; title: string; dirty: boolean; tone?: "danger" }
export function SectionRegistryProvider(props: { children: React.ReactNode }): React.ReactElement;
export function useRegisterSection(section: RegisteredSection): void;
export function useRegisteredSections(): RegisteredSection[];   // document order

// OutlineRail.tsx
export function OutlineRail(props: { facts?: React.ReactNode }): React.ReactElement;
// lists useRegisteredSections() as in-page links; dirty sections show a warning dot
// with an accessible "Unsaved changes" label; tone "danger" renders last in destructive colour;
// the link of the section nearest the top of the viewport has aria-current="location".

// UnsavedBar.tsx
export function UnsavedBar(): React.ReactElement | null;
// visible when some registered section is dirty and that section's element is not
// intersecting the viewport; text "Unsaved changes in {title}" (Lingui), button
// "Jump to it" scrolls to and focuses the section heading.

// SettingRow.tsx
export function SettingRow(props: { title: React.ReactNode; description?: React.ReactNode; children: React.ReactNode }): React.ReactElement;

// ValueSet.tsx
interface ValueSetProps {
  values: string[];
  onRemove: (value: string) => void;
  onAdd: (value: string) => void;          // called with the trimmed draft
  addLabel: string;                        // "Add scope", "Add domain"
  placeholder: string;
  emptyText?: string;                      // shown when values is empty, e.g. "Every domain is accepted."
  suggestions?: { value: string; description: string }[];  // only when the source publishes them
  noSuggestionsNote?: string;              // shown in the popover when suggestions is undefined
  validate?: (draft: string) => string | undefined;        // message shown under the popover input
  isDisabled?: boolean;
}
export function ValueSet(props: ValueSetProps): React.ReactElement;
```
- `ValueSet` look is the `#idp` scene's `ValueSet`: raised mono chips (`h-7 rounded-md border bg-background shadow-xs/5` with the coss 1px bottom bevel), a remove button inside each chip named "Remove {value}", a dashed "Add …" chip that opens a coss `Popover` (`side="bottom" align="end"`) holding an `Input` and an "Add" `Button`, plus suggestions when given. Enter in the input adds; Escape closes.
- `TagListField` keeps its form value `{ tags: string[]; draft: string }` and its `check` prop; the popover input is bound to `draft` so the existing "refuse text left in the input on submit" rule still works.

- [ ] **Step 1: Write failing tests:**
  - ObjectHeader — the title is the only `h1`; statuses render in order with their translated names; the identifier copy button is named by `label`.
  - OutlineRail + SectionRegistry — three `Section`s with ids render three links in order; marking one `dirty` adds an "Unsaved changes" indicator to its link; a `tone: "danger"` section is listed last.
  - UnsavedBar — hidden when nothing is dirty; with a dirty section and `IntersectionObserver` mocked to report it off-screen, the bar shows its title; "Jump to it" calls `scrollIntoView` on the section and focuses its heading; when the section intersects, the bar hides.
  - ValueSet — renders one chip per value; remove calls `onRemove`; adding via the popover with Enter calls `onAdd` with the trimmed value and clears the input; `validate` messages block the add; `emptyText` shows only for an empty list; suggestions list renders only when provided, otherwise `noSuggestionsNote` shows.
  - TagListField — existing behaviour still holds (`check` errors, submit refusing an unsent draft) with the new markup.
- [ ] **Step 2: Run them; they fail on the missing modules.**
- [ ] **Step 3: Implement.** Use the mock's `src/scenes/idp.tsx` for the composition of `ValueSet`, `SettingRow` (`Row` there) and the outline and unsaved bar.
- [ ] **Step 4: Run touched tests, Biome, `tsc`, then `mise run ci:frontend`.**
- [ ] **Step 5: Commit** — `feat(dashboard): add object page patterns and value sets`

### Task 8: Tables and entity cells on coss

**Files:**
- Modify: `components/custom/DataTable.tsx` (`TableColumn`, `DataTable`), `TableActionCell.tsx`, `TableEmptyState.tsx`, `ListSkeleton.tsx`, `EntityCell.tsx`, `EntityAvatar.tsx`, `ListCells.tsx`, `RelativeTime.tsx`, `RowsField.tsx`, `JsonBlock.tsx`
- Modify: `src/styles/index.css` (pinned-column rules restated with coss tokens if Task 1 left HeroUI token names)
- Test: `DataTable.test.tsx` (replace `.bg-surface-secondary`), `EntityCell.test.tsx`, `RelativeTime.test.tsx`, `RowsField.test.tsx`, `JsonBlock.test.ts`

**Interfaces:**
- Consumes: coss `Table*` (with `variant="card"` inside a `Frame`), `Avatar*`, `Tooltip*`, `Empty*`, `Skeleton`, `ScrollArea`.
- Produces: unchanged exports and props. `DataTable` keeps sorting, expandable rows and the pinned action column; it renders inside a `Frame` whose header slot accepts the existing toolbar. `EntityAvatar` gains `size?: "sm" | "md" | "lg" | "xl"` (24, 32, 48, 56 px; default `md`), `rounded-lg` tiles for apps and providers, round for people, letter fallback from the name's first one or two letters.
- [ ] **Step 1: Write failing tests:** DataTable — header row is `rowgroup` > `row` > `columnheader` with `aria-sort` on the sorted column; the expanded row has `aria-expanded="true"` on its toggle; the pinned column cell has `data-pinned="end"` (replaces the class assertion). EntityAvatar — `xl` renders 56px and a two-letter fallback for "University SSO" (`US`). RelativeTime — tooltip shows the exact time on hover and focus. RowsField — unchanged behaviour assertions pass.
- [ ] **Step 2: Run them; failures are the replaced assertions and new size.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run touched tests, Biome, `tsc`, then `mise run ci:frontend`.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild tables and entity cells on coss`

### Task 9: Console shell on coss Sidebar

**Files:**
- Modify: `components/custom/ConsoleLayout.tsx`, `ConsoleLayout.test.tsx`, `ConsoleLayoutSections.test.ts`, `LanguageMenu.tsx`, `ThemeSelect.tsx`, `AppLayout.tsx`, `RouteFeedback.tsx` (console and whole-window variants only)
- Delete: `components/custom/ScrollArea.tsx`, `components/custom/PageScrollArea.tsx` if no file still imports them after this task; otherwise leave for the removal phase
- Modify: `app/AppEnvironment.tsx` (drop `PageScrollArea` if deleted)
- Test: `ConsoleLayout.test.tsx`, `RouteFeedback.test.tsx`

**Interfaces:**
- Consumes: coss `SidebarProvider`, `Sidebar` (`variant="inset"`), `SidebarHeader`, `SidebarContent`, `SidebarGroup`, `SidebarGroupLabel`, `SidebarMenu*`, `SidebarMenuBadge`, `SidebarFooter`, `SidebarInset`, `SidebarTrigger`, `Breadcrumb*`, `InputGroup*`, `Kbd`, `Menu*`, `Avatar*`; `SectionRegistryProvider` (Task 7).
- Produces: unchanged `ConsoleLayout` and `visibleManagementSections`; the nav table (sections, role rules, labels) is reused as is. The header shows a `Breadcrumb` whose last item is the current page title (detail pages set it through the existing title mechanism); the search field with `⌘K` is visual only in this phase (no command palette) and is omitted rather than shown inert — render it only when a later phase passes `onSearch`. Content is `mx-auto w-full max-w-6xl px-6 py-8 lg:px-10`, wrapped in `SectionRegistryProvider`. The account block (avatar, name, role) opens a coss `Menu` with Profile, Security, language, theme and Sign out. `LanguageMenu` and `ThemeSelect` keep their atoms and become coss `Menu` / segmented control.
- `RouteFeedback`: `ConsoleRouteError`, `ConsoleRouteNotFound`, `AppRouteError`, `AppRouteNotFound` render as a `Frame` in the content column with the existing single way out and "Details". The public variants are Phase 2.
- [ ] **Step 1: Write failing tests:** ConsoleLayout — a member sees only account entries; an app manager sees Applications with only the kinds they manage; an admin sees Directory, Applications and System; the current entry has `aria-current="page"` (coss `isActive`); counts render as badges; the account menu offers Sign out; below the `md` breakpoint (mock `matchMedia`) the trigger opens the sidebar as a sheet. RouteFeedback — console error shows its title, message and the one action chosen by `describeRouteFailure`, plus "Details" when a request is attached.
- [ ] **Step 2: Run them; failures are the new structure.**
- [ ] **Step 3: Implement** following the mock's `src/shell.tsx`.
- [ ] **Step 4: Run touched tests, Biome, `tsc`, then `mise run ci:frontend`. Then a browser pass on `pnpm dev` with `?mock&mock.session.role=admin` and `role=member`: 1440×900, 1920×1080 and 390×844, light and dark, `en` and `zh`; review the shell at 2×.**
- [ ] **Step 5: Commit** — `feat(dashboard): rebuild the console shell on coss`

---

## Later phases (separate plans, written at each phase's start)

Each later plan consumes the components above and follows the same gates. In order:

1. **Public flows** — `PublicStage`, `AppStrip`, `ProviderGrid`, `ScopeRows`, `StepHeader`; `PublicLayout`, `PublicCard`, `PublicStep`, `PageHeading`, `AppToolbar`, `InstanceIdentity`, `LoginShell`, login-appearance, `ProviderButtons`, `OrSeparator`, public `RouteFeedback`; pages under `pages/public` and `pages/Login.tsx`. Reference: `#signin`, `#consent`.
2. **Member pages** — Security (passkeys, password and authenticator with `StepHeader` dialogs, identities, tokens), Profile, Connected apps, Devices and pairing. Reference: `#security`.
3. **Applications** — OIDC, SAML, forward-auth detail and new pages with `ObjectHeader`, `OutlineRail`, consequential saves, `UriList`, `AppAccessPanel`. Reference: `#oidc`.
4. **Identity providers** — detail (all sections and Steam / VRChat variants), new, diagnostics drawer with `StageTimeline`. Reference: `#idp`, `#idp-test`.
5. **Groups** — rule builder and side column. Reference: `#group`.
6. **Users, invitations, logs, settings** — including the sign-in page editor with live preview. Reference: `#appearance`.
7. **Removal and conventions** — drop `@heroui/react`, `react-aria`, `react-aria-components`, `overlayscrollbars*`, `tailwind-variants`; rewrite `AGENTS.md`, `dashboard/AGENTS.md`; regenerate `DESIGN.md` from the built result.

Backend additions (spec §Backend additions) and found defects (spec §Found defects) are separate tasks scheduled alongside the phase that surfaces them: target-app host with Phase 1 of the later list, latest connection-test result with Identity providers, rule-editor and manager-mutation defects with Groups and Applications.
