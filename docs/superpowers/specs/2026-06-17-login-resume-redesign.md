# Login-resume redesign — consistent server-side `return_to` validation

**Date:** 2026-06-17
**Status:** Design approved (revised after deeper codebase study); ready for a plan.
**Supersedes:** the client-only `return_to` guard (commit `ef07364`'s relaxed `safeReturnTo` is a stop-gap, replaced here).

## Problem

When `/oauth/authorize` or SAML SSO needs interactive login, the server bounces
the browser to `/login?return_to=<full, URL-encoded authorize/SSO URL>`
(`authorize.go:124,234,292`, `sso.go:142`). Two issues:

1. **The bounce emits an *absolute* URL**, forcing the client to normalise it.
2. **The passkey/password ceremony validates `return_to` only client-side.**
   `login/complete` returns JSON and the SPA navigates via `safeReturnTo` — no
   server-side check. On an IdP a single JS regression silently re-opens an open
   redirect, with no server backstop.

## What the codebase already does (the precedent to mirror)

Deeper study shows Prohibitorum **already chose "server-validated relative
`return_to`"**, not a delegated-UI challenge model:

- The **federation** login (`/auth/federation/{slug}/login?return_to=…`) runs
  `validateFederationReturnTo` (handle_federation.go:202) — **relative-only,
  same-origin, server-side** — stores it in the federation state, and the
  **callback performs the redirect server-side** (handle_federation.go:9).
- The `/welcome` confirm flow likewise keeps pending state server-side (KV grant
  + `GET …/auth/federation/confirm` to peek context), under the
  `/auth/<context>/<action>` naming style.

So the open-redirect requirement of **RFC 9700 §4.11.2** ("the AS MUST NOT be an
open redirector"; validate targets server-side —
[RFC 9700](https://www.rfc-editor.org/info/rfc9700/)) is already met *on the
federation path*. The gap is only that the **ceremony path** and the **bounce**
don't follow the same rule. The fix is **consistency**, not a parallel system.

> **Why not Ory Hydra's opaque `login_challenge` model?** ([login](https://www.ory.sh/docs/hydra/concepts/login)/[consent](https://www.ory.sh/docs/hydra/concepts/consent) flow.)
> Hydra's challenge exists to **decouple a separately-hosted, untrusted login UI**
> from the AS and hide request params from it. Prohibitorum's login UI is
> **first-party, same-origin, and embedded in the same binary** — that decoupling
> buys nothing here, while adding a KV grant store, a new endpoint, and a second
> redirect-resume mechanism that duplicates the existing `return_to` threading
> (incl. the federation path). For this architecture, server-validated relative
> `return_to` *is* the appropriate best practice; the challenge model is
> over-engineering. (Reconsider only if the login UI is ever split into a
> separate origin/app.)

## Design — one server-side `return_to` rule, enforced everywhere

**Invariant:** `return_to` is always a **same-origin relative path**, validated
**server-side** at every point it is consumed. The browser may carry it, but no
server endpoint ever redirects to (nor does the SPA ever navigate to) a value the
server hasn't validated.

1. **Generalise the validator.** Rename/extract `validateFederationReturnTo` →
   a shared `Server.validateReturnTo(rt string) (string, error)` (same rule:
   leading single `/`, not `//`, empty → `/`). The federation handler keeps using
   it; the login ceremony starts using it. (Forward-looking hook for
   `config.PublicOrigins` stays.)

2. **Bounce emits a *relative* `return_to`.** At `authorize.go:124,234,292` and
   `sso.go:142`, set `return_to` to the request's **`Path + "?" + RawQuery`**
   (same-origin path), not `Issuer + fullURL`. Removes the absolute-URL surface.

3. **Server-validate the ceremony.** `login/complete` (WebAuthn) and the
   password-login completion accept `return_to` in the request, run
   `validateReturnTo`, and **return the validated value** in the JSON response
   (`{ …sessionView, "returnTo": "/oauth/authorize?…" }`). The SPA navigates only
   to the **server-blessed** `returnTo` — the server is now the authority for the
   ceremony path too, matching federation.

4. **Client `safeReturnTo` reverts to strict relative-only** (reject absolute /
   `//` / schemes) — the same rule as the server, kept as defense-in-depth. The
   stop-gap relaxation from `ef07364` is removed. The SPA sends the raw query
   `return_to` to `login/complete` and trusts the echoed, server-validated value.

No new endpoints, no KV grant, no challenge — the `/welcome` and federation
naming/patterns are untouched and now uniformly applied.

## Flow (OIDC, after the change)

```
RP → /oauth/authorize (no session)
   → 302 /login?return_to=/oauth/authorize?…           (relative, server-built)
SPA /login → passkey/password ceremony
   → POST …/login/complete { …, return_to:/oauth/authorize?… }
   → server validates return_to → 200 { …session, returnTo:/oauth/authorize?… }
SPA → window.location.assign(returnTo)                   (server-blessed value)
   → /oauth/authorize (now has session) → code → RP redirect_uri
```
Federation path is unchanged (already server-validated); it benefits because the
bounce now hands it a relative `return_to` directly.

## Security properties

| Property | Mechanism |
|----------|-----------|
| AS is not an open redirector (RFC 9700 §4.11.2) | `validateReturnTo` (relative-only, same-origin) enforced server-side at **every** consumption point: federation callback (existing) + login ceremony (new) |
| No client-only security control | Ceremony redirect target is server-validated + echoed; SPA obeys the server |
| Defense-in-depth | Client `safeReturnTo` retains the identical strict rule |
| No absolute-URL surface | Bounce emits a relative path |

RP `redirect_uri` (exact-match, server-side) and PKCE/nonce are unchanged.

## Alternatives considered

- **Ory Hydra opaque `login_challenge` + KV grant** — the gold standard *for a
  decoupled login UI*. Rejected for a first-party same-origin UI (see box above):
  over-engineered, duplicates existing `return_to` threading.
- **Keep client-only `safeReturnTo`** — rejected: no server backstop on an IdP
  (the gap that prompted this).

## Rollout (phased)

1. **Validator + bounces:** extract `validateReturnTo`; make the OIDC-authorize
   *login* bounce + SAML SSO bounce emit relative `return_to`. (Go tests.)
2. **Ceremony:** thread + server-validate `return_to` through WebAuthn and
   password `login/complete`; SPA navigates to the echoed value; revert
   `safeReturnTo` to strict. (Go + FE tests.)
3. **Re-auth bounces:** apply the relative-`return_to` + validation to the forced
   re-auth / step-up bounces (`authorize.go:234,292`).
4. **Verify:** full gate + the two-instance federation lab end-to-end.

No data migration; RP boundary (`redirect_uri`/PKCE) unchanged.
