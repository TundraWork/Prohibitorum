package saml

import (
	"context"
	"encoding/base64"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/beevik/etree"
	crewjam "github.com/crewjam/saml"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
)

// samlSSORate caps SP-initiated SSO issuance per authenticated account and per
// SP. 60/min is generous for any legitimate browser SSO flow (each is a single
// redirect) while bounding a compromised session's ability to spray assertions
// at one or many SPs. Mirrors the OIDC authorizeRate* constants.
const (
	samlSSORateMax    = 60
	samlSSORateWindow = time.Minute
)

// SAML status-code URIs for non-success Responses. Pinned here to guard against
// namespace drift.
const (
	statusRequester = "urn:oasis:names:tc:SAML:2.0:status:Requester"
	statusNoPassive = "urn:oasis:names:tc:SAML:2.0:status:NoPassive"
)

// autoPostFormTmpl renders a self-submitting HTML form that POSTs a SAMLResponse
// (and optional RelayState) to the SP's ACS URL — the HTTP-POST binding's
// browser bounce. html/template auto-escapes every interpolation: the ACS URL
// is already DB-validated (resolveACS only ever returns a registered Location),
// and RelayState is attacker-influenced so MUST be HTML-escaped here. A
// <noscript> submit button keeps the flow usable without JavaScript.
var autoPostFormTmpl = template.Must(template.New("samlpost").Parse(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>Continue sign-in</title></head>
<body onload="document.forms[0].submit()">
<noscript><p>JavaScript is disabled. Click the button to continue.</p></noscript>
<form method="post" action="{{.ACSURL}}">
<input type="hidden" name="SAMLResponse" value="{{.SAMLResponse}}">
{{if .HasRelayState}}<input type="hidden" name="RelayState" value="{{.RelayState}}">
{{end}}<noscript><input type="submit" value="Continue"></noscript>
</form>
</body>
</html>`))

// autoPostData is the template payload for autoPostFormTmpl.
type autoPostData struct {
	ACSURL        string
	SAMLResponse  string
	RelayState    string
	HasRelayState bool
}

// HandleSSO implements the SP-initiated Web Browser SSO profile at
// GET /saml/sso. It orchestrates the full flow described in the v0.5 design
// (§Data flow steps 2–9): parse + validate the inbound AuthnRequest, gate on a
// live IdP session (bouncing unauthenticated users to /login), rate-limit,
// enforce single-use replay, resolve the NameID + attributes, build + sign the
// Response, persist a saml_session row for SLO, audit, and auto-POST the
// Response to the SP's ACS.
//
// SECURITY — error-channel ordering: until the AuthnRequest is parsed and its
// SP + ACS are DB-validated, the request target is UNTRUSTED, so EVERY parse
// failure is rendered as a DIRECT http.Error and NEVER redirected to an
// SP-supplied URL (open-redirect / assertion-exfiltration guard, mirroring the
// OIDC authorize handler).
func (i *IdP) HandleSSO(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// (2/3/4-parse) Parse + validate. Any error here is on the untrusted side
	// of the open-redirect guard → DIRECT error, never a redirect to an
	// SP-chosen target. Bad-request-class errors map to 400; an unexpected
	// (e.g. DB) error maps to 500.
	req, err := i.parseAuthnRequest(ctx, r)
	if err != nil {
		i.ssoParseError(w, err)
		return
	}
	sp := req.SP

	// (5) Session gate. A nil session, the disabled-mid-session sentinel
	// (non-nil Session with Data == nil, attached by LoadSession when an
	// account is disabled), or an explicitly-disabled account all count as
	// "not authenticated". Widening this guard also keeps the sess.Data deref
	// below safe (the v0.4 deep-audit lesson). Mirrors OIDC HandleAuthorize.
	sess := authn.SessionFromContext(ctx)
	if sess == nil || sess.Data == nil || (sess.Account != nil && sess.Account.Disabled) {
		if req.IsPassive {
			// The SP forbade an interactive login bounce. Issue a terminal
			// NoPassive Response (no assertion) and auto-POST it to the ACS.
			// Because this IS a terminal Response, consume the replay key here
			// too — a NoPassive answer counts as the single use of this ID.
			if cerr := i.consumeAuthnRequestID(ctx, req.RequestID); cerr != nil {
				if errors.Is(cerr, ErrReplayedRequest) {
					http.Error(w, "AuthnRequest replayed", http.StatusBadRequest)
				} else {
					http.Error(w, "internal error", http.StatusInternalServerError)
				}
				return
			}
			respXML, berr := i.buildStatusResponse(ctx, req.ACSURL, req.RequestID, statusRequester, statusNoPassive)
			if berr != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			i.writeAutoPost(w, req.ACSURL, respXML, req.RelayState)
			return
		}
		// Send the user to the login page; on success they return to this exact
		// SSO URL and the flow re-runs and issues. This is NOT an SP redirect,
		// so use a plain redirect to our own login.
		fullSSOURL := i.entityID() + r.URL.RequestURI()
		loginURL := i.entityID() + "/login?return_to=" + url.QueryEscape(fullSSOURL)
		http.Redirect(w, r, loginURL, http.StatusFound)
		return
	}

	// (4-rate) Per-account + per-SP rate limit. The user is authenticated, so a
	// direct 429 is appropriate (no point bouncing an over-limit caller).
	acctKey := "saml:sso:acct:" + strconv.Itoa(int(sess.Data.AccountID))
	spKey := "saml:sso:sp:" + sp.EntityID
	for _, key := range []string{acctKey, spKey} {
		if !i.rl.Allow(key, samlSSORateMax, samlSSORateWindow) {
			if ra := i.rl.RetryAfter(key); ra > 0 {
				w.Header().Set("Retry-After", strconv.Itoa(int(ra.Seconds())+1))
			}
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
	}

	// (4-replay) Single-use replay enforcement on the terminal/issue path. A
	// replayed ID is a client error → 400; any other KV error → 500.
	if cerr := i.consumeAuthnRequestID(ctx, req.RequestID); cerr != nil {
		if errors.Is(cerr, ErrReplayedRequest) {
			http.Error(w, "AuthnRequest replayed", http.StatusBadRequest)
		} else {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	// (6/7-prep) Recover the authentication-context snapshot for AuthnInstant.
	row, err := i.queries.GetSession(ctx, sess.Data.SessionID)
	if err != nil {
		// Uniform body (mirrors the OIDC authorize handler) so the HTTP response
		// never leaks which backend step failed; the specific cause stays in err.
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	authTime := row.AuthTime.Time

	// The session carries the live db.Account row.
	account := *sess.Account

	// (6) Stable, opaque, per-(account,sp) NameID.
	nameID, err := i.subjectID(ctx, account.ID, sp.ID, sp.NameIDFormat)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// (7-attrs) Project the account into SAML attributes per the SP's map.
	attrs, err := projectAttributes(account, sp.AttributeMap)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	sessionIndex := sess.Data.SessionID

	// (7) Build + sign the Response (which carries a signed bearer Assertion).
	respXML, err := i.buildResponse(ctx, sp, req.ACSURL, req.RequestID, nameID, attrs, authTime, sessionIndex)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// (8) Persist a saml_session row so SLO can later locate + revoke this SP
	// session. The expiry MUST mirror the assertion's SessionNotOnOrAfter
	// horizon, so use the SAME base (authTime) as buildResponse — anchoring on
	// time.Now() here would let the DB row outlive the assertion's
	// SessionNotOnOrAfter whenever the session is older than "now".
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

	// (8-audit) Best-effort audit of the SP use.
	accountID := account.ID
	_ = i.audit.Record(ctx, audit.Record{
		AccountID: &accountID,
		Factor:    audit.FactorSAMLSP,
		Event:     audit.EventUse,
		IP:        audit.ParseIPOrNil(r.RemoteAddr),
		UserAgent: r.UserAgent(),
		Detail: map[string]any{
			"reason": "sso",
			"sp":     sp.EntityID,
		},
	})

	// (9) Auto-POST the Response to the SP's ACS.
	i.writeAutoPost(w, req.ACSURL, respXML, req.RelayState)
}

// ssoParseError maps a parseAuthnRequest error to a DIRECT HTTP error. Every
// case is on the untrusted side of the open-redirect guard, so none redirect.
// Client-class errors (decode/SP/signature/ACS/Destination/malformed/oversize)
// collapse to 400; anything else (e.g. a DB failure during SP lookup) is 500.
func (i *IdP) ssoParseError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnknownSP),
		errors.Is(err, ErrInvalidACS),
		errors.Is(err, ErrBadDestination),
		errors.Is(err, ErrMalformedRequest),
		errors.Is(err, ErrOversizeRequest),
		errors.Is(err, ErrMissingSAMLRequest),
		errors.Is(err, ErrMissingSignature),
		errors.Is(err, ErrBadSignature),
		errors.Is(err, errWeakSigAlg):
		http.Error(w, "invalid SAML AuthnRequest", http.StatusBadRequest)
	default:
		// Unexpected (e.g. DB unavailable, decompression-internal, XML library
		// error). Fail closed with a server error.
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// writeAutoPost renders the self-submitting POST-binding form carrying the
// base64 SAMLResponse (and echoed RelayState, if present) to the SP's ACS.
func (i *IdP) writeAutoPost(w http.ResponseWriter, acsURL string, respXML []byte, relayState string) {
	data := autoPostData{
		ACSURL:        acsURL,
		SAMLResponse:  base64.StdEncoding.EncodeToString(respXML),
		RelayState:    relayState,
		HasRelayState: relayState != "",
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Defensive: this is sensitive (carries a signed assertion); never cache.
	w.Header().Set("Cache-Control", "no-store")
	if err := autoPostFormTmpl.Execute(w, data); err != nil {
		// Headers (and likely some body) are already written; we can only log
		// via a best-effort fallback. There is nothing further to send.
		return
	}
}

// buildStatusResponse builds, signs, and serializes a SAML Response that carries
// ONLY a Status (no Assertion) — used for terminal non-success answers such as
// NoPassive. It reuses the buildResponse build/sign/serialize pattern: emit the
// crewjam Response element (Issuer + Status, no Assertion), sign it via
// signElement, and serialize the resulting etree document to wire bytes.
func (i *IdP) buildStatusResponse(ctx context.Context, acsURL, inResponseTo, topStatus, subStatus string) ([]byte, error) {
	priv, certDER, _, ok := i.keys.signingKey(ctx)
	if !ok {
		return nil, errNoSigningKey
	}

	responseID, err := newSAMLID()
	if err != nil {
		return nil, err
	}

	response := crewjam.Response{
		ID:           responseID,
		InResponseTo: inResponseTo,
		Version:      "2.0",
		IssueInstant: time.Now(),
		Destination:  acsURL,
		Issuer:       &crewjam.Issuer{Value: i.entityID()},
		Status: crewjam.Status{
			StatusCode: crewjam.StatusCode{
				Value: topStatus,
				StatusCode: &crewjam.StatusCode{
					Value: subStatus,
				},
			},
		},
		Assertion: nil,
	}

	responseEl := response.Element()
	signedResponse, err := signElement(responseEl, priv, certDER)
	if err != nil {
		return nil, err
	}

	doc := etree.NewDocument()
	doc.SetRoot(signedResponse)
	return doc.WriteToBytes()
}
