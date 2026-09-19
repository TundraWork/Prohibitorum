---
name: Prohibitorum
description: Calm, exact, welcoming identity for a self-hosted org IdP.
theme:
  library: HeroUI React
  chroma: 0.1
  hue: 204
  lightness: 0.52
  base: 0.003
  radius: extra-small
  formRadius: small
typography:
  sans: "Hanken Grotesk, ui-sans-serif, system-ui, -apple-system, sans-serif"
  mono: "IBM Plex Mono, ui-monospace, SFMono-Regular, Menlo, monospace"
rounded:
  ordinary: "0.25rem"
  field: "0.5rem"
spacing:
  xs: "4px"
  sm: "8px"
  md: "16px"
  lg: "24px"
  xl: "40px"
---

# Design System: Prohibitorum

## Scope

The active application is `dashboard`: React, TypeScript, HeroUI and Tailwind CSS v4. The bilingual component preview at `/` and real public configuration preview at `/preview/api` retain a visible notice that login, enrollment and business pages are temporarily unavailable. Samples must be labelled as examples.

`dashboard-old` preserves the former frontend for reference. New components, styles, imports and builds use only the active application. M2 supplies shared API, Query, Router and Form mechanisms; business pages and the console arrive in later milestones.

## Character

Prohibitorum is calm, exact and welcoming. Open spacing, forgiving interactions and useful feedback make security tasks approachable. Copy states what happened and what to do next, without jargon, blame or hype.

Teal carries primary actions, selection and focus. Neutral surfaces keep attention on the task. State colors describe success, warning or danger and always accompany text, an icon or another visible cue.

Reveal administrative detail progressively. Avoid dense all-in-one forms, decorative metrics, neon security imagery, gradients in text and interchangeable icon-card grids. Warmth comes from typography, copy and spacing.

## Theme and colors

Use `@heroui/react` with `@heroui/styles`. Import Tailwind before HeroUI; centralize the application theme in `dashboard/src/styles`. Components consume HeroUI semantic tokens rather than defining separate palettes.

The approved parameters are `chroma=0.1`, `hue=204`, `lightness=0.52` and `base=0.003`. The primary seed is `oklch(0.52 0.1 204)`; `base` supplies the neutral chroma. Map these through HeroUI's theme mechanism and CSS OKLCH, with explicit light and dark semantic tokens.

Keep canvas, card and recessed surfaces visually distinct. Use the theme's foreground, muted, border, accent, success, warning and danger roles consistently. Neutral surfaces stay cool; brand color is not a substitute for a status label.

Normal text and placeholders must meet WCAG AA's 4.5:1 contrast in both themes; large text and visible control boundaries require 3:1. Choose foregrounds by actual contrast against their fill, including primary buttons, selected tabs, notifications and disabled examples.

M1 exposes dark styling through its preview state. A persisted console appearance menu belongs to the later console milestone. Every theme must preserve readable text and visible keyboard focus.

## Corners and spacing

| Role | Approved setting | CSS value | At a 16px root |
| --- | --- | --- | --- |
| Ordinary components | `extra-small` | `0.25rem` | `4px` |
| Form fields | `small` | `0.5rem` | `8px` |

Set `--radius: 0.25rem` and `--field-radius: 0.5rem` in `dashboard/src/styles/theme.css`. Apply these values explicitly to ordinary components and form controls where HeroUI's multipliers would produce a different radius. Verify the computed styles of buttons, cards, notifications, popovers, inputs and select triggers.

Use the spacing scale above. Cards normally use `24px` internal padding; compact content may use `16px`. Narrow layouts must wrap content and controls without overflowing the viewport.

## Typography

Hanken Grotesk carries English headings, body text, labels and data, with system Chinese fonts in the fallback stack. Bundle fonts as same-origin assets; Vite must not inline font files as data URLs. Use IBM Plex Mono for codes, tokens, IDs and other text copied verbatim.

| Role | Size | Weight | Line height |
| --- | --- | --- | --- |
| Display | `2rem` | 600 | 1.15 |
| Headline | `1.5rem` | 600 | 1.2 |
| Title | `1.125rem` | 600 | 1.3 |
| Body | `1rem` | 400 | 1.55 |
| Label | `0.8125rem` | 500 | 1.3 |
| Code | `0.875rem` | 450 | 1.5 |

Use sentence case. Keep long descriptions around 65–75 characters per line. Hierarchy comes from size, weight and spacing; prose stays in the sans-serif family.

## Components

Use HeroUI components directly and compose application-specific behavior under `dashboard/src/components/custom`. Theme through the shared stylesheet and documented component APIs. Preserve the library's keyboard, focus and accessibility behavior when composing controls.

### Buttons

Make the primary action visually prominent and use secondary or ghost treatments for alternatives. Reserve danger treatments for destructive confirmation. Interactive samples must perform their stated local action; they must not fake server submissions.

### Fields and tabs

Give every field a visible label and associate help or validation text with it. Show errors with text and a visible state change. Keyboard focus must remain distinct from hover, selection and validation states.

Use tabs to separate related areas without stacking every section vertically. Tabs, language controls and popovers must work with keyboard navigation; activating them must preserve current field input where the surrounding page remains mounted.

### Surfaces and notifications

Keep resting surfaces flat, separated by tone or a border. Use elevation for floating popovers, dialogs and notifications; dark-mode elevation also needs a visible surface or border distinction. Avoid colored side stripes on cards or alerts.

Notifications explain the result and any required next step. Query and mutation failures each produce one global notification, including when a form also displays an error. Store error descriptors so open notifications follow language changes; canceled requests stay silent.

Navigation keeps the header visible and immediately displays a content skeleton while route data loads. Public configuration and initialization status load concurrently into shared Query caches; background refresh keeps available data visible. During refresh, the disabled refresh button contains the spinner and localized loading text.

Forms freeze their fields during submission, show loading feedback beside the button, preserve failed input, and focus the first invalid field or error summary. Server errors use explicit field mappings, with unknown locations shown in the summary. The development-only `/__dev/forms` route exercises these mechanisms against isolated accounts.

### Future business pages

The console uses a left navigation area with account information above and language, sign-out and appearance controls below. Narrow layouts use a drawer. Group related settings with tabs, display lists in tables and open creation/editing forms on their own pages.

Codes, recovery values and enrollment links use monospace fields with verb-labelled copy actions. Mark one-time values explicitly. Access, credential and session states must reflect real backend data.

## Language and accessibility

Maintain English and Chinese Lingui PO catalogs together, including accessible names and notification copy. Language changes update visible text and HTML `lang` without changing the URL or clearing current input.

Use Jotai `atomWithStorage` with default storage and `getOnInit: true` for `prohibitorum.locale`. Store `en` or `zh` as JSON strings; select Chinese only for `zh`, otherwise English. Activate the initial language before React mounts and apply cross-tab storage updates.

Use semantic headings, labels and live announcements. Check keyboard focus, floating controls, text contrast and viewport overflow in light and dark modes at narrow and desktop widths. Production verification must use the Go-served UI and its CSP; keep scripts same-origin and allow runtime styles only when a demonstrated need justifies a narrow exception.
