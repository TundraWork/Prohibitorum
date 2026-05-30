package saml

import (
	"bytes"
	"compress/flate"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	crewjam "github.com/crewjam/saml"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

// fakeAuthnQueries is a minimal db.Querier for AuthnRequest parsing tests. It
// embeds db.Querier (nil) to satisfy the interface; only the three methods the
// parse path calls are implemented. An unimplemented method would panic with a
// nil-interface dispatch, which is the desired "this test exercised an
// unexpected query" signal.
type fakeAuthnQueries struct {
	db.Querier
	spByEntityID map[string]db.SamlSp
	acsBySP      map[int64][]db.SamlSpAc
	keysBySP     map[int64][]db.SamlSpKey
}

func (f *fakeAuthnQueries) GetSAMLSPByEntityID(_ context.Context, entityID string) (db.SamlSp, error) {
	if sp, ok := f.spByEntityID[entityID]; ok {
		return sp, nil
	}
	return db.SamlSp{}, pgx.ErrNoRows
}

func (f *fakeAuthnQueries) ListSAMLSPACSEndpoints(_ context.Context, spID int64) ([]db.SamlSpAc, error) {
	return f.acsBySP[spID], nil
}

func (f *fakeAuthnQueries) ListSAMLSPKeys(_ context.Context, arg db.ListSAMLSPKeysParams) ([]db.SamlSpKey, error) {
	return f.keysBySP[arg.SpID], nil
}

// testSPKey mints a fresh RSA-2048 key + self-signed cert and the PEM the SP's
// signing-key row carries.
func testSPKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate sp key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "sp.example.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create sp cert: %v", err)
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))
	return priv, certPEM
}

const (
	testSPEntityID = "https://sp.example.test/saml/metadata"
	testIdPOrigin  = "https://idp.example.test"
	testSSOURL     = testIdPOrigin + "/saml/sso"
	testACSURL     = "https://sp.example.test/saml/acs"
)

// newAuthnTestIdP builds an IdP with a memory KV and the supplied fake querier.
func newAuthnTestIdP(q db.Querier) *IdP {
	return &IdP{
		cfg:     &configx.Config{PublicOrigins: []string{testIdPOrigin}},
		queries: q,
		kv:      kv.NewMemoryStore(),
	}
}

// authnReqOpts parameterizes the test request builder.
type authnReqOpts struct {
	id           string
	destination  string
	acsURL       string
	acsIndex     string // AssertionConsumerServiceIndex (empty = unset)
	version      string // override AuthnRequest @Version (empty = "2.0")
	relayState   string
	hasRelay     bool
	forceAuthn   bool
	isPassive    bool
	nameIDFormat string // NameIDPolicy/@Format (empty = no NameIDPolicy element)

	// signing controls
	sign     bool
	signKey  *rsa.PrivateKey
	sigAlg   string // override; default rsa-sha256 URI
	omitSigP bool   // build the signature octet string but drop the Signature param
}

// buildAuthnRedirect marshals an AuthnRequest, deflates+base64s it as
// SAMLRequest, and (optionally) appends a detached HTTP-Redirect signature. The
// signed octet string is built from the SAME url.QueryEscape encoding the
// verifier reads back out of RawQuery, so production and test agree byte-for-byte.
func buildAuthnRedirect(t *testing.T, o authnReqOpts) *http.Request {
	t.Helper()

	version := o.version
	if version == "" {
		version = "2.0"
	}
	ar := crewjam.AuthnRequest{
		ID:           o.id,
		Version:      version,
		IssueInstant: time.Now().UTC(),
		Destination:  o.destination,
		Issuer:       &crewjam.Issuer{Value: testSPEntityID},
	}
	if o.acsURL != "" {
		ar.AssertionConsumerServiceURL = o.acsURL
	}
	if o.acsIndex != "" {
		ar.AssertionConsumerServiceIndex = o.acsIndex
	}
	if o.forceAuthn {
		v := true
		ar.ForceAuthn = &v
	}
	if o.isPassive {
		v := true
		ar.IsPassive = &v
	}
	if o.nameIDFormat != "" {
		f := o.nameIDFormat
		ar.NameIDPolicy = &crewjam.NameIDPolicy{Format: &f}
	}

	xmlBytes, err := xml.Marshal(ar)
	if err != nil {
		t.Fatalf("marshal authnrequest: %v", err)
	}

	var deflated bytes.Buffer
	fw, err := flate.NewWriter(&deflated, flate.DefaultCompression)
	if err != nil {
		t.Fatalf("new flate writer: %v", err)
	}
	if _, err := fw.Write(xmlBytes); err != nil {
		t.Fatalf("deflate write: %v", err)
	}
	if err := fw.Close(); err != nil {
		t.Fatalf("deflate close: %v", err)
	}
	samlRequest := base64.StdEncoding.EncodeToString(deflated.Bytes())

	// Build the raw query. We deliberately percent-encode each value with
	// url.QueryEscape and place the raw encoded form on the wire; the verifier
	// reads these same raw bytes back out of RawQuery.
	encReq := url.QueryEscape(samlRequest)
	rawQuery := "SAMLRequest=" + encReq
	if o.hasRelay {
		rawQuery += "&RelayState=" + url.QueryEscape(o.relayState)
	}

	if o.sign {
		sigAlg := o.sigAlg
		if sigAlg == "" {
			sigAlg = rsaSHA256SigAlg
		}
		encSigAlg := url.QueryEscape(sigAlg)

		// Construct the signed octet string in the fixed order.
		signed := "SAMLRequest=" + encReq
		if o.hasRelay {
			signed += "&RelayState=" + url.QueryEscape(o.relayState)
		}
		signed += "&SigAlg=" + encSigAlg

		key := o.signKey
		if key == nil {
			t.Fatal("buildAuthnRedirect: sign=true requires signKey")
		}

		var sigBytes []byte
		if isSHA1Algorithm(sigAlg) {
			h := sha1Sum([]byte(signed))
			sigBytes, err = rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA1, h)
		} else {
			h := sha256.Sum256([]byte(signed))
			sigBytes, err = rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
		}
		if err != nil {
			t.Fatalf("sign: %v", err)
		}

		rawQuery += "&SigAlg=" + encSigAlg
		if !o.omitSigP {
			rawQuery += "&Signature=" + url.QueryEscape(base64.StdEncoding.EncodeToString(sigBytes))
		}
	}

	req := &http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{Scheme: "https", Host: "idp.example.test", Path: "/saml/sso", RawQuery: rawQuery},
	}
	return req
}

// sha1Sum is only used by the weak-alg negative test; isolated here so the
// production code never imports crypto/sha1.
func sha1Sum(b []byte) []byte {
	h := sha1.Sum(b)
	return h[:]
}

// buildAuthnPost builds a POST-binding AuthnRequest with an ENVELOPED signature
// over the AuthnRequest element (signed with signKey/signCertDER), then
// base64.StdEncoding (NO deflate) as the SAMLRequest form value. It mirrors
// buildLogoutPost in slo_test.go. When o.sign is false the element is serialized
// unsigned.
func buildAuthnPost(t *testing.T, o authnReqOpts, signCertDER []byte) *http.Request {
	t.Helper()

	version := o.version
	if version == "" {
		version = "2.0"
	}
	ar := crewjam.AuthnRequest{
		ID:           o.id,
		Version:      version,
		IssueInstant: time.Now().UTC(),
		Destination:  o.destination,
		Issuer:       &crewjam.Issuer{Value: testSPEntityID},
	}
	if o.acsURL != "" {
		ar.AssertionConsumerServiceURL = o.acsURL
	}
	if o.acsIndex != "" {
		ar.AssertionConsumerServiceIndex = o.acsIndex
	}
	if o.forceAuthn {
		v := true
		ar.ForceAuthn = &v
	}
	if o.isPassive {
		v := true
		ar.IsPassive = &v
	}
	if o.nameIDFormat != "" {
		f := o.nameIDFormat
		ar.NameIDPolicy = &crewjam.NameIDPolicy{Format: &f}
	}

	el := ar.Element() // crewjam's Element() already sets the SAML "ID" attribute.

	if o.sign {
		if o.signKey == nil {
			t.Fatal("buildAuthnPost: sign=true requires signKey")
		}
		signed, err := signElement(el, o.signKey, signCertDER)
		if err != nil {
			t.Fatalf("sign authnrequest element: %v", err)
		}
		el = signed
	}

	doc := etree.NewDocument()
	doc.SetRoot(el)
	xmlBytes, err := doc.WriteToBytes()
	if err != nil {
		t.Fatalf("serialize authnrequest: %v", err)
	}

	form := url.Values{}
	form.Set("SAMLRequest", base64.StdEncoding.EncodeToString(xmlBytes))
	if o.hasRelay {
		form.Set("RelayState", o.relayState)
	}
	req := httptest.NewRequest(http.MethodPost, testSSOURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func newAuthnQueries(sp db.SamlSp, acs []db.SamlSpAc, keys []db.SamlSpKey) *fakeAuthnQueries {
	return &fakeAuthnQueries{
		spByEntityID: map[string]db.SamlSp{sp.EntityID: sp},
		acsBySP:      map[int64][]db.SamlSpAc{sp.ID: acs},
		keysBySP:     map[int64][]db.SamlSpKey{sp.ID: keys},
	}
}

func defaultSP(requireSigned bool) db.SamlSp {
	return db.SamlSp{
		ID:                        7,
		EntityID:                  testSPEntityID,
		RequireSignedAuthnRequest: requireSigned,
	}
}

func acsList() []db.SamlSpAc {
	return []db.SamlSpAc{
		{SpID: 7, Idx: 0, Binding: crewjam.HTTPPostBinding, Location: testACSURL, IsDefault: true},
	}
}

func signingKeyRows(certPEM string) []db.SamlSpKey {
	return []db.SamlSpKey{
		{ID: 1, SpID: 7, Use: "signing", CertPem: certPEM, NotAfter: pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true}},
	}
}

func TestAuthnReqHappyPathSigned(t *testing.T) {
	priv, certPEM := testSPKey(t)
	q := newAuthnQueries(defaultSP(true), acsList(), signingKeyRows(certPEM))
	idp := newAuthnTestIdP(q)

	req := buildAuthnRedirect(t, authnReqOpts{
		id:          "_req-happy-1",
		destination: testSSOURL,
		acsURL:      testACSURL,
		relayState:  "state-xyz/with+specials",
		hasRelay:    true,
		forceAuthn:  true,
		isPassive:   true,
		sign:        true,
		signKey:     priv,
	})

	got, err := idp.parseAuthnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("parseAuthnRequest: %v", err)
	}
	if got.RequestID != "_req-happy-1" {
		t.Errorf("RequestID = %q", got.RequestID)
	}
	if got.ACSURL != testACSURL {
		t.Errorf("ACSURL = %q, want %q", got.ACSURL, testACSURL)
	}
	if got.RelayState != "state-xyz/with+specials" {
		t.Errorf("RelayState = %q", got.RelayState)
	}
	if !got.ForceAuthn {
		t.Error("ForceAuthn = false, want true")
	}
	if !got.IsPassive {
		t.Error("IsPassive = false, want true")
	}
	if got.SP.ID != 7 {
		t.Errorf("SP.ID = %d, want 7", got.SP.ID)
	}
}

func TestAuthnReqRequiredButNoSignature(t *testing.T) {
	priv, certPEM := testSPKey(t)
	q := newAuthnQueries(defaultSP(true), acsList(), signingKeyRows(certPEM))
	idp := newAuthnTestIdP(q)

	// sign=false → no Signature/SigAlg params at all.
	req := buildAuthnRedirect(t, authnReqOpts{
		id:          "_req-nosig",
		destination: testSSOURL,
		acsURL:      testACSURL,
		sign:        false,
		signKey:     priv,
	})

	_, err := idp.parseAuthnRequest(context.Background(), req)
	if !errors.Is(err, ErrMissingSignature) {
		t.Fatalf("err = %v, want ErrMissingSignature", err)
	}
}

func TestAuthnReqWrongKeySignature(t *testing.T) {
	_, certPEM := testSPKey(t)  // cert registered for the SP
	wrongKey, _ := testSPKey(t) // a DIFFERENT key signs the request
	q := newAuthnQueries(defaultSP(true), acsList(), signingKeyRows(certPEM))
	idp := newAuthnTestIdP(q)

	req := buildAuthnRedirect(t, authnReqOpts{
		id:          "_req-wrongkey",
		destination: testSSOURL,
		acsURL:      testACSURL,
		sign:        true,
		signKey:     wrongKey,
	})

	_, err := idp.parseAuthnRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected signature verification error, got nil")
	}
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("err = %v, want ErrBadSignature", err)
	}
}

// testSPKeyWithValidity mints an SP key whose self-signed cert carries the
// supplied validity window, so a test can register an expired (or
// not-yet-valid) signing cert.
func testSPKeyWithValidity(t *testing.T, notBefore, notAfter time.Time) (*rsa.PrivateKey, string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate sp key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "sp.example.test"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create sp cert: %v", err)
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))
	return priv, certPEM
}

// TestAuthnReqExpiredCertRejected proves Fix A2: the redirect-binding path now
// skips a signing cert whose validity window does not include now (matching the
// POST binding's goxmldsig behavior). With only an expired cert registered, an
// otherwise-valid signature must be rejected as ErrBadSignature.
func TestAuthnReqExpiredCertRejected(t *testing.T) {
	priv, certPEM := testSPKeyWithValidity(t,
		time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour)) // expired yesterday
	q := newAuthnQueries(defaultSP(true), acsList(), signingKeyRows(certPEM))
	idp := newAuthnTestIdP(q)

	req := buildAuthnRedirect(t, authnReqOpts{
		id:          "_req-expired",
		destination: testSSOURL,
		acsURL:      testACSURL,
		sign:        true,
		signKey:     priv,
	})

	_, err := idp.parseAuthnRequest(context.Background(), req)
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("err = %v, want ErrBadSignature (expired cert must not verify)", err)
	}
}

// TestAuthnReqRotationSkipsExpiredCert proves the A2 skip is per-cert: an
// expired cert AND a live cert are registered; the request is signed with the
// LIVE key, and parsing must succeed (the expired cert is skipped, not fatal).
func TestAuthnReqRotationSkipsExpiredCert(t *testing.T) {
	_, expiredPEM := testSPKeyWithValidity(t,
		time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour))
	livePriv, livePEM := testSPKey(t)

	keys := []db.SamlSpKey{
		{ID: 1, SpID: 7, Use: "signing", CertPem: expiredPEM},
		{ID: 2, SpID: 7, Use: "signing", CertPem: livePEM},
	}
	q := newAuthnQueries(defaultSP(true), acsList(), keys)
	idp := newAuthnTestIdP(q)

	req := buildAuthnRedirect(t, authnReqOpts{
		id:          "_req-rotation",
		destination: testSSOURL,
		acsURL:      testACSURL,
		sign:        true,
		signKey:     livePriv,
	})

	if _, err := idp.parseAuthnRequest(context.Background(), req); err != nil {
		t.Fatalf("parseAuthnRequest with a live cert in the rotation set: %v", err)
	}
}

func TestAuthnReqWeakSHA1SigAlg(t *testing.T) {
	priv, certPEM := testSPKey(t)
	q := newAuthnQueries(defaultSP(true), acsList(), signingKeyRows(certPEM))
	idp := newAuthnTestIdP(q)

	req := buildAuthnRedirect(t, authnReqOpts{
		id:          "_req-sha1",
		destination: testSSOURL,
		acsURL:      testACSURL,
		sign:        true,
		signKey:     priv,
		sigAlg:      "http://www.w3.org/2000/09/xmldsig#rsa-sha1",
	})

	_, err := idp.parseAuthnRequest(context.Background(), req)
	if !errors.Is(err, errWeakSigAlg) {
		t.Fatalf("err = %v, want errWeakSigAlg", err)
	}
}

func TestAuthnReqRequestedACSNotRegistered(t *testing.T) {
	priv, certPEM := testSPKey(t)
	q := newAuthnQueries(defaultSP(true), acsList(), signingKeyRows(certPEM))
	idp := newAuthnTestIdP(q)

	req := buildAuthnRedirect(t, authnReqOpts{
		id:          "_req-badacs",
		destination: testSSOURL,
		acsURL:      "https://attacker.example/steal",
		sign:        true,
		signKey:     priv,
	})

	_, err := idp.parseAuthnRequest(context.Background(), req)
	if !errors.Is(err, ErrInvalidACS) {
		t.Fatalf("err = %v, want ErrInvalidACS", err)
	}
}

func TestAuthnReqNoACSResolvesDefault(t *testing.T) {
	priv, certPEM := testSPKey(t)
	q := newAuthnQueries(defaultSP(true), acsList(), signingKeyRows(certPEM))
	idp := newAuthnTestIdP(q)

	// No acsURL on the request → must resolve to the IsDefault endpoint.
	req := buildAuthnRedirect(t, authnReqOpts{
		id:          "_req-defaultacs",
		destination: testSSOURL,
		sign:        true,
		signKey:     priv,
	})

	got, err := idp.parseAuthnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("parseAuthnRequest: %v", err)
	}
	if got.ACSURL != testACSURL {
		t.Errorf("ACSURL = %q, want default %q", got.ACSURL, testACSURL)
	}
}

// TestAuthnReqNoACSNoDefaultLowestIndex proves Fix B1: when neither an ACS URL
// nor index is supplied AND no endpoint is marked isDefault, the implicit
// default per Metadata IndexedEndpointType §2.2.3 is the LOWEST-index endpoint
// (NOT an error). The list is intentionally given out of index order to prove
// we pick by Idx and not by slice position.
func TestAuthnReqNoACSNoDefaultLowestIndex(t *testing.T) {
	priv, certPEM := testSPKey(t)
	noDefault := []db.SamlSpAc{
		{SpID: 7, Idx: 2, Binding: crewjam.HTTPPostBinding, Location: "https://sp.example/acs/2", IsDefault: false},
		{SpID: 7, Idx: 0, Binding: crewjam.HTTPPostBinding, Location: testACSURL, IsDefault: false},
		{SpID: 7, Idx: 1, Binding: crewjam.HTTPPostBinding, Location: "https://sp.example/acs/1", IsDefault: false},
	}
	q := newAuthnQueries(defaultSP(true), noDefault, signingKeyRows(certPEM))
	idp := newAuthnTestIdP(q)

	req := buildAuthnRedirect(t, authnReqOpts{
		id:          "_req-nodefault",
		destination: testSSOURL,
		sign:        true,
		signKey:     priv,
	})

	got, err := idp.parseAuthnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("parseAuthnRequest: %v", err)
	}
	if got.ACSURL != testACSURL {
		t.Errorf("ACSURL = %q, want lowest-index %q", got.ACSURL, testACSURL)
	}
}

// TestAuthnReqNoACSNoEndpoints confirms that with ZERO registered ACS rows and
// neither a URL nor an index supplied, resolution fails with ErrInvalidACS.
func TestAuthnReqNoACSNoEndpoints(t *testing.T) {
	priv, certPEM := testSPKey(t)
	q := newAuthnQueries(defaultSP(true), []db.SamlSpAc{}, signingKeyRows(certPEM))
	idp := newAuthnTestIdP(q)

	req := buildAuthnRedirect(t, authnReqOpts{
		id:          "_req-noacsrows",
		destination: testSSOURL,
		sign:        true,
		signKey:     priv,
	})

	_, err := idp.parseAuthnRequest(context.Background(), req)
	if !errors.Is(err, ErrInvalidACS) {
		t.Fatalf("err = %v, want ErrInvalidACS", err)
	}
}

// TestAuthnReqACSByIndex proves Fix B1: an SP may identify its ACS by
// AssertionConsumerServiceIndex (Web SSO Profile §4.1.4.1). The indexed
// endpoint's Location is resolved.
func TestAuthnReqACSByIndex(t *testing.T) {
	priv, certPEM := testSPKey(t)
	acs := []db.SamlSpAc{
		{SpID: 7, Idx: 0, Binding: crewjam.HTTPPostBinding, Location: testACSURL, IsDefault: true},
		{SpID: 7, Idx: 5, Binding: crewjam.HTTPPostBinding, Location: "https://sp.example/acs/five", IsDefault: false},
	}
	q := newAuthnQueries(defaultSP(true), acs, signingKeyRows(certPEM))
	idp := newAuthnTestIdP(q)

	req := buildAuthnRedirect(t, authnReqOpts{
		id:          "_req-acsidx",
		destination: testSSOURL,
		acsIndex:    "5",
		sign:        true,
		signKey:     priv,
	})

	got, err := idp.parseAuthnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("parseAuthnRequest: %v", err)
	}
	if got.ACSURL != "https://sp.example/acs/five" {
		t.Errorf("ACSURL = %q, want indexed endpoint", got.ACSURL)
	}
}

// TestAuthnReqACSByBadIndex confirms an AssertionConsumerServiceIndex that
// matches no registered endpoint (and a non-integer index) is rejected with
// ErrInvalidACS rather than silently falling back to a default.
func TestAuthnReqACSByBadIndex(t *testing.T) {
	priv, certPEM := testSPKey(t)
	q := newAuthnQueries(defaultSP(true), acsList(), signingKeyRows(certPEM))
	idp := newAuthnTestIdP(q)

	for _, idx := range []string{"99", "notanint"} {
		req := buildAuthnRedirect(t, authnReqOpts{
			id:          "_req-acsbadidx-" + idx,
			destination: testSSOURL,
			acsIndex:    idx,
			sign:        true,
			signKey:     priv,
		})
		_, err := idp.parseAuthnRequest(context.Background(), req)
		if !errors.Is(err, ErrInvalidACS) {
			t.Fatalf("index %q: err = %v, want ErrInvalidACS", idx, err)
		}
	}
}

// TestAuthnReqBadVersion proves Fix B3: an AuthnRequest with Version != "2.0"
// is rejected as malformed (Core §3.2.1).
func TestAuthnReqBadVersion(t *testing.T) {
	priv, certPEM := testSPKey(t)
	q := newAuthnQueries(defaultSP(true), acsList(), signingKeyRows(certPEM))
	idp := newAuthnTestIdP(q)

	req := buildAuthnRedirect(t, authnReqOpts{
		id:          "_req-badver",
		destination: testSSOURL,
		acsURL:      testACSURL,
		version:     "1.1",
		sign:        true,
		signKey:     priv,
	})

	_, err := idp.parseAuthnRequest(context.Background(), req)
	if !errors.Is(err, ErrMalformedRequest) {
		t.Fatalf("err = %v, want ErrMalformedRequest", err)
	}
}

// TestAuthnReqReplay confirms single-use replay protection now lives in
// consumeAuthnRequestID (the terminal/issue path), NOT in parseAuthnRequest.
// parseAuthnRequest is pure parse+validate and writes no KV, so the login
// bounce (which re-parses the same SAMLRequest) does not trip replay; only
// consumeAuthnRequestID does, and only on the second call for a given ID.
func TestAuthnReqReplay(t *testing.T) {
	q := newAuthnQueries(defaultSP(true), acsList(), nil)
	idp := newAuthnTestIdP(q)
	ctx := context.Background()

	const id = "_req-replay-same"

	// parseAuthnRequest must NOT consume the replay key: parsing the same
	// request twice both succeed at the consume step below.
	if err := idp.consumeAuthnRequestID(ctx, id); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	if err := idp.consumeAuthnRequestID(ctx, id); !errors.Is(err, ErrReplayedRequest) {
		t.Fatalf("second consume err = %v, want ErrReplayedRequest", err)
	}
}

// TestAuthnReqParseDoesNotConsumeReplay confirms parseAuthnRequest can be run
// twice on the same SAMLRequest without tripping replay — the property the
// login-bounce return trip depends on.
func TestAuthnReqParseDoesNotConsumeReplay(t *testing.T) {
	priv, certPEM := testSPKey(t)
	q := newAuthnQueries(defaultSP(true), acsList(), signingKeyRows(certPEM))
	idp := newAuthnTestIdP(q)
	ctx := context.Background()

	mk := func() *http.Request {
		return buildAuthnRedirect(t, authnReqOpts{
			id:          "_req-parse-twice",
			destination: testSSOURL,
			acsURL:      testACSURL,
			sign:        true,
			signKey:     priv,
		})
	}

	if _, err := idp.parseAuthnRequest(ctx, mk()); err != nil {
		t.Fatalf("first parse: %v", err)
	}
	if _, err := idp.parseAuthnRequest(ctx, mk()); err != nil {
		t.Fatalf("second parse must also succeed (no replay on parse): %v", err)
	}
}

func TestAuthnReqBadDestination(t *testing.T) {
	priv, certPEM := testSPKey(t)
	q := newAuthnQueries(defaultSP(true), acsList(), signingKeyRows(certPEM))
	idp := newAuthnTestIdP(q)

	req := buildAuthnRedirect(t, authnReqOpts{
		id:          "_req-baddest",
		destination: "https://evil.example/saml/sso",
		acsURL:      testACSURL,
		sign:        true,
		signKey:     priv,
	})

	_, err := idp.parseAuthnRequest(context.Background(), req)
	if !errors.Is(err, ErrBadDestination) {
		t.Fatalf("err = %v, want ErrBadDestination", err)
	}
}

func TestAuthnReqUnknownSP(t *testing.T) {
	priv, certPEM := testSPKey(t)
	// Register an SP under a DIFFERENT entity ID so the lookup misses.
	other := defaultSP(true)
	other.EntityID = "https://someone-else.example/metadata"
	q := newAuthnQueries(other, acsList(), signingKeyRows(certPEM))
	idp := newAuthnTestIdP(q)

	req := buildAuthnRedirect(t, authnReqOpts{
		id:          "_req-unknownsp",
		destination: testSSOURL,
		acsURL:      testACSURL,
		sign:        true,
		signKey:     priv,
	})

	_, err := idp.parseAuthnRequest(context.Background(), req)
	if !errors.Is(err, ErrUnknownSP) {
		t.Fatalf("err = %v, want ErrUnknownSP", err)
	}
}

// TestAuthnReqDuplicateParamRejected confirms that a redirect query repeating a
// redirect-binding param (here, two SAMLRequest=) is rejected as malformed,
// closing the first-vs-last split-brain between the validated XML and the
// signature-checked octet string.
func TestAuthnReqDuplicateParamRejected(t *testing.T) {
	priv, certPEM := testSPKey(t)
	q := newAuthnQueries(defaultSP(true), acsList(), signingKeyRows(certPEM))
	idp := newAuthnTestIdP(q)

	// Build a normal signed request, then duplicate the SAMLRequest param.
	req := buildAuthnRedirect(t, authnReqOpts{
		id:          "_req-dup",
		destination: testSSOURL,
		acsURL:      testACSURL,
		sign:        true,
		signKey:     priv,
	})
	req.URL.RawQuery = "SAMLRequest=AAAA&" + req.URL.RawQuery

	_, err := idp.parseAuthnRequest(context.Background(), req)
	if !errors.Is(err, ErrMalformedRequest) {
		t.Fatalf("err = %v, want ErrMalformedRequest", err)
	}
}

// TestAuthnReqEmptyID confirms an AuthnRequest with an empty @ID is rejected as
// malformed before it can degenerate the replay key / InResponseTo.
func TestAuthnReqEmptyID(t *testing.T) {
	q := newAuthnQueries(defaultSP(false), acsList(), nil)
	idp := newAuthnTestIdP(q)

	req := buildAuthnRedirect(t, authnReqOpts{
		id:          "", // no @ID
		destination: testSSOURL,
		acsURL:      testACSURL,
		sign:        false,
	})

	_, err := idp.parseAuthnRequest(context.Background(), req)
	if !errors.Is(err, ErrMalformedRequest) {
		t.Fatalf("err = %v, want ErrMalformedRequest", err)
	}
}

// TestAuthnReqSigAlgWithoutSignature confirms that a SigAlg present with no
// Signature param yields ErrMissingSignature (omitSigP drops the Signature but
// keeps SigAlg).
func TestAuthnReqSigAlgWithoutSignature(t *testing.T) {
	priv, certPEM := testSPKey(t)
	q := newAuthnQueries(defaultSP(true), acsList(), signingKeyRows(certPEM))
	idp := newAuthnTestIdP(q)

	req := buildAuthnRedirect(t, authnReqOpts{
		id:          "_req-sigalg-nosig",
		destination: testSSOURL,
		acsURL:      testACSURL,
		sign:        true,
		signKey:     priv,
		omitSigP:    true, // SigAlg present, Signature param dropped
	})

	_, err := idp.parseAuthnRequest(context.Background(), req)
	if !errors.Is(err, ErrMissingSignature) {
		t.Fatalf("err = %v, want ErrMissingSignature", err)
	}
}

// TestAuthnReqSignatureWithoutSigAlg confirms that a Signature param present
// with NO SigAlg yields ErrMissingSignature. The builder cannot express this
// (it only emits Signature alongside SigAlg), so the RawQuery is constructed
// directly from a valid signed request with the SigAlg param stripped.
func TestAuthnReqSignatureWithoutSigAlg(t *testing.T) {
	priv, certPEM := testSPKey(t)
	q := newAuthnQueries(defaultSP(true), acsList(), signingKeyRows(certPEM))
	idp := newAuthnTestIdP(q)

	req := buildAuthnRedirect(t, authnReqOpts{
		id:          "_req-sig-nosigalg",
		destination: testSSOURL,
		acsURL:      testACSURL,
		sign:        true,
		signKey:     priv,
	})

	// Strip the SigAlg param, leaving SAMLRequest + Signature only.
	parts := strings.Split(req.URL.RawQuery, "&")
	kept := parts[:0]
	for _, p := range parts {
		if strings.HasPrefix(p, "SigAlg=") {
			continue
		}
		kept = append(kept, p)
	}
	req.URL.RawQuery = strings.Join(kept, "&")

	_, err := idp.parseAuthnRequest(context.Background(), req)
	if !errors.Is(err, ErrMissingSignature) {
		t.Fatalf("err = %v, want ErrMissingSignature", err)
	}
}

// TestAuthnReqUnsignedAllowed confirms that when the SP does NOT require signed
// requests, an unsigned AuthnRequest parses successfully.
func TestAuthnReqUnsignedAllowed(t *testing.T) {
	q := newAuthnQueries(defaultSP(false), acsList(), nil)
	idp := newAuthnTestIdP(q)

	req := buildAuthnRedirect(t, authnReqOpts{
		id:          "_req-unsigned-ok",
		destination: testSSOURL,
		acsURL:      testACSURL,
		sign:        false,
	})

	got, err := idp.parseAuthnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("parseAuthnRequest: %v", err)
	}
	if got.ACSURL != testACSURL {
		t.Errorf("ACSURL = %q", got.ACSURL)
	}
}

// spCertDER parses the SP signing-cert PEM back to its DER bytes, which the
// enveloped-signature builder (signElement) needs for the embedded KeyInfo.
func spCertDER(t *testing.T, certPEM string) []byte {
	t.Helper()
	cert, err := parseCertPEM(certPEM)
	if err != nil {
		t.Fatalf("parse sp cert PEM: %v", err)
	}
	return cert.Raw
}

// TestAuthnReqPostHappyPathSigned proves the HTTP-POST binding: an AuthnRequest
// carrying an ENVELOPED signature on the AuthnRequest element, base64'd (no
// deflate) as the SAMLRequest form value, parses and verifies against the SP's
// registered signing cert.
func TestAuthnReqPostHappyPathSigned(t *testing.T) {
	priv, certPEM := testSPKey(t)
	q := newAuthnQueries(defaultSP(true), acsList(), signingKeyRows(certPEM))
	idp := newAuthnTestIdP(q)

	req := buildAuthnPost(t, authnReqOpts{
		id:          "_req-post-happy",
		destination: testSSOURL,
		acsURL:      testACSURL,
		relayState:  "post-state/with+specials",
		hasRelay:    true,
		forceAuthn:  true,
		isPassive:   true,
		sign:        true,
		signKey:     priv,
	}, spCertDER(t, certPEM))

	got, err := idp.parseAuthnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("parseAuthnRequest (POST): %v", err)
	}
	if got.RequestID != "_req-post-happy" {
		t.Errorf("RequestID = %q", got.RequestID)
	}
	if got.ACSURL != testACSURL {
		t.Errorf("ACSURL = %q, want %q", got.ACSURL, testACSURL)
	}
	if got.RelayState != "post-state/with+specials" {
		t.Errorf("RelayState = %q", got.RelayState)
	}
	if !got.ForceAuthn {
		t.Error("ForceAuthn = false, want true")
	}
	if !got.IsPassive {
		t.Error("IsPassive = false, want true")
	}
	if got.SP.ID != 7 {
		t.Errorf("SP.ID = %d, want 7", got.SP.ID)
	}
}

// TestAuthnReqPostRequiredButNoSignature proves an UNSIGNED POST-binding
// AuthnRequest to an SP that requires signing is rejected: the enveloped
// signature is absent, so verifyPostAuthnSignature returns errNoSignature.
func TestAuthnReqPostRequiredButNoSignature(t *testing.T) {
	priv, certPEM := testSPKey(t)
	q := newAuthnQueries(defaultSP(true), acsList(), signingKeyRows(certPEM))
	idp := newAuthnTestIdP(q)

	req := buildAuthnPost(t, authnReqOpts{
		id:          "_req-post-nosig",
		destination: testSSOURL,
		acsURL:      testACSURL,
		sign:        false,
		signKey:     priv,
	}, spCertDER(t, certPEM))

	_, err := idp.parseAuthnRequest(context.Background(), req)
	if !errors.Is(err, errNoSignature) {
		t.Fatalf("err = %v, want errNoSignature (absent enveloped signature)", err)
	}
}

// TestAuthnReqPostWrongKeySignature proves a POST-binding AuthnRequest signed by
// a key whose cert is NOT registered for the SP fails enveloped-signature
// verification.
func TestAuthnReqPostWrongKeySignature(t *testing.T) {
	_, certPEM := testSPKey(t)            // cert registered for the SP
	wrongKey, wrongPEM := testSPKey(t)    // a DIFFERENT key+cert signs the request
	q := newAuthnQueries(defaultSP(true), acsList(), signingKeyRows(certPEM))
	idp := newAuthnTestIdP(q)

	req := buildAuthnPost(t, authnReqOpts{
		id:          "_req-post-wrongkey",
		destination: testSSOURL,
		acsURL:      testACSURL,
		sign:        true,
		signKey:     wrongKey,
	}, spCertDER(t, wrongPEM))

	_, err := idp.parseAuthnRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected enveloped-signature verification error, got nil")
	}
	if errors.Is(err, errNoSignature) {
		t.Fatalf("err = %v, want a signature-mismatch error, not errNoSignature", err)
	}
}
