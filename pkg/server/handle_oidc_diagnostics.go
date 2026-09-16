package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/federation"
	federationoidc "prohibitorum/pkg/federation/providers/oidc"
	sessstore "prohibitorum/pkg/session"
	"prohibitorum/pkg/weberr"
)

const oidcTestCookieName = "prohibitorum_oidc_test"

func diagnosticSameOrigin(handler http.HandlerFunc) http.HandlerFunc {
	protection := http.NewCrossOriginProtection()
	return protection.Handler(handler).ServeHTTP
}

func (s *Server) oidcDiagnostics() *federationoidc.Diagnostics {
	return federationoidc.NewDiagnostics(s.kvStore, federation.NewSecretStore(s.config.DataEncryptionKeys))
}
func (s *Server) oidcDiagnosticProvider(r *http.Request) (federation.Provider, error) {
	row, err := s.providerStateQ().GetUpstreamIDPBySlugAny(r.Context(), chi.URLParam(r, "slug"))
	if err != nil || row.Protocol != "oidc" {
		return federation.Provider{}, authn.ErrUpstreamIDPNotFound()
	}
	return providerFromDB(row), nil
}
func diagnosticActor(r *http.Request) (federationoidc.DiagnosticActor, error) {
	session := authn.SessionFromContext(r.Context())
	if err := authn.Check(session, contract.AuthRequirement{Kind: contract.AuthAdmin}); err != nil {
		return federationoidc.DiagnosticActor{}, err
	}
	if session.Data == nil || session.Account == nil || session.Data.SessionID == "" {
		return federationoidc.DiagnosticActor{}, authn.ErrFederationStateInvalid()
	}
	return federationoidc.DiagnosticActor{AccountID: session.Account.ID, SessionID: session.Data.SessionID}, nil
}
func diagnosticBrowser(r *http.Request) string {
	if cookie, err := r.Cookie(oidcTestCookieName); err == nil {
		return cookie.Value
	}
	return ""
}
func (s *Server) diagnosticCallback(slug string) string {
	return s.config.PublicOrigins[0] + "/api/prohibitorum/auth/federation/" + url.PathEscape(slug) + "/test/callback"
}
func diagnosticHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
}
func diagnosticEmptyBody(r *http.Request) bool {
	if r.Body == nil {
		return true
	}
	var one [1]byte
	n, err := r.Body.Read(one[:])
	return n == 0 && err == io.EOF
}
func writeDiagnosticError(w http.ResponseWriter, err error) {
	var resolution *federationoidc.DiagnosticResolutionError
	if errors.As(err, &resolution) {
		stage := resolution.Stage
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "upstream_temporarily_unavailable", "requestId": stage.RequestID, "stages": []federationoidc.DiagnosticStage{stage}, "details": map[string]any{"stage": stage.Name, "endpoint": stage.Endpoint, "durationMs": stage.DurationMS, "errorCode": stage.ErrorCode}})
		return
	}
	if errors.Is(err, federationoidc.ErrDiagnosticUnavailable) {
		writeAuthErr(w, authn.ErrFederationStateInvalid())
		return
	}
	// Upstream error strings may include response bodies or credentials.
	writeAuthErr(w, authn.ErrUpstreamTemporarilyUnavailable())
}
func (s *Server) handleOIDCEffectiveConfigHTTP(w http.ResponseWriter, r *http.Request) {
	diagnosticHeaders(w)
	actor, err := diagnosticActor(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if s.rateLimit(w, r, "oidc-effective:"+strconv.Itoa(int(actor.AccountID)), 20, time.Minute) {
		return
	}
	provider, err := s.oidcDiagnosticProvider(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if err := (federationoidc.Definition{}).ValidateConfig(provider.Config); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	var config federationoidc.Config
	_ = json.Unmarshal(provider.Config, &config)
	begin := time.Now()
	resolved, err := federationoidc.ResolveConfig(r.Context(), config)
	if err != nil {
		writeDiagnosticError(w, &federationoidc.DiagnosticResolutionError{Stage: federationoidc.DiagnosticStage{Name: "discovery", Status: "failed", DurationMS: time.Since(begin).Milliseconds(), Endpoint: federationoidc.DiagnosticEndpoint(config.IssuerURL), ErrorCode: "discovery_failed", RequestID: weberr.RequestIDFromContext(r.Context())}})
		return
	}
	values := map[string]any{"issuer": resolved.Issuer, "authorizationEndpoint": resolved.AuthorizationEndpoint, "tokenEndpoint": resolved.TokenEndpoint, "userinfoEndpoint": resolved.UserInfoEndpoint, "jwksEndpoint": resolved.JWKSEndpoint, "tokenAuthMethod": resolved.TokenAuthMethod, "pkceMethod": resolved.PKCEMethod, "scopes": resolved.Scopes}
	fields := map[string]any{}
	for name, value := range values {
		fields[name] = map[string]any{"value": value, "source": resolved.Sources[name]}
	}
	writeJSON(w, map[string]any{"mode": config.ConfigurationMode, "fetchedAt": resolved.FetchedAt, "fields": fields, "callbackUrl": s.diagnosticCallback(provider.Slug)})
}
func (s *Server) handleOIDCTestStartHTTP(w http.ResponseWriter, r *http.Request) {
	diagnosticHeaders(w)
	actor, err := diagnosticActor(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if !diagnosticEmptyBody(r) {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if s.rateLimit(w, r, "oidc-test:"+strconv.Itoa(int(actor.AccountID)), 6, time.Minute) {
		return
	}
	provider, err := s.oidcDiagnosticProvider(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	browser := diagnosticBrowser(r)
	if !federationoidc.ValidDiagnosticID(browser) {
		var raw [32]byte
		if _, err := rand.Read(raw[:]); err != nil {
			writeDiagnosticError(w, err)
			return
		}
		browser = base64.RawURLEncoding.EncodeToString(raw[:])
	}
	result, err := s.oidcDiagnostics().Start(r.Context(), provider, actor, browser, s.diagnosticCallback(provider.Slug), weberr.RequestIDFromContext(r.Context()))
	if err != nil {
		writeDiagnosticError(w, err)
		return
	}
	cookie := sessstore.FedStateCookie(s.config, r, browser)
	cookie.Name = oidcTestCookieName
	cookie.MaxAge = int(federationoidc.DiagnosticTTL.Seconds())
	http.SetCookie(w, cookie)
	writeJSON(w, result)
}
func (s *Server) handleOIDCTestCallbackHTTP(w http.ResponseWriter, r *http.Request) {
	diagnosticHeaders(w)
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	for key, values := range q {
		if len(values) != 1 || (key != "state" && key != "code" && key != "error" && key != "error_description" && key != "error_uri" && key != "iss" && key != "session_state") {
			writeAuthErr(w, authn.ErrBadRequest())
			return
		}
	}
	if (q.Get("code") == "") == (q.Get("error") == "") {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	id := q.Get("state")
	slug := chi.URLParam(r, "slug")
	if err := s.oidcDiagnostics().Callback(r.Context(), id, slug, diagnosticBrowser(r), q.Get("code"), q.Get("error"), q.Get("iss"), weberr.RequestIDFromContext(r.Context())); err != nil {
		writeDiagnosticError(w, err)
		return
	}
	http.Redirect(w, r, "/admin/identity-providers/"+url.PathEscape(slug)+"?test="+url.QueryEscape(id), http.StatusFound)
}
func (s *Server) handleOIDCTestGetHTTP(w http.ResponseWriter, r *http.Request) {
	diagnosticHeaders(w)
	actor, err := diagnosticActor(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	result, err := s.oidcDiagnostics().Get(r.Context(), chi.URLParam(r, "id"), chi.URLParam(r, "slug"), diagnosticBrowser(r), actor)
	if err != nil {
		writeDiagnosticError(w, err)
		return
	}
	writeJSON(w, result)
}
func (s *Server) handleOIDCTestCompleteHTTP(w http.ResponseWriter, r *http.Request) {
	diagnosticHeaders(w)
	actor, err := diagnosticActor(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if !diagnosticEmptyBody(r) {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	provider, err := s.oidcDiagnosticProvider(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	result, err := s.oidcDiagnostics().Complete(r.Context(), chi.URLParam(r, "id"), provider, diagnosticBrowser(r), actor, weberr.RequestIDFromContext(r.Context()))
	if err != nil {
		writeDiagnosticError(w, err)
		return
	}
	writeJSON(w, result)
}
