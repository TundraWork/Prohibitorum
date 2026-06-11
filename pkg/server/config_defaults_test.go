package server

import (
	"testing"

	"prohibitorum/pkg/configx"
)

// TestDefaultFederationScopes guards C6: the upstream-IdP empty-scopes fallback
// honors federation.default_scopes, and degrades to a minimal OIDC-valid set
// (containing "openid") when the config is empty.
func TestDefaultFederationScopes(t *testing.T) {
	s := &Server{config: &configx.Config{Federation: configx.FederationConfig{DefaultScopes: []string{"openid", "groups"}}}}
	got := s.defaultFederationScopes()
	if len(got) != 2 || got[0] != "openid" || got[1] != "groups" {
		t.Errorf("configured default scopes = %v, want [openid groups]", got)
	}

	s2 := &Server{config: &configx.Config{}}
	if got := s2.defaultFederationScopes(); len(got) == 0 || got[0] != "openid" {
		t.Errorf("empty-config fallback = %v, want a non-empty set starting with openid", got)
	}
}
