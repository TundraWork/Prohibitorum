# Auth-flow error pages & graceful session-expiry redirect — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make session expiry redirect the user to `/login` gracefully, and make every browser-navigated OAuth/OIDC/SAML/federation error land on the SPA `/error` page (with a friendly per-code message + correlation reference) instead of raw JSON/plaintext.

**Architecture:** Backend adds one leaf helper (`pkg/weberr`) that 302-redirects a browser to `/error?error=<code>&ref=<id>`; the federation/identity-link/invite handlers, OIDC authorize(pre-validation)/logout, and SAML SSO/IdP-init/SLO "cannot-respond-to-SP" error paths call it instead of writing JSON/plaintext. Frontend adds a context-aware global `401 no_session` seam in the API client that redirects reads to `/login?return_to=…&reason=session_expired` and surfaces a non-destructive banner for mutations, plus an `ErrorView` upgrade (reference line + auth-aware return button).

**Tech Stack:** Go (chi, pgx, slog), Vue 3 + Vite + Tailwind v4 + shadcn-vue/Reka, vitest, vue-i18n (en + zh), Playwright + chromium for runtime verification.

**Spec:** `docs/superpowers/specs/2026-06-20-auth-flow-error-pages-and-session-redirect-design.md`

**Standing conventions (from project memory):**
- Backend build uses `-tags nodynamic` (no cgo): `go build -tags nodynamic ./...`.
- Gate before done: `go build -tags nodynamic ./... && go vet ./... && go test ./...`; `cd dashboard && npm run test` (vitest), `npx vue-tsc -b` (the real FE typecheck), `node scripts/check-contrast.mjs`.
- `en.ts` apostrophe hazard: any English string containing `'` MUST use double-quote delimiters; grep-verify no curly `'` (U+2019) leaked after editing. Avoid a literal `@` in i18n messages (escape as `{'@'}`).
- `zh.ts` must stay at parity with `en.ts` (the `locales.parity.test.ts` / `locales.params.test.ts` / `locales.compile.test.ts` enforce this) — add every new key to BOTH.
- Reka primitive test idioms: click/`$emit`/mousedown, not `setValue`.
- The embedded SPA `dist/` is committed; rebuild + commit it at the done-gate (`cd dashboard && npm run build`).
- NEVER add a `Co-Authored-By` trailer to commits.

---

### Task 1: `pkg/weberr` — browser error-page redirect helper

**Goal:** A leaf package both `pkg/server` and `pkg/protocol/*` can import (no cycle) that mints a correlation ref and 302-redirects a browser to the SPA error page.

**Files:**
- Create: `pkg/weberr/weberr.go`
- Test: `pkg/weberr/weberr_test.go`

**Acceptance Criteria:**
- [ ] `NewRef()` returns an 8-char lowercase-hex string; successive calls differ.
- [ ] `RedirectToError(w, r, code, ref)` writes `302` with `Location: /error?error=<code>&ref=<ref>` (both query-escaped) and `Cache-Control: no-store`.
- [ ] Package imports stdlib only (`crypto/rand`, `encoding/hex`, `net/http`, `net/url`).

**Verify:** `go test ./pkg/weberr/...` → ok

**Steps:**

- [ ] **Step 1: Write the failing test**

```go
// pkg/weberr/weberr_test.go
package weberr

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

func TestNewRef_Format(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}$`)
	a, b := NewRef(), NewRef()
	if !re.MatchString(a) || !re.MatchString(b) {
		t.Fatalf("ref not 8 hex chars: %q %q", a, b)
	}
	if a == b {
		t.Fatalf("two refs collided: %q", a)
	}
}

func TestRedirectToError(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?x=1", nil)
	RedirectToError(rec, req, "invalid_client", "deadbeef")
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if got, want := rec.Header().Get("Location"), "/error?error=invalid_client&ref=deadbeef"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/weberr/...`
Expected: FAIL (package/functions don't exist).

- [ ] **Step 3: Write the implementation**

```go
// pkg/weberr/weberr.go

// Package weberr redirects browser-navigated requests to the SPA error page.
//
// Use ONLY for full-page (browser-navigated) dead-ends in OAuth/OIDC/SAML/
// federation flows — never for XHR/API endpoints (those keep JSON). The SPA
// ErrorView maps `error` to a friendly, non-technical message and shows `ref`
// as a support reference; full detail belongs in server logs / the audit trail.
package weberr

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
)

// NewRef returns a short random correlation reference (8 hex chars). Shown to
// the user and logged/audited server-side so support can correlate the two.
// Best-effort: a rand failure yields a fixed sentinel rather than an error,
// because the redirect must still happen.
func NewRef() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(b)
}

// RedirectToError 302-redirects the browser to /error?error=<code>&ref=<ref>.
// code is a stable, non-technical code the SPA maps to copy; ref comes from
// NewRef. Caller is responsible for logging/auditing the failure with ref.
func RedirectToError(w http.ResponseWriter, r *http.Request, code, ref string) {
	w.Header().Set("Cache-Control", "no-store")
	u := "/error?error=" + url.QueryEscape(code) + "&ref=" + url.QueryEscape(ref)
	http.Redirect(w, r, u, http.StatusFound)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/weberr/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/weberr/
git commit -m "feat(weberr): browser error-page redirect helper with correlation ref"
```

---

### Task 2: Federation, invite & identity-link handlers → `/error`

**Goal:** Convert the browser-navigated federation login/callback, invite start-federation, and identity-link begin/callback error paths from `writeAuthErr` (JSON) to a `/error` redirect carrying the AuthError code + a ref; preserve the existing audit rows (now stamped with the ref).

**Files:**
- Modify: `pkg/server/handle_auth.go` (add `redirectAuthErrToError` helper near `writeAuthErr`)
- Modify: `pkg/server/handle_federation.go:57-83,87-178` (login + callback error paths)
- Modify: `pkg/server/handle_invite_federation.go:31-63`
- Modify: `pkg/server/handle_me_identities.go:243-329` (link begin + callback error paths)
- Test: `pkg/server/handle_federation_error_redirect_test.go` (new)

**Acceptance Criteria:**
- [ ] Federation login with a bad `return_to` → `302 /error?error=invalid_return_to&ref=…` (not JSON).
- [ ] Federation callback with `?error=access_denied` → `302 /error?error=upstream_error&ref=…`; the existing audit row is written and includes `"ref"`.
- [ ] Federation callback with missing state/code → `302 /error?error=federation_state_invalid&ref=…` (no audit, as before).
- [ ] Identity-link callback upstream error → `302 /error?error=upstream_error&ref=…` (authenticated; audit row includes `"ref"`).
- [ ] Invite start-federation with empty token → `302 /error?error=invite_required&ref=…`.
- [ ] `handleListFederationProvidersHTTP` (an XHR endpoint) is UNCHANGED (still JSON).

**Verify:** `go test ./pkg/server/ -run Federation` → ok (note: this suite can flake under shared-DB parallel runs — re-run in isolation if a sudo/credentials assertion flakes).

**Steps:**

- [ ] **Step 1: Add the server helper** in `pkg/server/handle_auth.go` (immediately after `writeAuthErr`):

```go
// redirectAuthErrToError sends a browser-navigated flow error to the SPA
// /error page instead of writing JSON. Use ONLY on full-page (redirect-target)
// handlers — federation login/callback, identity link, invite start. The
// AuthError code drives the SPA message; a fresh ref is returned so the caller
// can stamp it onto an existing audit row. Falls back to "server_error".
func redirectAuthErrToError(w http.ResponseWriter, r *http.Request, err error) string {
	code := "server_error"
	if ae := authn.AsAuthError(err); ae != nil {
		code = ae.Code
	}
	ref := weberr.NewRef()
	weberr.RedirectToError(w, r, code, ref)
	return ref
}
```

Add the import `"prohibitorum/pkg/weberr"` to `handle_auth.go`.

- [ ] **Step 2: Convert `handle_federation.go`.** Replace each browser-facing `writeAuthErr(w, …)` with `redirectAuthErrToError(w, r, …)`:
  - `:62` `writeAuthErr(w, err)` → `redirectAuthErrToError(w, r, err)`
  - `:71` `writeAuthErr(w, authn.ErrFederationStateInvalid())` → `redirectAuthErrToError(w, r, authn.ErrFederationStateInvalid())`
  - `:74` `writeAuthErr(w, err)` → `redirectAuthErrToError(w, r, err)`
  - `:117` `writeAuthErr(w, authn.ErrFederationStateInvalid())` → `redirectAuthErrToError(w, r, authn.ErrFederationStateInvalid())`
  - `:135` `writeAuthErr(w, err)` → `redirectAuthErrToError(w, r, err)`
  - `:147` `writeAuthErr(w, gerr)` → `redirectAuthErrToError(w, r, gerr)`
  - `:173` `writeAuthErr(w, err)` → `redirectAuthErrToError(w, r, err)`
  - For the **upstream-error audit block (`:95-110`)**: stamp the ref onto the audit detail. Restructure so the ref is generated first:

```go
	if upstreamErr != "" {
		ref := weberr.NewRef()
		_ = s.Audit.Record(r.Context(), audit.Record{
			Factor: audit.FactorFederationOIDC,
			Event:  audit.EventFail,
			Detail: map[string]any{
				"reason":               "upstream_error",
				"upstream_code":        upstreamErr,
				"upstream_description": upstreamDesc,
				"ref":                  ref,
			},
		})
		weberr.RedirectToError(w, r, authn.ErrUpstreamError(upstreamErr, upstreamDesc).Code, ref)
		return
	}
```

  - Leave `handleListFederationProvidersHTTP` (`:183-194`) UNCHANGED — it's an XHR list endpoint.
  - Add `"prohibitorum/pkg/weberr"` to imports.

- [ ] **Step 3: Convert `handle_invite_federation.go`** (`:37,43,49`): each `writeAuthErr(w, …)` → `redirectAuthErrToError(w, r, …)`. (No imports beyond what `redirectAuthErrToError` needs — it lives in the same package.)

- [ ] **Step 4: Convert `handle_me_identities.go` link handlers** (`:253,263,266,310,321`): each `writeAuthErr(w, …)` → `redirectAuthErrToError(w, r, …)`. For the **upstream-error audit block (`:287-303`)**, mirror Step 2 — generate `ref := weberr.NewRef()` first, add `"ref": ref` to the audit `Detail`, then `weberr.RedirectToError(w, r, authn.ErrUpstreamError(upstreamErr, upstreamDesc).Code, ref)`. Add `"prohibitorum/pkg/weberr"` to imports. **Do NOT touch** `requireFreshSudo` (`:246`) — a `sudo_required` here is an XHR/redirect-gate handled elsewhere; only convert the listed `writeAuthErr` calls.

- [ ] **Step 5: Write the test** `pkg/server/handle_federation_error_redirect_test.go`. Follow the existing federation handler test setup in `pkg/server` (reuse the test Server constructor / fakes already used by `handle_federation` tests — grep `handleFederationCallbackHTTP` in `*_test.go` for the existing harness). Core assertions:

```go
func TestFederationLogin_BadReturnTo_RedirectsToErrorPage(t *testing.T) {
	// ... build test Server `s` as the existing federation tests do ...
	req := httptest.NewRequest(http.MethodGet, "/api/prohibitorum/auth/federation/foo/login?return_to=https://evil.example/x", nil)
	rec := httptest.NewRecorder()
	// route through chi so chi.URLParam(slug) resolves, or call the handler with a chi RouteContext.
	s.handleFederationLoginHTTP(rec, withChiURLParam(req, "slug", "foo"))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/error?error=invalid_return_to&ref=") {
		t.Fatalf("Location = %q", loc)
	}
}

func TestFederationCallback_UpstreamError_RedirectsToErrorPage(t *testing.T) {
	// ... build test Server `s` with an audit spy ...
	req := httptest.NewRequest(http.MethodGet, "/api/prohibitorum/auth/federation/foo/callback?error=access_denied&error_description=nope", nil)
	rec := httptest.NewRecorder()
	s.handleFederationCallbackHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/error?error=upstream_error&ref=") {
		t.Fatalf("Location = %q", loc)
	}
	// assert the audit spy captured a Detail with a non-empty "ref"
}
```

(If the existing tests already provide a `withChiURLParam`-style helper, reuse it; otherwise add one using `chi.NewRouteContext()` + `context.WithValue(r.Context(), chi.RouteCtxKey, rctx)`.)

- [ ] **Step 6: Run tests**

Run: `go test ./pkg/server/ -run Federation`
Expected: PASS (re-run in isolation if the shared suite flakes).

- [ ] **Step 7: Commit**

```bash
git add pkg/server/
git commit -m "feat(server): federation/invite/link flow errors redirect to SPA /error"
```

---

### Task 3: OIDC authorize (pre-validation) + logout → `/error`

**Goal:** Convert OIDC `authorize` errors raised **before** a trusted `redirect_uri` exists, and ALL `logout` (end_session) errors, from `writeOIDCError` (JSON) to a `/error` redirect. Preserve every post-validation `redirectError`-to-RP path (RFC 6749 §4.1.2.1).

**Files:**
- Modify: `pkg/protocol/oidc/errors.go` (add `redirectToErrorPage` helper method)
- Modify: `pkg/protocol/oidc/authorize.go:53-68` (pre-validation only)
- Modify: `pkg/protocol/oidc/logout.go:47,53,57,83,88,93,98`
- Test: `pkg/protocol/oidc/error_redirect_test.go` (new)

**Acceptance Criteria:**
- [ ] `authorize` with an unknown `client_id` → `302 /error?error=invalid_request&ref=…` (was JSON `invalid client`).
- [ ] `authorize` with a present-but-trusted client + bad `redirect_uri` → `302 /error?error=invalid_request&ref=…`.
- [ ] `authorize` with a transient client-load failure → `302 /error?error=server_error&ref=…`.
- [ ] `authorize` post-validation errors (bad response_type, scope, PKCE) STILL `302` to the RP's `redirect_uri` with `error=` params — UNCHANGED.
- [ ] `logout` with an invalid `id_token_hint` → `302 /error?error=invalid_request&ref=…`.
- [ ] `logout` with an unregistered `post_logout_redirect_uri` → `302 /error?error=invalid_request&ref=…` (never redirects to the unregistered URI).
- [ ] `logout` success paths (registered URI / issuer root) UNCHANGED.

**Verify:** `go test ./pkg/protocol/oidc/...` → ok

**Steps:**

- [ ] **Step 1: Add the helper** in `pkg/protocol/oidc/errors.go`:

```go
// redirectToErrorPage sends a browser-navigated authorize/logout error to the
// SPA /error page (relative redirect, same origin). Use ONLY on the DIRECT-
// error side of the open-redirect guard (before a trusted redirect_uri exists)
// and for end_session errors — never instead of redirectError, which is the
// RFC-mandated RP error channel once redirect_uri is trusted.
func (p *Provider) redirectToErrorPage(w http.ResponseWriter, r *http.Request, code string) {
	ref := weberr.NewRef()
	slog.Warn("oidc browser-facing flow error", "code", code, "ref", ref, "path", r.URL.Path)
	weberr.RedirectToError(w, r, code, ref)
}
```

Add imports `"log/slog"` and `"prohibitorum/pkg/weberr"` to `errors.go`. (Do NOT remove `writeOIDCError`/`writeInvalidClient`/`redirectError` — the token endpoint and RP error channel still use them.)

- [ ] **Step 2: Convert `authorize.go` pre-validation** (the three sites inside the open-redirect guard, `:53-68`):

```go
	client, err := loadClient(r.Context(), p.queries, clientID)
	if err != nil {
		if errors.Is(err, errInvalidClient) {
			p.redirectToErrorPage(w, r, errCodeInvalidRequest)
		} else {
			p.redirectToErrorPage(w, r, errCodeServerError)
		}
		return
	}

	if redirectURI == "" || !slices.Contains(client.RedirectUris, redirectURI) {
		p.redirectToErrorPage(w, r, errCodeInvalidRequest)
		return
	}
```

**Do not change** anything from `// (3) redirect_uri is now trusted` onward — every `redirectError(...)` stays.

- [ ] **Step 3: Convert `logout.go`** — replace each `writeOIDCError(w, http.StatusBadRequest, errCodeInvalidRequest, "…")` at `:47,53,57,83,88,93,98` with `p.redirectToErrorPage(w, r, errCodeInvalidRequest)` followed by `return`. Leave the two success `http.Redirect` paths (`:106`, `:111`) and the session-revocation logic UNCHANGED.

- [ ] **Step 4: Write the test** `pkg/protocol/oidc/error_redirect_test.go` (reuse the existing authorize/logout test harness — grep `HandleAuthorize` / `HandleLogout` in `*_test.go` for the Provider constructor + fake queries):

```go
func TestAuthorize_UnknownClient_RedirectsToErrorPage(t *testing.T) {
	p := newTestProvider(t) // existing harness; queries return errInvalidClient for unknown
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?client_id=nope&redirect_uri=https://rp/cb&response_type=code&scope=openid", nil)
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/error?error=invalid_request&ref=") {
		t.Fatalf("Location=%q", loc)
	}
}

func TestAuthorize_PostValidationError_StillRedirectsToRP(t *testing.T) {
	// valid client + registered redirect_uri but response_type != code
	// → Location must start with the RP redirect_uri, NOT /error (regression guard).
}

func TestLogout_InvalidHint_RedirectsToErrorPage(t *testing.T) {
	// id_token_hint = "garbage" → 302 /error?error=invalid_request&ref=…
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./pkg/protocol/oidc/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/protocol/oidc/
git commit -m "feat(oidc): pre-validation authorize + logout errors redirect to SPA /error"
```

---

### Task 4: SAML SSO / IdP-init / SLO "cannot-respond-to-SP" errors → `/error`

**Goal:** Replace plaintext `http.Error` on the SAML human-facing dead-ends (parse failures, unknown/disabled SP, idp-init-disabled, replay, rate-limit, internal errors, raw-XML fallback) with a `/error` redirect carrying a mapped code + ref. PRESERVE every SP-facing SAML-binding response (success auto-POST, passive/denied `<StatusCode>` responses, SLO responses, the existing app-access-denied `/error` redirects). Never redirect to an untrusted ACS.

**Files:**
- Modify: `pkg/protocol/saml/saml.go` (add `errorPage` helper method on `*IdP`)
- Modify: `pkg/protocol/saml/sso.go` (parse helper `ssoParseError` + internal/replay/rate sites)
- Modify: `pkg/protocol/saml/sso_init.go` (SP-validation + internal sites)
- Modify: `pkg/protocol/saml/slo.go` (parse helper `sloParseError` + internal sites + raw-XML fallback)
- Test: `pkg/protocol/saml/error_redirect_test.go` (new)

**Acceptance Criteria:**
- [ ] SP-initiated SSO with a malformed AuthnRequest → `302 /error?error=saml_request_invalid&ref=…` (was plaintext 400).
- [ ] IdP-initiated with an unknown/disabled `sp` → `302 /error?error=saml_sp_unknown&ref=…`.
- [ ] IdP-initiated where the SP has idp-init disabled → `302 /error?error=saml_idp_init_disabled&ref=…`.
- [ ] Any "internal error" path → `302 /error?error=server_error&ref=…`.
- [ ] Rate-limit path → `302 /error?error=rate_limited&ref=…`.
- [ ] A SUCCESS SSO still renders the auto-POST HTML form (regression guard — existing tests still pass).
- [ ] The passive / app-access-denied / SLO-response paths are unchanged.

**Verify:** `go test ./pkg/protocol/saml/...` → ok

**Steps:**

- [ ] **Step 1: Add the `errorPage` helper** in `pkg/protocol/saml/saml.go` (method on `*IdP`):

```go
// errorPage sends a browser-navigated SAML dead-end to the SPA /error page.
// Use ONLY when the IdP cannot safely produce a SAML response for the SP
// (malformed request, unknown/untrusted/disabled SP, replay, internal error).
// SP-binding responses (auto-POST success, passive/denied <StatusCode>, SLO
// responses) and the app-access-denied /error redirect must NOT route here.
// Logs code/ref/path only — never query values, NameID, tokens, or assertions.
func (i *IdP) errorPage(w http.ResponseWriter, r *http.Request, code string) {
	ref := weberr.NewRef()
	slog.Warn("saml browser-facing flow error", "code", code, "ref", ref, "path", r.URL.Path)
	weberr.RedirectToError(w, r, code, ref)
}
```

Add imports `"log/slog"` and `"prohibitorum/pkg/weberr"` to `saml.go`.

- [ ] **Step 2: Convert `sso.go`.** Change the parse helper signature and rewrite its body, then update its one call site and the inline error sites:

```go
// ssoParseError now takes r and routes to the SPA error page.
func (i *IdP) ssoParseError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrMalformedRequest) || /* existing parse/sig sentinels */ {
		i.errorPage(w, r, "saml_request_invalid")
		return
	}
	i.errorPage(w, r, "server_error")
}
```

  Apply this site→code mapping in `sso.go` (every listed `http.Error(...)` becomes the mapped `i.errorPage(w, r, …)` + `return`; pass `r`, already in scope):

  | Lines | Current | New code |
  |---|---|---|
  | `:105` call site | `i.ssoParseError(w, err)` | `i.ssoParseError(w, r, err)` |
  | `:124,179,243,297` | `"AuthnRequest replayed"` 400 | `saml_replayed` |
  | `:208` | `"rate limit exceeded"` 429 | `rate_limited` |
  | `:126,132,157,181,187,224,245,251,262,270,299,318,331,338,345,354,371` | `"internal error"` 500 | `server_error` |

  **PRESERVE (do not touch):** every `i.writeAutoPost(...)` (`:135,190,254,321,390`), every `i.buildStatusResponse(...)` block, the not-authenticated `/login` redirect, and the app-access-denied `/error` redirect.

- [ ] **Step 3: Convert `sso_init.go`** (all sites have `r` in scope):

  | Lines | Current | New code |
  |---|---|---|
  | `:59` | `"RelayState too large"` 400 | `saml_request_invalid` |
  | `:68` | `"missing sp parameter"` 400 | `saml_request_invalid` |
  | `:74,82` | `"unknown SP"` 400 (incl. disabled→collapsed) | `saml_sp_unknown` |
  | `:89` | `"IdP-initiated SSO is not enabled…"` 403 | `saml_idp_init_disabled` |
  | `:77,99,117,162,170,177,184,196,211` | `"internal error"` 500 | `server_error` |
  | `:153` | `"rate limit exceeded"` 429 | `rate_limited` |

  Each `http.Error(w, "…", status)` → `i.errorPage(w, r, "<code>")`. **PRESERVE** the not-authenticated `/login` redirect (`:52`), the app-access-denied `/error` redirect (`:134`), and `i.writeAutoPost(...)` (`:232`).

- [ ] **Step 4: Convert `slo.go`.** Change the parse helper signature + body and update its call sites + inline sites:

```go
// sloParseError now takes r and routes to the SPA error page.
func (i *IdP) sloParseError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrUnknownSP):
		i.errorPage(w, r, "saml_sp_unknown")
	case errors.Is(err, ErrSLOBadDestination), errors.Is(err, ErrSLOExpired), errors.Is(err, ErrMalformedRequest):
		i.errorPage(w, r, "saml_request_invalid")
	default:
		// signature/validation failures collapse to the generic request-invalid code
		i.errorPage(w, r, "saml_request_invalid")
	}
}
```

  (If the existing `sloParseError` distinguishes an internal-error branch, map that branch to `i.errorPage(w, r, "server_error")`.)

  Update call sites to pass `r`: `:69,77,96,104,117,122,129,134,148`. Inline sites:

  | Lines | Current | New code |
  |---|---|---|
  | `:82` | `"method not allowed"` 405 | `saml_request_invalid` |
  | `:87,138` | `"invalid SAML LogoutRequest"` 400 | `saml_request_invalid` |
  | `:99,160,252,513,518,522,531,536` | `"internal error"` 500 | `server_error` |
  | `:276` | `w.Write(respXML)` raw-XML fallback (no SP metadata) | `server_error` (replace the raw write with `i.errorPage(w, r, "server_error")`) |

  **PRESERVE** the SLO success delivery (`writeRedirectLogoutResponse` overall behaviour and `i.writeAutoPost(...)` at `:284`); only its *internal-error* `http.Error` branches map to `server_error`.

- [ ] **Step 5: Write the test** `pkg/protocol/saml/error_redirect_test.go` (reuse the SAML IdP test harness — grep `HandleSSO`/`HandleIdPInitiated` in `*_test.go` for the `NewIdP` setup + fakes):

```go
func TestSSO_MalformedRequest_RedirectsToErrorPage(t *testing.T) {
	i := newTestIdP(t)
	req := httptest.NewRequest(http.MethodGet, "/saml/sso?SAMLRequest=not-base64", nil)
	rec := httptest.NewRecorder()
	i.HandleSSO(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/error?error=saml_request_invalid&ref=") {
		t.Fatalf("Location=%q", loc)
	}
}

func TestIdPInit_UnknownSP_RedirectsToErrorPage(t *testing.T) {
	// /saml/sso/init?sp=does-not-exist (authenticated session) → 302 /error?error=saml_sp_unknown&ref=…
}

func TestIdPInit_NotEnabled_RedirectsToErrorPage(t *testing.T) {
	// known SP with idp-init disabled → 302 /error?error=saml_idp_init_disabled&ref=…
}
```

Confirm the existing SUCCESS-path SAML tests (auto-POST form) still pass — they are the regression guard for "we didn't break SP delivery."

- [ ] **Step 6: Run tests**

Run: `go test ./pkg/protocol/saml/...`
Expected: PASS (new redirects + existing success/passive/SLO tests).

- [ ] **Step 7: Commit**

```bash
git add pkg/protocol/saml/
git commit -m "feat(saml): cannot-respond-to-SP errors redirect to SPA /error; SP-binding responses preserved"
```

---

### Task 5: i18n — new error codes + session-expiry / error-page copy (en + zh)

**Goal:** Add friendly, non-technical copy for every new code and the new UI strings, to BOTH `en.ts` and `zh.ts`, keeping parity tests green.

**Files:**
- Modify: `dashboard/src/locales/en.ts`
- Modify: `dashboard/src/locales/zh.ts`
- Verify: `dashboard/src/locales/locales.parity.test.ts`, `locales.params.test.ts`, `locales.compile.test.ts`, `en.compile.test.ts` (existing — must pass)

**Acceptance Criteria:**
- [ ] New `errors.*` keys exist in both files: `invalid_request`, `invalid_client`, `invalid_redirect_uri`, `saml_request_invalid`, `saml_sp_unknown`, `saml_sp_disabled`, `saml_idp_init_disabled`, `saml_replayed`.
- [ ] New non-error keys exist in both files: `login.sessionExpired`, `error.reference`, `error.backToDashboard`, `sessionExpiry.message`, `sessionExpiry.signInAgain`.
- [ ] Parity/params/compile tests pass; no curly `'` (U+2019) leaked into `en.ts`.

**Verify:** `cd dashboard && npm run test -- locales` → all locale tests pass

**Steps:**

- [ ] **Step 1: Add to `en.ts`.** In the `errors: { … }` block (after the federation group, ~`:696`), add (note: strings with apostrophes use DOUBLE quotes):

```ts
    // OIDC authorize / logout (browser-facing protocol errors)
    invalid_request: 'That sign-in request was invalid or incomplete. Please start again from the application.',
    invalid_client: 'That application is not recognized. Please contact the application owner.',
    invalid_redirect_uri: 'That application is misconfigured (unrecognized return address). Please contact the application owner.',
    // SAML (browser-facing protocol errors)
    saml_request_invalid: 'That single sign-on request was invalid. Please start again from the application.',
    saml_sp_unknown: 'That application is not configured for single sign-on here. Please contact your administrator.',
    saml_sp_disabled: 'Single sign-on for that application is currently disabled. Please contact your administrator.',
    saml_idp_init_disabled: 'That application does not support starting sign-in from here. Please open it from the application instead.',
    saml_replayed: 'That sign-in request was already used. Please start again from the application.',
```

  In the `error: { … }` block, add:

```ts
    reference: 'Reference: {ref}',
    backToDashboard: 'Back to dashboard',
```

  In the `login: { … }` block, add:

```ts
    sessionExpired: 'Your session expired. Please sign in again.',
```

  Add a new top-level block (next to `login`/`error`):

```ts
  sessionExpiry: {
    message: 'Your session expired.',
    signInAgain: 'Sign in again',
  },
```

- [ ] **Step 2: Add the SAME keys to `zh.ts`** with Chinese copy matching the existing 你-register style:

```ts
    // errors.* additions
    invalid_request: '登录请求无效或不完整，请从应用重新发起。',
    invalid_client: '无法识别该应用，请联系应用所有者。',
    invalid_redirect_uri: '该应用配置有误（返回地址未注册），请联系应用所有者。',
    saml_request_invalid: '单点登录请求无效，请从应用重新发起。',
    saml_sp_unknown: '该应用尚未在此配置单点登录，请联系管理员。',
    saml_sp_disabled: '该应用的单点登录当前已停用，请联系管理员。',
    saml_idp_init_disabled: '该应用不支持从这里发起登录，请改为从应用打开。',
    saml_replayed: '该登录请求已被使用，请从应用重新发起。',
```

```ts
    // error.* additions
    reference: '参考编号：{ref}',
    backToDashboard: '返回控制台',
```

```ts
    // login.* addition
    sessionExpired: '你的会话已过期，请重新登录。',
```

```ts
  sessionExpiry: {
    message: '你的会话已过期。',
    signInAgain: '重新登录',
  },
```

- [ ] **Step 3: Grep-verify no curly apostrophe leaked into en.ts**

Run: `grep -nP "\x{2019}" dashboard/src/locales/en.ts`
Expected: no matches (exit 1).

- [ ] **Step 4: Run locale tests**

Run: `cd dashboard && npm run test -- locales`
Expected: parity/params/compile tests PASS.

- [ ] **Step 5: Commit**

```bash
git add dashboard/src/locales/
git commit -m "i18n: error codes + session-expiry/error-page copy (en + zh)"
```

---

### Task 6: API client — context-aware `401 no_session` seam

**Goal:** `lib/api.ts` invokes a registerable handler when (and only when) a response is `401` with body `code === "no_session"`, passing the request method; the call still rejects as before.

**Files:**
- Modify: `dashboard/src/lib/api.ts`
- Test: `dashboard/src/lib/api.test.ts`

**Acceptance Criteria:**
- [ ] `registerUnauthorizedHandler(fn)` stores a handler; passing `null` clears it.
- [ ] On `401 {code:"no_session"}`, the handler is called once with `{ method }` (GET/POST/PUT/DELETE), AND the promise still rejects with the `ApiError`.
- [ ] On `401 {code:"sudo_required"}` (or any non-`no_session` code), the handler is NOT called.
- [ ] On `403`/`500`/`200`, the handler is NOT called.
- [ ] `upload()` triggers the seam too (method `PUT`).

**Verify:** `cd dashboard && npm run test -- api` → PASS

**Steps:**

- [ ] **Step 1: Write the failing tests** (append to `dashboard/src/lib/api.test.ts`):

```ts
import { api, registerUnauthorizedHandler } from './api'

describe('401 no_session seam', () => {
  afterEach(() => registerUnauthorizedHandler(null))

  it('invokes the handler with the method on 401 no_session and still rejects', async () => {
    const spy = vi.fn()
    registerUnauthorizedHandler(spy)
    vi.mocked(fetch).mockResolvedValue(mockResponse(401, JSON.stringify({ code: 'no_session', message: 'x' })))
    await expect(api.get('/api/x')).rejects.toMatchObject({ code: 'no_session' })
    expect(spy).toHaveBeenCalledWith({ method: 'GET' })
  })

  it('does NOT invoke the handler on a non-no_session 401', async () => {
    const spy = vi.fn()
    registerUnauthorizedHandler(spy)
    vi.mocked(fetch).mockResolvedValue(mockResponse(401, JSON.stringify({ code: 'sudo_required', message: 'x' })))
    await expect(api.post('/api/x')).rejects.toMatchObject({ code: 'sudo_required' })
    expect(spy).not.toHaveBeenCalled()
  })

  it('does NOT invoke the handler on 403/500', async () => {
    const spy = vi.fn()
    registerUnauthorizedHandler(spy)
    vi.mocked(fetch).mockResolvedValue(mockResponse(403, JSON.stringify({ code: 'forbidden', message: 'x' })))
    await expect(api.get('/api/x')).rejects.toMatchObject({ code: 'forbidden' })
    expect(spy).not.toHaveBeenCalled()
  })
})
```

- [ ] **Step 2: Run to verify failure**

Run: `cd dashboard && npm run test -- api`
Expected: FAIL (`registerUnauthorizedHandler` is not exported).

- [ ] **Step 3: Implement the seam in `lib/api.ts`.** Add near the top (after `isApiError`):

```ts
export type UnauthorizedHandler = (ctx: { method: string }) => void
let unauthorizedHandler: UnauthorizedHandler | null = null

/**
 * Register a handler invoked when a request returns 401 with code
 * "no_session" (a fully-absent session). Wired in main.ts to redirect reads to
 * /login and surface a banner for mutations. Pass null to clear (tests).
 */
export function registerUnauthorizedHandler(fn: UnauthorizedHandler | null): void {
  unauthorizedHandler = fn
}

function maybeSignalUnauthorized(status: number, err: ApiError, method: string): void {
  if (status === 401 && err.code === 'no_session' && unauthorizedHandler) {
    unauthorizedHandler({ method })
  }
}
```

  In `request()`, replace the `if (!res.ok) { … throw err }` block with:

```ts
  if (!res.ok) {
    const err: ApiError = isApiError(data)
      ? data
      : { code: 'server_error', message: text || res.statusText }
    maybeSignalUnauthorized(res.status, err, method)
    throw err
  }
```

  In `upload()`, before `throw`, add `maybeSignalUnauthorized(res.status, <the ApiError being thrown>, 'PUT')`. Refactor `upload()`'s throw to a named `err` first so it can be passed:

```ts
  if (!res.ok) {
    const err: ApiError = isApiError(data) ? data : { code: 'server_error', message: text || res.statusText }
    maybeSignalUnauthorized(res.status, err, 'PUT')
    throw err
  }
```

- [ ] **Step 4: Run to verify pass**

Run: `cd dashboard && npm run test -- api`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add dashboard/src/lib/api.ts dashboard/src/lib/api.test.ts
git commit -m "feat(api): context-aware 401 no_session seam"
```

---

### Task 7: Session-expiry handler + `useSessionExpiry` composable + main.ts wiring

**Goal:** A unit-testable handler that, on `no_session`, no-ops on public routes; on authenticated routes redirects GET 401s to `/login?return_to=…&reason=session_expired` (clearing auth) and flags mutation 401s for the banner; wired into the seam in `main.ts`.

**Files:**
- Create: `dashboard/src/composables/useSessionExpiry.ts`
- Create: `dashboard/src/lib/sessionExpiry.ts` (the testable handler factory)
- Create: `dashboard/src/lib/sessionExpiry.test.ts`
- Modify: `dashboard/src/main.ts`

**Acceptance Criteria:**
- [ ] On a public route (`name` in the public set OR `meta.public`), the handler does nothing.
- [ ] On an authenticated route + GET: calls `clearAuth()` and `router.replace({name:'login', query:{return_to:<fullPath>, reason:'session_expired'}})`.
- [ ] On an authenticated route + mutation: calls `clearAuth()` and `setExpiredFlag()`, does NOT navigate.
- [ ] Idempotent: a second GET trigger while a redirect is in flight does not call `replace` again.
- [ ] `useSessionExpiry()` exposes a shared `expired` ref + `trigger()`/`reset()`.

**Verify:** `cd dashboard && npm run test -- sessionExpiry` → PASS

**Steps:**

- [ ] **Step 1: Create the composable** `dashboard/src/composables/useSessionExpiry.ts`:

```ts
import { ref } from 'vue'

// Module-level singleton: the banner and the handler share one flag.
const expired = ref(false)

export function useSessionExpiry() {
  return {
    expired,
    trigger(): void { expired.value = true },
    reset(): void { expired.value = false },
  }
}
```

- [ ] **Step 2: Write the failing test** `dashboard/src/lib/sessionExpiry.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createUnauthorizedHandler, __resetHandlingForTest } from './sessionExpiry'

function fakeRouter(name: string, meta: Record<string, unknown> = {}, fullPath = '/sessions') {
  return {
    currentRoute: { value: { name, meta, fullPath } },
    replace: vi.fn().mockResolvedValue(undefined),
  } as never
}

describe('createUnauthorizedHandler', () => {
  beforeEach(() => __resetHandlingForTest())

  it('no-ops on a public route', () => {
    const clearAuth = vi.fn(); const setExpiredFlag = vi.fn()
    const r = fakeRouter('login')
    createUnauthorizedHandler({ router: r, clearAuth, setExpiredFlag })({ method: 'GET' })
    expect(clearAuth).not.toHaveBeenCalled()
    expect((r as never as { replace: ReturnType<typeof vi.fn> }).replace).not.toHaveBeenCalled()
  })

  it('redirects an authenticated GET to /login with return_to + reason', () => {
    const clearAuth = vi.fn(); const setExpiredFlag = vi.fn()
    const r = fakeRouter('sessions', {}, '/sessions')
    createUnauthorizedHandler({ router: r, clearAuth, setExpiredFlag })({ method: 'GET' })
    expect(clearAuth).toHaveBeenCalled()
    const replace = (r as never as { replace: ReturnType<typeof vi.fn> }).replace
    expect(replace).toHaveBeenCalledWith({ name: 'login', query: { return_to: '/sessions', reason: 'session_expired' } })
  })

  it('flags (no navigation) on an authenticated mutation', () => {
    const clearAuth = vi.fn(); const setExpiredFlag = vi.fn()
    const r = fakeRouter('security', {}, '/security')
    createUnauthorizedHandler({ router: r, clearAuth, setExpiredFlag })({ method: 'POST' })
    expect(setExpiredFlag).toHaveBeenCalled()
    expect((r as never as { replace: ReturnType<typeof vi.fn> }).replace).not.toHaveBeenCalled()
  })

  it('is idempotent for concurrent GET triggers', () => {
    const clearAuth = vi.fn(); const setExpiredFlag = vi.fn()
    const r = fakeRouter('sessions')
    const h = createUnauthorizedHandler({ router: r, clearAuth, setExpiredFlag })
    h({ method: 'GET' }); h({ method: 'GET' })
    expect((r as never as { replace: ReturnType<typeof vi.fn> }).replace).toHaveBeenCalledTimes(1)
  })
})
```

- [ ] **Step 3: Run to verify failure**

Run: `cd dashboard && npm run test -- sessionExpiry`
Expected: FAIL (module missing).

- [ ] **Step 4: Implement** `dashboard/src/lib/sessionExpiry.ts`:

```ts
import type { Router } from 'vue-router'

// Threshold/public routes where a no_session is the expected, normal state —
// the handler must never redirect or flag on these (prevents /login loops and
// preserves ConsentView/WelcomeView/boot-time /me handling).
const PUBLIC_ROUTE_NAMES = new Set(['login', 'error', 'welcome', 'consent', 'enroll', 'pair', 'logout'])

export interface SessionExpiryDeps {
  router: Router
  clearAuth: () => void
  setExpiredFlag: () => void
}

let handling = false
/** test-only reset of the idempotency latch */
export function __resetHandlingForTest(): void { handling = false }

/**
 * Build the 401-no_session handler. Route-aware (no-op on public routes),
 * idempotent (one redirect per expiry), and read-vs-mutation aware: GET
 * navigations redirect to /login; mutations flag a non-destructive banner so
 * unsaved form input is not silently discarded.
 */
export function createUnauthorizedHandler(deps: SessionExpiryDeps) {
  return ({ method }: { method: string }): void => {
    const cur = deps.router.currentRoute.value
    const name = String(cur.name ?? '')
    if (cur.meta?.public === true || PUBLIC_ROUTE_NAMES.has(name)) return
    if (handling) return
    if (method === 'GET') {
      handling = true
      deps.clearAuth()
      void deps.router
        .replace({ name: 'login', query: { return_to: cur.fullPath, reason: 'session_expired' } })
        .finally(() => { handling = false })
    } else {
      deps.clearAuth()
      deps.setExpiredFlag()
    }
  }
}
```

- [ ] **Step 5: Run to verify pass**

Run: `cd dashboard && npm run test -- sessionExpiry`
Expected: PASS.

- [ ] **Step 6: Wire into `main.ts`:**

```ts
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import { i18n } from './i18n'
import { registerUnauthorizedHandler } from './lib/api'
import { createUnauthorizedHandler } from './lib/sessionExpiry'
import { useAuthStore } from './stores/auth'
import { useSessionExpiry } from './composables/useSessionExpiry'
import './assets/main.css'

const app = createApp(App)
const pinia = createPinia()
app.use(pinia).use(router).use(i18n)

registerUnauthorizedHandler(
  createUnauthorizedHandler({
    router,
    clearAuth: () => useAuthStore(pinia).clear(),
    setExpiredFlag: () => useSessionExpiry().trigger(),
  }),
)

app.mount('#app')
```

- [ ] **Step 7: Typecheck + commit**

Run: `cd dashboard && npx vue-tsc -b`
Expected: 0 errors.

```bash
git add dashboard/src/composables/useSessionExpiry.ts dashboard/src/lib/sessionExpiry.ts dashboard/src/lib/sessionExpiry.test.ts dashboard/src/main.ts
git commit -m "feat(spa): session-expiry handler (read=redirect, mutation=banner) wired to 401 seam"
```

---

### Task 8: `SessionExpiredBanner` global prompt

**Goal:** A persistent, non-dismissable top banner shown when the mutation-path expiry flag is set, offering "Sign in again" (navigates to `/login?return_to=<current>&reason=session_expired`); mounted app-wide.

**Files:**
- Create: `dashboard/src/components/custom/SessionExpiredBanner.vue`
- Create: `dashboard/src/components/custom/SessionExpiredBanner.test.ts`
- Modify: `dashboard/src/App.vue`

**Acceptance Criteria:**
- [ ] Renders nothing when `expired` is false; renders a `role="alert"` banner with the message + a "Sign in again" button when true.
- [ ] Clicking "Sign in again" calls `router.push` to `/login` with `return_to` = current `fullPath` and `reason: 'session_expired'`, then resets the flag.
- [ ] Mounted once in `App.vue` above `<RouterView />`.

**Verify:** `cd dashboard && npm run test -- SessionExpiredBanner` → PASS

**Steps:**

- [ ] **Step 1: Write the failing test** `dashboard/src/components/custom/SessionExpiredBanner.test.ts` (follow the existing custom-component test setup — mount with i18n + a router mock; grep another `custom/*.test.ts` for the `mountWithI18n` helper if present):

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import SessionExpiredBanner from './SessionExpiredBanner.vue'
import { useSessionExpiry } from '@/composables/useSessionExpiry'
import { i18n } from '@/i18n'

const push = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push, currentRoute: { value: { fullPath: '/security' } } }),
}))

describe('SessionExpiredBanner', () => {
  beforeEach(() => { useSessionExpiry().reset(); push.mockClear() })

  it('renders nothing when not expired', () => {
    const w = mount(SessionExpiredBanner, { global: { plugins: [i18n] } })
    expect(w.find('[role="alert"]').exists()).toBe(false)
  })

  it('shows the banner and navigates on click', async () => {
    useSessionExpiry().trigger()
    const w = mount(SessionExpiredBanner, { global: { plugins: [i18n] } })
    expect(w.find('[role="alert"]').exists()).toBe(true)
    await w.find('button').trigger('click')
    expect(push).toHaveBeenCalledWith({ name: 'login', query: { return_to: '/security', reason: 'session_expired' } })
    expect(useSessionExpiry().expired.value).toBe(false)
  })
})
```

- [ ] **Step 2: Run to verify failure**

Run: `cd dashboard && npm run test -- SessionExpiredBanner`
Expected: FAIL (component missing).

- [ ] **Step 3: Implement** `dashboard/src/components/custom/SessionExpiredBanner.vue`:

```vue
<script setup lang="ts">
/**
 * SessionExpiredBanner — shown when a session expires during an in-page
 * MUTATION (the read path redirects directly). Persistent + non-dismissable so
 * the user notices, but non-modal so they can copy any unsaved input before
 * re-authenticating. Mounted once app-wide in App.vue.
 */
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useSessionExpiry } from '@/composables/useSessionExpiry'
import { Button } from '@/components/ui/button'

const router = useRouter()
const { t } = useI18n()
const { expired, reset } = useSessionExpiry()

function signInAgain(): void {
  const returnTo = router.currentRoute.value.fullPath
  reset()
  void router.push({ name: 'login', query: { return_to: returnTo, reason: 'session_expired' } })
}
</script>

<template>
  <div
    v-if="expired"
    role="alert"
    class="fixed inset-x-0 top-0 z-50 flex items-center justify-center gap-4 bg-danger px-4 py-3 text-sm text-danger-foreground shadow"
  >
    <span>{{ t('sessionExpiry.message') }}</span>
    <Button size="sm" variant="secondary" @click="signInAgain">
      {{ t('sessionExpiry.signInAgain') }}
    </Button>
  </div>
</template>
```

(Use color tokens that exist in the project's theme — confirm `bg-danger`/`text-danger-foreground` exist; if the token names differ, use the established danger token pair. Check `dashboard/src/components/custom/StatusMessage.vue` / `Alert.vue` for the canonical danger classes and match them.)

- [ ] **Step 4: Mount in `App.vue`:**

```vue
<script setup lang="ts">
import { useTheme } from '@/composables/useTheme'
import { useLocale } from '@/composables/useLocale'
import SessionExpiredBanner from '@/components/custom/SessionExpiredBanner.vue'
useTheme()
useLocale()
</script>

<template>
  <SessionExpiredBanner />
  <RouterView />
</template>
```

- [ ] **Step 5: Run test + typecheck**

Run: `cd dashboard && npm run test -- SessionExpiredBanner && npx vue-tsc -b`
Expected: PASS + 0 type errors.

- [ ] **Step 6: Commit**

```bash
git add dashboard/src/components/custom/SessionExpiredBanner.vue dashboard/src/components/custom/SessionExpiredBanner.test.ts dashboard/src/App.vue
git commit -m "feat(spa): non-destructive session-expired banner for in-page mutations"
```

---

### Task 9: `LoginView` session-expired notice

**Goal:** When `?reason=session_expired` is present, `LoginView` shows a notice; on mount it clears the banner flag so the prompt doesn't linger once the user reaches login.

**Files:**
- Modify: `dashboard/src/pages/LoginView.vue`
- Test: `dashboard/src/pages/LoginView.test.ts`

**Acceptance Criteria:**
- [ ] With `route.query.reason === 'session_expired'`, an `Alert`/notice with `login.sessionExpired` copy renders.
- [ ] Without it, no notice renders.
- [ ] On mount, `useSessionExpiry().reset()` is called.

**Verify:** `cd dashboard && npm run test -- LoginView` → PASS

**Steps:**

- [ ] **Step 1: Add the failing test** (extend `dashboard/src/pages/LoginView.test.ts` — match its existing route/i18n mock setup):

```ts
it('shows the session-expired notice when reason=session_expired', async () => {
  // mount LoginView with route.query = { reason: 'session_expired' } per the file's harness
  // expect text t('login.sessionExpired') to be present
})
```

- [ ] **Step 2: Implement.** In `LoginView.vue` `<script setup>` add:

```ts
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useSessionExpiry } from '@/composables/useSessionExpiry'
import { Alert, AlertDescription } from '@/components/ui/alert'

const route = useRoute()
const sessionExpired = computed(() => route.query.reason === 'session_expired')
```

  In the existing `onMounted`, add as the first line: `useSessionExpiry().reset()`.

  In the template, inside the card `<div class="flex flex-col gap-6">` (above the `checking` template), add:

```vue
      <Alert v-if="sessionExpired" role="status" aria-live="polite">
        <AlertDescription>{{ t('login.sessionExpired') }}</AlertDescription>
      </Alert>
```

- [ ] **Step 3: Run test + typecheck**

Run: `cd dashboard && npm run test -- LoginView && npx vue-tsc -b`
Expected: PASS + 0 type errors.

- [ ] **Step 4: Commit**

```bash
git add dashboard/src/pages/LoginView.vue dashboard/src/pages/LoginView.test.ts
git commit -m "feat(spa): session-expired notice on the login page"
```

---

### Task 10: `ErrorView` upgrade — reference line + auth-aware return button

**Goal:** `ErrorView` shows the `ref` query value as a support reference, and picks its return button based on whether the user has a session ("Back to dashboard" vs "Return to sign in").

**Files:**
- Modify: `dashboard/src/pages/ErrorView.vue`
- Test: `dashboard/src/pages/ErrorView.test.ts` (create if absent; otherwise extend)

**Acceptance Criteria:**
- [ ] With `?error=upstream_error&ref=abc123`, the page shows the `errors.upstream_error` message and a "Reference: abc123" line.
- [ ] Without `ref`, no reference line renders.
- [ ] When the auth store has a session (`me` set), the button links to `/security` with `error.backToDashboard`; otherwise to `/login` with `error.returnToLogin`.

**Verify:** `cd dashboard && npm run test -- ErrorView` → PASS

**Steps:**

- [ ] **Step 1: Write/extend the test** `dashboard/src/pages/ErrorView.test.ts` (mount with i18n + a Pinia instance + route mock; grep an existing page test for the harness):

```ts
it('renders the reference line when ref is present', () => {
  // route.query = { error: 'upstream_error', ref: 'abc123' }; auth.me = null
  // expect text includes t('errors.upstream_error') and t('error.reference', { ref: 'abc123' })
})

it('shows Back to dashboard when authenticated', async () => {
  // auth store me set; expect a RouterLink to /security with t('error.backToDashboard')
})
```

- [ ] **Step 2: Implement.** In `ErrorView.vue` `<script setup>` add:

```ts
import { onMounted, ref as vueRef } from 'vue'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const reference = computed(() => String(route.query.ref ?? ''))
const hasSession = vueRef(false)

onMounted(async () => {
  // Public route — a 401 here is fine (the global handler no-ops on /error).
  try { await auth.ensureLoaded() } catch { /* ignore */ }
  hasSession.value = !!auth.me
})
```

  In the template, add the reference line under the message `<p>`:

```vue
      <p v-if="reference" class="text-xs text-muted">{{ t('error.reference', { ref: reference }) }}</p>
```

  Replace the existing return-button block with the auth-aware version:

```vue
      <Button as-child variant="outline" class="w-full">
        <RouterLink v-if="hasSession" to="/security">{{ t('error.backToDashboard') }}</RouterLink>
        <RouterLink v-else to="/login">{{ t('error.returnToLogin') }}</RouterLink>
      </Button>
```

- [ ] **Step 3: Run test + typecheck**

Run: `cd dashboard && npm run test -- ErrorView && npx vue-tsc -b`
Expected: PASS + 0 type errors.

- [ ] **Step 4: Commit**

```bash
git add dashboard/src/pages/ErrorView.vue dashboard/src/pages/ErrorView.test.ts
git commit -m "feat(spa): ErrorView reference line + auth-aware return button"
```

---

### Task 11: Smoke + runtime verification + full gate + dist rebuild

**Goal:** Add smoke coverage for the new redirect behavior, verify end-to-end at runtime in chromium, run the full gate, rebuild + commit the embedded SPA.

**Files:**
- Modify: the repo smoke script (find it: `grep -rl "SMOKE_EXIT" --include="*.sh" .` and the `mise`/scripts dir) — add a federation-callback-error case and a SAML bad-request case.
- Modify: `dashboard/pkg/webui/dist/*` (rebuilt artifacts) — actually `pkg/webui/dist/*`.

**Acceptance Criteria:**
- [ ] Smoke asserts `GET /api/prohibitorum/auth/federation/<slug>/callback?error=access_denied` returns `302` with `Location` starting `/error?error=upstream_error` (follow-redirects off).
- [ ] Smoke asserts a malformed SAML `GET /saml/sso?SAMLRequest=not-base64` returns `302` `Location` starting `/error?error=saml_request_invalid`.
- [ ] Runtime (chromium): session-expiry mid-app redirects to `/login` + shows the notice; a federation/SAML failure shows the styled `/error` page (not raw JSON/plaintext).
- [ ] Full gate green; `pkg/webui/dist` rebuilt and committed.

**Verify:** see steps (smoke `SMOKE_EXIT=0`; gate commands all exit 0).

**Steps:**

- [ ] **Step 1: Locate + extend the smoke script.** Find it (`grep -rl "SMOKE_EXIT" .`), and add two assertions in the same style as existing cases (curl with `-s -o /dev/null -w "%{http_code} %{redirect_url}"` and `--max-redirs 0`). Federation case: hit the callback with `?error=access_denied`, assert `302` + `Location` prefix `/error?error=upstream_error`. SAML case: hit `/saml/sso?SAMLRequest=not-base64`, assert `302` + `Location` prefix `/error?error=saml_request_invalid`. Mirror the existing smoke harness's server bring-up (per project memory, the smoke needs `PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK=true` and may need a fresh DB on an alternate port if `:8080` is held).

- [ ] **Step 2: Run the backend gate**

```bash
go build -tags nodynamic ./... && go vet ./... && go test ./...
```
Expected: all exit 0. (`pkg/server` may flake under parallel shared-DB runs — re-run the affected test in isolation to confirm.)

- [ ] **Step 3: Run the frontend gate**

```bash
cd dashboard && npm run test && npx vue-tsc -b && node scripts/check-contrast.mjs
```
Expected: vitest all green (new suites included), 0 type errors, contrast pairs all pass.

- [ ] **Step 4: Runtime verification in chromium (Playwright).** With a dev server running (`mise dev-server` + `mise dev-seed` + an enrolled admin), drive chromium (Playwright; `/usr/sbin/chromium --no-sandbox` executablePath fallback per project memory):
  - Sign in, navigate to a page, then delete/expire the session cookie server-side (or via devtools) and click another nav item → assert the URL becomes `/login?...reason=session_expired` and the notice text renders.
  - Submit a form (e.g. profile edit) after expiry → assert the banner appears (not an auto-redirect) and "Sign in again" navigates to `/login`.
  - Visit `/api/prohibitorum/auth/federation/<slug>/callback?error=access_denied` directly → assert the rendered page is the styled `/error` view with a "Reference: …" line, not raw JSON.
  - Capture screenshots as evidence.

- [ ] **Step 5: Rebuild + commit the embedded SPA**

```bash
cd dashboard && npm run build
cd .. && git add pkg/webui/dist && git commit -m "build(webui): rebuild embedded SPA for auth-flow error pages + session redirect"
```

- [ ] **Step 6: Final gate sanity**

```bash
go build -tags nodynamic ./... && go test ./... && cd dashboard && npm run test && npx vue-tsc -b
```
Expected: all green.

---

## Self-Review Notes

- **Spec coverage:** Part A (Tasks 6–9), Part B backend (Tasks 1–4), Part C ErrorView (Task 10), i18n (Task 5), verification (Task 11). Helper-placement constraint (leaf `pkg/weberr`) honored in Task 1. Correlation `ref` threaded through Tasks 1–4 + surfaced in Task 10. Masking honored via the per-package `errorPage`/`redirectToErrorPage` logging only code/ref/path.
- **Type consistency:** `RedirectToError(w, r, code, ref)` + `NewRef()` used identically across Tasks 1–4; `registerUnauthorizedHandler` (Task 6) ↔ `createUnauthorizedHandler` deps (Task 7) ↔ `useSessionExpiry` (Tasks 7,8,9); `error.reference`/`error.backToDashboard`/`login.sessionExpired`/`sessionExpiry.*` defined in Task 5 and consumed in Tasks 8–10.
- **Preserved-behavior guards** are explicit in every backend task (RP redirects, SAML SP-binding responses, XHR JSON).
