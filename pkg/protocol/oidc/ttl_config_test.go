package oidc

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"prohibitorum/pkg/configx"
)

// TestTTLResolversHonorConfigElseDefault covers the C1–C5 resolver contract:
// a configured oidc.*_ttl wins, an unset (zero) value falls back to the
// compiled-in default, and a Provider built without a cfg is nil-safe.
func TestTTLResolversHonorConfigElseDefault(t *testing.T) {
	// Unset config → compiled-in defaults.
	pd := &Provider{cfg: &configx.Config{}}
	cases := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"access", pd.accessTokenTTL(), AccessTokenTTL},
		{"id", pd.idTokenTTL(), IDTokenTTL},
		{"refresh", pd.refreshTokenTTL(), RefreshTokenTTL},
		{"code", pd.authCodeTTL(), AuthorizationCodeTTL},
		{"jwks", pd.jwksCacheMaxAge(), defaultJWKSCacheMaxAge},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("default %s TTL = %v, want %v", c.name, c.got, c.want)
		}
	}

	// nil cfg → still defaults, never a panic (HandleJWKS path).
	pn := &Provider{}
	if pn.accessTokenTTL() != AccessTokenTTL || pn.jwksCacheMaxAge() != defaultJWKSCacheMaxAge {
		t.Fatalf("nil-cfg resolvers did not fall back to defaults")
	}

	// Configured values are honored.
	pc := &Provider{cfg: &configx.Config{OIDC: configx.OIDCConfig{
		AccessTokenTTL:       5 * time.Minute,
		IDTokenTTL:           7 * time.Minute,
		RefreshTokenTTL:      48 * time.Hour,
		AuthorizationCodeTTL: 90 * time.Second,
		JWKSCacheMaxAge:      2 * time.Minute,
	}}}
	for _, c := range []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"access", pc.accessTokenTTL(), 5 * time.Minute},
		{"id", pc.idTokenTTL(), 7 * time.Minute},
		{"refresh", pc.refreshTokenTTL(), 48 * time.Hour},
		{"code", pc.authCodeTTL(), 90 * time.Second},
		{"jwks", pc.jwksCacheMaxAge(), 2 * time.Minute},
	} {
		if c.got != c.want {
			t.Errorf("configured %s TTL = %v, want %v", c.name, c.got, c.want)
		}
	}
	if cc := pc.jwksCacheControl(); cc != "public, max-age=120" {
		t.Errorf("jwksCacheControl = %q, want public, max-age=120", cc)
	}
}

// TestTokenGrantHonorsConfiguredTTLs is the wiring guard: it drives a real
// authorization_code grant against a Provider configured with NON-default
// access- and refresh-token lifetimes and asserts the response expires_in (C1)
// and the persisted refresh-family KV TTL (C3) reflect the config — catching any
// future regression where a mint/store site reverts to the package const.
func TestTokenGrantHonorsConfiguredTTLs(t *testing.T) {
	h := newTokenHarness(t)
	ctx := context.Background()

	const wantAccess = 5 * time.Minute
	const wantRefresh = 48 * time.Hour
	h.p.cfg.OIDC.AccessTokenTTL = wantAccess
	h.p.cfg.OIDC.RefreshTokenTTL = wantRefresh

	code := h.mintTestCode(t, baseAuthCode())
	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(codeExchangeForm(code, testVerifier, testRedirect)))
	if rec.Code != 200 {
		t.Fatalf("want 200, got %d (body %s)", rec.Code, rec.Body.String())
	}

	var resp tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if resp.ExpiresIn != int(wantAccess.Seconds()) {
		t.Errorf("expires_in = %d, want %d (configured access TTL not honored)", resp.ExpiresIn, int(wantAccess.Seconds()))
	}
	if resp.RefreshToken == "" {
		t.Fatal("expected a refresh token (offline_access granted)")
	}

	// The refresh family's KV entry must carry the configured 48h TTL, not the
	// 30d default const. Allow a small delta for elapsed wall-clock. The token
	// embeds the family ID, so we parse it and check the family record's TTL.
	fid, _, perr := parseRefreshToken(resp.RefreshToken)
	if perr != nil {
		t.Fatalf("parse refresh token: %v", perr)
	}
	ttl, err := h.p.kv.TTL(ctx, refreshFamilyKey(fid))
	if err != nil {
		t.Fatalf("read refresh family TTL: %v", err)
	}
	want := int64(wantRefresh.Seconds())
	if ttl < want-30 || ttl > want {
		t.Errorf("refresh family KV TTL = %ds, want ≈%ds (configured refresh TTL not honored)", ttl, want)
	}
}
