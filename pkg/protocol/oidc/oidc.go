// Package oidc implements Prohibitorum's OpenID Connect Provider surface.
//
// v0.1 STATUS: skeleton only. The endpoints are wired and return
// well-formed minimum responses (discovery doc, empty JWKS), but the
// authorization-code, token, and userinfo flows are stubs that return
// 501 Not Implemented. The structure is in place so the wiring can be
// reviewed independently of the cryptographic flow logic.
//
// Roadmap:
//   v0.2: signing key generation + rotation; /jwks returns real keys
//   v0.3: /authorize → login redirect + code issuance
//   v0.4: /token authorization_code grant + ID token + access token
//   v0.5: /token refresh_token grant with rotation + reuse detection
//   v0.6: /userinfo, /oauth/introspect, /oauth/revoke
//   v0.7: /oidc/logout (RP-initiated)
//
// See AUDIT.md for the full best-practice checklist.
package oidc

import (
	"encoding/json"
	"net/http"

	"prohibitorum/pkg/configx"
)

// Provider is the per-server OIDC handler. Constructed once by the
// server bootstrap and wired into the chi router.
type Provider struct {
	cfg  *configx.Config
	keys *keyCache // wired in Task 7; nil until then
}

func New(cfg *configx.Config) *Provider {
	return &Provider{cfg: cfg}
}

// Discovery doc — RFC 8414 / OIDC Discovery 1.0.
// Served at /.well-known/openid-configuration.
func (p *Provider) HandleDiscovery(w http.ResponseWriter, r *http.Request) {
	issuer := p.cfg.OIDC.Issuer
	if issuer == "" {
		http.Error(w, "issuer not configured", http.StatusInternalServerError)
		return
	}
	doc := map[string]any{
		"issuer":                 issuer,
		"authorization_endpoint": issuer + "/oauth/authorize",
		"token_endpoint":         issuer + "/oauth/token",
		"userinfo_endpoint":      issuer + "/oauth/userinfo",
		"jwks_uri":               issuer + "/oauth/jwks",
		"end_session_endpoint":   issuer + "/oidc/logout",

		"scopes_supported": []string{"openid", "profile"},
		"response_types_supported": []string{"code"},
		"grant_types_supported":    []string{"authorization_code", "refresh_token"},
		"id_token_signing_alg_values_supported": []string{"RS256"},

		"code_challenge_methods_supported": []string{"S256"},
		"subject_types_supported":          []string{"public"},

		"token_endpoint_auth_methods_supported": []string{
			"client_secret_basic",
			"client_secret_post",
			"none", // public clients via PKCE
		},

		"claims_supported": []string{
			"sub", "iss", "aud", "exp", "iat", "nonce",
			"auth_time", "amr", "acr",
			"username", "displayName", "role", "attributes",
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_ = json.NewEncoder(w).Encode(doc)
}

// JWKS endpoint. v0.1 returns an empty key set; v0.2 will materialize
// keys from `oidc_signing_key`.
func (p *Provider) HandleJWKS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write([]byte(`{"keys":[]}`))
}

// Authorize endpoint — STUB. v0.3 will validate query params, redirect
// to /login when no session, mint an authorization code, and 302 back
// to the client's redirect_uri.
func (p *Provider) HandleAuthorize(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "/oauth/authorize: not implemented in v0.1", http.StatusNotImplemented)
}

// Token endpoint — STUB.
func (p *Provider) HandleToken(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "/oauth/token: not implemented in v0.1", http.StatusNotImplemented)
}

// Userinfo endpoint — STUB.
func (p *Provider) HandleUserinfo(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "/oauth/userinfo: not implemented in v0.1", http.StatusNotImplemented)
}

// Logout endpoint (RP-initiated) — STUB.
func (p *Provider) HandleLogout(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "/oidc/logout: not implemented in v0.1", http.StatusNotImplemented)
}
