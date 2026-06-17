package oidc

import "testing"

func TestIsSupportedScope(t *testing.T) {
	for _, s := range []string{"openid", "profile", "email", "offline_access", "groups"} {
		if !IsSupportedScope(s) {
			t.Errorf("IsSupportedScope(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"address", "phone", "foo", "", "Openid", "groups "} {
		if IsSupportedScope(s) {
			t.Errorf("IsSupportedScope(%q) = true, want false", s)
		}
	}
}

func TestValidateAllowedScopes(t *testing.T) {
	if err := ValidateAllowedScopes([]string{"openid", "profile", "email", "groups", "offline_access"}); err != nil {
		t.Errorf("ValidateAllowedScopes(all supported) = %v, want nil", err)
	}
	if err := ValidateAllowedScopes(nil); err != nil {
		t.Errorf("ValidateAllowedScopes(nil) = %v, want nil", err)
	}
	if err := ValidateAllowedScopes([]string{"openid", "custom_thing"}); err == nil {
		t.Error("ValidateAllowedScopes(custom) = nil, want error")
	}
}

// TestSupportedScopesReturnsFreshSlice guards that callers cannot mutate the
// canonical set through the returned slice.
func TestSupportedScopesReturnsFreshSlice(t *testing.T) {
	s := SupportedScopes()
	s[0] = "mutated"
	if SupportedScopes()[0] != "openid" {
		t.Error("SupportedScopes() leaked a mutable backing array")
	}
}
