# Native Traefik ForwardAuth (Phase 1) — Design

Date: 2026-06-21
Status: Approved (brainstorming) — pending spec review

## Problem

A user needs to protect a service that sits behind Traefik using Traefik's
**ForwardAuth** middleware, authenticating against Prohibitorum, where the
protected service is on a **different registrable domain** than Prohibitorum.

They want Prohibitorum to answer the ForwardAuth sub-request directly ("we
receive the request and respond 200") rather than running a separate reverse
proxy (oauth2-proxy).

## The hard constraint (why this isn't a pure cookie-check)

Traefik ForwardAuth forwards the *original request's* `Cookie` header to the
auth endpoint. A request to `app.acme.io` carries only cookies scoped to
`app.acme.io`; Prohibitorum's session cookie (on `auth.example.com`) is **never**
sent there. So a pure "read session cookie → 200/401" check is impossible across
unrelated domains — and a login redirect alone loops forever, because returning
to `app.acme.io` still leaves no cookie there.

Every cross-domain forward-auth tool resolves this the same way: plant a small
**per-domain cookie** on the protected domain via one redirect/callback. This
design adopts the **Authentik embedded-outpost, forward-auth single-application**
model — the IdP answers ForwardAuth natively (no separate reverse proxy), and a
single auth path on each protected domain is routed to the IdP to establish that
per-domain cookie. App traffic still flows Traefik → backend untouched.

## Goals

- Prohibitorum natively answers Traefik ForwardAuth: **200 + identity headers**
  for an authenticated, authorized user; **302 to login** otherwise.
- Works across unrelated domains via a per-domain forward-auth cookie.
- Reuses the existing OIDC OP for authentication and the existing
  `oidc_client` + `oidc_client_access` + `access_restricted` model for
  **per-service RBAC** (no new authorization code).
- Steady state is a fast cookie check; authorization is re-evaluated **live**
  on every request.

## Non-goals / phasing

- **Phase 2 (separate spec):** a dashboard admin UI for forward-auth services;
  a multi-domain dev harness; forward-auth sign-out / session-revocation
  propagation to the per-domain cookie.
- **Not** a reverse proxy (we never proxy the app's traffic).
- **Not** the shared-parent-domain single-cookie model (rejected: domains are
  unrelated).
- **Not** oauth2-proxy (rejected: no external component).
- Non-Traefik proxies (nginx `auth_request`, Caddy `forward_auth`) are
  contract-compatible but undocumented here.

## Standards basis

- **Traefik ForwardAuth** ([docs](https://doc.traefik.io/traefik/reference/routing-configuration/http/middlewares/forwardauth/)): 2xx → allow + copy `authResponseHeaders` downstream (replacing client-sent ones); non-2xx → returned to the browser (so a 302 drives login). Best practice: `forwardedHeaders.trustedIPs` at the EntryPoint + `trustForwardHeader: true` on the middleware.
- **Authentik embedded outpost, forward-auth single-app mode** ([docs](https://docs.goauthentik.io/add-secure-apps/providers/proxy/forward_auth/)): the IdP answers ForwardAuth in-process; only a fixed auth path (`/outpost.goauthentik.io/*`) on the app domain is routed to it; the rest goes to the app.
- **Identity headers:** the Authelia `Remote-User` / `Remote-Name` / `Remote-Email` / `Remote-Groups` convention (widely recognized by downstream apps).

---

## Architecture

### Two Traefik hookups per protected service
1. A **ForwardAuth middleware** whose `address` is a fixed Prohibitorum URL —
   the **verify** endpoint. Traefik calls it for every request to the protected
   app, forwarding `X-Forwarded-Method/Proto/Host/Uri` + the app-domain cookies.
2. A **router on the protected domain** for a fixed auth-path prefix
   (e.g. `https://app.acme.io/.prohibitorum-forward-auth/*`) → Prohibitorum.
   This is where the OIDC callback lands so the per-domain cookie's `Set-Cookie`
   is scoped to `app.acme.io`.

### The forward-auth app entity (RBAC for free)
A forward-auth app is a normal **`oidc_client`** flagged for forward-auth.
Migration `018` adds to `oidc_client`:
- `forward_auth_enabled boolean NOT NULL DEFAULT false`
- `forward_auth_host text NULL` — the `X-Forwarded-Host` to match (e.g. `app.acme.io`); unique among forward-auth-enabled clients.

Its `redirect_uris` includes `https://app.acme.io/.prohibitorum-forward-auth/callback`.
**Per-service RBAC reuses `oidc_client_access` + `access_restricted`** unchanged
— admins grant groups/accounts to the backing client.

### The two cookies
- **Session cookie** (Prohibitorum domain) — unchanged (`__Host-`, host-only).
  Used only during the OIDC `/oauth/authorize` step on the Prohibitorum domain.
- **Per-domain forward-auth cookie** — minted on `app.acme.io` by the callback;
  opaque token → KV `fa_session:<token>` = `{account_id, client_id, exp}`.
  Host-only, `Secure`, `HttpOnly`, `SameSite=Lax` (survives the top-level GET
  callback→original redirect). Short TTL (configurable). Re-validated + RBAC
  re-checked on every verify.

### Flow
```
1. Browser → app.acme.io/foo
   Traefik ForwardAuth → GET https://auth.example.com/api/prohibitorum/forward-auth/verify
       (forwards X-Forwarded-Host=app.acme.io, …Uri=/foo, + app.acme.io cookies)
2. verify: resolve client by forward_auth_host; no/invalid fa-cookie
       → 302 /oauth/authorize?client_id=<app>&redirect_uri=https://app.acme.io/.prohibitorum-forward-auth/callback
              &response_type=code&scope=openid email groups&state=<signed: original https://app.acme.io/foo>
3. Browser → auth.example.com/oauth/authorize  (session cookie present)
       → login if needed → EXISTING access_restricted/RBAC check → consent skipped (require_consent=false)
       → 302 https://app.acme.io/.prohibitorum-forward-auth/callback?code=…&state=…
4. Traefik routes /.prohibitorum-forward-auth/* → callback handler (Prohibitorum):
       in-process code→identity exchange → mint KV fa_session
       → Set-Cookie (host-only, on app.acme.io) → 302 to original (from validated state)
5. Browser → app.acme.io/foo → verify: fa-cookie valid + live RBAC ok → 200 + Remote-* headers
       → Traefik forwards the original request to the backend with those headers
```

---

## Components / endpoints

| Unit | Responsibility |
|---|---|
| `GET /api/prohibitorum/forward-auth/verify` | The ForwardAuth target. Resolve client by `X-Forwarded-Host`; validate the per-domain fa-cookie + **live** access; 200 + identity headers, or 302 into the OIDC flow, or 403 (unknown host). |
| `GET /api/prohibitorum/forward-auth/callback` (reached on the protected domain via the routed prefix) | In-process OIDC code exchange → mint KV fa-session → Set-Cookie on the protected domain → 302 to the validated original URL. |
| KV `fa_session` | Per-domain forward-auth session: `{account_id, client_id, exp}`, short TTL. Reuses the existing `kv.Store` (`SetEx`/`Get`/`Delete`). |
| Per-domain cookie helper | Host-only `Secure HttpOnly SameSite=Lax` cookie scoped to the protected host. |
| `oidc_client` columns (migration `018`) | `forward_auth_enabled`, `forward_auth_host`. |
| Identity headers | `Remote-User` (username), `Remote-Name` (displayName), `Remote-Email` (if set), `Remote-Groups` (comma-joined `ListExposedGroupSlugsByAccount`). |
| State | Signed (or KV single-use) value carrying the original URL + nonce, minted at verify, validated at callback. |
| CLI registration | Create the backing OIDC client + set `forward_auth_enabled`/`forward_auth_host` (extend `oidc-client create` or a thin `forward-auth-app` wrapper). RBAC grants via the existing app-access CLI/endpoints. |
| Traefik docs | `docs/` page: the ForwardAuth middleware + EntryPoint `trustedIPs`/`trustForwardHeader`, the `/.prohibitorum-forward-auth/*` router, `authResponseHeaders`, and the OIDC client registration. |

### Endpoint contract (verify)
- Valid fa-cookie + account enabled + access granted → **200**, headers `Remote-User/Name/Email/Groups`.
- Missing/invalid/expired fa-cookie, OR account/access no longer valid → **302** to `/oauth/authorize` (the original URL is reconstructed from `X-Forwarded-Proto/Host/Uri` and carried in `state`).
- `X-Forwarded-Host` matches no forward-auth-enabled client → **403** (fail-closed).

---

## Security (research-driven)

- **Header spoofing** ([research](https://www.authelia.com/overview/security/measures/)): deployment requirement, documented — Traefik is the sole ingress; set `forwardedHeaders.trustedIPs` (EntryPoint) + `trustForwardHeader: true` (middleware) so client-spoofed `X-Forwarded-*` are stripped; `authResponseHeaders` replaces any client-sent `Remote-*`. We emit identity headers only on 200.
- **Open redirect** ([OWASP](https://cheatsheetseries.owasp.org/cheatsheets/Unvalidated_Redirects_and_Forwards_Cheat_Sheet.html), [oauth2-proxy](https://github.com/oauth2-proxy/oauth2-proxy/issues/724)): the post-login target lives in the OIDC `state` (signed/KV, minted server-side at verify), not reconstructed from spoofable headers at callback time; and the callback domain is the OIDC client's **exact-match `redirect_uri`** (the existing authorize open-redirect guard allowlists it). The reconstructed original URL's host is validated to equal the client's `forward_auth_host`.
- **Live authorization:** verify re-runs the existing access query each request, so revocation / group / disable changes take effect immediately, not at cookie expiry.
- **HTTPS-only**, short fa-session TTL, host-only `Secure HttpOnly` cookie. Per-domain cookies are isolated (host-only) — no cross-app cookie sharing.
- **Replay isolation:** the fa-session stores `client_id`; verify confirms the cookie's session matches the client resolved from `X-Forwarded-Host`.

---

## Testing

- **verify:** 200 + correct headers (valid cookie, access granted); 302 into `/oauth/authorize` with a well-formed `state` (no cookie); 403 (unknown host); 302 when access is revoked live (cookie valid but RBAC now denies).
- **callback:** code exchange → fa-session minted → host-only cookie set → 302 to the state's original URL; rejects a bad/missing/mismatched `state`; rejects a `redirect_uri`/host mismatch.
- **KV fa-session lifecycle:** mint, load, expiry, delete.
- **Open-redirect guards:** state tamper rejected; original-host ≠ `forward_auth_host` rejected.
- **Runtime:** a smoke/curl sequence simulating Traefik's forwarded headers + the per-domain cookie across the verify→authorize→callback→verify cycle (full multi-domain browser flow is Phase 2's dev harness).

## Phase 2 (deferred, separate spec)
Dashboard admin UI for forward-auth services (presented separately from OIDC
clients by filtering `forward_auth_enabled`); multi-domain dev harness;
forward-auth sign-out + revocation propagation.
