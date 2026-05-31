package session

import (
	"net/http"
	"testing"
	"time"

	"prohibitorum/pkg/configx"
)

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
