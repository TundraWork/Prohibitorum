# Traefik ForwardAuth integration

Prohibitorum can natively answer Traefik's **ForwardAuth** middleware, so you can
protect any service behind Traefik with your Prohibitorum login — **without
running a separate reverse-proxy component** (no oauth2-proxy). It reuses
Prohibitorum's OIDC provider for the login + the existing per-application access
control (RBAC) for authorization.

It works for protected services on **unrelated domains** (e.g. Prohibitorum on
`auth.example.com`, the app on `app.acme.io`). On the first request the user is
sent through the OIDC login and a small, host-only **per-domain cookie** is
planted on the protected domain; afterwards every request is just a fast
`200`/redirect check.

> **HTTPS is required.** The per-domain cookie is `Secure`; forward-auth does not
> work over plain HTTP. Use valid certificates (and ideally HSTS).

---

## 1. Register the protected service

Each protected service is a backing OIDC client flagged for forward-auth.
Register it with the CLI:

```bash
prohibitorum forward-auth-app create \
  --client-id app-acme \
  --host app.acme.io \
  --display-name "Acme App"
```

This creates a public (PKCE) OIDC client with
`redirect_uri = https://app.acme.io/.prohibitorum-forward-auth/callback` and
consent disabled, and flags it for forward-auth on host `app.acme.io`.

**Authorization (RBAC).** By default any logged-in user is allowed. To restrict
the service to specific groups/accounts, mark it access-restricted and grant
access using the existing OIDC-client access commands, e.g.:

```bash
# Restrict to granted principals, then grant a group (and/or --grant-account):
prohibitorum oidc-client access --client-id app-acme --access-restricted=true --grant-group staff
```

Access is re-evaluated **live on every request**, so revoking a group or
disabling an account takes effect immediately.

---

## 2. Configure Traefik

Two hookups are needed on the protected service's router: the ForwardAuth
**middleware** (the auth check) and a **route** for the per-domain callback path.

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

(Docker-label equivalents follow the same shape: a `forwardauth` middleware with
`address`, `trustForwardHeader=true`, `authResponseHeaders`, plus a second router
for the `PathPrefix(/.prohibitorum-forward-auth/)` → the Prohibitorum service.)

The backend reads identity from the `Remote-*` request headers (only present on
an allowed request).

### Sign out

The forward-auth prefix also serves a sign-out endpoint. Link users to:

    https://<protected-host>/.prohibitorum-forward-auth/sign_out

It clears the per-domain forward-auth cookie + session, then bounces to
Prohibitorum to terminate the SSO session and returns the browser to the app
(now unauthenticated, so the next request triggers a fresh login). Note:
forward-auth sessions already established on *other* protected domains remain
valid until they expire (`forward_auth.session_ttl`, default 1h) or the next
live authorization check denies them — signing out is immediate for the
dashboard and this app, and prevents silent re-login elsewhere, but does not
retroactively revoke other domains' per-domain cookies.

> The `sign_out` path is served by Prohibitorum, so the same forward-auth
> `PathPrefix(/.prohibitorum-forward-auth/)` router that handles `/callback`
> already covers it — no extra Traefik config is needed.

---

## 3. Security requirements (read this)

Forward-auth is only as trustworthy as the network boundary. These are operator
obligations, not optional hardening:

- **Traefik must OVERWRITE the forwarded headers — never pass client values
  through.** Prohibitorum reconstructs the original request (for the post-login
  redirect) and resolves the protected service from
  `X-Forwarded-Host` / `X-Forwarded-Proto` / `X-Forwarded-Uri`. If a client could
  set those, it could influence the redirect target. Setting
  `forwardedHeaders.trustedIPs` at the EntryPoint makes Traefik strip
  client-supplied `X-Forwarded-*` and set them itself from the real request;
  `trustForwardHeader: true` on the middleware then forwards those trusted
  values. **Set both.** Do not expose the EntryPoint to untrusted networks
  without `trustedIPs`.

- **The proxy must be the only way to reach the backend.** Identity is conveyed
  by the `Remote-*` headers. If the backend is reachable by any path that
  bypasses Traefik (another container on the same network, a LAN/VPN route),
  that path can forge `Remote-User`/`Remote-Groups`. Bind backends to internal
  networks only.

- **Strip client-supplied `Remote-*` headers.** `authResponseHeaders` already
  replaces the listed headers with Prohibitorum's verified values on an allowed
  request, but ensure no other middleware re-introduces client-controlled
  `Remote-*` headers toward the backend.

- **HTTPS only**, valid certs, ideally HSTS — the per-domain cookie is `Secure`.

---

## Local dev harness

To exercise the full browser flow locally — app → verify → authorize → callback → 200 with `Remote-*` headers — use the built-in dev harness:

```bash
mise run dev:forward-auth
```

On first run it writes a template to `.dev/dev-forward-auth.env` (gitignored) with `example.test` placeholder hostnames and cert paths. Fill in your real values (both hostnames must resolve to `127.0.0.1`), then re-run. The harness builds the binary, seeds a dev database, registers the forward-auth app client, starts a tiny `forward-auth-whoami` server that echoes the injected `Remote-*` headers, and generates a **Traefik** front (a static config at `.dev/traefik/traefik.yml` and a dynamic config at `.dev/traefik/dynamic.yml`) mirroring the canonical setup above — the forward-auth middleware, the per-domain `/.prohibitorum-forward-auth` router, and the backend services. If `traefik` is on your `PATH` the harness launches it for you; otherwise it prints the exact `traefik --configFile=…` command to run. (Traefik, not nginx: the verify endpoint answers an unauthenticated request with a `302` into the login flow, which Traefik's ForwardAuth forwards to the browser; nginx's `auth_request` cannot.) Open the protected app URL in a browser; after logging in you should see the identity headers echoed as plain text.

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

The endpoints:
- **`GET /api/prohibitorum/forward-auth/verify`** — the ForwardAuth target:
  `200` + identity headers (allowed), `302` to login (unauthenticated), or `403`
  (the host is not a registered forward-auth service).
- **`/.prohibitorum-forward-auth/callback`** — routed by you on each protected
  domain to Prohibitorum; completes the OIDC exchange and sets the per-domain
  cookie.
- **`/.prohibitorum-forward-auth/sign_out`** — routed the same way; clears the
  per-domain cookie + session and 302s to the IdP-domain
  **`GET /api/prohibitorum/forward-auth/sso-logout`**, which terminates the SSO
  session and redirects back only to a validated forward-auth host.
