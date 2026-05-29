package oidc

import (
	"testing"

	"prohibitorum/pkg/credential/password"
)

func TestClientGenConfidential(t *testing.T) {
	params, secret, err := BuildClientParams(ClientOptions{
		ClientID:     "c",
		RedirectURIs: []string{"https://rp/cb"},
	})
	if err != nil {
		t.Fatalf("BuildClientParams: %v", err)
	}
	if secret == "" {
		t.Fatal("expected non-empty plaintext secret for confidential client")
	}
	if !params.ClientSecretHash.Valid {
		t.Fatal("expected ClientSecretHash.Valid == true for confidential client")
	}
	if !password.VerifyRaw(secret, params.ClientSecretHash.String) {
		t.Fatal("plaintext secret did not verify against stored hash")
	}
	if params.TokenEndpointAuthMethod != "client_secret_basic" {
		t.Fatalf("TokenEndpointAuthMethod = %q, want client_secret_basic", params.TokenEndpointAuthMethod)
	}
	if !params.RequirePkce {
		t.Fatal("expected RequirePkce == true")
	}
	wantScopes := []string{"openid", "profile"}
	if len(params.AllowedScopes) != len(wantScopes) {
		t.Fatalf("AllowedScopes = %v, want %v", params.AllowedScopes, wantScopes)
	}
	for i := range wantScopes {
		if params.AllowedScopes[i] != wantScopes[i] {
			t.Fatalf("AllowedScopes = %v, want %v", params.AllowedScopes, wantScopes)
		}
	}
	if len(params.AllowedCodeChallengeMethods) != 1 || params.AllowedCodeChallengeMethods[0] != "S256" {
		t.Fatalf("AllowedCodeChallengeMethods = %v, want [S256]", params.AllowedCodeChallengeMethods)
	}
}

func TestClientGenPublic(t *testing.T) {
	params, secret, err := BuildClientParams(ClientOptions{
		ClientID:     "c",
		RedirectURIs: []string{"https://rp/cb"},
		Public:       true,
	})
	if err != nil {
		t.Fatalf("BuildClientParams: %v", err)
	}
	if secret != "" {
		t.Fatalf("expected empty plaintext secret for public client, got %q", secret)
	}
	if params.ClientSecretHash.Valid {
		t.Fatal("expected ClientSecretHash.Valid == false for public client")
	}
	if params.TokenEndpointAuthMethod != "none" {
		t.Fatalf("TokenEndpointAuthMethod = %q, want none", params.TokenEndpointAuthMethod)
	}
	if !params.RequirePkce {
		t.Fatal("expected RequirePkce == true for public client")
	}
}

func TestClientGenCustomScopesAndConsent(t *testing.T) {
	params, _, err := BuildClientParams(ClientOptions{
		ClientID:       "c",
		RedirectURIs:   []string{"https://rp/cb"},
		Scopes:         []string{"openid", "email", "groups"},
		RequireConsent: true,
	})
	if err != nil {
		t.Fatalf("BuildClientParams: %v", err)
	}
	want := []string{"openid", "email", "groups"}
	if len(params.AllowedScopes) != len(want) {
		t.Fatalf("AllowedScopes = %v, want %v", params.AllowedScopes, want)
	}
	for i := range want {
		if params.AllowedScopes[i] != want[i] {
			t.Fatalf("AllowedScopes = %v, want %v", params.AllowedScopes, want)
		}
	}
	if !params.RequireConsent {
		t.Fatal("expected RequireConsent == true")
	}
}

func TestClientGenValidation(t *testing.T) {
	if _, _, err := BuildClientParams(ClientOptions{RedirectURIs: []string{"https://rp/cb"}}); err == nil {
		t.Fatal("expected error when ClientID is missing")
	}
	if _, _, err := BuildClientParams(ClientOptions{ClientID: "c"}); err == nil {
		t.Fatal("expected error when RedirectURIs is empty")
	}
}

func TestClientGenSecretsAreUnique(t *testing.T) {
	opts := ClientOptions{ClientID: "c", RedirectURIs: []string{"https://rp/cb"}}
	_, s1, err := BuildClientParams(opts)
	if err != nil {
		t.Fatalf("BuildClientParams: %v", err)
	}
	_, s2, err := BuildClientParams(opts)
	if err != nil {
		t.Fatalf("BuildClientParams: %v", err)
	}
	if s1 == s2 {
		t.Fatal("expected two confidential builds to produce different secrets")
	}
}
