package saml

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	crewjam "github.com/crewjam/saml"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

// ---------------------------------------------------------------------------
// Test harness: a fake db.Querier covering the SSO orchestration path, plus a
// recording audit writer. The IdP is wired with a real in-memory KV and a real
// authn.RateLimiter so the replay/rate-limit logic is exercised for real.
// ---------------------------------------------------------------------------

// fakeSSOQueries serves the SSO handler's full query surface from memory. It
// embeds db.Querier (nil) so any UNimplemented method panics — the desired
// "this test exercised an unexpected query" signal.
type fakeSSOQueries struct {
	db.Querier

	sp        db.SamlSp
	acs       []db.SamlSpAc
	spKeys    []db.SamlSpKey
	idpKeys   []db.SigningKey
	authTime  time.Time
	subjectID string // pre-seeded stable NameID; "" => mint-on-first-use

	mu              sync.Mutex
	insertedSession []db.InsertSAMLSessionParams
	mintedSubject   map[int64]string // spID -> minted NameID (persisted across calls)
}

func (f *fakeSSOQueries) GetSAMLSPByEntityID(_ context.Context, entityID string) (db.SamlSp, error) {
	if entityID == f.sp.EntityID {
		return f.sp, nil
	}
	return db.SamlSp{}, pgx.ErrNoRows
}

func (f *fakeSSOQueries) ListSAMLSPACSEndpoints(_ context.Context, spID int64) ([]db.SamlSpAc, error) {
	return f.acs, nil
}

func (f *fakeSSOQueries) ListSAMLSPKeys(_ context.Context, _ db.ListSAMLSPKeysParams) ([]db.SamlSpKey, error) {
	return f.spKeys, nil
}

func (f *fakeSSOQueries) ListPublishableSigningKeys(context.Context) ([]db.SigningKey, error) {
	return f.idpKeys, nil
}

func (f *fakeSSOQueries) GetSession(_ context.Context, _ string) (db.Session, error) {
	return db.Session{AuthTime: pgtype.Timestamptz{Time: f.authTime, Valid: true}}, nil
}

func (f *fakeSSOQueries) GetSAMLSubjectID(_ context.Context, arg db.GetSAMLSubjectIDParams) (db.SamlSubjectID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.subjectID != "" {
		return db.SamlSubjectID{AccountID: arg.AccountID, SpID: arg.SpID, NameID: f.subjectID}, nil
	}
	if nid, ok := f.mintedSubject[arg.SpID]; ok {
		return db.SamlSubjectID{AccountID: arg.AccountID, SpID: arg.SpID, NameID: nid}, nil
	}
	return db.SamlSubjectID{}, pgx.ErrNoRows
}

func (f *fakeSSOQueries) InsertSAMLSubjectID(_ context.Context, arg db.InsertSAMLSubjectIDParams) (db.SamlSubjectID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mintedSubject == nil {
		f.mintedSubject = map[int64]string{}
	}
	// Emulate ON CONFLICT DO NOTHING/keep-existing: first writer wins.
	if existing, ok := f.mintedSubject[arg.SpID]; ok {
		return db.SamlSubjectID{AccountID: arg.AccountID, SpID: arg.SpID, NameID: existing}, nil
	}
	f.mintedSubject[arg.SpID] = arg.NameID
	return db.SamlSubjectID{AccountID: arg.AccountID, SpID: arg.SpID, NameID: arg.NameID}, nil
}

func (f *fakeSSOQueries) InsertSAMLSession(_ context.Context, arg db.InsertSAMLSessionParams) (db.SamlSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.insertedSession = append(f.insertedSession, arg)
	return db.SamlSession{SessionID: arg.SessionID, SpID: arg.SpID, NameID: arg.NameID, SessionIndex: arg.SessionIndex}, nil
}

func (f *fakeSSOQueries) sessions() []db.InsertSAMLSessionParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]db.InsertSAMLSessionParams(nil), f.insertedSession...)
}

// recordingAudit captures audit records for assertion.
type recordingAudit struct {
	mu      sync.Mutex
	records []audit.Record
}

func (r *recordingAudit) Record(_ context.Context, rec audit.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, rec)
	return nil
}

func (r *recordingAudit) all() []audit.Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]audit.Record(nil), r.records...)
}

// ssoHarness bundles the IdP under test with its recording dependencies.
type ssoHarness struct {
	idp     *IdP
	q       *fakeSSOQueries
	auditW  *recordingAudit
	idpCert *x509.Certificate // the IdP signing cert (for verifying our Response)
	spKey   *rsa.PrivateKey   // the SP's signing key (to sign AuthnRequests)
}

func newSSOHarness(t *testing.T, sp db.SamlSp) *ssoHarness {
	t.Helper()

	idpRow, _, idpCert := testSAMLSigningKeyRow(t)
	spKey, spCertPEM := testSPKey(t)

	q := &fakeSSOQueries{
		sp:       sp,
		acs:      []db.SamlSpAc{{SpID: sp.ID, Idx: 0, Binding: crewjam.HTTPPostBinding, Location: testACSURL, IsDefault: true}},
		spKeys:   []db.SamlSpKey{{ID: 1, SpID: sp.ID, Use: "signing", CertPem: spCertPEM, NotAfter: pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true}}},
		idpKeys:  []db.SigningKey{idpRow},
		authTime: time.Now().Add(-time.Minute),
	}

	cfg := &configx.Config{PublicOrigins: []string{testIdPOrigin}}
	idp := &IdP{
		cfg:     cfg,
		queries: q,
		kv:      kv.NewMemoryStore(),
		audit:   &recordingAudit{},
		rl:      authn.NewRateLimiter(),
		keys:    newSAMLKeyCache(q),
	}
	auditW := idp.audit.(*recordingAudit)

	return &ssoHarness{idp: idp, q: q, auditW: auditW, idpCert: idpCert, spKey: spKey}
}

// ssoSP returns a default SP for SSO tests: requires signed AuthnRequests, GHES
// attribute map, persistent NameID.
func ssoSP() db.SamlSp {
	return db.SamlSp{
		ID:                        7,
		EntityID:                  testSPEntityID,
		NameIDFormat:              "urn:oasis:names:tc:SAML:1.1:nameid-format:persistent",
		AttributeMap:              ghesDefaultAttributeMap(),
		RequireSignedAuthnRequest: true,
	}
}

// signedSSORequest builds a signed redirect-binding AuthnRequest GET request and
// attaches an authenticated session to its context.
func (h *ssoHarness) request(t *testing.T, id string, sess *authn.Session, opts ...func(*authnReqOpts)) *http.Request {
	t.Helper()
	o := authnReqOpts{
		id:          id,
		destination: testSSOURL,
		acsURL:      testACSURL,
		sign:        true,
		signKey:     h.spKey,
	}
	for _, fn := range opts {
		fn(&o)
	}
	req := buildAuthnRedirect(t, o)
	req.RemoteAddr = "203.0.113.7:54321"
	req.Header = http.Header{"User-Agent": []string{"test-agent/1.0"}}
	if sess != nil {
		req = req.WithContext(authn.WithSession(req.Context(), sess))
	}
	return req
}

// liveSession builds an authenticated session for the given account.
func liveSession(acct *db.Account) *authn.Session {
	return &authn.Session{
		Account: acct,
		Data: &authn.SessionData{
			SessionID: "sess-abc",
			AccountID: acct.ID,
		},
	}
}

func testAccount() *db.Account {
	return &db.Account{
		ID:       42,
		Username: "octocat",
		Role:     "user",
	}
}

// formActionRe extracts the form action URL and the SAMLResponse hidden input
// from an auto-POST page.
var (
	formActionRe   = regexp.MustCompile(`<form method="post" action="([^"]*)">`)
	samlResponseRe = regexp.MustCompile(`name="SAMLResponse" value="([^"]*)"`)
	relayStateRe   = regexp.MustCompile(`name="RelayState" value="([^"]*)"`)
)

func decodeAutoPost(t *testing.T, body string) (action string, samlResponse []byte) {
	t.Helper()
	am := formActionRe.FindStringSubmatch(body)
	if am == nil {
		t.Fatalf("no form action found in auto-POST body:\n%s", body)
	}
	sm := samlResponseRe.FindStringSubmatch(body)
	if sm == nil {
		t.Fatalf("no SAMLResponse input found in auto-POST body:\n%s", body)
	}
	// html/template escapes & as &amp; etc.; the base64 alphabet has none of
	// those, but unescape defensively in case padding/whitespace appears.
	b64 := strings.ReplaceAll(sm[1], "&#43;", "+")
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("decode SAMLResponse base64 %q: %v", b64, err)
	}
	return am[1], raw
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestSSONoSessionRedirectsToLogin(t *testing.T) {
	h := newSSOHarness(t, ssoSP())
	req := h.request(t, "_sso-nosession", nil) // no session
	rec := httptest.NewRecorder()

	h.idp.HandleSSO(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	wantPrefix := testIdPOrigin + "/login?return_to="
	if !strings.HasPrefix(loc, wantPrefix) {
		t.Fatalf("Location = %q, want prefix %q", loc, wantPrefix)
	}
	// The return_to must point back at our own SSO endpoint, not the SP.
	rt := strings.TrimPrefix(loc, wantPrefix)
	decoded, err := url.QueryUnescape(rt)
	if err != nil {
		t.Fatalf("unescape return_to: %v", err)
	}
	if !strings.HasPrefix(decoded, testSSOURL) {
		t.Errorf("return_to = %q, want it to start with %q", decoded, testSSOURL)
	}
}

func TestSSODisabledSentinelRedirectsToLogin(t *testing.T) {
	h := newSSOHarness(t, ssoSP())

	// Two flavours of the disabled sentinel must both bounce to login WITHOUT
	// panicking on a sess.Data deref.
	cases := []struct {
		name string
		sess *authn.Session
	}{
		{
			name: "nil Data sentinel",
			sess: &authn.Session{Account: testAccount(), Data: nil},
		},
		{
			name: "Account.Disabled",
			sess: func() *authn.Session {
				a := testAccount()
				a.Disabled = true
				return &authn.Session{Account: a, Data: &authn.SessionData{SessionID: "x", AccountID: a.ID}}
			}(),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := h.request(t, "_sso-disabled-"+tc.name, tc.sess)
			rec := httptest.NewRecorder()
			h.idp.HandleSSO(rec, req) // must not panic
			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302 login bounce; body=%s", rec.Code, rec.Body.String())
			}
			if !strings.HasPrefix(rec.Header().Get("Location"), testIdPOrigin+"/login?return_to=") {
				t.Fatalf("Location = %q, want login bounce", rec.Header().Get("Location"))
			}
		})
	}
}

func TestSSOIsPassiveNoSessionNoPassiveResponse(t *testing.T) {
	h := newSSOHarness(t, ssoSP())
	req := h.request(t, "_sso-passive", nil, func(o *authnReqOpts) {
		o.isPassive = true
	})
	rec := httptest.NewRecorder()

	h.idp.HandleSSO(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 NoPassive POST; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	action, respXML := decodeAutoPost(t, rec.Body.String())
	if action != testACSURL {
		t.Errorf("form action = %q, want ACS %q", action, testACSURL)
	}

	// The NoPassive Response is signed by buildStatusResponse; reparse the wire
	// bytes and verify the Response signature (mirrors TestSSOHappyPath) so the
	// status-response signing path is actually exercised end-to-end.
	doc, err := parseXMLSecure(respXML)
	if err != nil {
		t.Fatalf("parseXMLSecure(respXML): %v", err)
	}
	responseEl := doc.Root()
	if responseEl == nil || responseEl.Tag != "Response" {
		t.Fatalf("root element is not Response: %+v", responseEl)
	}
	if err := verifyElementSignature(responseEl, h.idpCert); err != nil {
		t.Errorf("NoPassive Response signature did not verify: %v", err)
	}

	var resp crewjam.Response
	if err := xml.Unmarshal(respXML, &resp); err != nil {
		t.Fatalf("unmarshal NoPassive Response: %v", err)
	}
	if resp.Assertion != nil {
		t.Error("NoPassive Response must NOT carry an Assertion")
	}
	if resp.Status.StatusCode.Value != statusResponder {
		t.Errorf("top StatusCode = %q, want %q", resp.Status.StatusCode.Value, statusResponder)
	}
	if resp.Status.StatusCode.StatusCode == nil || resp.Status.StatusCode.StatusCode.Value != statusNoPassive {
		t.Errorf("sub StatusCode = %+v, want %q", resp.Status.StatusCode.StatusCode, statusNoPassive)
	}
	if resp.InResponseTo != "_sso-passive" {
		t.Errorf("InResponseTo = %q, want _sso-passive", resp.InResponseTo)
	}
}

func TestSSOHappyPath(t *testing.T) {
	h := newSSOHarness(t, ssoSP())
	acct := testAccount()
	sess := liveSession(acct)
	req := h.request(t, "_sso-happy", sess, func(o *authnReqOpts) {
		o.relayState = "relay-123"
		o.hasRelay = true
	})
	rec := httptest.NewRecorder()

	h.idp.HandleSSO(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	action, respXML := decodeAutoPost(t, body)
	if action != testACSURL {
		t.Errorf("form action = %q, want %q", action, testACSURL)
	}
	if !relayStateRe.MatchString(body) {
		t.Error("expected a RelayState hidden input in the auto-POST form")
	}

	// (a) Our own verifier accepts BOTH the Response and the embedded Assertion
	// after reparsing the wire bytes (signElement output must round-trip first).
	doc, err := parseXMLSecure(respXML)
	if err != nil {
		t.Fatalf("parseXMLSecure(respXML): %v", err)
	}
	responseEl := doc.Root()
	if responseEl == nil || responseEl.Tag != "Response" {
		t.Fatalf("root element is not Response: %+v", responseEl)
	}
	if err := verifyElementSignature(responseEl, h.idpCert); err != nil {
		t.Errorf("Response signature did not verify: %v", err)
	}
	assertionEl := childByLocalName(responseEl, "Assertion")
	if assertionEl == nil {
		t.Fatal("no Assertion child in happy-path Response")
	}
	if err := verifyElementSignature(assertionEl, h.idpCert); err != nil {
		t.Errorf("Assertion signature did not verify: %v", err)
	}

	// (b) crewjam SP-side parse accepts the Response and recovers the NameID.
	idpMetaXML, err := h.idp.idpMetadata(context.Background())
	if err != nil {
		t.Fatalf("idpMetadata: %v", err)
	}
	var idpED crewjam.EntityDescriptor
	if err := xml.Unmarshal(idpMetaXML, &idpED); err != nil {
		t.Fatalf("unmarshal IdP metadata: %v", err)
	}
	acsParsed, _ := url.Parse(testACSURL)
	spProvider := crewjam.ServiceProvider{
		EntityID:    testSPEntityID,
		AcsURL:      *acsParsed,
		IDPMetadata: &idpED,
	}
	assertion, err := spProvider.ParseXMLResponse(respXML, []string{"_sso-happy"}, *acsParsed)
	if err != nil {
		t.Fatalf("crewjam ParseXMLResponse rejected our Response: %v", err)
	}
	if assertion.Subject == nil || assertion.Subject.NameID == nil {
		t.Fatal("parsed assertion has no Subject/NameID")
	}
	firstNameID := assertion.Subject.NameID.Value
	if firstNameID == "" {
		t.Fatal("NameID is empty")
	}

	// (c) a saml_session row was persisted.
	rows := h.q.sessions()
	if len(rows) != 1 {
		t.Fatalf("saml_session rows = %d, want 1", len(rows))
	}
	if rows[0].SessionID != "sess-abc" || rows[0].SpID != 7 || rows[0].NameID != firstNameID {
		t.Errorf("persisted saml_session = %+v, want SessionID=sess-abc SpID=7 NameID=%q", rows[0], firstNameID)
	}
	if !rows[0].NotOnOrAfter.Valid {
		t.Error("saml_session NotOnOrAfter must be Valid")
	}

	// (d) an audit record with FactorSAMLSP was emitted.
	recs := h.auditW.all()
	if len(recs) != 1 {
		t.Fatalf("audit records = %d, want 1", len(recs))
	}
	if recs[0].Factor != audit.FactorSAMLSP || recs[0].Event != audit.EventUse {
		t.Errorf("audit record = factor %q event %q, want %q/%q", recs[0].Factor, recs[0].Event, audit.FactorSAMLSP, audit.EventUse)
	}
	if recs[0].AccountID == nil || *recs[0].AccountID != acct.ID {
		t.Errorf("audit AccountID = %v, want %d", recs[0].AccountID, acct.ID)
	}

	// NameID is STABLE across a second SSO (different request ID, same account+SP).
	sess2 := liveSession(acct)
	req2 := h.request(t, "_sso-happy-2", sess2)
	rec2 := httptest.NewRecorder()
	h.idp.HandleSSO(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second SSO status = %d, want 200; body=%s", rec2.Code, rec2.Body.String())
	}
	_, respXML2 := decodeAutoPost(t, rec2.Body.String())
	assertion2, err := spProvider.ParseXMLResponse(respXML2, []string{"_sso-happy-2"}, *acsParsed)
	if err != nil {
		t.Fatalf("second ParseXMLResponse: %v", err)
	}
	if assertion2.Subject.NameID.Value != firstNameID {
		t.Errorf("NameID not stable: first=%q second=%q", firstNameID, assertion2.Subject.NameID.Value)
	}
}

func TestSSOForceAuthnBouncesToLogin(t *testing.T) {
	// ForceAuthn=true + a valid session but NO &reauth= nonce → 302 to /login
	// with a freshly-minted reauth nonce, and NO assertion issued.
	h := newSSOHarness(t, ssoSP())
	sess := liveSession(testAccount())
	req := h.request(t, "_sso-force", sess, func(o *authnReqOpts) {
		o.forceAuthn = true
		// Carry a RelayState so the inbound query has >1 SP-signed param; the
		// bounce must preserve them byte-for-byte (redirect-binding signature).
		o.hasRelay = true
		o.relayState = "rs-force-123"
	})
	// Capture the EXACT raw SP-signed query the SP put on the wire (the redirect
	// binding signs these raw octets); the bounce must echo them verbatim.
	inboundRaw := req.URL.RawQuery
	rec := httptest.NewRecorder()

	h.idp.HandleSSO(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 login bounce; body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	wantPrefix := testIdPOrigin + "/login?return_to="
	if !strings.HasPrefix(loc, wantPrefix) {
		t.Fatalf("Location = %q, want prefix %q", loc, wantPrefix)
	}
	rt, err := url.QueryUnescape(strings.TrimPrefix(loc, wantPrefix))
	if err != nil {
		t.Fatalf("unescape return_to: %v", err)
	}
	if !strings.HasPrefix(rt, testSSOURL) {
		t.Errorf("return_to = %q, want it to start with %q", rt, testSSOURL)
	}
	// The SP-signed params (SAMLRequest, RelayState) must survive the bounce
	// BYTE-IDENTICAL — re-encoding them would break the SP's redirect-binding
	// signature on the return trip. The return_to's raw query is the inbound raw
	// query with the reauth nonce appended, so the inbound raw octets must appear
	// verbatim as a prefix of the return_to query.
	_, rtQuery, _ := strings.Cut(rt, "?")
	if !strings.HasPrefix(rtQuery, inboundRaw+"&reauth=") {
		t.Errorf("return_to query = %q, want it to start with the verbatim SP-signed query %q followed by &reauth=", rtQuery, inboundRaw)
	}
	// The return_to must carry a single-use reauth nonce so the return trip can
	// satisfy the demand.
	rtu, err := url.Parse(rt)
	if err != nil {
		t.Fatalf("parse return_to: %v", err)
	}
	if rtu.Query().Get("reauth") == "" {
		t.Errorf("return_to %q missing &reauth= nonce", rt)
	}
	// No assertion was issued (no saml_session row, no auto-POST body).
	if rows := h.q.sessions(); len(rows) != 0 {
		t.Errorf("saml_session rows = %d, want 0 (no assertion issued on bounce)", len(rows))
	}
}

func TestSSOForceAuthnStaleSessionRebounces(t *testing.T) {
	// A session whose authTime PREDATES the demand must NOT satisfy ForceAuthn,
	// even when a (matching) reauth nonce is presented. Expect a re-bounce.
	h := newSSOHarness(t, ssoSP())
	ctx := context.Background()

	// Demand a re-auth NOW (the marker timestamp is "now").
	nonce, err := authn.DemandReauth(ctx, h.idp.kv, "saml:reauth:", testAccount().ID)
	if err != nil {
		t.Fatalf("DemandReauth: %v", err)
	}
	// The session's authTime is BEFORE the demand → stale.
	h.q.authTime = time.Now().Add(-time.Hour)

	sess := liveSession(testAccount())
	req := h.request(t, "_sso-force-stale", sess, func(o *authnReqOpts) {
		o.forceAuthn = true
	})
	// Present the demanded nonce on the URL.
	q := req.URL.Query()
	q.Set("reauth", nonce)
	req.URL.RawQuery = q.Encode()

	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 re-bounce (stale session); body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, testIdPOrigin+"/login?return_to=") {
		t.Fatalf("Location = %q, want login re-bounce", loc)
	}
	if rows := h.q.sessions(); len(rows) != 0 {
		t.Errorf("saml_session rows = %d, want 0 (stale session must not issue)", len(rows))
	}
}

func TestSSOForceAuthnFreshSessionIssues(t *testing.T) {
	// ForceAuthn=true + a fresh authTime (AFTER the demand) + a valid nonce →
	// the demand is satisfied and the assertion is issued (auto-POST).
	h := newSSOHarness(t, ssoSP())
	ctx := context.Background()

	nonce, err := authn.DemandReauth(ctx, h.idp.kv, "saml:reauth:", testAccount().ID)
	if err != nil {
		t.Fatalf("DemandReauth: %v", err)
	}
	// authTime post-dates the demand → fresh.
	h.q.authTime = time.Now().Add(time.Second)

	sess := liveSession(testAccount())
	req := h.request(t, "_sso-force-fresh", sess, func(o *authnReqOpts) {
		o.forceAuthn = true
	})
	q := req.URL.Query()
	q.Set("reauth", nonce)
	req.URL.RawQuery = q.Encode()

	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (assertion issued); body=%s", rec.Code, rec.Body.String())
	}
	action, respXML := decodeAutoPost(t, rec.Body.String())
	if action != testACSURL {
		t.Errorf("form action = %q, want ACS %q", action, testACSURL)
	}
	var resp crewjam.Response
	if err := xml.Unmarshal(respXML, &resp); err != nil {
		t.Fatalf("unmarshal Response: %v", err)
	}
	if resp.Assertion == nil {
		t.Error("fresh-session ForceAuthn must issue an Assertion")
	}
	if rows := h.q.sessions(); len(rows) != 1 {
		t.Errorf("saml_session rows = %d, want 1 (assertion issued)", len(rows))
	}
}

func TestSSOForceAuthnPassiveNoPassive(t *testing.T) {
	// ForceAuthn=true + IsPassive=true → IsPassive wins (OASIS): a terminal
	// NoPassive Response, no assertion. Even with a valid session.
	h := newSSOHarness(t, ssoSP())
	sess := liveSession(testAccount())
	req := h.request(t, "_sso-force-passive", sess, func(o *authnReqOpts) {
		o.forceAuthn = true
		o.isPassive = true
	})
	rec := httptest.NewRecorder()

	h.idp.HandleSSO(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 NoPassive POST; body=%s", rec.Code, rec.Body.String())
	}
	action, respXML := decodeAutoPost(t, rec.Body.String())
	if action != testACSURL {
		t.Errorf("form action = %q, want ACS %q", action, testACSURL)
	}

	// Verify the NoPassive Response is signed and carries the right status.
	doc, err := parseXMLSecure(respXML)
	if err != nil {
		t.Fatalf("parseXMLSecure(respXML): %v", err)
	}
	responseEl := doc.Root()
	if responseEl == nil || responseEl.Tag != "Response" {
		t.Fatalf("root element is not Response: %+v", responseEl)
	}
	if err := verifyElementSignature(responseEl, h.idpCert); err != nil {
		t.Errorf("NoPassive Response signature did not verify: %v", err)
	}

	var resp crewjam.Response
	if err := xml.Unmarshal(respXML, &resp); err != nil {
		t.Fatalf("unmarshal NoPassive Response: %v", err)
	}
	if resp.Assertion != nil {
		t.Error("ForceAuthn+IsPassive NoPassive Response must NOT carry an Assertion")
	}
	if resp.Status.StatusCode.Value != statusResponder {
		t.Errorf("top StatusCode = %q, want %q", resp.Status.StatusCode.Value, statusResponder)
	}
	if resp.Status.StatusCode.StatusCode == nil || resp.Status.StatusCode.StatusCode.Value != statusNoPassive {
		t.Errorf("sub StatusCode = %+v, want %q", resp.Status.StatusCode.StatusCode, statusNoPassive)
	}
	// No assertion was issued.
	if rows := h.q.sessions(); len(rows) != 0 {
		t.Errorf("saml_session rows = %d, want 0 (NoPassive issues no assertion)", len(rows))
	}
}

func TestSSOReplayRejected(t *testing.T) {
	h := newSSOHarness(t, ssoSP())
	acct := testAccount()

	// First issue with a given request ID succeeds.
	req1 := h.request(t, "_sso-replay", liveSession(acct))
	rec1 := httptest.NewRecorder()
	h.idp.HandleSSO(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first SSO status = %d, want 200; body=%s", rec1.Code, rec1.Body.String())
	}

	// Re-presenting the SAME request ID (shared KV) is rejected at consume.
	req2 := h.request(t, "_sso-replay", liveSession(acct))
	rec2 := httptest.NewRecorder()
	h.idp.HandleSSO(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("replay status = %d, want 400; body=%s", rec2.Code, rec2.Body.String())
	}
	// And no second saml_session row should have been persisted.
	if rows := h.q.sessions(); len(rows) != 1 {
		t.Errorf("saml_session rows = %d after replay, want 1", len(rows))
	}
}

func TestSSOUnknownSPDirectError(t *testing.T) {
	h := newSSOHarness(t, ssoSP())
	// Re-point the fake's SP to a DIFFERENT entity ID so the issuer lookup misses.
	h.q.sp.EntityID = "https://someone-else.example/metadata"

	req := h.request(t, "_sso-unknownsp", liveSession(testAccount()))
	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 direct error; body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("unknown-SP error must NOT redirect; got Location=%q", loc)
	}
}

func TestSSONameIDPolicyUnproducibleFormatInvalidNameIDPolicy(t *testing.T) {
	// An SP requesting a concrete NameIDPolicy/@Format this IdP does NOT produce
	// for it (the SP is configured persistent-1.1, but it asks for emailAddress)
	// → a terminal Requester/InvalidNameIDPolicy Response with NO Assertion (D8).
	h := newSSOHarness(t, ssoSP())
	sess := liveSession(testAccount())
	req := h.request(t, "_sso-nameidpolicy-bad", sess, func(o *authnReqOpts) {
		o.nameIDFormat = "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress"
	})
	rec := httptest.NewRecorder()

	h.idp.HandleSSO(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 InvalidNameIDPolicy POST; body=%s", rec.Code, rec.Body.String())
	}
	action, respXML := decodeAutoPost(t, rec.Body.String())
	if action != testACSURL {
		t.Errorf("form action = %q, want ACS %q", action, testACSURL)
	}

	// The InvalidNameIDPolicy Response goes through the same
	// buildStatusResponse→signElement path as NoPassive, so it MUST be signed.
	doc, err := parseXMLSecure(respXML)
	if err != nil {
		t.Fatalf("parseXMLSecure(respXML): %v", err)
	}
	responseEl := doc.Root()
	if responseEl == nil || responseEl.Tag != "Response" {
		t.Fatalf("root element is not Response: %+v", responseEl)
	}
	if err := verifyElementSignature(responseEl, h.idpCert); err != nil {
		t.Errorf("InvalidNameIDPolicy Response signature did not verify: %v", err)
	}

	var resp crewjam.Response
	if err := xml.Unmarshal(respXML, &resp); err != nil {
		t.Fatalf("unmarshal Response: %v", err)
	}
	if resp.Assertion != nil {
		t.Error("InvalidNameIDPolicy Response must NOT carry an Assertion")
	}
	if resp.Status.StatusCode.Value != statusRequester {
		t.Errorf("top StatusCode = %q, want %q", resp.Status.StatusCode.Value, statusRequester)
	}
	if resp.Status.StatusCode.StatusCode == nil || resp.Status.StatusCode.StatusCode.Value != statusInvalidNameIDPolicy {
		t.Errorf("sub StatusCode = %+v, want %q", resp.Status.StatusCode.StatusCode, statusInvalidNameIDPolicy)
	}
	if resp.InResponseTo != "_sso-nameidpolicy-bad" {
		t.Errorf("InResponseTo = %q, want _sso-nameidpolicy-bad", resp.InResponseTo)
	}
	// No assertion issued → no saml_session row.
	if rows := h.q.sessions(); len(rows) != 0 {
		t.Errorf("saml_session rows = %d, want 0 (InvalidNameIDPolicy issues no assertion)", len(rows))
	}
}

func TestSSONameIDPolicyMatchingFormatIssues(t *testing.T) {
	// NameIDPolicy/@Format equal to the SP's configured format → normal assertion.
	h := newSSOHarness(t, ssoSP())
	sess := liveSession(testAccount())
	req := h.request(t, "_sso-nameidpolicy-match", sess, func(o *authnReqOpts) {
		o.nameIDFormat = "urn:oasis:names:tc:SAML:1.1:nameid-format:persistent" // == ssoSP().NameIDFormat
	})
	rec := httptest.NewRecorder()

	h.idp.HandleSSO(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (assertion issued); body=%s", rec.Code, rec.Body.String())
	}
	_, respXML := decodeAutoPost(t, rec.Body.String())
	var resp crewjam.Response
	if err := xml.Unmarshal(respXML, &resp); err != nil {
		t.Fatalf("unmarshal Response: %v", err)
	}
	if resp.Assertion == nil {
		t.Error("matching NameIDPolicy/@Format must issue an Assertion")
	}
	if rows := h.q.sessions(); len(rows) != 1 {
		t.Errorf("saml_session rows = %d, want 1 (assertion issued)", len(rows))
	}
}

func TestSSONameIDPolicyUnspecifiedIssues(t *testing.T) {
	// Format=unspecified is the SP's escape hatch ("IdP, you pick") → normal
	// assertion using the SP's configured format.
	h := newSSOHarness(t, ssoSP())
	sess := liveSession(testAccount())
	req := h.request(t, "_sso-nameidpolicy-unspec", sess, func(o *authnReqOpts) {
		o.nameIDFormat = "urn:oasis:names:tc:SAML:2.0:nameid-format:unspecified"
	})
	rec := httptest.NewRecorder()

	h.idp.HandleSSO(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (assertion issued); body=%s", rec.Code, rec.Body.String())
	}
	_, respXML := decodeAutoPost(t, rec.Body.String())
	var resp crewjam.Response
	if err := xml.Unmarshal(respXML, &resp); err != nil {
		t.Fatalf("unmarshal Response: %v", err)
	}
	if resp.Assertion == nil {
		t.Error("Format=unspecified must issue an Assertion")
	}
	if rows := h.q.sessions(); len(rows) != 1 {
		t.Errorf("saml_session rows = %d, want 1 (assertion issued)", len(rows))
	}
}

// TestSSOPostUnsignedRejected drives HandleSSO with a POST-binding UNSIGNED
// AuthnRequest for an SP that requires signed requests. The enveloped-signature
// gate surfaces errNoSignature, which ssoParseError must map to a 400 (NOT the
// default 500), and no SAMLResponse/assertion may be issued. This exercises the
// full handler→ssoParseError mapping, regression-guarding the bug where the
// POST-reachable signature/XML sentinels fell through to the 500 branch.
func TestSSOPostUnsignedRejected(t *testing.T) {
	h := newSSOHarness(t, ssoSP()) // ssoSP() has RequireSignedAuthnRequest=true
	// Build an UNSIGNED POST AuthnRequest (sign:false → signCertDER unused).
	req := buildAuthnPost(t, authnReqOpts{
		id:          "_sso-post-unsigned",
		destination: testSSOURL,
		acsURL:      testACSURL,
		sign:        false,
	}, nil)
	req.RemoteAddr = "203.0.113.7:54321"
	req.Header.Set("User-Agent", "test-agent/1.0")
	// Attach a live session: the signature gate must reject BEFORE any
	// session-driven assertion issuance, so this never reaches an auto-POST.
	req = req.WithContext(authn.WithSession(req.Context(), liveSession(testAccount())))
	rec := httptest.NewRecorder()

	h.idp.HandleSSO(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (unsigned POST rejected at signature gate); body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); samlResponseRe.MatchString(body) {
		t.Errorf("rejected request must not issue a SAMLResponse; body=%s", body)
	}
	if rows := h.q.sessions(); len(rows) != 0 {
		t.Errorf("saml_session rows = %d, want 0 (no assertion issued)", len(rows))
	}
}

// TestSSOPostDoctypeRejected drives HandleSSO with a POST-binding body whose XML
// carries a DOCTYPE. parseXMLSecure (run during decode, before the signature
// gate) surfaces errXMLDTD, which ssoParseError must map to a 400 rather than the
// default 500. Guards the XXE/DTD-rejection sentinel on the POST intake path.
func TestSSOPostDoctypeRejected(t *testing.T) {
	h := newSSOHarness(t, ssoSP())
	doctypeXML := `<?xml version="1.0"?><!DOCTYPE AuthnRequest [<!ENTITY x "y">]>` +
		`<AuthnRequest xmlns="urn:oasis:names:tc:SAML:2.0:protocol" ID="_sso-post-dtd" Version="2.0">` +
		`<Issuer xmlns="urn:oasis:names:tc:SAML:2.0:assertion">` + testSPEntityID + `</Issuer></AuthnRequest>`
	form := url.Values{}
	form.Set("SAMLRequest", base64.StdEncoding.EncodeToString([]byte(doctypeXML)))
	req := httptest.NewRequest(http.MethodPost, testSSOURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(authn.WithSession(req.Context(), liveSession(testAccount())))
	rec := httptest.NewRecorder()

	h.idp.HandleSSO(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (DOCTYPE body rejected); body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); samlResponseRe.MatchString(body) {
		t.Errorf("rejected request must not issue a SAMLResponse; body=%s", body)
	}
}
