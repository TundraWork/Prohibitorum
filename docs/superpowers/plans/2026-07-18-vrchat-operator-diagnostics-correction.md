# VRChat Operator and Diagnostics Correction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Correct VRChat operator error placement, accept and safely normalize VRChat's documented login cookie, and make request-ID diagnostics record and retrieve real failures.

**Architecture:** The provider detail page gets an operator-scoped request state. VRChat response-cookie parsing accepts an omitted inbound `Secure` flag only at the pinned HTTPS origin and normalizes accepted cookies before persistence. A server middleware observes canonical public errors from raw chi and typed Huma paths, then writes one curated diagnostic record without affecting the response.

**Tech Stack:** Go 1.25, chi v5, Huma v2, PostgreSQL/sqlc, Vue 3, TypeScript, Vitest, Vue Test Utils, mise.

## Global Constraints

- Preserve all existing OIDC, Steam, SAML, authentication, and authorization contracts.
- Never store or log VRChat credentials, cookie values, request bodies, raw errors, headers, tokens, or SQL values.
- Keep diagnostic lookup exact-ID, admin-only, fresh-sudo gated, rate-limited, audited, and non-enumerable.
- A diagnostic write failure must never alter the original response status, headers, or JSON body.
- Accept a missing inbound `Secure` flag only from an already-approved fixed HTTPS VRChat origin; stored and outbound cookies must remain host-only and `Secure=true`.
- Do not include the pre-existing untracked root `package-lock.json` in any commit.

## File Responsibilities

- `dashboard/src/pages/admin/AdminUpstreamIdpDetailView.vue`: owns separate page and operator request states and contextual rendering.
- `dashboard/src/pages/admin/AdminUpstreamIdpDetailView.test.ts`: proves DOM placement and request-state isolation.
- `pkg/federation/providers/vrchat/cookies.go`: validates inbound VRChat cookie syntax/scope and normalizes accepted cookies.
- `pkg/federation/providers/vrchat/cookies_test.go`: proves documented-cookie compatibility and retained safety checks.
- `cmd/vrchatmock/main.go`: reproduces VRChat's documented missing-`Secure` login response.
- `cmd/vrchatmock/main_test.go`: locks the mock response contract.
- `pkg/weberr/weberr.go`: emits an observation callback only after public code/detail filtering.
- `pkg/server/diagnostic_capture.go`: request-local observation and post-handler diagnostic persistence.
- `pkg/server/server.go`: installs diagnostic capture after session loading and before route handlers.
- `pkg/server/operations.go`: reports typed Huma public errors to the same request-local capture.
- `pkg/server/huma_error_envelope_test.go`: proves typed Huma errors are recorded.
- `pkg/server/diagnostic_capture_test.go`: proves raw errors, safe fields, and write-failure isolation.
- `pkg/authn/errors.go`: registers and constructs `diagnostic_not_found`.
- `pkg/server/handle_admin_diagnostics.go`: uses the diagnostic-specific missing-resource error.
- `pkg/server/handle_admin_diagnostics_test.go`: proves missing lookup classification.
- `dashboard/src/lib/errorCodes.ts`, `dashboard/src/locales/en.ts`, `dashboard/src/locales/zh.ts`: expose/localize the new registered code.

---

### Task 1: Contextual operator request state

**Files:**
- Modify: `dashboard/src/pages/admin/AdminUpstreamIdpDetailView.vue:34-38,282-394,524-643`
- Test: `dashboard/src/pages/admin/AdminUpstreamIdpDetailView.test.ts`

**Interfaces:**
- Consumes: existing `useApi(): { busy, error, run, clear }` and `ErrorPanel`.
- Produces: component-local `operatorBusy`, `operatorError`, `runOperator`, and `clearOperator` bindings; no exported API change.

- [ ] **Step 1: Write the failing placement and isolation test**

Add this test inside the existing `describe` block:

```ts
it('renders operator failures after the active controls without using page error state', async () => {
  post.mockRejectedValue({
    code: 'upstream_temporarily_unavailable',
    requestId: 'rid-operator',
  })
  put.mockResolvedValue({ ...VRCHAT, displayName: 'Updated VRChat' })
  const w = await mountVrchat()
  await w.get('input[name="operatorUsername"]').setValue('operator')
  await w.get('input[name="operatorPassword"]').setValue('secret')
  await w.get('[data-test="operator-credentials-form"]').trigger('submit')
  await flushPromises()

  const card = w.get('[data-test="operator-session-card"]')
  const form = card.get('[data-test="operator-credentials-form"]')
  const alert = card.get('[role="alert"]')
  expect(w.findAll('[role="alert"]')).toHaveLength(1)
  expect(form.element.compareDocumentPosition(alert.element) & Node.DOCUMENT_POSITION_FOLLOWING).not.toBe(0)
  expect(alert.text()).toContain(en.errors.codes.upstream_temporarily_unavailable)

  await w.get('[data-test="save"]').trigger('click')
  await flushPromises()
  expect(card.get('[role="alert"]').text()).toContain(en.errors.codes.upstream_temporarily_unavailable)
})
```

- [ ] **Step 2: Run the test and verify the current global placement fails**

Run:

```bash
npm test -- --run dashboard/src/pages/admin/AdminUpstreamIdpDetailView.test.ts -t 'renders operator failures after the active controls'
```

Expected: FAIL because the operator card has no `[role="alert"]`.

- [ ] **Step 3: Add operator-scoped state and route all operator actions through it**

Next to the existing page state, add:

```ts
const { busy, error, run, clear } = useApi()
const {
  busy: operatorBusy,
  error: operatorError,
  run: runOperator,
  clear: clearOperator,
} = useApi()
```

In `startOperatorSession`, `verifyOperatorSession`, and `validateOperatorSession`, replace only their `run(...)` calls with `runOperator(...)`. In the verification failure branch, read `operatorError.value?.code`. In `replaceOperatorSession`, call `clearOperator()` instead of `clear()`.

For the operator start, verify, validate, and replace buttons, replace `busy` with `operatorBusy`. Leave save, disable, rotation, delete, initial load, and the page-level error panel on the original `busy/error/run/clear` state.

After the credential form / challenge form / valid-session action block and before `</CardContent>`, add:

```vue
<ErrorPanel
  v-if="operatorError"
  :error="operatorError"
  :is-admin="true"
  @dismiss="clearOperator"
/>
```

- [ ] **Step 4: Run the focused page suite**

Run:

```bash
npm test -- --run dashboard/src/pages/admin/AdminUpstreamIdpDetailView.test.ts
```

Expected: all `AdminUpstreamIdpDetailView` tests pass.

- [ ] **Step 5: Commit the UI correction**

```bash
git add dashboard/src/pages/admin/AdminUpstreamIdpDetailView.vue dashboard/src/pages/admin/AdminUpstreamIdpDetailView.test.ts
git diff --cached --check
git commit -m "fix: place VRChat operator errors contextually"
```

---

### Task 2: Documented VRChat cookie compatibility

**Files:**
- Modify: `pkg/federation/providers/vrchat/cookies.go:64-83`
- Test: `pkg/federation/providers/vrchat/cookies_test.go`
- Modify: `cmd/vrchatmock/main.go`
- Test: `cmd/vrchatmock/main_test.go`

**Interfaces:**
- Consumes: `validateResponseCookies(origin *url.URL, header http.Header, now time.Time) ([]http.Cookie, error)`.
- Produces: the same signature, with every accepted non-deletion response cookie normalized to `Domain == ""` and `Secure == true`.

- [ ] **Step 1: Add failing compatibility and containment tests**

Add to `cookies_test.go`:

```go
func TestCookieValidateNormalizesDocumentedCookieWithoutSecure(t *testing.T) {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	cookies, err := validateResponseCookies(cookieTestOrigin(t), cookieTestHeader(
		"auth=documented-secret; Path=/; HttpOnly; Expires="+now.Add(time.Hour).Format(http.TimeFormat),
	), now)
	if err != nil {
		t.Fatalf("validateResponseCookies() error = %v", err)
	}
	if len(cookies) != 1 || !cookies[0].Secure || cookies[0].Domain != "" || !cookies[0].HttpOnly {
		t.Fatalf("normalized cookie = %#v", cookies)
	}
	if _, err := encodeCookies(cookies); err != nil {
		t.Fatalf("encodeCookies(normalized) error = %v", err)
	}
}

func TestCookieValidateStillRejectsMissingHTTPOnly(t *testing.T) {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	_, err := validateResponseCookies(cookieTestOrigin(t), cookieTestHeader(
		"auth=secret; Path=/; Expires="+now.Add(time.Hour).Format(http.TimeFormat),
	), now)
	if !errors.Is(err, errInvalidCookie) {
		t.Fatalf("error = %v, want errInvalidCookie", err)
	}
}
```

Add to `cmd/vrchatmock/main_test.go` a login request assertion that the returned `auth` cookie is `HttpOnly`, has `Path=/`, and has `Secure == false`. Keep the existing follow-up cookie-authentication assertion.

- [ ] **Step 2: Run tests and verify missing Secure is rejected and mock differs from production**

Run:

```bash
go test ./pkg/federation/providers/vrchat ./cmd/vrchatmock -run 'TestCookieValidateNormalizesDocumentedCookieWithoutSecure|TestCookieValidateStillRejectsMissingHTTPOnly|Test.*AuthCookie' -count=1
```

Expected: the normalization test fails with `errInvalidCookie`; the mock assertion fails because `Secure` is currently true.

- [ ] **Step 3: Normalize accepted response cookies before any merge or persistence**

Change `normalizeResponseCookie` so the initial rejection condition no longer contains `!cookie.Secure`, then normalize after domain validation:

```go
if cookie == nil || (cookie.Name != "auth" && cookie.Name != "twoFactorAuth") ||
	cookie.Path != "/" || !cookie.HttpOnly ||
	len(cookie.Unparsed) != 0 || !validSameSite(cookie.SameSite) || cookie.Valid() != nil {
	return false
}
if cookie.Domain != "" && !strings.EqualFold(cookie.Domain, "api.vrchat.cloud") {
	return false
}
cookie.Domain = ""
cookie.Secure = true
```

Do not relax `validAuthenticationCookie`; encoded, decoded, merged-prior, and outbound cookies must continue to require `Secure`.

In the mock helper that sets `auth` and `twoFactorAuth`, remove `Secure: true` while preserving `HttpOnly: true`, `Path: "/"`, expiry, and `SameSite`.

- [ ] **Step 4: Run all cookie/client/mock tests**

Run:

```bash
go test ./pkg/federation/providers/vrchat ./cmd/vrchatmock -count=1
```

Expected: both packages pass, including unsafe path/domain/name/attribute and stored-cookie tests.

- [ ] **Step 5: Commit the compatibility correction**

```bash
git add pkg/federation/providers/vrchat/cookies.go pkg/federation/providers/vrchat/cookies_test.go cmd/vrchatmock/main.go cmd/vrchatmock/main_test.go
git diff --cached --check
git commit -m "fix: normalize documented VRChat auth cookies"
```

---

### Task 3: Canonical request diagnostic capture

**Files:**
- Modify: `pkg/weberr/weberr.go`
- Create: `pkg/server/diagnostic_capture.go`
- Create: `pkg/server/diagnostic_capture_test.go`
- Modify: `pkg/server/server.go:253-290`
- Modify: `pkg/server/operations.go:90-110`
- Test: `pkg/server/huma_error_envelope_test.go`

**Interfaces:**
- Consumes: `diagnostic.StoreWriter.Record(context.Context, diagnostic.Record) error`, `weberr.DefinitionFor(code)`, `weberr.RequestIDFromContext(ctx)`, `authn.SessionFromContext(ctx)`, and chi's matched route pattern.
- Produces: `diagnosticCaptureMW(store diagnostic.StoreWriter) func(http.Handler) http.Handler`; internal request capture method `observe(code string, fields map[string]any)`; `weberr.WriteJSON` observation through a structural `ObservePublicError(string, map[string]any)` writer interface.

- [ ] **Step 1: Write failing raw-handler and write-failure tests**

Create `pkg/server/diagnostic_capture_test.go` with a fake writer:

```go
package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"prohibitorum/pkg/diagnostic"
	"prohibitorum/pkg/weberr"
)

type recordingDiagnosticWriter struct {
	records []diagnostic.Record
	err     error
}

func (w *recordingDiagnosticWriter) Record(_ context.Context, rec diagnostic.Record) error {
	w.records = append(w.records, rec)
	return w.err
}

func TestDiagnosticCaptureRecordsCanonicalRawError(t *testing.T) {
	store := &recordingDiagnosticWriter{}
	router := chi.NewRouter()
	router.Use(diagnosticCaptureMW(store))
	router.Get("/broken/{id}", func(w http.ResponseWriter, r *http.Request) {
		weberr.WriteJSON(w, "validation_failed", map[string]any{"location": "body.name", "secret": "must-not-survive"}, "rid-capture")
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/broken/42", nil))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(store.records) != 1 {
		t.Fatalf("records = %d, want 1", len(store.records))
	}
	got := store.records[0]
	if got.RequestID != "rid-capture" || got.Code != "validation_failed" || got.Method != http.MethodGet || got.Route != "/broken/{id}" {
		t.Fatalf("record = %#v", got)
	}
	if _, leaked := got.Fields["secret"]; leaked {
		t.Fatalf("record leaked rejected detail: %#v", got.Fields)
	}
}

func TestDiagnosticCaptureWriteFailureDoesNotChangeResponse(t *testing.T) {
	store := &recordingDiagnosticWriter{err: errors.New("database unavailable: private value")}
	router := chi.NewRouter()
	router.Use(diagnosticCaptureMW(store))
	router.Get("/broken", func(w http.ResponseWriter, r *http.Request) {
		weberr.WriteJSON(w, "server_error", nil, "rid-write-failure")
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/broken", nil))
	if rec.Code != http.StatusInternalServerError || rec.Body.String() != `{"code":"server_error","requestId":"rid-write-failure"}`+"\n" {
		t.Fatalf("response changed: status=%d body=%q", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run raw capture tests and verify the middleware is absent**

Run:

```bash
go test ./pkg/server -run 'TestDiagnosticCapture' -count=1
```

Expected: build FAIL because `diagnosticCaptureMW` is undefined.

- [ ] **Step 3: Add a post-filter observation hook to the canonical raw writer**

In `pkg/weberr/weberr.go`, define the structural interface and notify after registry fallback/detail filtering but before writing:

```go
type publicErrorObserver interface {
	ObservePublicError(code string, details map[string]any)
}

func observePublicError(w http.ResponseWriter, code string, details map[string]any) {
	if observer, ok := w.(publicErrorObserver); ok {
		observer.ObservePublicError(code, details)
	}
}
```

Inside `WriteJSON`, after `details` is filtered and before `WriteHeader`, call:

```go
observePublicError(w, code, details)
```

The observer receives only the final registered code and allowlisted details, never the original error.

- [ ] **Step 4: Implement request-local capture and safe post-handler persistence**

Create `pkg/server/diagnostic_capture.go`. The implementation must:

```go
type diagnosticCaptureKey struct{}

type diagnosticCapture struct {
	code   string
	fields map[string]any
}

func (c *diagnosticCapture) observe(code string, fields map[string]any) {
	if c.code != "" {
		return
	}
	c.code = code
	if len(fields) != 0 {
		c.fields = make(map[string]any, len(fields))
		for key, value := range fields {
			c.fields[key] = value
		}
	}
}

type diagnosticResponseWriter struct {
	http.ResponseWriter
	capture *diagnosticCapture
}

func (w *diagnosticResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *diagnosticResponseWriter) ObservePublicError(code string, fields map[string]any) {
	w.capture.observe(code, fields)
}

func observeDiagnostic(ctx context.Context, code string, fields map[string]any) {
	if capture, ok := ctx.Value(diagnosticCaptureKey{}).(*diagnosticCapture); ok {
		capture.observe(code, fields)
	}
}
```

`diagnosticCaptureMW` creates the capture, adds it to the request context, passes a `diagnosticResponseWriter`, then records only when a canonical code was observed. Derive `route := chi.RouteContext(r.Context()).RoutePattern()` after `next.ServeHTTP`; if it is empty, use `"unmatched"`, never `r.URL.Path`. Set `Operation` to `r.Method + " " + route`, `Retryable` from `weberr.DefinitionFor(capture.code)`, and `AccountID` only from a non-nil authenticated session account. Use `context.WithoutCancel(r.Context())` for the write. On write failure, emit only a fixed warning such as `diagnostic: record failed`; do not attach the returned error.

- [ ] **Step 5: Wire the middleware into production in the correct order**

In `pkg/server/server.go`, construct `diagStore := diagnostic.New(queries)` before `router := chi.NewMux()`. Install:

```go
router.Use(weberr.RequestID)
router.Use(requestMetaMW(clientIPResolver.IP))
router.Use(sessstore.LoadSession(config, queries, sessionStore, clientIPResolver.IP))
router.Use(diagnosticCaptureMW(diagStore))
router.Use(maintenanceGateMW(brandingResolver))
```

Remove the later duplicate `diagStore := diagnostic.New(queries)`.

- [ ] **Step 6: Prove every typed Huma error path uses the same capture**

In the `humaConfig` response transformer, observe `*weberr.PublicError` values after stamping the request ID:

```go
if pe, ok := v.(*weberr.PublicError); ok {
	pe.RequestID = weberr.RequestIDFromContext(ctx.Context())
	observeDiagnostic(ctx.Context(), pe.Code, pe.Details)
}
```

This covers typed handler errors and Huma validation errors. In `writeHumaPublicErr`, after final registry fallback and detail filtering and before setting the response body, also add:

```go
observeDiagnostic(ctx.Context(), code, details)
```

This covers auth and sudo middleware rejections that bypass Huma serialization.

Add a `buildTestAPIWithDiagnostic` helper in `huma_error_envelope_test.go` that calls `router.Use(diagnosticCaptureMW(store))` before `humachi.New(router, humaConfig())`. Add a typed operation returning `authErrToHuma(authn.ErrInvalidRole())`, serve it through `weberr.RequestID(router)`, and assert one record with code `invalid_role`, route `/test/diagnostic/{id}`, method `GET`, account ID `1`, and fields containing only the registry-allowed `allowed` key.

- [ ] **Step 7: Run focused diagnostics and envelope suites**

Run:

```bash
gofmt -w pkg/weberr/weberr.go pkg/server/diagnostic_capture.go pkg/server/diagnostic_capture_test.go pkg/server/server.go pkg/server/operations.go pkg/server/huma_error_envelope_test.go
go test ./pkg/weberr ./pkg/diagnostic ./pkg/server -run 'Diagnostic|Huma|PublicErr|RequestID' -count=1
```

Expected: all selected tests pass; raw and typed errors each create exactly one safe record, and write failure preserves the original response.

- [ ] **Step 8: Commit diagnostic capture**

```bash
git add pkg/weberr/weberr.go pkg/server/diagnostic_capture.go pkg/server/diagnostic_capture_test.go pkg/server/server.go pkg/server/operations.go pkg/server/huma_error_envelope_test.go
git diff --cached --check
git commit -m "fix: persist request diagnostics"
```

---

### Task 4: Diagnostic lookup classification and frontend copy

**Files:**
- Modify: `pkg/authn/errors.go`
- Modify: `pkg/weberr/coverage_test.go`
- Modify: `pkg/server/handle_admin_diagnostics.go`
- Test: `pkg/server/handle_admin_diagnostics_test.go`
- Modify: `dashboard/src/lib/errorCodes.ts`
- Modify: `dashboard/src/locales/en.ts`
- Modify: `dashboard/src/locales/zh.ts`
- Test: `dashboard/src/locales.errors.parity.test.ts`

**Interfaces:**
- Produces: `authn.ErrDiagnosticNotFound() *authn.AuthError` registered as HTTP 404, code `diagnostic_not_found`, diagnostic kind `resource`, no detail keys, no retry/recovery hint.

- [ ] **Step 1: Change the missing-record test to require the dedicated code**

Update the not-found handler test to decode `weberr.PublicError` and assert:

```go
if rec.Code != http.StatusNotFound {
	t.Fatalf("status = %d, want 404", rec.Code)
}
var body weberr.PublicError
if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
	t.Fatal(err)
}
if body.Code != "diagnostic_not_found" {
	t.Fatalf("code = %q, want diagnostic_not_found", body.Code)
}
```

- [ ] **Step 2: Run the test and verify it currently reports account_not_found**

Run:

```bash
go test ./pkg/server -run 'DiagnosticLookup.*NotFound' -count=1
```

Expected: FAIL with code `account_not_found`.

- [ ] **Step 3: Register and return the diagnostic-specific error**

Add to the resource section of the auth registry:

```go
{Code: "diagnostic_not_found", Status: http.StatusNotFound, LocaleKey: "errors.diagnostic_not_found", DiagnosticKind: "resource"},
```

Add the constructor:

```go
func ErrDiagnosticNotFound() *AuthError {
	return newErr(http.StatusNotFound, "diagnostic_not_found", "Diagnostic record not found.")
}
```

In `handleAdminDiagnosticLookupHTTP`, replace `authn.ErrAccountNotFound()` for `diagnostic.ErrNotFound` with `authn.ErrDiagnosticNotFound()`.

Add an entry to registry coverage tests so the constructor and definition remain in sync.

- [ ] **Step 4: Add frontend manifest and localized copy**

Insert the alphabetically ordered manifest row:

```ts
{ code: 'diagnostic_not_found', details: [], recovery: '' },
```

Add locale keys under `errors.codes`:

```ts
// en.ts
diagnostic_not_found: 'That diagnostic record no longer exists or has expired.',

// zh.ts
diagnostic_not_found: '该诊断记录已不存在或已过期。',
```

- [ ] **Step 5: Run backend and frontend parity tests**

Run:

```bash
gofmt -w pkg/authn/errors.go pkg/weberr/coverage_test.go pkg/server/handle_admin_diagnostics.go pkg/server/handle_admin_diagnostics_test.go
go test ./pkg/authn ./pkg/weberr ./pkg/server -run 'Diagnostic|Registry|Coverage|Known' -count=1
npm test -- --run dashboard/src/locales.errors.parity.test.ts dashboard/src/lib/errors.test.ts
```

Expected: all selected tests pass and locale/manifest parity reports no missing or extra code.

- [ ] **Step 6: Commit lookup classification**

```bash
git add pkg/authn/errors.go pkg/weberr/coverage_test.go pkg/server/handle_admin_diagnostics.go pkg/server/handle_admin_diagnostics_test.go dashboard/src/lib/errorCodes.ts dashboard/src/locales/en.ts dashboard/src/locales/zh.ts
git diff --cached --check
git commit -m "fix: classify missing diagnostics"
```

---

### Task 5: End-to-end verification, bundle refresh, and delivery

**Files:**
- Modify (generated): `pkg/webui/dist/**`
- Modify: `STATUS.md` only if the existing v0.8 changelog section requires a correction note.

**Interfaces:**
- Consumes: completed Tasks 1-4.
- Produces: refreshed embedded dashboard bundle and verified master branch.

- [ ] **Step 1: Run focused backend and dashboard suites**

```bash
go test ./pkg/federation/providers/vrchat ./pkg/weberr ./pkg/diagnostic ./pkg/server ./cmd/vrchatmock -count=1
npm test -- --run dashboard/src/pages/admin/AdminUpstreamIdpDetailView.test.ts dashboard/src/components/custom/ErrorPanel.test.ts dashboard/src/locales.errors.parity.test.ts dashboard/src/lib/errors.test.ts
```

Expected: all packages and Vitest files pass.

- [ ] **Step 2: Build the dashboard and refresh the embedded bundle**

```bash
npm run build
```

Expected: Vue type-check and Vite production build pass; `pkg/webui/dist` contains the new hashed assets and no stale generated files.

- [ ] **Step 3: Run the full project CI gate**

```bash
mise run ci
```

Expected: Go vet/build/test and dashboard lint/typecheck/test/build all pass.

- [ ] **Step 4: Run the complete smoke ceremony**

```bash
mise run ci:smoke
```

Expected: exit 0; summary includes `vrchat 24/24` and `smoke OK`.

- [ ] **Step 5: Browser-verify desktop and mobile placement**

Start the existing development stack with its documented `mise run dev` target. In Chromium, navigate to the VRChat provider detail page, force an operator start failure, and verify at 1440×900 and 390×844:

- exactly one alert appears inside the Operator session card;
- it follows the active credential/code/action controls;
- username remains editable while password/code is cleared;
- details and diagnostic lookup remain operable;
- no alert appears above the page heading.

Then exercise the mock's missing-`Secure` login response and verify operator setup proceeds to challenge or valid state instead of returning `upstream_temporarily_unavailable`.

- [ ] **Step 6: Update status documentation only after verification**

If `STATUS.md` claims diagnostics are already persisted or VRChat cookies require inbound `Secure`, correct those exact statements and add a terse v0.8 correction note. Do not create new documentation files.

- [ ] **Step 7: Commit generated assets and any verified status correction**

```bash
git add -A pkg/webui/dist STATUS.md
git diff --cached --check
git commit -m "build: refresh corrected dashboard bundle"
```

Skip `STATUS.md` if unchanged. If the build produces no tracked changes and no status correction is needed, skip this commit.

- [ ] **Step 8: Request code review and resolve every finding**

Use the `requesting-code-review` skill. Review the complete range from commit `1686f90b` through `HEAD`, address all Critical/Important findings, and rerun the affected focused checks.

- [ ] **Step 9: Re-run final evidence after review fixes**

```bash
mise run ci
mise run ci:smoke
git status --short
```

Expected: both gates exit 0; only the pre-existing untracked root `package-lock.json` may remain.

- [ ] **Step 10: Push corrected master**

```bash
git push origin master
```

Expected: remote `origin/master` advances to the verified correction commit without force-push.
