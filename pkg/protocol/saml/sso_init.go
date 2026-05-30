package saml

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
)

// HandleIdPInitiated implements the IdP-initiated SSO profile (spec D11) at
// GET /saml/sso/init?sp=<entity_id>[&RelayState=…]. It is an app-launcher: the
// authenticated user picks an SP and the IdP emits an UNSOLICITED Response
// (no InResponseTo) to that SP's DEFAULT AssertionConsumerService.
//
// SECURITY — this is an unsolicited-assertion emitter, so it carries two
// deliberate guardrails that the SP-initiated HandleSSO does not need:
//
//  1. Per-SP opt-in (sp.AllowIdpInitiated, default false). An unsolicited
//     Response has no AuthnRequest to anchor it, so it is exactly the shape a
//     forged-login / login-CSRF attack wants. We refuse to emit one unless the
//     SP explicitly opted in — mirroring GHES, which rejects unsolicited SAML
//     unless the integration is configured to accept it.
//  2. Default-ACS-only delivery. The assertion goes ONLY to the SP's registered
//     default ACS (resolveACS with empty URL+index), never to a caller-supplied
//     URL — the same open-redirect / assertion-exfiltration guard as HandleSSO.
//
// Unlike HandleSSO this is NOT an AuthnRequest flow: there is no inbound request
// to parse/replay-consume, no SP signature to verify (we are the initiator), no
// NameIDPolicy and no ForceAuthn. The flow is: session gate → SP lookup →
// opt-in check → default ACS → build+sign unsolicited Response → persist
// saml_session → audit → auto-POST.
func (i *IdP) HandleIdPInitiated(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Session gate — IDENTICAL to HandleSSO's gate (nil session, the
	// disabled-mid-session sentinel with Data==nil, or an explicitly-disabled
	// account all count as "not authenticated"). Bounce to OUR OWN login with a
	// return_to back to this exact launcher URL; do NOT bounce to any SP. This
	// URL carries no signed query, so RequestURI() (path + raw query) is a safe,
	// faithful return target.
	sess := authn.SessionFromContext(ctx)
	if sess == nil || sess.Data == nil || sess.Account == nil || sess.Account.Disabled {
		returnTo := i.entityID() + r.URL.RequestURI()
		loginURL := i.entityID() + "/login?return_to=" + url.QueryEscape(returnTo)
		http.Redirect(w, r, loginURL, http.StatusFound)
		return
	}

	// SP lookup. The sp param is on the UNTRUSTED side of the open-redirect
	// guard (until we resolve a registered ACS), so an empty/unknown sp is a
	// DIRECT 400 and is NEVER redirected anywhere.
	spParam := r.URL.Query().Get("sp")
	if spParam == "" {
		http.Error(w, "missing sp parameter", http.StatusBadRequest)
		return
	}
	sp, err := i.queries.GetSAMLSPByEntityID(ctx, spParam)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "unknown SP", http.StatusBadRequest)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Per-SP opt-in guard. Emitting an unsolicited assertion to an SP that did
	// not ask for IdP-initiated SSO is refused outright (GHES posture).
	if !sp.AllowIdpInitiated {
		http.Error(w, "IdP-initiated SSO is not enabled for this SP", http.StatusForbidden)
		return
	}

	// Resolve the SP's DEFAULT ACS (empty URL + empty index → IsDefault, else
	// lowest-index). This is the open-redirect guard: the assertion is delivered
	// ONLY to a registered ACS. An SP with zero registered ACS rows yields
	// ErrInvalidACS → 500 (a registration error, not a client error).
	acsURL, err := i.resolveACS(ctx, sp.ID, "", "")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// The session carries the live db.Account row.
	account := *sess.Account

	// Per-account + per-SP rate limit. This is an unsolicited-assertion emitter
	// (signed Response + InsertSAMLSession row per call), so it MUST be bounded
	// exactly like the SP-initiated HandleSSO: same max/window, distinct
	// "ssoinit" key prefixes so the two flows don't share buckets. The user is
	// authenticated, so over-limit is a direct 429 (with Retry-After, mirroring
	// HandleSSO) rather than a login bounce. Placed AFTER the opt-in + ACS
	// resolution but BEFORE buildResponse/InsertSAMLSession so nothing is issued
	// or persisted once the limit is hit.
	acctKey := "saml:ssoinit:acct:" + strconv.Itoa(int(account.ID))
	spKey := "saml:ssoinit:sp:" + sp.EntityID
	for _, key := range []string{acctKey, spKey} {
		if !i.rl.Allow(key, samlSSORateMax, samlSSORateWindow) {
			if ra := i.rl.RetryAfter(key); ra > 0 {
				w.Header().Set("Retry-After", strconv.Itoa(int(ra.Seconds())+1))
			}
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
	}

	// Recover the authentication-context snapshot for AuthnInstant and the
	// session-expiry horizon (same source HandleSSO uses).
	row, err := i.queries.GetSession(ctx, sess.Data.SessionID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	authTime := row.AuthTime.Time

	// Stable, opaque, per-(account,sp) NameID.
	nameID, err := i.subjectID(ctx, account.ID, sp.ID, sp.NameIDFormat)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Project the account into SAML attributes per the SP's map.
	attrs, err := projectAttributes(account, sp.AttributeMap)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	sessionIndex := sess.Data.SessionID

	// Build + sign the UNSOLICITED Response: inResponseTo is "" so crewjam's
	// Element() guards (Response and SubjectConfirmationData both `if
	// InResponseTo != ""`) OMIT the attribute — producing the unsolicited shape
	// an SP expects for IdP-initiated SSO.
	respXML, err := i.buildResponse(ctx, sp, acsURL, "", nameID, attrs, authTime, sessionIndex)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Persist a saml_session row so SLO can later locate + revoke this SP
	// session. Anchor the expiry on authTime (same base as buildResponse) so the
	// DB row never outlives the assertion's SessionNotOnOrAfter.
	sessionExpiry := sessionNotOnOrAfter(sp, authTime)
	if _, err := i.queries.InsertSAMLSession(ctx, db.InsertSAMLSessionParams{
		SessionID:    sess.Data.SessionID,
		SpID:         sp.ID,
		NameID:       nameID,
		SessionIndex: sessionIndex,
		NotOnOrAfter: pgtype.Timestamptz{Time: sessionExpiry, Valid: true},
	}); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Best-effort audit of the IdP-initiated SP use.
	accountID := account.ID
	_ = i.audit.Record(ctx, audit.Record{
		AccountID: &accountID,
		Factor:    audit.FactorSAMLSP,
		Event:     audit.EventUse,
		IP:        audit.ParseIPOrNil(r.RemoteAddr),
		UserAgent: r.UserAgent(),
		Detail: map[string]any{
			"reason": "idp_initiated",
			"sp":     sp.EntityID,
		},
	})

	// Auto-POST the Response to the SP's DEFAULT ACS. RelayState is passed
	// through VERBATIM (may be empty): it is the SP's deep-link / target, and
	// the auto-POST template HTML-escapes it.
	i.writeAutoPost(w, acsURL, respXML, r.URL.Query().Get("RelayState"))
}
