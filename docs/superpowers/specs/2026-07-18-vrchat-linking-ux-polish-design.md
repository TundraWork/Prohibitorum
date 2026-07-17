# VRChat Linking UX Polish Design

**Date:** 2026-07-18
**Status:** Approved

## Scope

Correct five related presentation flaws in the shared error surface and VRChat user-facing federation flow:

1. Error-panel alignment, spacing, and missing error-code detail.
2. Missing instructions for finding a VRChat profile URL or user ID.
3. Weak hierarchy on the VRChat proof-publishing screen.
4. Missing context for visitors who follow a proof link from another person's VRChat bio.
5. Generic styling for the VRChat federation login button.

The changes are frontend-only. Federation APIs, proof behavior, account linking, public token privacy, and provider configuration remain unchanged.

## Selected Direction

The selected visual direction is a guided utility flow with a structured shared error panel and a guest-first public proof explanation. Product familiarity and task clarity take priority over decorative progress chrome.

## Shared Error Panel

`ErrorPanel.vue` remains the single code-driven error surface. Neutral informational `Alert` uses do not inherit error-specific layout changes.

### Layout

- Use a restrained rose-tinted surface with a low-contrast rose border.
- Reserve space for the dismiss control and position its 44px target at the true top-right.
- Align the visible dismiss mark with the center of the first message line.
- Use a consistent 8px internal step and 12px section step instead of nested 44px action rows and unrelated top margins.
- Keep summary text readable at a normal line height and cap long prose naturally within its container.
- Put `Details` and admin-only `View diagnostic` in one compact, wrapping action row.

### Expanded details

The Details disclosure is available for every error because every public error has a code. Its content is a semantic label/value layout in this order:

1. Error code, always shown in monospace.
2. Curated public detail fields, when present.
3. Request ID and its copy action, when present.

Diagnostic loading, error, and loaded regions render below the shared action row. Existing role, live-region, sequence-guard, localization, recovery, and secret-filtering behavior remains unchanged. Disclosure IDs must be unique if more than one panel is rendered.

## VRChat Identify Screen

The first screen makes the full profile URL the easiest input while retaining raw `usr_...` identifiers for experienced users.

Visible guidance appears before the input:

1. Open the VRChat website and sign in.
2. Open the user's own profile.
3. Copy the page address ending in `/user/usr_...` and paste it into Prohibitorum.

The screen includes an external `Open VRChat website` link and states that this flow never asks for VRChat credentials. The field label becomes `VRChat profile URL or user ID`; its placeholder is a complete profile URL. Existing submission, focus restoration, validation, and error handling remain unchanged.

## VRChat Proof Screen

The second screen uses visible task hierarchy rather than a flat stack of URL strings and paragraphs.

- Heading: add the one-time link to the VRChat bio.
- Profile context: a compact row with a clear `Open VRChat profile` action. The raw long URL is secondary, not the main affordance.
- Proof URL: the existing copyable code field is the focal control.
- Instructions: a semantic ordered list for copy, add to bio, and return to verify.
- Expiry: a quiet clock status row beneath the instructions.
- Local username: when required, a separate section divided from the proof instructions.
- Errors: directly above retry timing and the final verify action.
- Success: retains explicit user-controlled continuation and no automatic navigation.

The screen must remain usable at 390px width without horizontal overflow. No nested cards are introduced.

## Public Proof-Link Page

The public `/verify/vrchat/:proof` page becomes useful to both profile owners and visitors.

The first section explains:

- The profile owner temporarily added the link to prove control of a VRChat account to the configured Prohibitorum instance.
- A visitor does not need to take any action.
- Opening the page does not verify the person's real-world identity, sign the visitor in, approve access, or give the profile owner access to the visitor's account.

A separate `If this is your profile` section tells the owner to:

1. Return to the configured identity-provider instance and select `Verify profile`.
2. Remove the link after the instance confirms verification.
3. Close the notice page.

The page continues to make no API request and renders identical content for valid, expired, malformed, and unknown proof tokens. It must not reveal proof status. Every reference to the local service on this page uses the configured instance name rather than a hard-coded product name.

## VRChat Federation Button

Add a dedicated `VRChatButton.vue` beside `SteamButton.vue` and select it by `provider.protocol === 'vrchat'`.

- Built-in predefined VRChat mark using the vetted Simple Icons VRChat vector, checked into `dashboard/src/assets`; it reproduces the official mark and does not depend on an admin-uploaded provider icon.
- Background: VRChat Blue `#00A2E8`.
- Text and icon: `#0B1A21`.
- Text contrast: 6.19:1. White on the same blue is 2.86:1 and is not used.
- Native Prohibitorum button shape, full-width alignment, focus ring, and standard disabled behavior.
- A darker blue hover/active state that retains at least 4.5:1 text contrast.
- Provider display name remains the visible label.

Generic OIDC providers and Steam retain their existing rendering paths.

Official brand source: https://hello.vrchat.com/press

## Localization

All new visible strings are added in English and Simplified Chinese. Existing locale parity checks remain authoritative. Copy must be direct and instructional, without exposing implementation details such as operator cookies, private credentials, or proof-token validity.

## Accessibility

- One page-level `h1` per threshold page.
- Ordered sequences remain semantic `ol` elements.
- External links have standalone accessible names and use `noopener noreferrer`.
- Dismiss, details, diagnostic, copy, and profile actions remain keyboard reachable with visible focus.
- Error labels and values use semantic `dl`, `dt`, and `dd` structure.
- Error and VRChat brand colors meet WCAG 2.2 AA contrast.
- Mobile layouts have no horizontal overflow at 390px.

## Verification

Tests defend these observable contracts:

1. Every error exposes a Details control and expanded details contain the exact public error code.
2. The dismiss mark is structurally positioned at the top-right and the action/details regions follow the selected hierarchy.
3. The identify screen visibly explains how to get a profile URL, links to the VRChat website, and still submits either accepted input form.
4. The proof screen renders profile action, copyable proof URL, ordered instructions, expiry status, optional local username, contextual errors, and success without changing API calls.
5. The public proof page explains the link to guests, separates owner instructions, reveals no token status, and performs no API call.
6. VRChat providers use the dedicated branded button and built-in mark; Steam and OIDC paths remain unchanged.
7. English and Chinese locale parity passes.
8. Chromium confirms error-panel, identify, proof, public-notice, and login-button layouts at desktop and 390px mobile widths.
9. The embedded dashboard build and complete project CI gate pass.
