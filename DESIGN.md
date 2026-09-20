---
name: Prohibitorum
description: A calm, exact, welcoming identity interface built with HeroUI.
colors:
  accent: "oklch(52.00% 0.1 204)"
  accent-foreground: "oklch(99.11% 0 0)"
  background: "oklch(97.02% 0.003 204)"
  foreground: "oklch(21.03% 0.003 204)"
  surface: "oklch(100.00% 0.0015 204)"
  muted: "oklch(55.17% 0.006 204)"
  border: "oklch(90.00% 0.003 204)"
  default: "oklch(94.00% 0.003 204)"
  dark-background: "oklch(12.00% 0.003 204)"
  dark-foreground: "oklch(99.11% 0.003 204)"
  dark-surface: "oklch(21.03% 0.006 204)"
  dark-muted: "oklch(70.50% 0.006 204)"
  dark-border: "oklch(28.00% 0.003 204)"
typography:
  title:
    fontFamily: '"Inter Variable", ui-sans-serif, system-ui, sans-serif'
    fontSize: "24px"
    fontWeight: 600
    lineHeight: "32px"
  body:
    fontFamily: '"Inter Variable", ui-sans-serif, system-ui, sans-serif'
    fontSize: "16px"
    fontWeight: 400
    lineHeight: "24px"
  label:
    fontFamily: '"Inter Variable", ui-sans-serif, system-ui, sans-serif'
    fontSize: "14px"
    fontWeight: 500
    lineHeight: "20px"
rounded:
  base: "0.125rem"
  field: "0.25rem"
  control: "0.375rem"
spacing:
  "2": "8px"
  "3": "12px"
  "4": "16px"
  "6": "24px"
components:
  button-primary:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.accent-foreground}"
    rounded: "{rounded.control}"
    padding: "0px 16px"
  input:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.foreground}"
    rounded: "{rounded.field}"
    padding: "8px 12px"
  card:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.foreground}"
    rounded: "{rounded.control}"
    padding: "16px"
---

# Design System: Prohibitorum

## Overview

**Creative North Star: "The Quiet Reception Desk"**

The interface is calm, exact, and welcoming. Restrained, familiar controls help
members complete an identity task and help admins find the controls they need.

This record describes the current HeroUI implementation. The approved Theme
Builder configuration remains `chroma=0.1&hue=204&lightness=0.52&formRadius=small&radius=extra-small&base=0.003`.

**Key Characteristics:**
- Muted teal with cool near-neutral surfaces.
- Inter typography and compact, gently rounded controls.
- Subtle HeroUI shadows and tonal surface layers.
- Shared components across English, Chinese, light, and dark interfaces.

## Colors

### Primary

Muted teal identifies primary actions and focus. HeroUI derives hover and soft
accent states from the theme tokens.

### Neutral

Cool near-neutral backgrounds, white light-mode surfaces, and dark foregrounds
separate content without decorative color. Dark mode uses the corresponding
semantic tokens rather than inverting light-mode colors.

Status colors retain HeroUI's danger, warning, and success roles. Their exact
light/dark values remain in `dashboard/src/styles/theme.css`.

**The Semantic Color Rule.** Use the active theme's semantic tokens for UI states.

## Typography

Inter Variable is loaded locally, with system sans-serif fallbacks for glyphs
outside its coverage. Body text uses the body role; preview page headings use
the title role. Supporting copy is smaller and muted.

Buttons use the label role. Inputs use body-sized text on narrow screens and
smaller text from the small breakpoint, as provided by HeroUI.

## Layout

The public layout is a centered column capped at 64rem, with 16px horizontal
padding increasing to 24px at 640px. Major sections have 24px gaps. Preview
navigation and action rows wrap rather than forcing horizontal overflow.
The public toolbar stays fixed while the page scrolls. Sign-in centers its card
in the window with even space above and below once the card outgrows it; from
1024px only the card area scrolls.

The console uses a separate responsive navigation composition. Preserve its
existing layout and permission behavior when refining shared components.

## Elevation & Depth

Depth comes from subtle HeroUI shadows plus tonal surface layers. Default cards
and fields use the library's surface and field shadows; links and buttons are
flat at rest. Preserve library-owned elevation and reduced-motion behavior.

## Shapes

The theme's base radius and field radius are distinct. HeroUI derives component
radii from the base: observed buttons and cards use the control radius, while
inputs use the field radius. Preserve these derivations rather than applying
the base radius directly to every component.

## Components

### Buttons

Restrained and familiar. Primary actions use the accent pair; outline and ghost
actions keep neutral foregrounds. Standard buttons are 40px high below 768px
and 36px above it. Preserve HeroUI hover, pressed, pending, disabled, and focus states.

### Inputs / Fields

Inputs use semantic field colors, the field radius, and a subtle shadow.
Labels, descriptions, and validation messages remain associated with their
fields. HeroUI owns focus, invalid, hover, and disabled styling.

Fields that sit on a surface, such as those in the sign-in card, use the
secondary variant: the shadow is dropped and the fill follows the surface so
the field reads as part of the card. One-time-code fields follow the same
variant through `InputOTP`.

### Cards / Containers

Default cards use the surface color, control radius, and surface shadow.
Content has 16px padding and 12px gaps; card titles use compact medium-weight text.

### Navigation

Public navigation uses wrapping HeroUI links. Preview tabs use the secondary
variant with the library indicator. Console navigation uses its existing shared
composition, including unavailable states and narrow-screen behavior.

### Notices

Use HeroUI Alert. A single message uses `Alert.Title` inside `Alert.Content`;
add `Alert.Description` only for supplementary text.

## Do's and Don'ts

### Do:
- **Do** preserve HeroUI component defaults and the approved theme export.
- **Do** import `theme.css` after `@heroui/styles`.
- **Do** write custom layouts as Tailwind classes in JSX and share compositions through `dashboard/src/components/custom`.
- **Do** preserve visible keyboard focus, reduced-motion support, and bilingual copy.

### Don't:
- **Don't** copy visual styling from `dashboard-old` or wireframe mockups.
- **Don't** replace the approved palette or component defaults during refinement.
- **Don't** introduce neon security motifs, decorative mascots, or dense enterprise configuration sprawl.
