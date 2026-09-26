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

The console shell reads as three tonal layers, and they are tokens rather than
borders: the rail and header are `surface-secondary` (the chrome), `main` keeps
the page's `background`, and every card is the library's white `surface`. The
header alone closes with a `separator` rule, because it is sticky and content
scrolls under it. The rail's selected entry is the accent's soft tint
(`bg-accent-soft`, icon in `text-accent`) — the only place the product's colour
appears in the shell, and a step *above* the rail where `default` would read as
a hole punched in it. Rail labels are `text-foreground/85` rather than `muted`:
on the secondary surface that token measures about 4.2:1 in light mode, under
the body-text floor.

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

### Record lists and detail pages

A management table leads with an identity cell: `EntityAvatar` with the
entity's icon, its name, and the monospace identifier under it. HeroUI's
`Avatar` keeps its own shape at `size="sm"`; the console rounds it to the
control radius so a list row and an icon card show the same entity the same
way.

A row that is switched off recedes rather than being labelled: the whole
identity cell drops to a fraction of its opacity, icon and text together. It
carries no chip — a status label on every settled row costs the measure and
buries the row that is genuinely wrong — and the recession is decoration on top
of a fact, never the only record of it, so the same word reaches a screen
reader. The state that does get a mark is the one an operator has to act on: a
provider that is not ready cannot be enabled, so it carries `NotReadyChip`,
HeroUI's status-chip anatomy with a dot and a word.

The four federation lists take their shared cells from `ListCells`:
`NotReadyChip`, `CodeValue`, `RedirectCell`, `LinkedAccountsCell`,
`PrincipalSourceCell` and `CertificateExpiryCell`. Each leads with one
diagnostic column answering "is this row set up the way I expect" — a redirect
count, a signing-certificate expiry, a linked-account count — because that fact
differs per kind and a generic column would be empty for someone. A value the
table will clip keeps the whole of itself in a tooltip.

A detail page stacks `Section`s, one per block, with the heading on the page
background and a single `ConsoleCard` under it. The access policy shared by the
three application kinds is `AppAccessPanel`, and a read-only value that is meant
to be copied elsewhere — a Client ID, an Entity ID, a callback address — is
`CopyValue`, HeroUI's `InputGroup` with a copy suffix.

### Loading

A list of records waits in the shape it will arrive in, not behind a spinner:
`ListSkeleton` from `dashboard/src/components/custom` draws an icon-sized block
and two lines per row, and everything that shows a list of records uses it —
`ItemList`, `DataTable` (one placeholder per column), and the panels that read
with `useQuery` and so draw their own pending state. The shimmer is HeroUI's
`skeleton--shimmer`: the class goes on the container and its children take
`animationType="none"`, so one shimmer passes over the whole set rather than each
bar pulsing on its own. The skeleton is `aria-hidden`; a placeholder has nothing
to announce.

A `Spinner` is for a wait that is a process rather than a shape: the route-level
pending component, an in-flight lookup, and the work a `Button` reports through
`isPending`.

### Navigation

Public navigation uses wrapping HeroUI links. Preview tabs use the secondary
variant with the library indicator. Console navigation uses its existing shared
composition, including unavailable states and narrow-screen behavior. The rail
names the instance and the header names the section in view, so each string is
written once: the header carries no eyebrow repeating the instance above its
heading.

### Notices

Use HeroUI Alert. A single message uses `Alert.Title` inside `Alert.Content`;
add `Alert.Description` only for supplementary text.

Inside a card or a dialog, use `SurfaceAlert` from `dashboard/src/components/custom`
instead: the surface shadow is dropped and the alert fills with the status's
soft tint (`bg-*-soft`; the default status uses `bg-surface-secondary`), so it
reads as part of the surface rather than a raised block. Page-level notices on
the background plane keep the library's surface shadow. A shared piece that can
be drawn on either plane, such as `SecretReveal`, takes `onSurface` from the
caller that knows which one it is. In a dialog, `SecretReveal` also takes
`inDialog`: the dialog heading draws the title, and the reveal draws the body and
puts its continue control in `Modal.Footer`, which is where HeroUI keeps dialog
actions.

### Confirmations

A consequential action confirms through `ConfirmDialog` from
`dashboard/src/components/custom`: HeroUI's `AlertDialog` with
`AlertDialog.Icon` beside the heading, the consequence in the body, cancel
(`tertiary`) on the left and the action on the right. The status sets the icon
and the action button: `danger` for what removes something for good, with a
`danger` button; `warning` for a change of state that can be undone, and
`accent` for a step worth a second look, both with a `primary` button.

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
