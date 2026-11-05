package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

// TestCeremonyCookieSecureFromConfig pins the ceremony cookie's Secure flag to
// the deployment-stable secureCookies(cfg) (the https public-origin scheme),
// matching the session and fed-state cookies — NOT a per-request TLS/proxy probe
// that would drop Secure behind a TLS-terminating proxy with TrustProxy off
// (audit SESS-2). The request is plain http with no proxy header.
func TestCeremonyCookieSecureFromConfig(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/login/begin", nil) // r.TLS == nil

	if c := CeremonyCookie(secureCfg(), r, CeremonyCookieName, "v"); !c.Secure {
		t.Errorf("https deployment: ceremony cookie Secure = false, want true (behind a TLS proxy r.TLS is nil)")
	}
	if c := CeremonyCookie(devCfg(), r, CeremonyCookieName, "v"); c.Secure {
		t.Errorf("http dev deployment: ceremony cookie Secure = true, want false")
	}
}

func secureCfg() *configx.Config {
	return &configx.Config{PublicOrigins: []string{"https://idp.example.com"}}
}

func devCfg() *configx.Config {
	return &configx.Config{PublicOrigins: []string{"http://localhost:8080"}}
}

func TestSessionCookieNameFor(t *testing.T) {
	if got := SessionCookieNameFor(secureCfg()); got != "__Host-"+SessionCookieName {
		t.Errorf("secure name = %q, want %q", got, "__Host-"+SessionCookieName)
	}
	if got := SessionCookieNameFor(devCfg()); got != SessionCookieName {
		t.Errorf("dev name = %q, want %q", got, SessionCookieName)
	}
	if got := SessionCookieNameFor(&configx.Config{}); got != SessionCookieName {
		t.Errorf("no-origin name = %q, want plain %q", got, SessionCookieName)
	}
}

// TestLoadSessionRenewalCookieMaxAge drives the middleware's renewal branch:
// a remembered session that refreshes re-emits Max-Age = rememberTTL; a
// plain one re-emits no Max-Age at all. The session's own Remember decides,
// not the request.
func TestLoadSessionRenewalCookieMaxAge(t *testing.T) {
	for _, tc := range []struct {
		name        string
		remember    bool
		wantPresent bool
	}{
		{"remembered renews with Max-Age", true, true},
		{"plain renews without Max-Age", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mem := kv.NewMemoryStore()
			t.Cleanup(func() { _ = mem.Close() })
			store := NewSessionStore(mem, noopSessionQueries{}, 40*time.Millisecond, time.Hour)
			ctx := context.Background()
			var token string
			var err error
			if tc.remember {
				token, _, err = store.Issue(ctx, 42, "", "", []string{"hwk"}, nil, true)
			} else {
				token, _, err = store.Issue(ctx, 42, "", "", []string{"hwk"}, nil, false)
			}
			if err != nil {
				t.Fatal(err)
			}
			// Pull both sessions past the plain TTL's refresh threshold
			// (40ms/4 = 10ms) while leaving them live: the remembered one
			// must be rewritten because its own TTL is the full hour.
			rewriteSessionExpiryForTest(t, mem, 42, token, time.Now().Add(20*time.Millisecond))

			rec := httptest.NewRecorder()
			handler := LoadSession(devCfg(), fakeAccountQuerier{}, store, func(*http.Request) string { return "" })(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
			)
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.AddCookie(&http.Cookie{Name: cookieNameFor(tc.remember), Value: EncodeCookie([]Entry{{Seq: 1, AccountID: 42, Token: token}})})
			handler.ServeHTTP(rec, req)
			cookieHeader := rec.Header().Get("Set-Cookie")
			if tc.wantPresent && !strings.Contains(cookieHeader, "Max-Age=3600") {
				t.Errorf("renewal Set-Cookie %q missing Max-Age=3600", cookieHeader)
			}
			// The temp cookie has no Max-Age to extend, so a plain renewal
			// rewrites nothing.
			if !tc.wantPresent && cookieHeader != "" {
				t.Errorf("plain renewal must not rewrite a cookie, got %q", cookieHeader)
			}
		})
	}
}

func cookieNameFor(remember bool) string {
	if remember {
		return SessionCookieName
	}
	return TempSessionCookieName
}

// fakeAccountQuerier answers LoadSession's GetAccountByID with an enabled
// row; LoadSession needs the full db.Querier, so the rest panics if reached.
type fakeAccountQuerier struct {
	db.Querier
}

func (fakeAccountQuerier) GetAccountByID(context.Context, int32) (db.Account, error) {
	return db.Account{ID: 42, Username: "alice"}, nil
}
