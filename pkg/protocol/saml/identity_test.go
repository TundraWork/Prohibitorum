package saml

import (
	"testing"
	"time"

	"prohibitorum/pkg/configx"
)

// TestEntityIDBaseURLSplit guards the C7 split: a configured SAML EntityID
// drives the IdP's SAML identity (Issuer / metadata EntityID) but MUST NOT leak
// into the reachable endpoint URLs, which always derive from the public origin.
// A symbolic (non-URL) EntityID like a URN is the sharpest case — if it ever
// pollutes ssoURL/sloURL/metadataURL the IdP advertises unreachable endpoints.
func TestEntityIDBaseURLSplit(t *testing.T) {
	const origin = "https://idp.example.test"

	// No explicit EntityID → identity and URL base both derive from the origin.
	i := &IdP{cfg: &configx.Config{PublicOrigins: []string{origin}}}
	if got := i.entityID(); got != origin {
		t.Errorf("entityID (default) = %q, want %q", got, origin)
	}
	if got := i.baseURL(); got != origin {
		t.Errorf("baseURL (default) = %q, want %q", got, origin)
	}

	// Explicit symbolic EntityID → identity uses it; endpoints stay on origin.
	i2 := &IdP{cfg: &configx.Config{PublicOrigins: []string{origin}}}
	i2.cfg.SAML.EntityID = "urn:example:idp"
	if got := i2.entityID(); got != "urn:example:idp" {
		t.Errorf("entityID (override) = %q, want urn:example:idp", got)
	}
	if got := i2.baseURL(); got != origin {
		t.Errorf("baseURL (override) = %q, want %q (must ignore EntityID)", got, origin)
	}
	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"ssoURL", i2.ssoURL(), origin + "/saml/sso"},
		{"sloURL", i2.sloURL(), origin + "/saml/slo"},
		{"metadataURL", i2.metadataURL(), origin + "/saml/metadata"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q (symbolic EntityID must not leak into endpoint URLs)", tc.name, tc.got, tc.want)
		}
	}
}

// TestSAMLSessionLifetimeResolver guards C9: the IdP-wide SessionNotOnOrAfter
// fallback honors saml.session_lifetime, is nil-cfg-safe, and falls back to the
// package default when unset.
func TestSAMLSessionLifetimeResolver(t *testing.T) {
	if got := (&IdP{}).samlSessionLifetime(); got != defaultSessionLifetime {
		t.Errorf("nil-cfg samlSessionLifetime = %v, want default %v", got, defaultSessionLifetime)
	}
	i := &IdP{cfg: &configx.Config{}}
	if got := i.samlSessionLifetime(); got != defaultSessionLifetime {
		t.Errorf("unset samlSessionLifetime = %v, want default %v", got, defaultSessionLifetime)
	}
	i.cfg.SAML.SessionLifetime = 2 * time.Hour
	if got := i.samlSessionLifetime(); got != 2*time.Hour {
		t.Errorf("configured samlSessionLifetime = %v, want 2h", got)
	}
}
