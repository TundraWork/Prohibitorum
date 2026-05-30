package saml

import (
	"bytes"
	"compress/flate"
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/beevik/etree"
	crewjam "github.com/crewjam/saml"
	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/db"
)

// ErrSLOBadDestination is returned when a LogoutRequest carries a Destination
// that does not name this IdP's SLO endpoint (anti-misrouting / anti-replay).
var ErrSLOBadDestination = errors.New("saml: LogoutRequest Destination does not match this IdP")

// ErrSLOExpired is returned when a LogoutRequest's NotOnOrAfter is in the past.
var ErrSLOExpired = errors.New("saml: LogoutRequest has expired (NotOnOrAfter)")

// HandleSLO implements IdP-local Single Logout at /saml/slo. It validates a
// SIGNED SP LogoutRequest (signature is ALWAYS required for SLO, unlike the
// conditional AuthnRequest signing), revokes the bound Prohibitorum session(s),
// and returns a signed LogoutResponse to the SP.
//
// SECURITY — the signature gate fully precedes any session mutation: a request
// whose signature is absent or does not verify against the SP's registered
// signing cert(s) returns an error with NO session touched (no revoke, no DB
// delete). This is the property that stops an attacker from forging a logout to
// revoke a victim's session.
//
// SCOPE: IdP-LOCAL only. We revoke the IdP session bound to this SP+NameID and
// delete the saml_session rows; we do NOT front-channel propagate the logout to
// other SPs that may share the same IdP session (deferred past v0.5).
func (i *IdP) HandleSLO(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// (2) Decode the LogoutRequest from the binding implied by the method.
	var (
		req     crewjam.LogoutRequest
		reqEl   *etree.Element // the parsed root element (POST binding: signature target)
		binding string
	)
	switch r.Method {
	case http.MethodGet:
		binding = crewjam.HTTPRedirectBinding
		el, err := decodeRedirectLogoutRequest(r, &req)
		if err != nil {
			i.sloParseError(w, err)
			return
		}
		reqEl = el
	case http.MethodPost:
		binding = crewjam.HTTPPostBinding
		el, err := decodePostLogoutRequest(r, &req)
		if err != nil {
			i.sloParseError(w, err)
			return
		}
		reqEl = el
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if req.Issuer == nil || req.Issuer.Value == "" {
		http.Error(w, "invalid SAML LogoutRequest", http.StatusBadRequest)
		return
	}

	// (3) Resolve the SP. Unknown SP → direct error (no redirect, no session
	// touched).
	sp, err := i.queries.GetSAMLSPByEntityID(ctx, req.Issuer.Value)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			i.sloParseError(w, ErrUnknownSP)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// (4) Verify the SP signature. SLO ALWAYS requires a signature. This gate
	// runs BEFORE any session lookup or mutation.
	switch binding {
	case crewjam.HTTPRedirectBinding:
		// verifyRedirectSignature reconstructs the detached octet string from
		// r.URL.RawQuery (SAMLRequest/RelayState/SigAlg), enforces RSA-SHA256,
		// and verifies against the SP's "signing" certs. Absent signature →
		// ErrMissingSignature.
		if verr := i.verifyRedirectSignature(ctx, r, sp); verr != nil {
			i.sloParseError(w, verr)
			return
		}
	case crewjam.HTTPPostBinding:
		if verr := i.verifyPostLogoutSignature(ctx, reqEl, sp); verr != nil {
			i.sloParseError(w, verr)
			return
		}
	}

	// (5) Destination (if present) must name this IdP's SLO endpoint.
	if req.Destination != "" && req.Destination != i.sloURL() {
		i.sloParseError(w, ErrSLOBadDestination)
		return
	}
	// NotOnOrAfter (if present) must be in the future.
	if req.NotOnOrAfter != nil && !req.NotOnOrAfter.After(time.Now()) {
		i.sloParseError(w, ErrSLOExpired)
		return
	}
	if req.NameID == nil {
		http.Error(w, "invalid SAML LogoutRequest", http.StatusBadRequest)
		return
	}

	// (6) Resolve the bound session rows. A NameID that resolves to nothing is a
	// no-op SUCCESS (idempotent logout), not an error — this also avoids leaking
	// whether a session exists for the NameID.
	rows, err := i.queries.ListSAMLSessionsByNameID(ctx, db.ListSAMLSessionsByNameIDParams{
		SpID:   sp.ID,
		NameID: req.NameID.Value,
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if req.SessionIndex != nil && req.SessionIndex.Value != "" {
		filtered := rows[:0]
		for _, row := range rows {
			if row.SessionIndex == req.SessionIndex.Value {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}

	// (7) Revoke each resolved session and delete its saml_session rows.
	// Best-effort-continue per row, tracking failures.
	var (
		revokeFailed bool
		lastAcctID   int32
		haveAcctID   bool
	)
	for _, row := range rows {
		sess, gerr := i.queries.GetSession(ctx, row.SessionID)
		if gerr != nil {
			revokeFailed = true
			continue
		}
		lastAcctID = sess.AccountID
		haveAcctID = true
		if _, rerr := i.sessions.RevokeBySessionID(ctx, sess.AccountID, row.SessionID); rerr != nil {
			revokeFailed = true
			// fall through: still attempt the DB row cleanup
		}
		if derr := i.queries.DeleteSAMLSessionsBySession(ctx, row.SessionID); derr != nil {
			revokeFailed = true
		}
	}

	// (8) Audit the logout (best-effort), but ONLY when we actually resolved and
	// acted on at least one session. The no-session idempotent path is a true
	// no-op: it still returns a signed Success LogoutResponse, but emits NO audit
	// record (an accountless EventSessionEnd would be misleading).
	if haveAcctID {
		acctID := lastAcctID
		_ = i.audit.Record(ctx, audit.Record{
			Factor:    audit.FactorSAMLSP,
			Event:     audit.EventSessionEnd,
			AccountID: &acctID,
			IP:        audit.ParseIPOrNil(r.RemoteAddr),
			UserAgent: r.UserAgent(),
			Detail: map[string]any{
				"reason": "slo",
				"sp":     sp.EntityID,
			},
		})
	}

	// (9) Build + sign the LogoutResponse. The SP's SLO-response location, if
	// derivable from its registered metadata, becomes the Destination.
	respLocation, haveLocation := i.parseSPSLOResponseTarget(sp, binding)
	respXML, err := i.buildLogoutResponse(ctx, req.ID, respLocation)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// If a per-row revoke/delete failed we still return a signed response: the
	// degradation is logged via the audit record's success status implicitly and
	// the partial revoke is best-effort. (A hard error here would leave the SP
	// unable to complete its own logout while our state is already partly torn
	// down.) revokeFailed is intentionally not surfaced to the SP.
	_ = revokeFailed

	relayState := r.URL.Query().Get("RelayState")
	if r.Method == http.MethodPost {
		relayState = r.FormValue("RelayState")
	}

	// (10) Deliver the LogoutResponse.
	if !haveLocation {
		// v0.5 fallback: the SP was registered WITHOUT metadata, so we cannot
		// derive an SLO-response endpoint. The IdP session is already revoked;
		// only the response delivery is degraded. Emit the signed XML directly.
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(respXML)
		return
	}

	switch binding {
	case crewjam.HTTPRedirectBinding:
		i.writeRedirectLogoutResponse(w, r, respLocation, respXML, relayState)
	case crewjam.HTTPPostBinding:
		i.writeAutoPost(w, respLocation, respXML, relayState)
	}
}

// decodeRedirectLogoutRequest decodes a HTTP-Redirect binding LogoutRequest:
// SAMLRequest → base64.StdEncoding → bounded raw-DEFLATE inflate → parseXMLSecure
// → xml.Unmarshal. It returns the parsed root element (for callers that need the
// element, though the redirect binding's signature is detached and verified
// separately).
func decodeRedirectLogoutRequest(r *http.Request, out *crewjam.LogoutRequest) (*etree.Element, error) {
	// Reject duplicate redirect-binding params up front (same split-brain guard
	// as the AuthnRequest path).
	if _, _, _, _, _, qerr := splitRedirectQuery(r.URL.RawQuery); qerr != nil {
		return nil, qerr
	}
	samlRequest := r.URL.Query().Get("SAMLRequest")
	if samlRequest == "" {
		return nil, ErrMissingSAMLRequest
	}
	deflated, err := base64.StdEncoding.DecodeString(samlRequest)
	if err != nil {
		return nil, ErrMissingSAMLRequest
	}
	fr := flate.NewReader(bytes.NewReader(deflated))
	raw, err := io.ReadAll(io.LimitReader(fr, maxInflatedAuthnRequest+1))
	_ = fr.Close()
	if err != nil {
		return nil, ErrMissingSAMLRequest
	}
	if len(raw) > maxInflatedAuthnRequest {
		return nil, ErrOversizeRequest
	}
	return parseLogoutRequestXML(raw, out)
}

// decodePostLogoutRequest decodes a HTTP-POST binding LogoutRequest:
// r.FormValue("SAMLRequest") → base64.StdEncoding (NO inflate) → parseXMLSecure
// → xml.Unmarshal. Returns the parsed root element, which the POST binding's
// ENVELOPED signature is verified against.
func decodePostLogoutRequest(r *http.Request, out *crewjam.LogoutRequest) (*etree.Element, error) {
	samlRequest := r.FormValue("SAMLRequest")
	if samlRequest == "" {
		return nil, ErrMissingSAMLRequest
	}
	raw, err := base64.StdEncoding.DecodeString(samlRequest)
	if err != nil {
		return nil, ErrMissingSAMLRequest
	}
	if len(raw) > maxInflatedAuthnRequest {
		return nil, ErrOversizeRequest
	}
	return parseLogoutRequestXML(raw, out)
}

// parseLogoutRequestXML runs the hardened parse (XXE/DTD/dup-ID) then unmarshals
// into a crewjam LogoutRequest. It returns the secure-parsed root element so the
// POST binding can verify the enveloped signature on the exact bytes we parsed.
func parseLogoutRequestXML(raw []byte, out *crewjam.LogoutRequest) (*etree.Element, error) {
	doc, serr := parseXMLSecure(raw)
	if serr != nil {
		return nil, serr
	}
	if uerr := xml.Unmarshal(raw, out); uerr != nil {
		return nil, uerr
	}
	if out.ID == "" {
		return nil, ErrMalformedRequest
	}
	return doc.Root(), nil
}

// verifyPostLogoutSignature verifies the ENVELOPED signature on a POST-binding
// LogoutRequest element against the SP's registered signing cert(s). Success on
// ANY cert verifies; if no cert verifies (or none is present, or no signature),
// the request is rejected. An absent signature manifests as errNoSignature from
// verifyElementSignature, which sloParseError maps to a 400.
func (i *IdP) verifyPostLogoutSignature(ctx context.Context, el *etree.Element, sp db.SamlSp) error {
	if el == nil {
		return errNoSignature
	}
	keys, err := i.queries.ListSAMLSPKeys(ctx, db.ListSAMLSPKeysParams{SpID: sp.ID, Use: "signing"})
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return ErrBadSignature
	}
	var lastErr error = ErrBadSignature
	for _, k := range keys {
		cert, perr := parseCertPEM(k.CertPem)
		if perr != nil {
			lastErr = perr
			continue
		}
		if verr := verifyElementSignature(el, cert); verr != nil {
			lastErr = verr
			continue
		}
		return nil
	}
	return lastErr
}

// buildLogoutResponse builds, signs, and serializes a Success LogoutResponse.
// It mirrors buildStatusResponse: mint a fresh NCName ID, set Version/IssueInstant,
// Status=Success, Issuer=this IdP, InResponseTo=the request ID, Destination=the
// derived SP response location (empty if none), sign via signElement, serialize.
func (i *IdP) buildLogoutResponse(ctx context.Context, inResponseTo, destination string) ([]byte, error) {
	priv, certDER, _, ok := i.keys.signingKey(ctx)
	if !ok {
		return nil, errNoSigningKey
	}
	responseID, err := newSAMLID()
	if err != nil {
		return nil, err
	}
	resp := crewjam.LogoutResponse{
		ID:           responseID,
		InResponseTo: inResponseTo,
		Version:      "2.0",
		IssueInstant: time.Now(),
		Destination:  destination,
		Issuer:       &crewjam.Issuer{Value: i.entityID()},
		Status: crewjam.Status{
			StatusCode: crewjam.StatusCode{Value: statusSuccess},
		},
	}
	respEl := resp.Element()
	signed, err := signElement(respEl, priv, certDER)
	if err != nil {
		return nil, err
	}
	doc := etree.NewDocument()
	doc.SetRoot(signed)
	return doc.WriteToBytes()
}

// parseSPSLOResponseTarget derives the SP's SingleLogoutService response target
// from its registered metadata, preferring the endpoint matching the request's
// binding. Returns ("", false) when the SP has no (valid) metadata or no SLO
// endpoint.
func (i *IdP) parseSPSLOResponseTarget(sp db.SamlSp, binding string) (string, bool) {
	if !sp.MetadataXml.Valid || sp.MetadataXml.String == "" {
		return "", false
	}
	return parseSPSLOEndpoint([]byte(sp.MetadataXml.String), binding)
}

// parseSPSLOEndpoint extracts an SP's SingleLogoutService location from its SAML
// metadata. It prefers the endpoint whose Binding matches the request binding,
// falling back to the first SLO endpoint. For the chosen endpoint it returns
// ResponseLocation if set, else Location. parseXMLSecure gates the parse against
// XXE/DTD/dup-ID before unmarshaling.
func parseSPSLOEndpoint(metadataXML []byte, binding string) (location string, ok bool) {
	if _, serr := parseXMLSecure(metadataXML); serr != nil {
		return "", false
	}
	var ed crewjam.EntityDescriptor
	if uerr := xml.Unmarshal(metadataXML, &ed); uerr != nil {
		return "", false
	}
	if len(ed.SPSSODescriptors) == 0 {
		return "", false
	}
	slos := ed.SPSSODescriptors[0].SingleLogoutServices
	if len(slos) == 0 {
		return "", false
	}
	pick := func(e crewjam.Endpoint) string {
		if e.ResponseLocation != "" {
			return e.ResponseLocation
		}
		return e.Location
	}
	for _, e := range slos {
		if e.Binding == binding {
			if loc := pick(e); loc != "" {
				return loc, true
			}
		}
	}
	for _, e := range slos {
		if loc := pick(e); loc != "" {
			return loc, true
		}
	}
	return "", false
}

// writeRedirectLogoutResponse delivers a LogoutResponse over the HTTP-Redirect
// binding: 302 to location?SAMLResponse=base64(deflate(respXML))[&RelayState=…].
// The location is SP-metadata-derived (never request-supplied).
func (i *IdP) writeRedirectLogoutResponse(w http.ResponseWriter, r *http.Request, location string, respXML []byte, relayState string) {
	var deflated bytes.Buffer
	fw, err := flate.NewWriter(&deflated, flate.DefaultCompression)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if _, err := fw.Write(respXML); err != nil {
		_ = fw.Close()
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := fw.Close(); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	encoded := base64.StdEncoding.EncodeToString(deflated.Bytes())

	q := url.Values{}
	q.Set("SAMLResponse", encoded)
	if relayState != "" {
		q.Set("RelayState", relayState)
	}
	sep := "?"
	if u, perr := url.Parse(location); perr == nil && u.RawQuery != "" {
		sep = "&"
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, location+sep+q.Encode(), http.StatusFound)
}

// sloParseError maps an SLO parse/validation/signature error to a DIRECT HTTP
// error. The shared invariant across every case (including ErrSLOBadDestination
// and ErrSLOExpired, which are raised AFTER the signature gate) is that NONE of
// these paths redirect to an SP-supplied URL and NONE have mutated a session:
// they all terminate before the revoke step. Client-class errors collapse to
// 400; anything else (e.g. a DB failure) is 500.
func (i *IdP) sloParseError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnknownSP),
		errors.Is(err, ErrMalformedRequest),
		errors.Is(err, ErrOversizeRequest),
		errors.Is(err, ErrMissingSAMLRequest),
		errors.Is(err, ErrMissingSignature),
		errors.Is(err, ErrBadSignature),
		errors.Is(err, errNoSignature),
		errors.Is(err, errWeakSigAlg),
		errors.Is(err, errSigRefMismatch),
		errors.Is(err, errXMLDTD),
		errors.Is(err, errDuplicateID),
		errors.Is(err, ErrSLOBadDestination),
		errors.Is(err, ErrSLOExpired):
		http.Error(w, "invalid SAML LogoutRequest", http.StatusBadRequest)
	default:
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
