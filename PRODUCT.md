# Product

## Users

Members and admins of a single small organization running Prohibitorum as
their self-hosted identity provider. Single-tenant, first-party: everyone
who touches the UI belongs to the same org.

- **Members** sign in to reach downstream apps (via OIDC or SAML) and manage
  their own identity: registering and naming passkeys, setting up password +
  TOTP fallback, viewing and revoking active sessions, redeeming an
  enrollment invite. Often non-technical; they meet this UI at the login
  screen and the consent screen, occasionally in their own account area.
- **Admins** manage the directory: creating accounts, issuing enrollment
  invitations (the only recovery path, since there is no email channel),
  setting roles and attributes, and reviewing credentials. Same person is
  often both a member and an admin in a small org.

Context of use: a browser, at a desk or on a phone, usually mid-task — they
came here to get into something else, or to fix one specific thing about
their account. The IdP is infrastructure; time spent in it is overhead the
user wants to minimize.

## Product Purpose

Prohibitorum is a homegrown, single-tenant identity provider for small orgs.
It owns the account directory, authenticates users with WebAuthn,
Password + TOTP/recovery codes, or upstream OIDC, Steam, and VRChat
federation, and issues sessions plus OIDC/SAML assertions to downstream apps.
The UI's job is to make three things effortless and trustworthy: signing in,
granting consent, and self-managing credentials, with a role-gated admin layer
for directory management on top.

Success looks like: a member completes a passkey login or enrollment without
hesitation and without reading instructions; an admin issues an invitation
and sees its state at a glance; and at no point does anyone wonder whether
the thing guarding their identity is competent. The interface should be
forgettable in the best way, the user gets in, does the one thing, and leaves.

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
