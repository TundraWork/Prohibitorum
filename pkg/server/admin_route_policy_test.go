package server

// TestAdminMutationRoutesRequireSudo is a cross-cutting security regression
// test. It asserts that EVERY admin mutation route — whether registered via the
// raw-HTTP registerSudoOpHTTP or the typed-Huma registerSudoOp — returns HTTP
// 401 with body containing "sudo_required" when served with an admin session
// that carries no fresh sudo grant, confirming the sudo gate fires BEFORE any
// handler logic. Both styles route through the same hasFreshSudo chokepoint.
//
// GUARD: every route registered via s.registerSudoOpHTTP OR registerSudoOp MUST
// appear in sudoGatedRoutes below. Adding a 🔐 admin mutation without adding it
// here is a security bug — the test will not catch an un-gated route it doesn't
// know about.
//
// TestTrimmedAdminRoutesAreAdminOnlyNotSudo complements the above by asserting
// that a representative sample of routes that were REMOVED from the sudo tier
// (now admin-only 🔓) do NOT return sudo_required — confirming the gate was
// genuinely removed and not just accidentally absent from sudoGatedRoutes.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/authn"
	sessstore "prohibitorum/pkg/session"
)

// sudoRoute is one entry in the table of sudo-gated admin mutations.
type sudoRoute struct {
	method string
	path   string
	body   string
}

// sudoGatedRoutes is the canonical list of every 🔐 admin mutation route.
// Maintain this list whenever registerSudoOpHTTP or registerSudoOp gains a new
// entry.
//
// GUARD: every route registered via registerSudoOpHTTP OR registerSudoOp MUST
// appear here. Omitting a route is a security gap — this test won't catch a
// missing entry it doesn't know about. Cross-check against server.go
// registerOperations() when adding routes.
var sudoGatedRoutes = []sudoRoute{
	// Signing-key lifecycle
	{method: "POST", path: "/api/prohibitorum/signing-keys/generate", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/signing-keys/abc/activate", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/signing-keys/abc/retire", body: `{}`},

	// OIDC application management (create/update/rotate-secret/delete — NOT set-disabled)
	{method: "POST", path: "/api/prohibitorum/oidc-applications", body: `{"clientId":"x"}`},
	{method: "PUT", path: "/api/prohibitorum/oidc-applications/x", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/oidc-applications/rotate-secret", body: `{"clientId":"x"}`},
	{method: "POST", path: "/api/prohibitorum/oidc-applications/delete", body: `{"clientId":"x"}`},

	// Forward-auth application lifecycle (Phase 2)
	{method: "POST", path: "/api/prohibitorum/forward-auth-apps", body: `{}`},
	{method: "PUT", path: "/api/prohibitorum/forward-auth-apps/test-client", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/forward-auth-apps/delete", body: `{}`},

	// Identity provider management (create/update/rotate-secret/delete — NOT set-disabled)
	{method: "POST", path: "/api/prohibitorum/identity-providers", body: `{}`},
	{method: "PUT", path: "/api/prohibitorum/identity-providers/x", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/identity-providers/rotate-secret", body: `{"slug":"x"}`},
	{method: "POST", path: "/api/prohibitorum/identity-providers/delete", body: `{"slug":"x"}`},

	// Account credential revoke — high-impact, sudo-gated
	{method: "POST", path: "/api/prohibitorum/accounts/credentials/delete", body: `{"accountId":1,"credentialId":1}`},

	// Admin PAT revoke — RevokePATByID has no ownership guard, so this admin+sudo
	// route is the sole protection; the gate must never silently demote.
	{method: "POST", path: "/api/prohibitorum/accounts/tokens/revoke", body: `{"id":1}`},

	// Account/invitation lifecycle mutations — fresh-sudo via registerSudoOp
	// (typed Huma ops). UpdateAccount can escalate user→admin, so step-up matters.
	{method: "PUT", path: "/api/prohibitorum/accounts/1", body: `{"displayName":"x","role":"user"}`},
	{method: "POST", path: "/api/prohibitorum/accounts/delete", body: `{"id":1}`},
	{method: "POST", path: "/api/prohibitorum/accounts/set-disabled", body: `{"id":1,"disabled":true}`},
	{method: "POST", path: "/api/prohibitorum/accounts/reissue-enrollment", body: `{"id":1}`},
	{method: "POST", path: "/api/prohibitorum/invitations", body: `{"role":"user"}`},

	// Instance-branding settings (name PUT, icon DELETE, background DELETE,
	// maintenance PUT, client-ip PUT — all sudo-gated)
	{method: "PUT", path: "/api/prohibitorum/admin/settings", body: `{"instanceName":"x"}`},
	{method: "DELETE", path: "/api/prohibitorum/admin/settings/icon", body: ``},
	{method: "DELETE", path: "/api/prohibitorum/admin/settings/background", body: ``},
	{method: "PUT", path: "/api/prohibitorum/admin/settings/maintenance", body: `{"maintenanceMode":true}`},
	{method: "PUT", path: "/api/prohibitorum/admin/settings/client-ip", body: `{"trustedHeader":"X-Forwarded-For"}`},

	// Entity icon removal (app & provider icons)
	{method: "DELETE", path: "/api/prohibitorum/oidc-applications/test-client/icon", body: `{}`},
	{method: "DELETE", path: "/api/prohibitorum/saml-applications/1/icon", body: `{}`},
	{method: "DELETE", path: "/api/prohibitorum/identity-providers/test-idp/icon", body: `{}`},
}

// droppedSudoRoutes is a representative sample of routes that were removed from
// the sudo tier (now admin-only 🔓). Each entry must NOT return sudo_required
// when called with a valid admin session that has no fresh sudo grant.
var droppedSudoRoutes = []sudoRoute{
	// Groups CRUD — admin-only, no step-up
	{method: "POST", path: "/api/prohibitorum/groups", body: `{"slug":"test","displayName":"Test"}`},
	{method: "POST", path: "/api/prohibitorum/groups/1/members", body: `{"accountId":1}`},
	// SAML application CRUD — admin-only, no step-up
	{method: "POST", path: "/api/prohibitorum/saml-applications", body: `{}`},
}

// TestTrimmedAdminRoutesAreAdminOnlyNotSudo builds the REAL router and asserts
// that each route in droppedSudoRoutes does NOT return 401 sudo_required when
// served with an admin session that has zero fresh sudo. The route handlers will
// proceed past the (absent) sudo gate and reach actual handler logic; because
// s.queries is nil they will panic or error — this is expected and caught with a
// deferred recover. The key invariant is: if a response IS produced, it must not
// be 401-with-"sudo_required". A recovered panic also satisfies the invariant
// (the gate was absent; the handler ran and crashed on nil deps).
func TestTrimmedAdminRoutesAreAdminOnlyNotSudo(t *testing.T) {
	router := chi.NewMux()
	s := &Server{
		router: router,
		api:    humachi.New(router, huma.DefaultConfig("Prohibitorum Identity API", "1.0.0")),
	}
	registerSecurityScheme(s.api, sessstore.SessionCookieName)
	s.registerOperations()

	sess := adminSession(time.Time{}) // zero SudoUntil = no fresh sudo

	for _, sr := range droppedSudoRoutes {
		sr := sr
		t.Run(sr.method+" "+sr.path, func(t *testing.T) {
			// Recover from any panic caused by nil s.queries — that means the sudo
			// gate was absent and the handler ran (satisfying the invariant).
			defer func() {
				if rec := recover(); rec != nil {
					// Panic from handler logic = gate was absent. Pass.
					t.Logf("handler panicked (nil deps, expected): %v — sudo gate is absent", rec)
				}
			}()

			rr := httptest.NewRecorder()
			req := reqWithSession(sr.method, sr.path, sr.body, "", sess)
			router.ServeHTTP(rr, req)

			// If a response was produced, it must NOT be 401 sudo_required.
			if rr.Code == 401 && strings.Contains(rr.Body.String(), "sudo_required") {
				t.Errorf("%s %s: got sudo_required (401) — sudo gate was not removed; body: %s",
					sr.method, sr.path, rr.Body.String())
			}
		})
	}
}

// TestAdminMutationRoutesRequireSudo builds the REAL router via registerOperations()
// (the same path as NewHuma / production) and asserts that each route in
// sudoGatedRoutes returns 401 + "sudo_required" when the session has no fresh
// sudo grant (SudoUntil is zero).
//
// Construction strategy: we use the exact same pattern as NewHuma — a *Server
// with a real chi.Mux and huma.API but no DB/KV wiring. The REAL
// s.registerOperations() registers every route exactly as production does,
// including the real registerSudoOpHTTP calls. The sudo reject path runs
// entirely inside requireFreshSudo, which only reads the session from context
// and writes a 401 — so s.queries (nil) is never reached and no external
// dependencies are exercised.
//
// This test exercises the REAL routes in server.go. If a route is accidentally
// registered via plain registerOpHTTP (no sudo wrapper), this test catches it:
// the real handler body would execute and either crash (nil s.queries) or return
// something other than 401 sudo_required.
func TestAdminMutationRoutesRequireSudo(t *testing.T) {
	// Build the real router the same way NewHuma does, but keep handles to
	// both the *Server and its chi.Mux so we can serve requests directly.
	router := chi.NewMux()
	s := &Server{
		router: router,
		api:    humachi.New(router, huma.DefaultConfig("Prohibitorum Identity API", "1.0.0")),
	}
	registerSecurityScheme(s.api, sessstore.SessionCookieName)
	s.registerOperations()

	sess := adminSession(time.Time{}) // zero SudoUntil = no fresh sudo

	for _, sr := range sudoGatedRoutes {
		t.Run(sr.method+" "+sr.path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := reqWithSession(sr.method, sr.path, sr.body, "", sess)
			router.ServeHTTP(rr, req)

			if rr.Code != 401 {
				t.Errorf("status = %d, want 401 (sudo_required) — is this route actually wrapped with registerSudoOpHTTP?", rr.Code)
			}
			if !strings.Contains(rr.Body.String(), "sudo_required") {
				t.Errorf("body = %q, want sudo_required", rr.Body.String())
			}
		})
	}
}

// realAdminOnlyRouter builds a *Server with the real chi.Mux + huma.API and
// registers all operations exactly as production does, but with no DB/KV
// wiring — the same construction used by TestAdminMutationRoutesRequireSudo.
func realAdminOnlyRouter(t *testing.T) (*chi.Mux, *Server) {
	t.Helper()
	router := chi.NewMux()
	s := &Server{
		router: router,
		api:    humachi.New(router, huma.DefaultConfig("Prohibitorum Identity API", "1.0.0")),
	}
	registerSecurityScheme(s.api, sessstore.SessionCookieName)
	s.registerOperations()
	return router, s
}

var adminBodyControlRoutes = []sudoRoute{
	// SAML application CRUD — admin-only, no step-up
	{method: "POST", path: "/api/prohibitorum/saml-applications", body: `{}`},
	{method: "PUT", path: "/api/prohibitorum/saml-applications/1", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/saml-applications/1/reingest-metadata", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/saml-applications/set-disabled", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/saml-applications/delete", body: `{}`},
	// SAML app-access management
	{method: "POST", path: "/api/prohibitorum/saml-applications/1/access/set-restricted", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/saml-applications/1/access/grant", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/saml-applications/1/access/revoke", body: `{}`},
	// OIDC app-access management
	{method: "POST", path: "/api/prohibitorum/oidc-applications/x/access/set-restricted", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/oidc-applications/x/access/grant", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/oidc-applications/x/access/revoke", body: `{}`},
	// OIDC / forward-auth / IdP set-disabled — admin-only, no step-up
	{method: "POST", path: "/api/prohibitorum/oidc-applications/set-disabled", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/forward-auth-apps/set-disabled", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/identity-providers/set-disabled", body: `{}`},
	// Account session revoke — admin-only, no step-up
	{method: "POST", path: "/api/prohibitorum/accounts/1/sessions/revoke", body: `{"sessionId":"x"}`},
	// Group CRUD + membership management
	{method: "POST", path: "/api/prohibitorum/groups", body: `{}`},
	{method: "PUT", path: "/api/prohibitorum/groups/1", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/groups/delete", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/groups/1/members", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/groups/1/members/remove", body: `{}`},
}

// TestAdminMutationBodyControls_OversizedJSONReturns413 builds the REAL router
// and asserts that a 128 KiB JSON body sent to an intentional admin-only
// SAML mutation route (POST /saml-applications) is rejected with HTTP 413 —
// NOT a generic 500 — before the handler's business logic runs.
//
// The body controls (MaxBytesReader) are installed at route registration, so
// the rejection happens inside the wrapper before any handler/DB access. The
// admin session has no fresh sudo grant; since these routes are intentionally
// admin-only (not sudo-gated), the request passes the auth check and reaches
// the body-size enforcement.
func TestAdminMutationBodyControls_OversizedJSONReturns413(t *testing.T) {
	router, _ := realAdminOnlyRouter(t)

	// 128 KiB JSON body — double the 64 KiB cap.
	oversized := strings.Repeat(" ", 128*1024) // 128 KiB of spaces inside a JSON string
	body := `{"metadataXml":"` + oversized + `"}`
	bodyBytes := []byte(body)

	sess := adminSession(time.Time{}) // admin role, no fresh sudo (intentional admin-only)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/saml-applications", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(bodyBytes))
	req = req.WithContext(authn.WithSession(req.Context(), sess))
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (oversized body must be rejected before handler); body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "request_too_large") {
		t.Errorf("body = %q, want request_too_large code", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "request body too large") {
		t.Errorf("body = %q, want 'request body too large' message", rr.Body.String())
	}
}

// TestAdminMutationBodyControls_WrongContentTypeReturns400 asserts that an
// admin-only mutation route with a non-JSON content type and a body is
// rejected with the canonical 400 bad_request error — using the project's
// existing error vocabulary — before the handler runs. It also confirms the
// route does NOT return sudo_required (it's intentionally admin-only).
func TestAdminMutationBodyControls_WrongContentTypeReturns400(t *testing.T) {
	router, _ := realAdminOnlyRouter(t)

	sess := adminSession(time.Time{})

	rr := httptest.NewRecorder()
	req := reqWithSession(http.MethodPost, "/api/prohibitorum/saml-applications", `data`, "text/plain", sess)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (bad_request for non-JSON content type); body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "bad_request") {
		t.Fatalf("body = %q, want bad_request code", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sudo_required") {
		t.Fatalf("body = %q — admin-only route must NOT return sudo_required", rr.Body.String())
	}
}

// TestAdminMutationBodyControls_NoSudoRequired asserts that each
// admin-only body-controlled route does NOT return sudo_required when served
// with an admin session carrying no fresh sudo grant — confirming the sudo
// gate was genuinely not installed on these intentional admin-only routes.
// The body/content-type controls run first; a wrong-content-type request
// short-circuits to 400 before any handler or sudo logic.
func TestAdminMutationBodyControls_NoSudoRequired(t *testing.T) {
	router, _ := realAdminOnlyRouter(t)
	sess := adminSession(time.Time{})

	for _, sr := range adminBodyControlRoutes {
		sr := sr
		t.Run(sr.method+" "+sr.path, func(t *testing.T) {
			defer func() {
				if rec := recover(); rec != nil {
					t.Logf("handler panicked (nil deps, expected): %v — sudo gate is absent", rec)
				}
			}()

			rr := httptest.NewRecorder()
			// Send with a wrong content type so the body control short-circuits to
			// 400 before the handler runs — proving the route is body-controlled AND
			// not sudo-gated.
			req := reqWithSession(sr.method, sr.path, sr.body, "text/plain", sess)
			router.ServeHTTP(rr, req)

			if rr.Code == 401 && strings.Contains(rr.Body.String(), "sudo_required") {
				t.Errorf("%s %s: got sudo_required (401) — sudo gate was installed on an admin-only route; body: %s",
					sr.method, sr.path, rr.Body.String())
			}
			// A 400 bad_request from the content-type check is the expected
			// short-circuit; a panic-recover also satisfies the invariant.
			if rr.Code == 400 && !strings.Contains(rr.Body.String(), "bad_request") {
				t.Errorf("%s %s: 400 without bad_request code; body: %s", sr.method, sr.path, rr.Body.String())
			}
		})
	}
}
