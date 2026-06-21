package server

import "testing"

func TestEntityIconURL(t *testing.T) {
	t.Parallel()
	if got := entityIconURL("oidc_client", "my-app", ""); got != "" {
		t.Errorf("empty etag should yield empty URL, got %q", got)
	}
	got := entityIconURL("oidc_client", "my-app", "abcdef0123456789")
	if got != "/icon/oidc_client/my-app?v=abcdef01" {
		t.Errorf("got %q", got)
	}
	if got := entityIconURL("saml_sp", "a b", "deadbeef"); got != "/icon/saml_sp/a%20b?v=deadbeef" {
		t.Errorf("escape: got %q", got)
	}
	if !entityIconKinds["upstream_idp"] || entityIconKinds["bogus"] {
		t.Error("entityIconKinds allowlist wrong")
	}
}
