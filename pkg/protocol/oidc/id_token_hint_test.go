package oidc

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestVerifyIDTokenHint(t *testing.T) {
	h := newEndpointHarness(t)
	ctx := context.Background()

	expired := h.mintHint(t, "sid-1", time.Now().Add(-24*time.Hour))
	claims, err := h.p.verifyIDTokenHint(ctx, expired)
	if err != nil {
		t.Fatalf("an expired ID token is still a valid hint: %v", err)
	}
	if claims["aud"] != testClientID {
		t.Fatalf("claims = %v", claims)
	}

	wrongIssuer, err := h.p.signJWT(ctx, map[string]any{"iss": "https://other.example", "sub": testSubject, "aud": testClientID}, "JWT")
	if err != nil {
		t.Fatal(err)
	}
	accessToken := h.mintAccessToken(t, testSubject, testClientID, "openid", "jti-hint", time.Now().Add(time.Hour))
	parts := strings.Split(expired, ".")
	tampered := parts[0] + "." + parts[1] + "." + strings.Repeat("A", len(parts[2]))
	for name, token := range map[string]string{
		"access token":  accessToken,
		"wrong issuer":  wrongIssuer,
		"bad signature": tampered,
		"not a JWT":     "not-a-token",
	} {
		if _, err := h.p.verifyIDTokenHint(ctx, token); err == nil {
			t.Errorf("%s accepted as an id_token_hint", name)
		}
	}
}
