package saml

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"prohibitorum/pkg/appaccess"
	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
	"prohibitorum/pkg/session"
	"prohibitorum/pkg/weberr"
)

var (
	ErrUnknownSP                   = errors.New("saml: unknown service provider")
	ErrInvalidACS                  = errors.New("saml: ACS URL does not match registered endpoints")
	ErrMissingSignature            = errors.New("saml: SP signature required but absent")
	ErrAccessAuthorizerUnavailable = errors.New("saml: app access authorizer unavailable")
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
	// clientIP resolves the effective client IP for audit records. nil in unit tests,
	// where auditIP falls back to the request peer.
	clientIP func(*http.Request) string
	access   appaccess.SAMLAuthorizer
}

// NewIdP constructs an IdP, building the SAML signing-key cache from queries
// and retaining every dependency the handlers attach to. The parameter order
// and names mirror the OIDC New constructor exactly.
func NewIdP(cfg *configx.Config, queries db.Querier, kvStore kv.Store, sessions *session.SessionStore, auditW audit.Writer, rl *authn.RateLimiter, clientIP func(*http.Request) string, access appaccess.SAMLAuthorizer) *IdP {
	return &IdP{
		cfg:      cfg,
		queries:  queries,
		kv:       kvStore,
		sessions: sessions,
		audit:    auditW,
		rl:       rl,
		keys:     newSAMLKeyCache(queries, cfg.DataEncryptionKeys),
		clientIP: clientIP,
		access:   access,
	}
}

func (i *IdP) evaluateSAMLAccess(ctx context.Context, accountID int32, spID int64) (appaccess.Decision, error) {
	if i.access == nil {
		return appaccess.Decision{}, ErrAccessAuthorizerUnavailable
	}
	return i.access.EvaluateSAML(ctx, accountID, spID)
}

func samlAccessAuditReason(source appaccess.DecisionSource) string {
	if source == appaccess.SourceManualDeny {
		return "manual_deny"
	}
	return "no_matching_group"
}

// auditIP returns the effective client IP for audit records: the injected resolver
// when wired, otherwise the raw request peer.
func (i *IdP) auditIP(r *http.Request) string {
	if i.clientIP != nil {
		return i.clientIP(r)
	}
	return r.RemoteAddr
}

// InvalidateKeyCache marks the SAML signing-key cache stale so the next SSO
// signing or metadata render reloads the key set from the database. Admin
// signing-key lifecycle mutations (generate / activate / retire) call this so
// the SAML metadata and the active signer reflect the change immediately
// instead of lagging by up to keyCacheTTL.
func (i *IdP) InvalidateKeyCache() {
	i.keys.invalidate()
}

// errorPage sends a browser-navigated SAML dead-end to the SPA /error page.
// Use ONLY when the IdP cannot safely produce a SAML response for the SP
// (malformed request, unknown/untrusted/disabled SP, replay, internal error).
// SP-binding responses (auto-POST success, passive/denied <StatusCode>, SLO
// responses) and the app-access-denied /error redirect must NOT route here.
//
// reason is the internal fine-grained code (samlFailureReason lookup or the
// caller's failure-step name); "" is allowed when nothing more specific is
// known. cause is the underlying error (nil for pure condition checks): its
// text is logged server-side ONLY, never sent to the browser and never
// recorded to audit.
//
// Log boundary: the failure line carries protocol fields the SP itself put on
// the wire (Issuer, ACS URL/index, Destination, SigAlg, certificate
// fingerprints and validity windows — inside cause.Error()), plus the
// correlation ids and the failure-step reason. It never carries SAMLRequest
// contents (deflated or inflated), RelayState values, Signature values,
// NameID, assertions, sessions, or tokens. These are operator logs only: no
// database write, no response body, no audit Detail — the same channel split
// as PHB-3's upstream_error.
func (i *IdP) errorPage(w http.ResponseWriter, r *http.Request, code, reason string, cause error) {
	ref := weberr.NewRef()
	args := []any{"code", code, "ref", ref, "path", r.URL.Path}
	if reason != "" {
		args = append(args, "event", "saml_flow_failure", "reason", reason)
	}
	if reqID := weberr.RequestIDFromContext(r.Context()); reqID != "" {
		args = append(args, "request_id", reqID)
	}
	if cause != nil {
		args = append(args, "saml_error", cause.Error())
	}
	slog.Warn("saml browser-facing flow error", args...)
	weberr.RedirectToError(w, r, code, ref)
}

// samlFailureReason maps a wrapped sentinel error to its stable, greppable
// internal reason code — the many-to-one partner of the public error codes
// ssoParseError/sloParseError choose. Returns "" for non-sentinel failures:
// those name the failed STEP at the callsite instead (session_load,
// replay_consume, build_response, …) because their text is a driver error
// that says nothing about which step failed.
func samlFailureReason(err error) string {
	switch {
	case errors.Is(err, ErrUnknownSP):
		return "sp_unknown"
	case errors.Is(err, ErrInvalidACS):
		return "acs_not_registered"
	case errors.Is(err, ErrBadDestination):
		return "destination_mismatch"
	case errors.Is(err, ErrMalformedRequest):
		return "request_malformed"
	case errors.Is(err, ErrOversizeRequest):
		return "request_oversize"
	case errors.Is(err, ErrMissingSAMLRequest):
		return "saml_request_missing"
	case errors.Is(err, ErrMissingSignature):
		return "signature_missing"
	case errors.Is(err, ErrBadSignature):
		return "signature_mismatch"
	case errors.Is(err, errNoSignature):
		return "signature_absent"
	case errors.Is(err, errWeakSigAlg):
		return "sigalg_weak"
	case errors.Is(err, errBadSigAlg):
		return "sigalg_bad"
	case errors.Is(err, errSigRefMismatch):
		return "signature_ref_mismatch"
	case errors.Is(err, errXMLDTD):
		return "xml_dtd_rejected"
	case errors.Is(err, errDuplicateID):
		return "xml_duplicate_id"
	case errors.Is(err, ErrStaleRequest):
		return "request_stale"
	case errors.Is(err, ErrReplayedRequest):
		return "request_replayed"
	case errors.Is(err, ErrSLOBadDestination):
		return "slo_destination_mismatch"
	case errors.Is(err, ErrSLOExpired):
		return "slo_expired"
	case errors.Is(err, ErrSLOMissingID):
		return "slo_missing_id"
	case errors.Is(err, ErrSLOStaleIssueInstant):
		return "slo_stale_issue_instant"
	case errors.Is(err, ErrSLOReplayedRequest):
		return "slo_replayed"
	default:
		return ""
	}
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
