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
	"strings"
	"time"

	crewjam "github.com/crewjam/saml"
	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
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
}

// parseAuthnRequest decodes and fully validates an inbound SP-initiated
// AuthnRequest carried over the HTTP-Redirect binding. It is security-critical:
// it verifies the SP's detached signature (when the SP requires it), pins the
// Destination to this IdP, and — most importantly — resolves the
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
	// --- 0. Reject duplicate redirect-binding params ----------------------
	// url.Query().Get returns the FIRST occurrence while splitRedirectQuery
	// (signature path) keeps the LAST, so a duplicated SAMLRequest/RelayState/
	// SigAlg/Signature could split-brain the validated XML and the signed octet
	// string. Reject up front, regardless of whether the SP requires signing.
	if _, _, _, _, _, qerr := splitRedirectQuery(r.URL.RawQuery); qerr != nil {
		return nil, qerr
	}

	// --- 1. HTTP-Redirect decode of SAMLRequest ---------------------------
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

	// Security gate: reject DTDs/entities/dup-IDs before structural parsing.
	if _, serr := parseXMLSecure(raw); serr != nil {
		return nil, serr
	}

	var req crewjam.AuthnRequest
	if uerr := xml.Unmarshal(raw, &req); uerr != nil {
		return nil, uerr
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

	// --- 3. SP detached-signature verification (when required) ------------
	if sp.RequireSignedAuthnRequest {
		if err := i.verifyRedirectSignature(ctx, r, sp); err != nil {
			return nil, err
		}
	}

	// SAML requires AuthnRequest/@ID. An empty ID would degenerate the replay
	// key to "saml:authnreq:" (collapsing all such requests into one slot) and
	// leave InResponseTo empty downstream — reject it as malformed.
	if req.ID == "" {
		return nil, ErrMalformedRequest
	}

	// --- 4. Destination check ---------------------------------------------
	// Destination is optional on the wire, but if present it MUST name this
	// IdP's SSO endpoint (anti-misrouting / anti-replay-against-other-IdP).
	if req.Destination != "" && req.Destination != i.ssoURL() {
		return nil, ErrBadDestination
	}

	// --- 5. ACS resolution (open-redirect guard) --------------------------
	acsURL, err := i.resolveACS(ctx, sp.ID, req.AssertionConsumerServiceURL)
	if err != nil {
		return nil, err
	}

	return &authnReq{
		SP:         sp,
		RequestID:  req.ID,
		ACSURL:     acsURL,
		RelayState: r.URL.Query().Get("RelayState"),
		IsPassive:  derefBool(req.IsPassive),
		ForceAuthn: derefBool(req.ForceAuthn),
	}, nil
}

// consumeAuthnRequestID enforces single-use replay protection for an
// AuthnRequest ID. It is called from the terminal/issue path (HandleSSO), NOT
// from parseAuthnRequest, so a login bounce that re-parses the same SAMLRequest
// does not trip replay (see parseAuthnRequest's doc comment).
//
// PLACEMENT: this deliberately lives here in authnreq.go — next to the replay
// sentinel (ErrReplayedRequest) and TTL (AuthnRequestTTL) it depends on — even
// though its sole caller is HandleSSO in sso.go. It is intentionally NOT folded
// into parseAuthnRequest: keeping the KV write out of parseAuthnRequest is what
// lets parseAuthnRequest stay a pure (side-effect-free) reader that the login
// bounce can safely re-run. Do not move or inline it.
//
// The first call for an id stores a marker (TTL AuthnRequestTTL) and returns
// nil; any subsequent call within the TTL returns ErrReplayedRequest.
//
// NOTE: the Get→SetEx sequence is NOT atomic across KV ops (mirrors the OIDC
// refresh-token pattern). Two concurrent presentations of the same fresh
// AuthnRequest ID could both miss the Get and proceed. This is an accepted
// limitation: a fully atomic check-and-set would need a KV primitive the Store
// interface does not expose, and an AuthnRequest's blast radius is one login
// that still requires a live IdP session.
func (i *IdP) consumeAuthnRequestID(ctx context.Context, id string) error {
	replayKey := "saml:authnreq:" + id
	if _, gerr := i.kv.Get(ctx, replayKey); gerr == nil {
		return ErrReplayedRequest
	} else if !errors.Is(gerr, kv.ErrKeyNotFound) {
		return gerr
	}
	return i.kv.SetEx(ctx, replayKey, "1", AuthnRequestTTL)
}

// resolveACS maps the SP-requested AssertionConsumerService URL to a registered
// endpoint, or selects the SP's default. It is the open-redirect guard: it
// NEVER returns a URL that is not a registered ACS Location for this SP, so the
// IdP cannot be tricked into delivering a signed assertion to an attacker URL.
//
//   - requested != "": MUST exact-string-match one registered ACS Location.
//   - requested == "": use the SP's IsDefault ACS Location.
//
// Any failure to satisfy the above yields ErrInvalidACS.
func (i *IdP) resolveACS(ctx context.Context, spID int64, requested string) (string, error) {
	endpoints, err := i.queries.ListSAMLSPACSEndpoints(ctx, spID)
	if err != nil {
		return "", err
	}
	if requested != "" {
		for _, e := range endpoints {
			if e.Location == requested {
				return e.Location, nil
			}
		}
		return "", ErrInvalidACS
	}
	for _, e := range endpoints {
		if e.IsDefault {
			return e.Location, nil
		}
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
