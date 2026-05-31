# Session-cookie scoping fix — design

**Goal:** Make an authenticated browser session reach Prohibitorum's root-level
OIDC/SAML protocol endpoints, fixing the cookie-path mismatch surfaced by the
v0.6 post-implementation audit. The session cookie is currently scoped
`Path=/api/prohibitorum`, so a real browser never sends it to `/oauth/authorize`,
`/saml/sso`, `/saml/sso/init`, or `/saml/slo` — the session gate sees no session
and bounces to `/login` in an unbreakable loop. (The `cmd/smoke` harness masks
this by manually re-attaching the cookie to root-path requests, a maneuver a
browser won't perform.)

**Approach:** Scope the session cookie to `Path=/` (the universal standard — an
IdP's login UI and its authorize/SSO endpoints share one origin, so a root-path
cookie covers both). Adopt the `__Host-` prefix in secure deployments for
browser-enforced session-fixation / subdomain-injection defense, falling back to
a plain non-`Secure` cookie in HTTP dev. Purely a cookie-identity/scoping change
— no route, issuer, or discovery-metadata changes.

**Context:**
- Root cause + the finding: `AUDIT.md` → "v0.6 post-impl audit … Architectural
  finding"; `docs/superpowers/notes/2026-05-31-session-handoff.md`.
- Current code: `pkg/session/middleware.go` — `FreshSessionCookie`,
  `ClearedSessionCookie`, `CeremonyCookie`, `isSecure`, and the `LoadSession`
  read path; `SessionCookieName`.
- Research (mainstream IdP practice, 2026-05-31): IdP session cookies are
  `Path=/`, `SameSite=Lax`, `HttpOnly`, `Secure`; the `__Host-` prefix is the
  gold standard for a single-host IdP session cookie (MDN/OWASP; Keycloak/Ory
  Hydra both keep the session cookie at the IdP origin covering the authorize
  endpoint). `SameSite=Lax` is the safe default that survives top-level redirect
  navigations; `Strict` breaks the redirect; `None` is only needed for cross-site
  POST/iframe flows and is "less secure" (Curity).

---

## Decisions

- **D1 — `Path=/`.** The session cookie's `Path` changes from `/api/prohibitorum`
  to `/`, so it is sent to the root-level protocol routes. The prior "narrow path
  so static assets don't carry it" optimization is dropped — the cookie is an
  opaque `HttpOnly` token and being sent on all paths is exactly what every
  mainstream IdP does. The stale comment is corrected.

- **D2 — `SameSite=Lax`.** Unchanged from today. Covers the dominant flows: OIDC
  `/oauth/authorize` (top-level GET redirect) and SAML HTTP-Redirect-binding
  AuthnRequests (incl. GHES). Known gap (accepted, documented): a SAML
  HTTP-POST-binding AuthnRequest is a cross-site POST, so a logged-in user hitting
  that path won't have the cookie sent and bounces through `/login` once — the
  same family as the already-deferred `ForceAuthn`+POST-binding limitation. We do
  NOT adopt `SameSite=None` (it requires `Secure`/HTTPS-always, broadens the
  cross-site exposure, and is increasingly browser-restricted).

- **D3 — Deployment-conditional `__Host-` + `Secure`.** A single
  `secureCookies` signal, derived from the canonical public origin's scheme
  (`scheme(cfg.PublicOrigins[0]) == "https"`), governs both the cookie name prefix
  and the `Secure` attribute. This is deployment-stable and TLS-proxy-safe (the
  operator declares the public origin), avoiding per-request variance in the
  cookie identity.

  | | Secure deployment (`PUBLIC_ORIGIN` = https) | Dev (http, e.g. `http://localhost:8080`) |
  |---|---|---|
  | Name | `__Host-<base>` | `<base>` (plain) |
  | `Secure` | `true` | `false` |
  | `Path` | `/` | `/` |
  | `SameSite` | `Lax` | `Lax` |
  | `HttpOnly` | `true` | `true` |
  | `Domain` | (none) | (none) |

  Rationale for the dev fallback: `__Host-` *requires* `Secure`, and Go's
  `net/http/cookiejar` will not send a `Secure` cookie over `http://` — so an
  always-`Secure` cookie would be unusable by HTTP clients (incl. the smoke).
  This is the conventional dev/prod cookie split.

- **D4 — `CeremonyCookie` unchanged.** It stays `Path=/api/prohibitorum/auth`,
  `SameSite=Strict` — it is used only within the WebAuthn API ceremony (which
  lives under `/api/prohibitorum/auth`), so its narrow path is correct, and it
  *cannot* be `__Host-` (that prefix forbids a sub-path). Out of scope.

- **D5 — No route/issuer/metadata changes.** The protocol routes stay root-level;
  the OIDC issuer (`PUBLIC_ORIGIN`), discovery document, SAML EntityID, and SP
  ACS targets are unchanged. (Moving the routes under `/api/prohibitorum` to match
  the old cookie path was considered and rejected — it would break every
  published OIDC/SAML endpoint and registered RP/SP.) We fix the cookie, not the
  routes.

---

## Components

`pkg/session/middleware.go`:
- A small helper `secureCookies(cfg *configx.Config) bool` — `true` iff the first
  public origin's scheme is `https`. (Single source of truth for D3.)
- A `sessionCookieName(secure bool) string` — `"__Host-" + base` when secure, else
  `base`. Used symmetrically by set, clear, and read.
- `FreshSessionCookie` / `ClearedSessionCookie` — set `Path:"/"`, the conditional
  name, `Secure: secureCookies(cfg)`, `SameSite: Lax`, `HttpOnly: true`, no
  `Domain`. (Clear must match name+path+attributes to delete.)
- `LoadSession` (the read path) — read the cookie by `sessionCookieName(secureCookies(cfg))`.
  If a deployment flips scheme, old cookies under the other name are simply not
  found → treated as no session (re-login); acceptable.

`cmd/smoke/`:
- Drop the manual cookie-re-attach workaround in `authorizeWithSession` (and any
  sibling that hand-attaches the session cookie to root-path requests). Over the
  HTTP dev server the cookie is now plain (non-`Secure`) + `Path=/`, so the Go
  cookie-jar sends it to `/oauth/*` and `/saml/*` automatically — the smoke then
  *behaviorally* exercises the browser-equivalent path.

---

## Data flow (the fix, in a real HTTPS browser)

1. User authenticates (API ceremony) → `FreshSessionCookie` sets
   `__Host-<base>=…; Path=/; Secure; SameSite=Lax; HttpOnly`.
2. An RP/SP sends the browser to `GET /oauth/authorize?…` or `GET /saml/sso?…`
   (top-level navigation). The browser attaches the `Path=/` cookie (Lax permits
   top-level navigations).
3. The session gate sees the session → issues the code/assertion. No `/login`
   loop.

---

## Error handling / edge cases

- **Non-localhost plain-HTTP deployment** (misconfigured prod): `secureCookies`
  is false → a plain non-`Secure` cookie is set (works), but without `__Host-`
  hardening. This is an operator misconfiguration (prod must be HTTPS); no special
  handling beyond the dev fallback.
- **Scheme flip mid-deployment**: cookies under the previous name aren't read →
  users re-login once. Acceptable.
- **SAML POST-binding AuthnRequest while logged-in** (D2 gap): cookie not sent on
  the cross-site POST → no-session → `/login` bounce. Documented limitation,
  consistent with the deferred `ForceAuthn`+POST item.

---

## Testing

- **Unit tests** (`pkg/session/middleware_test.go` or equivalent): assert
  `FreshSessionCookie` / `ClearedSessionCookie` attributes in BOTH modes —
  secure deployment → name `__Host-<base>`, `Secure=true`, `Path=/`,
  `SameSite=Lax`, no `Domain`; dev (http origin) → plain name, `Secure=false`,
  `Path=/`, `SameSite=Lax`. Assert clear matches set (name+path). Assert
  `LoadSession` reads the correctly-named cookie in each mode.
- **Smoke** (`cmd/smoke`): remove the manual re-attach; confirm the full v0.2–v0.6
  suite stays green with the jar auto-sending the cookie to the protocol routes
  (this is the behavioral proof the audit wanted). Optionally assert the login
  `Set-Cookie` carries `Path=/` + `SameSite=Lax` (+ in a secure-config variant,
  the `__Host-` name + `Secure`).
- **Full gate:** `go build ./...`, `go vet ./...`, `go test ./...`, `cmd/smoke`
  `SMOKE_EXIT=0`.
- A real-browser HTTPS end-to-end test is out of scope (no browser harness); the
  attribute-level unit tests + the dev-mode behavioral smoke are the verification,
  and the production browser behavior follows from `Path=/` + `SameSite=Lax` per
  the web-platform spec.

---

## Out of scope
- The `/login` UI itself (frontend / dashboard — a separate future chunk). This
  fix only ensures an *existing* session cookie reaches the protocol endpoints;
  how the login page is rendered is unchanged.
- `SameSite=None` / cross-site POST-binding seamlessness (D2).
- The `CeremonyCookie` (D4).

After this lands, update `AUDIT.md` to close the v0.6 architectural finding.
