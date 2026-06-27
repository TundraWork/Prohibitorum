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

## 3. Security requirements

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
