// saml_mock.go — in-process mock SAML SP for the SAML smoke steps.
//
// The mock SP owns an RSA key + self-signed cert, generates its own
// EntityDescriptor metadata (for `saml-sp create --metadata-file`), builds
// SIGNED HTTP-Redirect-binding AuthnRequests and LogoutRequests (detached
// signature over the EXACT url.QueryEscape'd octet string the IdP reconstructs),
// and verifies the auto-POSTed SAMLResponse via crewjam ServiceProvider.
//
// This is its own translation of the wire behavior the IdP enforces: cmd/smoke
// is a separate package and cannot reach the saml package's unexported helpers,
// so the deflate/base64/sign/verify code here mirrors authnreq.go +
// authnreq_test.go byte-for-byte (confirmed against the vendored crewjam source
// and the passing Task 7 interop test).
package main

import (
	"bytes"
	"compress/flate"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/beevik/etree"
	crewjam "github.com/crewjam/saml"
	dsig "github.com/russellhaering/goxmldsig"
)

// rsaSHA256SigAlgURI is the only redirect-binding signature algorithm the IdP
// accepts (SHA-1 is rejected). Mirrors saml.rsaSHA256SigAlg.
const rsaSHA256SigAlgURI = "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256"

// persistentNameIDFormat11 mirrors saml.persistentNameIDFormat11 — the default
// NameID format the IdP issues and the format the mock SP stamps on its
// LogoutRequest NameID.
const persistentNameIDFormat11 = "urn:oasis:names:tc:SAML:1.1:nameid-format:persistent"

// mockSP is an in-process SAML service provider used by the SAML smoke steps.
type mockSP struct {
	entityID string
	acsURL   string
	sloURL   string
	key      *rsa.PrivateKey
	certDER  []byte
}

// newMockSP generates a fresh RSA-2048 key + self-signed cert for the SP. The
// SLO endpoint is derived from the ACS host so the IdP can deliver a
// redirect-binding LogoutResponse back to a real SP location.
func newMockSP(entityID, acsURL string) (*mockSP, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("mockSP: generate key: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "mock-sp.smoke.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("mockSP: create cert: %w", err)
	}
	return &mockSP{
		entityID: entityID,
		acsURL:   acsURL,
		sloURL:   "https://mock-sp.smoke.test/saml/slo",
		key:      key,
		certDER:  der,
	}, nil
}

// metadataXML builds the SP's SAML EntityDescriptor: one POST-binding ACS
// (index 0, isDefault) and one signing KeyDescriptor carrying the SP's cert.
// The IdP's parseSPMetadata ingests exactly these fields.
func (m *mockSP) metadataXML() ([]byte, error) {
	isDefault := true
	ed := crewjam.EntityDescriptor{
		EntityID: m.entityID,
		SPSSODescriptors: []crewjam.SPSSODescriptor{
			{
				SSODescriptor: crewjam.SSODescriptor{
					RoleDescriptor: crewjam.RoleDescriptor{
						ProtocolSupportEnumeration: "urn:oasis:names:tc:SAML:2.0:protocol",
						KeyDescriptors: []crewjam.KeyDescriptor{
							{
								Use: "signing",
								KeyInfo: crewjam.KeyInfo{
									X509Data: crewjam.X509Data{
										X509Certificates: []crewjam.X509Certificate{
											{Data: base64.StdEncoding.EncodeToString(m.certDER)},
										},
									},
								},
							},
						},
					},
					SingleLogoutServices: []crewjam.Endpoint{
						{Binding: crewjam.HTTPRedirectBinding, Location: m.sloURL},
					},
				},
				AssertionConsumerServices: []crewjam.IndexedEndpoint{
					{
						Binding:   crewjam.HTTPPostBinding,
						Location:  m.acsURL,
						Index:     0,
						IsDefault: &isDefault,
					},
				},
			},
		},
	}
	body, err := xml.MarshalIndent(ed, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), body...), nil
}

// authnRequestRedirect builds a SIGNED HTTP-Redirect-binding AuthnRequest and
// returns the query string (everything after "?") to append to the IdP's
// /saml/sso URL. The detached signature covers the octet string
//
//	SAMLRequest=<esc>&SigAlg=<esc>
//
// (RelayState would slot between them if sent). The Signature param itself is
// NOT part of the signed string. Signing reproduces saml.verifyRedirectSignature
// byte-for-byte by url.QueryEscape'ing the SAME values that go on the wire.
//
// destination is the IdP /saml/sso URL (pinned by the IdP's Destination check);
// pass "" to omit Destination. Returns the request ID for replay/InResponseTo
// assertions.
// authnOpts carries the optional AuthnRequest knobs (ForceAuthn, IsPassive,
// NameIDPolicy/@Format). The zero value reproduces the default request.
type authnOpts struct {
	forceAuthn   bool
	isPassive    bool
	nameIDFormat string // when non-empty, sets NameIDPolicy/@Format
}

// buildAuthnRequest constructs the crewjam AuthnRequest with the optional knobs
// applied. Shared by the redirect- and POST-binding builders.
func (m *mockSP) buildAuthnRequest(requestID, destination, acsURL string, opts authnOpts) crewjam.AuthnRequest {
	ar := crewjam.AuthnRequest{
		ID:           requestID,
		Version:      "2.0",
		IssueInstant: time.Now().UTC(),
		Destination:  destination,
		Issuer:       &crewjam.Issuer{Value: m.entityID},
	}
	if acsURL != "" {
		ar.AssertionConsumerServiceURL = acsURL
	}
	if opts.forceAuthn {
		v := true
		ar.ForceAuthn = &v
	}
	if opts.isPassive {
		v := true
		ar.IsPassive = &v
	}
	if opts.nameIDFormat != "" {
		f := opts.nameIDFormat
		ar.NameIDPolicy = &crewjam.NameIDPolicy{Format: &f}
	}
	return ar
}

func (m *mockSP) authnRequestRedirect(destination, acsURL string, sign bool) (query, requestID string, err error) {
	return m.authnRequestRedirectOpts(destination, acsURL, sign, authnOpts{})
}

// authnRequestRedirectOpts is authnRequestRedirect with the optional knobs.
func (m *mockSP) authnRequestRedirectOpts(destination, acsURL string, sign bool, opts authnOpts) (query, requestID string, err error) {
	requestID, err = newMockSAMLID()
	if err != nil {
		return "", "", err
	}
	ar := m.buildAuthnRequest(requestID, destination, acsURL, opts)
	xmlBytes, err := xml.Marshal(ar)
	if err != nil {
		return "", "", fmt.Errorf("marshal authnrequest: %w", err)
	}
	samlRequest, err := deflateBase64(xmlBytes)
	if err != nil {
		return "", "", err
	}
	encReq := url.QueryEscape(samlRequest)
	rawQuery := "SAMLRequest=" + encReq
	if sign {
		encSigAlg := url.QueryEscape(rsaSHA256SigAlgURI)
		signed := "SAMLRequest=" + encReq + "&SigAlg=" + encSigAlg
		sum := sha256.Sum256([]byte(signed))
		sigBytes, serr := rsa.SignPKCS1v15(rand.Reader, m.key, crypto.SHA256, sum[:])
		if serr != nil {
			return "", "", fmt.Errorf("sign authnrequest: %w", serr)
		}
		rawQuery += "&SigAlg=" + encSigAlg
		rawQuery += "&Signature=" + url.QueryEscape(base64.StdEncoding.EncodeToString(sigBytes))
	}
	return rawQuery, requestID, nil
}

// logoutRequestRedirect builds a SIGNED HTTP-Redirect-binding LogoutRequest for
// the given stable NameID (and optional SessionIndex), returning the query
// string to append to the IdP's /saml/slo URL. crewjam's LogoutRequest.MarshalXML
// panics on a nil *RelaxedTime NotOnOrAfter, so we serialize via Element()+etree
// (the slo_test.go approach).
func (m *mockSP) logoutRequestRedirect(destination, nameID, sessionIndex string) (query, requestID string, err error) {
	requestID, err = newMockSAMLID()
	if err != nil {
		return "", "", err
	}
	lr := crewjam.LogoutRequest{
		ID:           requestID,
		Version:      "2.0",
		IssueInstant: time.Now().UTC(),
		Destination:  destination,
		Issuer:       &crewjam.Issuer{Value: m.entityID},
		NameID:       &crewjam.NameID{Value: nameID, Format: persistentNameIDFormat11},
	}
	if sessionIndex != "" {
		lr.SessionIndex = &crewjam.SessionIndex{Value: sessionIndex}
	}
	doc := etree.NewDocument()
	doc.SetRoot(lr.Element())
	xmlBytes, err := doc.WriteToBytes()
	if err != nil {
		return "", "", fmt.Errorf("serialize logoutrequest: %w", err)
	}
	samlRequest, err := deflateBase64(xmlBytes)
	if err != nil {
		return "", "", err
	}
	encReq := url.QueryEscape(samlRequest)
	encSigAlg := url.QueryEscape(rsaSHA256SigAlgURI)
	signed := "SAMLRequest=" + encReq + "&SigAlg=" + encSigAlg
	sum := sha256.Sum256([]byte(signed))
	sigBytes, serr := rsa.SignPKCS1v15(rand.Reader, m.key, crypto.SHA256, sum[:])
	if serr != nil {
		return "", "", fmt.Errorf("sign logoutrequest: %w", serr)
	}
	rawQuery := "SAMLRequest=" + encReq
	rawQuery += "&SigAlg=" + encSigAlg
	rawQuery += "&Signature=" + url.QueryEscape(base64.StdEncoding.EncodeToString(sigBytes))
	return rawQuery, requestID, nil
}

// authnRequestPostForm builds a POST-binding (enveloped-signed) AuthnRequest and
// returns the base64.StdEncoding SAMLRequest form value to POST to /saml/sso,
// plus the request ID. The enveloped signature is produced with goxmldsig (the
// same library the IdP's verifyPostAuthnSignature validates against), then the
// <ds:Signature> is relocated to immediately follow <Issuer> so the IdP's
// XSD-strict parse + reference-by-ID verification accept it.
func (m *mockSP) authnRequestPostForm(destination, acsURL string, opts authnOpts) (samlRequest, requestID string, err error) {
	requestID, err = newMockSAMLID()
	if err != nil {
		return "", "", err
	}
	ar := m.buildAuthnRequest(requestID, destination, acsURL, opts)
	el := ar.Element()

	ctx, err := dsig.NewSigningContext(m.key, [][]byte{m.certDER})
	if err != nil {
		return "", "", fmt.Errorf("new signing context: %w", err)
	}
	ctx.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
	if err := ctx.SetSignatureMethod(dsig.RSASHA256SignatureMethod); err != nil {
		return "", "", fmt.Errorf("set signature method: %w", err)
	}
	signed, err := ctx.SignEnveloped(el)
	if err != nil {
		return "", "", fmt.Errorf("sign enveloped: %w", err)
	}
	relocateSignatureAfterIssuer(signed)

	doc := etree.NewDocument()
	doc.SetRoot(signed)
	xmlBytes, err := doc.WriteToBytes()
	if err != nil {
		return "", "", fmt.Errorf("serialize signed authnrequest: %w", err)
	}
	return base64.StdEncoding.EncodeToString(xmlBytes), requestID, nil
}

// relocateSignatureAfterIssuer mirrors saml.relocateSignatureAfterIssuer: moves
// the direct-child <ds:Signature> goxmldsig appended last to immediately follow
// <Issuer>, matching the SAML 2.0 schema ordering strict SPs (and the IdP's
// XSD-aware parse) require. No-op if there is no direct-child Signature.
func relocateSignatureAfterIssuer(signed *etree.Element) {
	var sig *etree.Element
	for _, ch := range signed.ChildElements() {
		if ch.Tag == "Signature" {
			sig = ch
			break
		}
	}
	if sig == nil {
		return
	}
	sigSlot := -1
	for i, tok := range signed.Child {
		if e, ok := tok.(*etree.Element); ok && e == sig {
			sigSlot = i
			break
		}
	}
	if sigSlot < 0 {
		return
	}
	signed.RemoveChildAt(sigSlot)
	insertIdx := 0
	for _, ch := range signed.ChildElements() {
		if ch.Tag == "Issuer" {
			for i, tok := range signed.Child {
				if e, ok := tok.(*etree.Element); ok && e == ch {
					insertIdx = i + 1
					break
				}
			}
			break
		}
	}
	signed.InsertChildAt(insertIdx, sig)
}

// spVerifier wraps a crewjam ServiceProvider plus the parsed ACS URL so the
// smoke can call ParseXMLResponse without re-parsing the URL each time.
type spVerifier struct {
	provider *crewjam.ServiceProvider
	acsURL   url.URL
}

// parse verifies + parses a SAMLResponse, asserting its InResponseTo matches one
// of the possibleRequestIDs. crewjam internally enforces signature,
// Destination/Recipient == ACS, Audience == EntityID, and time windows.
func (v *spVerifier) parse(respXML []byte, requestID string) (*crewjam.Assertion, error) {
	return v.provider.ParseXMLResponse(respXML, []string{requestID}, v.acsURL)
}

// serviceProvider builds a crewjam ServiceProvider configured to verify a
// SAMLResponse from the given IdP metadata. Mirrors the Task 7 interop test.
func (m *mockSP) serviceProvider(idpMetaXML []byte) (*spVerifier, error) {
	return m.serviceProviderOpts(idpMetaXML, false)
}

// serviceProviderOpts is serviceProvider with an AllowIDPInitiated toggle. When
// true the crewjam ServiceProvider accepts an UNSOLICITED Response (no
// InResponseTo) — required to verify the IdP-initiated SSO Response.
func (m *mockSP) serviceProviderOpts(idpMetaXML []byte, allowIdPInitiated bool) (*spVerifier, error) {
	var idpED crewjam.EntityDescriptor
	if err := xml.Unmarshal(idpMetaXML, &idpED); err != nil {
		return nil, fmt.Errorf("unmarshal IdP metadata: %w", err)
	}
	acsParsed, err := url.Parse(m.acsURL)
	if err != nil {
		return nil, fmt.Errorf("parse ACS URL: %w", err)
	}
	return &spVerifier{
		provider: &crewjam.ServiceProvider{
			EntityID:          m.entityID,
			AcsURL:            *acsParsed,
			IDPMetadata:       &idpED,
			AllowIDPInitiated: allowIdPInitiated,
		},
		acsURL: *acsParsed,
	}, nil
}

// parseUnsolicited verifies + parses an UNSOLICITED SAMLResponse (no
// InResponseTo). possibleRequestIDs is nil, which crewjam requires for an
// IdP-initiated assertion (and which only succeeds when AllowIDPInitiated=true).
func (v *spVerifier) parseUnsolicited(respXML []byte) (*crewjam.Assertion, error) {
	return v.provider.ParseXMLResponse(respXML, nil, v.acsURL)
}

// verifyMetadataSignature asserts the IdP EntityDescriptor carries an enveloped
// <ds:Signature> that verifies against the cert embedded in its own first
// KeyDescriptor (self-consistency), and returns the parsed EntityDescriptor for
// validUntil inspection. Uses goxmldsig directly since cmd/smoke cannot reach
// the saml package's unexported verifyElementSignature.
func verifyMetadataSignature(idpMetaXML []byte) (*crewjam.EntityDescriptor, error) {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(idpMetaXML); err != nil {
		return nil, fmt.Errorf("parse metadata XML: %w", err)
	}
	root := doc.Root()
	if root == nil {
		return nil, errors.New("metadata has no root element")
	}
	// Find the direct-child <ds:Signature>.
	var sig *etree.Element
	for _, ch := range root.ChildElements() {
		if ch.Tag == "Signature" {
			sig = ch
			break
		}
	}
	if sig == nil {
		return nil, errors.New("metadata EntityDescriptor has no <ds:Signature> child")
	}

	var ed crewjam.EntityDescriptor
	if err := xml.Unmarshal(idpMetaXML, &ed); err != nil {
		return nil, fmt.Errorf("unmarshal EntityDescriptor: %w", err)
	}
	if len(ed.IDPSSODescriptors) == 0 || len(ed.IDPSSODescriptors[0].KeyDescriptors) == 0 {
		return nil, errors.New("metadata has no IDPSSODescriptor KeyDescriptor to verify against")
	}
	certB64 := ed.IDPSSODescriptors[0].KeyDescriptors[0].KeyInfo.X509Data.X509Certificates[0].Data
	certDER, err := base64.StdEncoding.DecodeString(strings.TrimSpace(certB64))
	if err != nil {
		return nil, fmt.Errorf("decode embedded cert: %w", err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("parse embedded cert: %w", err)
	}

	// goxmldsig uses "Id" by default; SAML uses "ID". Override so the enveloped
	// transform excludes the Signature keyed on the EntityDescriptor's ID attr.
	store := dsig.MemoryX509CertificateStore{Roots: []*x509.Certificate{cert}}
	vctx := dsig.NewDefaultValidationContext(&store)
	vctx.IdAttribute = "ID"
	if _, err := vctx.Validate(root); err != nil {
		return nil, fmt.Errorf("metadata signature does not verify against embedded cert: %w", err)
	}
	return &ed, nil
}

// ssoPostForm POSTs a base64 SAMLRequest (HTTP-POST binding) to /saml/sso.
// The session cookie is now Path=/, so c's jar auto-sends c's authenticated IdP
// session to the root-mounted endpoint (browser-equivalent); no manual attach.
// Redirects are NOT followed. Returns status + body; a 200 carries the auto-POST
// HTML with the SAMLResponse.
func ssoPostForm(c *client, samlRequest, relayState string) (status int, body string, err error) {
	form := url.Values{"SAMLRequest": {samlRequest}}
	if relayState != "" {
		form.Set("RelayState", relayState)
	}
	req, err := http.NewRequest(http.MethodPost, c.base+"/saml/sso", strings.NewReader(form.Encode()))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	hc := &http.Client{
		Jar:     c.jar,
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), nil
}

// ssoInit drives GET /saml/sso/init?sp=<entityID>[&RelayState=…]. The session
// cookie is now Path=/, so c's jar auto-sends it to the root-mounted endpoint
// (browser-equivalent); no manual attach. Redirects are NOT followed. Returns
// status + body; a 200 carries the auto-POST HTML with the unsolicited SAMLResponse.
func ssoInit(c *client, spEntityID, relayState string) (status int, body string, err error) {
	q := url.Values{"sp": {spEntityID}}
	if relayState != "" {
		q.Set("RelayState", relayState)
	}
	req, err := http.NewRequest(http.MethodGet, c.base+"/saml/sso/init?"+q.Encode(), nil)
	if err != nil {
		return 0, "", err
	}
	hc := &http.Client{
		Jar:     c.jar,
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), nil
}

// extractRelayState pulls the RelayState hidden-input value out of an auto-POST
// HTML page body (HTML-unescaped), or "" if absent.
func extractRelayState(htmlBody string) string {
	m := relayStateInputRe.FindStringSubmatch(htmlBody)
	if m == nil {
		return ""
	}
	return html.UnescapeString(m[1])
}

var relayStateInputRe = regexp.MustCompile(`name="RelayState" value="([^"]*)"`)

// createSAMLSPIdPInitiated registers a SAML SP with --allow-idp-initiated set,
// mirroring createSAMLSP's exec pattern.
func createSAMLSPIdPInitiated(baseURL, kind string, metadataXML []byte) error {
	tmp, err := os.CreateTemp("", "smoke-sp-idpinit-metadata-*.xml")
	if err != nil {
		return fmt.Errorf("create temp metadata file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(metadataXML); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write metadata file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close metadata file: %w", err)
	}
	cmd := exec.Command("mise", "exec", "--", "go", "run", "./cmd/prohibitorum",
		"saml-sp", "create", "--kind", kind, "--allow-idp-initiated", "--metadata-file", tmp.Name())
	cmd.Env = append(os.Environ(), "PROHIBITORUM_PUBLIC_ORIGIN="+baseURL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("saml-sp create (idp-initiated): %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Registered SAML SP") {
		return fmt.Errorf("saml-sp create (idp-initiated): unexpected output:\n%s", out)
	}
	return nil
}

// deflateBase64 raw-DEFLATEs (RFC 1951) then base64.StdEncoding-encodes b, the
// HTTP-Redirect binding's SAMLRequest encoding.
func deflateBase64(b []byte) (string, error) {
	var deflated bytes.Buffer
	fw, err := flate.NewWriter(&deflated, flate.DefaultCompression)
	if err != nil {
		return "", fmt.Errorf("flate writer: %w", err)
	}
	if _, err := fw.Write(b); err != nil {
		_ = fw.Close()
		return "", fmt.Errorf("deflate write: %w", err)
	}
	if err := fw.Close(); err != nil {
		return "", fmt.Errorf("deflate close: %w", err)
	}
	return base64.StdEncoding.EncodeToString(deflated.Bytes()), nil
}

// newMockSAMLID returns a SAML-NCName-safe request ID ("_"+hex(16 random bytes)).
func newMockSAMLID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "_" + hex.EncodeToString(raw[:]), nil
}

// samlResponseInputRe extracts the base64 SAMLResponse from an auto-POST HTML
// page. Mirrors the samlResponseRe in sso_test.go.
var samlResponseInputRe = regexp.MustCompile(`name="SAMLResponse" value="([^"]*)"`)

// extractSAMLResponse pulls the base64-decoded SAMLResponse XML out of an
// auto-POST HTML page body.
func extractSAMLResponse(htmlBody string) ([]byte, error) {
	m := samlResponseInputRe.FindStringSubmatch(htmlBody)
	if m == nil {
		return nil, fmt.Errorf("no SAMLResponse hidden input in auto-POST body:\n%s", htmlBody)
	}
	// html/template HTML-escapes the attribute value: the base64.Std alphabet's
	// '+' becomes "&#43;" and '/' may become "&#47;" / '=' stays. Unescape before
	// decoding (mirrors decodeAutoPost in sso_test.go).
	b64 := html.UnescapeString(m[1])
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("decode SAMLResponse base64: %w", err)
	}
	return raw, nil
}

// =========================================================================
// SAML smoke client helpers (live against the dev server)
// =========================================================================

// fetchSAMLMetadata GETs the IdP's /saml/metadata document (root-mounted, no
// session needed) and returns the raw bytes.
func fetchSAMLMetadata(baseURL string) ([]byte, error) {
	resp, err := http.Get(baseURL + "/saml/metadata")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /saml/metadata: %d %s", resp.StatusCode, body)
	}
	return body, nil
}

// ssoWithSession drives GET /saml/sso?<query> with c's authenticated IdP
// session. /saml/sso is root-mounted; the session cookie is now Path=/, so c's
// jar auto-sends it to the endpoint (browser-equivalent); no manual attach.
// Redirects are NOT followed (a session bounce → 302 /login is observable).
// Returns the response status and body. A 200 carries the auto-POST HTML with
// the SAMLResponse.
func ssoWithSession(c *client, query string) (status int, body string, err error) {
	req, err := http.NewRequest(http.MethodGet, c.base+"/saml/sso?"+query, nil)
	if err != nil {
		return 0, "", err
	}
	hc := &http.Client{
		Jar:     c.jar,
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), nil
}

// sloRedirect drives GET /saml/slo?<query> (no session cookie required — SLO is
// authenticated by the SP's detached signature, not an IdP session). Returns the
// response status, the Location header (a signed redirect-binding
// LogoutResponse), and the body. Redirects are NOT followed.
func sloRedirect(c *client, query string) (status int, location, body string, err error) {
	req, err := http.NewRequest(http.MethodGet, c.base+"/saml/slo?"+query, nil)
	if err != nil {
		return 0, "", "", err
	}
	hc := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, "", "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Location"), string(b), nil
}

// decodeRedirectLogoutResponse decodes a redirect-binding LogoutResponse
// Location URL: pull SAMLResponse → base64.Std → raw-INFLATE → unmarshal a
// crewjam LogoutResponse. Used to assert the IdP returned a signed Success
// LogoutResponse.
func decodeRedirectLogoutResponse(location string) (*crewjam.LogoutResponse, error) {
	u, err := url.Parse(location)
	if err != nil {
		return nil, fmt.Errorf("parse LogoutResponse Location: %w", err)
	}
	samlResp := u.Query().Get("SAMLResponse")
	if samlResp == "" {
		return nil, fmt.Errorf("LogoutResponse Location has no SAMLResponse: %q", location)
	}
	deflated, err := base64.StdEncoding.DecodeString(samlResp)
	if err != nil {
		return nil, fmt.Errorf("decode SAMLResponse base64: %w", err)
	}
	fr := flate.NewReader(bytes.NewReader(deflated))
	raw, err := io.ReadAll(fr)
	_ = fr.Close()
	if err != nil {
		return nil, fmt.Errorf("inflate LogoutResponse: %w", err)
	}
	var lr crewjam.LogoutResponse
	if err := xml.Unmarshal(raw, &lr); err != nil {
		return nil, fmt.Errorf("unmarshal LogoutResponse: %w", err)
	}
	return &lr, nil
}

// createSAMLSP shells out to `prohibitorum saml-sp create --kind <kind>
// --metadata-file <file>`, mirroring createOIDCClient's exec pattern. The CLI
// inherits PROHIBITORUM_* from os.Environ() and PUBLIC_ORIGIN is set so config
// parse succeeds. The SP metadata is written to a temp file first.
func createSAMLSP(baseURL, kind string, metadataXML []byte) error {
	tmp, err := os.CreateTemp("", "smoke-sp-metadata-*.xml")
	if err != nil {
		return fmt.Errorf("create temp metadata file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(metadataXML); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write metadata file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close metadata file: %w", err)
	}
	cmd := exec.Command("mise", "exec", "--", "go", "run", "./cmd/prohibitorum",
		"saml-sp", "create", "--kind", kind, "--metadata-file", tmp.Name())
	cmd.Env = append(os.Environ(), "PROHIBITORUM_PUBLIC_ORIGIN="+baseURL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("saml-sp create: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Registered SAML SP") {
		return fmt.Errorf("saml-sp create: unexpected output:\n%s", out)
	}
	return nil
}

// samlAttrValue returns the first value of the named SAML attribute in the
// assertion's AttributeStatements, or "" if absent.
func samlAttrValue(a *crewjam.Assertion, name string) string {
	if a == nil {
		return ""
	}
	for _, stmt := range a.AttributeStatements {
		for _, attr := range stmt.Attributes {
			if attr.Name == name || attr.FriendlyName == name {
				if len(attr.Values) > 0 {
					return attr.Values[0].Value
				}
			}
		}
	}
	return ""
}

// samlAttrNames lists every attribute Name present in the assertion (for
// diagnostics on a missing-attribute failure).
func samlAttrNames(a *crewjam.Assertion) []string {
	var names []string
	if a == nil {
		return names
	}
	for _, stmt := range a.AttributeStatements {
		for _, attr := range stmt.Attributes {
			names = append(names, attr.Name)
		}
	}
	return names
}

// =========================================================================
// SAML DB assertions (reusing the smoke's dbScalar pgx helper)
// =========================================================================

// verifySAMLSubjectStable asserts there is EXACTLY one saml_subject_id row for
// the account (one SP in this smoke) and its name_id equals the NameID the
// SAMLResponse carried — proving the per-(account,sp) NameID is stable + opaque.
func verifySAMLSubjectStable(accountID int32, wantNameID string) error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	if dburl == "" {
		return errors.New("PROHIBITORUM_DATABASE_URL not set")
	}
	rows, err := dbScalar(dburl, fmt.Sprintf(
		"SELECT name_id FROM saml_subject_id WHERE account_id=%d", accountID))
	if err != nil {
		return err
	}
	if len(rows) != 1 {
		return fmt.Errorf("expected exactly 1 saml_subject_id row for account %d, got %d (%v)",
			accountID, len(rows), rows)
	}
	if rows[0] != wantNameID {
		return fmt.Errorf("saml_subject_id.name_id: want %q (matching SAMLResponse NameID), got %q",
			wantNameID, rows[0])
	}
	log.Printf("  saml_subject_id: 1 row, name_id matches SAMLResponse NameID ✓")
	return nil
}

// verifySAMLSessionCount asserts at least minRows saml_session rows reference a
// session belonging to the account (joined via session.account_id).
func verifySAMLSessionCount(accountID int32, minRows int) error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	rows, err := dbScalar(dburl, fmt.Sprintf(
		"SELECT count(*)::text FROM saml_session ss "+
			"JOIN session s ON s.id = ss.session_id WHERE s.account_id=%d", accountID))
	if err != nil {
		return err
	}
	if len(rows) != 1 {
		return fmt.Errorf("saml_session count query returned %d rows", len(rows))
	}
	n, perr := strconv.Atoi(rows[0])
	if perr != nil {
		return fmt.Errorf("parse saml_session count %q: %w", rows[0], perr)
	}
	if n < minRows {
		return fmt.Errorf("expected >=%d saml_session rows for account %d, got %d", minRows, accountID, n)
	}
	log.Printf("  saml_session rows for account = %d (>=%d) ✓", n, minRows)
	return nil
}

// verifySAMLAuditEvents asserts credential_event has lower-bound counts for
// the saml_sp factor: ≥1 use (sso) and ≥1 session_end (slo). The concrete reason
// lives in detail->>'reason' ('sso' / 'slo').
func verifySAMLAuditEvents() error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	if dburl == "" {
		return errors.New("PROHIBITORUM_DATABASE_URL not set")
	}
	rows, err := dbScalar(dburl,
		"SELECT event || ':' || COALESCE(detail->>'reason','') || ':' || count(*)::text "+
			"FROM credential_event WHERE factor='saml_sp' "+
			"GROUP BY event, COALESCE(detail->>'reason','') "+
			"ORDER BY event, COALESCE(detail->>'reason','')")
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, row := range rows {
		parts := strings.SplitN(row, ":", 3)
		if len(parts) != 3 {
			continue
		}
		n, _ := strconv.Atoi(parts[2])
		counts[parts[0]+":"+parts[1]] = n
	}
	want := []struct {
		key string
		min int
	}{
		// use/sso: step 91 + 92 + 94 (cSLO) + 98 first presentation = ≥4.
		{"use:sso", 3},
		// session_end/slo: the SLO at step 95 = ≥1.
		{"session_end:slo", 1},
	}
	for _, w := range want {
		if counts[w.key] < w.min {
			return fmt.Errorf("credential_event saml_sp %s: want >=%d, got %d (full counts=%v)",
				w.key, w.min, counts[w.key], counts)
		}
	}
	log.Printf("  credential_event covers SAML lifecycle (counts=%v)", counts)
	return nil
}
