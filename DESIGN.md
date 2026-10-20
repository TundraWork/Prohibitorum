---
name: Prohibitorum
description: A quiet, exact identity console on coss ui, with a sign-in page each instance can dress.
colors:
  primary: "oklch(26.9% 0 0)"
  primary-foreground: "oklch(98.5% 0 0)"
  background: "#ffffff"
  foreground: "oklch(26.9% 0 0)"
  sidebar: "oklch(98.5% 0 0)"
  sidebar-foreground: "#727272"
  card: "#ffffff"
  popover: "#ffffff"
  muted: "rgb(0 0 0 / 4%)"
  muted-foreground: "#676767"
  accent: "rgb(0 0 0 / 4%)"
  border: "rgb(0 0 0 / 8%)"
  input: "rgb(0 0 0 / 10%)"
  ring: "oklch(70.8% 0 0)"
  destructive: "oklch(63.7% 0.237 25.331)"
  destructive-foreground: "oklch(50.5% 0.213 27.518)"
  warning: "oklch(76.9% 0.188 70.08)"
  warning-foreground: "oklch(55.5% 0.163 48.998)"
  success: "oklch(69.6% 0.17 162.48)"
  success-foreground: "oklch(50.8% 0.118 165.612)"
  info: "oklch(62.3% 0.214 259.815)"
  info-foreground: "oklch(48.8% 0.243 264.376)"
  dark-background: "#141414"
  dark-foreground: "oklch(97% 0 0)"
  dark-sidebar: "#111111"
  dark-card: "#181818"
  dark-popover: "#1d1d1d"
  dark-muted: "rgb(255 255 255 / 4%)"
  dark-muted-foreground: "#818181"
  dark-border: "rgb(255 255 255 / 6%)"
  dark-input: "rgb(255 255 255 / 8%)"
  dark-destructive-foreground: "oklch(70.4% 0.191 22.216)"
  dark-warning-foreground: "oklch(82.8% 0.189 84.429)"
  dark-success-foreground: "oklch(76.5% 0.177 163.223)"
  dark-info-foreground: "oklch(70.7% 0.165 254.624)"
typography:
  headline:
    fontFamily: '"Cal Sans", "Noto Sans SC", ui-sans-serif, system-ui, sans-serif'
    fontSize: "24px"
    fontWeight: 600
    lineHeight: "32px"
    letterSpacing: "-0.025em"
  dialog-title:
    fontFamily: '"Cal Sans", "Noto Sans SC", ui-sans-serif, system-ui, sans-serif'
    fontSize: "20px"
    fontWeight: 700
    lineHeight: 1
  title:
    fontFamily: '"Cal Sans", "Noto Sans SC", ui-sans-serif, system-ui, sans-serif'
    fontSize: "14px"
    fontWeight: 600
    lineHeight: "20px"
  body:
    fontFamily: '"Cal Sans", "Noto Sans SC", ui-sans-serif, system-ui, sans-serif'
    fontSize: "14px"
    fontWeight: 400
    lineHeight: "20px"
  label:
    fontFamily: '"Cal Sans", "Noto Sans SC", ui-sans-serif, system-ui, sans-serif'
    fontSize: "14px"
    fontWeight: 500
    lineHeight: "20px"
  caption:
    fontFamily: '"Cal Sans", "Noto Sans SC", ui-sans-serif, system-ui, sans-serif'
    fontSize: "12px"
    fontWeight: 400
    lineHeight: "16px"
  value:
    fontFamily: '"Paper Mono", ui-monospace, Menlo, monospace'
    fontSize: "13px"
    fontWeight: 400
  value-list:
    fontFamily: '"Paper Mono", ui-monospace, Menlo, monospace'
    fontSize: "12px"
    fontWeight: 400
rounded:
  sm: "6px"
  md: "8px"
  lg: "10px"
  xl: "14px"
  2xl: "16px"
  stage-card: "22px"
  stage-frame: "28px"
  full: "9999px"
spacing:
  frame-inset: "4px"
  "2": "8px"
  "3": "12px"
  "4": "16px"
  panel: "20px"
  "6": "24px"
  stage-card: "28px"
  "8": "32px"
  "10": "40px"
components:
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.primary-foreground}"
    rounded: "{rounded.lg}"
    height: "32px"
    padding: "0 11px"
  button-outline:
    backgroundColor: "{colors.popover}"
    textColor: "{colors.foreground}"
    rounded: "{rounded.lg}"
    height: "32px"
    padding: "0 11px"
  button-destructive:
    backgroundColor: "{colors.destructive}"
    textColor: "#ffffff"
    rounded: "{rounded.lg}"
    height: "32px"
  input:
    backgroundColor: "{colors.background}"
    textColor: "{colors.foreground}"
    rounded: "{rounded.lg}"
    height: "32px"
  frame:
    backgroundColor: "{colors.muted}"
    rounded: "{rounded.2xl}"
    padding: "{spacing.frame-inset}"
  frame-panel:
    backgroundColor: "{colors.background}"
    textColor: "{colors.foreground}"
    rounded: "{rounded.xl}"
    padding: "{spacing.panel}"
  well:
    backgroundColor: "{colors.muted}"
    rounded: "{rounded.xl}"
    padding: "16px"
  badge-success:
    textColor: "{colors.success-foreground}"
    rounded: "{rounded.sm}"
    height: "18px"
  dialog:
    backgroundColor: "{colors.popover}"
    textColor: "{colors.foreground}"
    rounded: "{rounded.2xl}"
    width: "512px"
  stage-card:
    backgroundColor: "{colors.card}"
    textColor: "{colors.foreground}"
    rounded: "{rounded.stage-card}"
    padding: "{spacing.stage-card}"
    width: "440px"
---

# Design System: Prohibitorum

## Overview

**Creative North Star: "The Quiet Console"**

The dashboard is the coss ui registry, vendored at a fixed commit and used as
shipped: a neutral, near-monochrome console where the hierarchy comes from
layered tone rather than from colour or decoration. The page sits on a light
grey canvas, the work sits on an inset white panel, and every titled block is a
coss Frame: a muted band carrying the title, with the content on a white panel
inside it. Status colour appears only where a state needs to be read.

The public pages (sign-in, consent, enrolment, pairing, errors) are the one
place the system dresses up. They draw a stage that floats over a background an
administrator chooses: the page's own canvas, a colour, a gradient preset, a
Bing or Unsplash picture, or uploaded images. The card and the toolbar capsules
can be translucent and frosted. The console checks the text contrast of that
choice before it is saved.

Interaction follows PRODUCT.md's split: the console is exact and restrained,
the copy is plain and reassuring, and colour is never the only signal for a
state.

**Key Characteristics:**
- Monochrome neutrals (neutral-800 primary on white); status hues reserved for status.
- Cal Sans for every word of UI; Paper Mono for values a person copies or compares.
- Depth through tone: canvas, then inset panel, then Frame band, then FramePanel.
- Concentric radii: each inner surface's corner is the outer one minus its inset.
- One block per Frame; never a card inside a card.
- Light and dark themes from one set of semantic tokens.

## Colors

A grey-scale console with four status hues, all taken from Tailwind's palette
through coss's semantic tokens in `dashboard/src/styles/coss.css`.

### Primary
- **Ink** (primary, neutral-800): the primary button, the instance's fallback
  avatar tile, and the foreground text colour. In dark mode it inverts to
  neutral-100 with neutral-800 text on it. There is no brand hue in the console.

### Neutral
- **Paper** (background, white): the inset panel, FramePanels and inputs.
- **Canvas** (sidebar, neutral-50): the page behind the inset panel and the
  sidebar itself, so the navigation and the canvas are one surface.
- **Veil** (muted / accent / secondary, black at 4%): the Frame band, wells,
  hover fills and the dialog footer. Being translucent, it tints whatever it
  sits on, which is how a band reads the same over the canvas and the panel.
- **Hairline** (border, black at 8%; input, black at 10%): every border. Dark
  mode uses white at 6% and 8%.
- **Graphite** (muted-foreground): descriptions, captions and identifiers.
- **Sidebar text** (sidebar-foreground, ink mixed 64% into the canvas):
  unselected navigation labels.

### Status
- **Destructive** (red-500 fill, red-700 text; red-400 text in dark): danger
  sections, destructive buttons, failure alerts.
- **Warning** (amber-500 icon and dot, amber-700 text): consequences of a save,
  unsaved-edit dots, "Not ready" and "Restricted".
- **Success** (emerald-500, emerald-700 text): "Enabled", "Readable", copied checks.
- **Info** (blue-500, blue-700 text): the Admin role badge, accent confirmations.

Status badges are a soft tint of the hue (8%, 16% in dark) with the
`-foreground` text, never a solid fill.

### Named Rules
**The Status-Only Colour Rule.** Hue means state. Chrome, navigation and
layout stay neutral; a coloured surface always says something is enabled,
failing, pending or dangerous, and it says it in words or an icon too.

**The Dot-Plus-Word Rule.** A state that can be scanned in a column ("Enabled",
"Not ready", "Sent to applications", unsaved) carries a 6px dot in the current
colour beside its word. Disabled has no chip in lists: the row recedes to 70%
opacity and the identifier under it never dims.

## Typography

**UI Font:** Cal Sans (variable, 300 to 800), with Noto Sans SC for Chinese,
then the system sans.
**Value Font:** Paper Mono Regular, with ui-monospace fallbacks.

**Character:** Cal Sans gives the console a geometric, slightly warm voice at
small sizes; Paper Mono marks the strings that are data rather than prose.
Both are self-hosted woff2 files in `dashboard/src/styles/fonts`.

### Hierarchy
- **Headline** (600, 24px, tight tracking): the page's only `h1`, from
  PageHeader, ObjectHeader and the public PageHeading.
- **Dialog title** (700, 20px): coss DialogTitle and AlertDialogTitle.
- **Title** (600, 16px): a Section's `h2` in the Frame band, one step above
  body text; the band and the step together separate a block from its rows.
- **Body** (400, 14px from `sm`, 16px below it): descriptions, cells, field text.
  Inputs and buttons are 16px on phones so iOS does not zoom.
- **Label** (500, 14px): buttons, field labels, navigation entries, and the
  title of a list or danger row. A row title is never larger than its action's
  label.
- **Caption** (400, 13/18px, the `text-caption` theme token): a row's detail
  line, a field's description, a hint and an inline error.
- **Small** (400, 12px): rail facts, change counts, separators such as "or
  with your password".
- **Value** (Paper Mono, 13px): identifiers in headers, scopes, URLs, header
  names, JSON, colours typed by hand.
- **Value in a list** (Paper Mono, 12px, muted): the identifier under an
  EntityCell name and CodeValue cells.

### Named Rules
**The Copy-or-Compare Rule.** Paper Mono is for a value a person copies,
pastes or compares between rows: client IDs, entity IDs, slugs, scopes, URLs,
recovery codes, pairing codes. Words, even short ones like "Confidential", stay
in Cal Sans in the foreground colour. The two never swap within a column.

**The No-Kicker Rule.** A page or section starts at its title. There are no
uppercase eyebrows above headings; uppercase appears only inside `Kbd`
shortcut hints.

## Layout

**Console shell.** coss Sidebar in its `inset` variant: a 16rem sidebar on the
canvas and, from `md` (768px), the content on a white inset panel with an 8px
margin, 14px corners and a faint shadow. The panel's header is sticky, 56px
tall, translucent white with backdrop blur and a bottom hairline; it carries
the sidebar trigger, a breadcrumb (section, then record) and, from `sm`, a
240px search field with a `⌘K` hint. Content is one measured column, at most
1152px (`max-w-6xl`), centred, with 24px side padding (40px from `lg`) and
32px vertical padding. It is a size container, which tables measure against.

**Page columns.**
- Member pages (Security, Sessions, Devices, Tokens) use PageBody: one column
  at most 768px (`max-w-3xl`), left-aligned on the breadcrumb's edge, 32px
  between blocks.
- Lists: PageHeader, then a 16px-gap stack of toolbar row and DataTable.
- Object pages: AppDetailLayout, a grid of `minmax(0,1fr)` and a 200px outline
  rail from `lg`, 40px apart, sections 24px apart.
- The sign-in page editor: 380px controls and a sticky preview column from
  `lg`; below `lg` the preview comes first.

**At 1920px** the content column stops at 1152px and centres inside the inset
panel; the canvas takes the remainder. **At 1440px** the column nearly fills
the inset panel and object pages show the rail. **At 390px** the sidebar
becomes an 18rem sheet named in the reader's language, the search field is
hidden, the outline rail is dropped and its facts move under the header,
header actions drop under the title, dialogs become bottom sheets with square
corners, and the public stage fills the width with a 20px card padding.

**Breakpoints:** `sm` 640px, `md` 768px (sidebar), `lg` 1024px (rail,
two-column editor, card position), plus coss's `3xl` 1600px and `4xl` 2000px.

**Rhythm:** a 4px base. 6px Frame bezel, 8px between controls, 12px header
meta, 16px inside wells, 24px between sections, 32px between page regions.

**The One-Edge Rule.** Everything inside a Frame starts on one content edge,
`--frame-inset` (20px from `sm`, 16px below it): the band's icon and title, the
panel's fields, a list row's icon tile and a table's first and last cells. The
band and header cells sit outside the panel's 1px border, so they add it. The
public stage follows the same rule with `--stage-inset` (28px, 20px below
`sm`): the strip above the card and the stay-signed-in band below it start on
the card's content edge, and the band's space above and below its text is
equal.

## Elevation & Depth

The console is layered by tone, with shadow only at the hairline scale.
Resting surfaces carry coss's `shadow-xs/5` (an extra-small shadow at 5%
opacity) plus a 1px inner highlight: black at 4% on the bottom edge in light
mode, white at 6% on the top edge in dark mode. That highlight is what makes a
FramePanel, an input or an outline button read as a raised sheet.

### Shadow Vocabulary
- **Resting sheet** (`shadow-xs/5` plus the 1px edge highlight): FramePanel,
  inputs, outline buttons, badges, toolbar capsules.
- **Floating** (`shadow-lg/5`): popovers, menus, selects, dialogs, sheets.
- **Unsaved bar** (`shadow-lg/10` plus a border ring): the floating reminder at
  the bottom of an object page.
- **Pinned column** (`-8px 0 8px -8px rgb(0 0 0 / 0.15)`): only while a wide
  table can still scroll under its pinned column, driven by a scroll timeline.
- **Frosted stage** (`shadow-2xl` at black 30%, white 40% ring,
  `backdrop-blur-2xl`): the public stage over a picture, gradient or colour.

### Named Rules
**The Four-Layer Rule.** Canvas, inset panel, Frame band, FramePanel. A new
surface finds its place in that stack; it does not add a fifth.

**The No Card-in-Card Rule.** A Section owns exactly one panel. An ItemList or
DataTable inside a Section becomes that panel; anything else is placed on the
Section's own panel. Inside a panel, grouping is done with a well, never with
another bordered card. coss `Card` is vendored but not used by any page.

## Shapes

The base radius is 10px (`--radius: 0.625rem`), with steps at 6, 8, 10, 14 and
16px. Corners are concentric: a Frame (16px) insets its panel by 4px, so the
panel is 12 to 14px; a pseudo-element highlight is the radius minus 1px; the
public stage frame (28px) holds its card (22px) at a 6px inset. Controls are
10px, small buttons and icon buttons 8px, badges 6px, checkboxes 4px, radios,
switches and person avatars round. Application and provider icons are rounded
tiles; people are circles.

A **well** is the one in-panel grouping shape: 14px corners, the 60% veil and a
60% hairline ring (`rounded-xl bg-muted/60 ring-1 ring-border/60`). It holds a
pairing code, a callback preview, an endpoint group, the access summary, or a
rule group. Nested rule groups keep the ring and indent; the fill stops after
two levels.

Borders are hairlines throughout. Header facts are divided by 1px vertical
rules that belong to each item, so a wrapped line never ends on a dangling
separator.

## Components

### Buttons
- **Shape:** gently rounded (10px), 36px tall at every width (coss's desktop
  step-down is removed); touch pointers get a 44px hit area. Section and row
  actions use this size, and a text action in a row is at least 80px wide so a
  two-character label does not shrink to a chip. The 32px compact size is for
  ghost helpers and controls inside a field or rule. A page's primary action is
  40px with its icon set in an inset chip, like the passkey button.
- **Primary:** ink fill, white text, a 16% white inner top highlight.
- **Outline:** popover fill, input-colour border, resting-sheet shadow; hover
  adds the accent veil at 50%.
- **Ghost:** no fill; used for Discard, Clear filters, row menus and back arrows.
- **Destructive:** red fill and white text; **destructive-outline** keeps the
  outline shape with red text and a red-tinted hover.
- **Focus:** a 2px ring in the ring colour with a 1px offset. Disabled is 64%
  opacity. A pending button keeps its width and shows a spinner.

### Inputs / Fields
- **Style:** white fill, input-colour border, 10px corners, resting-sheet shadow.
- **Focus:** the border takes the ring colour with a soft 24% ring.
- **Error:** the destructive border and ring, with the message under the field.
- **Compact values** switch to Paper Mono at 13px from `sm`.
- In dark mode, unselected checkboxes and radios get a 38% white border so the
  boundary reaches 3:1.

### Section
The titled block of every console page, drawn as a coss Frame. The band holds
an optional muted 16px icon, the `h2` title, an optional description and the
block's one primary action on the trailing edge. The panel holds the content.
An optional footer band holds the block's save. `tone="danger"` tints the
whole Frame with destructive at 6% and turns the title red. A Section with
unsaved edits registers itself, which feeds the outline rail and the unsaved
bar. AsyncSection draws the title in every state; while loading it shows three
skeleton bars, and on failure a danger alert with "Try again".

### Consequence footer
A Section footer for a save that does more than store a value: a warning
triangle, a sentence saying what the save will do, then Discard (ghost) and a
primary button named for the action ("Remove scope and save"). `danger` tone
explains a refused save, and the disabled button is described by it. `neutral`
tone has no icon and only says how many changes are waiting.

### ObjectHeader, AppDetailLayout and the rail
ObjectHeader is the top of an object page: the entity's icon tile, the `h1`,
then one meta line of status badges, a copyable Paper Mono identifier (13px,
breaks after slashes) and short facts, separated by hairlines, with the
page's actions on the trailing edge from `sm`. A switched-off object dims its
icon and title to 70%. AppDetailLayout places the header, the sections and,
from `lg`, the 200px OutlineRail: "On this page" links, one per Section,
the current one highlighted, unsaved ones dotted in amber, the danger section
last, and the page's facts under a rule in 12px. The UnsavedBar floats 24px
above the bottom, centred, and leads back to the first unsaved section that
is off screen.

### Lists: DataTable and EntityCell
Lists are coss Tables driven by TanStack's column model. Sorting, filtering
and paging belong to the server; more rows load from a sentinel as it scrolls
into view. Inside a Section the table is the Section's panel; on its own it
draws its own Frame. The leading column is always an EntityCell: icon, name
linking to the record, and the identifier under it in 12px muted Paper Mono,
capped in width with an ellipsis and a tooltip. Disabled rows dim and carry a
small "Disabled" badge; restricted rows show a padlock. The application lists
share 224px data columns so their columns line up. A table wider than its
container pins its leading column, which casts a shadow only while there is
more to scroll. Empty tables say what is missing in one line under an icon
tile and repeat no action from the header.

### Filter toolbar
A wrapping row above the table: a search InputGroup with a search icon
(at least 224px, at most 384px), then FilterPopover, an outline button whose
count says how many filters are set inside it, opening a 320px popover with
a 16px title and the controls stacked 16px apart. A ghost "Clear filters"
appears only while something is set. A failed read shows an inline failure
above the table.

### Dialogs, the sudo step and reveal-once
Dialogs are coss popups: 16px corners, popover fill, at most 512px wide, a
24px header, an optional panel, and a footer band on the 72% veil with the
actions right-aligned (stacked, primary first, on phones). ConfirmDialog
places a status icon beside the heading: red circle for removal, amber
triangle for a reversible change of state, blue info for a second look. Cancel
always sits before the one confirming action.

The **sudo dialog** is mounted once on the console. When the server asks for a
fresh verification it parks the request, offers a passkey button and a
password-and-authenticator form separated by an "or" rule, then replays the
request once. It cannot be closed while verifying or replaying.

**Reveal-once** (recovery codes, personal access tokens) shows a secret the
server never returns again: the value, copy and download controls, and a
"saved it" confirmation that unlocks Continue. Until it is checked, Escape does
nothing, the close button is hidden, and a press outside never dismisses it.

### Public sign-in stage and login appearance
The toolbar has no fill: two 36px capsules with 14px corners carry the
instance identity, the language menu and, unless the settings force a theme,
the theme menu. The stage is a 28px-corner frame with a 6px inset around a
22px-corner card, 440px wide (480px for wide steps), centred from `sm`, and
placed left, centre or right from `lg`. The frame has a strip above the card
(who is asking) and a footer under it (a choice or the decision). Over a
background it is frosted; over the plain canvas it uses the Frame band. The
card is opaque `card` by default, or translucent at a chosen opacity with
optional blur and saturation; translucent controls inside it become
half-opacity fills of the same base. Every public page builds its title row
with PublicStep: an optional ghost back arrow, the 24px heading (which takes
focus on arrival), a 14px muted description, then content and actions. A
picture's credit sits in the bottom corner away from the card from `lg`, and
under the stage below it.

### Sign-in page editor and preview
Settings, Sign-in page: Background (source tiles plus its controls), Card
position, Theme, and Card and capsule surfaces as Sections, each marking its
count of changed settings with an amber dot. Beside them, sticky from `lg`, a
Preview Section renders the real public scene and the instance's first
sign-in step inside an inert frame laid out at 1280 by 800 (desktop) or 390 by
844 (phone), scaled to a 16:10 box. The preview's theme and size toggles sit
in its band; its footer holds the one Save for all sections as a neutral
consequence footer.

### Readability check
The card section's footer measures the WCAG contrast of the card's text
against the worst place behind the card in the preview, working through the
frame's and the card's fill and blur. The verdict is a badge and a sentence:
green "Readable" with the ratio (floored to a tenth) when it reaches 4.5:1,
amber "Hard to read" with the advice to raise the opacity or pick another
background, or an outline "Not measured" when the background cannot be read.
It never guesses.

### Navigation
The sidebar lists the account pages, then the management headings the account
may see. Headings are 12px muted labels, not heading elements. The selected
entry takes the accent veil and full ink; others use the sidebar text colour.
The instance identity sits at the top and the account menu at the bottom
(account pages, language, theme, sign out).

## Do's and Don'ts

### Do:
- **Do** put every console block in a Section, and let a DataTable or ItemList become its panel.
- **Do** group things inside a panel with a well (`rounded-xl bg-muted/60 ring-1 ring-border/60`).
- **Do** use semantic tokens (`bg-muted`, `text-muted-foreground`, `border`) so both themes follow from one class.
- **Do** set identifiers, scopes, URLs and codes in Paper Mono: 13px in panels and headers, 12px muted in list rows.
- **Do** pair every status hue with a word or an icon, and a 6px dot where the state is scanned in a column.
- **Do** name a consequential save for what it does, put its consequence in the Section footer, and confirm irreversible actions in ConfirmDialog.
- **Do** keep corners concentric: an inner radius is the outer radius minus the inset.

### Don't:
- **Don't** put a card or bordered box inside a FramePanel.
- **Don't** use gradients, glows or decorative fills in the console chrome. Gradients belong to the public backdrop presets, which are the sign-in page's own material.
- **Don't** introduce a brand hue in the console; the primary is ink.
- **Don't** put uppercase eyebrows or kickers above titles.
- **Don't** use shadows heavier than `shadow-lg/5` on console surfaces; the frosted stage's large shadow is for the public page only.
- **Don't** use Paper Mono for words, or Cal Sans for values a person copies.
- **Don't** hand-edit `src/components/ui` or `coss.css`; re-copy from the registry and re-apply the documented patches.
