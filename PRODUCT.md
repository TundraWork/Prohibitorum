# Product

## Users

Members, scoped application managers, and admins of a single small
organization running Prohibitorum as their self-hosted identity provider.
Single-tenant, first-party: everyone who touches the UI belongs to the same
org.

- **Members** sign in to reach downstream apps (via OIDC, SAML, or
  forward-auth) and manage their own identity: registering and naming
  passkeys, setting up password + TOTP fallback, viewing and revoking active
  sessions, redeeming an enrollment invite, or using a VRChat profile to
  prove eligibility for local registration or recovery. VRChat proof itself
  never signs them in; they finish with the same local passkey ceremony as
  every other enrollment. Often non-technical, they meet this UI at the login
  screen and consent screen, occasionally in their own account area.
- **Application managers** (`app_manager`) keep member capabilities and manage
  access policy only for explicitly assigned apps. Assignment never grants
  app use, protocol configuration, account, credential, provider, or instance
  management. Their **Managed applications** surface shows only those apps.
- **Admins** manage the directory: creating accounts, issuing enrollment
  invitations and resets, configuring the fixed-link-only VRChat proof
  provider, setting roles and attributes, assigning application managers, and
  reviewing credentials. The same person is often both a member and an admin
  in a small org.

Context of use: a browser, at a desk or on a phone, usually mid-task — they
came here to get into something else, or to fix one specific thing about
their account. The IdP is infrastructure; time spent in it is overhead the
user wants to minimize.

## Product Purpose

Prohibitorum is a homegrown, single-tenant identity provider for small orgs.
It owns the account directory, authenticates users with WebAuthn, Password +
TOTP/recovery codes, or upstream OIDC and Steam federation, evaluates
app-bound access policy, and issues sessions plus OIDC/SAML assertions to
downstream apps. VRChat is deliberately narrower: profile proof can authorize
a short-lived local registration or recovery enrollment, but cannot become a
direct sign-in credential. The UI's job is to make signing in, proof-backed
local enrollment, consent, self-management, and scoped app-policy management
effortless and trustworthy.

Success looks like: a member completes a passkey login or enrollment without
hesitation and without reading instructions; an admin issues an invitation
and sees its state at a glance; and at no point does anyone wonder whether
the thing guarding their identity is competent. The interface should be
forgettable in the best way, the user gets in, does the one thing, and leaves.

## Application Access Policy

Every group is permanently bound to one downstream application: an OIDC app, a
forward-auth app through its backing OIDC client, or a SAML app. An app can have
one manual group with per-account `allow`, `deny`, or neutral decisions and any
number of calculated rule groups. Bindings cannot be moved or shared, and rule
groups never take manual members.

Rules evaluate live verified provider connections, enrolled login methods, and
available avatar state/source. There is no cached calculated membership: a
connection, credential, or avatar change affects the next decision. For a
restricted app, the result is always **manual deny → manual allow → any
matching rule → deny**; an open app remains open until restriction is enabled.

The same policy service makes the decision and app-aware claims. When an app
opts in, a manually allowed account receives that app's exposed manual-group
slug and every exposed matching rule-group slug, never a group's slug from
another app. A manual denial produces no token or assertion.

Application managers work in **Managed applications**. Its delegated surface is
scoped to `/managed-applications/{kind}/{appId}` and exposes only assigned apps;
global admins retain configuration and assign or remove managers with fresh
sudo. Policy changes are audited without recording raw rules or evaluated facts.

This is a destructive cutover: the prior shared policy and direct per-account
access data are deleted and every app starts open. The deleted policy cannot be
recovered; operators recreate per-app policy before deliberately enabling
restriction.

## Brand Personality

A deliberate split, and the split is the brand:

- **Interaction is warm and human.** Generous spacing, reassuring
  micro-feedback, forgiving flows, plain-language guidance when something
  goes wrong. A non-technical member registering their first passkey should
  feel guided, not interrogated. Approachable, never sterile.
- **Language is quiet, precise, and trustworthy.** Labels, descriptions,
  and especially error and security messages are exact and unhurried. No
  hype, no marketing voice, no false cheer about security. State what is
  true and what to do next.

Three words for the whole: **calm, exact, welcoming.** The warmth lives in
the layout and the moments; the precision lives in the words.

## Anti-references

- **Heavy enterprise admin (Keycloak, old Authentik).** No dense tabbed
  config sprawl, no gray-on-gray operator consoles, no everything-on-one-
  screen complexity. Admin power is revealed progressively, not dumped.
- **Dark hacker / terminal aesthetic.** No neon-on-black, no
  monospace-everything, no "cyber" security theater. Guarding identity is
  serious work that should look calm and ordinary, not dramatic.
- (Shared bans still apply: no generic SaaS-cream hero-metric template, no
  consumer-login playful gradients/mascots. Warmth here comes from space,
  pacing, and tone, not from decoration.)

## Design Principles

1. **Trust through clarity, not decoration.** A security tool earns
   confidence by being legible, predictable, and exact, not by looking
   "secure." If a screen needs a lock icon to feel trustworthy, the screen
   is wrong.
2. **Warm hand, precise word.** Interactions feel human and reassuring;
   copy stays quiet, exact, and hype-free. The two are not in tension, they
   are the product's signature. Carry it into every flow.
3. **One vocabulary, two depths.** Members and admins use the same component
   language. Admin surfaces add density and control behind role gates; they
   are never a different, harsher app bolted on.
4. **Keyboard is a first-class input.** Every flow completes without a
   mouse, passkey and TOTP ceremonies especially. Focus is always visible;
   tab order always sane. (WCAG 2.2 AA is the floor, keyboard-first is the
   commitment.)
5. **The tool disappears into the task.** People come to get in and get out.
   Reduce steps, defer rarely-used controls, never make the user watch the
   interface perform. Forgettable is a feature.

## Accessibility & Inclusion

- **Target: WCAG 2.2 AA, with a keyboard-first commitment on top.** AA
  contrast, target sizes, labels, and status messaging everywhere; plus full
  keyboard operability and a visible, high-contrast focus indicator through
  every flow, since passkey/2FA steps must be completable without a pointer.
- **Reduced motion is honored.** Every transition has a
  `prefers-reduced-motion` alternative (crossfade or instant).
- **Do not rely on color alone** for state (error, success, session-active,
  invitation-expired). Pair color with text, icon, or shape, this also
  serves color-blind users.
- **Plain-language errors.** Security errors are where users are most
  stressed; messages must say what happened and what to do next, without
  jargon, without blame.
