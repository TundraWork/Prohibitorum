package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
)

// Unknown paths under machine-facing prefixes must answer 404, not the SPA
// shell. Grafana's generic_oauth client probes {userinfo_endpoint}/emails when
// the userinfo response carries no email (grafana/grafana#68678); an HTML body
// there surfaces as "invalid character '<' looking for beginning of value" —
// an error that hides the actual cause (the account has no email address).
// Browser routes (client-side SPA paths) keep the HTML fallback.
func TestMachinePathPrefixesFallThroughTo404NotSPA(t *testing.T) {
	t.Parallel()

	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><title>Prohibitorum</title>"))
	})
	handler := machinePathGuard(spa)

	// The exact URL Grafana derives from the configured userinfo endpoint.
	for _, path := range []string{
		"/oauth/userinfo/emails",
		"/oauth/token/extra",
		"/saml/sso/extra",
		"/oidc/logout/extra",
		"/api/prohibitorum/nope",
		"/avatar/00000000-0000-0000-0000-000000000000/extra",
		"/.well-known/nope",
		// Bare prefix roots ("/oauth" without slash) are guarded too.
		"/oauth", "/saml",
	} {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, rr.Code)
		}
		if ct := rr.Header().Get("Content-Type"); strings.Contains(ct, "text/html") {
			t.Errorf("%s: Content-Type = %q, want non-HTML 404", path, ct)
		}
		if strings.Contains(rr.Body.String(), "<!doctype html>") {
			t.Errorf("%s: body is the SPA shell, want plain 404", path)
		}
	}

	// SPA client-side routes keep the HTML fallback so deep links work.
	for _, path := range []string{
		"/", "/login", "/apps", "/admin/accounts",
		"/oauth2-lookalike", // prefix guard must be exact, not substring
	} {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200 (SPA fallback)", path, rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "<!doctype html>") {
			t.Errorf("%s: body is not the SPA shell", path)
		}
	}
}

func TestSignedOutHomeRedirectsToSignIn(t *testing.T) {
	t.Parallel()

	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<!doctype html><title>Prohibitorum</title>"))
	})
	handler := signedOutHome(spa)
	serve := func(method, path string, sess *authn.Session) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if sess != nil {
			req = req.WithContext(authn.WithSession(req.Context(), sess))
		}
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr
	}

	// No session, and a disabled account's sentinel session, go to sign in.
	for _, sess := range []*authn.Session{nil, {Account: &db.Account{ID: 1}}} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			rr := serve(method, "/", sess)
			if rr.Code != http.StatusFound || rr.Header().Get("Location") != "/login" {
				t.Errorf("%s / (session %v): %d %q, want 302 /login", method, sess != nil, rr.Code, rr.Header().Get("Location"))
			}
			if rr.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("%s /: redirect must not be cached", method)
			}
		}
	}

	// A signed-in reader gets the launcher, and other addresses are the SPA's.
	signedIn := &authn.Session{Account: &db.Account{ID: 1}, Token: "t", Data: &authn.SessionData{SessionID: "s"}}
	if rr := serve(http.MethodGet, "/", signedIn); rr.Code != http.StatusOK {
		t.Errorf("signed in: status %d, want the shell", rr.Code)
	}
	for _, path := range []string{"/login", "/admin", "/consent"} {
		if rr := serve(http.MethodGet, path, nil); rr.Code != http.StatusOK {
			t.Errorf("%s: status %d, want the shell", path, rr.Code)
		}
	}
}
