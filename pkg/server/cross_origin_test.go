package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
	sessstore "prohibitorum/pkg/session"
	"prohibitorum/pkg/weberr"
)

// routeWiringSigningKeyQueries hands the oidcop key cache an empty key set;
// embedding db.Querier keeps every other method a panic-on-use stub. The
// wiring test only exercises routing, not signing or DB access.
type routeWiringSigningKeyQueries struct {
	db.Querier
}

func (routeWiringSigningKeyQueries) ListPublishableSigningKeys(context.Context) ([]db.SigningKey, error) {
	return nil, nil
}

func (routeWiringSigningKeyQueries) GetOIDCClient(context.Context, string) (db.OidcClient, error) {
	return db.OidcClient{}, sql.ErrNoRows
}

func newCrossOriginTestRouter(publicOrigins []string) http.Handler {
	router := chi.NewRouter()
	installRequestGuardChain(router, publicOrigins)
	ok := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}
	router.Post("/api/prohibitorum/admin/settings", ok)
	router.Post("/oauth/token", ok)
	router.Post("/saml/sso", ok)
	router.Get("/api/prohibitorum/accounts", ok)
	return router
}

func crossOriginPost(t *testing.T, router http.Handler, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.Host = "id.example"
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestCrossOriginMWRejectsCrossSiteWrite(t *testing.T) {
	router := newCrossOriginTestRouter([]string{"https://id.example"})

	rec := crossOriginPost(t, router, "/api/prohibitorum/admin/settings", map[string]string{
		"Sec-Fetch-Site": "cross-site",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	var body struct {
		Code      string `json:"code"`
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if body.Code != "cross_origin_request" {
		t.Errorf("code = %q, want cross_origin_request", body.Code)
	}
	if body.RequestID == "" || body.RequestID != rec.Header().Get("X-Request-ID") {
		t.Errorf("requestId = %q, want the X-Request-ID header value", body.RequestID)
	}
}

func TestCrossOriginMWRejectsSameSiteSiblingSubdomain(t *testing.T) {
	router := newCrossOriginTestRouter([]string{"https://id.example"})

	rec := crossOriginPost(t, router, "/api/prohibitorum/admin/settings", map[string]string{
		"Sec-Fetch-Site": "same-site",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (SameSite=Lax cookies still ride sibling-subdomain requests)", rec.Code)
	}
}

func TestCrossOriginMWAllowsSameOriginWrite(t *testing.T) {
	router := newCrossOriginTestRouter([]string{"https://id.example"})

	rec := crossOriginPost(t, router, "/api/prohibitorum/admin/settings", map[string]string{
		"Sec-Fetch-Site": "same-origin",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func TestCrossOriginMWTrustedOriginWithDifferentHost(t *testing.T) {
	// A reverse proxy that rewrites Host breaks the Origin==Host comparison;
	// a registered public origin must still be accepted.
	router := newCrossOriginTestRouter([]string{"https://public.example"})

	rec := crossOriginPost(t, router, "/api/prohibitorum/admin/settings", map[string]string{
		"Origin": "https://public.example",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

// The OIDC connection-test endpoints used to wrap their handlers in a local
// CrossOriginProtection; the global middleware replaces it, so the same
// guarantee is asserted here through the full route table.
func TestCrossOriginMWStillGuardsOIDCDiagnostics(t *testing.T) {
	router := chi.NewMux()
	router.Use(weberr.RequestID)
	router.Use(newCrossOriginMW(nil))
	s := &Server{router: router, rateLimiter: authn.NewRateLimiter(), providerStateQueriesOverride: &fakeProviderStateQueries{}}
	s.api = humachi.New(router, humaConfig())
	registerSecurityScheme(s.api, sessstore.SessionCookieName)
	s.registerOperations()

	for _, suffix := range []string{"/tests", "/tests/id/complete"} {
		req := httptest.NewRequest(http.MethodPost, "https://id.example/api/prohibitorum/identity-providers/corp"+suffix, nil)
		req.Header.Set("Origin", "https://attacker.example")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req = req.WithContext(authn.WithSession(req.Context(), &authn.Session{Account: &db.Account{ID: 7, Role: "admin"}, Data: &authn.SessionData{AccountID: 7, SessionID: "session"}}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("cross-origin %s status = %d, want 403", suffix, rec.Code)
		}
		var body struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Code != "cross_origin_request" {
			t.Fatalf("cross-origin %s body = %s", suffix, rec.Body.String())
		}
	}
}

func TestCrossOriginMWAllowsNonBrowserRequests(t *testing.T) {
	router := newCrossOriginTestRouter([]string{"https://id.example"})

	rec := crossOriginPost(t, router, "/api/prohibitorum/admin/settings", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (CLI/PAT/server-to-server carry neither header)", rec.Code)
	}
}

func TestCrossOriginMWBypassesProtocolEndpoints(t *testing.T) {
	router := newCrossOriginTestRouter([]string{"https://id.example"})

	for _, path := range []string{"/oauth/token", "/saml/sso"} {
		rec := crossOriginPost(t, router, path, map[string]string{
			"Sec-Fetch-Site": "cross-site",
		})
		if rec.Code != http.StatusNoContent {
			t.Errorf("POST %s status = %d, want 204", path, rec.Code)
		}
	}
}

func TestCrossOriginMWAllowsCrossSiteGet(t *testing.T) {
	router := newCrossOriginTestRouter([]string{"https://id.example"})

	req := httptest.NewRequest(http.MethodGet, "/api/prohibitorum/accounts", nil)
	req.Host = "id.example"
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (safe methods are always allowed)", rec.Code)
	}
}

// TestRouteTableCORSAndBypassWiring asserts the real registerOperations()
// wiring: the six OP machine endpoints answer preflights and carry
// Access-Control-Allow-Origin on actual responses, the browser-redirect and
// management surfaces carry none, and the cross-origin protection bypasses
// exactly the six POST patterns the protocol needs. It fails if the CORS
// wrappers, OPTIONS routes, or the global middleware line are removed from
// server.go.
func TestRouteTableCORSAndBypassWiring(t *testing.T) {
	// The wiring under test is newRouteTable — the shared construction
	// NewServer and NewHuma both call — so removing the guard chain or a
	// CORS wrapper from production code fails here.
	router, _ := newRouteTable(nil, routeWiringSigningKeyQueries{})

	do := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Host = "id.example"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	corsEndpoints := []struct{ path, methods string }{
		{"/.well-known/openid-configuration", "GET"},
		{"/oauth/jwks", "GET"},
		{"/oauth/token", "POST"},
		{"/oauth/revoke", "POST"},
		{"/oauth/introspect", "POST"},
		{"/oauth/userinfo", "GET, POST"},
	}
	for _, ep := range corsEndpoints {
		preflight := do(http.MethodOptions, ep.path)
		if preflight.Code != http.StatusNoContent {
			t.Errorf("OPTIONS %s: status %d, want 204", ep.path, preflight.Code)
		}
		if got := preflight.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("OPTIONS %s: Access-Control-Allow-Origin %q, want *", ep.path, got)
		}
		if got := preflight.Header().Get("Access-Control-Allow-Methods"); got != ep.methods {
			t.Errorf("OPTIONS %s: Access-Control-Allow-Methods %q, want %q", ep.path, got, ep.methods)
		}
		// An actual request — any status the real handler produces — must
		// still carry the CORS header, proving the CORS wrapper is applied.
		method := strings.Split(ep.methods, ",")[0]
		actual := do(method, ep.path)
		if got := actual.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("%s %s: Access-Control-Allow-Origin %q, want * (CORS wrapper missing?)", method, ep.path, got)
		}
	}

	// Surfaces that must NOT advertise CORS: the authorize redirect and the
	// management API.
	for _, path := range []string{"/oauth/authorize", "/api/prohibitorum/me"} {
		rec := do(http.MethodGet, path)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("GET %s: unexpected Access-Control-Allow-Origin %q", path, got)
		}
	}

	// The six bypassed POST patterns pass the global cross-origin protection
	// even with Sec-Fetch-Site: cross-site; a cookie-authenticated management
	// write with the same header is refused.
	for _, path := range []string{"/oauth/token", "/oauth/revoke", "/oauth/introspect", "/oauth/userinfo", "/saml/sso", "/saml/slo"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Host = "id.example"
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code == http.StatusForbidden {
			t.Errorf("POST %s with Sec-Fetch-Site: cross-site: got 403, want the bypass to admit it (body=%s)", path, rec.Body.String())
		}
	}
	apiReq := httptest.NewRequest(http.MethodPut, "/api/prohibitorum/me/avatar/selection", nil)
	apiReq.Host = "id.example"
	apiReq.Header.Set("Sec-Fetch-Site", "cross-site")
	apiRec := httptest.NewRecorder()
	router.ServeHTTP(apiRec, apiReq)
	if apiRec.Code != http.StatusForbidden {
		t.Errorf("cross-site PUT /api/prohibitorum/...: status %d, want 403 (global middleware missing?)", apiRec.Code)
	}
}
