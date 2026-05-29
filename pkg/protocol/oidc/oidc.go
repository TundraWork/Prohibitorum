// Package oidc implements Prohibitorum's OpenID Connect Provider surface.
//
// v0.4 STATUS: the downstream OP is implemented across the package's files —
// discovery and JWKS here; client load/auth (client.go), claim projection
// (claims.go), authorization-code and refresh-token stores (codes.go,
// refresh.go), JWT sign/verify and the signing-key cache (jwt.go, keys.go).
// The endpoint handlers (/authorize, /token, /userinfo, /introspect,
// /revoke, /oidc/logout) are attached as methods on Provider in their own
// files. The Provider carries every dependency those handlers need.
//
// See AUDIT.md for the full best-practice checklist.
package oidc

import (
	"encoding/json"
	"net/http"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
	"prohibitorum/pkg/session"
)

// Provider is the per-server OIDC handler. Constructed once by the
// server bootstrap and wired into the chi router. It carries every
// dependency the endpoint handlers (in the sibling files) require.
type Provider struct {
	cfg      *configx.Config
	queries  db.Querier
	kv       kv.Store
	sessions *session.SessionStore
	audit    audit.Writer
	rl       *authn.RateLimiter
	keys     *keyCache
}

// New constructs a Provider, building the signing-key cache from queries and
// retaining every dependency the handlers attach to.
func New(cfg *configx.Config, queries db.Querier, kvStore kv.Store, sessions *session.SessionStore, auditW audit.Writer, rl *authn.RateLimiter) *Provider {
	return &Provider{
		cfg:      cfg,
		queries:  queries,
		kv:       kvStore,
		sessions: sessions,
		audit:    auditW,
		rl:       rl,
		keys:     newKeyCache(queries),
	}
}

// HandleDiscovery serves the OP metadata document — RFC 8414 / OIDC
// Discovery 1.0 — at /.well-known/openid-configuration. It advertises the
// full v0.4 surface (token introspection/revocation, RFC 9207 iss param).
func (p *Provider) HandleDiscovery(w http.ResponseWriter, r *http.Request) {
	issuer := p.cfg.OIDC.Issuer
	if issuer == "" {
		http.Error(w, "issuer not configured", http.StatusInternalServerError)
		return
	}
	authMethods := []string{
		"client_secret_basic",
		"client_secret_post",
		"none", // public clients via PKCE
	}
	doc := map[string]any{
		"issuer":                 issuer,
		"authorization_endpoint": issuer + "/oauth/authorize",
		"token_endpoint":         issuer + "/oauth/token",
		"userinfo_endpoint":      issuer + "/oauth/userinfo",
		"jwks_uri":               issuer + "/oauth/jwks",
		"introspection_endpoint": issuer + "/oauth/introspect",
		"revocation_endpoint":    issuer + "/oauth/revoke",
		"end_session_endpoint":   issuer + "/oidc/logout",

		"scopes_supported":                      []string{"openid", "profile", "offline_access"},
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"id_token_signing_alg_values_supported": []string{"RS256"},

		"code_challenge_methods_supported": []string{"S256"},
		"subject_types_supported":          []string{"public"},

		// RFC 9207: we set the `iss` parameter on authorization responses.
		"authorization_response_iss_parameter_supported": true,

		"token_endpoint_auth_methods_supported":         authMethods,
		"introspection_endpoint_auth_methods_supported": authMethods,
		"revocation_endpoint_auth_methods_supported":    authMethods,

		"claims_supported": []string{
			"sub", "iss", "aud", "exp", "iat", "nonce",
			"auth_time", "amr", "acr", "sid", "at_hash",
			"username", "displayName", "role", "attributes",
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_ = json.NewEncoder(w).Encode(doc)
}

// HandleJWKS serves the active public signing keys from the key cache as a
// JWK Set at /oauth/jwks.
func (p *Provider) HandleJWKS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_ = json.NewEncoder(w).Encode(p.keys.jwks(r.Context()))
}
