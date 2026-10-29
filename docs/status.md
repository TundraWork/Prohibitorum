# Status

What Prohibitorum implements today, its known limits, and the roadmap. Details
live in the linked documents; [`api.md`](./api.md) is the authoritative route
reference.

## Implemented

**Accounts and sign-in**

- Passkeys (WebAuthn) are the primary method. Password + TOTP is the fallback,
  with ten single-use recovery codes; a recovery code can also reset the
  authenticator.
- Accounts are created through admin-issued enrollment links (invitations),
  through a provider set to create accounts, or by `enroll-admin` for the first
  admin. Recovery is a recovery code or a new enrollment link from an admin;
  email is never a recovery channel.
- Sensitive actions require a recent sign-in (sudo): one passkey or
  password + authenticator prompt covers every guarded action until it expires.
- A new device can sign in by showing a pairing code that a signed-in device
  approves.
- Account emails are verified with a mailed code when SMTP is configured, or
  asserted by an admin.
- Personal access tokens act as their owner at the forward-auth gateway and,
  at the `full` level, on the management API. See
  [`forward-auth.md`](./forward-auth.md).

**Upstream identity providers** ([`architecture.md`](./architecture.md#upstream-federation))

- OpenID Connect (discovery or manual endpoints, code flow with PKCE, or the
  implicit `id_token` responses), Steam OpenID 2.0, and VRChat profile proof.
- Each provider has an "allow sign-in" switch and a provisioning mode:
  create accounts, invitation only, or link only. A provider with sign-in off
  only verifies identities for invitations and linked accounts; VRChat starts
  that way.
- A first sign-in through a provider asks the member to confirm the new
  identity before a session is issued. Members link and unlink identities
  themselves; the last sign-in method cannot be removed.
- Display name, verified email, and avatar sync from the provider on sign-in.

**Downstream protocols** ([`integration.md`](./integration.md))

- OpenID Connect provider: authorization code + PKCE (S256), refresh-token
  rotation with reuse detection, userinfo, introspection, revocation,
  RP-initiated logout, `prompt`/`max_age` re-authentication, and a consent page.
- SAML 2.0 IdP: SP- and IdP-initiated SSO over Redirect and POST bindings,
  signed metadata, IdP-local single logout, a GHES attribute profile, and a
  consent page.
- Traefik forward-auth for applications without their own login.

**Access control**

- Admins define groups once and each application selects the ones it uses.
  A manual group holds per-account allow or deny decisions; a rule group is
  evaluated from live facts: verified connections, sign-in methods, and avatar.
- Each application is open to every member or restricted. On a restricted
  application, a deny in any selected manual group wins, then an allow, then a
  matching rule group.
- Matching groups can be sent to the application as OIDC claims or a SAML
  attribute.
- An admin can assign any active account to manage specific applications. The
  assignment grants no use of the application and no other admin rights.

**Dashboard**

- English and Chinese, light and dark themes, desktop and mobile.
- Public pages: sign-in, recovery, enrollment, consent, device pairing,
  identity confirmation, error, and maintenance.
- Members: application launcher, profile and avatar, security (passkeys,
  password and authenticator, sessions, connected identities, personal access
  tokens), connected applications, and devices.
- Admins: users, groups, invitations, identity providers, OIDC / SAML /
  forward-auth applications, sign-in page appearance, mail, maintenance mode,
  client-IP handling, signing keys, and the audit log.

**Operations** ([`config.md`](./config.md), [`tooling.md`](./tooling.md))

- One binary with the dashboard embedded; PostgreSQL plus Redis or the
  in-memory store. Migrations run on start.
- Signing keys move through pending → active → decommissioning → retired, so
  tokens signed by the previous key keep verifying during the grace window.
  Private keys and provider secrets are encrypted at rest with versioned keys.
- Every credential and management action is written to the audit log.
- Releases are signed with cosign and ship with SBOMs and build provenance.

## Known limits

- With several replicas, a signing-key change reaches the other replicas within
  five minutes (the key cache lifetime).
- Sign-out is local to Prohibitorum: it does not propagate to OIDC clients or
  to other SAML service providers.
- Federated accounts re-authenticate through the upstream provider; upstream
  refresh tokens are not stored.
- SAML assertions and NameIDs are signed but not encrypted.

## Roadmap

None of these are scheduled.

- HSM/KMS-backed signing (AWS KMS, GCP KMS, Vault Transit), so the private key
  never leaves the vault.
- A password breach-list check (NIST SP 800-63B-4 §5.1.1.2) for the password
  fallback.
- Coordinated single sign-out: OIDC front- and back-channel logout and SAML
  multi-SP logout.
- Audit-log export to a SIEM.

Added on demand (tracked in [`audit.md`](./audit.md)): DPoP, PAR, JAR, mTLS,
SAML assertion and NameID encryption, pairwise `sub`. Not planned: dynamic
client registration and upstream refresh-token storage.
