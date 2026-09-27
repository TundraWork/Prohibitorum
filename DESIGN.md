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
(`bg-accent-soft`, icon in `text-accent-soft-foreground`, the pair HeroUI draws
on that tint) — the only place the product's colour appears in the shell, and a
step *above* the rail where `default` would read as a hole punched in it. Hover
is half of that tint, so it previews the selection. Rail labels are
`text-foreground/85` rather than `muted`: on the secondary surface that token
measures about 4.2:1 in light mode, under the body-text floor. Unselected icons
are `text-foreground/60` and group headings `text-foreground/70`, so both clear
their floors (3:1 and 4.5:1) in either theme.

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

A list of addresses — an OIDC client's redirect and post-logout URIs — is one
input per address, each with its remove button on the input's centre line and
an "Add address" button under the last row. A row's message is drawn under that
row, so the button never moves out of line, and only the input at fault is
marked. A list the client cannot do without keeps its last row, and that row's
disabled remove button says why in a tooltip. A closed set of choices that each
need a word of explanation, such as the OIDC scopes, is a `secondary`
`CheckboxGroup` laid out like HeroUI's add-ons demo: the value in monospace,
since it is the literal string a client sends, with one line under it saying
what it grants, both inside the label and the label across the row, so a press
anywhere on the row toggles the box. The checkbox is named by the value alone;
the line under it stays its description. A choice the product requires is
`isReadOnly` and ticked, not disabled, which would fade it into looking
unavailable.

An open list of short literal values — an identity provider's scopes and its
allowed email domains — is `TagListField`: HeroUI's `TagGroup` with each value
as a monospace tag and its own remove button, then a `secondary` `InputGroup`
whose suffix is an "Add" button. Enter or the button adds the typed value after
the field's own check; a value it refuses stays in the input, marked, with the
reason under it. Values are kept exactly as typed, a value the list cannot do
without (`openid`) is drawn without a remove button, and text left in the input
stops the save with "Press Enter to add it, or clear the box." rather than being
added or dropped for the reader.

A closed set of choices whose cost has to be read before picking is a
`secondary` `Select` whose options are two lines tall — the name, then a
`Description` saying what picking it does — held to the trigger's width, with
the trigger showing the name alone. `OptionSelectField` draws it for all
three places that take this shape: an OIDC client's subject source and
forward-auth's `Remote-User` (through `PrincipalSourceField`), and an identity
provider's provisioning mode and client authentication method. A name that is a
literal protocol value, such as `client_secret_basic`, is monospace. An option
the form's current state rules out is disabled in the list, and the save says
why rather than changing the value for the reader.

An optional override of a default is hidden until the reader adds it. An
identity provider's endpoints under discovery draw no inputs at first; an
outline "Override an endpoint" `Dropdown` adds one row per endpoint, each row an
input with its visible label and its remove button on the input's centre line,
and the menu disables what is already overridden. Only when the reader chooses
to enter every endpoint by hand are all four fields drawn.

### Cards / Containers

Default cards use the surface color, control radius, and surface shadow.
Content has 16px padding and 12px gaps; card titles use compact medium-weight text.

`ConsoleCard` caps its content at the reading measure (`max-w-lg`) and takes
`wide`, which lifts the cap to the card's own width. The default is for a form
read top to bottom: one value per line. `wide` is for the two kinds of block
that shape does not fit — content laid out in columns the eye compares down the
page, like the SAML attribute map, and rows that have to line up with each
other. Widening one such card keeps the page to a single content width, since
the lists beside it already run the full column (`max-w-4xl` in `main`).
A row editor laid out in columns, like the attribute map, switches on its own
container's width rather than the viewport's: below the breakpoint each row
becomes a `Fieldset` with a numbered legend and a visible label on every
control, since a stack of boxes under no column names says nothing about which
is which. Both layouts are rendered and only one is displayed.
A forward-auth application's token scopes are the same shape at two columns,
name and description: a `wide` card on the detail page, and the create page's
reading measure, which is why that editor switches at 28rem of its own width.
An OIDC application's claim aliases are the same editor again, at two equal
columns — both halves are claim names — with the name input and the source
select in monospace at the library's own control height. The select's trigger
draws the claim name as plain text rather than a copy of the option, whose
hidden check mark would make it a pixel taller than the input beside it.

A card holding several blocks names each one with an `ItemList` title. Three
unlabelled cards stacked under one section heading read as one undifferentiated
pile: the heading names the section, not the block, so each block carries its
own name inside its card. The access policy panel — a restriction row plus the
group and manager lists — follows this on all three application kinds.

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
`CopyValue`: a read-only field with a visible `Label` over a `secondary`
`InputGroup`, so it lines up with the fields beside it. A click anywhere on the
field selects the value and copies it; the copy icon at the trailing edge is
decoration, not a button, and turns into a check with a brief "Copied" tooltip
below it. A copy the clipboard refuses shows a `SurfaceAlert` under the field.

An identity provider's diagnostics are an `ItemList` drawn exactly like the
danger zone under them: one row per diagnostic — the effective configuration,
the connection test — with its icon, its name, one line saying what it does
(the configuration's becomes "Read …" once it has been read), and its action at
the trailing edge. What a diagnostic finds opens in a dialog rather than
growing under the row, so the section keeps one shape before and after: the
configuration as a three-column list (what the value is, the value in
monospace, a soft `Chip` naming where it came from) that stacks name and source
over the value on a narrow screen, followed by the test callback address; a
test as its overall state, then one row per stage with an outcome icon, the
stage's measured details in muted text, and its status word, coloured only
when the stage failed. Each dialog's footer holds "Close" and the action again.
A diagnostic or a danger-zone action that fails says so in the console's error
toast only; the row and the dialog keep their shape.

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
composition and narrow-screen behavior. Its entries are router links drawn with
`buttonVariants({ variant: "ghost" })`, so they open in a new tab and keep the
button's focus ring and press scale; selected entries are medium weight and the
rest regular. The management entries sit under three headings — directory,
applications, system — and a heading with nothing visible under it is left out.
The instance avatar, the entry icons, the headings and the account avatar share
one edge 20px into the rail. On a narrow screen the same list opens in a drawer
on the rail's gray, with 44px rows. A rail too long for the window follows
HeroUI's overflowing tabs: the edge with more behind it carries a full-row strip
with a chevron that scrolls towards it, fading into the list, and the scrollbar
shows only while the rail moves. The rail
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

A consequential change made in a form is confirmed when it is saved, not when
it is made. The control stays free to move — a reader comparing options is not
committing to one — and while the form holds such a change its save button is
`SubmitButton` with `tone="warning"`: the theme's warning colour and a
`TriangleAlert` before the label, so the state is not carried by colour alone.
Submitting checks the form, then opens a `warning` `ConfirmDialog` naming the
consequence; the write happens from the dialog, and cancelling leaves the change
in the form, unsent. A changed OIDC subject source or forward-auth
`Remote-User`, a saved token scope removed, and a saved OIDC scope removed are
saved this way.

### Step-up verification

A write the server guards with a fresh sudo window stops at `SudoDialog`, the
console's only prompt to confirm who is signed in. `ShieldCheck` stays beside
the heading as the dialog's `Modal.Icon`, and the header says why the check is
needed. The methods the account has are side by side in the body, each with
its own full-width control: a passkey button with a `Fingerprint` icon, then a
rule with "or" in the middle, then the current password and authenticator code
with the submit button right under them. An account with one method sees only
that method and no rule. One method leads, with the only `primary` button and
the focus the dialog opens on: the passkey when the browser can use one, and
otherwise the password, whose submit button is `secondary` while the passkey
leads. A passkey the browser cannot use is a disabled `secondary` button. The dialog has no footer and
no cancel button: the close button in the corner and Escape close it. While a
verification or the replayed write is in flight the dialog is locked, the
close button disabled and Escape ignored, and the other method's controls are
disabled.

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
