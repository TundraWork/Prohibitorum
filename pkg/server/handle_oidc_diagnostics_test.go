package server

import (
	"context"
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
	"prohibitorum/pkg/credential/pat"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

func TestOIDCDiagnosticRoutesRejectNonAdmins(t *testing.T) {
	for _, path := range []struct{ method, path string }{{"GET", "/api/prohibitorum/identity-providers/corp/effective-config"}, {"POST", "/api/prohibitorum/identity-providers/corp/tests"}, {"GET", "/api/prohibitorum/identity-providers/corp/tests/id"}, {"POST", "/api/prohibitorum/identity-providers/corp/tests/id/complete"}} {
		for _, role := range []string{"", "user"} {
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

func TestOIDCDiagnosticHandlersRoundTripWithoutAccountsOrSessions(t *testing.T) {
	for _, responseType := range []string{"code", "id_token"} {
		t.Run(responseType, func(t *testing.T) { oidcDiagnosticRoundTrip(t, responseType) })
	}
}

// oidcDiagnosticRoundTrip runs one public client's connection test end to
// end. With id_token the upstream answers in the fragment, so the callback is
// reached through the relay page and no token request is made.
func oidcDiagnosticRoundTrip(t *testing.T, responseType string) {
	implicit := responseType == "id_token"
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
	raw = strings.Replace(raw, `"tokenAuthMethod":"discovery"`, `"tokenAuthMethod":"none","responseType":"`+responseType+`"`, 1)
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
		if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" || !strings.Contains(rec.Body.String(), `"responseType":{"source":"manual","value":"`+responseType+`"}`) {
			t.Fatalf("effective %d: %s", rec.Code, rec.Body.String())
		}
	}
	if discoveries.Load() != 2 {
		t.Fatal("effective config did not fetch again")
	}
	// The effective configuration is a plain admin read, so a full-access PAT
	// reaches it; the connection tests stay bound to the browser session.
	patReq := httptest.NewRequest("GET", "/api/prohibitorum/identity-providers/corp/effective-config", nil)
	patReq = patReq.WithContext(authn.WithPATSession(patReq.Context(), &authn.Session{Account: &db.Account{ID: 7, Role: "admin"}, PAT: &authn.PATPrincipal{ID: 3, Access: pat.AccessFull}}))
	patRec := httptest.NewRecorder()
	router.ServeHTTP(patRec, patReq)
	if patRec.Code != 200 {
		t.Fatalf("effective config via PAT %d: %s", patRec.Code, patRec.Body.String())
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
	if implicit {
		// The browser's first request has no fragment and so no query; the
		// relay page then loads the callback with the fragment as the query.
		fragment, err := url.ParseQuery(callback.Fragment)
		if err != nil || fragment.Get("id_token") == "" || callback.RawQuery != "" {
			t.Fatalf("implicit callback = %s", callback)
		}
		callback.Fragment = ""
		rec = request("GET", callback.RequestURI(), "", false)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "location.hash") {
			t.Fatalf("fragment callback %d: %s", rec.Code, rec.Body.String())
		}
		callback.RawQuery = fragment.Encode()
	}
	// Parameters the provider adds on its own must not fail the callback.
	rec = request("GET", callback.RequestURI()+"&scope=openid+email&authuser=0&prompt=consent", "", false)
	if rec.Code != 302 || tokenCalls.Load() != 0 {
		t.Fatalf("callback %d: %s", rec.Code, rec.Body.String())
	}
	destination, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if len(destination.Query()) != 1 || destination.Query().Get("test") != start.ID || strings.Contains(destination.String(), "code=") || strings.Contains(destination.String(), "id_token=") {
		t.Fatal("callback exposed code or id_token")
	}
	rec = request("POST", "/api/prohibitorum/identity-providers/corp/tests/"+start.ID+"/complete", "", true)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"succeeded"`) {
		t.Fatalf("complete %d: %s", rec.Code, rec.Body.String())
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("test issued local session")
	}
	wantTokenCalls := int32(1)
	if implicit {
		wantTokenCalls = 0
	}
	rec = request("POST", "/api/prohibitorum/identity-providers/corp/tests/"+start.ID+"/complete", "", true)
	if rec.Code != 200 || tokenCalls.Load() != wantTokenCalls {
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

// TestOIDCDiagnosticErrorCallbackKeepsDescription starts a second test and
// delivers the provider's error redirect to the public callback; the result
// the dashboard reads carries the provider's error_description, while the
// management-page redirect still exposes only the opaque test ID.
func TestOIDCDiagnosticErrorCallbackKeepsDescription(t *testing.T) {
	op, err := mockop.New("")
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(op.Routes())
	defer upstream.Close()
	op.SetBase(upstream.URL)
	raw := strings.Replace(exactOIDCConfig, "https://issuer.example", upstream.URL, 1)
	raw = strings.Replace(raw, `"allowPrivateNetwork":false`, `"allowPrivateNetwork":true`, 1)
	raw = strings.Replace(raw, `"tokenAuthMethod":"discovery"`, `"tokenAuthMethod":"none"`, 1)
	query := &fakeProviderStateQueries{row: db.UpstreamIdp{ID: 7, Slug: "corp", Protocol: "oidc", ProviderConfig: []byte(raw), SecretStatus: "unconfigured"}}
	s := &Server{config: &configx.Config{PublicOrigins: []string{"https://rp.example"}}, kvStore: kv.NewMemoryStore(), rateLimiter: authn.NewRateLimiter(), providerStateQueriesOverride: query}
	router := chi.NewRouter()
	admin := contract.AuthRequirement{Kind: contract.AuthAdmin}
	s.registerAdminBodyOpHTTP(router, "POST", "/api/prohibitorum/identity-providers/{slug}/tests", admin, s.handleOIDCTestStartHTTP)
	registerOpHTTP(router, "GET", "/api/prohibitorum/auth/federation/{slug}/test/callback", contract.AuthRequirement{Kind: contract.AuthPublic}, s.handleOIDCTestCallbackHTTP)
	registerOpHTTP(router, "GET", "/api/prohibitorum/identity-providers/{slug}/tests/{id}", admin, s.handleOIDCTestGetHTTP)
	session := authn.WithSession(context.Background(), &authn.Session{Account: &db.Account{ID: 7, Role: "admin"}, Data: &authn.SessionData{AccountID: 7, SessionID: "admin-session"}})
	req := httptest.NewRequest("POST", "/api/prohibitorum/identity-providers/corp/tests", nil).WithContext(session)
	rec0 := httptest.NewRecorder()
	router.ServeHTTP(rec0, req)
	var start struct{ ID string }
	if err := json.Unmarshal(rec0.Body.Bytes(), &start); err != nil {
		t.Fatal(err)
	}
	cookie := rec0.Result().Cookies()[0]
	description := "AADSTS9002346: Application 'mock-client' is configured for use by Microsoft Account users only."
	failing := httptest.NewRequest("GET", "/api/prohibitorum/auth/federation/corp/test/callback?error=invalid_request&error_description="+url.QueryEscape(description)+"&state="+start.ID, nil)
	failing.AddCookie(cookie)
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, failing)
	if rec1.Code != 302 {
		t.Fatalf("error callback %d: %s", rec1.Code, rec1.Body.String())
	}
	if loc := rec1.Header().Get("Location"); !strings.HasPrefix(loc, "/admin/identity-providers/corp?test=") || strings.Contains(loc, "error") {
		t.Fatalf("redirect %s", loc)
	}
	read := httptest.NewRequest("GET", "/api/prohibitorum/identity-providers/corp/tests/"+start.ID, nil).WithContext(session)
	read.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, read)
	if rec2.Code != 200 || !strings.Contains(rec2.Body.String(), `"status":"failed"`) || !strings.Contains(rec2.Body.String(), `"errorCode":"invalid_request"`) || !strings.Contains(rec2.Body.String(), `"errorDescription":"`+description[:20]) {
		t.Fatalf("result %d: %s", rec2.Code, rec2.Body.String())
	}
}

// TestOIDCDiagnosticErrorCallbackRejectsRepeatedDescription checks the
// at-most-once rule now covers error_description: a parameter-smuggling
// callback with two values is a 400 before anything is read.
func TestOIDCDiagnosticErrorCallbackRejectsRepeatedDescription(t *testing.T) {
	s := &Server{config: &configx.Config{PublicOrigins: []string{"https://rp.example"}}, kvStore: kv.NewMemoryStore(), rateLimiter: authn.NewRateLimiter()}
	router := chi.NewRouter()
	registerOpHTTP(router, "GET", "/api/prohibitorum/auth/federation/{slug}/test/callback", contract.AuthRequirement{Kind: contract.AuthPublic}, s.handleOIDCTestCallbackHTTP)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/prohibitorum/auth/federation/corp/test/callback?error=access_denied&error_description=one&error_description=two&state=whatever", nil))
	if rec.Code != 400 {
		t.Fatalf("repeated error_description accepted: %d %s", rec.Code, rec.Body.String())
	}
}
