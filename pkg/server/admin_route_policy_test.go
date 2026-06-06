package server

// TestAdminMutationRoutesRequireSudo is a cross-cutting security regression
// test. It asserts that EVERY admin mutation route registered via
// registerSudoOpHTTP returns HTTP 401 with body containing "sudo_required"
// when served with an admin session that carries no fresh sudo grant —
// confirming the sudo gate fires BEFORE any handler logic.
//
// GUARD: every route registered via s.registerSudoOpHTTP MUST appear in
// sudoGatedRoutes below. Adding a 🔐 admin route without adding it here is a
// security bug — the test will not catch an un-gated route it doesn't know about.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/contract"
)

// sudoRoute is one entry in the table of sudo-gated admin mutations.
type sudoRoute struct {
	method string
	path   string
	body   string
}

// sudoGatedRoutes is the canonical list of every 🔐 admin mutation route.
// Maintain this list whenever s.registerSudoOpHTTP gains a new entry.
//
// GUARD: every route registered via s.registerSudoOpHTTP MUST appear here.
// Omitting a route is a security gap — this test won't catch a missing entry it
// doesn't know about. Cross-check against server.go registerOperations() when
// adding routes.
var sudoGatedRoutes = []sudoRoute{
	// Signing-key lifecycle
	{method: "POST", path: "/api/prohibitorum/signing-keys/generate", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/signing-keys/abc/activate", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/signing-keys/abc/retire", body: `{}`},

	// OIDC client management
	{method: "POST", path: "/api/prohibitorum/oidc-clients", body: `{"clientId":"x"}`},
	{method: "PUT", path: "/api/prohibitorum/oidc-clients/x", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/oidc-clients/rotate-secret", body: `{"clientId":"x"}`},
	{method: "POST", path: "/api/prohibitorum/oidc-clients/delete", body: `{"clientId":"x"}`},

	// SAML SP management
	{method: "POST", path: "/api/prohibitorum/saml-providers", body: `{}`},
	{method: "PUT", path: "/api/prohibitorum/saml-providers/1", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/saml-providers/1/reingest-metadata", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/saml-providers/delete", body: `{"id":1}`},

	// Upstream IdP management
	{method: "POST", path: "/api/prohibitorum/upstream-idps", body: `{}`},
	{method: "PUT", path: "/api/prohibitorum/upstream-idps/x", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/upstream-idps/rotate-secret", body: `{"slug":"x"}`},
	{method: "POST", path: "/api/prohibitorum/upstream-idps/delete", body: `{"slug":"x"}`},

	// Account credential revoke — promoted to sudo (Task 9)
	{method: "POST", path: "/api/prohibitorum/accounts/credentials/delete", body: `{"accountId":1,"credentialId":1}`},
}

// TestAdminMutationRoutesRequireSudo builds a minimal chi router (matching the
// real registration path via registerSudoOpHTTP) and asserts that each route in
// sudoGatedRoutes returns 401 + "sudo_required" when the session has no fresh
// sudo grant (SudoUntil is zero).
//
// Construction strategy: registerSudoOpHTTP requires only s.sessionStore to
// actually consume a sudo grant — but the reject path (no fresh sudo) runs
// entirely inside requireFreshSudo, which only reads the session from context
// and writes the error. So &Server{} is sufficient for all reject-path tests;
// the handler body is never reached, and no DB/KV dependencies are exercised.
func TestAdminMutationRoutesRequireSudo(t *testing.T) {
	admin := contract.AuthRequirement{Kind: contract.AuthAdmin}

	// Build a chi router with every sudo-gated route registered against a
	// sentinel handler. The sentinel must never be called — if it runs, the
	// sudo gate is absent or broken.
	s := &Server{}
	router := chi.NewRouter()

	sentinel := func(called *bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			*called = true
			w.WriteHeader(204)
		}
	}

	// Track per-route whether the sentinel fired.
	type routeEntry struct {
		sudoRoute
		called bool
	}
	entries := make([]routeEntry, len(sudoGatedRoutes))
	for i, sr := range sudoGatedRoutes {
		entries[i].sudoRoute = sr
		s.registerSudoOpHTTP(router, sr.method, sr.path, admin,
			sentinel(&entries[i].called))
	}

	sess := adminSession(time.Time{}) // zero SudoUntil = no fresh sudo

	for i := range entries {
		e := &entries[i]
		t.Run(e.method+" "+e.path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := reqWithSession(e.method, e.path, e.body, "", sess)
			router.ServeHTTP(rr, req)

			if e.called {
				t.Errorf("handler ran despite missing fresh sudo — route is not properly gated")
			}
			if rr.Code != 401 {
				t.Errorf("status = %d, want 401", rr.Code)
			}
			if !strings.Contains(rr.Body.String(), "sudo_required") {
				t.Errorf("body = %q, want sudo_required", rr.Body.String())
			}
		})
	}
}
