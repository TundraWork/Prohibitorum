package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/kv"
	sessstore "prohibitorum/pkg/session"
)

func TestNavigationMW(t *testing.T) {
	cfg := &configx.Config{PublicOrigins: []string{"https://id.example.com"}}
	redirectingTo := func(location string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.SetCookie(w, &http.Cookie{Name: "kept", Value: "1"})
			http.Redirect(w, r, location, http.StatusFound)
		})
	}
	consent := redirectingTo("https://id.example.com/consent?ticket=t-1")
	callback := redirectingTo("https://app.example.org/cb?code=c-secret&state=s")

	serve := func(store kv.Store, h http.Handler, method, path string, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rr := httptest.NewRecorder()
		navigationMW(store, cfg)(h).ServeHTTP(rr, req)
		return rr
	}
	follow := map[string]string{navigationHeader: "follow"}
	named := func(t *testing.T, rr *httptest.ResponseRecorder) string {
		t.Helper()
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %q", rr.Code, rr.Body.String())
		}
		var got navigationLocation
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("body %q is not a location: %v", rr.Body.String(), err)
		}
		if rr.Header().Get("Location") != "" {
			t.Fatal("Location header still set")
		}
		if rr.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("Cache-Control = %q", rr.Header().Get("Cache-Control"))
		}
		return got.Location
	}

	t.Run("names a dashboard page a hop leads to", func(t *testing.T) {
		for _, path := range []string{"/oauth/authorize?client_id=wiki", accountChooserContinuePath + "?ticket=c-1"} {
			rr := serve(kv.NewMemoryStore(), consent, http.MethodGet, path, map[string]string{
				navigationHeader: "follow", "Sec-Fetch-Site": "same-origin",
			})
			if got := named(t, rr); got != "https://id.example.com/consent?ticket=t-1" {
				t.Fatalf("%s: location = %q", path, got)
			}
			if !strings.Contains(rr.Header().Get("Set-Cookie"), "kept=1") {
				t.Fatalf("%s: the hop's cookie was dropped", path)
			}
		}
	})

	t.Run("holds an application's callback for the browser, never naming its code", func(t *testing.T) {
		store := kv.NewMemoryStore()
		rr := serve(store, callback, http.MethodGet, "/oauth/authorize", follow)
		if got := named(t, rr); got != handoffPath {
			t.Fatalf("location = %q, want %q", got, handoffPath)
		}
		if strings.Contains(rr.Body.String(), "c-secret") {
			t.Fatalf("body names the code: %q", rr.Body.String())
		}
		var handoff *http.Cookie
		for _, c := range rr.Result().Cookies() {
			if c.Name == sessstore.HandoffCookieName {
				handoff = c
			}
		}
		if handoff == nil || !handoff.HttpOnly || handoff.Path != handoffPath {
			t.Fatalf("handoff cookie = %+v", handoff)
		}

		s := &Server{config: cfg, kvStore: store}
		collect := func() *httptest.ResponseRecorder {
			req := httptest.NewRequest(http.MethodGet, handoffPath, nil)
			req.AddCookie(handoff)
			rr := httptest.NewRecorder()
			s.handleNavigationHandoff(rr, req)
			return rr
		}
		first := collect()
		if first.Code != http.StatusFound || first.Header().Get("Location") != "https://app.example.org/cb?code=c-secret&state=s" {
			t.Fatalf("handoff: status = %d, Location = %q", first.Code, first.Header().Get("Location"))
		}
		// Spent: a second load has nothing to continue.
		again := collect()
		if again.Code != http.StatusFound || !strings.HasPrefix(again.Header().Get("Location"), "/error?") {
			t.Fatalf("second handoff: status = %d, Location = %q", again.Code, again.Header().Get("Location"))
		}
	})

	t.Run("refuses a handoff without its cookie", func(t *testing.T) {
		s := &Server{config: cfg, kvStore: kv.NewMemoryStore()}
		rr := httptest.NewRecorder()
		s.handleNavigationHandoff(rr, httptest.NewRequest(http.MethodGet, handoffPath, nil))
		if !strings.HasPrefix(rr.Header().Get("Location"), "/error?") {
			t.Fatalf("Location = %q", rr.Header().Get("Location"))
		}
	})

	t.Run("holds a same-site address that is not a dashboard page", func(t *testing.T) {
		for _, location := range []string{
			"/forward-auth/callback?state=s",
			"/consent?code=c-secret",
			"//app.example.org/consent",
			"https://id.example.com.evil.example/consent",
		} {
			rr := serve(kv.NewMemoryStore(), redirectingTo(location), http.MethodGet, "/oauth/authorize", follow)
			if got := named(t, rr); got != handoffPath {
				t.Fatalf("%s: location = %q, want the handoff", location, got)
			}
		}
	})

	t.Run("redirects as before without the header", func(t *testing.T) {
		rr := serve(kv.NewMemoryStore(), callback, http.MethodGet, "/oauth/authorize", nil)
		if rr.Code != http.StatusFound || rr.Header().Get("Location") == "" {
			t.Fatalf("status = %d, Location = %q", rr.Code, rr.Header().Get("Location"))
		}
		if rr.Header().Get("Vary") != navigationHeader {
			t.Fatalf("Vary = %q", rr.Header().Get("Vary"))
		}
	})

	t.Run("redirects a request another site sent", func(t *testing.T) {
		rr := serve(kv.NewMemoryStore(), callback, http.MethodGet, "/oauth/authorize", map[string]string{
			navigationHeader: "follow", "Sec-Fetch-Site": "cross-site",
		})
		if rr.Code != http.StatusFound {
			t.Fatalf("status = %d, want 302", rr.Code)
		}
	})

	t.Run("leaves other paths alone", func(t *testing.T) {
		for _, path := range []string{"/saml/sso", "/consent"} {
			rr := serve(kv.NewMemoryStore(), consent, http.MethodGet, path, follow)
			if rr.Code != http.StatusFound {
				t.Fatalf("%s: status = %d, want 302", path, rr.Code)
			}
			if rr.Header().Get("Vary") != "" {
				t.Fatalf("%s: Vary = %q", path, rr.Header().Get("Vary"))
			}
		}
	})

	t.Run("passes an answer that is not a redirect through", func(t *testing.T) {
		plain := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("refused"))
		})
		rr := serve(kv.NewMemoryStore(), plain, http.MethodGet, "/oauth/authorize", follow)
		if rr.Code != http.StatusBadRequest || rr.Body.String() != "refused" {
			t.Fatalf("status = %d, body = %q", rr.Code, rr.Body.String())
		}
	})

	t.Run("names the maintenance gate's redirect", func(t *testing.T) {
		gate := maintenanceGateMW(maintenanceResolver(true))(consent)
		req := httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
		req.Header.Set(navigationHeader, "follow")
		req = req.WithContext(authn.WithSession(req.Context(), sessionForRole("user")))
		rr := httptest.NewRecorder()
		navigationMW(kv.NewMemoryStore(), cfg)(gate).ServeHTTP(rr, req)
		if got := named(t, rr); got != "/" {
			t.Fatalf("location = %q, want /", got)
		}
	})
}
