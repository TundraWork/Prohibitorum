package saml

import (
	"errors"
	"strings"
	"time"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
	"prohibitorum/pkg/session"
)

var (
	ErrUnknownSP        = errors.New("saml: unknown service provider")
	ErrInvalidACS       = errors.New("saml: ACS URL does not match registered endpoints")
	ErrMissingSignature = errors.New("saml: SP signature required but absent")
)

// IdP is the per-server SAML Identity Provider handler. It mirrors the OIDC
// Provider: constructed once by the server bootstrap, it carries every
// dependency the endpoint handlers (in sibling files) require, including the
// reused signing-key cache.
type IdP struct {
	cfg      *configx.Config
	queries  db.Querier
	kv       kv.Store
	sessions *session.SessionStore
	audit    audit.Writer
	rl       *authn.RateLimiter
	keys     *samlKeyCache
}

// NewIdP constructs an IdP, building the SAML signing-key cache from queries
// and retaining every dependency the handlers attach to. The parameter order
// and names mirror the OIDC New constructor exactly.
func NewIdP(cfg *configx.Config, queries db.Querier, kvStore kv.Store, sessions *session.SessionStore, auditW audit.Writer, rl *authn.RateLimiter) *IdP {
	return &IdP{
		cfg:      cfg,
		queries:  queries,
		kv:       kvStore,
		sessions: sessions,
		audit:    auditW,
		rl:       rl,
		keys:     newSAMLKeyCache(queries),
	}
}

// InvalidateKeyCache marks the SAML signing-key cache stale so the next SSO
// signing or metadata render reloads the key set from the database. Admin
// signing-key lifecycle mutations (generate / activate / retire) call this so
// the SAML metadata and the active signer reflect the change immediately
// instead of lagging by up to keyCacheTTL.
func (i *IdP) InvalidateKeyCache() {
	i.keys.invalidate()
}

// entityID is the IdP's SAML EntityID — the stable identifier SPs key trust on.
// It is the operator-configured saml.entity_id when set, otherwise the first
// public origin. Per SAML metadata best practice the EntityID is an IDENTIFIER,
// not a location: it need not be a reachable URL (a URN is valid) and SHOULD be
// chosen to never change, because changing it invalidates the trust every
// registered SP has on file. Endpoint URLs are built from baseURL(), NOT this —
// so an operator can pin a stable EntityID independent of the HTTP origin.
// Returns "" (rather than panicking) if neither is configured.
func (i *IdP) entityID() string {
	if i.cfg == nil {
		return ""
	}
	if id := strings.TrimSpace(i.cfg.SAML.EntityID); id != "" {
		return id
	}
	if len(i.cfg.PublicOrigins) == 0 {
		return ""
	}
	return i.cfg.PublicOrigins[0]
}

// baseURL is the reachable origin used to construct the IdP's SAML endpoint URLs
// (SSO/SLO/metadata) and the dashboard login-bounce redirects. Unlike entityID
// it MUST be a real, reachable origin, so it is always the first public origin —
// never the possibly-symbolic saml.entity_id. Returns "" if no origin is set.
func (i *IdP) baseURL() string {
	if i.cfg == nil || len(i.cfg.PublicOrigins) == 0 {
		return ""
	}
	return i.cfg.PublicOrigins[0]
}

// samlSessionLifetime is the SessionNotOnOrAfter horizon used when a SP does not
// set an explicit session_lifetime: the operator-configured saml.session_lifetime
// when positive, else the package default. (Per-SP session_lifetime still wins
// over this; see sessionNotOnOrAfter.)
func (i *IdP) samlSessionLifetime() time.Duration {
	if i.cfg != nil && i.cfg.SAML.SessionLifetime > 0 {
		return i.cfg.SAML.SessionLifetime
	}
	return defaultSessionLifetime
}

// ssoURL is the IdP's SingleSignOnService endpoint.
func (i *IdP) ssoURL() string {
	base := i.baseURL()
	if base == "" {
		return ""
	}
	return base + "/saml/sso"
}

// sloURL is the IdP's SingleLogoutService endpoint.
func (i *IdP) sloURL() string {
	base := i.baseURL()
	if base == "" {
		return ""
	}
	return base + "/saml/slo"
}

// metadataURL is the IdP's metadata document endpoint.
func (i *IdP) metadataURL() string {
	base := i.baseURL()
	if base == "" {
		return ""
	}
	return base + "/saml/metadata"
}
