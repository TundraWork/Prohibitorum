package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"prohibitorum/pkg/authn"
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

func TestFreshSessionCookie_SecureDeployment(t *testing.T) {
	c := FreshSessionCookie(secureCfg(), nil, 42, "tok", time.Hour)
	if c.Name != "__Host-"+SessionCookieName {
		t.Errorf("Name = %q, want __Host-%s", c.Name, SessionCookieName)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want /", c.Path)
	}
	if !c.Secure {
		t.Error("Secure = false, want true in https deployment")
	}
	if !c.HttpOnly {
		t.Error("HttpOnly = false, want true")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.Domain != "" {
		t.Errorf("Domain = %q, want empty (__Host- forbids Domain)", c.Domain)
	}
	if c.MaxAge != int(time.Hour.Seconds()) {
		t.Errorf("MaxAge = %d, want %d", c.MaxAge, int(time.Hour.Seconds()))
	}
}

func TestFreshSessionCookie_DevDeployment(t *testing.T) {
	c := FreshSessionCookie(devCfg(), nil, 42, "tok", time.Hour)
	if c.Name != SessionCookieName {
		t.Errorf("Name = %q, want plain %s", c.Name, SessionCookieName)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want /", c.Path)
	}
	if c.Secure {
		t.Error("Secure = true, want false over http dev (cookiejar won't send Secure over http)")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
}

func TestClearedSessionCookie_MatchesFresh(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  *configx.Config
	}{{"secure", secureCfg()}, {"dev", devCfg()}} {
		t.Run(tc.name, func(t *testing.T) {
			fresh := FreshSessionCookie(tc.cfg, nil, 42, "tok", time.Hour)
			clear := ClearedSessionCookie(tc.cfg, nil)
			if clear.Name != fresh.Name {
				t.Errorf("clear Name = %q, fresh Name = %q (must match to delete)", clear.Name, fresh.Name)
			}
			if clear.Path != fresh.Path {
				t.Errorf("clear Path = %q, fresh Path = %q (must match to delete)", clear.Path, fresh.Path)
			}
			if clear.Secure != fresh.Secure {
				t.Errorf("clear Secure = %v, fresh Secure = %v", clear.Secure, fresh.Secure)
			}
			if clear.MaxAge != -1 {
				t.Errorf("clear MaxAge = %d, want -1", clear.MaxAge)
			}
			if clear.Value != "" {
				t.Errorf("clear Value = %q, want empty", clear.Value)
			}
		})
	}
}

// TestSessionCookie_RemembersMaxAge pins the remember-me cookie contract:
// remembered sessions carry Max-Age = rememberTTL; the rest omit Max-Age
// entirely (browser-session cookie), and the clear cookie still matches both
// on name/path/attributes.
func TestSessionCookie_RemembersMaxAge(t *testing.T) {
	store := NewSessionStore(nil, nil, time.Hour, 24*time.Hour)
	r := httptest.NewRequest(http.MethodGet, "/", nil)

	remembered := store.SessionCookie(devCfg(), r, 42, "tok", &authn.SessionData{Remember: true})
	if remembered.MaxAge != int((24 * time.Hour).Seconds()) {
		t.Errorf("remembered MaxAge = %d, want %d", remembered.MaxAge, int((24*time.Hour).Seconds()))
	}
	if s := remembered.String(); !strings.Contains(s, "Max-Age=86400") {
		t.Errorf("remembered Set-Cookie %q missing Max-Age", s)
	}

	plain := store.SessionCookie(devCfg(), r, 42, "tok", &authn.SessionData{})
	if plain.MaxAge != 0 {
		t.Errorf("plain MaxAge = %d, want 0", plain.MaxAge)
	}
	if s := plain.String(); strings.Contains(s, "Max-Age") {
		t.Errorf("plain Set-Cookie %q must not carry Max-Age", s)
	}

	// Legacy data (nil) also decodes as not-remembered.
	if c := store.SessionCookie(devCfg(), r, 42, "tok", nil); c.MaxAge != 0 {
		t.Errorf("nil data MaxAge = %d, want 0", c.MaxAge)
	}

	clear := ClearedSessionCookie(devCfg(), nil)
	if clear.Name != plain.Name || clear.Path != plain.Path || clear.Secure != plain.Secure {
		t.Errorf("clear cookie (%q %q secure=%v) must match plain (%q %q secure=%v)",
			clear.Name, clear.Path, clear.Secure, plain.Name, plain.Path, plain.Secure)
	}
	if clear.Name != remembered.Name || clear.Path != remembered.Path {
		t.Errorf("clear cookie must also match the remembered cookie's name/path")
	}
}

// TestFreshSessionCookie_ZeroMaxAgeOmitted pins the FreshSessionCookie
// semantics SessionCookie builds on: maxAge 0 writes no Max-Age.
func TestFreshSessionCookie_ZeroMaxAgeOmitted(t *testing.T) {
	c := FreshSessionCookie(devCfg(), nil, 42, "tok", 0)
	if c.MaxAge != 0 {
		t.Fatalf("MaxAge = %d, want 0", c.MaxAge)
	}
	if s := c.String(); strings.Contains(s, "Max-Age") {
		t.Errorf("Set-Cookie %q must not carry Max-Age for maxAge=0", s)
	}
	c = FreshSessionCookie(devCfg(), nil, 42, "tok", time.Hour)
	if s := c.String(); !strings.Contains(s, "Max-Age=3600") {
		t.Errorf("Set-Cookie %q missing Max-Age=3600", s)
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
			req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: CookieValue(42, token)})
			handler.ServeHTTP(rec, req)
			cookieHeader := rec.Header().Get("Set-Cookie")
			if tc.wantPresent && !strings.Contains(cookieHeader, "Max-Age=3600") {
				t.Errorf("renewal Set-Cookie %q missing Max-Age=3600", cookieHeader)
			}
			if !tc.wantPresent && strings.Contains(cookieHeader, "Max-Age") {
				t.Errorf("plain renewal Set-Cookie %q must not carry Max-Age", cookieHeader)
			}
		})
	}
}

// fakeAccountQuerier answers LoadSession's GetAccountByID with an enabled
// row; LoadSession needs the full db.Querier, so the rest panics if reached.
type fakeAccountQuerier struct {
	db.Querier
}

func (fakeAccountQuerier) GetAccountByID(context.Context, int32) (db.Account, error) {
	return db.Account{ID: 42, Username: "alice"}, nil
}
