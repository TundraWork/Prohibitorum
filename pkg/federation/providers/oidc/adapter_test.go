package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	oidclib "github.com/zitadel/oidc/v3/pkg/oidc"

	"prohibitorum/cmd/smoke/mockop"
	"prohibitorum/pkg/authn"
	federationcore "prohibitorum/pkg/federation"
)

type adapterFakeClient struct {
	tokens        *Tokens
	tokenEndpoint string
	exchanges     *int
	userInfo      map[string]any
	userInfoCalls int
	userInfoErr   error
	rawUserInfo   map[string]any
	rawUserErr    error
	exchangeErr   error
}

func (c *adapterFakeClient) Issuer() string { return "https://issuer.test" }
func (c *adapterFakeClient) TokenEndpoint() string {
	if c.tokenEndpoint != "" {
		return c.tokenEndpoint
	}
	return "https://issuer.test/token"
}
func (c *adapterFakeClient) AuthURL(state, nonce, challenge string) string {
	return "https://issuer.test/auth?state=" + state + "&nonce=" + nonce + "&challenge=" + challenge
}
func (c *adapterFakeClient) Exchange(context.Context, string, string, string, string) (*Tokens, error) {
	if c.exchanges != nil {
		*c.exchanges = *c.exchanges + 1
	}
	return c.tokens, c.exchangeErr
}
func (c *adapterFakeClient) UserInfo(context.Context, string, string) (map[string]any, error) {
	c.userInfoCalls++
	return c.userInfo, c.userInfoErr
}
func (c *adapterFakeClient) UserInfoRaw(_ context.Context, _, _ string) (map[string]any, error) {
	return c.rawUserInfo, c.rawUserErr
}

func adapterTestConfig(issuer string, allowPrivate bool) json.RawMessage {
	raw, _ := json.Marshal(Config{
		ConfigurationMode: "discovery", TokenAuthMethod: "discovery", PKCEMethod: "S256",
		IssuerURL: issuer, ClientID: "client", Scopes: []string{"openid"},
		AllowedDomains: []string{}, UsernameClaim: "preferred_username",
		DisplayNameClaim: "name", EmailClaim: "email", PictureClaim: "picture",
		SubjectClaim:         "sub",
		RequireVerifiedEmail: true, AllowPrivateNetwork: allowPrivate,
	})
	return raw
}

func TestAdapterBeginAndAdvanceVerifiedIdentity(t *testing.T) {
	store := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
	secret, err := store.SealProviderSecret([]byte("client-secret"), 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	provider := federationcore.Provider{ID: 7, Slug: "corp", Protocol: Protocol, Config: adapterTestConfig("https://issuer.test", false), Secret: secret, SecretStatus: "valid"}
	adapter := NewAdapter(store)
	adapter.resolveConfig = func(_ context.Context, c Config) (ResolvedConfig, error) {
		return ResolvedConfig{Issuer: c.IssuerURL, AuthorizationEndpoint: c.IssuerURL + "/auth", TokenEndpoint: c.IssuerURL + "/token", JWKSEndpoint: c.IssuerURL + "/keys", PKCEMethod: c.PKCEMethod, TokenAuthMethod: "client_secret_basic", Scopes: c.Scopes}, nil
	}
	adapter.newClient = func(context.Context, Config, ResolvedConfig, string, string) (clientAPI, error) {
		return &adapterFakeClient{tokens: &Tokens{IDToken: "fake-jwt", Issuer: "https://issuer.test", Subject: "sub", EmailVerified: true, AMR: []string{"pwd"}, Raw: map[string]any{"preferred_username": "alice", "name": "Alice", "email": "alice@example.com", "picture": "https://cdn.test/a.png"}}}, nil
	}
	state, action, err := adapter.Begin(context.Background(), provider, federationcore.BeginContext{Intent: federationcore.IntentLogin, FlowID: "flow", CallbackURL: "https://idp.test/callback"})
	if err != nil {
		t.Fatal(err)
	}
	if action.Kind != federationcore.ActionRedirect || action.URL == "" {
		t.Fatalf("action = %+v", action)
	}
	result, err := adapter.Advance(context.Background(), provider, state, federationcore.ActionInput{Kind: federationcore.ActionRedirect, Code: "code", Issuer: "https://issuer.test"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Identity == nil || result.Identity.Username != "alice" || result.Identity.DisplayName != "Alice" || result.Identity.Email == nil || *result.Identity.Email != "alice@example.com" || !result.Identity.EmailVerificationSupported || result.Identity.AvatarURL != "https://cdn.test/a.png" {
		t.Fatalf("identity = %+v", result.Identity)
	}
	if len(result.State) != 0 || result.Next != nil {
		t.Fatalf("terminal result carried state/action: %+v", result)
	}
}

func TestAdapterDefersUserInfoAvatarFallback(t *testing.T) {
	store := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
	secret, err := store.SealProviderSecret([]byte("client-secret"), 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	provider := federationcore.Provider{
		ID: 7, Slug: "corp", Protocol: Protocol,
		Config:       adapterTestConfig("https://issuer.test", false),
		Secret:       secret,
		SecretStatus: "valid",
	}
	adapter := NewAdapter(store)
	client := &adapterFakeClient{
		tokens: &Tokens{
			IDToken: "fake-jwt", Issuer: "https://issuer.test", Subject: "sub", AccessToken: "access-token",
			Raw: map[string]any{},
		},
		userInfo: map[string]any{"picture": "https://cdn.test/fallback.png"},
	}
	adapter.resolveConfig = func(_ context.Context, c Config) (ResolvedConfig, error) {
		return ResolvedConfig{Issuer: c.IssuerURL, AuthorizationEndpoint: c.IssuerURL + "/auth", TokenEndpoint: c.IssuerURL + "/token", JWKSEndpoint: c.IssuerURL + "/keys", PKCEMethod: c.PKCEMethod, TokenAuthMethod: "client_secret_basic", Scopes: c.Scopes}, nil
	}
	adapter.newClient = func(context.Context, Config, ResolvedConfig, string, string) (clientAPI, error) {
		return client, nil
	}
	state, _, err := adapter.Begin(context.Background(), provider, federationcore.BeginContext{
		Intent: federationcore.IntentLogin, FlowID: "flow", CallbackURL: "https://idp.test/callback",
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := adapter.Advance(context.Background(), provider, state, federationcore.ActionInput{
		Kind: federationcore.ActionRedirect, Code: "code",
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.userInfoCalls != 0 {
		t.Fatalf("Advance made %d synchronous UserInfo calls", client.userInfoCalls)
	}
	if result.Identity == nil || result.Identity.AvatarURL != "" || result.Avatar == nil {
		t.Fatalf("terminal avatar delivery = %+v identity=%+v", result.Avatar, result.Identity)
	}

	avatarURL, err := adapter.ResolveAvatar(context.Background(), provider, *result.Avatar)
	if err != nil {
		t.Fatal(err)
	}
	if avatarURL != "https://cdn.test/fallback.png" || client.userInfoCalls != 1 {
		t.Fatalf("detached fallback URL = %q, UserInfo calls = %d", avatarURL, client.userInfoCalls)
	}
}

func TestAdapterAdvanceAllowsOptionalAuthorizationResponseIssuer(t *testing.T) {
	store := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
	secret, err := store.SealProviderSecret([]byte("client-secret"), 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	provider := federationcore.Provider{
		ID: 7, Slug: "corp", Protocol: Protocol,
		Config:       adapterTestConfig("https://issuer.test", false),
		Secret:       secret,
		SecretStatus: "valid",
	}
	adapter := NewAdapter(store)
	adapter.resolveConfig = func(_ context.Context, c Config) (ResolvedConfig, error) {
		return ResolvedConfig{Issuer: c.IssuerURL, AuthorizationEndpoint: c.IssuerURL + "/auth", TokenEndpoint: c.IssuerURL + "/token", JWKSEndpoint: c.IssuerURL + "/keys", PKCEMethod: c.PKCEMethod, TokenAuthMethod: "client_secret_basic", Scopes: c.Scopes}, nil
	}
	adapter.newClient = func(context.Context, Config, ResolvedConfig, string, string) (clientAPI, error) {
		return &adapterFakeClient{tokens: &Tokens{IDToken: "fake-jwt", Issuer: "https://issuer.test", Subject: "sub"}}, nil
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
		Config:       adapterTestConfig("https://issuer.test", false),
		Secret:       secret,
		SecretStatus: "valid",
	}
	adapter := NewAdapter(store)
	builds := 0
	adapter.resolveConfig = func(_ context.Context, c Config) (ResolvedConfig, error) {
		return ResolvedConfig{Issuer: c.IssuerURL, AuthorizationEndpoint: c.IssuerURL + "/auth", TokenEndpoint: c.IssuerURL + "/token", JWKSEndpoint: c.IssuerURL + "/keys", PKCEMethod: c.PKCEMethod, TokenAuthMethod: "client_secret_basic", Scopes: c.Scopes}, nil
	}
	adapter.newClient = func(context.Context, Config, ResolvedConfig, string, string) (clientAPI, error) {
		builds++
		return &adapterFakeClient{tokens: &Tokens{IDToken: "fake-jwt", Issuer: "https://issuer.test", Subject: "sub"}}, nil
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
		Config:       adapterTestConfig("https://issuer.test", false),
		Secret:       secret,
		SecretStatus: "valid",
	}
	other := provider
	other.Slug = "other"
	adapter := NewAdapter(store)
	builds := 0
	adapter.resolveConfig = func(_ context.Context, c Config) (ResolvedConfig, error) {
		return ResolvedConfig{Issuer: c.IssuerURL, AuthorizationEndpoint: c.IssuerURL + "/auth", TokenEndpoint: c.IssuerURL + "/token", JWKSEndpoint: c.IssuerURL + "/keys", PKCEMethod: c.PKCEMethod, TokenAuthMethod: "client_secret_basic", Scopes: c.Scopes}, nil
	}
	adapter.newClient = func(context.Context, Config, ResolvedConfig, string, string) (clientAPI, error) {
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
		Config:       adapterTestConfig("https://issuer.test", false),
		Secret:       secretV1,
		SecretStatus: "valid",
	}
	adapter := NewAdapter(store)
	builds := 0
	adapter.resolveConfig = func(_ context.Context, c Config) (ResolvedConfig, error) {
		return ResolvedConfig{Issuer: c.IssuerURL, AuthorizationEndpoint: c.IssuerURL + "/auth", TokenEndpoint: c.IssuerURL + "/token", JWKSEndpoint: c.IssuerURL + "/keys", PKCEMethod: c.PKCEMethod, TokenAuthMethod: "client_secret_basic", Scopes: c.Scopes}, nil
	}
	adapter.newClient = func(context.Context, Config, ResolvedConfig, string, string) (clientAPI, error) {
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
		Config:       adapterTestConfig("https://issuer.test", false),
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
	adapter.resolveConfig = func(_ context.Context, c Config) (ResolvedConfig, error) {
		return ResolvedConfig{Issuer: c.IssuerURL, AuthorizationEndpoint: c.IssuerURL + "/auth", TokenEndpoint: c.IssuerURL + "/token", JWKSEndpoint: c.IssuerURL + "/keys", PKCEMethod: c.PKCEMethod, TokenAuthMethod: "client_secret_basic", Scopes: c.Scopes}, nil
	}
	adapter.newClient = func(context.Context, Config, ResolvedConfig, string, string) (clientAPI, error) {
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
		Config:       adapterTestConfig("https://issuer.test", true),
		Secret:       secret,
		SecretStatus: "valid",
	}
	adapter := NewAdapter(store)
	builds := 0
	var policies []bool
	adapter.resolveConfig = func(_ context.Context, c Config) (ResolvedConfig, error) {
		return ResolvedConfig{Issuer: c.IssuerURL, AuthorizationEndpoint: c.IssuerURL + "/auth", TokenEndpoint: c.IssuerURL + "/token", JWKSEndpoint: c.IssuerURL + "/keys", PKCEMethod: c.PKCEMethod, TokenAuthMethod: "client_secret_basic", Scopes: c.Scopes}, nil
	}
	adapter.newClient = func(_ context.Context, config Config, _ ResolvedConfig, _, _ string) (clientAPI, error) {
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
	provider.Config = adapterTestConfig("https://issuer.test", false)
	begin()
	begin()
	if builds != 2 {
		t.Fatalf("client builds = %d, want 2 across private-network policy change", builds)
	}
	if len(policies) != 2 || !policies[0] || policies[1] {
		t.Fatalf("client build policies = %v, want [true false]", policies)
	}
}

func TestAdapterAdvanceClassifiesIssuerAndExchangeFailures(t *testing.T) {
	tests := []struct {
		name        string
		issuer      string
		exchangeErr error
		wantReason  federationcore.FailureReason
	}{
		{name: "authorization response issuer", issuer: "https://attacker.test", wantReason: federationcore.FailureIssuerMismatch},
		{name: "code exchange", exchangeErr: errors.New("token endpoint refused code"), wantReason: federationcore.FailureCodeExchange},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
			secret, err := store.SealProviderSecret([]byte("client-secret"), 7, 1)
			if err != nil {
				t.Fatal(err)
			}
			provider := federationcore.Provider{
				ID: 7, Slug: "corp", Protocol: Protocol,
				Config: adapterTestConfig("https://issuer.test", false),
				Secret: secret, SecretStatus: "valid",
			}
			adapter := NewAdapter(store)
			client := &adapterFakeClient{
				tokens:      &Tokens{Issuer: "https://issuer.test", Subject: "sub"},
				exchangeErr: test.exchangeErr,
			}
			adapter.resolveConfig = func(_ context.Context, c Config) (ResolvedConfig, error) {
				return ResolvedConfig{Issuer: c.IssuerURL, AuthorizationEndpoint: c.IssuerURL + "/auth", TokenEndpoint: c.IssuerURL + "/token", JWKSEndpoint: c.IssuerURL + "/keys", PKCEMethod: c.PKCEMethod, TokenAuthMethod: "client_secret_basic", Scopes: c.Scopes}, nil
			}
			adapter.newClient = func(context.Context, Config, ResolvedConfig, string, string) (clientAPI, error) {
				return client, nil
			}
			state, _, err := adapter.Begin(context.Background(), provider, federationcore.BeginContext{
				Intent: federationcore.IntentLogin, FlowID: "flow", CallbackURL: "https://idp.test/callback",
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = adapter.Advance(context.Background(), provider, state, federationcore.ActionInput{
				Kind: federationcore.ActionRedirect, Code: "code", Issuer: test.issuer,
			})
			if ae := authn.AsAuthError(err); ae == nil || ae.Code != "federation_state_invalid" {
				t.Fatalf("public error = %v, want federation_state_invalid", err)
			}
			if reason, ok := federationcore.FailureReasonOf(err); !ok || reason != test.wantReason {
				t.Fatalf("failure reason = %q, want %q", reason, test.wantReason)
			}
		})
	}
}

// TestAdapterAdvanceUserinfoFallback drives the no-id_token path end to end on
// the fake client: identity fields come from userinfo claims, the subject from
// subjectClaim, the issuer from the flow state, and the picture is delivered
// directly instead of as a deferred reference.
func TestAdapterAdvanceUserinfoFallback(t *testing.T) {
	store := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
	secret, err := store.SealProviderSecret([]byte("client-secret"), 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	provider := federationcore.Provider{
		ID: 7, Slug: "corp", Protocol: Protocol,
		Config: json.RawMessage(strings.Replace(strings.Replace(string(adapterTestConfig("https://issuer.test", false)), `"subjectClaim":"sub"`, `"subjectClaim":"id"`, 1), `"usernameClaim":"preferred_username"`, `"usernameClaim":"login"`, 1)),
		Secret: secret, SecretStatus: "valid",
	}
	adapter := NewAdapter(store)
	client := &adapterFakeClient{
		tokens: &Tokens{AccessToken: "at_fallback", TokenType: "Bearer"},
		rawUserInfo: map[string]any{
			"id": json.Number("67890"), "login": "octocat", "name": "Octo Cat",
			"email": "octo@example.test", "email_verified": false,
			"picture": "https://cdn.test/octo.png",
		},
	}
	adapter.resolveConfig = func(_ context.Context, c Config) (ResolvedConfig, error) {
		return ResolvedConfig{Issuer: c.IssuerURL, AuthorizationEndpoint: c.IssuerURL + "/auth", TokenEndpoint: c.IssuerURL + "/token", JWKSEndpoint: c.IssuerURL + "/keys", PKCEMethod: c.PKCEMethod, TokenAuthMethod: "client_secret_basic", Scopes: c.Scopes}, nil
	}
	adapter.newClient = func(context.Context, Config, ResolvedConfig, string, string) (clientAPI, error) {
		return client, nil
	}
	state, _, err := adapter.Begin(context.Background(), provider, federationcore.BeginContext{
		Intent: federationcore.IntentLogin, FlowID: "flow", CallbackURL: "https://idp.test/callback",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Advance(context.Background(), provider, state, federationcore.ActionInput{
		Kind: federationcore.ActionRedirect, Code: "code",
	})
	if err != nil {
		t.Fatal(err)
	}
	identity := result.Identity
	if identity == nil {
		t.Fatal("Advance returned no identity")
	}
	if identity.Subject != "67890" {
		t.Errorf("Subject = %q, want numeric subjectClaim value 67890", identity.Subject)
	}
	if identity.Issuer != "https://issuer.test" {
		t.Errorf("Issuer = %q, want flow state ExpectedIss", identity.Issuer)
	}
	if identity.Username != "octocat" || identity.DisplayName != "Octo Cat" {
		t.Errorf("Username/DisplayName = %q/%q, want octocat/Octo Cat", identity.Username, identity.DisplayName)
	}
	if identity.Email == nil || *identity.Email != "octo@example.test" {
		t.Errorf("Email = %v, want octo@example.test", identity.Email)
	}
	if identity.EmailVerified {
		t.Error("EmailVerified = true, want false (email_verified false in claims)")
	}
	if !identity.EmailVerificationSupported {
		t.Error("EmailVerificationSupported = false, want true so requireVerifiedEmail keeps gating")
	}
	if identity.AMR != nil {
		t.Errorf("AMR = %v, want nil on the userinfo path", identity.AMR)
	}
	if result.Avatar == nil || result.Avatar.URL != "https://cdn.test/octo.png" {
		t.Errorf("Avatar = %+v, want direct picture URL", result.Avatar)
	}
	if result.Avatar != nil && result.Avatar.Opaque != nil {
		t.Errorf("Avatar.Opaque = %v, want nil (no deferred fetch on userinfo path)", result.Avatar.Opaque)
	}
}

// TestAdapterAdvanceUserinfoFallbackEmailVerifiedFromClaims pins the read of
// email_verified out of userinfo claims: a true claim flows into EmailVerified
// (which a requireVerifiedEmail resolver turns into a pass), a false claim and
// a missing claim both read as false — and are then refused by that resolver.
func TestAdapterAdvanceUserinfoFallbackEmailVerifiedFromClaims(t *testing.T) {
	for name, verified := range map[string]any{"true": true, "false": false, "absent": nil} {
		t.Run(name, func(t *testing.T) {
			store := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
			secret, err := store.SealProviderSecret([]byte("client-secret"), 7, 1)
			if err != nil {
				t.Fatal(err)
			}
			provider := federationcore.Provider{
				ID: 7, Slug: "corp", Protocol: Protocol,
				Config: json.RawMessage(strings.Replace(string(adapterTestConfig("https://issuer.test", false)), `"subjectClaim":"sub"`, `"subjectClaim":"id"`, 1)),
				Secret: secret, SecretStatus: "valid",
			}
			adapter := NewAdapter(store)
			claims := map[string]any{
				"id": json.Number("67890"), "login": "octocat",
				"email": "octo@example.test", "picture": "https://cdn.test/octo.png",
			}
			if verified != nil {
				claims["email_verified"] = verified
			}
			client := &adapterFakeClient{
				tokens:      &Tokens{AccessToken: "at_fallback", TokenType: "Bearer"},
				rawUserInfo: claims,
			}
			adapter.resolveConfig = func(_ context.Context, c Config) (ResolvedConfig, error) {
				return ResolvedConfig{Issuer: c.IssuerURL, AuthorizationEndpoint: c.IssuerURL + "/auth", TokenEndpoint: c.IssuerURL + "/token", JWKSEndpoint: c.IssuerURL + "/keys", PKCEMethod: c.PKCEMethod, TokenAuthMethod: "client_secret_basic", Scopes: c.Scopes}, nil
			}
			adapter.newClient = func(context.Context, Config, ResolvedConfig, string, string) (clientAPI, error) {
				return client, nil
			}
			state, _, err := adapter.Begin(context.Background(), provider, federationcore.BeginContext{
				Intent: federationcore.IntentLogin, FlowID: "flow", CallbackURL: "https://idp.test/callback",
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := adapter.Advance(context.Background(), provider, state, federationcore.ActionInput{
				Kind: federationcore.ActionRedirect, Code: "code",
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Identity == nil {
				t.Fatal("Advance returned no identity")
			}
			want := verified == true
			if result.Identity.EmailVerified != want {
				t.Errorf("EmailVerified = %v, want %v", result.Identity.EmailVerified, want)
			}
			if !result.Identity.EmailVerificationSupported {
				t.Error("EmailVerificationSupported = false, want true")
			}
		})
	}
}

// TestAdapterAdvanceClassifiesUserinfoFallbackFailures pins the failure
// mapping: a missing/unusable subjectClaim value and a userinfo fetch failure
// both surface as upstream_identity_unavailable while the public error stays
// federation_state_invalid; the raw upstream text rides the cause.
func TestAdapterAdvanceClassifiesUserinfoFallbackFailures(t *testing.T) {
	tests := []struct {
		name       string
		client     *adapterFakeClient
		wantSubstr string
	}{
		{
			name: "subject claim absent",
			client: &adapterFakeClient{
				tokens:      &Tokens{AccessToken: "at", TokenType: "Bearer"},
				rawUserInfo: map[string]any{"login": "octocat"},
			},
			wantSubstr: `"sub"`,
		},
		{
			name: "userinfo request failed",
			client: &adapterFakeClient{
				tokens:     &Tokens{AccessToken: "at", TokenType: "Bearer"},
				rawUserErr: errors.New("userinfo: status 401"),
			},
			wantSubstr: "status 401",
		},
		{
			// Unparseable id_token (form-encoded response, malformed JWT):
			// Exchange wraps it in errUpstreamIdentityParse, and the adapter
			// must classify it as upstream_identity_unavailable rather than
			// code_exchange_failed, so the operator log says "no usable
			// identity" instead of "the code was bad".
			name: "unparseable id_token",
			client: &adapterFakeClient{
				exchangeErr: fmt.Errorf("%w: federation/oidc: code exchange: %w", errUpstreamIdentityParse, oidclib.ErrParse),
			},
			wantSubstr: errUpstreamIdentityParse.Error(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
			secret, err := store.SealProviderSecret([]byte("client-secret"), 7, 1)
			if err != nil {
				t.Fatal(err)
			}
			provider := federationcore.Provider{
				ID: 7, Slug: "corp", Protocol: Protocol,
				Config: adapterTestConfig("https://issuer.test", false),
				Secret: secret, SecretStatus: "valid",
			}
			adapter := NewAdapter(store)
			adapter.resolveConfig = func(_ context.Context, c Config) (ResolvedConfig, error) {
				return ResolvedConfig{Issuer: c.IssuerURL, AuthorizationEndpoint: c.IssuerURL + "/auth", TokenEndpoint: c.IssuerURL + "/token", JWKSEndpoint: c.IssuerURL + "/keys", PKCEMethod: c.PKCEMethod, TokenAuthMethod: "client_secret_basic", Scopes: c.Scopes}, nil
			}
			adapter.newClient = func(context.Context, Config, ResolvedConfig, string, string) (clientAPI, error) {
				return test.client, nil
			}
			state, _, err := adapter.Begin(context.Background(), provider, federationcore.BeginContext{
				Intent: federationcore.IntentLogin, FlowID: "flow", CallbackURL: "https://idp.test/callback",
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = adapter.Advance(context.Background(), provider, state, federationcore.ActionInput{
				Kind: federationcore.ActionRedirect, Code: "code",
			})
			if ae := authn.AsAuthError(err); ae == nil || ae.Code != "federation_state_invalid" {
				t.Fatalf("public error = %v, want federation_state_invalid", err)
			}
			if reason, ok := federationcore.FailureReasonOf(err); !ok || reason != federationcore.FailureUpstreamNoIdentity {
				t.Fatalf("failure reason = %q, want upstream_identity_unavailable", reason)
			}
			// The raw upstream text rides the cause, not Error() (which stays
			// "federation flow failed"): recover it through the multi-error
			// Unwrap and match the cause text.
			multi, isMulti := err.(interface{ Unwrap() []error })
			if !isMulti {
				t.Fatal("flow failure lost its Unwrap() []error")
			}
			found := false
			for _, unwrapped := range multi.Unwrap() {
				if unwrapped != nil && strings.Contains(unwrapped.Error(), test.wantSubstr) {
					found = true
				}
			}
			if !found {
				t.Errorf("unwrapped causes do not carry upstream text %q", test.wantSubstr)
			}
		})
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
		Config:       adapterTestConfig("https://issuer.test", false),
		Secret:       secret,
		SecretStatus: "valid",
	}
	adapter := NewAdapter(store)
	exchanges := 0
	adapter.resolveConfig = func(_ context.Context, c Config) (ResolvedConfig, error) {
		return ResolvedConfig{Issuer: c.IssuerURL, AuthorizationEndpoint: c.IssuerURL + "/auth", TokenEndpoint: c.IssuerURL + "/token", JWKSEndpoint: c.IssuerURL + "/keys", PKCEMethod: c.PKCEMethod, TokenAuthMethod: "client_secret_basic", Scopes: c.Scopes}, nil
	}
	adapter.newClient = func(context.Context, Config, ResolvedConfig, string, string) (clientAPI, error) {
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
	if reason, ok := federationcore.FailureReasonOf(err); !ok || reason != federationcore.FailureTokenEndpointDrift {
		t.Fatalf("Advance failure reason = %q, want token_endpoint_drift", reason)
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
		Config:       adapterTestConfig(server.URL, true),
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
	provider := federationcore.Provider{Protocol: Protocol, Config: json.RawMessage(validDefinitionConfig), Secret: &federationcore.SealedSecret{Ciphertext: []byte{1}, Nonce: []byte{2}, KeyVersion: 1}, SecretStatus: "valid"}
	if !definition.Ready(provider) {
		t.Fatal("valid provider not ready")
	}
	provider.SecretStatus = "invalid"
	if definition.Ready(provider) {
		t.Fatal("invalid secret status ready")
	}
	if err := definition.ValidateSecret(adapterTestConfig("https://issuer.test", false), nil); err == nil {
		t.Fatal("empty secret accepted")
	}
	if err := definition.ValidateConfig(json.RawMessage(`{"issuerUrl":"http://issuer.test"}`)); err == nil {
		t.Fatal("invalid config accepted")
	}
}
