package saml

import (
	"bytes"
	"compress/flate"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/beevik/etree"
	crewjam "github.com/crewjam/saml"
	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/db"
)

// AuthnRequestTTL bounds how long a parsed AuthnRequest's ID is remembered for
// single-use replay detection. SP-initiated requests are consumed within
// seconds of issuance; five minutes generously covers clock skew and slow
// redirects while keeping the replay window short.
const AuthnRequestTTL = 5 * time.Minute

// rsaSHA256SigAlg is the only SP-signature algorithm we accept on the
// HTTP-Redirect binding. SHA-1 (rsa-sha1) is rejected as weak (see xmlsec.go's
// isSHA1Algorithm / errWeakSigAlg).
const rsaSHA256SigAlg = "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256"

// maxInflatedAuthnRequest bounds the DEFLATE-inflated size of an inbound
// SAMLRequest. The HTTP-Redirect binding is an unauthenticated, public route:
// an SP-controlled (or spoofed) SAMLRequest can be crafted to decompress to
// hundreds of MB (a "decompression bomb" DoS) before any SP lookup or
// signature check runs. We cap the inflate at 10 MB — matching crewjam/saml's
// own DEFLATE limit — and reject anything larger.
const maxInflatedAuthnRequest = 10 * 1024 * 1024 // 10 MB

var (
	// ErrReplayedRequest is returned when an AuthnRequest with an ID we have
	// already seen (within AuthnRequestTTL) is presented again.
	ErrReplayedRequest = errors.New("saml: AuthnRequest ID replayed")
	// ErrBadDestination is returned when the request's Destination is set and
	// does not name this IdP's SSO endpoint.
	ErrBadDestination = errors.New("saml: AuthnRequest Destination does not match this IdP")
	// ErrBadSignature is returned when a required SP signature is present but
	// does not verify against any of the SP's registered signing certs.
	ErrBadSignature = errors.New("saml: SP signature verification failed")
	// ErrMissingSAMLRequest is returned when the SAMLRequest query param is
	// absent or undecodable.
	ErrMissingSAMLRequest = errors.New("saml: SAMLRequest missing or malformed")
	// ErrOversizeRequest is returned when the DEFLATE-inflated SAMLRequest
	// exceeds maxInflatedAuthnRequest (decompression-bomb guard).
	ErrOversizeRequest = errors.New("saml: SAMLRequest exceeds maximum inflated size")
	// ErrMalformedRequest is returned for structurally invalid redirect-binding
	// requests: duplicate redirect-binding params, or an AuthnRequest missing
	// the required @ID.
	ErrMalformedRequest = errors.New("saml: AuthnRequest malformed")
)

// authnReq is the validated, IdP-side view of an inbound SP AuthnRequest. Every
// field has already passed signature/replay/Destination/ACS validation by the
// time parseAuthnRequest returns it; downstream code (the /saml/sso handler)
// trusts these values without re-checking.
type authnReq struct {
	SP         db.SamlSp
	RequestID  string
	ACSURL     string
	RelayState string
	IsPassive  bool
	ForceAuthn bool
	// NameIDFormat is the requested NameIDPolicy/@Format (empty if the request
	// carried no NameIDPolicy or an empty Format). HandleSSO honors a concrete
	// requested format only if it matches what this SP is configured to produce;
	// see the D8 NameIDPolicy check there.
	NameIDFormat string
}

// parseAuthnRequest decodes and fully validates an inbound SP-initiated
// AuthnRequest. It accepts BOTH the HTTP-Redirect binding (GET; detached
// signature) and the HTTP-POST binding (POST; enveloped signature), dispatching
// on r.Method exactly as HandleSLO does for the LogoutRequest. It is
// security-critical: it verifies the SP's signature (when the SP requires it),
// pins the Destination to this IdP, and — most importantly — resolves the
// AssertionConsumerService URL ONLY to a registered endpoint so the IdP can
// never be coerced into POSTing a signed assertion to an attacker-chosen URL
// (open-redirect / assertion-exfiltration guard).
//
// It is PURE parse+validate: it never writes to KV. Single-use replay
// protection is DEFERRED to consumeAuthnRequestID, called only on the terminal
// issue path. This is deliberate: the spec's SP-initiated bounce 302s an
// unauthenticated user to /login?return_to=<full SSO URL>, and the browser
// returns to /saml/sso with the SAME SAMLRequest — re-running parseAuthnRequest.
// If parsing consumed the replay key, that legitimate return trip would trip
// replay. Consuming only at issue time keeps the bounce working while still
// guaranteeing a given AuthnRequest ID yields at most one Response.
func (i *IdP) parseAuthnRequest(ctx context.Context, r *http.Request) (*authnReq, error) {
	// --- 1. Decode the AuthnRequest from the binding implied by the method.
	// GET → HTTP-Redirect (DEFLATE, detached sig); POST → HTTP-POST (no inflate,
	// enveloped sig). reqEl is the secure-parsed root element, used only by the
	// POST binding's enveloped-signature verification.
	var (
		req     crewjam.AuthnRequest
		reqEl   *etree.Element
		binding string
	)
	switch r.Method {
	case http.MethodGet:
		binding = crewjam.HTTPRedirectBinding
		el, err := decodeRedirectAuthnRequest(r, &req)
		if err != nil {
			return nil, err
		}
		reqEl = el
	case http.MethodPost:
		binding = crewjam.HTTPPostBinding
		el, err := decodePostAuthnRequest(r, &req)
		if err != nil {
			return nil, err
		}
		reqEl = el
	default:
		return nil, ErrMissingSAMLRequest
	}

	if req.Issuer == nil || req.Issuer.Value == "" {
		return nil, ErrUnknownSP
	}

	// --- 2. SP lookup -----------------------------------------------------
	sp, err := i.queries.GetSAMLSPByEntityID(ctx, req.Issuer.Value)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnknownSP
		}
		return nil, err
	}

	// --- 3. SP signature verification (when required) ---------------------
	// The verification differs by binding: HTTP-Redirect uses a DETACHED
	// signature over the query octet string (verifyRedirectSignature), while
	// HTTP-POST uses an ENVELOPED signature on the AuthnRequest element
	// (verifyPostAuthnSignature) — mirroring the SLO LogoutRequest paths.
	if sp.RequireSignedAuthnRequest {
		switch binding {
		case crewjam.HTTPRedirectBinding:
			if verr := i.verifyRedirectSignature(ctx, r, sp); verr != nil {
				return nil, verr
			}
		case crewjam.HTTPPostBinding:
			if verr := i.verifyPostAuthnSignature(ctx, reqEl, sp); verr != nil {
				return nil, verr
			}
		}
	}

	// --- 4. Destination check ---------------------------------------------
	// Destination is optional on the wire, but if present it MUST name this
	// IdP's SSO endpoint (anti-misrouting / anti-replay-against-other-IdP).
	if req.Destination != "" && req.Destination != i.ssoURL() {
		return nil, ErrBadDestination
	}

	// --- 5. ACS resolution (open-redirect guard) --------------------------
	// An SP may identify its ACS by URL OR by AssertionConsumerServiceIndex
	// (Web Browser SSO Profile §4.1.4.1). resolveACS resolves both and applies
	// the lowest-index implicit default when neither is supplied.
	acsURL, err := i.resolveACS(ctx, sp.ID, req.AssertionConsumerServiceURL, req.AssertionConsumerServiceIndex)
	if err != nil {
		return nil, err
	}

	// NameIDPolicy/@Format is optional; crewjam models Format as *string. Capture
	// the requested format (empty when NameIDPolicy is absent or carries no
	// Format) so HandleSSO can enforce the D8 producible-format rule without
	// re-touching the crewjam request.
	var nameIDFormat string
	if req.NameIDPolicy != nil && req.NameIDPolicy.Format != nil {
		nameIDFormat = *req.NameIDPolicy.Format
	}

	// RelayState travels alongside the SAMLRequest in the same transport: the
	// query string for the HTTP-Redirect binding, the POST form for HTTP-POST
	// (mirroring HandleSLO's relayState handling).
	relayState := r.URL.Query().Get("RelayState")
	if binding == crewjam.HTTPPostBinding {
		relayState = r.FormValue("RelayState")
	}

	return &authnReq{
		SP:           sp,
		RequestID:    req.ID,
		ACSURL:       acsURL,
		RelayState:   relayState,
		IsPassive:    derefBool(req.IsPassive),
		ForceAuthn:   derefBool(req.ForceAuthn),
		NameIDFormat: nameIDFormat,
	}, nil
}

// decodeRedirectAuthnRequest decodes a HTTP-Redirect binding AuthnRequest:
// SAMLRequest → base64.StdEncoding → bounded raw-DEFLATE inflate → parseXMLSecure
// → xml.Unmarshal. It mirrors decodeRedirectLogoutRequest in slo.go (same
// duplicate-param guard, same decompression-bomb cap) and returns the parsed
// root element. The redirect binding's signature is DETACHED (over the query
// octet string) and verified separately by verifyRedirectSignature, so the
// returned element is unused on this path but kept for symmetry with the POST
// path.
func decodeRedirectAuthnRequest(r *http.Request, out *crewjam.AuthnRequest) (*etree.Element, error) {
	// Reject duplicate redirect-binding params up front: url.Query().Get returns
	// the FIRST occurrence while splitRedirectQuery (signature path) keeps the
	// LAST, so a duplicated SAMLRequest/RelayState/SigAlg/Signature could
	// split-brain the validated XML and the signed octet string.
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
	// HTTP-Redirect uses raw DEFLATE (RFC 1951), NOT zlib — flate.NewReader.
	// Bound the inflate: this is an unauthenticated public route, so an attacker
	// could otherwise present a small SAMLRequest that decompresses to hundreds
	// of MB (decompression-bomb DoS) before any SP lookup / signature check. We
	// read at most maxInflatedAuthnRequest+1 bytes and reject anything over the
	// cap.
	fr := flate.NewReader(bytes.NewReader(deflated))
	raw, err := io.ReadAll(io.LimitReader(fr, maxInflatedAuthnRequest+1))
	_ = fr.Close()
	if err != nil {
		return nil, ErrMissingSAMLRequest
	}
	if len(raw) > maxInflatedAuthnRequest {
		return nil, ErrOversizeRequest
	}
	return parseAuthnRequestXML(raw, out)
}

// decodePostAuthnRequest decodes a HTTP-POST binding AuthnRequest:
// r.FormValue("SAMLRequest") → base64.StdEncoding (NO inflate) → parseXMLSecure
// → xml.Unmarshal. It mirrors decodePostLogoutRequest in slo.go. Returns the
// secure-parsed root element, which the POST binding's ENVELOPED signature is
// verified against.
func decodePostAuthnRequest(r *http.Request, out *crewjam.AuthnRequest) (*etree.Element, error) {
	samlRequest := r.FormValue("SAMLRequest")
	if samlRequest == "" {
		return nil, ErrMissingSAMLRequest
	}
	raw, err := base64.StdEncoding.DecodeString(samlRequest)
	if err != nil {
		return nil, ErrMissingSAMLRequest
	}
	// POST has no DEFLATE layer, but cap the decoded size sanely all the same
	// (same bound as the inflate path in slo.go's decodePostLogoutRequest).
	if len(raw) > maxInflatedAuthnRequest {
		return nil, ErrOversizeRequest
	}
	return parseAuthnRequestXML(raw, out)
}

// parseAuthnRequestXML runs the hardened parse (XXE/DTD/dup-ID) then unmarshals
// into a crewjam AuthnRequest, enforcing the required @ID and Version="2.0"
// (Core §3.2.1). It returns the secure-parsed root element so the POST binding
// can verify the enveloped signature on the exact bytes we parsed. Mirrors
// parseLogoutRequestXML in slo.go.
func parseAuthnRequestXML(raw []byte, out *crewjam.AuthnRequest) (*etree.Element, error) {
	doc, serr := parseXMLSecure(raw)
	if serr != nil {
		return nil, serr
	}
	if uerr := xml.Unmarshal(raw, out); uerr != nil {
		return nil, uerr
	}
	// SAML requires AuthnRequest/@ID. An empty ID would degenerate the replay
	// key to "saml:authn_request_replay:{spEntityID}:" (collapsing all such
	// requests for that SP into one slot) and leave InResponseTo empty
	// downstream — reject it as malformed.
	if out.ID == "" {
		return nil, ErrMalformedRequest
	}
	// SAML Core §3.2.1: every request MUST carry Version="2.0".
	if out.Version != "2.0" {
		return nil, ErrMalformedRequest
	}
	return doc.Root(), nil
}

// verifyPostAuthnSignature verifies the ENVELOPED signature on a POST-binding
// AuthnRequest element against the SP's registered signing cert(s). It mirrors
// verifyPostLogoutSignature in slo.go exactly: success on ANY cert verifies; an
// absent signature surfaces as errNoSignature (which the SSO handler maps to a
// rejection), and a present-but-bad signature surfaces as ErrBadSignature.
func (i *IdP) verifyPostAuthnSignature(ctx context.Context, el *etree.Element, sp db.SamlSp) error {
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

// consumeAuthnRequestID enforces single-use replay protection for an
// AuthnRequest ID, scoped by the SP entity ID. It is called from the
// terminal/issue path (HandleSSO), NOT from parseAuthnRequest, so a login
// bounce that re-parses the same SAMLRequest does not trip replay (see
// parseAuthnRequest's doc comment).
//
// PLACEMENT: this deliberately lives here in authnreq.go — next to the replay
// sentinel (ErrReplayedRequest) and TTL (AuthnRequestTTL) it depends on — even
// though its sole caller is HandleSSO in sso.go. It is intentionally NOT folded
// into parseAuthnRequest: keeping the KV write out of parseAuthnRequest is what
// lets parseAuthnRequest stay a pure (side-effect-free) reader that the login
// bounce can safely re-run. Do not move or inline it.
//
// The check is atomic via SetNX: the first call for a (spEntityID, id) pair
// sets the replay marker (TTL AuthnRequestTTL) and returns nil; any subsequent
// call within the TTL returns ErrReplayedRequest. Concurrent first-presentations
// of the same ID are serialised by SetNX — exactly one succeeds. A KV error
// causes an immediate rejection (fail closed): the request is never allowed
// through on a storage failure.
//
// The key is scoped by SP entity ID: the same request ID issued by two
// different SPs is treated as two independent requests.
func (i *IdP) consumeAuthnRequestID(ctx context.Context, spEntityID, id string) error {
	replayKey := "saml:authn_request_replay:" + spEntityID + ":" + id
	ok, err := i.kv.SetNX(ctx, replayKey, "1", AuthnRequestTTL)
	if err != nil {
		return err // fail closed: a KV error must not allow the request through
	}
	if !ok {
		return ErrReplayedRequest
	}
	return nil
}

// resolveACS maps the SP's requested AssertionConsumerService — identified by
// URL or by AssertionConsumerServiceIndex — to a registered endpoint, or
// selects the SP's default. It is the open-redirect guard: it NEVER returns a
// URL that is not a registered ACS Location for this SP, so the IdP cannot be
// tricked into delivering a signed assertion to an attacker URL.
//
// Resolution precedence (Web Browser SSO Profile §4.1.4.1, Core §3.4.1,
// Metadata §2.4.4.1 / IndexedEndpointType §2.2.3):
//
//  1. requestedURL != "": MUST exact-string-match one registered ACS Location;
//     no match → ErrInvalidACS. (open-redirect guard — never echo an
//     unregistered URL.)
//  2. requestedIndex non-empty: MUST match a registered ACS whose Idx equals
//     it → return that endpoint's Location; no such index → ErrInvalidACS.
//  3. neither: return the IsDefault ACS; if NONE is marked default, return the
//     ACS with the LOWEST Idx (the spec's implicit default). Zero ACS rows →
//     ErrInvalidACS.
//
// requestedIndex is the raw AssertionConsumerServiceIndex string from the
// AuthnRequest (empty = unset); a non-empty value that does not parse to an int
// is treated as a non-matching index → ErrInvalidACS.
func (i *IdP) resolveACS(ctx context.Context, spID int64, requestedURL, requestedIndex string) (string, error) {
	endpoints, err := i.queries.ListSAMLSPACSEndpoints(ctx, spID)
	if err != nil {
		return "", err
	}

	// 1. ACS-by-URL: exact match against a registered Location.
	if requestedURL != "" {
		for _, e := range endpoints {
			if e.Location == requestedURL {
				return e.Location, nil
			}
		}
		return "", ErrInvalidACS
	}

	// 2. ACS-by-index: match the registered endpoint whose Idx equals the
	// requested index. A malformed (non-integer) index never matches.
	if requestedIndex != "" {
		idx, perr := strconv.Atoi(requestedIndex)
		if perr != nil {
			return "", ErrInvalidACS
		}
		for _, e := range endpoints {
			if int(e.Idx) == idx {
				return e.Location, nil
			}
		}
		return "", ErrInvalidACS
	}

	// 3. Neither supplied: the explicit IsDefault endpoint wins; otherwise the
	// spec's implicit default is the endpoint with the LOWEST Idx.
	var (
		lowest  *db.SamlSpAc
		haveAny bool
	)
	for idx := range endpoints {
		e := &endpoints[idx]
		if e.IsDefault {
			return e.Location, nil
		}
		if !haveAny || e.Idx < lowest.Idx {
			lowest = e
			haveAny = true
		}
	}
	if haveAny {
		return lowest.Location, nil
	}
	return "", ErrInvalidACS
}

// verifyRedirectSignature verifies the SAML 2.0 HTTP-Redirect binding's
// detached signature (Bindings §3.4.4.1). The SP signs the octet string formed
// by the URL-encoded query values in the FIXED order:
//
//	SAMLRequest=<v>&RelayState=<v>&SigAlg=<v>
//
// (RelayState omitted iff the SP did not send it), where each <v> is the EXACT
// percent-encoded bytes the SP put on the wire. We therefore reconstruct the
// signed string from r.URL.RawQuery (NOT r.URL.Query(), which percent-decodes
// and would let us re-encode to bytes that differ from the SP's). The Signature
// param itself is base64(raw RSA signature) and is not part of the signed
// string.
func (i *IdP) verifyRedirectSignature(ctx context.Context, r *http.Request, sp db.SamlSp) error {
	rawSAMLRequest, rawRelayState, rawSigAlg, hasRelayState, hasSigAlg, qerr := splitRedirectQuery(r.URL.RawQuery)
	if qerr != nil {
		return qerr
	}

	// Signature presence is checked from the decoded query — its value is not
	// part of the signed string.
	sigB64 := r.URL.Query().Get("Signature")
	if sigB64 == "" {
		return ErrMissingSignature
	}
	if !hasSigAlg {
		return ErrMissingSignature
	}

	// Only RSA-SHA256 is accepted. SigAlg arrives percent-encoded in the raw
	// query; decode it for comparison.
	sigAlg := r.URL.Query().Get("SigAlg")
	if isSHA1Algorithm(sigAlg) {
		return errWeakSigAlg
	}
	if sigAlg != rsaSHA256SigAlg {
		return errWeakSigAlg
	}

	sigBytes, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return ErrBadSignature
	}

	// Reconstruct the signed octet string from the RAW (still-encoded) values.
	var b strings.Builder
	b.WriteString("SAMLRequest=")
	b.WriteString(rawSAMLRequest)
	if hasRelayState {
		b.WriteString("&RelayState=")
		b.WriteString(rawRelayState)
	}
	b.WriteString("&SigAlg=")
	b.WriteString(rawSigAlg)
	signed := b.String()

	h := sha256.Sum256([]byte(signed))

	// Pull the SP's signing certs (a key-rotation set); success on ANY verifies.
	keys, err := i.queries.ListSAMLSPKeys(ctx, db.ListSAMLSPKeysParams{SpID: sp.ID, Use: "signing"})
	if err != nil {
		return err
	}
	now := time.Now()
	for _, k := range keys {
		cert, perr := parseCertPEM(k.CertPem)
		if perr != nil {
			continue
		}
		// Crypto consistency with the POST binding (goxmldsig rejects expired
		// certs): skip a cert whose validity window does not include now, but
		// keep scanning so a rotation set with at least one live cert still
		// verifies. If none is valid, the loop falls through to ErrBadSignature.
		if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
			continue
		}
		pub, ok := cert.PublicKey.(*rsa.PublicKey)
		if !ok {
			continue
		}
		if rsa.VerifyPKCS1v15(pub, crypto.SHA256, h[:], sigBytes) == nil {
			return nil
		}
	}
	return ErrBadSignature
}

// splitRedirectQuery parses a raw URL query string (NOT percent-decoded) and
// returns the raw right-hand sides of SAMLRequest, RelayState, and SigAlg, plus
// whether RelayState and SigAlg were present. We must keep the values in their
// exact on-the-wire encoding to reproduce the SP's signed octet string.
//
// A query that repeats any of the redirect-binding params (SAMLRequest,
// RelayState, SigAlg, Signature) is REJECTED as ErrMalformedRequest. Picking
// first-or-last would otherwise create a split-brain: parseAuthnRequest reads
// SAMLRequest via url.Query().Get (first occurrence) while this function keeps
// the last, so a duplicated param could make the validated XML diverge from the
// signature-checked octet string. Rejecting is the only safe resolution.
func splitRedirectQuery(rawQuery string) (samlRequest, relayState, sigAlg string, hasRelayState, hasSigAlg bool, err error) {
	var seenReq, seenSig bool
	for pair := range strings.SplitSeq(rawQuery, "&") {
		if pair == "" {
			continue
		}
		key, val, _ := strings.Cut(pair, "=")
		switch key {
		case "SAMLRequest":
			if seenReq {
				return "", "", "", false, false, ErrMalformedRequest
			}
			seenReq = true
			samlRequest = val
		case "RelayState":
			if hasRelayState {
				return "", "", "", false, false, ErrMalformedRequest
			}
			relayState = val
			hasRelayState = true
		case "SigAlg":
			if hasSigAlg {
				return "", "", "", false, false, ErrMalformedRequest
			}
			sigAlg = val
			hasSigAlg = true
		case "Signature":
			if seenSig {
				return "", "", "", false, false, ErrMalformedRequest
			}
			seenSig = true
		}
	}
	return samlRequest, relayState, sigAlg, hasRelayState, hasSigAlg, nil
}

// parseCertPEM decodes a single PEM-encoded X.509 certificate.
func parseCertPEM(pemStr string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("saml: SP cert PEM did not decode")
	}
	return x509.ParseCertificate(block.Bytes)
}

// derefBool safely dereferences an optional XML boolean attribute (nil → false).
func derefBool(b *bool) bool {
	return b != nil && *b
}
