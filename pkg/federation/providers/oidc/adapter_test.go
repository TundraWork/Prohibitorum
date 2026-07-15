package oidc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"prohibitorum/cmd/smoke/mockop"
	"prohibitorum/pkg/authn"
	federationcore "prohibitorum/pkg/federation"
)

type adapterFakeClient struct {
	tokens        *Tokens
	tokenEndpoint string
	exchanges     *int
}

func (c *adapterFakeClient) Issuer() string { return "https://issuer.test" }
func (c *adapterFakeClient) TokenEndpoint() string {
	if c.tokenEndpoint != "" {
		return c.tokenEndpoint
	}
	return "https://issuer.test/token"
}
func (c *adapterFakeClient) AuthURL(state, nonce, challenge string) string { return "https://issuer.test/auth?state=" + state + "&nonce=" + nonce + "&challenge=" + challenge }
func (c *adapterFakeClient) Exchange(context.Context, string, string, string, string) (*Tokens, error) {
	if c.exchanges != nil {
		*c.exchanges = *c.exchanges + 1
	}
	return c.tokens, nil
}
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

func TestAdapterAdvanceAllowsOptionalAuthorizationResponseIssuer(t *testing.T) {
	store := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
	secret, err := store.SealProviderSecret([]byte("client-secret"), 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	provider := federationcore.Provider{
		ID: 7, Slug: "corp", Protocol: Protocol,
		Config:       json.RawMessage(`{"issuerUrl":"https://issuer.test","clientId":"client","scopes":["openid"]}`),
		Secret:       secret,
		SecretStatus: "valid",
	}
	adapter := NewAdapter(store)
	adapter.newClient = func(context.Context, Config, string, string) (clientAPI, error) {
		return &adapterFakeClient{tokens: &Tokens{Issuer: "https://issuer.test", Subject: "sub"}}, nil
	}
	state, _, err := adapter.Begin(context.Background(), provider, federationcore.BeginContext{
		Intent: federationcore.IntentLogin, FlowID: "flow", CallbackURL: "https://idp.test/callback",
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := adapter.Advance(context.Background(), provider, state, federationcore.ActionInput{
		Kind: federationcore.ActionRedirect,
		Code: "code",
	})
	if err != nil {
		t.Fatalf("Advance without authorization-response iss: %v", err)
	}
	if result.Identity == nil || result.Identity.Issuer != "https://issuer.test" {
		t.Fatalf("identity = %+v", result.Identity)
	}
}

func TestAdapterCachesClientAcrossBeginAndAdvance(t *testing.T) {
	store := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
	secret, err := store.SealProviderSecret([]byte("client-secret"), 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	provider := federationcore.Provider{
		ID: 7, Slug: "corp", Protocol: Protocol,
		Config:       json.RawMessage(`{"issuerUrl":"https://issuer.test","clientId":"client","scopes":["openid"]}`),
		Secret:       secret,
		SecretStatus: "valid",
	}
	adapter := NewAdapter(store)
	builds := 0
	adapter.newClient = func(context.Context, Config, string, string) (clientAPI, error) {
		builds++
		return &adapterFakeClient{tokens: &Tokens{Issuer: "https://issuer.test", Subject: "sub"}}, nil
	}

	state, _, err := adapter.Begin(context.Background(), provider, federationcore.BeginContext{
		Intent: federationcore.IntentLogin, FlowID: "flow-1", CallbackURL: "https://idp.test/callback",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := adapter.Begin(context.Background(), provider, federationcore.BeginContext{
		Intent: federationcore.IntentLogin, FlowID: "flow-2", CallbackURL: "https://idp.test/callback",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Advance(context.Background(), provider, state, federationcore.ActionInput{
		Kind: federationcore.ActionRedirect, Code: "code",
	}); err != nil {
		t.Fatal(err)
	}
	if builds != 1 {
		t.Fatalf("client builds = %d, want 1 discovery-backed build", builds)
	}
}

func TestAdapterInvalidateClientCacheEvictsOnlyProviderSlug(t *testing.T) {
	store := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
	secret, err := store.SealProviderSecret([]byte("client-secret"), 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	provider := federationcore.Provider{
		ID: 7, Slug: "corp", Protocol: Protocol,
		Config:       json.RawMessage(`{"issuerUrl":"https://issuer.test","clientId":"client","scopes":["openid"]}`),
		Secret:       secret,
		SecretStatus: "valid",
	}
	other := provider
	other.Slug = "other"
	adapter := NewAdapter(store)
	builds := 0
	adapter.newClient = func(context.Context, Config, string, string) (clientAPI, error) {
		builds++
		return &adapterFakeClient{}, nil
	}
	begin := func(p federationcore.Provider, callbackURL string) {
		t.Helper()
		if _, _, err := adapter.Begin(context.Background(), p, federationcore.BeginContext{
			Intent: federationcore.IntentLogin, FlowID: "flow", CallbackURL: callbackURL,
		}); err != nil {
			t.Fatal(err)
		}
	}

	begin(provider, "https://idp.test/login-callback")
	begin(provider, "https://idp.test/link-callback")
	begin(other, "https://idp.test/login-callback")
	if builds != 3 {
		t.Fatalf("initial client builds = %d, want 3", builds)
	}

	adapter.InvalidateClientCache(provider.Slug)
	begin(provider, "https://idp.test/login-callback")
	begin(other, "https://idp.test/login-callback")
	if builds != 4 {
		t.Fatalf("client builds after invalidation = %d, want 4", builds)
	}
}

func TestAdapterClientCacheRebuildsOnKeyVersionChange(t *testing.T) {
	store := federationcore.NewSecretStore(map[int][]byte{
		1: make([]byte, 32),
		2: make([]byte, 32),
	})
	secretV1, err := store.SealProviderSecret([]byte("client-secret-v1"), 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	secretV2, err := store.SealProviderSecret([]byte("client-secret-v2"), 7, 2)
	if err != nil {
		t.Fatal(err)
	}
	provider := federationcore.Provider{
		ID: 7, Slug: "corp", Protocol: Protocol,
		Config:       json.RawMessage(`{"issuerUrl":"https://issuer.test","clientId":"client","scopes":["openid"]}`),
		Secret:       secretV1,
		SecretStatus: "valid",
	}
	adapter := NewAdapter(store)
	builds := 0
	adapter.newClient = func(context.Context, Config, string, string) (clientAPI, error) {
		builds++
		return &adapterFakeClient{}, nil
	}
	begin := func() {
		t.Helper()
		if _, _, err := adapter.Begin(context.Background(), provider, federationcore.BeginContext{
			Intent: federationcore.IntentLogin, FlowID: "flow", CallbackURL: "https://idp.test/callback",
		}); err != nil {
			t.Fatal(err)
		}
	}

	begin()
	provider.Secret = secretV2
	begin()
	begin()
	if builds != 2 {
		t.Fatalf("client builds = %d, want 2 after key-version rotation", builds)
	}
}

func TestAdapterClientCacheExpiresAfterFifteenMinutes(t *testing.T) {
	store := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
	secret, err := store.SealProviderSecret([]byte("client-secret"), 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	provider := federationcore.Provider{
		ID: 7, Slug: "corp", Protocol: Protocol,
		Config:       json.RawMessage(`{"issuerUrl":"https://issuer.test","clientId":"client","scopes":["openid"]}`),
		Secret:       secret,
		SecretStatus: "valid",
	}
	adapter := NewAdapter(store)
	if adapter.cacheTTL != 15*time.Minute {
		t.Fatalf("cache TTL = %s, want 15m", adapter.cacheTTL)
	}
	now := time.Unix(1_700_000_000, 0)
	adapter.now = func() time.Time { return now }
	builds := 0
	adapter.newClient = func(context.Context, Config, string, string) (clientAPI, error) {
		builds++
		return &adapterFakeClient{}, nil
	}
	begin := func() {
		t.Helper()
		if _, _, err := adapter.Begin(context.Background(), provider, federationcore.BeginContext{
			Intent: federationcore.IntentLogin, FlowID: "flow", CallbackURL: "https://idp.test/callback",
		}); err != nil {
			t.Fatal(err)
		}
	}

	begin()
	now = now.Add(15*time.Minute - time.Nanosecond)
	begin()
	now = now.Add(time.Nanosecond)
	begin()
	if builds != 2 {
		t.Fatalf("client builds = %d, want one initial build and one at TTL expiry", builds)
	}
}

func TestAdapterClientCacheSeparatesPrivateNetworkPolicy(t *testing.T) {
	store := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
	secret, err := store.SealProviderSecret([]byte("client-secret"), 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	provider := federationcore.Provider{
		ID: 7, Slug: "corp", Protocol: Protocol,
		Config:       json.RawMessage(`{"issuerUrl":"https://issuer.test","clientId":"client","scopes":["openid"],"allowPrivateNetwork":true}`),
		Secret:       secret,
		SecretStatus: "valid",
	}
	adapter := NewAdapter(store)
	builds := 0
	var policies []bool
	adapter.newClient = func(_ context.Context, config Config, _, _ string) (clientAPI, error) {
		builds++
		policies = append(policies, config.AllowPrivateNetwork)
		return &adapterFakeClient{}, nil
	}
	begin := func() {
		t.Helper()
		if _, _, err := adapter.Begin(context.Background(), provider, federationcore.BeginContext{
			Intent: federationcore.IntentLogin, FlowID: "flow", CallbackURL: "https://idp.test/callback",
		}); err != nil {
			t.Fatal(err)
		}
	}

	begin()
	provider.Config = json.RawMessage(`{"issuerUrl":"https://issuer.test","clientId":"client","scopes":["openid"],"allowPrivateNetwork":false}`)
	begin()
	begin()
	if builds != 2 {
		t.Fatalf("client builds = %d, want 2 across private-network policy change", builds)
	}
	if len(policies) != 2 || !policies[0] || policies[1] {
		t.Fatalf("client build policies = %v, want [true false]", policies)
	}
}

func TestAdapterAdvanceRejectsTokenEndpointDriftBeforeExchange(t *testing.T) {
	store := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
	secret, err := store.SealProviderSecret([]byte("client-secret"), 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	provider := federationcore.Provider{
		ID: 7, Slug: "corp", Protocol: Protocol,
		Config:       json.RawMessage(`{"issuerUrl":"https://issuer.test","clientId":"client","scopes":["openid"]}`),
		Secret:       secret,
		SecretStatus: "valid",
	}
	adapter := NewAdapter(store)
	exchanges := 0
	adapter.newClient = func(context.Context, Config, string, string) (clientAPI, error) {
		return &adapterFakeClient{
			tokens:        &Tokens{Issuer: "https://issuer.test", Subject: "sub"},
			tokenEndpoint: "https://issuer.test/token",
			exchanges:     &exchanges,
		}, nil
	}
	raw, _, err := adapter.Begin(context.Background(), provider, federationcore.BeginContext{
		Intent: federationcore.IntentLogin, FlowID: "flow", CallbackURL: "https://idp.test/callback",
	})
	if err != nil {
		t.Fatal(err)
	}
	var state adapterState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	state.TokenURL = "https://attacker.test/token"
	raw, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}

	_, err = adapter.Advance(context.Background(), provider, raw, federationcore.ActionInput{
		Kind: federationcore.ActionRedirect, Code: "code",
	})
	if authErr := authn.AsAuthError(err); authErr == nil || authErr.Code != "federation_state_invalid" {
		t.Fatalf("Advance error = %v, want federation_state_invalid", err)
	}
	if exchanges != 0 {
		t.Fatalf("token exchanges = %d, want 0 on endpoint drift", exchanges)
	}
}

func TestAdapterCachePreventsRepeatedOIDCDiscovery(t *testing.T) {
	op, err := mockop.New("")
	if err != nil {
		t.Fatal(err)
	}
	discoveryHits := 0
	handler := op.Routes()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			discoveryHits++
		}
		handler.ServeHTTP(w, r)
	}))
	op.SetBase(server.URL)
	t.Cleanup(server.Close)

	store := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
	secret, err := store.SealProviderSecret([]byte("client-secret"), 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	provider := federationcore.Provider{
		ID: 7, Slug: "corp", Protocol: Protocol,
		Config: json.RawMessage(`{
			"issuerUrl":"` + server.URL + `",
			"clientId":"client",
			"scopes":["openid"],
			"allowPrivateNetwork":true
		}`),
		Secret:       secret,
		SecretStatus: "valid",
	}
	adapter := NewAdapter(store)
	for _, flowID := range []string{"flow-1", "flow-2"} {
		if _, _, err := adapter.Begin(context.Background(), provider, federationcore.BeginContext{
			Intent: federationcore.IntentLogin, FlowID: flowID, CallbackURL: "https://idp.test/callback",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if discoveryHits != 1 {
		t.Fatalf("OIDC discovery hits = %d, want 1 across repeated begin requests", discoveryHits)
	}
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
