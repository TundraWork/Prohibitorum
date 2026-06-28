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
        authResponseHeaders:
          - Remote-User
          - Remote-Name
          - Remote-Email
          - Remote-Groups
          - Remote-Scopes

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

Docker-label equivalents follow the same shape: a `forwardauth` middleware with `address`, `trustForwardHeader=true`, `authResponseHeaders`, plus a second router for `PathPrefix(/.prohibitorum-forward-auth/)` → the Prohibitorum service.

The backend reads identity from the `Remote-*` request headers (present only on allowed requests).

### Sign out

Link users to:

    https://<protected-host>/.prohibitorum-forward-auth/sign_out

Clears the per-domain cookie + session, bounces to Prohibitorum to terminate the SSO session, then returns the browser to the app. Forward-auth sessions on *other* protected domains remain valid until they expire (`forward_auth.session_ttl`, default 1h) or a live authorization check denies them.

> `sign_out` is served by Prohibitorum, so the same `PathPrefix(/.prohibitorum-forward-auth/)` router already covers it — no extra Traefik config needed.

---

## 3. Personal Access Tokens

A **Personal Access Token (PAT)** is a user-owned bearer credential that can authenticate to the forward-auth verify endpoint. This enables non-browser API or automation clients to traverse the gateway without a session cookie.

### Per-app scope model

Each forward-auth app (OIDC client) carries an admin-defined **scope vocabulary** — a list of `{name, description}` pairs that the operator declares meaningful for that app. Examples: `read`, `write`, `admin:users`, or any opaque label the upstream service understands.

When a user creates a PAT they choose **which forward-auth apps** the token may reach, and **which scopes** to request from each app's vocabulary. Two grant modes:

- **Per-app grants** (`allApps: false`) — the PAT lists one or more forward-auth apps; for each app the user selects a subset of that app's declared scopes. The token is only accepted at those specific apps.
- **All-apps mode** (`allApps: true`) — the PAT is accepted at every forward-auth app the owner is authorized to access. No per-app scopes are carried; `Remote-Scopes` is always empty. Useful for identity-only integrations that do not consume scopes.

**Per-app isolation.** When the gateway evaluates a PAT-bearing request it knows which forward-auth app is being accessed (from `X-Forwarded-Host`). It emits `Remote-Scopes` containing *only* the scopes the PAT granted to **that specific app**. Scopes granted to other apps in the same PAT are never leaked to the current app.

### Bearer authentication at the verify endpoint

When `GET /api/prohibitorum/forward-auth/verify` receives an `Authorization: Bearer <PAT>` header, it enters **API mode** — a terminal path that never redirects:

| Outcome | HTTP | Meaning |
|---------|------|---------|
| Valid token, owner allowed | `200` | Identity headers emitted (including `Remote-Scopes`); Traefik forwards request upstream. |
| Invalid, expired, or revoked token; disabled owner | `401` | Token authentication failed. |
| Valid token, owner not authorized for this app | `403` | PAT does not grant access to this app, or RBAC denied. |

No `Authorization` header present → the existing browser flow: valid cookie → `200`, no/expired cookie → `302` into the login flow.

PATs act as the owning user with the **intersection** of the owner's authorization, the PAT's per-app grants (`appGrants`), and the protected-app access policy.

PATs are accepted **only** at the forward-auth verify endpoint. They are not accepted at the admin API or OIDC/SAML endpoints.

### Authoritative identity headers

The gateway emits five headers on every allowed request, **all unconditionally** (even empty), so Traefik's `authResponseHeaders` copy overwrites any client-supplied value:

| Header | Content |
|--------|---------|
| `Remote-User` | Subject identifier of the authenticated user. |
| `Remote-Name` | Display name. |
| `Remote-Email` | Primary email address. |
| `Remote-Groups` | Comma-joined group slugs exposed to downstreams. |
| `Remote-Scopes` | Comma-joined scopes the PAT granted to **this specific app** (per-app isolation); **empty string for cookie/browser sessions and for `allApps` PATs**. The gateway does not interpret these labels — the upstream service enforces them. |

The operator **must** list all five in `authResponseHeaders` (or use `authResponseHeadersRegex: "Remote-.*"`) so Prohibitorum's authoritative values always overwrite any client-supplied copies. Update the Traefik middleware from the example in section 2:

```yaml
authResponseHeaders:
  - Remote-User
  - Remote-Name
  - Remote-Email
  - Remote-Groups
  - Remote-Scopes
```

### Required Traefik configuration for PAT-protected routers

> Because Prohibitorum runs as a forward-auth verifier and is not in the request data path, it cannot unilaterally remove the client's raw PAT from the upstream request. Deployments must configure Traefik to forward the authoritative `Remote-*` headers from Prohibitorum and strip the original `Authorization` header before the request reaches upstream.

Two requirements:

1. **`authResponseHeaders` for all five `Remote-*` headers** — ensures the gateway's verified values reach the upstream service (see example above).

2. **An explicit `headers` middleware that removes the inbound `Authorization` header** before the request is forwarded upstream. Do NOT rely on `authResponseHeaders` to clear `Authorization` — that behaviour is not guaranteed across Traefik versions. Strip it explicitly on PAT-protected routers.

Example middleware and router configuration:

```yaml
http:
  middlewares:
    prohibitorum-forwardauth:
      forwardAuth:
        address: "https://auth.example.com/api/prohibitorum/forward-auth/verify"
        trustForwardHeader: true
        authResponseHeaders:
          - Remote-User
          - Remote-Name
          - Remote-Email
          - Remote-Groups
          - Remote-Scopes

    strip-authorization:
      headers:
        customRequestHeaders:
          Authorization: ""   # empty string → Traefik removes the header

  routers:
    acme-app:
      rule: "Host(`app.acme.io`)"
      entryPoints: ["websecure"]
      # Chain: forward-auth first, then strip Authorization before reaching backend.
      middlewares:
        - prohibitorum-forwardauth
        - strip-authorization
      service: acme-app-backend
      tls: {}
```

The `strip-authorization` middleware is only required on routers where PAT-bearing clients are expected. Browser-only apps that never send `Authorization` headers can omit it, though adding it is harmless.

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
