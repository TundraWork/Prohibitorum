package oidc

import (
	"context"
	"encoding/json"
	"testing"

	federationcore "prohibitorum/pkg/federation"
)

type adapterFakeClient struct{ tokens *Tokens }
func (c *adapterFakeClient) Issuer() string { return "https://issuer.test" }
func (c *adapterFakeClient) TokenEndpoint() string { return "https://issuer.test/token" }
func (c *adapterFakeClient) AuthURL(state, nonce, challenge string) string { return "https://issuer.test/auth?state=" + state + "&nonce=" + nonce + "&challenge=" + challenge }
func (c *adapterFakeClient) Exchange(context.Context, string, string, string, string) (*Tokens, error) { return c.tokens, nil }
func (c *adapterFakeClient) UserInfo(context.Context, string, string) (map[string]any, error) { return nil, nil }

func TestAdapterBeginAndAdvanceVerifiedIdentity(t *testing.T) {
	store := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
	secret, err := store.SealProviderSecret([]byte("client-secret"), 7, 1); if err != nil { t.Fatal(err) }
	provider := federationcore.Provider{ID: 7, Slug: "corp", Protocol: Protocol, Config: json.RawMessage(`{"issuerUrl":"https://issuer.test","clientId":"client","scopes":["openid"],"usernameClaim":"preferred_username","displayNameClaim":"name","emailClaim":"email","pictureClaim":"picture"}`), Secret: secret, SecretStatus: "valid"}
	adapter := NewAdapter(store)
	adapter.newClient = func(context.Context, Config, string, string) (clientAPI, error) {
		return &adapterFakeClient{tokens: &Tokens{Issuer: "https://issuer.test", Subject: "sub", EmailVerified: true, AMR: []string{"pwd"}, Raw: map[string]any{"preferred_username":"alice","name":"Alice","email":"alice@example.com","picture":"https://cdn.test/a.png"}}}, nil
	}
	state, action, err := adapter.Begin(context.Background(), provider, federationcore.BeginContext{Intent: federationcore.IntentLogin, FlowID: "flow", CallbackURL: "https://idp.test/callback"})
	if err != nil { t.Fatal(err) }
	if action.Kind != federationcore.ActionRedirect || action.URL == "" { t.Fatalf("action = %+v", action) }
	result, err := adapter.Advance(context.Background(), provider, state, federationcore.ActionInput{Kind: federationcore.ActionRedirect, Code: "code", Issuer: "https://issuer.test"})
	if err != nil { t.Fatal(err) }
	if result.Identity == nil || result.Identity.Username != "alice" || result.Identity.DisplayName != "Alice" || result.Identity.Email == nil || *result.Identity.Email != "alice@example.com" || result.Identity.AvatarURL != "https://cdn.test/a.png" {
		t.Fatalf("identity = %+v", result.Identity)
	}
	if len(result.State) != 0 || result.Next != nil { t.Fatalf("terminal result carried state/action: %+v", result) }
}

func TestDefinitionReadiness(t *testing.T) {
	definition := Definition{}
	provider := federationcore.Provider{Protocol: Protocol, Config: json.RawMessage(`{"issuerUrl":"https://issuer.test","clientId":"client","scopes":["openid"]}`), Secret: &federationcore.SealedSecret{Ciphertext: []byte{1}, Nonce: []byte{2}, KeyVersion: 1}, SecretStatus: "valid"}
	if !definition.Ready(provider) { t.Fatal("valid provider not ready") }
	provider.SecretStatus = "invalid"
	if definition.Ready(provider) { t.Fatal("invalid secret status ready") }
	if err := definition.ValidateSecret(nil); err == nil { t.Fatal("empty secret accepted") }
	if err := definition.ValidateConfig(json.RawMessage(`{"issuerUrl":"http://issuer.test"}`)); err == nil { t.Fatal("invalid config accepted") }
}
