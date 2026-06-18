# Color System Redesign (Cool Slate) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the flat white + single-teal palette with a layered "Cool Slate" color system — full tonal ramps, three distinct neutral surface planes, an AA-correct state palette, a teal-family info role, and a first-class dark-mode twin with a user toggle.

**Architecture:** All design tokens live in `dashboard/src/assets/main.css` (Tailwind v4 `@theme` + the shadcn-vue `:root`/`.dark` variable bridge). The core change is an **inversion**: the page canvas becomes a faint cool gray and cards become pure white, so surfaces separate by tone with no resting shadow. A standalone contrast script gates WCAG 2.2 AA in both themes. Dark mode is driven by `@vueuse/core`'s `useColorMode` (no inline FOUC script — CSP forbids it).

**Tech Stack:** Vue 3, Tailwind v4, shadcn-vue/Reka UI, `@vueuse/core`, `class-variance-authority`, vitest, vue-tsc; Go `go:embed` for the built SPA.

**Spec:** `docs/superpowers/specs/2026-06-18-color-system-redesign-design.md`

---

## File Structure

| File | Responsibility | Action |
|---|---|---|
| `dashboard/scripts/check-contrast.mjs` | WCAG contrast gate over the token values (both themes) | Create |
| `dashboard/src/assets/main.css` | All color tokens: ramps, `:root` inversion, `.dark` block, info/border-strong | Modify |
| `dashboard/src/components/custom/StatusBadge.vue` | AA-correct state pills (`-50`/`-700`) + `info` variant | Modify |
| `dashboard/src/components/custom/StatusBadge.test.ts` | Variant/class coverage | Create |
| `dashboard/src/composables/useTheme.ts` | Thin wrapper over `useColorMode` (storage key, modes, html class) | Create |
| `dashboard/src/composables/useTheme.test.ts` | localStorage + html-class behavior | Create |
| `dashboard/src/components/custom/ThemeToggle.vue` | 3-way light/dark/system control | Create |
| `dashboard/src/components/custom/ThemeToggle.test.ts` | Renders 3 options, selecting applies the mode | Create |
| `dashboard/src/locales/en.ts` | `theme.*` labels | Modify |
| `dashboard/src/App.vue` | Initialize theme globally (applies to threshold + app routes) | Modify |
| `dashboard/src/components/custom/NavUser.vue` | Mount `ThemeToggle` in the account dropdown | Modify |
| `dashboard/src/components/custom/NavUser.test.ts` | Toggle present in dropdown | Modify |
| `DESIGN.md` | §2 colors, §4 elevation, Named Rules updated to match | Modify |
| `pkg/webui/dist/**` | Rebuilt embedded SPA (done-gate) | Modify (build artifact) |

---

### Task 1: Contrast verification gate

**Goal:** A dependency-free script that proves every critical fg/bg pair clears WCAG 2.2 AA in both themes, so palette values are verified by math, not by eye.

**Files:**
- Create: `dashboard/scripts/check-contrast.mjs`

**Acceptance Criteria:**
- [ ] Converts OKLCH → linear sRGB → WCAG relative luminance → contrast ratio.
- [ ] Asserts a sanity pair (black/white ≥ 20:1) and all light + dark critical pairs from the spec.
- [ ] Prints PASS/FAIL per pair and exits non-zero if any pair is below its threshold.

**Verify:** `cd dashboard && node scripts/check-contrast.mjs` → prints `N/N pairs pass`, exit code 0.

**Steps:**

- [ ] **Step 1: Write the script**

Create `dashboard/scripts/check-contrast.mjs`:

```js
// scripts/check-contrast.mjs
// WCAG 2.2 contrast gate for the Cool Slate color system (light + dark).
// No deps: OKLCH -> linear sRGB -> WCAG relative luminance -> contrast ratio.
// Pair values MUST stay in sync with the tokens in src/assets/main.css.

function oklchToLinearSrgb(L, C, H) {
  const hr = (H * Math.PI) / 180
  const a = C * Math.cos(hr)
  const b = C * Math.sin(hr)
  const l_ = L + 0.3963377774 * a + 0.2158037573 * b
  const m_ = L - 0.1055613458 * a - 0.0638541728 * b
  const s_ = L - 0.0894841775 * a - 1.2914855480 * b
  const l = l_ ** 3, m = m_ ** 3, s = s_ ** 3
  const r = 4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s
  const g = -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s
  const bl = -0.0041960863 * l - 0.7034186147 * m + 1.7076147010 * s
  return [r, g, bl].map((v) => Math.min(1, Math.max(0, v)))
}
function luminance([L, C, H]) {
  const [r, g, b] = oklchToLinearSrgb(L, C, H)
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}
function contrast(fg, bg) {
  const a = luminance(fg), b = luminance(bg)
  const [hi, lo] = a >= b ? [a, b] : [b, a]
  return (hi + 0.05) / (lo + 0.05)
}

// [label, fg(oklch L,C,H), bg(oklch L,C,H), minRatio]
const PAIRS = [
  ['sanity black/white',          [0, 0, 0],          [1, 0, 0],          20],
  // --- light ---
  ['L ink / canvas',              [0.22, 0.015, 240], [0.985, 0.005, 235], 4.5],
  ['L muted / canvas',            [0.48, 0.013, 240], [0.985, 0.005, 235], 4.5],
  ['L muted / card',              [0.48, 0.013, 240], [1, 0, 0],           4.5],
  ['L link tide-600 / card',      [0.47, 0.130, 205], [1, 0, 0],           4.5],
  ['L white / primary tide-600',  [1, 0, 0],          [0.47, 0.130, 205],  4.5],
  ['L white / destructive',       [1, 0, 0],          [0.51, 0.175, 22],   4.5],
  ['L sage-700 / sage-50',        [0.45, 0.105, 150], [0.95, 0.035, 150],  4.5],
  ['L amber-700 / amber-50',      [0.50, 0.090, 65],  [0.96, 0.045, 80],   4.5],
  ['L rose-700 / rose-50',        [0.46, 0.165, 22],  [0.95, 0.035, 20],   4.5],
  ['L info tide-700 / info-bg',   [0.40, 0.128, 205], [0.95, 0.030, 205],  4.5],
  ['L focus tide-500 / canvas',   [0.55, 0.118, 205], [0.985, 0.005, 235], 3.0],
  // --- dark ---
  ['D ink / canvas',              [0.95, 0.005, 240], [0.17, 0.012, 240],  4.5],
  ['D muted / canvas',            [0.68, 0.010, 240], [0.17, 0.012, 240],  4.5],
  ['D link / canvas',             [0.80, 0.090, 205], [0.17, 0.012, 240],  4.5],
  ['D link / card',               [0.80, 0.090, 205], [0.205, 0.013, 240], 4.5],
  ['D darklabel / primary',       [0.17, 0.012, 240], [0.72, 0.120, 200],  4.5],
  ['D darklabel / destructive',   [0.16, 0.010, 22],  [0.64, 0.175, 22],   4.5],
  ['D sage text / tint',          [0.80, 0.100, 150], [0.27, 0.050, 150],  4.5],
  ['D amber text / tint',         [0.84, 0.110, 80],  [0.29, 0.050, 70],   4.5],
  ['D rose text / tint',          [0.76, 0.140, 22],  [0.28, 0.070, 20],   4.5],
  ['D info text / tint',          [0.80, 0.090, 205], [0.27, 0.050, 205],  4.5],
  ['D focus / card',              [0.72, 0.110, 205], [0.205, 0.013, 240], 3.0],
]

let failed = 0
for (const [label, fg, bg, min] of PAIRS) {
  const ratio = contrast(fg, bg)
  const ok = ratio >= min
  if (!ok) failed++
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${ratio.toFixed(2)}:1  (min ${min.toFixed(1)})  ${label}`)
}
console.log(`\n${PAIRS.length - failed}/${PAIRS.length} pairs pass`)
if (failed) {
  console.error(`\n${failed} pair(s) below threshold — adjust the token (lower L by ~0.02 for text-on-light, or raise L for text-on-dark) in BOTH this file and main.css, then re-run.`)
  process.exit(1)
}
```

- [ ] **Step 2: Run it**

Run: `cd dashboard && node scripts/check-contrast.mjs`
Expected: every line `PASS`, final `23/23 pairs pass`, exit 0.
If any line is `FAIL`: lower that color's `L` by `0.02` (text-on-light) or raise it (text-on-dark) here, note the new value for Task 2's token, and re-run until all pass. The known-risk pair is `white / destructive` — `rose-600 (0.51)` is chosen specifically so it passes; do not revert destructive to `rose-500 (0.58)`.

- [ ] **Step 3: Commit**

```bash
cd /home/tundra/projects/tundra/prohibitorum
git add dashboard/scripts/check-contrast.mjs
git commit -m "test(webui): WCAG contrast gate for the Cool Slate palette"
```

---

### Task 2: Rewrite the token system in main.css

**Goal:** Replace `main.css`'s tokens with the full Cool Slate system: numbered ramps, the canvas/card inversion, the three-plane neutrals, the state `-50/-500/-600/-700` steps, info + border-strong tokens, and a complete `.dark` block.

**Files:**
- Modify: `dashboard/src/assets/main.css` (replace the `@theme`, `:root`, `.dark`, and `@theme inline` blocks)

**Acceptance Criteria:**
- [ ] Existing utility names still resolve (`text-ink`, `text-muted`, `bg-sunken`, `bg-surface`, `bg-background`, `border-border`) — revalued, not removed.
- [ ] New ramp utilities exist: `bg-tide-50`, `text-tide-700`, `bg-sage-50`, `text-sage-700`, `bg-amber-50`, `text-amber-700`, `bg-rose-50`, `text-rose-700`, `bg-info`, `text-info-foreground`, `border-info-border`, `border-border-strong`.
- [ ] `--background` resolves to the toned canvas and `--card`/`--popover` to pure white (the inversion).
- [ ] `.dark` overrides every role from the spec §4; the `@theme inline` bridge is intact so dark recolors the whole kit.
- [ ] `npm run build` succeeds and `node scripts/check-contrast.mjs` still passes.

**Verify:** `cd dashboard && npm run build && node scripts/check-contrast.mjs` → build OK, `23/23 pairs pass`.

**Steps:**

- [ ] **Step 1: Replace the file contents**

Replace `dashboard/src/assets/main.css` with:

```css
@import "tailwindcss";
@import "@fontsource-variable/hanken-grotesk";
@import "@fontsource/ibm-plex-mono";

/* =============================================================================
   Cool Slate Design System — Tailwind v4 @theme
   Source of truth: docs/superpowers/specs/2026-06-18-color-system-redesign-design.md
   Token values mirror dashboard/scripts/check-contrast.mjs (AA gate).
   ============================================================================= */

@theme {
  /* --- Tide (teal) ramp — hue 205, the sole brand anchor --- */
  --color-tide-50:  oklch(0.97 0.018 205);
  --color-tide-100: oklch(0.93 0.038 205);
  --color-tide-200: oklch(0.87 0.065 205);
  --color-tide-300: oklch(0.78 0.090 205);
  --color-tide-400: oklch(0.67 0.110 205);
  --color-tide-500: oklch(0.55 0.118 205);   /* fills, focus-ring tone, large text */
  --color-tide-600: oklch(0.47 0.130 205);   /* primary fill w/ white label, links, AA text */
  --color-tide-700: oklch(0.40 0.128 205);   /* hover/active, info text */
  --color-tide-800: oklch(0.33 0.105 205);
  --color-tide-900: oklch(0.26 0.075 205);
  --color-tide-950: oklch(0.20 0.052 205);
  /* Back-compat aliases (existing utilities text-tide / bg-tide-strong etc.) */
  --color-tide:        oklch(0.55 0.118 205);
  --color-tide-strong: oklch(0.47 0.130 205);

  /* --- Ember (scarce warm brand accent) — hue 42 --- */
  --color-ember:     oklch(0.70 0.150 42);
  --color-ember-600: oklch(0.58 0.140 42);

  /* --- State: Sage / Amber / Rose. -500 = fill/icon, -700 = AA text --- */
  --color-sage-50:  oklch(0.95 0.035 150);
  --color-sage-100: oklch(0.90 0.060 150);
  --color-sage-500: oklch(0.62 0.130 150);
  --color-sage-600: oklch(0.52 0.120 150);
  --color-sage-700: oklch(0.45 0.105 150);
  --color-sage:     oklch(0.62 0.130 150);   /* back-compat alias */

  --color-amber-50:  oklch(0.96 0.045 80);
  --color-amber-100: oklch(0.92 0.075 80);
  --color-amber-500: oklch(0.76 0.140 75);
  --color-amber-600: oklch(0.62 0.110 70);
  --color-amber-700: oklch(0.50 0.090 65);
  --color-amber:     oklch(0.76 0.140 75);   /* back-compat alias */

  --color-rose-50:  oklch(0.95 0.035 20);
  --color-rose-100: oklch(0.90 0.060 20);
  --color-rose-500: oklch(0.58 0.180 22);
  --color-rose-600: oklch(0.51 0.175 22);    /* destructive fill (white label, AA) */
  --color-rose-700: oklch(0.46 0.165 22);
  --color-rose:     oklch(0.58 0.180 22);    /* back-compat alias */

  /* --- Info (teal-family, distinct from the tide-50 selection wash) --- */
  --color-info:            oklch(0.95 0.030 205);   /* callout bg */
  --color-info-border:     oklch(0.87 0.065 205);   /* = tide-200 */
  --color-info-icon:       oklch(0.47 0.130 205);   /* = tide-600 */
  --color-info-foreground: oklch(0.40 0.128 205);   /* = tide-700 */

  /* --- Neutral ramp (cool, hue ~235-240) --- */
  --color-neutral-0:   oklch(1 0 0);
  --color-neutral-25:  oklch(0.985 0.005 235);
  --color-neutral-50:  oklch(0.975 0.006 235);
  --color-neutral-100: oklch(0.965 0.008 235);
  --color-neutral-150: oklch(0.945 0.010 235);
  --color-neutral-200: oklch(0.910 0.010 235);
  --color-neutral-300: oklch(0.850 0.011 235);
  --color-neutral-400: oklch(0.720 0.012 238);
  --color-neutral-500: oklch(0.620 0.012 240);
  --color-neutral-600: oklch(0.480 0.013 240);
  --color-neutral-700: oklch(0.400 0.014 240);
  --color-neutral-800: oklch(0.300 0.015 240);
  --color-neutral-900: oklch(0.220 0.015 240);
  --color-neutral-950: oklch(0.160 0.012 240);

  /* --- Semantic neutrals (back-compat names, revalued) --- */
  --color-bg:           oklch(0.985 0.005 235);  /* CANVAS — the page (was pure white) */
  --color-surface:      oklch(1 0 0);            /* CARD — the white that lifts (was 0.985) */
  --color-sunken:       oklch(0.965 0.008 235);  /* sidebar / toolbar / well */
  --color-border:       oklch(0.910 0.010 235);
  --color-border-strong:oklch(0.850 0.011 235);
  --color-ink:          oklch(0.220 0.015 240);  /* body text (~12:1 on canvas) */
  --color-ink-strong:   oklch(0.160 0.012 240);
  --color-muted:        oklch(0.480 0.013 240);  /* secondary text (>=4.5:1 on canvas) */

  /* --- Typography --- */
  --font-sans: "Hanken Grotesk Variable", ui-sans-serif, system-ui, -apple-system, sans-serif;
  --font-mono: "IBM Plex Mono", ui-monospace, SFMono-Regular, Menlo, monospace;

  /* --- Border Radius --- */
  --radius-sm:   6px;
  --radius-md:   10px;
  --radius-lg:   14px;
  --radius-full: 9999px;

  /* --- Elevation (flat at rest; depth only on state) --- */
  --shadow-raised:  0 1px 2px oklch(0.22 0.015 240 / 0.06), 0 2px 8px oklch(0.22 0.015 240 / 0.07);
  --shadow-overlay: 0 8px 32px oklch(0.16 0.012 240 / 0.18);
}

/* =============================================================================
   shadcn-vue semantic variable aliases mapped onto Cool Slate tokens (LIGHT).
   ============================================================================= */
:root {
  --background:            var(--color-bg);        /* canvas */
  --foreground:            var(--color-ink);

  --card:                  var(--color-surface);   /* white, lifts off canvas */
  --card-foreground:       var(--color-ink);
  --popover:               var(--color-surface);
  --popover-foreground:    var(--color-ink);

  --primary:               var(--color-tide-600);
  --primary-foreground:    oklch(1 0 0);

  --secondary:             var(--color-sunken);
  --secondary-foreground:  var(--color-ink);
  --muted:                 var(--color-sunken);
  --muted-foreground:      var(--color-muted);

  /* Ghost/outline hover background — neutral hover-well, NOT Ember. */
  --accent:                var(--color-neutral-150);
  --accent-foreground:     var(--color-ink);

  --destructive:           var(--color-rose-600);  /* white label passes AA */
  --destructive-foreground: oklch(1 0 0);

  --border:                var(--color-border);
  --input:                 var(--color-border);
  --ring:                  var(--color-tide-500);

  --radius:                10px;

  --chart-1:               var(--color-tide-500);
  --chart-2:               var(--color-ember);
  --chart-3:               var(--color-sage-500);
  --chart-4:               var(--color-amber-500);
  --chart-5:               var(--color-rose-500);

  --sidebar:                    var(--color-sunken);
  --sidebar-foreground:         var(--color-ink);
  --sidebar-primary:            var(--color-tide-600);
  --sidebar-primary-foreground: oklch(1 0 0);
  --sidebar-accent:             var(--color-tide-50);
  --sidebar-accent-foreground:  var(--color-tide-700);
  --sidebar-border:             var(--color-border);
  --sidebar-ring:               var(--color-tide-500);
}

/* =============================================================================
   Dark mode — "Cool Slate, dark". Inverted surface logic: canvas mid-dark,
   cards lift lighter, sidebar recesses darker. Brand/state hues lightened.
   Primary + destructive flip to a DARK label on a brighter fill.
   ============================================================================= */
.dark {
  --background:            oklch(0.170 0.012 240);
  --foreground:            oklch(0.950 0.005 240);

  --card:                  oklch(0.205 0.013 240);
  --card-foreground:       oklch(0.950 0.005 240);
  --popover:               oklch(0.205 0.013 240);
  --popover-foreground:    oklch(0.950 0.005 240);

  --primary:               oklch(0.72 0.120 200);
  --primary-foreground:    oklch(0.170 0.012 240);

  --secondary:             oklch(0.245 0.013 240);
  --secondary-foreground:  oklch(0.950 0.005 240);
  --muted:                 oklch(0.245 0.013 240);
  --muted-foreground:      oklch(0.680 0.010 240);

  --accent:                oklch(0.255 0.014 240);
  --accent-foreground:     oklch(0.950 0.005 240);

  --destructive:           oklch(0.64 0.175 22);
  --destructive-foreground: oklch(0.160 0.010 22);

  --border:                oklch(0.300 0.012 240);
  --input:                 oklch(0.300 0.012 240);
  --ring:                  oklch(0.72 0.110 205);

  /* Brand text tokens used directly by components (text-ink/text-muted/...) */
  --color-ink:             oklch(0.950 0.005 240);
  --color-ink-strong:      oklch(0.985 0.003 240);
  --color-muted:           oklch(0.680 0.010 240);
  --color-bg:              oklch(0.170 0.012 240);
  --color-surface:         oklch(0.205 0.013 240);
  --color-sunken:          oklch(0.145 0.011 240);
  --color-border:          oklch(0.300 0.012 240);
  --color-border-strong:   oklch(0.370 0.012 240);

  /* State + info: lightened text tones and low-L tint backgrounds for dark. */
  --color-sage-50:  oklch(0.27 0.050 150);
  --color-sage-700: oklch(0.80 0.100 150);
  --color-amber-50: oklch(0.29 0.050 70);
  --color-amber-700: oklch(0.84 0.110 80);
  --color-rose-50:  oklch(0.28 0.070 20);
  --color-rose-700: oklch(0.76 0.140 22);
  --color-info:            oklch(0.27 0.050 205);
  --color-info-border:     oklch(0.35 0.060 205);
  --color-info-icon:       oklch(0.80 0.090 205);
  --color-info-foreground: oklch(0.80 0.090 205);

  --shadow-overlay: 0 8px 32px oklch(0 0 0 / 0.5);

  --sidebar:                    oklch(0.145 0.011 240);
  --sidebar-foreground:         oklch(0.950 0.005 240);
  --sidebar-primary:            oklch(0.72 0.120 200);
  --sidebar-primary-foreground: oklch(0.170 0.012 240);
  --sidebar-accent:             oklch(0.27 0.050 205);
  --sidebar-accent-foreground:  oklch(0.85 0.080 205);
  --sidebar-border:             oklch(0.300 0.012 240);
  --sidebar-ring:               oklch(0.72 0.110 205);
}

/* =============================================================================
   Bridge shadcn-vue semantic vars into Tailwind v4 theme tokens so utilities
   (bg-primary, bg-card, ring-ring, bg-info, ...) are generated. "inline" keeps
   each as a var() reference, so the .dark overrides above recolor the whole kit.
   ============================================================================= */
@theme inline {
  --color-background:             var(--background);
  --color-foreground:             var(--foreground);
  --color-card:                   var(--card);
  --color-card-foreground:        var(--card-foreground);
  --color-popover:                var(--popover);
  --color-popover-foreground:     var(--popover-foreground);
  --color-primary:                var(--primary);
  --color-primary-foreground:     var(--primary-foreground);
  --color-secondary:              var(--secondary);
  --color-secondary-foreground:   var(--secondary-foreground);
  --color-muted-foreground:       var(--muted-foreground);
  --color-accent:                 var(--accent);
  --color-accent-foreground:      var(--accent-foreground);
  --color-destructive:            var(--destructive);
  --color-destructive-foreground: var(--destructive-foreground);
  --color-input:                  var(--input);
  --color-ring:                   var(--ring);
  --color-chart-1:                var(--chart-1);
  --color-chart-2:                var(--chart-2);
  --color-chart-3:                var(--chart-3);
  --color-chart-4:                var(--chart-4);
  --color-chart-5:                var(--chart-5);
  --color-sidebar:                     var(--sidebar);
  --color-sidebar-foreground:          var(--sidebar-foreground);
  --color-sidebar-primary:             var(--sidebar-primary);
  --color-sidebar-primary-foreground:  var(--sidebar-primary-foreground);
  --color-sidebar-accent:              var(--sidebar-accent);
  --color-sidebar-accent-foreground:   var(--sidebar-accent-foreground);
  --color-sidebar-border:              var(--sidebar-border);
  --color-sidebar-ring:                var(--sidebar-ring);
}

/* =============================================================================
   Base layer.
   ============================================================================= */
@layer base {
  *,
  ::after,
  ::before {
    border-color: var(--color-border);
  }

  body {
    background-color: var(--color-bg);
    color:            var(--color-ink);
    font-family:      var(--font-sans);
    line-height:      1.55;
    -webkit-font-smoothing:  antialiased;
    -moz-osx-font-smoothing: grayscale;
  }

  code, kbd, samp, pre {
    font-family: var(--font-mono);
  }

  ::selection {
    background-color: oklch(0.55 0.118 205 / 0.20);
    color: var(--color-ink);
  }
}

/* =============================================================================
   Reduced motion — honored project-wide.
   ============================================================================= */
@media (prefers-reduced-motion: reduce) {
  *,
  ::before,
  ::after {
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
    transition-duration: 0.01ms !important;
    scroll-behavior: auto !important;
  }
}
```

- [ ] **Step 2: Build + contrast gate**

Run: `cd dashboard && npm run build && node scripts/check-contrast.mjs`
Expected: `vue-tsc -b` + `vite build` succeed (exit 0); `23/23 pairs pass`.
If a contrast pair fails, adjust the offending token here AND in `check-contrast.mjs` (keep them in sync) and re-run.

- [ ] **Step 3: Sanity-check the inversion took effect**

Run: `cd dashboard && rg -n -- '--background:\s+var\(--color-bg\)' src/assets/main.css` and `rg -n -- '--card:\s+var\(--color-surface\)' src/assets/main.css`
Expected: both match (canvas → background, white → card).

- [ ] **Step 4: Commit**

```bash
cd /home/tundra/projects/tundra/prohibitorum
git add dashboard/src/assets/main.css
git commit -m "feat(webui): Cool Slate token system — layered neutrals, full ramps, dark block"
```

---

### Task 3: AA-correct StatusBadge + info variant

**Goal:** Re-base every `StatusBadge` variant on the `-50` tint + `-700` text tones (fixing the amber-on-tint AA failure where it renders `text-amber` at L0.76), and add an `info` variant.

**Files:**
- Modify: `dashboard/src/components/custom/StatusBadge.vue`
- Create: `dashboard/src/components/custom/StatusBadge.test.ts`

**Acceptance Criteria:**
- [ ] `success`/`caution`/`danger` use `bg-*-50 text-*-700`; `caution` no longer emits `text-amber` (L0.76).
- [ ] A new `info` variant renders `bg-tide-50 text-tide-700`.
- [ ] `neutral` stays `bg-sunken text-muted`.
- [ ] The text label remains the non-color signal (Do-Not-Rely-On-Color-Alone is satisfied by content, so no icon is forced).

**Verify:** `cd dashboard && npx vitest run src/components/custom/StatusBadge.test.ts` → all pass.

**Steps:**

- [ ] **Step 1: Write the failing test**

Create `dashboard/src/components/custom/StatusBadge.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import StatusBadge from './StatusBadge.vue'

const cls = (variant?: string) =>
  mount(StatusBadge, { props: variant ? { variant } : {}, slots: { default: 'X' } }).classes().join(' ')

describe('StatusBadge', () => {
  it('defaults to neutral', () => {
    expect(cls()).toContain('bg-sunken')
    expect(cls()).toContain('text-muted')
  })
  it('success uses the -50/-700 pair', () => {
    expect(cls('success')).toContain('bg-sage-50')
    expect(cls('success')).toContain('text-sage-700')
  })
  it('caution uses amber-700 text (NOT the L0.76 amber-500)', () => {
    const c = cls('caution')
    expect(c).toContain('bg-amber-50')
    expect(c).toContain('text-amber-700')
    expect(c).not.toMatch(/\btext-amber\b/)
  })
  it('danger uses the -50/-700 pair', () => {
    expect(cls('danger')).toContain('bg-rose-50')
    expect(cls('danger')).toContain('text-rose-700')
  })
  it('info uses the teal family', () => {
    expect(cls('info')).toContain('bg-tide-50')
    expect(cls('info')).toContain('text-tide-700')
  })
})
```

- [ ] **Step 2: Run test, expect FAIL**

Run: `cd dashboard && npx vitest run src/components/custom/StatusBadge.test.ts`
Expected: FAIL (current variants emit `bg-sage/12 text-sage`, `text-amber`, etc.; no `info`).

- [ ] **Step 3: Update the component**

Replace the `cva` config and `Props` type in `dashboard/src/components/custom/StatusBadge.vue`:

```ts
const badge = cva(
  'inline-flex items-center gap-1 whitespace-nowrap rounded-full px-2 py-0.5 text-xs font-medium',
  {
    variants: {
      variant: {
        neutral: 'bg-sunken text-muted',
        success: 'bg-sage-50 text-sage-700',
        caution: 'bg-amber-50 text-amber-700',
        danger: 'bg-rose-50 text-rose-700',
        info: 'bg-tide-50 text-tide-700',
      },
    },
    defaultVariants: { variant: 'neutral' },
  },
)
type Props = { variant?: VariantProps<typeof badge>['variant']; class?: HTMLAttributes['class'] }
```

(The `<script setup>` imports and `<template>` are unchanged.)

- [ ] **Step 4: Run test, expect PASS**

Run: `cd dashboard && npx vitest run src/components/custom/StatusBadge.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/tundra/projects/tundra/prohibitorum
git add dashboard/src/components/custom/StatusBadge.vue dashboard/src/components/custom/StatusBadge.test.ts
git commit -m "fix(webui): AA-correct StatusBadge tints + info variant"
```

---

### Task 4: useTheme composable

**Goal:** A single composable wrapping `@vueuse/core`'s `useColorMode` that persists `light`/`dark`/`auto` to `localStorage['theme']` and toggles the `dark` class on `<html>`.

**Files:**
- Create: `dashboard/src/composables/useTheme.ts`
- Create: `dashboard/src/composables/useTheme.test.ts`

**Acceptance Criteria:**
- [ ] Exposes `mode` (a writable ref of `'light' | 'dark' | 'auto'`) and `setMode(m)`.
- [ ] Setting `mode='dark'` adds `class="dark"` to `document.documentElement`; `'light'` removes it.
- [ ] Selection persists to `localStorage['theme']`.

**Verify:** `cd dashboard && npx vitest run src/composables/useTheme.test.ts` → all pass.

**Steps:**

- [ ] **Step 1: Write the failing test**

Create `dashboard/src/composables/useTheme.test.ts`:

```ts
import { describe, it, expect, beforeEach } from 'vitest'
import { nextTick } from 'vue'
import { useTheme } from './useTheme'

describe('useTheme', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.className = ''
  })

  it('applies the dark class when set to dark', async () => {
    const { setMode } = useTheme()
    setMode('dark')
    await nextTick()
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(localStorage.getItem('theme')).toBe('dark')
  })

  it('removes the dark class when set to light', async () => {
    const { setMode } = useTheme()
    setMode('dark'); await nextTick()
    setMode('light'); await nextTick()
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(localStorage.getItem('theme')).toBe('light')
  })
})
```

- [ ] **Step 2: Run test, expect FAIL**

Run: `cd dashboard && npx vitest run src/composables/useTheme.test.ts`
Expected: FAIL ("Failed to resolve import './useTheme'").

- [ ] **Step 3: Write the composable**

Create `dashboard/src/composables/useTheme.ts`:

```ts
import { useColorMode } from '@vueuse/core'

export type ThemeMode = 'light' | 'dark' | 'auto'

/**
 * Single source of truth for the app theme. Persists the user's choice to
 * localStorage['theme'] and toggles `class="dark"` on <html>. `auto` follows
 * the OS prefers-color-scheme. Call once at app start (App.vue) so the class
 * applies on every route, including pre-login threshold pages.
 *
 * Note: CSP is `script-src 'self'`, so there is no inline FOUC-prevention
 * script; the class is applied by this composable during app bootstrap.
 */
export function useTheme() {
  const mode = useColorMode({
    storageKey: 'theme',
    attribute: 'class',
    selector: 'html',
    modes: { light: 'light', dark: 'dark' },
    initialValue: 'auto',
    emitAuto: true,
  })
  function setMode(m: ThemeMode): void {
    mode.value = m
  }
  return { mode, setMode }
}
```

- [ ] **Step 4: Run test, expect PASS**

Run: `cd dashboard && npx vitest run src/composables/useTheme.test.ts`
Expected: PASS. (`useColorMode` with `attribute: 'class'` adds `light`/`dark` to `<html>`; only `.dark` has CSS, the stray `light` class is inert.)

- [ ] **Step 5: Commit**

```bash
cd /home/tundra/projects/tundra/prohibitorum
git add dashboard/src/composables/useTheme.ts dashboard/src/composables/useTheme.test.ts
git commit -m "feat(webui): useTheme composable (light/dark/auto via useColorMode)"
```

---

### Task 5: ThemeToggle control + i18n

**Goal:** A compact 3-way control (Light / System / Dark) bound to `useTheme`, with translated, keyboard-operable, labelled options.

**Files:**
- Create: `dashboard/src/components/custom/ThemeToggle.vue`
- Create: `dashboard/src/components/custom/ThemeToggle.test.ts`
- Modify: `dashboard/src/locales/en.ts`

**Acceptance Criteria:**
- [ ] Renders three controls (Light, System, Dark) with icons and accessible labels.
- [ ] Implemented as a `role="radiogroup"` of `role="radio"` buttons (mutually exclusive) with a roving tabindex and Left/Right/Up/Down arrow navigation (keyboard-first, WCAG 2.2 AA); the active option has `aria-checked="true"`; clicking or arrowing to another sets that mode.
- [ ] Labels come from `t('theme.*')`.

**Verify:** `cd dashboard && npx vitest run src/components/custom/ThemeToggle.test.ts` → all pass.

> **Post-Task-4 corrections (applied at dispatch):** (1) the component binds the active state to `stored` (the persisted selection from `useTheme`, which can be `'auto'`), NOT `mode` (the resolved value) — otherwise the System button never reads active. (2) The test must use the project's real i18n setup (`createI18n({ legacy: false, ... messages: { en } })` via `global.plugins`, mirroring `NavUser.test.ts`) plus a `window.matchMedia` shim, since the component calls `useI18n()` and `useColorMode` reads `prefers-color-scheme`. The `t`-mock sketch below is superseded.

**Steps:**

- [ ] **Step 1: Add i18n keys**

In `dashboard/src/locales/en.ts`, add a top-level `theme` key as a sibling of `accountMenu`:

```ts
  theme: {
    label: 'Theme',
    light: 'Light',
    dark: 'Dark',
    system: 'System',
  },
```

After editing, grep-verify no curly quotes were introduced (en.ts apostrophe hazard):
Run: `cd dashboard && rg -n "[‘’]" src/locales/en.ts` → expected: no output.

- [ ] **Step 2: Write the failing test**

Create `dashboard/src/components/custom/ThemeToggle.test.ts`:

```ts
import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import ThemeToggle from './ThemeToggle.vue'

const i18n = {
  global: { mocks: { t: (k: string) => k } },
}

describe('ThemeToggle', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.className = ''
  })

  it('renders three options', () => {
    const w = mount(ThemeToggle, i18n)
    expect(w.findAll('button')).toHaveLength(3)
  })

  it('selecting Dark applies the dark theme', async () => {
    const w = mount(ThemeToggle, i18n)
    await w.get('[data-test="theme-dark"]').trigger('click')
    await nextTick()
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(w.get('[data-test="theme-dark"]').attributes('aria-pressed')).toBe('true')
  })
})
```

- [ ] **Step 3: Run test, expect FAIL**

Run: `cd dashboard && npx vitest run src/components/custom/ThemeToggle.test.ts`
Expected: FAIL ("Failed to resolve import './ThemeToggle.vue'").

- [ ] **Step 4: Write the component**

Create `dashboard/src/components/custom/ThemeToggle.vue`:

```vue
<script setup lang="ts">
/** ThemeToggle — compact light/system/dark switch bound to useTheme. */
import { useI18n } from 'vue-i18n'
import { Sun, Moon, Monitor } from 'lucide-vue-next'
import { useTheme, type ThemeMode } from '@/composables/useTheme'
import { cn } from '@/lib/utils'

const { t } = useI18n()
const { mode, setMode } = useTheme()

const options: { value: ThemeMode; icon: typeof Sun; label: string; test: string }[] = [
  { value: 'light', icon: Sun, label: t('theme.light'), test: 'theme-light' },
  { value: 'auto', icon: Monitor, label: t('theme.system'), test: 'theme-system' },
  { value: 'dark', icon: Moon, label: t('theme.dark'), test: 'theme-dark' },
]
</script>

<template>
  <div class="flex items-center justify-between gap-2 px-1 py-1">
    <span class="text-xs font-medium text-muted">{{ t('theme.label') }}</span>
    <div role="group" :aria-label="t('theme.label')" class="inline-flex rounded-md bg-sunken p-0.5">
      <button
        v-for="o in options"
        :key="o.value"
        type="button"
        :data-test="o.test"
        :aria-label="o.label"
        :aria-pressed="mode === o.value"
        :class="cn(
          'inline-flex size-7 cursor-pointer items-center justify-center rounded-[7px] outline-none transition-colors',
          'focus-visible:ring-2 focus-visible:ring-ring',
          mode === o.value ? 'bg-card text-ink shadow-raised' : 'text-muted hover:text-ink',
        )"
        @click="setMode(o.value)"
      >
        <component :is="o.icon" class="size-4" aria-hidden="true" />
      </button>
    </div>
  </div>
</template>
```

- [ ] **Step 5: Run test, expect PASS**

Run: `cd dashboard && npx vitest run src/components/custom/ThemeToggle.test.ts`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /home/tundra/projects/tundra/prohibitorum
git add dashboard/src/components/custom/ThemeToggle.vue dashboard/src/components/custom/ThemeToggle.test.ts dashboard/src/locales/en.ts
git commit -m "feat(webui): ThemeToggle control + theme.* i18n"
```

---

### Task 6: Initialize theme globally + wire into NavUser

**Goal:** Apply the persisted theme on every route (App.vue), and surface the toggle in the account dropdown without the menu closing on interaction.

**Files:**
- Modify: `dashboard/src/App.vue`
- Modify: `dashboard/src/components/custom/NavUser.vue`
- Modify: `dashboard/src/components/custom/NavUser.test.ts`

**Acceptance Criteria:**
- [ ] `useTheme()` is called once in `App.vue` setup, so the class applies to threshold + app routes.
- [ ] `ThemeToggle` renders inside the account dropdown, in a non-`DropdownMenuItem` wrapper (so clicking it does not auto-close the menu), separated from the other actions.
- [ ] `NavUser.test.ts` asserts the toggle is present.

**Verify:** `cd dashboard && npx vitest run src/components/custom/NavUser.test.ts && npm run build` → tests pass, build OK.

**Steps:**

- [ ] **Step 1: Initialize theme in App.vue**

In `dashboard/src/App.vue`, inside `<script setup>`, add the import and call (placement among existing setup code is fine; the call registers the color-mode watcher for the whole app):

```ts
import { useTheme } from '@/composables/useTheme'
useTheme()
```

- [ ] **Step 2: Mount ThemeToggle in NavUser**

In `dashboard/src/components/custom/NavUser.vue`:

Add the import alongside the other custom-component imports:

```ts
import ThemeToggle from '@/components/custom/ThemeToggle.vue'
```

Insert a theme section into `DropdownMenuContent`, between the edit-profile item and the separator before sign-out. Place `ThemeToggle` in a plain wrapper (NOT a `DropdownMenuItem`) so interacting with it does not close the menu:

```vue
          <DropdownMenuItem data-test="account-edit" @select="openEdit">
            <Pencil />
            <span>{{ t('accountMenu.editProfile') }}</span>
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <div class="px-1 py-1" @pointerdown.stop @keydown.stop>
            <ThemeToggle />
          </div>
          <DropdownMenuSeparator />
          <DropdownMenuItem data-test="account-signout" @select="signOut">
            <LogOut />
            <span>{{ t('nav.signOut') }}</span>
          </DropdownMenuItem>
```

(`@pointerdown.stop`/`@keydown.stop` keep Reka's menu keyboard/typeahead from hijacking the toggle's clicks and arrow keys.)

- [ ] **Step 3: Extend NavUser.test.ts**

Add a test asserting the toggle renders. Append inside the existing top-level `describe` in `dashboard/src/components/custom/NavUser.test.ts` (reuse the file's existing mount helper/store setup; the pattern below matches the project's other dropdown tests — if the existing file uses a `renderOpen()`/`mountNavUser()` helper, call that instead of re-mounting):

```ts
  it('shows the theme toggle in the dropdown', async () => {
    const w = mountNavUser() // use the file's existing helper that opens the menu
    await openMenu(w)        // existing helper; or trigger the DropdownMenuTrigger
    expect(w.find('[data-test="theme-dark"]').exists()).toBe(true)
  })
```

If `NavUser.test.ts` has no open-menu helper, follow the existing test's setup verbatim and assert `wrapper.findComponent(ThemeToggle).exists()` after the menu is opened.

- [ ] **Step 4: Run tests + build**

Run: `cd dashboard && npx vitest run src/components/custom/NavUser.test.ts && npm run build`
Expected: tests pass; `vue-tsc -b` + `vite build` exit 0.

- [ ] **Step 5: Commit**

```bash
cd /home/tundra/projects/tundra/prohibitorum
git add dashboard/src/App.vue dashboard/src/components/custom/NavUser.vue dashboard/src/components/custom/NavUser.test.ts
git commit -m "feat(webui): global theme init + ThemeToggle in account menu"
```

---

### Task 7: Surface sweep + dark-mode review

**Goal:** Confirm no surface hard-assumes a pure-white page under the inversion, and visually verify both themes across representative surfaces.

**Files:**
- Modify (only if the sweep finds a real issue): any view using a hardcoded white/literal neutral instead of a token.

**Acceptance Criteria:**
- [ ] The `bg-surface` (1×) and `bg-muted` (1×) usages still read correctly with the inversion (surface = white, muted = sunken); fix to the right token only if visibly wrong.
- [ ] The two `text-white` spinners (`WelcomeView.vue`, `UserAvatar.vue`) sit on colored fills (teal/ember/avatar) in both themes — confirm legible; leave as-is if so.
- [ ] `AuthBackdrop.vue`'s literal-OKLCH painted scenery is unchanged (explicit non-goal) and its card/header still meet contrast over the image in dark mode.
- [ ] No CSP change (no inline FOUC script added).

**Verify:** `cd dashboard && npm run dev` then load `/login`, `/`, `/security`, an admin list + detail, and a dialog, toggling the theme via the account menu. Confirm: cards lift off the canvas (light), surfaces invert correctly (dark), focus rings visible, badges legible.

**Steps:**

- [ ] **Step 1: Re-grep the surface assumptions**

Run: `cd dashboard && rg -n 'bg-surface|bg-muted\b|bg-white|text-white' src -g '*.vue'`
Expected: `bg-surface` (1, a card-like panel → now white, fine), `bg-muted` (1 → sunken, fine), the two spinner `text-white` (on colored fills, fine), and `AuthBackdrop` (out of scope). Only edit a file if a usage is genuinely wrong under the inversion; otherwise no change.

- [ ] **Step 2: Live dark/light pass**

Run: `cd dashboard && npm run dev` (proxies `/api` etc. to `:8080`; start the Go dev server separately if needed per project tooling).
Toggle theme via the account menu on: `/login`, `/` (security), an admin list, an admin detail, and an open dialog. Note any unreadable text, an unlit focus ring, or a surface that fails to separate. Fix by routing the offending element through a token (e.g. `bg-card`, `text-muted`, `border-border`); do not introduce new literals.

- [ ] **Step 3: Commit (only if fixes were made)**

```bash
cd /home/tundra/projects/tundra/prohibitorum
git add -A dashboard/src
git commit -m "fix(webui): route stray surfaces through tokens for dark mode"
```

If no fixes were needed, record that in the task notes and skip the commit.

---

### Task 8: Update DESIGN.md + rebuild dist (done-gate)

**Goal:** Bring the design system doc in line with the implemented tokens and rules, rebuild the embedded SPA, and run the full project gate.

**Files:**
- Modify: `DESIGN.md`
- Modify (build artifact): `pkg/webui/dist/**`

**Acceptance Criteria:**
- [ ] DESIGN.md §2 lists the new ramps + the canvas/card inversion; the dark-mode values are pinned (no longer "exact tokens pinned once implemented").
- [ ] DESIGN.md Named Rules: Cool-Hand revised ("cards are pure white; canvas is a faint cool gray"), State-Has-a-Color extended (teal carries info), plus the new `-500/-700` Rule and Layered-Surface Rule; §4 elevation hue updated to 240.
- [ ] `pkg/webui/dist` is rebuilt and committed.
- [ ] Full gate green.

**Verify:**
```bash
cd /home/tundra/projects/tundra/prohibitorum/dashboard && npm run build && npx vitest run && node scripts/check-contrast.mjs
cd /home/tundra/projects/tundra/prohibitorum && go build -tags nodynamic ./... && go vet ./...
```
Expected: vite build OK, all vitest pass, `23/23 pairs pass`, Go build + vet exit 0.

**Steps:**

- [ ] **Step 1: Edit DESIGN.md**

Update the `colors:` front-matter block and §2 to the new token values (canvas `0.985 0.005 235`, surface white, `muted 0.48 0.013 240`, `ink 0.22 0.015 240`, the tide/sage/amber/rose ramps, info, border-strong). Replace the dark-mode note in §2 with the pinned dark values from the spec §4. Edit §4 elevation shadows' hue from `230` to `240`. In the Named Rules, revise Cool-Hand and State-Has-a-Color and add the `-500/-700` Rule and the Layered-Surface Rule (wording from spec §6). Keep Scarce-Accent, Flat-Until-It-Acts, Code-Gets-Mono, One-Voice unchanged.

After editing, grep-verify no curly quotes/apostrophe damage:
Run: `rg -n "[‘’]" DESIGN.md` → expected: no output (straight quotes only).

Also apply three carried-over `main.css` hygiene tweaks from the Task 2 review (cosmetic, no behavior change): (a) restore a one-line comment at `--color-muted` noting its dual role (brand secondary TEXT vs the shadcn `--muted` background surface); (b) add a one-line comment above the neutral `0→950` ramp noting it is a light-reference scale and components should prefer the dark-aware semantic tokens (`bg-surface`/`bg-sunken`/`text-ink`/`text-muted`); (c) fix the missing space in `--color-border-strong:oklch(...)` → `--color-border-strong: oklch(...)`.

Carried from the Task 3 review (also cosmetic/non-breaking): (d) add a one-line comment above the `StatusBadge.test.ts` amber guard explaining the `/\btext-amber(?!-)\b/` negative-lookahead (so it isn't simplified back into the false-positive bug); (e) in `dashboard/src/pages/admin/AdminSigningKeysView.vue`, add `'info'` to the local `type Variant = 'neutral' | 'success' | 'caution' | 'danger'` union now that `StatusBadge` supports it (no current caller uses it, so this is forward-correctness only); (f) add a brief comment on the `NavUser.vue` ThemeToggle wrapper `<div>` explaining why it stops `pointerdown`/`keydown` (keeps the menu open + prevents the menu hijacking the radiogroup's arrow keys; Escape still closes via Reka's window-level listener).

- [ ] **Step 2: Rebuild the embedded SPA**

Run: `cd dashboard && npm run build`
Expected: build writes hashed assets into `pkg/webui/dist` (per the project's Vite `build.outDir`). Confirm: `cd /home/tundra/projects/tundra/prohibitorum && git status --short pkg/webui/dist` shows changes.

- [ ] **Step 3: Run the full gate**

```bash
cd /home/tundra/projects/tundra/prohibitorum/dashboard && npm run build && npx vitest run && node scripts/check-contrast.mjs
cd /home/tundra/projects/tundra/prohibitorum && go build -tags nodynamic ./... && go vet ./...
```
Expected: all green (vite OK, vitest all pass, `23/23 pairs pass`, Go build + vet exit 0).

- [ ] **Step 4: Commit**

```bash
cd /home/tundra/projects/tundra/prohibitorum
git add DESIGN.md pkg/webui/dist
git commit -m "docs(design)+build(webui): document Cool Slate system, rebuild embedded SPA"
```

---

## Self-Review

**Spec coverage:**
- Layered neutral stack / inversion → Task 2 (canvas `--background`, white `--card`). ✔
- Tide ramp + accents-only application → Task 2 (ramp) + Task 3/5 (tide-50/700 usage). ✔
- State palette + `-500/-700` rule → Task 2 (ramps) + Task 3 (StatusBadge). ✔
- Info (teal family) → Task 2 (info tokens) + Task 3 (`info` badge variant). ✔
- Ember scarce → unchanged token in Task 2. ✔
- Elevation (flat at rest, two shadows) → Task 2 (`--shadow-raised/-overlay`). ✔
- Dark-mode twin + values → Task 2 (`.dark` block). ✔
- Dark behavior (OS default + toggle, threshold follows OS) → Task 4 (`auto` init) + Task 6 (App.vue global init + NavUser toggle). ✔
- Dark primary/destructive label flip → Task 2 (`.dark --primary-foreground`/`--destructive-foreground`). ✔
- AA verification (both themes, scripted) → Task 1 + run in Tasks 2/8. ✔
- StatusBadge amber AA bug fix → Task 3. ✔
- NavUser toggle → Task 6. ✔
- Surface sweep → Task 7. ✔
- DESIGN.md update + dist rebuild → Task 8. ✔
- Non-goal `AuthBackdrop` untouched → asserted in Task 7. ✔

**Placeholder scan:** No "TBD"/"add error handling"/"similar to". The one conditional ("if a contrast pair fails, lower L by 0.02…") is an explicit, bounded instruction with the math behind it, not a placeholder. The NavUser test step hedges on the existing file's helper names because those are unknown without reading the file at execution time — the assertion (`[data-test="theme-dark"]` exists / `findComponent(ThemeToggle)`) is concrete.

**Type consistency:** `ThemeMode = 'light' | 'dark' | 'auto'` is defined in `useTheme.ts` and consumed identically in `ThemeToggle.vue` (options use `'light'`/`'auto'`/`'dark'`). `setMode`/`mode` names match across composable, toggle, and tests. Token names referenced in Task 3 (`bg-sage-50`, `text-amber-700`, `bg-tide-50`, …) are all defined in Task 2's `@theme`. `data-test="theme-dark"` matches between `ThemeToggle.vue`, its test, and the NavUser test.

---

## Execution Notes

- No worktree: per project convention this work happens on `master` (the repo carries an unpushed range there).
- Commits omit any `Co-Authored-By` trailer (standing user rule).
- Flaky `pkg/server` Go suite is unrelated to this FE work; the gate here is `go build`/`go vet` + vitest + the contrast script, not the full Go test suite.

---

## Review follow-ups (non-blocking; final whole-implementation review = SHIP-WITH-FOLLOWUPS)

The 8 tasks landed and the final Opus review approved with these deferred items. The one **Important** finding (the `info` StatusBadge stayed light in dark mode because the Tide ramp's `-50/-700` lack `.dark` overrides) was **fixed in-cycle** — the variant now uses the dark-aware, gate-tested `bg-info text-info-foreground` (commit after Task 8). Remaining, intentionally deferred:

- **F-1 (Minor) — back-compat brand/state aliases are stuck at their light values in dark mode.** The un-numbered aliases `--color-sage / -amber / -rose / -tide / -tide-strong / -ember` are defined only in `@theme`, not re-declared in `.dark`. Components still using `text-sage` (status "Saved/Created" lines, ~20 files), `text-amber`, `text-tide`/`-strong`, and `text-ember`/`bg-ember` brand marks (~25 sites total) therefore do NOT lighten in dark mode. All currently **pass WCAG AA** stuck-at-light (computed: `text-sage` 5.20:1, `text-amber` 8.17:1, `text-ember` ~6.3:1), so this is a visual-consistency gap, not a contrast failure. The one borderline is `SudoModal.vue`'s `text-tide-strong` glyph on a `bg-tide/10` circle at ~2.98:1 vs the 3:1 non-text bar (decorative icon beside a text title). **Fix when ready:** add `.dark` overrides lightening those six aliases (~`sage 0.80 / amber 0.84 / rose 0.76 / tide & tide-strong 0.80 / ember 0.78`), and FIRST audit each `bg-{ember,tide,...}` solid-fill usage so a fill+label pair doesn't lose contrast; then add the corresponding dark text pairs to `check-contrast.mjs` so the gate covers what ships.
- **F-2 (Minor) — first-paint flash for dark-mode users.** CSP is `script-src 'self'`, so there is no inline pre-paint script; the `.dark` class is applied during Vue bootstrap (`App.vue` → `useTheme()`). A dark-preferring user may see a brief light flash. Accepted tradeoff (documented in `useTheme.ts`); avoiding it cleanly would need a nonce'd inline script.
- **F-3 (Minor, pre-existing) — `EditProfileDialog.vue` `hover:bg-muted/50`** uses the `--color-muted` text token as a hover background rather than the intended `--muted`/sunken surface. Works in both themes; semantically loose. Tidy opportunistically.
- **Pending (human):** the live in-browser visual pass across every surface in BOTH themes (`/welcome`, login/enroll/consent threshold pages, account + admin views, dialogs, the account-menu theme toggle). This is the only acceptance step a subagent could not perform.
