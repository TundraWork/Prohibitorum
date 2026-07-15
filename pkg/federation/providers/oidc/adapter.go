package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	federationcore "prohibitorum/pkg/federation"
)

const (
	Protocol       = "oidc"
	clientCacheTTL = 15 * time.Minute
)

type Config struct {
	IssuerURL           string   `json:"issuerUrl"`
	ClientID            string   `json:"clientId"`
	Scopes              []string `json:"scopes"`
	AllowedAlgorithms   []string `json:"allowedAlgorithms,omitempty"`
	UsernameClaim       string   `json:"usernameClaim,omitempty"`
	DisplayNameClaim    string   `json:"displayNameClaim,omitempty"`
	EmailClaim          string   `json:"emailClaim,omitempty"`
	PictureClaim        string   `json:"pictureClaim,omitempty"`
	AllowPrivateNetwork bool     `json:"allowPrivateNetwork,omitempty"`
}

type Definition struct{}

func (Definition) Protocol() string { return Protocol }
func (Definition) Descriptor() federationcore.Descriptor {
	return federationcore.Descriptor{Protocol: Protocol, SearchFields: []federationcore.SearchField{
		{Key: "subject", Operators: []federationcore.SearchOperator{federationcore.SearchExact}},
		{Key: "email", Operators: []federationcore.SearchOperator{federationcore.SearchExact, federationcore.SearchPrefix, federationcore.SearchContains}},
	}, RequiresSecret: true}
}
func (Definition) ValidateConfig(raw json.RawMessage) error {
	config, err := decodeConfig(raw)
	if err != nil { return err }
	if config.ClientID == "" { return errors.New("federation/oidc: client id is required") }
	if len(config.Scopes) == 0 { return errors.New("federation/oidc: scopes are required") }
	if !config.AllowPrivateNetwork {
		return federationcore.ValidateIssuerURL(config.IssuerURL)
	}
	issuer, err := url.Parse(config.IssuerURL)
	if err != nil || issuer.Host == "" || issuer.User != nil || (issuer.Scheme != "https" && issuer.Scheme != "http") {
		return errors.New("federation/oidc: invalid trusted issuer URL")
	}
	return nil
}
func (Definition) ValidateSecret(secret []byte) error {
	if len(secret) == 0 { return errors.New("federation/oidc: client secret is required") }
	return nil
}
func (d Definition) Ready(provider federationcore.Provider) bool {
	return provider.Protocol == Protocol && !provider.Disabled && provider.Secret != nil && provider.SecretStatus == "valid" && d.ValidateConfig(provider.Config) == nil
}

type clientAPI interface {
	Issuer() string
	TokenEndpoint() string
	AuthURL(string, string, string) string
	Exchange(context.Context, string, string, string, string) (*Tokens, error)
	UserInfo(context.Context, string, string) (map[string]any, error)
}

type clientWrapper struct{ client *Client }
func (c clientWrapper) Issuer() string { return c.client.Issuer() }
func (c clientWrapper) TokenEndpoint() string { return c.client.TokenEndpoint() }
func (c clientWrapper) AuthURL(state, nonce, challenge string) string { return c.client.AuthURL(state, nonce, challenge) }
func (c clientWrapper) Exchange(ctx context.Context, code, verifier, issuer, nonce string) (*Tokens, error) {
	return c.client.Exchange(ctx, code, verifier, issuer, nonce)
}
func (c clientWrapper) UserInfo(ctx context.Context, accessToken, subject string) (map[string]any, error) {
	return c.client.UserInfo(ctx, accessToken, subject)
}

type clientCacheKey struct {
	slug                string
	keyVersion          int32
	callbackURL         string
	allowPrivateNetwork bool
}

type cachedClient struct {
	client    clientAPI
	expiresAt time.Time
}

type Adapter struct {
	secrets    *federationcore.SecretStore
	newClient  func(context.Context, Config, string, string) (clientAPI, error)
	clientCache sync.Map
	cacheTTL    time.Duration
	now         func() time.Time
}

func NewAdapter(secrets *federationcore.SecretStore) *Adapter {
	adapter := &Adapter{secrets: secrets, cacheTTL: clientCacheTTL, now: time.Now}
	adapter.newClient = func(ctx context.Context, config Config, secret, callbackURL string) (clientAPI, error) {
		client, err := NewClient(ctx, config.ClientID, secret, callbackURL, config.Scopes, config.IssuerURL, config.AllowedAlgorithms, config.AllowPrivateNetwork)
		if err != nil { return nil, err }
		return clientWrapper{client: client}, nil
	}
	return adapter
}

func (*Adapter) Protocol() string { return Protocol }

type adapterState struct {
	CallbackURL  string `json:"callbackUrl"`
	ExpectedIss  string `json:"expectedIssuer"`
	TokenURL     string `json:"tokenEndpoint"`
	Nonce        string `json:"nonce"`
	CodeVerifier string `json:"codeVerifier"`
}

type avatarReference struct {
	client       clientAPI
	accessToken string
	subject     string
	pictureClaim string
}

func (a *Adapter) Begin(ctx context.Context, provider federationcore.Provider, begin federationcore.BeginContext) (json.RawMessage, federationcore.NextAction, error) {
	_, client, err := a.client(ctx, provider, begin.CallbackURL)
	if err != nil { return nil, federationcore.NextAction{}, err }
	verifier, err := randomB64(32); if err != nil { return nil, federationcore.NextAction{}, err }
	nonce, err := randomB64(16); if err != nil { return nil, federationcore.NextAction{}, err }
	state, err := json.Marshal(adapterState{CallbackURL: begin.CallbackURL, ExpectedIss: client.Issuer(), TokenURL: client.TokenEndpoint(), Nonce: nonce, CodeVerifier: verifier})
	if err != nil { return nil, federationcore.NextAction{}, err }
	challengeDigest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeDigest[:])
	return state, federationcore.NextAction{Kind: federationcore.ActionRedirect, URL: client.AuthURL(begin.FlowID, nonce, challenge)}, nil
}

func (a *Adapter) Advance(ctx context.Context, provider federationcore.Provider, raw json.RawMessage, input federationcore.ActionInput) (federationcore.AdvanceResult, error) {
	if input.Kind != federationcore.ActionRedirect {
		return federationcore.AdvanceResult{}, federationcore.NewFailure(federationcore.FailureStateInvalid, nil)
	}
	var state adapterState
	if err := json.Unmarshal(raw, &state); err != nil {
		return federationcore.AdvanceResult{}, federationcore.NewFailure(federationcore.FailureStateInvalid, nil)
	}
	if state.CallbackURL == "" || state.ExpectedIss == "" || state.Nonce == "" || state.CodeVerifier == "" {
		return federationcore.AdvanceResult{}, federationcore.NewFailure(federationcore.FailureStateInvalid, nil)
	}
	if input.Issuer != "" && input.Issuer != state.ExpectedIss {
		return federationcore.AdvanceResult{}, federationcore.NewFailure(federationcore.FailureIssuerMismatch, map[string]any{
			"expected_iss": state.ExpectedIss,
			"got_iss":      input.Issuer,
		})
	}
	config, client, err := a.client(ctx, provider, state.CallbackURL)
	if err != nil {
		return federationcore.AdvanceResult{}, err
	}
	if client.Issuer() != state.ExpectedIss {
		return federationcore.AdvanceResult{}, federationcore.NewFailure(federationcore.FailureIssuerMismatch, map[string]any{
			"expected_iss": state.ExpectedIss,
			"got_iss":      client.Issuer(),
		})
	}
	if client.TokenEndpoint() != state.TokenURL {
		return federationcore.AdvanceResult{}, federationcore.NewFailure(federationcore.FailureTokenEndpointDrift, map[string]any{
			"expected": state.TokenURL,
			"got":      client.TokenEndpoint(),
		})
	}
	tokens, err := client.Exchange(ctx, input.Code, state.CodeVerifier, state.ExpectedIss, state.Nonce)
	if err != nil {
		return federationcore.AdvanceResult{}, federationcore.NewFailure(federationcore.FailureCodeExchange, nil)
	}
	usernameClaim := config.UsernameClaim
	if usernameClaim == "" {
		usernameClaim = "preferred_username"
	}
	displayClaim := config.DisplayNameClaim
	if displayClaim == "" {
		displayClaim = "name"
	}
	emailClaim := config.EmailClaim
	if emailClaim == "" {
		emailClaim = "email"
	}
	pictureClaim := config.PictureClaim
	if pictureClaim == "" {
		pictureClaim = "picture"
	}
	emailValue := ClaimString(tokens.Raw, emailClaim)
	var email *string
	if emailValue != "" {
		email = new(emailValue)
	}
	avatarURL := ClaimString(tokens.Raw, pictureClaim)
	identity := &federationcore.VerifiedIdentity{
		Issuer: tokens.Issuer, Subject: tokens.Subject, Email: email,
		EmailVerified: tokens.EmailVerified, EmailVerificationSupported: true,
		Username: ClaimString(tokens.Raw, usernameClaim), DisplayName: ClaimString(tokens.Raw, displayClaim),
		AMR: append([]string(nil), tokens.AMR...), AvatarURL: avatarURL,
	}
	result := federationcore.AdvanceResult{Identity: identity}
	if avatarURL == "" && tokens.AccessToken != "" {
		result.Avatar = &federationcore.AvatarDelivery{Opaque: &avatarReference{
			client: client, accessToken: tokens.AccessToken,
			subject: tokens.Subject, pictureClaim: pictureClaim,
		}}
	}
	return result, nil
}

func (a *Adapter) ResolveAvatar(ctx context.Context, _ federationcore.Provider, delivery federationcore.AvatarDelivery) (string, error) {
	if delivery.URL != "" {
		return delivery.URL, nil
	}
	reference, ok := delivery.Opaque.(*avatarReference)
	if !ok || reference.client == nil || reference.accessToken == "" || reference.subject == "" || reference.pictureClaim == "" {
		return "", errors.New("federation/oidc: invalid avatar reference")
	}
	userInfo, err := reference.client.UserInfo(ctx, reference.accessToken, reference.subject)
	if err != nil {
		return "", err
	}
	return ClaimString(userInfo, reference.pictureClaim), nil
}

// InvalidateClientCache evicts every cached client for slug. Administrative
// provider updates call this after commit so policy/config changes take effect
// on the next flow rather than after the bounded cache window.
func (a *Adapter) InvalidateClientCache(slug string) {
	a.clientCache.Range(func(key, _ any) bool {
		if cacheKey, ok := key.(clientCacheKey); ok && cacheKey.slug == slug {
			a.clientCache.Delete(key)
		}
		return true
	})
}

func (a *Adapter) client(ctx context.Context, provider federationcore.Provider, callbackURL string) (Config, clientAPI, error) {
	config, err := decodeConfig(provider.Config)
	if err != nil {
		return Config{}, nil, err
	}
	var keyVersion int32
	if provider.Secret != nil {
		keyVersion = provider.Secret.KeyVersion
	}
	key := clientCacheKey{
		slug:                provider.Slug,
		keyVersion:          keyVersion,
		callbackURL:         callbackURL,
		allowPrivateNetwork: config.AllowPrivateNetwork,
	}
	if value, ok := a.clientCache.Load(key); ok {
		entry := value.(*cachedClient)
		if a.now().Before(entry.expiresAt) {
			return config, entry.client, nil
		}
		a.clientCache.Delete(key)
	}

	config, secret, err := a.open(provider)
	if err != nil {
		return Config{}, nil, err
	}
	client, err := a.newClient(ctx, config, secret, callbackURL)
	if err != nil {
		return Config{}, nil, err
	}
	a.clientCache.Store(key, &cachedClient{client: client, expiresAt: a.now().Add(a.cacheTTL)})
	return config, client, nil
}

func (a *Adapter) open(provider federationcore.Provider) (Config, string, error) {
	config, err := decodeConfig(provider.Config)
	if err != nil { return Config{}, "", err }
	if provider.Secret == nil { return Config{}, "", errors.New("federation/oidc: provider secret is missing") }
	secret, err := a.secrets.OpenProviderSecret(*provider.Secret, provider.ID)
	if err != nil { return Config{}, "", err }
	if err := (Definition{}).ValidateSecret(secret); err != nil { return Config{}, "", err }
	return config, string(secret), nil
}

func decodeConfig(raw json.RawMessage) (Config, error) {
	var config Config
	if err := json.Unmarshal(raw, &config); err != nil { return Config{}, fmt.Errorf("federation/oidc: decode config: %w", err) }
	return config, nil
}

func randomB64(size int) (string, error) {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil { return "", err }
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

var _ federationcore.Definition = Definition{}
var _ federationcore.Adapter = (*Adapter)(nil)
var _ federationcore.AvatarResolver = (*Adapter)(nil)
