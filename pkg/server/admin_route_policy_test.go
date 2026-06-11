package server

// TestAdminMutationRoutesRequireSudo is a cross-cutting security regression
// test. It asserts that EVERY admin mutation route — whether registered via the
// raw-HTTP registerSudoOpHTTP or the typed-Huma registerSudoOp — returns HTTP
// 401 with body containing "sudo_required" when served with an admin session
// that carries no fresh sudo grant, confirming the sudo gate fires BEFORE any
// handler logic. Both styles route through the same consumeFreshSudo chokepoint.
//
// GUARD: every route registered via s.registerSudoOpHTTP OR registerSudoOp MUST
// appear in sudoGatedRoutes below. Adding a 🔐 admin mutation without adding it
// here is a security bug — the test will not catch an un-gated route it doesn't
// know about.

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

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

	// OIDC application management
	{method: "POST", path: "/api/prohibitorum/oidc-applications", body: `{"clientId":"x"}`},
	{method: "PUT", path: "/api/prohibitorum/oidc-applications/x", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/oidc-applications/rotate-secret", body: `{"clientId":"x"}`},
	{method: "POST", path: "/api/prohibitorum/oidc-applications/delete", body: `{"clientId":"x"}`},

	// SAML application management
	{method: "POST", path: "/api/prohibitorum/saml-applications", body: `{}`},
	{method: "PUT", path: "/api/prohibitorum/saml-applications/1", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/saml-applications/1/reingest-metadata", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/saml-applications/delete", body: `{"id":1}`},

	// Identity provider management
	{method: "POST", path: "/api/prohibitorum/identity-providers", body: `{}`},
	{method: "PUT", path: "/api/prohibitorum/identity-providers/x", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/identity-providers/rotate-secret", body: `{"slug":"x"}`},
	{method: "POST", path: "/api/prohibitorum/identity-providers/delete", body: `{"slug":"x"}`},

	// Account credential revoke — promoted to sudo (Task 9)
	{method: "POST", path: "/api/prohibitorum/accounts/credentials/delete", body: `{"accountId":1,"credentialId":1}`},

	// Per-account session revoke (admin, sudo-gated)
	{method: "POST", path: "/api/prohibitorum/accounts/1/sessions/revoke", body: `{"sessionId":"abc"}`},

	// Account/invitation lifecycle mutations — fresh-sudo via registerSudoOp
	// (typed Huma ops). UpdateAccount can escalate user→admin, so step-up matters.
	{method: "PUT", path: "/api/prohibitorum/accounts/1", body: `{"displayName":"x","role":"user"}`},
	{method: "POST", path: "/api/prohibitorum/accounts/delete", body: `{"id":1}`},
	{method: "POST", path: "/api/prohibitorum/accounts/revoke-sessions", body: `{"id":1}`},
	{method: "POST", path: "/api/prohibitorum/accounts/reissue-enrollment", body: `{"id":1}`},
	{method: "POST", path: "/api/prohibitorum/invitations", body: `{"role":"user"}`},
	{method: "POST", path: "/api/prohibitorum/invitations/revoke", body: `{"token":"x"}`},
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
