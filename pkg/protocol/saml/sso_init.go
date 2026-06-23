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
		returnTo := i.baseURL() + r.URL.RequestURI()
		loginURL := i.baseURL() + "/login?return_to=" + url.QueryEscape(returnTo)
		http.Redirect(w, r, loginURL, http.StatusFound)
		return
	}

	// Spec §3.4.3: RelayState MUST NOT exceed 80 bytes (N7). It is echoed
	// verbatim (HTML-escaped) into the auto-POST form, so bound it up front.
	if len(r.URL.Query().Get("RelayState")) > maxRelayStateBytes {
		i.errorPage(w, r, "saml_request_invalid")
		return
	}

	// SP lookup. The sp param is on the UNTRUSTED side of the open-redirect
	// guard (until we resolve a registered ACS), so an empty/unknown sp dead-ends
	// at the IdP's OWN SPA /error page and is NEVER redirected to an SP URL.
	spParam := r.URL.Query().Get("sp")
	if spParam == "" {
		i.errorPage(w, r, "saml_request_invalid")
		return
	}
	sp, err := i.queries.GetSAMLSPByEntityID(ctx, spParam)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			i.errorPage(w, r, "saml_sp_unknown")
			return
		}
		i.errorPage(w, r, "server_error")
		return
	}
	// A disabled SP is treated as if it were unregistered — the flow is denied.
	if sp.Disabled {
		i.errorPage(w, r, "saml_sp_unknown")
		return
	}

	// Per-SP opt-in guard. Emitting an unsolicited assertion to an SP that did
	// not ask for IdP-initiated SSO is refused outright (GHES posture).
	if !sp.AllowIdpInitiated {
		i.errorPage(w, r, "saml_idp_init_disabled")
		return
	}

	// Resolve the SP's DEFAULT ACS (empty URL + empty index → IsDefault, else
	// lowest-index). This is the open-redirect guard: the assertion is delivered
	// ONLY to a registered ACS. An SP with zero registered ACS rows yields
	// ErrInvalidACS → 500 (a registration error, not a client error).
	acsURL, err := i.resolveACS(ctx, sp.ID, "", "")
	if err != nil {
		i.errorPage(w, r, "server_error")
		return
	}

	// The session carries the live db.Account row.
	account := *sess.Account

	// Per-app access gate (RBAC). The user is authenticated and enabled; a
	// restricted SP requires a direct or via-group grant. NO admin bypass.
	// IdP-initiated SSO is ALWAYS interactive (there is no IsPassive), so a denial
	// goes ONLY to the IdP's own /error page — never a terminal SAML Response.
	// Fail CLOSED: a predicate error is a direct 500. Placed before the
	// rate-limit / build / persist so nothing is issued for an unauthorized user.
	authzed, aerr := i.queries.IsAccountAuthorizedForSAMLSP(ctx, db.IsAccountAuthorizedForSAMLSPParams{
		AccountID: pgtype.Int4{Int32: account.ID, Valid: true},
		SpID:      sp.ID,
	})
	if aerr != nil {
		i.errorPage(w, r, "server_error")
		return
	}
	if !authzed.Bool {
		acctID := account.ID
		_ = i.audit.Record(ctx, audit.Record{
			AccountID: &acctID,
			Factor:    audit.FactorSAMLSP,
			Event:     audit.EventAccessDenied,
			IP:        audit.ParseIPOrNil(r.RemoteAddr),
			UserAgent: r.UserAgent(),
			Detail: map[string]any{
				"reason": "app_access_denied",
				"sp":     sp.EntityID,
			},
		})
		u := i.baseURL() + "/error?reason=app_access_denied&app=" + url.QueryEscape(sp.DisplayName)
		http.Redirect(w, r, u, http.StatusFound)
		return
	}

	// Advisory consent gate. IdP-initiated SSO is always interactive (no
	// IsPassive), so always honor it. Placed after RBAC and before the rate
	// limit / build / persist so nothing is issued for an un-acknowledged SP.
	if redirected, cerr := i.maybeDemandSAMLConsent(w, r, account.ID, sp); cerr != nil {
		i.errorPage(w, r, "server_error")
		return
	} else if redirected {
		return
	}

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
			i.errorPage(w, r, "rate_limited")
			return
		}
	}

	// Recover the authentication-context snapshot for AuthnInstant and the
	// session-expiry horizon (same source HandleSSO uses).
	row, err := i.queries.GetSession(ctx, sess.Data.SessionID)
	if err != nil {
		i.errorPage(w, r, "server_error")
		return
	}
	authTime := row.AuthTime.Time

	// Issue the assertion (shared with SP-initiated + consent-resume).
	i.issueAssertion(w, r, account, sp, acsURL, "", r.URL.Query().Get("RelayState"), authTime, sess.Data.SessionID, "idp_initiated")
}
