# Color system redesign — Cool Slate: layered neutrals, full ramps, dark-mode twin

**Date:** 2026-06-18
**Status:** Design (approved in brainstorming)

## Goal

The shipped UI reads as "barely two colors" — pure white plus the Tide teal —
and feels flat and barebone. The root cause is not the hue count: it is that
the three neutral surfaces (`bg` 1.0, `surface` 0.985, `sunken` 0.965) sit
within ~3.5% lightness of each other and collapse into one flat plane, while
color only ever enters as the primary.

This redesign makes the color system serious without changing the brand
identity. Decisions taken during brainstorming:

- **Scope:** a ground-up color-system redesign — full tonal ramps, richer
  layered neutrals, a complete state palette, and a first-class dark-mode twin.
- **Brand backbone:** **Tide teal stays the sole anchor.** No second brand
  hue. Ember remains the scarce warm brand-mark spark.
- **Teal presence:** **accents only.** Teal carries controls, selection, and
  focus. The richness comes from a deep, layered neutral system + real
  elevation (the GitHub/Linear-light philosophy), not from ambient teal washes.
- **Neutral character:** **Cool Slate** — neutrals carry a faint *cool* cast
  (hue ~235–240, very low chroma). Never warm.
- **Info color:** uses the teal family (a teal wash distinct from the canvas),
  not a new foreign hue.
- **Dark mode:** a designed twin; OS `prefers-color-scheme` default + a user
  toggle; the dark primary button flips to a dark label on a brighter teal
  fill.

The bar is WCAG 2.2 AA in **both** themes (PRODUCT.md), verified by a contrast
script, not by eye.

## Non-goals

- Changing the Tide teal hue, adding a second brand color, or ambient teal
  surface washes (all explicitly ruled out).
- New components, layout changes, or a motion-system redesign (separate topic).
- Redesigning the threshold "Drenched" `AuthBackdrop` scenery. It stays; this
  work only ensures its card/header tokens resolve correctly in light and dark.
- Any feature/backend work.

---

## 1. The foundation: layered neutral surface stack

The single highest-impact change. Today the page is white (`1 0 0`) and cards
are a hair *grayer* (`0.985`), so cards **recede** and the separation is ~1.5%.
We invert this: the **canvas becomes the toned layer** and **cards become pure
white**, so cards, dialogs, and popovers **lift by tone alone** — preserving
the Flat-Until-It-Acts rule (no resting shadow needed).

All neutrals carry a faint cool cast (hue ~235–240, chroma ≤ 0.015). Warm is
forbidden.

### Neutral ramp (backing scale; light)

| Token | OKLCH | Primary use |
|---|---|---|
| `neutral-0` | `1 0 0` | card / surface (the white that lifts) |
| `neutral-25` | `0.985 0.005 235` | canvas (the page) |
| `neutral-50` | `0.975 0.006 235` | subtle inset / striped row |
| `neutral-100` | `0.965 0.008 235` | sunken: sidebar, toolbar, table header, well |
| `neutral-150` | `0.945 0.010 235` | hover-well: row hover, ghost-button hover |
| `neutral-200` | `0.910 0.010 235` | border (hairline divider, input stroke) |
| `neutral-300` | `0.850 0.011 235` | border-strong (dividers on tint, group outline) |
| `neutral-400` | `0.720 0.012 238` | disabled control stroke |
| `neutral-500` | `0.620 0.012 240` | faint text/icon (decorative, 3:1) |
| `neutral-600` | `0.480 0.013 240` | muted text, captions, placeholders (≥4.5:1) |
| `neutral-700` | `0.400 0.014 240` | secondary ink |
| `neutral-800` | `0.300 0.015 240` | strong secondary ink |
| `neutral-900` | `0.220 0.015 240` | ink: body + headings (~13:1 on canvas) |
| `neutral-950` | `0.160 0.012 240` | ink-strong: max-contrast headings / dark base |

### Semantic surface roles (light)

| Role | Token source | Used for |
|---|---|---|
| Canvas | `neutral-25` | the page behind everything |
| Surface | `neutral-0` | cards, panels, popovers, dialogs, inputs |
| Sunken | `neutral-100` | sidebar, toolbars, table headers, code/well fields |
| Hover-well | `neutral-150` | row hover, ghost-button hover, segmented track |
| Border | `neutral-200` | hairline dividers, input stroke |
| Border-strong | `neutral-300` | dividers on tinted surfaces, group outlines |
| Ink | `neutral-900` | body + headings |
| Ink-strong | `neutral-950` | optional max-contrast headings/numbers |
| Muted | `neutral-600` | secondary text — nudged from old 0.50 → **0.48** so it clears 4.5:1 on the *toned* canvas, not just on white |
| Faint | `neutral-500` | disabled text/icons only |

**Why it reads "designed":** three genuinely distinct planes (canvas / card /
sunken) + two border weights give the eye structure everywhere, before any
teal or state color is added.

---

## 2. Brand and state ramps

### Tide (teal) — hue 205, identity preserved

The two current values stay put as `500`/`600`; the ramp gives teal the range
to do tints, hovers, and selection instead of one flat fill.

| Step | OKLCH | Role |
|---|---|---|
| `tide-50` | `0.97 0.018 205` | selected-row / active-nav background, subtle wash |
| `tide-100` | `0.93 0.038 205` | teal badge bg, progress track, selected-count chip |
| `tide-200` | `0.87 0.065 205` | info-callout border |
| `tide-300` | `0.78 0.090 205` | |
| `tide-400` | `0.67 0.110 205` | |
| `tide-500` | `0.55 0.118 205` | **(current Tide)** fills, focus-ring tone, large text |
| `tide-600` | `0.47 0.130 205` | **(current Tide-Strong)** primary fill (white label), links, AA text |
| `tide-700` | `0.40 0.128 205` | hover/active on primary; info-callout text |
| `tide-800` | `0.33 0.105 205` | pressed |
| `tide-900` | `0.26 0.075 205` | |
| `tide-950` | `0.20 0.052 205` | |

**Application (accents-only):** primary fill `tide-600` → hover `tide-700` →
pressed `tide-800`, white label; links / primary text `tide-600`; focus ring
`tide-500` (3px halo); active nav / selected row = `tide-50` fill + `tide-700`
label + a weight bump and a leading marker (**never** a side-stripe); checked
controls `tide-500`/`tide-600`.

### State palettes

Rule for every chromatic role — **the `-500`/`-700` rule**: `-500` is the
fill/icon tone (always paired with text); `-700` is the text-weight tone that
clears 4.5:1 on its own.

| State | `-50` (tint bg) | `-100` | `-500` (fill/icon) | `-600` | `-700` (text, AA) |
|---|---|---|---|---|---|
| **Sage** (success / confirmed / session-active) | `0.95 0.035 150` | `0.90 0.060 150` | `0.62 0.130 150` *(current)* | `0.52 0.120 150` | `0.45 0.105 150` |
| **Amber** (caution / pending / expiring) | `0.96 0.045 80` | `0.92 0.075 80` | `0.76 0.140 75` *(current)* | `0.62 0.110 70` | `0.50 0.090 65` |
| **Rose** (danger / revoked / failed) | `0.95 0.035 20` | `0.90 0.060 20` | `0.58 0.180 22` *(current)* | `0.51 0.175 22` | `0.46 0.165 22` |

Amber is the AA-critical one: its `-500` (0.76) is **icon-only**; any amber
text uses `-700`. This fixes a latent bug — the current `StatusBadge` renders
`text-amber` (0.76) on a faint tint, well under AA.

### Info (teal-family, not a new hue)

A teal wash clearly stronger than the canvas so it reads as a deliberate panel,
yet distinct from a *selected row* (which uses the fainter `tide-50` and no
icon).

| Token | OKLCH | Use |
|---|---|---|
| `info-bg` | `0.95 0.030 205` | informational callout background |
| `info-border` | `0.87 0.065 205` (`tide-200`) | callout border |
| `info-icon` | `0.47 0.130 205` (`tide-600`) | leading info icon |
| `info-text` | `0.40 0.128 205` (`tide-700`) | callout text |

State-Has-a-Color holds for **credential/session** states (sage/amber/rose);
teal carries brand **and** info; Ember is brand-only.

### Ember (scarce warm accent) — hue 42

| Token | OKLCH | Use |
|---|---|---|
| `ember-500` | `0.70 0.150 42` *(current)* | brand mark, the one human moment |
| `ember-600` | `0.58 0.140 42` | rare welcome-headline text accent only |

Unchanged in spirit: no more than a couple of Ember elements per screen; never
a hover/state/decoration.

---

## 3. Elevation

Minimal and earned. With a toned canvas, white cards separate at rest by tone,
so Flat-Until-It-Acts is preserved. Two shadow tokens:

- `shadow-raised` (hover/active on interactive surfaces):
  `0 1px 2px oklch(0.22 0.015 240 / 0.06), 0 2px 8px oklch(0.22 0.015 240 / 0.07)`
- `shadow-overlay` (dialog/popover/menu/toast):
  `0 8px 32px oklch(0.16 0.012 240 / 0.18)` + a `border-strong` hairline

In dark mode, elevation reads through the **lighter card surface + border**
rather than shadow (dark shadows barely show); the overlay shadow becomes
`0 8px 32px oklch(0 0 0 / 0.5)`.

---

## 4. Dark-mode twin ("Cool Slate, dark")

Cool near-black (hue ~240, low chroma) with the surface logic **inverted**:
canvas is mid-dark, cards lift *lighter*, the sidebar recesses *darker*. Same
three-plane structure as light.

### Surface roles (dark)

| Role | OKLCH |
|---|---|
| Canvas (`--background`) | `0.170 0.012 240` |
| Surface / card / popover | `0.205 0.013 240` *(lifted)* |
| Sunken / sidebar | `0.145 0.011 240` *(recessed)* |
| Hover-well / muted bg | `0.245 0.013 240` |
| Border | `0.300 0.012 240` |
| Border-strong | `0.370 0.012 240` |
| Ink (`--foreground`) | `0.950 0.005 240` *(~14:1 on canvas)* |
| Muted text | `0.680 0.010 240` *(≥4.5:1 on canvas)* |
| Faint text | `0.500 0.010 240` *(disabled)* |

### Brand / state in dark (lightened to hold contrast)

| Role | OKLCH | Notes |
|---|---|---|
| Primary fill | `0.72 0.120 200` | **dark label** `0.170 0.012 240` (the flip) |
| Primary hover / pressed | `0.66 0.120 200` / `0.60 0.118 200` | |
| Link / primary text | `0.80 0.090 205` | AA on dark canvas |
| Focus ring | `0.72 0.110 205` | |
| Destructive fill | `0.64 0.175 22` | **dark label** `0.160 0.010 22` (same flip rationale) |
| Sage text / tint bg | `0.80 0.100 150` / `0.27 0.050 150` | |
| Amber text / tint bg | `0.84 0.110 80` / `0.29 0.050 70` | |
| Rose text / tint bg | `0.76 0.140 22` / `0.28 0.070 20` | |
| Info text / tint bg | `0.80 0.090 205` / `0.27 0.050 205` | |
| Ember | `0.78 0.150 42` | |

### Behavior

- Default to OS `prefers-color-scheme`.
- A user toggle in the account dropdown (`NavUser`), persisted to
  `localStorage` (key `theme` = `light` | `dark` | `system`), applied as a
  `class="dark"` on `<html>`.
- Threshold pages (pre-login: `/login`, `/enroll`, `/consent`, `/logout`,
  `/error`, `/welcome`, `/pair`) follow the OS preference; no persisted
  per-account choice exists pre-auth.

---

## 5. shadcn-vue token mapping (`src/assets/main.css`)

The vendored `ui/` primitives read shadcn-vue's semantic CSS variables. The
work is: (a) define the full ramps as `@theme` tokens so utilities exist, (b)
remap the `:root` aliases onto the new roles (the **inversion**), (c) fill the
`.dark` block, (d) keep the `@theme inline` bridge so `bg-*`/`text-*`/`ring-*`
utilities are generated.

### `:root` (light) — key remaps

| shadcn var | New value | Change |
|---|---|---|
| `--background` | `neutral-25` (canvas) | **was white** → now toned canvas |
| `--card`, `--popover` | `neutral-0` (white) | **was 0.985** → now white (lifts) |
| `--foreground`, `--card-foreground`, `--popover-foreground` | `neutral-900` | — |
| `--primary` | `tide-600` | unchanged |
| `--primary-foreground` | white | unchanged |
| `--secondary`, `--muted` | `neutral-100` (sunken) | — |
| `--secondary-foreground` | `neutral-900` | — |
| `--muted-foreground` | `neutral-600` (**0.48**) | nudged from 0.50 |
| `--accent` | `neutral-150` (hover-well) | stays a neutral tint, not Ember |
| `--accent-foreground` | `neutral-900` | — |
| `--destructive` | `rose-500` | unchanged |
| `--destructive-foreground` | white | unchanged |
| `--border`, `--input` | `neutral-200` | — |
| `--border-strong` *(new)* | `neutral-300` | new token |
| `--ring` | `tide-500` | unchanged |
| `--sidebar` | `neutral-100` | — |
| `--sidebar-accent` | `tide-50` | teal selection wash |
| `--sidebar-accent-foreground` | `tide-700` | — |
| `--sidebar-primary` | `tide-600` | — |
| `--chart-1..5` | `tide-500`, `ember-500`, `sage-500`, `amber-500`, `rose-500` | — |

### `.dark` — fill the currently-empty block

Map the same shadcn vars onto the dark roles in §4 (the `@theme inline` bridge
means overriding `:root` values inside `.dark` recolors the whole kit). Notably
`--primary-foreground` and `--destructive-foreground` become the **dark** label
in dark mode.

### Color-mode toggle

A small composable (`useColorMode`-style or `@vueuse/core`'s `useColorMode`)
that reads `localStorage.theme`, falls back to the OS preference, and toggles
`document.documentElement.classList`. Exposed via a control in `NavUser`.

---

## 6. Component / surface impact

- **`main.css`** — the ramps, the `:root` inversion, the `.dark` block, the
  `border-strong` and `info-*` tokens, `muted-foreground` 0.50 → 0.48.
- **`StatusBadge.vue`** — switch each variant to `-50` background + `-700` text
  + an icon (fixes the amber-on-tint AA failure); add an `info` variant.
- **`NavUser.vue`** — add the theme toggle (light / dark / system).
- **`CodeField` and well/sunken surfaces** — confirm they reference `--sunken`
  / `--muted` rather than a hardcoded gray.
- **Sweep** for any view that hard-assumed a pure-white page background or
  hardcoded `bg-white` / literal neutral hexes; route them through tokens.
- **Dark-mode QA pass** across every threshold page and authenticated view.
- **DESIGN.md** §2 (colors), §4 (elevation), and the Named Rules are updated as
  part of implementation to match this spec (not now).

### Named-rule changes (DESIGN.md §2)

- **Cool-Hand Rule — revised, intent intact.** "Cards are pure white; the
  canvas is a faint **cool** gray." Warmth still forbidden.
- **State-Has-a-Color — extended.** Credential/session states stay
  sage/amber/rose; teal carries brand + info; no fourth state hue.
- **New — the `-500`/`-700` Rule** (fill/icon tone vs. AA text-weight tone).
- **New — the Layered-Surface Rule** (canvas / card / sunken are three distinct
  planes; cards lift by tone, not resting shadow).
- **Unchanged:** Scarce-Accent (Ember), Flat-Until-It-Acts, Code-Gets-Mono,
  One-Voice.

---

## 7. Accessibility verification

A contrast check (script over the resolved sRGB values) asserts every pairing
in **both** themes before the work is considered done. Targets: **≥4.5:1** for
body text, **≥3:1** for large/bold text, UI-component boundaries, and the focus
indicator (WCAG 2.2 Focus Appearance). Values in this spec may be nudged by
≤0.02 L by the script to clear a threshold; the script's pass is the gate.

### Critical pairs (must pass)

**Light**

| Foreground | Background | Threshold |
|---|---|---|
| Ink `neutral-900` | canvas `neutral-25` | 4.5:1 (≈12:1 expected) |
| Muted `neutral-600` (0.48) | canvas `neutral-25` | 4.5:1 |
| Muted `neutral-600` (0.48) | card white | 4.5:1 |
| Link `tide-600` | card white | 4.5:1 |
| White label | primary `tide-600` | 4.5:1 |
| White label | destructive `rose-500` | 4.5:1 — **if it falls short, darken the destructive fill toward `0.54 0.180 22`** |
| `sage-700` / `amber-700` / `rose-700` | their `-50` tints | 4.5:1 (badges) |
| Focus ring `tide-500` | adjacent surfaces | 3:1 |

**Dark**

| Foreground | Background | Threshold |
|---|---|---|
| Ink `0.95` | canvas `0.17` | 4.5:1 (≈14:1 expected) |
| Muted `0.68` | canvas `0.17` | 4.5:1 |
| Link `0.80 0.09 205` | canvas / card | 4.5:1 |
| Dark label `0.17` | primary `0.72 0.12 200` | 4.5:1 |
| Dark label `0.16` | destructive `0.64 0.175 22` | 4.5:1 |
| State text tones | their dark tint bgs | 4.5:1 |
| Focus ring `0.72 0.11 205` | dark surfaces | 3:1 |

---

## 8. Delivery flow

This spec is the decision record. Implementation will be driven by a
writing-plans plan: rewrite `main.css`, fix `StatusBadge`, add the theme
toggle, sweep hardcoded surfaces, run the contrast script, do the dark-mode QA
pass, update DESIGN.md, and rebuild + commit the embedded SPA `dist` at the
done-gate (per project convention; Vite hashes are non-deterministic).
