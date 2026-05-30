package saml

import (
	"errors"

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

// entityID is the IdP's SAML EntityID — the first configured public origin.
// Returns "" (rather than panicking) if no origin is configured.
func (i *IdP) entityID() string {
	if i.cfg == nil || len(i.cfg.PublicOrigins) == 0 {
		return ""
	}
	return i.cfg.PublicOrigins[0]
}

// ssoURL is the IdP's SingleSignOnService endpoint.
func (i *IdP) ssoURL() string {
	base := i.entityID()
	if base == "" {
		return ""
	}
	return base + "/saml/sso"
}

// sloURL is the IdP's SingleLogoutService endpoint.
func (i *IdP) sloURL() string {
	base := i.entityID()
	if base == "" {
		return ""
	}
	return base + "/saml/slo"
}

// metadataURL is the IdP's metadata document endpoint.
func (i *IdP) metadataURL() string {
	base := i.entityID()
	if base == "" {
		return ""
	}
	return base + "/saml/metadata"
}
