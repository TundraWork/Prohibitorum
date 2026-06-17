# Login-resume redesign — server-owned redirect via opaque challenge

**Date:** 2026-06-17
**Status:** Design proposed; ready for an implementation plan.
**Supersedes:** the client-reflected `return_to=<URL>` login bounce (commit `ef07364`'s relaxed `safeReturnTo` is a stop-gap, reverted by this design).

## Problem

When `/oauth/authorize` or SAML SSO needs interactive login, the server bounces
the browser to `/login?return_to=<full, URL-encoded authorize/SSO URL>`
(`authorize.go:124,234,292`, `sso.go:142`). The SPA reads that URL and, after the
login ceremony, navigates to it. **The redirect target is an attacker-influenceable
value reflected through the browser, and the *only* guard is client-side
(`safeReturnTo`).** There is no server-side validation. On an IdP — a prime
phishing target — a single JS regression silently re-opens an open redirect, and
the authorization-server "owns" nothing about where the user lands.

## Best practice (researched)

- **Ory Hydra** keeps *all* authorization-request state server-side; the browser
  carries only a short-lived **opaque `login_challenge`**, the login UI fetches
  context server-to-server, and the AS reconstructs and owns the final redirect.
  "This keeps sensitive parameters out of reach of browser-side tampering and
  centralizes security validation in the authorization server itself."
  ([login flow](https://www.ory.sh/docs/hydra/concepts/login),
  [consent flow](https://www.ory.sh/docs/hydra/concepts/consent))
- **RFC 9700 (OAuth 2.0 Security BCP, Jan 2025) §4.11.2** — the *Authorization
  Server MUST NOT be an open redirector*; redirect targets are validated
  server-side (exact match), never reflected unvalidated through the browser.
  ([RFC 9700](https://www.rfc-editor.org/info/rfc9700/))

**Principle to adopt:** the browser never carries a redirect URL for a
cross-context (protocol) login. It carries an opaque, single-use, short-TTL
pointer; the server stores, validates, and performs the redirect.

## Design

### 1. Resume record (KV-backed, opaque challenge)

A pending interactive login is stored in the existing KV (`pkg/kv`, which has
`SetEx` + atomic `Pop`):

- **Key:** `login:resume:<challenge>` — `<challenge>` is 32 bytes of `crypto/rand`,
  base64url (same generator as the refresh-token family IDs).
- **Value (JSON):** `{ "resume_path": "/oauth/authorize?…", "kind": "oidc"|"saml", "client_name": "Acme Console", "created_at": <unix> }`.
  `resume_path` is a **same-origin path+query** the server generated — never a full
  URL, never client-supplied.
- **TTL:** 10 min. **Single-use:** consumed with `Pop` on resume.

### 2. `beginLogin` helper (replaces every `/login?return_to=` bounce)

```go
// beginLogin stores the pending interactive login server-side and bounces the
// browser to the login UI with only an opaque challenge — no redirect URL.
func (p *Provider) beginLogin(ctx, w, r, resumePath, clientName, kind string) {
    ch := randChallenge()
    rec, _ := json.Marshal(resumeRecord{ResumePath: resumePath, ClientName: clientName, Kind: kind, CreatedAt: now})
    _ = p.kv.SetEx(ctx, "login:resume:"+ch, string(rec), 10*time.Minute)
    http.Redirect(w, r, p.issuer()+"/login?login_challenge="+ch, http.StatusFound)
}
```

Migration: `authorize.go:124` (login), `authorize.go:234,292` (forced re-auth /
step-up — add a `force_reauth` flag to the record), and `sso.go:142` (SAML SSO)
all call `beginLogin` with their own same-origin `resume_path`. **No bounce emits
a URL into the browser any more.**

### 3. Resume endpoint — the server owns the redirect

`GET /api/prohibitorum/auth/resume?login_challenge=<ch>` (public route):

1. **Require an authenticated session.** No session → bounce back to
   `/login?login_challenge=<ch>` *without consuming the challenge* (the user still
   needs to log in).
2. `Pop` the record (atomic single-use). Missing/expired → `302 /` (dashboard).
3. **Re-validate** `resume_path` is a same-origin path (single leading `/`, not
   `//`, no scheme) — defense-in-depth even though the server generated it.
4. `302` to `issuer + resume_path`.

This endpoint is **not** an open redirector: the target is server-stored and
server-generated, the query param is only an opaque ID, and it is re-validated.

### 4. SPA changes

- **`LoginView`** reads `login_challenge` (not `return_to`) for protocol logins.
  On success — or when already authenticated — it does
  `window.location.assign('/api/prohibitorum/auth/resume?login_challenge=' + ch)`
  and lets the **server** redirect. With neither param → dashboard.
- **`safeReturnTo` reverts to strict relative-only** (reject absolute URLs). The
  only remaining `return_to` is the SPA's own auth-guard value (`to.fullPath`, a
  *relative* same-origin route) — handled internally by the router, never a full
  URL. Reverting removes the open-redirect surface my stop-gap introduced.

### 5. Already-authenticated / `skip`

`/oauth/authorize` already short-circuits when a session exists (issues a code
without ever reaching `/login`). The SPA's on-mount session check then only
matters for *direct* `/login` visits: authenticated → resume (challenge) or
dashboard. (Optional phase 3: a `GET …/auth/login-request?login_challenge=<ch>`
context endpoint returning `{ client_name, kind, skip }` so the UI can show "Acme
Console wants you to sign in" — mirrors Hydra's `skip` signal.)

## Security properties

| Property | Mechanism |
|----------|-----------|
| No browser-tamperable redirect target | Browser carries only an opaque challenge; `resume_path` is server-stored |
| AS is not an open redirector (RFC 9700 §4.11.2) | Resume target server-generated, same-origin, re-validated; never a query-supplied URL |
| Forgery / guessing | 32-byte random challenge; missing record → dashboard, not an error oracle |
| Replay | Single-use (`Pop`) + 10-min TTL |
| Resume only post-auth | Resume endpoint requires a session before consuming |
| Defense-in-depth | Strict client `safeReturnTo` retained for SPA-internal relative paths |

RP `redirect_uri` validation (exact match, server-side) and PKCE/nonce CSRF on
the OIDC flow are unchanged — this redesign concerns only the *login bounce*.

## Alternatives considered

- **Signed `return_to` (HMAC/JWT, stateless).** Server signs the URL it emits; a
  server endpoint verifies signature + same-origin before redirecting. Lighter
  (no KV record), but the URL still rides in the browser, needs signing-key
  management, and gives no `skip`/context. A reasonable middle ground; rejected as
  primary because the opaque-challenge model is the recognized IdP pattern, keeps
  the URL fully out of the browser, and reuses the existing KV.
- **Keep client-only `safeReturnTo`.** Rejected: a security control with no
  server-side backstop on an IdP.

## Rollout (phased)

1. **Core:** resume-record store + `randChallenge` + `beginLogin` + `/auth/resume`;
   migrate the OIDC authorize *login* bounce + SAML SSO bounce; SPA `LoginView`
   uses `login_challenge`; revert `safeReturnTo` to strict. Go + FE tests.
2. **Re-auth:** migrate `authorize.go:234,292` (forced re-auth / step-up) with the
   `force_reauth` record flag.
3. **Optional:** `…/auth/login-request` context endpoint + "App wants you to sign
   in" UI (Hydra `skip`).

No data migration; the KV is ephemeral. The change is backwards-compatible at the
RP boundary (RP `redirect_uri`/PKCE flow unchanged); only the internal login
bounce shape changes.
