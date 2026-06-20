# Auth-flow error pages & graceful session-expiry redirect — Design

Date: 2026-06-20
Status: Approved (brainstorming) — pending spec review

## Problem

Two related defects make the SPA dump users onto broken or raw states during
authentication-adjacent flows:

1. **Session expiry / logout does not redirect to login.** When a session
   expires (or is revoked) server-side, navigating between SPA pages renders
   per-component load errors instead of sending the user to `/login`. The user
   is stuck on a half-broken page.

2. **OAuth / OIDC / SAML flow errors surface as raw JSON or plaintext.** Many
   browser-navigated protocol endpoints (federation login/callback, identity
   linking, invite-federation, OIDC authorize/logout, SAML SSO/IdP-init/SLO)
   respond to *error* paths with `application/json` (`writeAuthErr` /
   `writeOIDCError`) or bare `http.Error` plaintext. Because the browser
   navigated there via a full-page redirect, the user sees machine output with
   no frontend chrome whenever an upstream IdP is misconfigured, the upstream
   rejects the connection, a SAML request is malformed, etc.

## Root causes

### Problem 1
`installGuard` (`dashboard/src/router/index.ts`) calls `auth.ensureLoaded()`,
which is **memoized** by `_loaded` in `stores/auth.ts`. After `/me` succeeds at
first load, later navigations never re-check the session. With `auth.me` still
populated, the guard allows navigation; the page's data fetch then returns
`401 {code: "no_session"}`, which `useApi.run()` maps into the page's inline
`error`. **There is no global 401 handler** in `lib/api.ts` — only `SudoModal`
redirects to `/login`, and only for its narrow sudo case.

### Problem 2
There is no shared "redirect a browser to the SPA error page" helper. Each
browser-navigated handler emits its protocol/JSON error directly:

- Federation login/callback, identity link begin/callback, invite
  start-federation, federation-confirm → JSON via `writeAuthErr`
  (`pkg/server/handle_federation.go`, `handle_me_identities.go`,
  `handle_invite_federation.go`, `handle_federation_confirm.go`).
- OIDC authorize pre-`redirect_uri`-validation and logout → JSON via
  `writeOIDCError` (`pkg/protocol/oidc/authorize.go`, `logout.go`).
- SAML SSO / IdP-init / SLO parse and internal failures → plaintext via
  `http.Error` (`pkg/protocol/saml/sso.go`, `sso_init.go`, `slo.go`).

The precedent for the correct behavior already exists: app-access-denied
redirects an interactive browser to `/error?reason=app_access_denied&app=…`
(`pkg/protocol/oidc/access.go`, `sso.go`, `sso_init.go`). The SPA `ErrorView`
+ the `errors.*` i18n map already render most federation codes.

## Goals

- A session that expires mid-app sends the user to `/login` (preserving where
  they were) with a brief "session expired" notice — not inline component
  errors.
- Every **browser-navigated** OAuth/OIDC/SAML error lands on the SPA `/error`
  page with a friendly, code-specific (non-technical) message and a support
  **reference ID**, never raw JSON/plaintext.
- Genuine XHR/API endpoints keep JSON; spec-mandated OAuth redirects-to-RP are
  preserved.

## Non-goals / out of scope

- **Silent token refresh.** Prohibitorum uses cookie sessions with no refresh
  token; interactive re-login is the only recovery path. (Curity confirms
  navigate-to-login is the correct terminal behavior for cookie-session SPAs.)
- **Richer SAML `<StatusCode>` error responses to the SP** for recoverable,
  post-validation failures. Best practice (WorkOS/Scalekit/IBM) is, when the
  AuthnRequest parsed *and* the ACS URL is trusted, to return a SAML status
  response so the SP owns the UX. The existing passive/access-denied paths
  already do a subset of this; broadening it is a larger protocol change and is
  **deferred**. This cycle only stops the plaintext bleeding for the
  "cannot safely respond to the SP" failures.
- Changing the OAuth `authorize` **post-validation** behavior — errors after a
  trusted `redirect_uri` continue to redirect to the RP with `error=` params
  per RFC 6749 §4.1.2.1 / RFC 9700.
- Consent and federation-confirm endpoints — these are XHR-backed by real SPA
  pages (`ConsentView`, `WelcomeView`) that already handle errors in-page; they
  keep JSON.

## Standards basis

- **RFC 6749 §4.1.2.1 / RFC 9700 (OAuth 2.0 Security BCP):** if the
  authorization server cannot validate `client_id`/`redirect_uri`, it MUST NOT
  redirect to the (untrusted) URI — it MUST inform the resource owner directly.
  → pre-validation authorize errors render on our `/error` page; post-validation
  errors redirect to the RP.
- **OpenID Connect RP-Initiated Logout 1.0:** prescribes no JSON error format;
  the OP should inform/prompt the user. → logout errors render on `/error`.
- **SAML error-handling best practice:** never POST a response to an
  unvalidated/untrusted ACS; for "cannot safely respond" failures show the
  IdP's own error page with a friendly message + correlation/reference ID; mask
  NameID/sensitive identifiers in logs.

---

## Design

### Part A — Graceful session-expiry redirect (Problem 1)

**Mechanism: a context-aware global 401 seam in the API client.**

1. **`lib/api.ts` gains an `onUnauthorized` seam.** The module stays
   framework-agnostic (no router/pinia import → no cycles). It exposes
   `registerUnauthorizedHandler(fn)` and, inside `request()`/`upload()`, when a
   response is **401 with body `code === "no_session"`**, it invokes the
   registered handler with `{ method }` before throwing the `ApiError` as today.
   - Scoped strictly to `no_session`. Other 401 codes (`sudo_required`,
     `bad_credentials`, `partial_session_invalid`, …) are left to their existing
     owners (SudoModal, login form) and still throw normally.

2. **`main.ts` registers the handler** after the router and pinia exist. The
   handler is:
   - **Route-aware:** no-op when the current route is a public/threshold route
     (`login`, `error`, `welcome`, `consent`, `enroll`, `pair`, `logout`). This
     single rule preserves the bespoke `no_session` handling in `ConsentView` /
     `WelcomeView` and the expected unauthenticated `/me` at boot — no per-call
     opt-out flags needed.
   - **Idempotent:** a module flag fires the response once so concurrent 401s
     from parallel fetches don't stack redirects/notices; reset after the
     navigation/notice settles.
   - **Context-aware (read vs form), per current best practice:**
     - **GET request 401** (page load / navigation) → `auth.clear()` then
       `router.replace({ name: 'login', query: { return_to: <current fullPath>,
       reason: 'session_expired' } })`.
     - **Mutation 401** (POST/PUT/DELETE) → do **not** auto-navigate (would
       silently discard unsaved form input). Instead surface a non-destructive,
       app-level "Your session expired — sign in again" prompt that the user
       triggers to navigate to `/login?return_to=…&reason=session_expired`. This
       is a small global banner/dialog driven by a tiny reactive
       `sessionExpired` flag (e.g. in the auth store or a dedicated composable).

3. **`LoginView`** reads `?reason=session_expired` and renders a notice
   ("Your session expired. Please sign in again."). On successful login the
   existing `return_to` handling returns the user to where they were.

> Note: The existing router guard is unchanged — the interceptor catches expiry
> reactively on the next fetch (navigation or in-page action), which is the
> single seam that covers both. Guard re-validation is intentionally not added.

#### Part A — components / interfaces

| Unit | Responsibility | Interface |
|---|---|---|
| `lib/api.ts` | Detect `401 no_session`, invoke seam | `registerUnauthorizedHandler(fn: (ctx: {method: string}) => void)` |
| `main.ts` wiring | Decide redirect vs prompt; route-aware; idempotent | reads `router.currentRoute`, calls `auth.clear()` + `router.replace` or sets `sessionExpired` |
| `stores/auth.ts` (or composable) | Hold `sessionExpired` flag for the mutation case | `sessionExpired: Ref<boolean>`, setter/clear |
| Global prompt | Show the manual "sign in again" action for mutations | small banner/dialog in `App.vue` / `DashboardLayout.vue` |
| `LoginView` | Show the session-expired notice | reads `route.query.reason` |

### Part B — Browser-facing flow errors → SPA `/error` (Problem 2)

**Mechanism: a single backend helper that redirects a browser to the SPA error
page, carrying a stable error code and a per-incident reference ID.**

```
// pseudocode — exact package/signature finalized in the plan
func redirectToErrorPage(w, r, code string) {
    ref := newRef()                    // short correlation id (see below)
    logFailure(r.Context(), code, ref) // structured log; mask sensitive data
    u := "/error?error=" + url.QueryEscape(code) + "&ref=" + url.QueryEscape(ref)
    http.Redirect(w, r, u, http.StatusFound)
}
```

- **Helper placement.** The helper is used from both `pkg/server` and
  `pkg/protocol/{oidc,saml}`. Since `pkg/server` imports `pkg/protocol` (not the
  reverse), the shared helper must live in a leaf package both can import (or be
  a tiny per-package wrapper over one shared `ref`+log core). Finalized in the
  plan; must not introduce an import cycle.
- **Reference ID (`ref`).** There is no request-ID middleware today, so the
  helper generates a short random correlation ID (e.g. 8 hex chars from
  `crypto/rand`). It is written to the structured log for the failure and, where
  a federation/SAML failure already records an audit event, included in that
  event's detail. The `/error` page displays it as "Reference: `<ref>`" so a
  user can quote it to an admin who finds the full detail in the logs/audit
  trail. (Honors the "backend should have a concept like error ID" requirement
  and matches SAML/SPA error-page best practice.)
- **Stable error code (`error`).** Drives the human-readable, non-technical
  message via the SPA `errors.<code>` i18n map. Existing codes are reused;
  new codes are added where missing.
- **Sensitive-data masking.** Browser-facing failure logs must not include raw
  assertions, NameID, tokens, or full upstream payloads; mask/redact per SAML
  best practice. (Verify existing logging during implementation.)

#### Endpoints converted to `/error?error=<code>&ref=<id>`

| Flow | File | Error paths converted |
|---|---|---|
| Federation login | `pkg/server/handle_federation.go` | unknown IdP, invalid `return_to` |
| Federation callback | `pkg/server/handle_federation.go` | upstream OP error, state invalid, federator failures, session-issue failure |
| Identity link begin/callback | `pkg/server/handle_me_identities.go` | unknown IdP, upstream OP error, state invalid, link failures |
| Invite start-federation | `pkg/server/handle_invite_federation.go` | invite required, invalid `return_to`, redemption errors |
| OIDC authorize (pre-validation only) | `pkg/protocol/oidc/authorize.go` | invalid client, invalid/unmatched `redirect_uri`, and other errors raised **before** a trusted `redirect_uri` exists |
| OIDC logout | `pkg/protocol/oidc/logout.go` | invalid/unknown `id_token_hint`, unregistered `post_logout_redirect_uri`, unknown client |
| SAML SSO (SP-init) | `pkg/protocol/saml/sso.go` | parse failure, rate-limit, replay, internal errors (the "cannot safely respond to SP" set) |
| SAML IdP-init | `pkg/protocol/saml/sso_init.go` | missing/unknown/disabled SP, idp-init-disabled, RelayState too large, ACS-resolution/internal errors, rate-limit |
| SAML SLO | `pkg/protocol/saml/slo.go` | LogoutRequest parse/destination/expiry/signature failures, unknown/disabled SP, internal errors, raw-XML fallback |

#### Explicitly preserved (NOT changed)

- OAuth `authorize` **post-validation** errors → redirect to the RP's
  `redirect_uri` with `error=` (RFC-mandated).
- OAuth/OIDC token, introspection, revocation endpoints → JSON (RFC-mandated,
  not browser-navigated).
- SAML success delivery (auto-POST form), passive `<StatusCode>` responses,
  SLO success responses, and the existing interactive app-access-denied
  `/error?reason=…` redirects.
- XHR/API endpoints with real SPA pages: `/consent` (GET/POST),
  `/auth/federation/confirm` (GET/POST), all `/me/*` and admin reads → JSON.

#### New stable error codes (need en.ts + zh.ts copy)

- SAML: `saml_request_invalid`, `saml_sp_unknown`, `saml_sp_disabled`,
  `saml_idp_init_disabled`, `saml_replayed` (generic `server_error`,
  `rate_limited` reused).
- OIDC authorize: `invalid_client`, `invalid_redirect_uri`, `invalid_request`
  (if not already present).
- Federation codes (`upstream_error`, `federation_state_invalid`,
  `invalid_return_to`, `no_session`, …) already exist and are reused.

### Part C — `ErrorView` upgrade (shared)

`dashboard/src/pages/ErrorView.vue`:

1. **Per-code message:** ensure every code above resolves to a specific,
   non-technical message via `errors.<code>` — never the generic fallback.
2. **Reference line:** when `?ref=` is present, render small muted text
   "Reference: `<ref>`" beneath the message (the quotable "error ID").
3. **Auth-aware return button:** if the auth store has a session →
   "Back to dashboard" (`/security`); otherwise → "Return to sign in"
   (`/login`). Fixes the logged-in identity-link/callback-failure case where the
   user should not be pointed at the login page.

---

## Data flow

**Session expiry (read):**
`user navigates → guard allows (memoized me) → page GET /api/… → 401 no_session
→ api.ts seam → handler (authed route, GET) → auth.clear() + router.replace(
/login?return_to=…&reason=session_expired) → LoginView shows notice → login →
return_to.`

**Session expiry (mutation):**
`user submits form → POST /api/… → 401 no_session → api.ts seam → handler
(authed route, non-GET) → set sessionExpired flag → global prompt "sign in
again" → user clicks → /login?return_to=…&reason=session_expired.`

**Browser-facing flow error:**
`browser navigates to protocol endpoint → error → redirectToErrorPage(code) →
log+ref (masked) → 302 /error?error=<code>&ref=<id> → webui serves index.html →
SPA ErrorView → friendly errors.<code> message + "Reference: <ref>" +
auth-aware return button.`

---

## Testing & verification

**Backend (Go):**
- Per converted handler: assert a `302` to `/error?error=<code>` (and `ref`
  present) on each error path; assert preserved paths still emit JSON / SAML /
  RP-redirect unchanged.
- SAML: assert the "cannot safely respond" set redirects to `/error` while
  success/passive/SLO responses are unchanged; assert no redirect to an
  untrusted ACS.
- Smoke: add a federation-callback-error case and a SAML bad-request case that
  assert a `302 → /error` (not JSON/plaintext).

**Frontend (Vitest):**
- `lib/api.ts` seam: 401 `no_session` invokes the handler; non-`no_session`
  401 does not; success/other statuses unaffected.
- `main.ts` handler logic (or extracted composable): authed GET → redirect with
  `return_to`+`reason`; authed mutation → sets `sessionExpired`, no navigation;
  public route → no-op; idempotent.
- `ErrorView`: renders `errors.<code>` message, "Reference: <ref>" when `ref`
  present, and the auth-aware return button.
- i18n parity + compile tests pass for new codes (en.ts + zh.ts).

**Runtime (Playwright + chromium, per standing rule):**
- Force a session-expiry mid-app (clear the cookie / revoke), navigate →
  observe smooth redirect to `/login` + notice; submit a form → observe the
  non-destructive prompt.
- Trigger a federation callback failure and a SAML malformed-request →
  observe the `/error` page with message + reference (not raw JSON/plaintext).

## Risks

- **Redirect loop on `/login`.** Mitigated by the route-aware no-op on public
  routes and `no_session`-only scoping.
- **SAML regression — breaking SP-facing responses.** Mitigated by converting
  only the "cannot safely respond to SP" set and asserting success/passive/SLO
  paths are unchanged in tests; never redirect to an untrusted ACS.
- **Leaking detail via `error_description`.** Mitigated by mapping to stable
  codes (friendly messages) and putting detail only in masked server
  logs/audit, surfaced to the user solely as the opaque `ref`.
- **Double-fire / race between guard and interceptor.** Mitigated by the
  idempotent module flag.
