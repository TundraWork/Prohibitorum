package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	oidclib "github.com/zitadel/oidc/v3/pkg/oidc"

	"prohibitorum/cmd/smoke/mockop"
	"prohibitorum/pkg/authn"
	federationcore "prohibitorum/pkg/federation"
)

type adapterFakeClient struct {
	tokens         *Tokens
	implicitTokens *Tokens
	tokenEndpoint  string
	exchanges      *int
	verifies       *int
	userInfo       map[string]any
	userInfoCalls  int
	userInfoErr    error
	rawUserInfo    map[string]any
	rawUserErr     error
	exchangeErr    error
	implicitErr    error
}

func (c *adapterFakeClient) Issuer() string { return "https://issuer.test" }
func (c *adapterFakeClient) TokenEndpoint() string {
	if c.tokenEndpoint != "" {
		return c.tokenEndpoint
	}
	return "https://issuer.test/token"
}
func (c *adapterFakeClient) AuthURL(state, nonce, challenge string) string {
	return "https://issuer.test/auth?response_type=code&state=" + state + "&nonce=" + nonce + "&challenge=" + challenge
}
func (c *adapterFakeClient) ImplicitAuthURL(state, nonce string) string {
	return "https://issuer.test/auth?response_type=id_token&state=" + state + "&nonce=" + nonce
}
func (c *adapterFakeClient) Exchange(context.Context, string, string, string, string) (*Tokens, error) {
	if c.exchanges != nil {
		*c.exchanges = *c.exchanges + 1
	}
	return c.tokens, c.exchangeErr
}
func (c *adapterFakeClient) VerifyImplicit(_ context.Context, _, _, _ string) (*Tokens, error) {
	if c.verifies != nil {
		*c.verifies = *c.verifies + 1
	}
	if c.implicitErr != nil {
		return nil, c.implicitErr
	}
	return c.implicitTokens, nil
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

// adapterPublicConfig is a public client with the openid scope, set to the
// given sign-in response; "" leaves the setting out of the saved JSON.
func adapterPublicConfig(issuer, responseType string) json.RawMessage {
	raw, _ := json.Marshal(Config{
		ConfigurationMode: "discovery", TokenAuthMethod: "none", PKCEMethod: "S256",
		IssuerURL: issuer, ClientID: "client", Scopes: []string{"openid", "profile"},
		AllowedDomains: []string{}, UsernameClaim: "preferred_username",
		DisplayNameClaim: "name", EmailClaim: "email", PictureClaim: "picture",
		SubjectClaim: "sub", RequireVerifiedEmail: true, ResponseType: responseType,
	})
	return raw
}

// responseTypeHarness runs Begin and Advance against a fake client that
// counts code exchanges and id_token verifications separately, so a test can
// tell which path a callback took.
type responseTypeHarness struct {
	adapter              *Adapter
	provider             federationcore.Provider
	exchanges, verifies  int
	codeTokens, idTokens *Tokens
}

func newResponseTypeHarness(responseType string) *responseTypeHarness {
	h := &responseTypeHarness{
		provider:   federationcore.Provider{ID: 7, Slug: "public", Protocol: Protocol, Config: adapterPublicConfig("https://issuer.test", responseType)},
		codeTokens: &Tokens{IDToken: "back-channel-jwt", AccessToken: "at", Issuer: "https://issuer.test", Subject: "sub-code", Raw: map[string]any{"preferred_username": "coder"}},
		idTokens:   &Tokens{IDToken: "front-channel-jwt", Issuer: "https://issuer.test", Subject: "sub-implicit", EmailVerified: true, Raw: map[string]any{"preferred_username": "imp", "name": "Imp User"}},
	}
	h.adapter = NewAdapter(federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)}))
	h.adapter.resolveConfig = func(_ context.Context, c Config) (ResolvedConfig, error) {
		return ResolvedConfig{Issuer: c.IssuerURL, AuthorizationEndpoint: c.IssuerURL + "/auth", TokenEndpoint: c.IssuerURL + "/token", JWKSEndpoint: c.IssuerURL + "/keys", PKCEMethod: c.PKCEMethod, TokenAuthMethod: "none", ResponseType: c.responseType(), Scopes: c.Scopes}, nil
	}
	h.adapter.newClient = func(context.Context, Config, ResolvedConfig, string, string) (clientAPI, error) {
		return &adapterFakeClient{tokens: h.codeTokens, implicitTokens: h.idTokens, exchanges: &h.exchanges, verifies: &h.verifies, userInfo: map[string]any{"sub": "sub-code"}}, nil
	}
	return h
}

func (h *responseTypeHarness) begin(t *testing.T) (json.RawMessage, adapterState, string) {
	t.Helper()
	raw, action, err := h.adapter.Begin(context.Background(), h.provider, federationcore.BeginContext{Intent: federationcore.IntentLogin, FlowID: "flow", CallbackURL: "https://idp.test/callback"})
	if err != nil {
		t.Fatal(err)
	}
	var state adapterState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	return raw, state, action.URL
}

// TestAdapterIDTokenOnlyFlow drives a provider set to ID token only through
// Begin and Advance: the redirect asks for response_type=id_token without
// PKCE, the callback's front-channel id_token becomes the identity, and no
// code exchange happens.
func TestAdapterIDTokenOnlyFlow(t *testing.T) {
	h := newResponseTypeHarness(responseTypeIDToken)
	raw, state, authURL := h.begin(t)
	if !strings.Contains(authURL, "response_type=id_token") || strings.Contains(authURL, "challenge") {
		t.Fatalf("authorization URL = %s, want the implicit request without PKCE", authURL)
	}
	if !state.Implicit || state.CodeVerifier != "" {
		t.Fatalf("state = %+v, want implicit with no code verifier", state)
	}
	result, err := h.adapter.Advance(context.Background(), h.provider, raw, federationcore.ActionInput{Kind: federationcore.ActionRedirect, IDToken: "front-channel-jwt"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Identity == nil || result.Identity.Subject != "sub-implicit" || result.Identity.Username != "imp" {
		t.Fatalf("identity = %+v", result.Identity)
	}
	if h.verifies != 1 || h.exchanges != 0 {
		t.Fatalf("verifies/exchanges = %d/%d, want 1/0", h.verifies, h.exchanges)
	}
}

// TestAdapterResponseTypeDefaultsToCode pins that a configuration without the
// setting keeps the code flow with PKCE, even for a public client with the
// openid scope: providers saved before the setting existed do not change.
func TestAdapterResponseTypeDefaultsToCode(t *testing.T) {
	if strings.Contains(string(adapterPublicConfig("https://issuer.test", "")), "responseType") {
		t.Fatal("fixture still carries responseType")
	}
	config, err := decodeConfig(adapterPublicConfig("https://issuer.test", ""))
	if err != nil {
		t.Fatalf("a configuration without responseType must still decode: %v", err)
	}
	if config.responseType() != responseTypeCode {
		t.Fatalf("unset response type reads as %q", config.responseType())
	}
	h := newResponseTypeHarness("")
	raw, state, authURL := h.begin(t)
	if !strings.Contains(authURL, "response_type=code") || !strings.Contains(authURL, "challenge=") {
		t.Fatalf("authorization URL = %s, want the code request with PKCE", authURL)
	}
	if state.Implicit || state.CodeVerifier == "" || state.Resolved.ResponseType != responseTypeCode {
		t.Fatalf("state = %+v, want the code flow with a verifier", state)
	}
	if _, err := h.adapter.Advance(context.Background(), h.provider, raw, federationcore.ActionInput{Kind: federationcore.ActionRedirect, Code: "code"}); err != nil {
		t.Fatal(err)
	}
	if h.exchanges != 1 || h.verifies != 0 {
		t.Fatalf("exchanges/verifies = %d/%d, want 1/0", h.exchanges, h.verifies)
	}
}

// TestAdapterCodeFlowIgnoresInjectedIDToken pins that an id_token added to a
// code-flow callback is never verified as the identity: the flow state, not
// the callback, decides the path, so the implicit flow cannot be forced onto
// a provider that did not opt in.
func TestAdapterCodeFlowIgnoresInjectedIDToken(t *testing.T) {
	h := newResponseTypeHarness(responseTypeCode)
	raw, _, _ := h.begin(t)
	result, err := h.adapter.Advance(context.Background(), h.provider, raw, federationcore.ActionInput{Kind: federationcore.ActionRedirect, Code: "code", IDToken: "front-channel-jwt"})
	if err != nil {
		t.Fatal(err)
	}
	if h.verifies != 0 || h.exchanges != 1 {
		t.Fatalf("verifies/exchanges = %d/%d, want 0/1", h.verifies, h.exchanges)
	}
	if result.Identity == nil || result.Identity.Subject != "sub-code" {
		t.Fatalf("identity = %+v, want the exchanged one", result.Identity)
	}
}

// TestAdapterImplicitFlowIgnoresCode pins the other direction: a code on an
// implicit callback is never exchanged, and without an id_token the callback
// fails.
func TestAdapterImplicitFlowIgnoresCode(t *testing.T) {
	h := newResponseTypeHarness(responseTypeIDToken)
	raw, _, _ := h.begin(t)
	_, err := h.adapter.Advance(context.Background(), h.provider, raw, federationcore.ActionInput{Kind: federationcore.ActionRedirect, Code: "code"})
	if reason, ok := federationcore.FailureReasonOf(err); !ok || reason != federationcore.FailureCodeExchange {
		t.Fatalf("Advance error = %v, want code_exchange_failed", err)
	}
	if h.exchanges != 0 || h.verifies != 0 {
		t.Fatalf("exchanges/verifies = %d/%d, want 0/0", h.exchanges, h.verifies)
	}
	result, err := h.adapter.Advance(context.Background(), h.provider, raw, federationcore.ActionInput{Kind: federationcore.ActionRedirect, Code: "code", IDToken: "front-channel-jwt"})
	if err != nil {
		t.Fatal(err)
	}
	if h.exchanges != 0 || h.verifies != 1 || result.Identity == nil || result.Identity.Subject != "sub-implicit" {
		t.Fatalf("exchanges/verifies = %d/%d, identity = %+v", h.exchanges, h.verifies, result.Identity)
	}
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
	if result.Identity.UserInfoFallback {
		t.Error("ID-token identity marked as userinfo fallback")
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
	if !identity.UserInfoFallback {
		t.Error("UserInfoFallback = false, want true")
	}
	if result.Avatar == nil || result.Avatar.URL != "https://cdn.test/octo.png" {
		t.Errorf("Avatar = %+v, want direct picture URL", result.Avatar)
	}
	if result.Avatar != nil && result.Avatar.Opaque != nil {
		t.Errorf("Avatar.Opaque = %v, want nil (no deferred fetch on userinfo path)", result.Avatar.Opaque)
	}
}

// TestAdapterAdvanceUserinfoFallbackNestedClaimPaths drives the no-id_token
// path with every profile claim configured as a gjson path (PHB-121):
// username, display name, email and avatar read from inside nested objects,
// and the subject from an array element, the way an admin would map a
// bgm.tv-shaped or GitHub-shaped userinfo document without flat claims.
func TestAdapterAdvanceUserinfoFallbackNestedClaimPaths(t *testing.T) {
	store := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
	secret, err := store.SealProviderSecret([]byte("client-secret"), 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	// Every claim override is a path; none of the names exist as flat keys.
	provider := federationcore.Provider{
		ID: 7, Slug: "corp", Protocol: Protocol,
		Config: json.RawMessage(strings.NewReplacer(
			`"subjectClaim":"sub"`, `"subjectClaim":"identities.0.id"`,
			`"usernameClaim":"preferred_username"`, `"usernameClaim":"profile.login"`,
			`"displayNameClaim":"name"`, `"displayNameClaim":"profile.display_name"`,
			`"emailClaim":"email"`, `"emailClaim":"contacts.0.email"`,
			`"pictureClaim":"picture"`, `"pictureClaim":"avatar.large"`,
		).Replace(string(adapterTestConfig("https://issuer.test", false)))),
		Secret: secret, SecretStatus: "valid",
	}
	adapter := NewAdapter(store)
	client := &adapterFakeClient{
		tokens: &Tokens{AccessToken: "at_fallback", TokenType: "Bearer"},
		rawUserInfo: map[string]any{
			"identities": []any{map[string]any{"id": json.Number("67890")}},
			"profile": map[string]any{
				"login": "octocat", "display_name": "Octo Cat",
			},
			"contacts": []any{
				map[string]any{"email": "octo@example.test", "primary": true},
			},
			"avatar": map[string]any{
				"large": "https://cdn.test/octo-large.png",
			},
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
		t.Errorf("Subject = %q, want nested subjectClaim identities.0.id value 67890", identity.Subject)
	}
	if identity.Username != "octocat" || identity.DisplayName != "Octo Cat" {
		t.Errorf("Username/DisplayName = %q/%q, want octocat/Octo Cat via profile.* paths", identity.Username, identity.DisplayName)
	}
	if identity.Email == nil || *identity.Email != "octo@example.test" {
		t.Errorf("Email = %v, want octo@example.test via contacts.0.email", identity.Email)
	}
	if result.Avatar == nil || result.Avatar.URL != "https://cdn.test/octo-large.png" {
		t.Errorf("Avatar = %+v, want avatar.large URL", result.Avatar)
	}
}

// TestAdapterAdvanceIDTokenNestedClaimPaths is the id_token-path twin: the
// same path overrides read from Tokens.Raw, which the code flow fills with
// extras plus hoisted standard claims. The deferred avatar reference keeps
// the pictureClaim path, and the diagnostics picture hint (below) relies on
// the same lookup.
func TestAdapterAdvanceIDTokenNestedClaimPaths(t *testing.T) {
	store := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
	secret, err := store.SealProviderSecret([]byte("client-secret"), 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	provider := federationcore.Provider{
		ID: 7, Slug: "corp", Protocol: Protocol,
		Config: json.RawMessage(strings.NewReplacer(
			`"usernameClaim":"preferred_username"`, `"usernameClaim":"profile.login"`,
			`"displayNameClaim":"name"`, `"displayNameClaim":"profile.display_name"`,
			`"emailClaim":"email"`, `"emailClaim":"contacts.0.email"`,
			`"pictureClaim":"picture"`, `"pictureClaim":"avatar.large"`,
		).Replace(string(adapterTestConfig("https://issuer.test", false)))),
		Secret: secret, SecretStatus: "valid",
	}
	adapter := NewAdapter(store)
	client := &adapterFakeClient{
		tokens: &Tokens{
			IDToken: "jwt", AccessToken: "at", TokenType: "Bearer",
			Subject: "sub-1", Issuer: "https://issuer.test",
			Raw: map[string]any{
				"profile": map[string]any{"login": "octocat", "display_name": "Octo Cat"},
				"contacts": []any{
					map[string]any{"email": "octo@example.test"},
				},
				"avatar": map[string]any{"large": "https://cdn.test/octo-large.png"},
			},
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
	if identity.Username != "octocat" || identity.DisplayName != "Octo Cat" {
		t.Errorf("Username/DisplayName = %q/%q, want octocat/Octo Cat via profile.* paths", identity.Username, identity.DisplayName)
	}
	if identity.Email == nil || *identity.Email != "octo@example.test" {
		t.Errorf("Email = %v, want octo@example.test via contacts.0.email", identity.Email)
	}
	if result.Avatar == nil || result.Avatar.URL != "https://cdn.test/octo-large.png" {
		t.Errorf("Avatar = %+v, want avatar.large URL from the id_token", result.Avatar)
	}
	run := func(t *testing.T, authenticate bool, accessToken string) federationcore.AvatarDelivery {
		t.Helper()
		raw := adapterTestConfig("https://issuer.test", false)
		if authenticate {
			raw = json.RawMessage(strings.Replace(string(raw), `"pkceMethod":"S256"`, `"pkceMethod":"S256","authenticateAvatarFetch":true`, 1))
		}
		provider := federationcore.Provider{ID: 7, Slug: "corp", Protocol: Protocol, Config: raw, Secret: secret, SecretStatus: "valid"}
		adapter := NewAdapter(store)
		client := &adapterFakeClient{tokens: &Tokens{
			IDToken: "fake-jwt", AccessToken: accessToken, TokenType: "Bearer",
			Issuer: "https://issuer.test", Subject: "sub",
			Raw: map[string]any{"picture": "https://issuer.test/avatar.png"},
		}}
		adapter.resolveConfig = func(_ context.Context, c Config) (ResolvedConfig, error) {
			return ResolvedConfig{Issuer: c.IssuerURL, AuthorizationEndpoint: c.IssuerURL + "/auth", TokenEndpoint: "https://tokens.issuer.test/token", UserInfoEndpoint: "https://issuer.test/userinfo", JWKSEndpoint: c.IssuerURL + "/keys", PKCEMethod: c.PKCEMethod, TokenAuthMethod: "client_secret_basic", Scopes: c.Scopes}, nil
		}
		adapter.newClient = func(context.Context, Config, ResolvedConfig, string, string) (clientAPI, error) {
			return client, nil
		}
		state, _, err := adapter.Begin(context.Background(), provider, federationcore.BeginContext{Intent: federationcore.IntentLogin, FlowID: "flow", CallbackURL: "https://idp.test/callback"})
		if err != nil {
			t.Fatal(err)
		}
		result, err := adapter.Advance(context.Background(), provider, state, federationcore.ActionInput{Kind: federationcore.ActionRedirect, Code: "code"})
		if err != nil {
			t.Fatal(err)
		}
		if result.Avatar == nil {
			t.Fatal("no avatar delivery")
		}
		return *result.Avatar
	}

	t.Run("flag on carries token and provider origins", func(t *testing.T) {
		delivery := run(t, true, "at_avatar")
		if delivery.BearerToken != "at_avatar" {
			t.Fatalf("BearerToken = %q", delivery.BearerToken)
		}
		want := []string{"https://issuer.test", "https://tokens.issuer.test"}
		if !reflect.DeepEqual(delivery.BearerOrigins, want) {
			t.Fatalf("BearerOrigins = %v, want %v", delivery.BearerOrigins, want)
		}
	})
	t.Run("flag off carries nothing", func(t *testing.T) {
		delivery := run(t, false, "at_avatar")
		if delivery.BearerToken != "" || delivery.BearerOrigins != nil {
			t.Fatalf("delivery = %+v, want anonymous", delivery)
		}
	})
	t.Run("flag on without access token carries nothing", func(t *testing.T) {
		delivery := run(t, true, "")
		if delivery.BearerToken != "" || delivery.BearerOrigins != nil {
			t.Fatalf("delivery = %+v, want anonymous", delivery)
		}
	})

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
