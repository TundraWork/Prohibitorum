package oidc

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	federationcore "prohibitorum/pkg/federation"
	"prohibitorum/pkg/kv"
)

const DiagnosticTTL = 10 * time.Minute

var ErrDiagnosticUnavailable = errors.New("OIDC test unavailable")

// DiagnosticResolutionError carries only safe metadata, never upstream text.
type DiagnosticResolutionError struct{ Stage DiagnosticStage }

func (e *DiagnosticResolutionError) Error() string { return "OIDC discovery failed" }

type DiagnosticActor struct {
	AccountID int32
	SessionID string
}
type DiagnosticStage struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	DurationMS int64  `json:"durationMs,omitempty"`
	Endpoint   string `json:"endpoint,omitempty"`
	HTTPStatus int    `json:"httpStatus,omitempty"`
	ErrorCode  string `json:"errorCode,omitempty"`
	RequestID  string `json:"requestId,omitempty"`
}
type DiagnosticResult struct {
	Status    string            `json:"status"`
	ExpiresAt time.Time         `json:"expiresAt"`
	Stages    []DiagnosticStage `json:"stages"`
	Claims    map[string]any    `json:"claims,omitempty"`
}
type diagnosticFlow struct {
	DiagnosticResult
	Actor         DiagnosticActor `json:"actor"`
	BrowserDigest string          `json:"browserDigest"`
	ProviderID    int64           `json:"providerId"`
	ProviderSlug  string          `json:"providerSlug"`
	Identity      string          `json:"identity"`
	Resolved      ResolvedConfig  `json:"resolved"`
	CallbackURL   string          `json:"callbackUrl"`
	Nonce         string          `json:"nonce,omitempty"`
	Verifier      string          `json:"verifier,omitempty"`
	Code          string          `json:"code,omitempty"`
}

type Diagnostics struct {
	store   kv.Store
	secrets *federationcore.SecretStore
	now     func() time.Time
}

func NewDiagnostics(store kv.Store, secrets *federationcore.SecretStore) *Diagnostics {
	return &Diagnostics{store: store, secrets: secrets, now: time.Now}
}

type DiagnosticStart struct {
	ID               string    `json:"id"`
	AuthorizationURL string    `json:"authorizationUrl"`
	ExpiresAt        time.Time `json:"expiresAt"`
}

func diagnosticKey(id string) string { return "oidc:admin-test:" + id }
func ValidDiagnosticID(id string) bool {
	b, err := base64.RawURLEncoding.DecodeString(id)
	return err == nil && len(b) == 32 && base64.RawURLEncoding.EncodeToString(b) == id
}
func DiagnosticEndpoint(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host + u.EscapedPath()
}

func (d *Diagnostics) Start(ctx context.Context, provider federationcore.Provider, actor DiagnosticActor, browser, callback, requestID string) (DiagnosticStart, error) {
	if actor.AccountID == 0 || actor.SessionID == "" || !ValidDiagnosticID(browser) || !(Definition{}).Ready(provider) {
		return DiagnosticStart{}, ErrDiagnosticUnavailable
	}
	config, err := decodeConfig(provider.Config)
	if err != nil {
		return DiagnosticStart{}, ErrDiagnosticUnavailable
	}
	start := d.now()
	resolved, err := ResolveConfig(ctx, config)
	if err != nil {
		return DiagnosticStart{}, &DiagnosticResolutionError{Stage: DiagnosticStage{Name: "discovery", Status: "failed", DurationMS: d.now().Sub(start).Milliseconds(), Endpoint: DiagnosticEndpoint(config.IssuerURL), ErrorCode: "discovery_failed", RequestID: requestID}}
	}
	id, err := randomB64(32)
	if err != nil {
		return DiagnosticStart{}, err
	}
	nonce, err := randomB64(32)
	if err != nil {
		return DiagnosticStart{}, err
	}
	verifier, challenge := "", ""
	if config.PKCEMethod != "off" {
		verifier, err = randomB64(32)
		if err != nil {
			return DiagnosticStart{}, err
		}
		challenge = verifier
		if config.PKCEMethod == "S256" {
			digest := sha256.Sum256([]byte(verifier))
			challenge = base64.RawURLEncoding.EncodeToString(digest[:])
		}
	}
	// Building the authorization URL does not require decrypting credentials.
	client, err := NewClient(ctx, config.ClientID, "", callback, resolved, nil, config.AllowPrivateNetwork)
	if err != nil {
		return DiagnosticStart{}, err
	}
	status := "succeeded"
	if config.ConfigurationMode == "manual" {
		status = "skipped"
	}
	flow := diagnosticFlow{DiagnosticResult: DiagnosticResult{Status: "awaiting_callback", ExpiresAt: d.now().Add(DiagnosticTTL), Stages: []DiagnosticStage{{Name: "discovery", Status: status, DurationMS: d.now().Sub(start).Milliseconds(), Endpoint: DiagnosticEndpoint(config.IssuerURL), RequestID: requestID}, {Name: "authorize", Status: "pending", Endpoint: DiagnosticEndpoint(resolved.AuthorizationEndpoint)}}}, Actor: actor, BrowserDigest: federationcore.BrowserDigest(browser), ProviderID: provider.ID, ProviderSlug: provider.Slug, Identity: providerIdentity(provider), Resolved: resolved, CallbackURL: callback, Nonce: nonce, Verifier: verifier}
	raw, err := json.Marshal(flow)
	if err != nil {
		return DiagnosticStart{}, err
	}
	ok, err := d.store.SetNX(ctx, diagnosticKey(id), string(raw), DiagnosticTTL)
	if err != nil {
		return DiagnosticStart{}, err
	}
	if !ok {
		return DiagnosticStart{}, ErrDiagnosticUnavailable
	}
	return DiagnosticStart{ID: id, AuthorizationURL: client.AuthURL(id, nonce, challenge), ExpiresAt: flow.ExpiresAt}, nil
}

func (d *Diagnostics) read(ctx context.Context, id, slug, browser string, actor *DiagnosticActor) (diagnosticFlow, string, error) {
	if !ValidDiagnosticID(id) || !ValidDiagnosticID(browser) {
		return diagnosticFlow{}, "", ErrDiagnosticUnavailable
	}
	raw, err := d.store.Get(ctx, diagnosticKey(id))
	if err != nil {
		if errors.Is(err, kv.ErrKeyNotFound) {
			err = ErrDiagnosticUnavailable
		}
		return diagnosticFlow{}, "", err
	}
	var flow diagnosticFlow
	if json.Unmarshal([]byte(raw), &flow) != nil || flow.ProviderSlug != slug || !d.now().Before(flow.ExpiresAt) || !federationcore.BrowserBindingOK(flow.BrowserDigest, browser) || (actor != nil && (*actor != flow.Actor || actor.AccountID == 0 || actor.SessionID == "")) {
		return diagnosticFlow{}, "", ErrDiagnosticUnavailable
	}
	return flow, raw, nil
}
func (d *Diagnostics) replace(ctx context.Context, id, old string, flow diagnosticFlow) (bool, error) {
	ttl := flow.ExpiresAt.Sub(d.now())
	if ttl <= 0 {
		return false, ErrDiagnosticUnavailable
	}
	raw, err := json.Marshal(flow)
	if err != nil {
		return false, err
	}
	return d.store.CompareAndSwap(ctx, diagnosticKey(id), old, string(raw), ttl)
}
func clearDiagnosticSecrets(flow *diagnosticFlow) {
	flow.Code = ""
	flow.Nonce = ""
	flow.Verifier = ""
}

func (d *Diagnostics) Callback(ctx context.Context, id, slug, browser, code, upstreamError, issuer, requestID string) error {
	flow, raw, err := d.read(ctx, id, slug, browser, nil)
	if err != nil {
		return err
	}
	if flow.Status != "awaiting_callback" {
		return ErrDiagnosticUnavailable
	}
	stage := DiagnosticStage{Name: "callback", Status: "succeeded", RequestID: requestID}
	flow.Stages[1].Status = "succeeded"
	if upstreamError != "" {
		stage.Status = "failed"
		stage.ErrorCode = safeOAuthError(upstreamError)
		flow.Stages[1].Status = "failed"
	} else if code == "" || len(code) > 8192 || (issuer != "" && issuer != flow.Resolved.Issuer) {
		stage.Status = "failed"
		stage.ErrorCode = "invalid_callback"
	}
	flow.Stages = append(flow.Stages, stage)
	if stage.Status == "failed" {
		flow.Status = "failed"
		clearDiagnosticSecrets(&flow)
	} else {
		flow.Status = "ready"
		flow.Code = code
	}
	ok, err := d.replace(ctx, id, raw, flow)
	if err != nil {
		return err
	}
	if !ok {
		return ErrDiagnosticUnavailable
	}
	return nil
}
func safeOAuthError(value string) string {
	switch value {
	case "access_denied", "invalid_request", "unauthorized_client", "unsupported_response_type", "invalid_scope", "server_error", "temporarily_unavailable", "invalid_client", "invalid_grant":
		return value
	}
	return "upstream_error"
}

func (d *Diagnostics) Get(ctx context.Context, id, slug, browser string, actor DiagnosticActor) (DiagnosticResult, error) {
	flow, _, err := d.read(ctx, id, slug, browser, &actor)
	return flow.DiagnosticResult, err
}

func (d *Diagnostics) Complete(ctx context.Context, id string, provider federationcore.Provider, browser string, actor DiagnosticActor, requestID string) (DiagnosticResult, error) {
	flow, raw, err := d.read(ctx, id, provider.Slug, browser, &actor)
	if err != nil {
		return DiagnosticResult{}, err
	}
	if flow.Status != "ready" {
		return flow.DiagnosticResult, nil
	}
	if flow.ProviderID != provider.ID || flow.Identity != providerIdentity(provider) || !(Definition{}).Ready(provider) {
		flow.Status = "failed"
		flow.Stages = append(flow.Stages, DiagnosticStage{Name: "token_exchange", Status: "failed", ErrorCode: "configuration_changed", RequestID: requestID})
		clearDiagnosticSecrets(&flow)
		ok, err := d.replace(ctx, id, raw, flow)
		if err != nil {
			return DiagnosticResult{}, err
		}
		if !ok {
			return d.Get(ctx, id, provider.Slug, browser, actor)
		}
		return flow.DiagnosticResult, nil
	}
	code, verifier, nonce := flow.Code, flow.Verifier, flow.Nonce
	flow.Status = "running"
	clearDiagnosticSecrets(&flow)
	ok, err := d.replace(ctx, id, raw, flow)
	if err != nil {
		return DiagnosticResult{}, err
	}
	if !ok {
		return d.Get(ctx, id, provider.Slug, browser, actor)
	}
	running, _ := json.Marshal(flow)
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	config, secret, err := NewAdapter(d.secrets).open(provider)
	if err == nil {
		var client *Client
		client, err = NewClient(runCtx, config.ClientID, secret, flow.CallbackURL, flow.Resolved, nil, config.AllowPrivateNetwork)
		if err == nil {
			tokens, stages, exchangeErr := client.ExchangeDiagnostic(runCtx, code, verifier, flow.Resolved.Issuer, nonce, requestID)
			flow.Stages = append(flow.Stages, stages...)
			err = exchangeErr
			if err == nil {
				flow.Claims = map[string]any{"issuer": tokens.Issuer, "subject": tokens.Subject, "username": ClaimString(tokens.Raw, config.UsernameClaim), "name": ClaimString(tokens.Raw, config.DisplayNameClaim), "email": ClaimString(tokens.Raw, config.EmailClaim), "email_verified": tokens.EmailVerified}

				for key, value := range flow.Claims {
					if text, ok := value.(string); ok && len(text) > 512 {
						flow.Claims[key] = string([]rune(text)[:min(512, len([]rune(text)))])
					}
				}
				begin := d.now()
				stage := DiagnosticStage{Name: "userinfo", Status: "skipped", Endpoint: DiagnosticEndpoint(flow.Resolved.UserInfoEndpoint), RequestID: requestID}
				if flow.Resolved.UserInfoEndpoint != "" {
					stage.Status = "succeeded"
					_, stage.HTTPStatus, err = client.UserInfoDiagnostic(runCtx, tokens.AccessToken, tokens.Subject)
					if err != nil {
						stage.Status = "failed"
						stage.ErrorCode = "userinfo_failed"
					}
					stage.DurationMS = d.now().Sub(begin).Milliseconds()
				}
				flow.Stages = append(flow.Stages, stage)
			}
		}
	}
	if err != nil {
		flow.Status = "failed"
		if len(flow.Stages) == 3 {
			flow.Stages = append(flow.Stages, DiagnosticStage{Name: "token_exchange", Status: "failed", ErrorCode: "credentials_unavailable", RequestID: requestID})
		}
	} else {
		flow.Status = "succeeded"
	}
	ok, saveErr := d.replace(context.WithoutCancel(ctx), id, string(running), flow)
	if saveErr != nil {
		return DiagnosticResult{}, saveErr
	}
	if !ok {
		return DiagnosticResult{}, ErrDiagnosticUnavailable
	}
	return flow.DiagnosticResult, nil
}
