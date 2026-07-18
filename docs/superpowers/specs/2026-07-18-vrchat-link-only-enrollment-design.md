# VRChat Link-Only Enrollment and Recovery Design

**Date:** 2026-07-18
**Status:** Approved in conversation

## Problem

VRChat profile proof is not equivalent to an OAuth or OpenID Connect authentication ceremony. Prohibitorum must therefore stop treating VRChat as a full upstream identity provider that can directly provision an account or issue a normal session.

VRChat remains useful for three narrower purposes:

1. proving control of a VRChat profile before creating a local account;
2. recovering access to an existing local account through the shared credential-recovery ceremony; and
3. attaching a verified VRChat identity to an already-authenticated local account.

A local account and a local sign-in credential remain mandatory. VRChat proof alone never creates a session.

## Decisions

- VRChat providers are permanently `link_only`.
- The existing branded “Login with VRChat” button remains on the login page.
- The button starts VRChat profile verification, not normal federation login resolution.
- Unknown verified profiles continue into shared local-account enrollment.
- Already-linked profiles continue into shared local-credential recovery.
- Authenticated users continue linking VRChat from Connected Accounts.
- The shared enrollment surface owns credential choices. VRChat adds no passkey-, password-, or TOTP-specific account-creation UI.
- The current shared `/enroll/:token` implementation performs WebAuthn registration. If shared enrollment later adds other credential choices, the VRChat handoff inherits them without a VRChat-specific change.
- This is a hard cutover. There are no real users requiring backward-compatibility behavior and no legacy VRChat-login grace path.

## Provider Contract

### Fixed mode

The backend accepts `link_only` as the only valid mode for `protocol = "vrchat"` on provider creation and update. It rejects `auto_provision` and `invite_only` rather than silently normalizing them.

The dashboard removes the editable mode selector for VRChat and shows explanatory text instead. A migration changes any existing VRChat provider row with another mode to `link_only`.

OIDC and Steam provider modes and behavior remain unchanged.

### Availability

An enabled, operational VRChat provider may serve:

- public profile verification for registration or recovery; and
- authenticated profile verification for Connected Accounts linking.

Disabling the provider blocks all three uses. Enrollment completion rechecks provider availability so disabling a provider also prevents unfinished VRChat registration or recovery from completing.

Operator-session setup, health state, metadata filtering, entity icons, and audit access remain unchanged.

## Public Registration and Recovery Flow

The existing login-page VRChat button remains visually grouped with the available authentication choices. Its endpoint dispatches to a new `IntentEnroll` federation intent rather than `IntentLogin`. Only the VRChat protocol supports `IntentEnroll`; the resolver never treats it as a normal login intent.

### User notice

The first profile-identification screen displays this always-visible notice:

> **Your VRChat account is only used to verify your identity and help you recover access. If you’re new here, you’ll create a local account and sign-in method after verification.**

It also displays:

> Can you still sign in to your local account? Link VRChat from Connected Accounts instead.

The existing profile-finding instructions, credential warning, proof publication steps, expiration display, and safe external links remain.

The proof screen no longer asks for a local username. Local account fields belong exclusively to shared enrollment.

### Verification result

After exact profile and proof-link verification, the server looks up the identity by provider, issuer, and subject.

#### Unknown identity

The server issues a 15-minute, single-use `federated_register` enrollment containing a server-side snapshot of the verified VRChat identity. It creates neither an account nor a session at this stage. The browser is redirected to `/enroll/:token`.

The shared enrollment UI collects the local account fields and invokes its normal credential ceremony. The verified VRChat display name may be offered as an editable display-name default; a local username is never inferred from it.

Successful enrollment completion performs one database transaction that:

1. atomically consumes the enrollment;
2. rechecks that the provider remains enabled and is the expected VRChat provider;
3. rechecks that the verified issuer and subject are still unclaimed;
4. validates and creates the local user account with role `user`;
5. creates the local credential;
6. creates the `account_identity` row with curated VRChat metadata; and
7. commits all records together.

Only after commit does the server issue the normal local session and set its cookie.

#### Existing identity

The server does not resolve the identity into a normal federation session. It issues a 15-minute `reset` enrollment targeting the owning account and records the VRChat provider as the recovery source, then redirects to `/enroll/:token`.

The existing reset completion contract remains authoritative:

1. disabled accounts are rejected;
2. the enrollment is consumed atomically;
3. existing credentials are replaced through the shared recovery ceremony;
4. previous sessions are revoked; and
5. a fresh normal session is issued only after the new local credential commits.

Public responses do not reveal the owning account’s username, email, account ID, role, or other private fields.

## Authenticated Connected Accounts Linking

Connected Accounts continues using the existing session-bound `IntentLink` flow:

1. the server binds the operation to the authenticated account and session;
2. the user completes exact VRChat profile proof;
3. the resolver attaches the identity and curated metadata atomically to that account; and
4. the flow returns to Connected Accounts.

This path creates no enrollment, account, credential, or login session. An identity already owned by another account produces the existing identity-conflict error. Unlinking VRChat does not remove the local account or local credentials.

## Enrollment Data Model

The enrollment intent constraint gains `federated_register`.

A federated-registration enrollment stores the minimum server-controlled snapshot required to complete account creation:

- upstream provider ID and slug;
- verified issuer and subject;
- verified display name;
- validated, curated upstream metadata;
- optional adapter-validated avatar URL;
- expiration and consumption state.

These identity-snapshot fields are valid only for `federated_register`. Avatar delivery is best-effort after the account transaction commits and cannot leave account, credential, or identity data partially written.

The browser receives only the existing high-entropy enrollment token and safe preview fields. It cannot submit or replace provider identity data. Preview may expose the editable display-name suggestion but no private account lookup result.

Recovery continues using the existing account-targeted `reset` intent. A nullable `recovery_source_upstream_idp_id` is set only for VRChat-issued recovery and lets completion recheck provider availability; ordinary administrator-issued reset enrollments leave it null and retain their existing behavior. Neither form copies the owning account or VRChat identity into browser-controlled state.

## Security and Concurrency

- VRChat proof alone never issues a session.
- Federation state remains short-lived and browser-bound.
- Proof tokens remain exact-match, high-entropy, expiring values.
- Registration and recovery enrollments expire 15 minutes after issuance and are single-use.
- Enrollment identity snapshots are written only from adapter-verified server data.
- Registration completion revalidates provider protocol, mode, enabled state, and identity uniqueness.
- Database uniqueness remains the final authority for concurrent username and identity claims.
- A registration race has one winner; losing attempts create no partial account, credential, or identity.
- Disabled accounts cannot use VRChat recovery.
- Recovery revokes prior sessions before issuing the replacement session.
- No audit record, log, diagnostic detail, or public response contains VRChat cookies, proof tokens, enrollment tokens, operator credentials, or private account lookup fields.
- External VRChat links retain fixed destinations or server-validated profile URLs and `noopener noreferrer` behavior.

## Error Behavior

Existing safe errors remain authoritative for:

- invalid VRChat profile identifiers;
- missing profile proof;
- operator-session unavailability;
- upstream rate limiting and temporary failures;
- expired, malformed, consumed, or replayed enrollment state;
- username collisions; and
- authenticated identity-link conflicts.

A provider disabled between proof and enrollment completion fails closed without creating account data. A disabled recovery target fails without revealing whether the identity is linked to that account.

Unexpected storage, transaction, or session failures use the registered public error envelope and request diagnostics; raw identity or credential data is never returned.

## Administration UX

VRChat provider create and detail screens:

- identify the provider as link-only;
- omit the editable provisioning-mode control;
- explain profile verification, local account creation, recovery, and authenticated linking;
- retain enable/disable, operator-session, metadata, icon, filter, and audit controls; and
- never describe VRChat as OAuth, OIDC, or a direct local sign-in credential.

## Audit Events

Audit records distinguish these outcomes without storing bearer material:

- VRChat profile proof completed;
- federated local registration issued and completed;
- VRChat-backed local credential recovery issued and completed;
- authenticated VRChat identity link completed;
- provider-disabled rejection;
- identity or username conflict; and
- enrollment expiration, replay, or invalid state.

Registration and recovery completion audit records include the resulting account ID only after the relevant account relationship is established.

## Verification Requirements

### Backend and data

- VRChat provider create/update rejects every mode except `link_only`.
- Migration changes existing VRChat modes to `link_only` while leaving other protocols untouched.
- Login-page VRChat verification never enters normal federation account resolution.
- Unknown verified identities create an enrollment but no account or session.
- Federated enrollment completion atomically creates account, credential, identity, metadata, and then a session.
- Existing verified identities create reset enrollment but no immediate session.
- Recovery replaces credentials, revokes prior sessions, and only then issues a fresh session.
- Provider/account disablement, expiration, replay, concurrent identity claims, and username races fail closed.
- Authenticated linking remains account/session-bound.
- OIDC and Steam provisioning and sign-in contracts are unchanged.

### Frontend

- The exact approved English notice and equivalent Simplified Chinese notice are present and structurally aligned.
- The proof screen contains no local-username field.
- Successful new and existing verification routes to the correct shared enrollment state.
- VRChat mode cannot be changed in admin forms.
- Connected Accounts still offers authenticated VRChat linking.
- Public errors reveal no account lookup details.

### End-to-end

Browser verification covers new registration, existing-account recovery, and authenticated linking at desktop and mobile widths, including keyboard focus, loading/errors, expiration, and no horizontal overflow.

The smoke flow proves that no session cookie is issued before shared enrollment completion. Full CI, production dashboard build, embedded dashboard drift validation, and final whole-range code review pass before delivery.
