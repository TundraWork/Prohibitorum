package oidc_test

import (
	"context"
	"strings"
	"testing"

	federationoidc "prohibitorum/pkg/federation/oidc"
)

// TestHardenedClient_BlocksInternalIssuer guards N2: with the dial screen on
// (allowPrivateNetwork=false, the production default), NewClient's discovery
// fetch against an internal / loopback issuer must be refused at dial time —
// the SSRF primitive is closed regardless of what the issuer URL points at.
func TestHardenedClient_BlocksInternalIssuer(t *testing.T) {
	for _, issuer := range []string{
		"http://127.0.0.1:9/",     // loopback
		"http://169.254.169.254/", // cloud metadata (link-local)
		"http://10.0.0.1/",        // RFC1918
		"http://[::1]:9/",         // IPv6 loopback
	} {
		_, err := federationoidc.NewClient(
			context.Background(),
			"client", "secret", "https://rp.example.test/cb",
			[]string{"openid"}, issuer, nil,
			false, // dial screen ON
		)
		if err == nil {
			t.Errorf("NewClient(%q): expected dial to be blocked, got nil error", issuer)
			continue
		}
		if !strings.Contains(err.Error(), "blocked") {
			t.Errorf("NewClient(%q): error %q does not mention the dial block", issuer, err)
		}
	}
}

func TestValidateIssuerURL(t *testing.T) {
	good := []string{
		"https://idp.example.com",
		"https://idp.example.com/realms/x",
		"https://login.microsoftonline.com/tenant/v2.0",
	}
	for _, u := range good {
		if err := federationoidc.ValidateIssuerURL(u); err != nil {
			t.Errorf("ValidateIssuerURL(%q) = %v, want nil", u, err)
		}
	}

	bad := []string{
		"",                           // empty
		"http://idp.example.com",     // not https
		"ftp://idp.example.com",      // wrong scheme
		"https://10.0.0.1",           // IP literal
		"https://169.254.169.254/x",  // metadata IP literal
		"https://[::1]",              // IPv6 literal
		"https://user:pass@idp.test", // userinfo
		"https://",                   // no host
		"://nope",                    // unparseable
	}
	for _, u := range bad {
		if err := federationoidc.ValidateIssuerURL(u); err == nil {
			t.Errorf("ValidateIssuerURL(%q) = nil, want error", u)
		}
	}
}
