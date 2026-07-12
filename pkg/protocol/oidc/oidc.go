// Package oidc implements Prohibitorum's OpenID Connect Provider surface.
//
// STATUS: the downstream OP is implemented across the package's files —
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
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
	"prohibitorum/pkg/session"
	"prohibitorum/pkg/weberr"
)

// Provider is the per-server OIDC handler. Constructed once by the
// server bootstrap and wired into the chi router. It carries every
// dependency the endpoint handlers (in the sibling files) require.
type Provider struct {
	cfg         *configx.Config
	queries     db.Querier
	kv          kv.Store
	sessions    *session.SessionStore
	audit       audit.Writer
	rl          *authn.RateLimiter
	keys        *keyCache
	maintenance func(context.Context) bool // nil = never in maintenance
	// clientIP resolves the effective client IP for audit records. nil in bare-Config
	// unit tests, where auditIP falls back to the request peer.
	clientIP func(*http.Request) string
}

// SetMaintenanceChecker injects a callback reporting whether maintenance mode is
// on. The forward-auth gateway uses it to deny non-admin principals, which
// authenticate off a PAT or a per-domain cookie — outside the main session
// middleware that enforces maintenance everywhere else. nil leaves the gateway
// unaffected (the default for tests).
func (p *Provider) SetMaintenanceChecker(fn func(context.Context) bool) { p.maintenance = fn }

// New constructs a Provider, building the signing-key cache from queries and
// retaining every dependency the handlers attach to.
func New(cfg *configx.Config, queries db.Querier, kvStore kv.Store, sessions *session.SessionStore, auditW audit.Writer, rl *authn.RateLimiter, clientIP func(*http.Request) string) *Provider {
	return &Provider{
		cfg:      cfg,
		queries:  queries,
		kv:       kvStore,
		sessions: sessions,
		audit:    auditW,
		rl:       rl,
		keys:     newKeyCache(queries, cfg.DataEncryptionKeys),
		clientIP: clientIP,
	}
}

// auditIP returns the effective client IP for audit records: the injected resolver
// when wired, otherwise the raw request peer (unit tests build Providers without it).
func (p *Provider) auditIP(r *http.Request) string {
	if p.clientIP != nil {
		return p.clientIP(r)
	}
	return r.RemoteAddr
}

// defaultJWKSCacheMaxAge is the compiled-in fallback for the JWKS / discovery
// Cache-Control max-age used when oidc.jwks_cache_max_age is unset (e.g. a
// hand-built test Config). A parsed config supplies its own default.
const defaultJWKSCacheMaxAge = 5 * time.Minute

// effectiveDuration returns configured when positive, else def. This is how an
// operator-supplied oidc.* lifetime overrides the compiled-in default while a
// zero/absent value falls back safely (the package consts AccessTokenTTL, … are
// the defaults).
func effectiveDuration(configured, def time.Duration) time.Duration {
	if configured > 0 {
		return configured
	}
	return def
}

// oidcConf returns the OIDC config block, nil-safe: a Provider built without a
// cfg (some endpoint unit tests) yields the zero OIDCConfig, so every resolver
// below falls back to its compiled-in default rather than nil-dereferencing.
func (p *Provider) oidcConf() configx.OIDCConfig {
	if p.cfg == nil {
		return configx.OIDCConfig{}
	}
	return p.cfg.OIDC
}

// The TTL resolvers below are the single source of truth for the effective
// OIDC lifetimes: every mint / KV-store site reads through them so a configured
// oidc.*_ttl is always honored, falling back to the package-level default const
// when unset. (configx already supplies non-zero defaults in production; the
// fallback covers tests that construct a bare Config.)
func (p *Provider) accessTokenTTL() time.Duration {
	return effectiveDuration(p.oidcConf().AccessTokenTTL, AccessTokenTTL)
}
func (p *Provider) idTokenTTL() time.Duration {
	return effectiveDuration(p.oidcConf().IDTokenTTL, IDTokenTTL)
}
func (p *Provider) refreshTokenTTL() time.Duration {
	return effectiveDuration(p.oidcConf().RefreshTokenTTL, RefreshTokenTTL)
}
func (p *Provider) authCodeTTL() time.Duration {
	return effectiveDuration(p.oidcConf().AuthorizationCodeTTL, AuthorizationCodeTTL)
}
func (p *Provider) jwksCacheMaxAge() time.Duration {
	return effectiveDuration(p.oidcConf().JWKSCacheMaxAge, defaultJWKSCacheMaxAge)
}

// jwksCacheControl renders the Cache-Control value for the JWKS + discovery
// responses from the configured max-age.
func (p *Provider) jwksCacheControl() string {
	return fmt.Sprintf("public, max-age=%d", int(p.jwksCacheMaxAge().Seconds()))
}

// HandleDiscovery serves the OP metadata document — RFC 8414 / OIDC
// Discovery 1.0 — at /.well-known/openid-configuration. It advertises the
// full OP surface (token introspection/revocation, RFC 9207 iss param).
func (p *Provider) HandleDiscovery(w http.ResponseWriter, r *http.Request) {
	issuer := p.cfg.OIDC.Issuer
	if issuer == "" {
		requestID := weberr.RequestIDFromContext(r.Context())
		slog.Warn("oidc discovery: issuer not configured", "request_id", requestID)
		weberr.WriteJSON(w, "server_error", nil, requestID)
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

		"scopes_supported":                      SupportedScopes(),
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"id_token_signing_alg_values_supported": []string{"RS256"},

		"code_challenge_methods_supported": []string{"S256"},
		"subject_types_supported":          []string{"public"},
		// OIDC Core §3.1.2.1 prompt values honored by /authorize (T2.3).
		// select_account is accepted but a no-op for this single-tenant OP.
		"prompt_values_supported": []string{"none", "login", "consent", "select_account"},

		// RFC 9207: we set the `iss` parameter on authorization responses.
		"authorization_response_iss_parameter_supported": true,

		"token_endpoint_auth_methods_supported":         authMethods,
		"introspection_endpoint_auth_methods_supported": authMethods,
		"revocation_endpoint_auth_methods_supported":    authMethods,

		"claims_supported": []string{
			"sub", "iss", "aud", "exp", "iat", "nonce",
			"auth_time", "amr", "acr", "sid", "at_hash",
			"username", "displayName", "role", "attributes",
			"email", "email_verified",
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", p.jwksCacheControl())
	_ = json.NewEncoder(w).Encode(doc)
}

// InvalidateKeyCache marks the signing-key cache stale so the next JWKS read or
// token-signing operation reloads the key set from the database. Admin
// signing-key lifecycle mutations (generate / activate / retire) call this so
// the published JWKS and the active signer reflect the change immediately
// instead of lagging by up to keyCacheTTL.
func (p *Provider) InvalidateKeyCache() {
	p.keys.invalidate()
}

// HandleJWKS serves the active public signing keys from the key cache as a
// JWK Set at /oauth/jwks.
func (p *Provider) HandleJWKS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", p.jwksCacheControl())
	_ = json.NewEncoder(w).Encode(p.keys.jwks(r.Context()))
}
