package oidc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/zitadel/oidc/v3/pkg/client/rp"
	oidclib "github.com/zitadel/oidc/v3/pkg/oidc"
	"golang.org/x/oauth2"

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
	Status    string              `json:"status"`
	ExpiresAt time.Time           `json:"expiresAt"`
	Stages    []DiagnosticStage   `json:"stages"`
	Identity  *DiagnosticIdentity `json:"identity,omitempty"`
	IDToken   *DiagnosticDocument `json:"idToken,omitempty"`
	UserInfo  *DiagnosticDocument `json:"userinfo,omitempty"`
}

// DiagnosticField is one identity field as login maps it: Value is a string,
// a bool for emailVerified, or nil when the claim is absent. Source names the
// document it came from (id_token, userinfo or configuration) and Claim the
// claim that was read.
type DiagnosticField struct {
	Value  any    `json:"value"`
	Source string `json:"source"`
	Claim  string `json:"claim,omitempty"`
}
type DiagnosticIdentity struct {
	Issuer        DiagnosticField `json:"issuer"`
	Subject       DiagnosticField `json:"subject"`
	Username      DiagnosticField `json:"username"`
	DisplayName   DiagnosticField `json:"displayName"`
	Email         DiagnosticField `json:"email"`
	EmailVerified DiagnosticField `json:"emailVerified"`
	Picture       DiagnosticField `json:"picture"`
}

// DiagnosticDocument carries an upstream JSON document as compacted text, so
// numbers and key order reach the browser exactly as the provider sent them.
// A document over diagnosticDocumentLimit keeps only its compacted size.
type DiagnosticDocument struct {
	JSON         string `json:"json,omitempty"`
	OmittedBytes int    `json:"omittedBytes,omitempty"`
}

type diagnosticFlow struct {
	DiagnosticResult
	Actor         DiagnosticActor `json:"actor"`
	BrowserDigest string          `json:"browserDigest"`
	ProviderID    int64           `json:"providerId"`
	ProviderSlug  string          `json:"providerSlug"`
	// ProviderIdentity is the provider configuration fingerprint. Its key must
	// differ from the embedded DiagnosticResult.Identity: encoding/json keeps
	// the shallower of two fields that share a key and drops the other.
	ProviderIdentity string         `json:"providerIdentity"`
	Resolved         ResolvedConfig `json:"resolved"`
	// AdapterState is the login Adapter's own flow state from Begin.
	AdapterState json.RawMessage `json:"adapterState,omitempty"`
	Code         string          `json:"code,omitempty"`
	Issuer       string          `json:"issuer,omitempty"`
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

// Start begins a connection test through the same Adapter.Begin that login
// uses; only the flow storage and the callback route differ.
func (d *Diagnostics) Start(ctx context.Context, provider federationcore.Provider, actor DiagnosticActor, browser, callback, requestID string) (DiagnosticStart, error) {
	if actor.AccountID == 0 || actor.SessionID == "" || !ValidDiagnosticID(browser) || !(Definition{}).Ready(provider) {
		return DiagnosticStart{}, ErrDiagnosticUnavailable
	}
	id, err := randomB64(32)
	if err != nil {
		return DiagnosticStart{}, err
	}
	adapter := NewAdapter(d.secrets)
	var discovery DiagnosticStage
	adapter.resolveConfig = func(ctx context.Context, config Config) (ResolvedConfig, error) {
		begin := d.now()
		resolved, err := ResolveConfig(ctx, config)
		discovery = DiagnosticStage{Name: "discovery", Status: "succeeded", DurationMS: d.now().Sub(begin).Milliseconds(), Endpoint: DiagnosticEndpoint(config.IssuerURL), RequestID: requestID}
		if err != nil {
			discovery.Status, discovery.ErrorCode = "failed", "discovery_failed"
		} else if config.ConfigurationMode == "manual" {
			discovery.Status = "skipped"
		}
		return resolved, err
	}
	raw, next, err := adapter.Begin(ctx, provider, federationcore.BeginContext{FlowID: id, CallbackURL: callback})
	if err != nil {
		if discovery.Status == "failed" {
			return DiagnosticStart{}, &DiagnosticResolutionError{Stage: discovery}
		}
		return DiagnosticStart{}, err
	}
	var state adapterState
	if err := json.Unmarshal(raw, &state); err != nil {
		return DiagnosticStart{}, err
	}
	flow := diagnosticFlow{DiagnosticResult: DiagnosticResult{Status: "awaiting_callback", ExpiresAt: d.now().Add(DiagnosticTTL), Stages: []DiagnosticStage{discovery, {Name: "authorize", Status: "pending", Endpoint: DiagnosticEndpoint(state.Resolved.AuthorizationEndpoint)}}}, Actor: actor, BrowserDigest: federationcore.BrowserDigest(browser), ProviderID: provider.ID, ProviderSlug: provider.Slug, ProviderIdentity: providerIdentity(provider), Resolved: state.Resolved, AdapterState: raw}
	stored, err := json.Marshal(flow)
	if err != nil {
		return DiagnosticStart{}, err
	}
	ok, err := d.store.SetNX(ctx, diagnosticKey(id), string(stored), DiagnosticTTL)
	if err != nil {
		return DiagnosticStart{}, err
	}
	if !ok {
		return DiagnosticStart{}, ErrDiagnosticUnavailable
	}
	return DiagnosticStart{ID: id, AuthorizationURL: next.URL, ExpiresAt: flow.ExpiresAt}, nil
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
	flow.AdapterState = nil
	flow.Code = ""
	flow.Issuer = ""
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
	} else if code == "" || len(code) > 8192 {
		stage.Status = "failed"
		stage.ErrorCode = "invalid_callback"
	}
	flow.Stages = append(flow.Stages, stage)
	if stage.Status == "failed" {
		flow.Status = "failed"
		clearDiagnosticSecrets(&flow)
	} else {
		// Adapter.Advance checks iss, exactly as it does for login.
		flow.Status = "ready"
		flow.Code, flow.Issuer = code, issuer
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

// Complete finishes a connection test by calling the login Adapter.Advance
// with the stored callback. A recorder on the context observes the upstream
// requests and the verified documents; the test itself holds no protocol
// logic, so what it reports is what login would do.
func (d *Diagnostics) Complete(ctx context.Context, id string, provider federationcore.Provider, browser string, actor DiagnosticActor, requestID string) (DiagnosticResult, error) {
	flow, raw, err := d.read(ctx, id, provider.Slug, browser, &actor)
	if err != nil {
		return DiagnosticResult{}, err
	}
	if flow.Status != "ready" {
		return flow.DiagnosticResult, nil
	}
	if len(flow.AdapterState) == 0 {
		// Started before connection tests stored the adapter state.
		return DiagnosticResult{}, ErrDiagnosticUnavailable
	}
	if flow.ProviderID != provider.ID || flow.ProviderIdentity != providerIdentity(provider) || !(Definition{}).Ready(provider) {
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
	state, input := flow.AdapterState, federationcore.ActionInput{Kind: federationcore.ActionRedirect, Code: flow.Code, Issuer: flow.Issuer}
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
	runCtx, recorder := withRecorder(runCtx)
	adapter := NewAdapter(d.secrets)
	result, advanceErr := adapter.Advance(runCtx, provider, state, input)
	advancedAt := time.Now()
	config, _ := decodeConfig(provider.Config)
	_, _, _, pictureClaim := config.profileClaims()

	// Login resolves a missing avatar through userinfo after sign-in. The test
	// makes that call now; when the id_token already has a picture it still
	// asks userinfo once, through the same ResolveAvatar, only to show it.
	var avatarURL string
	var avatarErr error
	if advanceErr == nil && !result.Identity.UserInfoFallback {
		if result.Avatar != nil && result.Avatar.Opaque != nil {
			avatarURL, avatarErr = adapter.ResolveAvatar(runCtx, provider, *result.Avatar)
		} else if client, tokens, _ := recorder.exchange(); client != nil && tokens.AccessToken != "" && flow.Resolved.UserInfoEndpoint != "" {
			reference := &avatarReference{client: clientWrapper{client: client}, accessToken: tokens.AccessToken, subject: tokens.Subject, pictureClaim: pictureClaim}
			_, avatarErr = adapter.ResolveAvatar(runCtx, provider, federationcore.AvatarDelivery{Opaque: reference})
		}
	}

	flow.Stages = append(flow.Stages, diagnosticStages(recorder, flow.Resolved, advanceErr, avatarErr, advancedAt, requestID)...)
	flow.Status = "succeeded"
	for _, stage := range flow.Stages {
		if stage.Status == "failed" {
			flow.Status = "failed"
		}
	}
	if _, tokens, _ := recorder.exchange(); tokens != nil && tokens.IDToken != "" {
		flow.IDToken = idTokenDocument(tokens.IDToken)
	}
	if call := recorder.call(upstreamUserInfo); call.status >= 200 && call.status < 300 && flow.Stages[len(flow.Stages)-1].ErrorCode != "userinfo_failed" {
		flow.UserInfo = diagnosticDocument(recorder.userInfoBody())
	}
	if advanceErr == nil {
		flow.DiagnosticResult.Identity = diagnosticIdentity(*result.Identity, config, avatarURL)
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

// diagnosticStages reads the token_exchange, id_token and userinfo stages
// from the Advance outcome and the recorded requests.
func diagnosticStages(recorder *diagnosticRecorder, resolved ResolvedConfig, advanceErr, avatarErr error, advancedAt time.Time, requestID string) []DiagnosticStage {
	token := recorder.call(upstreamToken)
	exchange := DiagnosticStage{Name: "token_exchange", Status: "succeeded", Endpoint: DiagnosticEndpoint(resolved.TokenEndpoint), HTTPStatus: token.status, DurationMS: token.duration.Milliseconds(), RequestID: requestID}
	verification := DiagnosticStage{Name: "id_token", Status: "succeeded", RequestID: requestID}
	userinfo := DiagnosticStage{Name: "userinfo", Status: "skipped", Endpoint: DiagnosticEndpoint(resolved.UserInfoEndpoint), RequestID: requestID}
	if call := recorder.call(upstreamUserInfo); call.count > 0 {
		userinfo.Status, userinfo.HTTPStatus, userinfo.DurationMS = "succeeded", call.status, call.duration.Milliseconds()
	}
	fail := func(stage *DiagnosticStage, code string) { stage.Status, stage.ErrorCode = "failed", code }

	reason, flowFailure := federationcore.FailureReasonOf(advanceErr)
	var retrieval *oauth2.RetrieveError
	switch {
	case advanceErr == nil:
	case !flowFailure:
		// Only opening the provider fails outside the flow vocabulary, and
		// that is almost always the client secret.
		fail(&exchange, "credentials_unavailable")
	case reason == federationcore.FailureIssuerMismatch:
		fail(&exchange, "issuer_mismatch")
	case reason == federationcore.FailureStateInvalid || reason == federationcore.FailureTokenEndpointDrift:
		fail(&exchange, "configuration_changed")
	case reason == federationcore.FailureCodeExchange && errors.As(advanceErr, &retrieval):
		fail(&exchange, safeOAuthError(retrieval.ErrorCode))
	case reason == federationcore.FailureCodeExchange && (token.status < 200 || token.status >= 300):
		fail(&exchange, "token_exchange_failed")
	case reason == federationcore.FailureCodeExchange:
		fail(&verification, diagnosticVerificationError(advanceErr))
	case errors.Is(advanceErr, errUpstreamIdentityParse):
		fail(&verification, "id_token_invalid")
	case errors.Is(advanceErr, errNoUserInfoEndpoint):
		fail(&userinfo, "userinfo_required")
	case errors.Is(advanceErr, errNoSubject):
		fail(&userinfo, "subject_missing")
	case reason == federationcore.FailureUpstreamNoIdentity:
		fail(&userinfo, "userinfo_failed")
	default:
		fail(&exchange, "token_exchange_failed")
	}
	if exchange.Status == "failed" {
		verification.Status = "skipped"
		return []DiagnosticStage{exchange, verification}
	}
	_, tokens, exchangedAt := recorder.exchange()
	if exchangedAt.IsZero() {
		exchangedAt = advancedAt
	}
	if !token.end.IsZero() {
		verification.DurationMS = exchangedAt.Sub(token.end).Milliseconds()
	}
	if verification.Status == "failed" {
		return []DiagnosticStage{exchange, verification}
	}
	if tokens != nil && tokens.IDToken == "" {
		verification.Status, verification.DurationMS = "skipped", 0
	}
	switch {
	case avatarErr == nil:
	case errors.Is(avatarErr, rp.ErrUserInfoSubNotMatching):
		fail(&userinfo, "userinfo_subject_mismatch")
	default:
		fail(&userinfo, "userinfo_failed")
	}
	return []DiagnosticStage{exchange, verification, userinfo}
}

// diagnosticIdentity labels each field of the identity login verified with
// the document and claim it was read from.
func diagnosticIdentity(identity federationcore.VerifiedIdentity, config Config, resolvedAvatar string) *DiagnosticIdentity {
	usernameClaim, displayClaim, emailClaim, pictureClaim := config.profileClaims()
	text := func(value string) any {
		if value == "" {
			return nil
		}
		return value
	}
	source := "id_token"
	result := &DiagnosticIdentity{
		Issuer:  DiagnosticField{Value: text(identity.Issuer), Source: "id_token", Claim: "iss"},
		Subject: DiagnosticField{Value: text(identity.Subject), Source: "id_token", Claim: "sub"},
	}
	if identity.UserInfoFallback {
		source = "userinfo"
		result.Issuer = DiagnosticField{Value: text(identity.Issuer), Source: "configuration"}
		result.Subject = DiagnosticField{Value: text(identity.Subject), Source: source, Claim: config.SubjectClaim}
	}
	email := ""
	if identity.Email != nil {
		email = *identity.Email
	}
	result.Username = DiagnosticField{Value: text(identity.Username), Source: source, Claim: usernameClaim}
	result.DisplayName = DiagnosticField{Value: text(identity.DisplayName), Source: source, Claim: displayClaim}
	result.Email = DiagnosticField{Value: text(email), Source: source, Claim: emailClaim}
	result.EmailVerified = DiagnosticField{Value: identity.EmailVerified, Source: source, Claim: "email_verified"}
	result.Picture = DiagnosticField{Value: text(identity.AvatarURL), Source: source, Claim: pictureClaim}
	if identity.AvatarURL == "" && resolvedAvatar != "" {
		result.Picture = DiagnosticField{Value: resolvedAvatar, Source: "userinfo", Claim: pictureClaim}
	}
	return result
}

// idTokenDocument returns the payload of an id_token that Exchange already
// verified.
func idTokenDocument(token string) *DiagnosticDocument {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return nil
	}
	return diagnosticDocument(payload, 0)
}

// diagnosticDocument compacts a JSON object. size, when larger than the text,
// is the compacted size of a body the recorder only kept in part.
func diagnosticDocument(raw []byte, size int) *DiagnosticDocument {
	if size > diagnosticDocumentLimit {
		return &DiagnosticDocument{OmittedBytes: size}
	}
	var compacted bytes.Buffer
	if json.Compact(&compacted, raw) != nil || !bytes.HasPrefix(compacted.Bytes(), []byte("{")) {
		return nil
	}
	if compacted.Len() > diagnosticDocumentLimit {
		return &DiagnosticDocument{OmittedBytes: compacted.Len()}
	}
	return &DiagnosticDocument{JSON: compacted.String()}
}

func diagnosticVerificationError(err error) string {
	for _, entry := range []struct {
		cause error
		code  string
	}{
		{oidclib.ErrIssuerInvalid, "issuer_mismatch"}, {oidclib.ErrAudience, "audience_mismatch"}, {oidclib.ErrNonceInvalid, "nonce_mismatch"}, {oidclib.ErrExpired, "token_expired"}, {oidclib.ErrSignatureUnsupportedAlg, "signing_algorithm"}, {oidclib.ErrSignatureInvalid, "signature_invalid"}, {oidclib.ErrAtHash, "access_token_hash"}, {rp.ErrMissingIDToken, "missing_id_token"},
	} {
		if errors.Is(err, entry.cause) {
			return entry.code
		}
	}
	return "id_token_invalid"
}
