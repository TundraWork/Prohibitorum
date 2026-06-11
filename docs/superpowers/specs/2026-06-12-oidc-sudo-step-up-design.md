# OIDC-based sudo step-up — design

**Date:** 2026-06-12
**Status:** approved (brainstorm) — pending spec review → implementation plan
**Scope:** add upstream-OIDC re-authentication as a third sudo step-up method, so
users without a passkey or password+TOTP (federated-only) — and any user with a
linked, enabled upstream identity — can satisfy the sudo gate.

## Problem

The sudo step-up gate (`handle_sudo.go`) accepts only `webauthn` and
`password_totp`. `availableSudoMethods` (`handle_sudo.go:92-99`) explicitly
filters out `federation_oidc` even though it is a real sign-in method
(`pkg/authn/flow.go:30`, surfaced by `AvailableMethods` at `flow.go:80`), with
the rationale "federation isn't a sudo factor — it doesn't re-prove possession."

Consequence: a **federated-only** user (signed in through an upstream OIDC IdP,
no passkey, no password+TOTP) has an **empty** sudo-method set and therefore
cannot perform any sudo-gated action — including adding a passkey or password.
That is a chicken-and-egg lockout: the only way to gain a local factor is itself
sudo-gated. This affects regular and admin accounts alike.

The original objection is valid only for a *silent* federation bounce. A
**forced** upstream re-authentication (`prompt=login` + `max_age=0`, with the
returned `auth_time` verified fresh) genuinely re-proves control of the upstream
account — which is precisely the credential a federated-only user holds.

## Goals / non-goals

In scope:
- `federation_oidc` becomes a selectable sudo method whenever the caller has at
  least one linked, **enabled** upstream identity (regardless of whether they
  also hold a passkey/password).
- OIDC sudo forces a fresh upstream re-auth and grants the same one-shot
  `SudoUntil` window as the other methods.

Out of scope (YAGNI):
- No change to `webauthn` / `password_totp` sudo (they already work).
- No "recent federated login counts as sudo" shortcut (rejected: not forced).
- No auto-resume of the originally-blocked action across the redirect (the user
  re-triggers it on return; see Frontend).
- No SAML-based sudo (SAML is downstream-only in this project).

## Current state (anchored)

- `pkg/authn/flow.go:28-30` — `MethodWebAuthn`, `MethodPasswordTOTP`,
  `MethodFederationOIDC` constants; `AvailableMethods` returns federation at
  `flow.go:80` when the account has a linked identity.
- `pkg/server/handle_sudo.go:85-101` — `availableSudoMethods` drops everything
  except webauthn/password_totp.
- `pkg/server/handle_sudo.go` begin/complete — webauthn returns an assertion and
  finishes at `/me/sudo/complete`; password_totp verifies at complete;
  `stampSudoUntil` / `consumeFreshSudo` own the grant lifecycle (the latter now
  fails closed on KV error per audit SESS-1).
- `pkg/federation/oidc/client.go:185` — `AuthURL(state, nonce, codeChallenge)`
  injects params via `oauth2.SetAuthURLParam`; extensible to add `prompt`/`max_age`.
- `pkg/federation/oidc/client.go:232-245` — code exchange returns
  `oidc.IDTokenClaims` (carries `AuthTime`); iss/nonce already verified.
- `pkg/federation/oidc/state.go` — `FedState` models flows via nilable markers
  (`LinkingAccountID`, `EnrollmentToken`) + per-purpose KV keys (`LoginKey`,
  `LinkKey`) + a `BrowserBinding` (set for login/invite, empty for link per
  audit OIDCFED-1).
- `pkg/federation/oidc/federation.go` — `begin()` builds state + AuthURL;
  `HandleCallback` / `LinkCallback` consume state, code-exchange, resolve the
  `(iss,sub)` identity.

## Design

### 1. Sudo-method availability

`availableSudoMethods` keeps webauthn/password_totp and **adds**
`federation_oidc` when the account has ≥1 linked, enabled identity. The
`/me/sudo/methods` response gains the providers the UI needs:

```json
{
  "methods": ["webauthn", "password_totp", "federation_oidc"],
  "federationProviders": [{ "slug": "google", "displayName": "Google" }]
}
```

`federationProviders` lists only providers that are (a) linked to this account
and (b) not `disabled`. Empty `federationProviders` ⇒ federation not offered even
if the method string is present (defensive; the FE keys off the provider list).

Source of the linked-and-enabled set: join the caller's `account_identity` rows
to non-disabled `upstream_idp` rows (a new read query, or reuse the
`/me/identities` projection filtered to enabled providers).

### 2. Begin (redirect ceremony)

`POST /me/sudo/begin` with body `{ "method": "federation_oidc", "slug": "...",
"returnTo": "/security" }`:

1. Require an authenticated session (existing gate) + the per-account rate limit
   already on `/me/sudo/begin`.
2. Validate `slug` is one of the caller's linked, enabled identities; else
   `sudo_method_unavailable`.
3. Start a **sudo-purpose** federation flow (new `Federator.SudoBegin`):
   - `AuthURL` extended with `prompt=login` and `max_age=0`;
   - `FedState` bound to: the session `AccountID` (new `SudoAccountID *int32`),
     the target `slug`, the **expected upstream `sub`** of the linked identity,
     a `BrowserBinding` (cookie set by the handler), and a safe `ReturnTo`;
   - stashed single-use under a new `SudoKey(stateToken)` namespace.
4. Respond `{ "redirect": "<authorize URL>" }`. (Webauthn/password branches are
   unchanged; the FE branches on method.)

### 3. Callback

`GET /api/prohibitorum/me/sudo/federation/callback?code=…&state=…&iss=…`
(authenticated session required — this is a dashboard route, unlike the login
callback):

1. Pop the sudo-flow state (single-use); verify the `BrowserBinding` cookie.
2. Code-exchange + verify id_token: signature via the upstream JWKS, `iss` exact
   match, `aud` == our client_id, `nonce` match (all via the existing client),
   plus RFC 9207 `iss` response-param handling as login already does.
3. **Identity match:** resolve `(iss, sub)` and require it to equal the session
   account's linked identity for `slug`. Re-authenticating as a *different*
   upstream account (even at the same IdP) ⇒ `sudo_identity_mismatch`, no grant.
4. **Freshness:** require `now − auth_time ≤ maxStepUpAuthAge` (proposed 120s).
   `max_age=0` obliges a conformant IdP to return a fresh `auth_time`; a missing
   or stale value ⇒ reject (`sudo_reauth_stale`). Fails **closed**: an upstream
   that ignores `prompt=login`/`max_age` cannot mint a grant.
5. `stampSudoUntil(w, r, sess, "federation_oidc")`; audit
   `auth.sudo_granted method=federation_oidc`.
6. Redirect to `ReturnTo` (server-side-validated same-origin relative path,
   consistent with how the federation login flow carries `ReturnTo` through
   `FedState`; the FE's own `safeReturnTo` is a separate client-side guard).

On any failure: audit a structured `auth.sudo_failed` reason and redirect to
`ReturnTo` with an error marker (or render `/error`) — never stamp sudo.

### 4. `FedState` sudo flow

Add `SudoAccountID *int32` and `ExpectedSub string` to `FedState`, plus
`SudoKey(token)`. `begin()` populates `BrowserBinding` for the sudo flow (it
*does* carry the cookie, unlike the link flow). The login/link/invite flows are
untouched. `Federator` gains `SudoBegin(ctx, accountID, slug, returnTo)` and
`SudoCallback(ctx, stateToken, code, iss, browserToken, currentAccountID)`.

### 5. Frontend

- `lib/sudo`: `SudoModal` renders a "Re-authenticate with {provider}" action for
  `federation_oidc` (one per entry in `federationProviders`; a small picker when
  there are several). Clicking calls `/me/sudo/begin` then `hardRedirect`s to the
  returned `redirect` URL with `returnTo` = the current route.
- Because a full-page redirect can't resume the in-page `withSudo` retry, the
  callback returns the user to `returnTo` with sudo now fresh; the modal copy
  sets that expectation ("you'll be sent to {provider} and brought back"), and
  the user re-triggers the action.
- A user holding multiple factors sees all of them (e.g. webauthn + federation).

### 6. Security invariants (explicit)

- Forced fresh re-auth: `prompt=login` + `max_age=0` + `auth_time ≤ 120s`, fail-closed.
- Same-upstream-account identity match (resolved `(iss,sub)` == caller's linked identity).
- State bound to the session account + single-use + browser-binding cookie (CSRF).
- Disabled providers excluded from begin and the methods list.
- `returnTo` open-redirect-guarded (same-origin path only).
- Reuses the SSRF-hardened outbound federation client; no new egress surface.

## Error model

New `*authn.AuthError` codes: `sudo_identity_mismatch`, `sudo_reauth_stale`
(both 401-class, collapse to a generic step-up-failed message for the user).
Existing `sudo_method_unavailable` covers an unlinked/disabled slug. en.ts gains
English strings for the new codes.

## Testing

- Unit (`pkg/server`): `availableSudoMethods` surfaces federation only with a
  linked+enabled identity; `/me/sudo/begin` for federation builds a
  `prompt=login`/`max_age=0` URL and binds state to the session+sub; callback
  rejects a mismatched identity, rejects a stale/absent `auth_time`, and on the
  happy path stamps `SudoUntil` + audits.
- Unit (`pkg/federation/oidc`): `SudoBegin`/`SudoCallback` state binding +
  identity/freshness checks (mirroring the existing federation_test fakes).
- Smoke: a federated user performs OIDC sudo against the in-process mock OP, then
  completes a sudo-gated action (e.g. add-passkey). Requires the mock OP
  (`cmd/smoke/mockop`) to honor `prompt=login`/`max_age` and emit a fresh
  `auth_time` — a small mock enhancement is in scope for this work.
- FE (vitest): SudoModal renders the provider action and redirects on click.

## Open questions

None blocking. `maxStepUpAuthAge` is proposed at 120s (a comfortable upper bound
for a real re-auth round-trip); revisit if an upstream's clock skew trips it.
