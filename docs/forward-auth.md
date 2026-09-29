# Traefik ForwardAuth integration

Prohibitorum natively answers Traefik's **ForwardAuth** middleware — no oauth2-proxy. It reuses the OIDC provider for login and the existing per-application RBAC for authorization. Works for apps on unrelated domains (e.g. Prohibitorum on `auth.example.com`, protected app on `app.acme.io`): the first request drives OIDC login; subsequent requests are a fast `200`/redirect check against a host-only **per-domain cookie**.

> **HTTPS is required.** The per-domain cookie is `Secure`; forward-auth does not work over plain HTTP.

---

## 1. Register the protected service

```bash
prohibitorum forward-auth-app create \
  --client-id app-acme \
  --host app.acme.io \
  --display-name "Acme App"
```

Creates a public (PKCE) OIDC client with `redirect_uri = https://app.acme.io/.prohibitorum-forward-auth/callback`, consent disabled, flagged for forward-auth on `app.acme.io`.

**RBAC.** By default any logged-in user is allowed. To restrict:

```bash
# Restrict to granted principals, then grant a group (and/or --grant-account):
prohibitorum oidc-client access --client-id app-acme --access-restricted=true --grant-group staff
```

Access is re-evaluated **live on every request**.

---

## 2. Configure Traefik

### Trust the proxy chain at the EntryPoint (required — see Security)

```yaml
# static config
entryPoints:
  websecure:
    address: ":443"
    forwardedHeaders:
      trustedIPs:
        - "10.0.0.0/8"      # your trusted upstream proxy / LB ranges ONLY
```

### Middleware + router (dynamic config)

```yaml
http:
  middlewares:
    prohibitorum-forwardauth:
      forwardAuth:
        address: "https://auth.example.com/api/prohibitorum/forward-auth/verify"
        trustForwardHeader: true
        addAuthCookiesToResponse:
          - __Host-prohibitorum_forward_auth
        authResponseHeaders:
          - Remote-User
          - Remote-Name
          - Remote-Email
          - Remote-Groups

  routers:
    # The protected app — gated by the forward-auth middleware.
    acme-app:
      rule: "Host(`app.acme.io`)"
      entryPoints: ["websecure"]
      middlewares: ["prohibitorum-forwardauth"]
      service: acme-app-backend
      tls: {}

    # The per-domain auth/callback path — routed to Prohibitorum, NOT the app,
    # and NOT gated by the forward-auth middleware. This is where the OIDC
    # callback lands so the per-domain cookie is scoped to app.acme.io.
    acme-app-forwardauth:
      rule: "Host(`app.acme.io`) && PathPrefix(`/.prohibitorum-forward-auth/`)"
      entryPoints: ["websecure"]
      service: prohibitorum
      tls: {}
```

Docker-label equivalents follow the same shape: a `forwardauth` middleware with `address`, `trustForwardHeader=true`, `authResponseHeaders`, `addAuthCookiesToResponse=__Host-prohibitorum_forward_auth`, plus a second router for `PathPrefix(/.prohibitorum-forward-auth/)` → the Prohibitorum service.

The backend reads identity from the `Remote-*` request headers (present only on allowed requests).

### Session renewal

Successful cookie-authenticated requests renew the server session when its
remaining lifetime is at most one quarter of `forward_auth.session_ttl`
(default 1h). Active users can keep working beyond the initial hour. An expired
session still requires the normal login redirect; denied requests and PAT requests
do not renew a browser session.

Verify sends a host-only `Set-Cookie` on renewal. Configure
`addAuthCookiesToResponse` as above so Traefik copies it to the protected
application's response. Do not add `Set-Cookie` to `authResponseHeaders`:
that setting copies headers to the upstream request. The cookie remains a browser
session cookie (no Max-Age); renewal extends the server lifetime without changing
the random token, so parallel requests and sign-out refer to the same session.
Existing sessions can renew without signing in again after upgrading.

A continuously open WebSocket does not make new verify requests. Renewal occurs
on subsequent HTTP requests that pass through the middleware.

### Sign out

Link users to:

    https://<protected-host>/.prohibitorum-forward-auth/sign_out

Clears the per-domain cookie + session, bounces to Prohibitorum to terminate the SSO session, then returns the browser to the app. Forward-auth sessions on *other* protected domains remain valid while used and until their sliding lifetime expires (`forward_auth.session_ttl`, default 1h) or a live authorization check denies them.

> `sign_out` is served by Prohibitorum, so the same `PathPrefix(/.prohibitorum-forward-auth/)` router already covers it — no extra Traefik config needed.

---

## 3. Personal Access Tokens

A **Personal Access Token (PAT)** is a user-owned bearer credential that can authenticate to the forward-auth verify endpoint, and, at the upper levels, to the management API. This lets non-browser API or automation clients traverse the gateway without a session cookie.

### Access levels

Each PAT carries one **access level**. When a user creates a token they pick it; the levels include one another in this order:

| Level | At the gateway | Management API (`/api/prohibitorum/*`) |
|-------|----------------|----------------------------------------|
| `selected_apps` | only the forward-auth apps chosen on the token | not allowed (`403 pat_api_not_allowed`) |
| `all_apps` | every forward-auth app the owner is authorized to access | not allowed (`403 pat_api_not_allowed`) |
| `full` | same as `all_apps` | allowed as the owner; actions that need a fresh sudo fail with `401 sudo_required` |
| `sudo` | same as `all_apps` | allowed as the owner; every sudo check passes, so it can also create further PATs |

No level grants more than the owner already has. The gateway still applies the protected app's access policy on every request, and the management API still applies the owner's role and app-manager assignments. Apps carry no scope vocabulary and the gateway sends no `Remote-Scopes` header; the protected service decides what an identity may do. Calling the management API with a PAT is described in `api.md`.

### PAT authentication at the verify endpoint

When `GET /api/prohibitorum/forward-auth/verify` receives an `X-Prohibitorum-PAT: <PAT>` header, it enters **API mode** — a terminal path that never redirects:

| Outcome | HTTP | Meaning |
|---------|------|---------|
| Valid token, owner allowed | `200` | Identity headers emitted; Traefik forwards request upstream. |
| Invalid, expired, or revoked token; disabled owner | `401` | Token authentication failed. |
| Valid token, owner not authorized for this app | `403` | PAT does not grant access to this app, or RBAC denied. |

No `X-Prohibitorum-PAT` header present → the existing browser flow: valid cookie → `200`, no/expired cookie → `302` into the login flow.

PATs act as the owning user with the **intersection** of the owner's authorization, the token's access level (for `selected_apps`, its listed apps), and the protected-app access policy.

Send the raw token without a `Bearer ` prefix. An empty, repeated, malformed, or
whitespace-padded PAT header returns `401` (the value is not trimmed) even if a
valid browser cookie is present. The verifier
ignores `Authorization`, leaving Basic/Bearer credentials available to the
protected application.

**Upgrade:** change PAT clients from `Authorization: Bearer <PAT>` to
`X-Prohibitorum-PAT: <PAT>`. Replace any proxy middleware that strips
`Authorization` with one that strips only `X-Prohibitorum-PAT`, after forward-auth.
There is no legacy Authorization fallback.

PATs are accepted at the forward-auth verify endpoint and, for `full` and `sudo` levels, at the management API under the same `X-Prohibitorum-PAT` header. They are never accepted at OIDC/SAML endpoints.

### Authoritative identity headers

The gateway emits four headers on every allowed request, **all unconditionally** (even empty), so Traefik's `authResponseHeaders` copy overwrites any client-supplied value:

| Header | Content |
|--------|---------|
| `Remote-User` | Configured account identifier. Each forward-auth app can use the stable account `sub`, username, or unique verified email; username is the default. |
| `Remote-Name` | Display name. |
| `Remote-Email` | Primary email address. |
| `Remote-Groups` | Comma-joined group slugs exposed to downstreams. |

The application setting applies to browser sessions and PAT requests on the next verification. Choosing verified email requires the current account email to be verified and unique across accounts. If the selected value is missing or ambiguous, verification returns `403` without identity headers instead of falling back to another identifier. Changing the source can make the protected application treat an existing person as a different account.

The operator **must** list all four in `authResponseHeaders` (or use `authResponseHeadersRegex: "Remote-.*"`) so Prohibitorum's authoritative values always overwrite any client-supplied copies. Update the Traefik middleware from the example in section 2:

```yaml
authResponseHeaders:
  - Remote-User
  - Remote-Name
  - Remote-Email
  - Remote-Groups
```

### Required Traefik configuration for PAT-protected routers

> Because Prohibitorum runs as a forward-auth verifier and is not in the request data path, it cannot unilaterally remove the client's raw PAT from the upstream request. Deployments must configure Traefik to forward the authoritative `Remote-*` headers from Prohibitorum and strip the original `X-Prohibitorum-PAT` header before the request reaches upstream.

Two requirements:

1. **`authResponseHeaders` for all four `Remote-*` headers** — ensures the gateway's verified values reach the upstream service (see example above).

2. **An explicit `headers` middleware that removes the inbound `X-Prohibitorum-PAT` header** before the request is forwarded upstream. Do NOT rely on `authResponseHeaders` to clear `X-Prohibitorum-PAT` — that behaviour is not guaranteed across Traefik versions. Strip it explicitly on PAT-protected routers.

Example middleware and router configuration:

```yaml
http:
  middlewares:
    prohibitorum-forwardauth:
      forwardAuth:
        address: "https://auth.example.com/api/prohibitorum/forward-auth/verify"
        trustForwardHeader: true
        addAuthCookiesToResponse:
          - __Host-prohibitorum_forward_auth
        authResponseHeaders:
          - Remote-User
          - Remote-Name
          - Remote-Email
          - Remote-Groups

    strip-prohibitorum-pat:
      headers:
        customRequestHeaders:
          X-Prohibitorum-PAT: ""   # empty string → Traefik removes the header

  routers:
    acme-app:
      rule: "Host(`app.acme.io`)"
      entryPoints: ["websecure"]
      # Chain: forward-auth first, then strip X-Prohibitorum-PAT before reaching backend.
      middlewares:
        - prohibitorum-forwardauth
        - strip-prohibitorum-pat
      service: acme-app-backend
      tls: {}
```

The `strip-prohibitorum-pat` middleware is only required on routers where PAT-bearing clients are expected. Browser-only apps that never send `X-Prohibitorum-PAT` headers can omit it, though adding it is harmless.

---

## 4. Security requirements

- **Set both `forwardedHeaders.trustedIPs` and `trustForwardHeader: true`.** `trustedIPs` makes Traefik strip client-supplied `X-Forwarded-*` and set them from the real request; `trustForwardHeader: true` then forwards those trusted values. Prohibitorum reconstructs the original request and resolves the protected service from `X-Forwarded-Host` / `X-Forwarded-Proto` / `X-Forwarded-Uri` — a client that can set those can influence the redirect target. Do not expose the EntryPoint to untrusted networks without `trustedIPs`.

- **The proxy must be the only path to the backend.** Identity is conveyed by `Remote-*` headers. Any route that bypasses Traefik (another container on the same network, a LAN/VPN route) can forge `Remote-User` / `Remote-Groups`. Bind backends to internal networks only.

- **No other middleware may introduce client-controlled `Remote-*` headers.** `authResponseHeaders` replaces the listed headers with Prohibitorum's verified values on an allowed request, but a middleware upstream of that replacement could reintroduce client values.

- **HTTPS only**, valid certs, ideally HSTS — the per-domain cookie is `Secure`.

---

## Local dev harness

```bash
mise run dev:forward-auth
```

On first run writes a template to `.dev/dev-forward-auth.env` (gitignored) with `example.test` placeholder hostnames and cert paths. Fill in real values (both hostnames must resolve to `127.0.0.1`), then re-run. The harness builds the binary, seeds a dev database, registers the forward-auth app client, starts a `forward-auth-whoami` server that echoes the injected `Remote-*` headers, and generates Traefik config (`.dev/traefik/traefik.yml` + `.dev/traefik/dynamic.yml`) mirroring the canonical setup. If `traefik` is on your `PATH` the harness launches it; otherwise it prints the `traefik --configFile=…` command.

(Traefik, not nginx: the verify endpoint answers an unauthenticated request with a `302` into the login flow, which ForwardAuth forwards to the browser; nginx's `auth_request` cannot.)

Open the protected app URL; after login you should see the identity headers echoed as plain text.

---

## How it works

```
1. Browser → app.acme.io/foo
   Traefik ForwardAuth → GET https://auth.example.com/api/prohibitorum/forward-auth/verify
                         (Traefik forwards X-Forwarded-* + app.acme.io cookies)
2. No/expired forward-auth cookie → 302 into Prohibitorum's OIDC login
   (on auth.example.com, where your Prohibitorum session lives; access is enforced here)
3. → 302 app.acme.io/.prohibitorum-forward-auth/callback?code=…&state=…
   Traefik routes that path to Prohibitorum, which plants a host-only
   forward-auth cookie on app.acme.io and 302s back to /foo
4. Browser → app.acme.io/foo → verify sees the cookie + live access → 200 + Remote-* headers
   Traefik forwards the request to the backend with those headers
```

Endpoints:
- **`GET /api/prohibitorum/forward-auth/verify`** — the ForwardAuth target: `200` + identity headers (allowed), `302` to login (unauthenticated), or `403` (host not a registered forward-auth service).
- **`/.prohibitorum-forward-auth/callback`** — routed by you on each protected domain to Prohibitorum; completes the OIDC exchange and sets the per-domain cookie.
- **`/.prohibitorum-forward-auth/sign_out`** — routed the same way; clears the per-domain cookie + session and `302`s to **`GET /api/prohibitorum/forward-auth/sso-logout`**, which terminates the SSO session and redirects back only to a validated forward-auth host.
