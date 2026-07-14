package steam

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"

	federationcore "prohibitorum/pkg/federation"
)

func TestAdapterMapsVerifiedSteamIdentity(t *testing.T) {
	store := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
	secret, err := store.SealProviderSecret([]byte("api-key"), 7, 1); if err != nil { t.Fatal(err) }
	provider := federationcore.Provider{ID: 7, Slug: "steam", Protocol: Protocol, Config: json.RawMessage(`{}`), Secret: secret, SecretStatus: "valid"}
	adapter := NewAdapter(store)
	adapter.verify = func(context.Context, url.Values, string) (string, error) { return "76561198000000000", nil }
	adapter.summary = func(context.Context, string, string) (Summary, error) { return Summary{PersonaName: "Gaben", AvatarURL: "https://cdn/avatar.jpg"}, nil }
	state, action, err := adapter.Begin(context.Background(), provider, federationcore.BeginContext{FlowID: "flow", CallbackURL: "https://idp.test/api/prohibitorum/auth/federation/steam/callback"})
	if err != nil { t.Fatal(err) }
	if action.Kind != federationcore.ActionRedirect || action.URL == "" { t.Fatalf("action = %+v", action) }
	result, err := adapter.Advance(context.Background(), provider, state, federationcore.ActionInput{Kind: federationcore.ActionRedirect, Params: url.Values{"openid.mode":{"id_res"}}})
	if err != nil { t.Fatal(err) }
	identity := result.Identity
	if identity == nil || identity.Issuer != Issuer || identity.Subject != "76561198000000000" || identity.Username != "steam_76561198000000000" || identity.DisplayName != "Gaben" || identity.AvatarURL != "https://cdn/avatar.jpg" || identity.UpstreamData["profileUrl"] != "https://steamcommunity.com/profiles/76561198000000000" {
		t.Fatalf("identity = %+v", identity)
	}
}

func TestDefinitionRequiresValidatedSecret(t *testing.T) {
	definition := Definition{}
	provider := federationcore.Provider{Protocol: Protocol, Config: json.RawMessage(`{}`), Secret: &federationcore.SealedSecret{Ciphertext: []byte{1}, Nonce: []byte{2}, KeyVersion: 1}, SecretStatus: "valid"}
	if !definition.Ready(provider) { t.Fatal("valid Steam provider not ready") }
	provider.SecretStatus = "invalid"
	if definition.Ready(provider) { t.Fatal("invalid secret status ready") }
}
