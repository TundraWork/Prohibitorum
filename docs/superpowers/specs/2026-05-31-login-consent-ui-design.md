# Login + Consent UI — design

**Goal:** Give Prohibitorum its first browser frontend: the interactive `/login`,
OIDC `/consent`, logout, and error surfaces the OIDC/SAML flows need. Today the
protocol layers bounce an unauthenticated browser to `…/login?return_to=…`, but
no such page exists, and the OIDC OP has no consent step (`client.RequireConsent`
returns a `consent_required` stub). This chunk builds the frontend webapp and the
net-new consent backend so interactive flows complete end-to-end in a real browser.

**Scope (this chunk):** `/login` (WebAuthn passkey + password→TOTP + federation),
OIDC `/consent` (remembered grants + trusted-client skip), logout/post-logout
landing, shared error/info page. Bilingual zh-CN + en. The admin dashboard,
account self-service (`/me`) UI, enrollment/registration UI, and recovery-code
sign-in are **out of scope** (future chunks); this is the foundation they build on.

**Approach:** A Vue 3 + Vite SPA, built to static assets and **embedded in the Go
binary** (`go:embed`) and served **same-origin** — essential for an IdP, because
the `Path=/` `__Host-` session cookie, the WebAuthn RPID, and the OIDC issuer /
SAML EntityID are all one origin. The SPA talks to the existing JSON auth APIs;
the protocol layers keep owning every redirect/security decision (the SPA fetches
context and POSTs decisions, never reconstructs flow state).

**Context:**
- Bounce points: `pkg/protocol/oidc/authorize.go` (`…/login?return_to=`, the
  `RequireConsent` stub at step 5, `prompt=login/none/consent`, `max_age`/`reauth`
  nonce), `pkg/protocol/saml/authnreq.go` + `sso_init.go` (`/login?return_to=`).
- Existing auth APIs (raw chi JSON, under `/api/prohibitorum/auth`):
  `login/begin`+`login/complete` (WebAuthn), `password/begin`, `totp/verify`,
  `logout`; `federation/{slug}/login`+`/callback`; OIDC `GET /oidc/logout`.
  `GET /auth/status` returns only `{bootstrapped}` → the SPA uses `/me` to detect
  a live session. Error envelope: `pkg/authn.AuthError{Status, Code, Message}`
  with a stable machine `Code` + a **zh-CN** `Message`.
- `oidc_client` already has `RequireConsent` (the trusted-skip flag is `false`),
  plus `logo_uri`/`policy_uri`/`tos_uri`/display name. No stored-grants table and
  no federation-providers list endpoint exist yet — both net-new.
- Reauth nonce pattern to mirror for the consent ticket: `pkg/authn/reauth.go`
  (`DemandReauth`/`ConsumeReauth` — single-use, account-bound KV nonce).

---

## Decisions

- **D1 — Stack.** Vue 3 + Vite + TypeScript; Vue Router (history mode) routes
  `/login`, `/consent`, `/logout`, `/error`; Pinia for light shared state
  (locale, session); **Nuxt UI v4 standalone** (Reka UI + Tailwind v4, semantic
  theming) for components; **vue-i18n** (zh-CN + en); `@simplewebauthn/browser`
  for the WebAuthn ceremony. Lives in `dashboard/`.

- **D2 — Same-origin embedded deploy.** `vite build` → `dashboard/dist/` →
  `//go:embed` into a new `pkg/webui`. A `mise` task chains the frontend build
  before `go build`; `dashboard/dist/` is gitignored (built in CI/build). The Go
  server serves the SPA via the chi router's **`NotFound` handler** (not a `/*`
  route — that would risk shadowing): it serves an embedded asset when the path
  matches one in `dist/`, else returns `index.html` (so SPA deep-links work).
  Because all `/api/*`, `/oauth/*`, `/saml/*`, `/oidc/*`, `/.well-known/*` routes
  are explicitly registered, they match first and never reach `NotFound` — so the
  fallback cannot shadow the API/protocol surface.

- **D3 — Dev proxy.** Vite dev server (`:5173`) proxies `/api`, `/oauth`,
  `/saml`, `/oidc`, `/.well-known` → Go `:8080`. Same host ⇒ the `Path=/` session
  cookie and WebAuthn RPID work across the port (cookies are host-, not
  port-scoped; RPID is the host).

- **D4 — `/login`.** Reads `return_to`, authenticates, then navigates
  (`window.location`) to `return_to` (re-hits authorize/SSO, now with a session).
  Validates `return_to` is **same-origin** before navigating (open-redirect
  defense). One screen, passkey-forward:
  - **Passkey** (prominent): `/auth/login/begin` → `@simplewebauthn/browser` →
    `/auth/login/complete`.
  - **Federation** buttons from a new `GET /api/prohibitorum/auth/federation`
    (enabled providers: slug + display name) → navigate to
    `/auth/federation/{slug}/login?return_to=…`.
  - **Password → TOTP** (progressive): identifier+password (`/auth/password/begin`)
    → TOTP (`/auth/totp/verify`).
  On load, `GET /me`; if a live session exists, skip to `return_to` — **except**
  when the bounce carries `&reauth=` (forced re-auth): the UI does not shortcut;
  the backend enforces the fresh credential.

- **D5 — Consent state (net-new).** Migration `006_oidc_consent.sql`:
  `oidc_consent (account_id, client_id, granted_scopes text[], created_at,
  updated_at)`, unique `(account_id, client_id)`. sqlc queries: `GetConsent`,
  `UpsertConsent` (stores the **union** of previously-granted + newly-approved
  scopes), `DeleteConsent` (added for future `/me` revocation; **no UI this
  chunk**).

- **D6 — Consent ticket (net-new, KV).** Single-use, account-bound, ~10 min TTL,
  mirroring `pkg/authn/reauth.go`. Captures `{account_id, client_id,
  requested_scopes, redirect_uri, state}`. It is what makes the consent decision
  CSRF-safe (the POST carries a server-minted secret) and lets **deny** emit a
  correct `access_denied` RP redirect.

- **D7 — `authorize.go` step (5) rewrite.** Replace the stub:
  ```
  if client.RequireConsent:
      granted = GetConsent(account, client)
      needConsent = (requested ⊄ granted) OR (prompt contains "consent")
      if !needConsent: proceed                      # remembered-grant skip
      else:
          if prompt == none: redirectError(consent_required → RP)
          else: mint consent ticket; 302 → /consent?ticket=<nonce>&return_to=<authorizeURL>
  else: proceed                                     # RequireConsent=false = trusted-client skip (today's false path)
  ```

- **D8 — Consent app API (session-gated, `/api/prohibitorum/consent`).**
  - `GET …/consent?ticket=` → validates the ticket belongs to the current session
    account; returns `{ client:{display_name, logo_uri, policy_uri, tos_uri},
    account:{display_name}, scopes:[names] }`. Scope **descriptions** are NOT
    returned — they live in the frontend i18n bundle (must be bilingual).
  - `POST …/consent {ticket, decision}` → consumes the ticket (single-use):
    - **approve** → `UpsertConsent(account, client, union(granted, requested))`;
      returns `{redirect: <return_to authorize URL>}`. SPA navigates → `authorize`
      re-runs → grant now covers scopes → issues the code.
    - **deny** → no grant stored; returns
      `{redirect: <redirect_uri>?error=access_denied&state=…}` (from the ticket).
      SPA navigates → RP gets `access_denied`.

- **D9 — `/consent` page.** Reads `ticket`+`return_to`; `GET …/consent` → renders
  RP name + (initial-avatar, see D13) + "「{app}」请求以下权限 / requests the
  following permissions", the localized scope list, "以 {account} 身份继续".
  Approve/Deny follow the returned `{redirect}`. Invalid/expired ticket → `/error`;
  missing session (shouldn't happen post-gate) → `/login?return_to=current`.

- **D10 — `/logout` + post-logout.** SPA landing/confirmation. App-initiated:
  `POST /auth/logout` → "已退出登录 / Signed out". For OIDC RP-initiated logout,
  `/oidc/logout` keeps its current server behavior; when a
  `post_logout_redirect_uri` is in play the landing offers/auto-follows a
  "返回 {app} / Return to {app}" continue. No change to `/oidc/logout`'s server
  logic in this chunk.

- **D11 — `/error`.** Shared SPA route `?code=&description=` rendering localized
  friendly text for `invalid_request`, `access_denied`, `server_error`, expired
  session, invalid/expired login-or-consent ticket, bad `return_to`. It is the
  dead-end for interactive flows with **no RP to redirect to**; we do NOT reroute
  every backend JSON error through it.

- **D12 — i18n.** vue-i18n, **zh-CN default + en**; locale = persisted choice →
  `navigator.language` → zh-CN; switcher in the shell; `<html lang>` synced. Two
  string domains: (a) UI strings + **scope descriptions** authored in both
  locales; (b) backend-`Code` → localized-string map, **falling back to the
  backend's zh `Message`** for unmapped codes (never blank).

- **D13 — Security.** `frame-ancestors 'none'` + `X-Frame-Options: DENY` on the
  SPA shell (consent-clickjacking defense). Strict CSP: `default-src 'self';
  connect-src 'self'; img-src 'self' data:`. **Remote RP `logo_uri` images are
  NOT loaded this chunk** (would loosen `img-src` / add an SSRF-ish surface) — a
  generated initial/letter avatar is shown instead; remote-logo support deferred.
  Consent CSRF = the D6 ticket (+ same-origin + `SameSite=Lax`). `return_to` =
  same-origin-only guard before navigation. WebAuthn RPID = issuer host. No
  secrets in the bundle; auth purely via the `HttpOnly` cookie + JSON APIs.

- **D14 — Federation-list endpoint (net-new).** `GET /api/prohibitorum/auth/
  federation` (public) → `[{slug, display_name}]` for enabled upstream IdPs, for
  the `/login` "sign in with" buttons.

---

## Components

**Backend (new/changed):**
- `pkg/webui/` — `//go:embed` the built `dist/` + the SPA-fallback http.Handler;
  mounted last in `pkg/server/server.go`. Sets the CSP / frame-deny headers
  (D13) on the shell.
- `db/migrations/006_oidc_consent.sql` + `db/queries/oidc_consent.sql` (sqlc:
  `GetConsent`, `UpsertConsent`, `DeleteConsent`).
- Consent check + ticket: extend `pkg/protocol/oidc/authorize.go` step (5) (D7);
  consent-ticket helpers in `pkg/authn` (new `consent.go`, or generalize the
  reauth nonce helper) (D6).
- `pkg/server/handle_consent.go` — `GET`/`POST /api/prohibitorum/consent` (D8).
- Federation list — handler (extend `pkg/server/handle_federation.go`) + route
  (D14).
- `pkg/server/server.go` — register the consent + federation-list routes and the
  SPA fallback (last).

**Frontend (`dashboard/`):**
- Vite + Vue 3 + TS scaffold; Nuxt UI v4 (Vite plugin + `app.use(ui)`); Tailwind
  v4; vue-i18n (zh+en); Pinia; Vue Router.
- Pages: `LoginView`, `ConsentView`, `LogoutView`, `ErrorView`.
- Components: `PasskeyButton`, `PasswordTotpForm`, `FederationButtons`,
  `ConsentScopeList`, `LocaleSwitcher`, app shell/layout.
- Lib: a small `api` client (fetch wrapper, credentials: 'include', error-code
  mapping), a `webauthn` helper (`@simplewebauthn/browser` wrapping
  begin/complete), a `returnTo` same-origin guard, the scope-description i18n map.
- Build: `mise` task `frontend:build` (→ `dashboard/dist/`) wired before the Go
  binary build.

---

## Data flow

**Interactive OIDC with consent (happy path):**
1. RP → browser → `GET /oauth/authorize?…` (no session). authorize step 4 → 302
   `…/login?return_to=<authorizeURL>`.
2. SPA `/login` authenticates (passkey / password+TOTP / federation) → session
   cookie set → navigate to `return_to`.
3. `GET /oauth/authorize` again: session present; `client.RequireConsent` and no
   covering grant → mint ticket → 302 `…/consent?ticket=<n>&return_to=<authzURL>`.
4. SPA `/consent`: `GET …/consent?ticket` → render. User **approves** → `POST
   …/consent {ticket,approve}` → `UpsertConsent` → `{redirect:return_to}`.
5. SPA navigates to `return_to` → `GET /oauth/authorize`: grant now covers scopes
   → issues code → 302 to RP `redirect_uri?code=…&state=…`.

**Deny:** step 4 POST `{ticket,deny}` → `{redirect:<redirect_uri>?error=
access_denied&state=…}` → SPA navigates → RP. **Remembered-grant skip:** a later
authorize finds a covering grant at step 3 → straight to code. **`prompt=consent`:**
forces the consent bounce even with a grant; approve refreshes the union.
**`prompt=none` needing consent:** `consent_required` to RP (no UI).

**SAML:** unchanged except that `/login` now exists; after login the SPA returns
to the SSO `return_to` and the IdP issues the assertion (SAML has no consent step).

---

## Error handling / edge cases
- **Invalid/expired/forged consent ticket** → `GET/POST …/consent` 4xx → SPA
  `/error` (ticket invalid). Single-use: a replayed approve fails.
- **Ticket account mismatch** (session account ≠ ticket account) → reject.
- **`return_to` not same-origin** → SPA refuses to navigate; shows `/error`
  (backend federation `return_to` allowlist already independently guards its leg).
- **No session at `/consent`** (shouldn't happen post-gate) → `/login?return_to=`.
- **Unmapped backend error `Code`** → display backend zh `Message` (D12 fallback).
- **WebAuthn unsupported / user-cancelled** → inline localized message; other
  methods remain available.
- **Federation list empty** → simply render no provider buttons.

## Testing
- **Frontend (Vitest + Vue Test Utils):** login method components; consent
  rendering from API context; i18n code-fallback; the `return_to` same-origin
  guard. APIs mocked.
- **Backend (Go unit):** rewritten `authorize` step 5 (skip-when-covered,
  `prompt=consent` forces, `prompt=none`→`consent_required`, deny→`access_denied`
  redirect); `oidc_consent` queries; consent ticket (single-use, account-bound);
  federation-list endpoint.
- **E2e (`cmd/smoke`):** drive the consent JSON flow end-to-end — RequireConsent
  client → authorize bounces (ticket) → approve API → grant stored → re-authorize
  issues code; remembered-grant skip on second authorize; `prompt=consent`
  re-prompt; deny → `access_denied`. Full gate: `go build/vet/test ./...`, smoke
  `SMOKE_EXIT=0`.
- **Honest limitation:** no headless-browser test of the Vue UI itself this chunk
  (consistent with the project's no-browser-harness stance); UI verification is
  Vitest + manual. A Playwright suite is a deferred follow-up.

## Out of scope
- Admin dashboard, `/me` account self-service UI, enrollment/registration UI,
  recovery-code sign-in (future chunks).
- Remote RP `logo_uri` image loading (D13 — initial-avatar instead).
- Browser-based UI e2e (Playwright) — deferred.
- Any change to `/oidc/logout`'s server logic, the OAuth/SAML token/assertion
  issuance, or routes/issuer.
