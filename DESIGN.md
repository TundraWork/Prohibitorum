---
name: Prohibitorum
description: Calm, exact, welcoming identity for a self-hosted org IdP. Warm in the hand, precise in the word.
colors:
  # Brand
  tide: "oklch(0.55 0.118 205)"          # primary teal — fills, current selection, focus
  tide-strong: "oklch(0.47 0.130 205)"   # text-weight primary, links, hover/active (AA on white)
  ember: "oklch(0.70 0.150 42)"          # warm accent — human / brand moments, used scarcely
  # State
  sage: "oklch(0.62 0.130 150)"          # success / confirmed / session-active
  amber: "oklch(0.76 0.140 75)"          # caution / pending / expiring
  rose: "oklch(0.58 0.180 22)"           # danger / revoked / failed / clone-warning
  # Neutral (light mode)
  bg: "oklch(1 0 0)"                     # pure white — the surface carries no warmth
  surface: "oklch(0.985 0.005 205)"      # cards, panels
  sunken: "oklch(0.965 0.006 205)"       # sidebar / toolbar (the second neutral layer)
  border: "oklch(0.920 0.006 205)"       # hairline dividers, input strokes
  ink: "oklch(0.22 0.015 230)"           # body text (~12:1 on bg)
  muted: "oklch(0.50 0.012 230)"         # secondary text (~4.5:1 on bg — AA, not the 3.5 floor)
typography:
  display:
    fontFamily: "Hanken Grotesk, ui-sans-serif, system-ui, -apple-system, sans-serif"
    fontSize: "2rem"
    fontWeight: 600
    lineHeight: 1.15
    letterSpacing: "-0.01em"
  headline:
    fontFamily: "Hanken Grotesk, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1.5rem"
    fontWeight: 600
    lineHeight: 1.2
    letterSpacing: "-0.005em"
  title:
    fontFamily: "Hanken Grotesk, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1.125rem"
    fontWeight: 600
    lineHeight: 1.3
  body:
    fontFamily: "Hanken Grotesk, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1rem"
    fontWeight: 400
    lineHeight: 1.55
  label:
    fontFamily: "Hanken Grotesk, ui-sans-serif, system-ui, sans-serif"
    fontSize: "0.8125rem"
    fontWeight: 500
    lineHeight: 1.3
    letterSpacing: "0.01em"
  code:
    fontFamily: "IBM Plex Mono, ui-monospace, SFMono-Regular, Menlo, monospace"
    fontSize: "0.875rem"
    fontWeight: 450
    lineHeight: 1.5
rounded:
  sm: "6px"
  md: "10px"
  lg: "14px"
  full: "9999px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "16px"
  lg: "24px"
  xl: "40px"
components:
  button-primary:
    backgroundColor: "{colors.tide-strong}"
    textColor: "{colors.bg}"
    rounded: "{rounded.md}"
    padding: "10px 18px"
  button-primary-hover:
    backgroundColor: "oklch(0.42 0.135 205)"
    textColor: "{colors.bg}"
    rounded: "{rounded.md}"
    padding: "10px 18px"
  button-ghost:
    backgroundColor: "transparent"
    textColor: "{colors.tide-strong}"
    rounded: "{rounded.md}"
    padding: "10px 18px"
  input:
    backgroundColor: "{colors.bg}"
    textColor: "{colors.ink}"
    rounded: "{rounded.md}"
    padding: "10px 12px"
  badge-success:
    backgroundColor: "oklch(0.95 0.040 150)"
    textColor: "oklch(0.40 0.100 150)"
    rounded: "{rounded.full}"
    padding: "2px 10px"
---

# Design System: Prohibitorum

## 1. Overview

**Creative North Star: "The Welcoming Vault"**

A vault is precise, and you trust it without thinking. But this one has its
lights on and a person at the desk who is glad you came. Prohibitorum guards
identity for a small org, and the interface carries a deliberate split: the
*interaction* is warm and human (open spacing, forgiving flows, reassuring
feedback, plain guidance when something breaks), while the *language* stays
quiet, exact, and free of hype. Warmth in the hand; precision in the word.
Calm, exact, welcoming.

The color system encodes that split. A calm teal ("Tide") carries every
action, selection, and focus state. A single warm accent ("Ember") is the
human moment: the brand mark, a welcome on the enrollment screen, the rare
note of personality. The surface itself stays pure white and carries no
warmth at all, because warmth that lives in the background is the AI-cream
cliché; here warmth lives in the accent, the copy, and the breathing room.
Three state colors (Sage, Amber, Rose) are reserved for things that are
literally true about a credential or session: confirmed, pending, revoked.

This system explicitly rejects the **heavy enterprise admin console**
(Keycloak / old Authentik: gray-on-gray, dense tabbed sprawl, everything on
one screen) and the **dark hacker / terminal aesthetic** (neon-on-black,
monospace-everything, security theater). Guarding identity is serious work
that should look calm and ordinary, not dramatic. It also rejects the
fintech navy-and-gold reflex and the AI-purple-on-white attractor, which is
why the brand color is a fresh teal rather than the obvious indigo.

**Key Characteristics:**
- Pure-white surface; warmth carried by accent + copy + space, never by the background.
- One calm primary (Tide) for all actions; one scarce warm accent (Ember) for human moments.
- A deliberate, semantic palette: every chromatic role maps to a real state, none is decorative.
- Single humanist sans for everything; mono reserved for things you read or copy verbatim.
- Keyboard-first: a visible, high-contrast focus ring is non-negotiable on every interactive element.
- Flat at rest; depth appears only as a response to state.

## 2. Colors

A calm teal foundation with one warm human accent over a clean white surface; chromatic roles are semantic, never decorative.

### Primary
- **Tide** (`oklch(0.55 0.118 205)`): the brand teal. Primary action fills, the current/selected item, focus rings, progress. Calm and fresh, not corporate-navy, not a generic UI-kit green. Filled Tide buttons take a **white** label (Helmholtz-Kohlrausch: dark text on a saturated mid-tone reads muddy).
- **Tide Strong** (`oklch(0.47 0.130 205)`): the text-weight and button variant. Use for link text, primary button fills carrying normal-weight labels, and hover/active, anywhere Tide must pass 4.5:1 on white. Tide at 0.55 passes only large-text 3:1.

### Secondary
- **Ember** (`oklch(0.70 0.150 42)`): the one warm accent. The brand mark, the welcome moment on enrollment/login, a sparing highlight. This is where the product's human warmth lives. It is a *brand* color, not a *state* color, it never signals success or danger.

### Tertiary (state)
- **Sage** (`oklch(0.62 0.130 150)`): success, confirmed, session-active, passkey verified.
- **Amber** (`oklch(0.76 0.140 75)`): caution, pending, expiring (an invitation about to lapse, a password+TOTP fallback still active).
- **Rose** (`oklch(0.58 0.180 22)`): danger, revoked, failed verification, clone-warning on a suspect authenticator.

### Neutral
- **Ink** (`oklch(0.22 0.015 230)`): body text and headings. ~12:1 on white.
- **Muted** (`oklch(0.50 0.012 230)`): secondary text, captions, placeholders. ~4.5:1 on white, deliberately AA-readable, not the elegant-but-illegible light gray.
- **Border** (`oklch(0.920 0.006 205)`): hairline dividers and input strokes.
- **Surface** (`oklch(0.985 0.005 205)`) / **Sunken** (`oklch(0.965 0.006 205)`): cards/panels, and the cooler second layer for the sidebar and toolbars.
- **Background** (`oklch(1 0 0)`): pure white.

> Dark mode mirrors these roles on a cool near-black (`bg ≈ oklch(0.16 0.008 230)`, `ink ≈ oklch(0.95 0.005 230)`), with Tide and Ember lightened ~0.12–0.15 L to hold contrast. Exact dark tokens are pinned once they're implemented in the theme.

### Named Rules
**The Warm-Word, Cool-Hand Rule.** Warmth is carried by the Ember accent, the copy, and the spacing. Never by tinting the background. The surface is pure white (`oklch(1 0 0)`); a warm-cream body background is forbidden.

**The Scarce Accent Rule.** Ember appears on no more than a couple of elements per screen. Its rarity is the point. Tide carries the work; Ember marks the human moment. An Ember used for decoration is a bug.

**The State-Has-a-Color Rule.** Sage, Amber, and Rose are reserved for real credential/session states. They are never decorative, and color is never the only signal, always pair with text, an icon, or shape (color-blind users, and the WCAG 2.2 AA bar).

**The Threshold Exception (scoped Drenched override).** The shared centered auth surfaces (login, enroll, consent, logout, error) carry a full-bleed painted scenery background behind a soft, near-opaque card, a deliberate, *Drenched* exception to the pure-white rule above. It applies ONLY to those threshold pages; the authenticated app stays pure white. Legibility is non-negotiable: a contrast scrim guarantees the card and header hold WCAG 2.2 AA over any image. The painting supplies warmth; the card keeps the restrained system (Tide controls, Tide focus ring, Ember used only in the brand mark). Implemented via `components/AuthBackdrop.vue` (drop a real `src/assets/auth-scene.*` to replace the painterly CSS placeholder).

## 3. Typography

**Display / Body Font:** Hanken Grotesk (with `ui-sans-serif, system-ui, -apple-system, sans-serif` fallback)
**Label/Mono Font:** IBM Plex Mono (with `ui-monospace, SFMono-Regular, Menlo, monospace` fallback)

**Character:** One warm humanist sans carries everything, headings, labels, body, data, so the interface reads as a single calm voice. The mono is not decoration: it is the typeface of things the user must read or type *exactly*. (Hanken Grotesk / IBM Plex Mono are the committed direction; confirm or swap the exact families at implementation.)

### Hierarchy
A fixed rem scale (product UI views at consistent DPI; fluid headings shrink badly in a sidebar). Ratio ≈ 1.2–1.3.
- **Display** (600, `2rem`, 1.15): the one big moment per screen, page title on auth/enrollment, the dashboard section header.
- **Headline** (600, `1.5rem`, 1.2): card and panel titles.
- **Title** (600, `1.125rem`, 1.3): list-row headings, form-section labels.
- **Body** (400, `1rem`, 1.55): prose and descriptions. Cap measure at 65–75ch on long copy (error explanations, consent descriptions).
- **Label** (500, `0.8125rem`, +0.01em): buttons, field labels, table headers, badges. Sentence case, never ALL CAPS sentences.
- **Code** (450, `0.875rem`, mono): TOTP codes, recovery codes, enrollment tokens, key IDs, client IDs, JWKS, redirect URIs.

### Named Rules
**The Code-Gets-Mono Rule.** Anything the user reads or copies verbatim, a TOTP code, a recovery code, an enrollment token, a key ID, a redirect URI, is set in IBM Plex Mono. Prose is never mono. Mono is the signal "this is exact; transcribe it exactly."

**The One-Voice Rule.** Hanken Grotesk does headings, body, labels, and data. Hierarchy comes from size and weight, not from a second sans-serif. No display faces in UI labels.

## 4. Elevation

Flat by default. Surfaces sit directly on the background separated by the Border hairline and the Sunken/Surface tonal step, not by resting shadows. Depth is a *response to state*: it appears when an element lifts (hover on an interactive card), gains focus, or floats above the page (dialog, popover, dropdown, toast). This keeps the resting interface calm and makes motion meaningful when it happens.

### Shadow Vocabulary
- **Raised** (`box-shadow: 0 1px 2px oklch(0.22 0.015 230 / 0.06), 0 2px 8px oklch(0.22 0.015 230 / 0.06)`): hover/active on interactive surfaces.
- **Overlay** (`box-shadow: 0 8px 32px oklch(0.22 0.015 230 / 0.14)`): dialogs, popovers, command menus, anything floating above the page.

### Named Rules
**The Flat-Until-It-Acts Rule.** A surface at rest has no shadow. The moment it becomes interactive (hover), focused, or floating, it earns elevation. If a card has a resting drop-shadow for "depth," the shadow is wrong.

## 5. Components

Built on **shadcn-vue / Reka UI** (Tailwind v4). The system is applied by aliasing shadcn-vue's semantic CSS variables (`--primary`, `--ring`, `--destructive`, …) onto the Tide/neutral OKLCH tokens in `src/assets/main.css`, restyling the vendored primitives through tokens rather than rebuilding them. *(Exact per-component token specs are deferred until they live in code. The direction below is binding.)*

### Buttons
- **Shape:** gently rounded (`10px` / `{rounded.md}`), friendly but not pill-shaped.
- **Primary:** Tide-Strong fill, white label, `10px 18px`. The single most prominent action per view.
- **Hover / Focus:** darken to `oklch(0.42 0.135 205)` on hover (150–250ms); focus shows the Tide focus ring (see Inputs). Never rely on color shift alone for focus.
- **Ghost / Secondary:** transparent fill, Tide-Strong label, subtle Surface tint on hover. For secondary actions and toolbar controls.
- **Danger:** Rose fill (white label) only for destructive confirms (revoke session, delete credential, disable account).

### Cards / Containers
- **Corner Style:** `14px` (`{rounded.lg}`).
- **Background:** Surface on the white page; the sidebar uses Sunken.
- **Shadow Strategy:** flat at rest (see Elevation). Border hairline separates; shadow only on hover/lift.
- **Border:** 1px Border. Never a colored side-stripe.
- **Internal Padding:** `24px` (`{spacing.lg}`); compact lists may use `16px`.

### Inputs / Fields
- **Style:** white fill, 1px Border, `10px` radius, Ink text, Muted placeholder (at 4.5:1, never lighter).
- **Focus:** Border shifts to Tide plus a 2px Tide focus ring (`box-shadow: 0 0 0 3px oklch(0.55 0.118 205 / 0.25)`). This is the keyboard-first commitment made visible.
- **Error:** Border and helper text shift to Rose, paired with an icon and a plain-language message, never color alone.

### Navigation
- **Style:** persistent left sidebar on Sunken; sections role-gated (member items always; admin items appear only for admins, same vocabulary, more depth).
- **States:** current route marked with a Tide indicator plus weight change (not color alone); hover raises a subtle Surface tint. Full keyboard traversal with visible focus.
- **Mobile:** sidebar collapses to a drawer (structural responsive behavior, not fluid type).

### Codes & Tokens (signature)
The one place this app diverges from a generic dashboard. TOTP codes, recovery-code lists, enrollment tokens, and key/client IDs render in IBM Plex Mono inside a Sunken, `10px`-radius field with a copy affordance (verb-labelled, e.g. "Copy enrollment link"). Recovery codes shown once are visually marked as one-time. The mono + sunken treatment says "transcribe this exactly."

## 6. Do's and Don'ts

### Do:
- **Do** keep the page background pure white (`oklch(1 0 0)`); carry warmth through Ember, copy, and spacing.
- **Do** reserve Ember for human/brand moments, no more than a couple per screen (The Scarce Accent Rule).
- **Do** use Tide-Strong (`oklch(0.47 0.130 205)`) for any teal that carries text (links, primary labels); reserve Tide 0.55 for fills and large text.
- **Do** set every verbatim code/token/ID in IBM Plex Mono (The Code-Gets-Mono Rule).
- **Do** give every interactive element a visible, high-contrast focus ring; keyboard operability through every passkey/TOTP flow is the bar (WCAG 2.2 AA + keyboard-first).
- **Do** pair every state color with text, icon, or shape, color is never the only signal.
- **Do** write security and error copy in plain language: what happened, what to do next, no jargon, no blame.
- **Do** keep surfaces flat at rest; let shadow appear only on hover, focus, or float.

### Don't:
- **Don't** build the **heavy enterprise admin console**: gray-on-gray, dense tabbed config sprawl, everything on one screen (the Keycloak / old-Authentik look). Reveal admin power progressively.
- **Don't** use the **dark hacker / terminal aesthetic**: neon-on-black, monospace-everything, "cyber" security theater.
- **Don't** ship the generic SaaS look: warm-cream background, hero-metric template (big-number + gradient), identical icon-card grids.
- **Don't** use consumer-login playful gradients, mascots, or gradient text. Warmth here is space and tone, not decoration.
- **Don't** drift back to the obvious hues: navy-and-gold fintech, AI-purple-on-white, or a generic UI-kit green. The brand is Tide teal on purpose.
- **Don't** use a colored `border-left`/`border-right` stripe on cards, alerts, or list items. Full border, background tint, or icon instead.
- **Don't** use light-gray body or placeholder text "for elegance." Muted is `oklch(0.50 0.012 230)` and holds 4.5:1.
- **Don't** put a resting drop-shadow on a card for "depth" (The Flat-Until-It-Acts Rule).
