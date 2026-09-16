package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"prohibitorum/cmd/smoke/mockop"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

func TestOIDCDiagnosticRoutesRejectNonAdmins(t *testing.T) {
	for _, path := range []struct{ method, path string }{{"GET", "/api/prohibitorum/identity-providers/corp/effective-config"}, {"POST", "/api/prohibitorum/identity-providers/corp/tests"}, {"GET", "/api/prohibitorum/identity-providers/corp/tests/id"}, {"POST", "/api/prohibitorum/identity-providers/corp/tests/id/complete"}} {
		for _, role := range []string{"", "user", "app_manager"} {
			t.Run(path.path+role, func(t *testing.T) {
				router := chi.NewRouter()
				s := &Server{router: router}
				s.api = humachi.New(router, humaConfig())
				s.registerOperations()
				req := httptest.NewRequest(path.method, path.path, nil)
				if role != "" {
					req = req.WithContext(authn.WithSession(req.Context(), &authn.Session{Account: &db.Account{ID: 7, Role: role}, Data: &authn.SessionData{AccountID: 7, SessionID: "session"}}))
				}
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				if rec.Code != 401 && rec.Code != 403 {
					t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
				}
			})
		}
	}
}

func TestOIDCDiagnosticPOSTRejectsCrossOrigin(t *testing.T) {
	for _, suffix := range []string{"/tests", "/tests/id/complete"} {
		router := chi.NewRouter()
		s := &Server{router: router}
		s.api = humachi.New(router, humaConfig())
		s.registerOperations()
		req := httptest.NewRequest("POST", "https://rp.example/api/prohibitorum/identity-providers/corp"+suffix, nil)
		req.Header.Set("Origin", "https://attacker.example")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req = req.WithContext(authn.WithSession(req.Context(), &authn.Session{Account: &db.Account{ID: 7, Role: "admin"}, Data: &authn.SessionData{AccountID: 7, SessionID: "session"}}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Fatalf("cross-origin %s status=%d", suffix, rec.Code)
		}
	}
}

func TestOIDCDiagnosticHandlersRoundTripWithoutAccountsOrSessions(t *testing.T) {
	op, err := mockop.New("")
	if err != nil {
		t.Fatal(err)
	}
	var tokenCalls atomic.Int32
	var discoveries atomic.Int32
	var discoveryFailure atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			tokenCalls.Add(1)
		}
		if r.URL.Path == "/.well-known/openid-configuration" {
			discoveries.Add(1)
			if discoveryFailure.Load() {
				http.Error(w, "upstream-hidden-secret", 500)
				return
			}
		}
		op.Routes().ServeHTTP(w, r)
	}))
	defer upstream.Close()
	op.SetBase(upstream.URL)
	op.SetClaims("subject", "upstream@example.com", true, "upstream", "Upstream")
	raw := strings.Replace(exactOIDCConfig, "https://issuer.example", upstream.URL, 1)
	raw = strings.Replace(raw, `"allowPrivateNetwork":false`, `"allowPrivateNetwork":true`, 1)
	raw = strings.Replace(raw, `"tokenAuthMethod":"discovery"`, `"tokenAuthMethod":"none"`, 1)
	query := &fakeProviderStateQueries{row: db.UpstreamIdp{ID: 7, Slug: "corp", Protocol: "oidc", ProviderConfig: []byte(raw), SecretStatus: "unconfigured"}}
	s := &Server{config: &configx.Config{PublicOrigins: []string{"https://rp.example"}}, kvStore: kv.NewMemoryStore(), rateLimiter: authn.NewRateLimiter(), providerStateQueriesOverride: query}
	router := chi.NewRouter()
	admin := contract.AuthRequirement{Kind: contract.AuthAdmin}
	registerOpHTTP(router, "GET", "/api/prohibitorum/identity-providers/{slug}/effective-config", admin, s.handleOIDCEffectiveConfigHTTP)
	s.registerAdminBodyOpHTTP(router, "POST", "/api/prohibitorum/identity-providers/{slug}/tests", admin, s.handleOIDCTestStartHTTP)
	registerOpHTTP(router, "GET", "/api/prohibitorum/auth/federation/{slug}/test/callback", contract.AuthRequirement{Kind: contract.AuthPublic}, s.handleOIDCTestCallbackHTTP)
	registerOpHTTP(router, "GET", "/api/prohibitorum/identity-providers/{slug}/tests/{id}", admin, s.handleOIDCTestGetHTTP)
	s.registerAdminBodyOpHTTP(router, "POST", "/api/prohibitorum/identity-providers/{slug}/tests/{id}/complete", admin, s.handleOIDCTestCompleteHTTP)
	// Nil queries, federationService and sessionStore ensure this cannot resolve
	// business accounts or issue a new local session.
	var cookie *http.Cookie
	request := func(method, path, body string, session bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if session {
			req = req.WithContext(authn.WithSession(req.Context(), &authn.Session{Account: &db.Account{ID: 7, Role: "admin"}, Data: &authn.SessionData{AccountID: 7, SessionID: "admin-session"}}))
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	for i := 0; i < 2; i++ {
		rec := request("GET", "/api/prohibitorum/identity-providers/corp/effective-config", "", true)
		if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("effective %d: %s", rec.Code, rec.Body.String())
		}
	}
	if discoveries.Load() != 2 {
		t.Fatal("effective config did not fetch again")
	}
	discoveryFailure.Store(true)
	for _, requestInfo := range []struct{ method, path string }{{"GET", "/effective-config"}, {"POST", "/tests"}} {
		failure := request(requestInfo.method, "/api/prohibitorum/identity-providers/corp"+requestInfo.path, "", true)
		if failure.Code != 503 || !strings.Contains(failure.Body.String(), "discovery_failed") || !strings.Contains(failure.Body.String(), "details") || strings.Contains(failure.Body.String(), "upstream-hidden-secret") {
			t.Fatalf("discovery failure %d: %s", failure.Code, failure.Body.String())
		}
	}
	discoveryFailure.Store(false)
	bad := request("POST", "/api/prohibitorum/identity-providers/corp/tests", `{"code":"not-accepted"}`, true)
	if bad.Code != 400 {
		t.Fatal("unexpected body accepted")
	}
	rec := request("POST", "/api/prohibitorum/identity-providers/corp/tests", "", true)
	if rec.Code != 200 {
		t.Fatalf("start %d: %s", rec.Code, rec.Body.String())
	}
	var start struct{ ID, AuthorizationURL string }
	if err := json.Unmarshal(rec.Body.Bytes(), &start); err != nil {
		t.Fatal(err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != oidcTestCookieName || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookies=%+v", cookies)
	}
	cookie = cookies[0]
	browser := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := browser.Get(start.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	callback, err := res.Location()
	if err != nil {
		t.Fatal(err)
	}
	rec = request("GET", callback.RequestURI(), "", false)
	if rec.Code != 302 || tokenCalls.Load() != 0 {
		t.Fatalf("callback %d: %s", rec.Code, rec.Body.String())
	}
	destination, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if len(destination.Query()) != 1 || destination.Query().Get("test") != start.ID || strings.Contains(destination.String(), "code=") {
		t.Fatal("callback exposed code")
	}
	rec = request("POST", "/api/prohibitorum/identity-providers/corp/tests/"+start.ID+"/complete", "", true)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"succeeded"`) {
		t.Fatalf("complete %d: %s", rec.Code, rec.Body.String())
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("test issued local session")
	}
	rec = request("POST", "/api/prohibitorum/identity-providers/corp/tests/"+start.ID+"/complete", "", true)
	if rec.Code != 200 || tokenCalls.Load() != 1 {
		t.Fatal("complete replayed exchange")
	}
	rec = request("GET", callback.RequestURI(), "", false)
	if rec.Code == 302 {
		t.Fatal("callback replay accepted")
	}
	rec = request("GET", callback.RequestURI()+"&state=duplicate", "", false)
	if rec.Code != 400 {
		t.Fatal("duplicate state accepted")
	}
}
