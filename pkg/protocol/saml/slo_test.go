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
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beevik/etree"
	crewjam "github.com/crewjam/saml"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
	"prohibitorum/pkg/session"
)

const testSLOURL = testIdPOrigin + "/saml/slo"
const testSPSLOLocation = "https://sp.example.test/saml/slo"

// ---------------------------------------------------------------------------
// SLO test harness
//
// RevokeBySessionID is a concrete method on *session.SessionStore (not an
// interface the IdP holds), so we wire a REAL SessionStore over a memory KV and
// a fake SessionQueries, and seed it via Issue. That exercises the real
// revoke-by-session-id path: after a successful SLO the session must be gone
// from the store.
// ---------------------------------------------------------------------------

// fakeSLOQueries serves the SLO handler's query surface from memory.
type fakeSLOQueries struct {
	db.Querier

	sp      db.SamlSp
	spKeys  []db.SamlSpKey
	idpKeys []db.SigningKey

	mu       sync.Mutex
	sessions []db.SamlSession      // saml_session rows, keyed by (sp,nameID)
	pgSess   map[string]db.Session // sessionID -> session (for GetSession)
	deleted  []string              // sessionIDs passed to DeleteSAMLSessionsBySession
}

func (f *fakeSLOQueries) GetSAMLSPByEntityID(_ context.Context, entityID string) (db.SamlSp, error) {
	if entityID == f.sp.EntityID {
		return f.sp, nil
	}
	return db.SamlSp{}, pgx.ErrNoRows
}

func (f *fakeSLOQueries) ListSAMLSPKeys(_ context.Context, _ db.ListSAMLSPKeysParams) ([]db.SamlSpKey, error) {
	return f.spKeys, nil
}

func (f *fakeSLOQueries) ListPublishableSigningKeys(context.Context) ([]db.SigningKey, error) {
	return f.idpKeys, nil
}

func (f *fakeSLOQueries) ListSAMLSessionsByNameID(_ context.Context, arg db.ListSAMLSessionsByNameIDParams) ([]db.SamlSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []db.SamlSession
	for _, s := range f.sessions {
		if s.SpID == arg.SpID && s.NameID == arg.NameID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeSLOQueries) GetSession(_ context.Context, id string) (db.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.pgSess[id]; ok {
		return s, nil
	}
	return db.Session{}, pgx.ErrNoRows
}

func (f *fakeSLOQueries) DeleteSAMLSessionsBySession(_ context.Context, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, sessionID)
	kept := f.sessions[:0]
	for _, s := range f.sessions {
		if s.SessionID != sessionID {
			kept = append(kept, s)
		}
	}
	f.sessions = kept
	return nil
}

func (f *fakeSLOQueries) deletedRows() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deleted...)
}

// fakeSessionQueries is the minimal session.SessionQueries stub: it satisfies
// the interface so a real SessionStore can Issue/Revoke over the memory KV.
type fakeSessionQueries struct{}

func (fakeSessionQueries) InsertSession(_ context.Context, arg db.InsertSessionParams) (db.Session, error) {
	return db.Session{ID: arg.ID, AccountID: arg.AccountID}, nil
}
func (fakeSessionQueries) RevokeSession(context.Context, string) error             { return nil }
func (fakeSessionQueries) RevokeAllSessionsByAccount(context.Context, int32) error { return nil }

// sloHarness bundles the IdP under test for SLO scenarios.
type sloHarness struct {
	idp     *IdP
	q       *fakeSLOQueries
	auditW  *recordingAudit
	store   *session.SessionStore
	idpCert *x509.Certificate
	spKey   *rsa.PrivateKey
	spCert  *x509.Certificate
}

func sloSP() db.SamlSp {
	// Registered WITH metadata advertising redirect + POST SLO endpoints so the
	// LogoutResponse target is derivable.
	meta := `<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="` + testSPEntityID + `">
  <SPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <SingleLogoutService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="` + testSPSLOLocation + `"/>
    <SingleLogoutService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" Location="` + testSPSLOLocation + `"/>
    <AssertionConsumerService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" Location="` + testACSURL + `" index="0" isDefault="true"/>
  </SPSSODescriptor>
</EntityDescriptor>`
	return db.SamlSp{
		ID:           7,
		EntityID:     testSPEntityID,
		NameIDFormat: "urn:oasis:names:tc:SAML:1.1:nameid-format:persistent",
		MetadataXml:  pgtype.Text{String: meta, Valid: true},
	}
}

func newSLOHarness(t *testing.T, sp db.SamlSp) *sloHarness {
	t.Helper()

	idpRow, _, idpCert := testSAMLSigningKeyRow(t)
	spKey, spCertPEM := testSPKey(t)
	spCert, err := parseCertPEM(spCertPEM)
	if err != nil {
		t.Fatalf("parse sp cert: %v", err)
	}

	q := &fakeSLOQueries{
		sp:      sp,
		spKeys:  []db.SamlSpKey{{ID: 1, SpID: sp.ID, Use: "signing", CertPem: spCertPEM, NotAfter: pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true}}},
		idpKeys: []db.SigningKey{idpRow},
		pgSess:  map[string]db.Session{},
	}

	memKV := kv.NewMemoryStore()
	store := session.NewSessionStore(memKV, fakeSessionQueries{}, time.Hour)

	cfg := &configx.Config{PublicOrigins: []string{testIdPOrigin}}
	idp := &IdP{
		cfg:      cfg,
		queries:  q,
		kv:       kv.NewMemoryStore(),
		sessions: store,
		audit:    &recordingAudit{},
		keys:     newSAMLKeyCache(q),
	}
	auditW := idp.audit.(*recordingAudit)

	return &sloHarness{idp: idp, q: q, auditW: auditW, store: store, idpCert: idpCert, spKey: spKey, spCert: spCert}
}

// seedSession issues a real session for accountID into the store and records the
// matching saml_session + pg session rows. Returns the live SessionID.
func (h *sloHarness) seedSession(t *testing.T, accountID int32, nameID, sessionIndex string) string {
	t.Helper()
	_, data, err := h.store.Issue(context.Background(), accountID, "203.0.113.9", "ua/1.0", []string{"pwd", "otp", "mfa"}, nil)
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
	sid := data.SessionID
	h.q.mu.Lock()
	h.q.sessions = append(h.q.sessions, db.SamlSession{
		ID: int64(len(h.q.sessions) + 1), SessionID: sid, SpID: h.q.sp.ID, NameID: nameID, SessionIndex: sessionIndex,
		NotOnOrAfter: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	})
	h.q.pgSess[sid] = db.Session{ID: sid, AccountID: accountID}
	h.q.mu.Unlock()
	return sid
}

// sessionAlive reports whether a session with sessionID still lives in the store
// for accountID.
func (h *sloHarness) sessionAlive(t *testing.T, accountID int32, sessionID string) bool {
	t.Helper()
	recs, err := h.store.ListByAccount(context.Background(), accountID)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	for _, r := range recs {
		if r.Data.SessionID == sessionID {
			return true
		}
	}
	return false
}

// sloReqOpts parameterizes the LogoutRequest builder.
type sloReqOpts struct {
	id           string
	destination  string
	nameID       string
	sessionIndex string
	notOnOrAfter *time.Time
	version      string // override LogoutRequest @Version (empty = "2.0")

	relayState string
	hasRelay   bool

	// signing
	sign    bool
	signKey *rsa.PrivateKey
}

// buildLogoutRedirect builds a redirect-binding (GET) signed LogoutRequest. The
// detached signature octet string mirrors verifyRedirectSignature byte-for-byte.
func buildLogoutRedirect(t *testing.T, o sloReqOpts) *http.Request {
	t.Helper()
	version := o.version
	if version == "" {
		version = "2.0"
	}
	lr := crewjam.LogoutRequest{
		ID:           o.id,
		Version:      version,
		IssueInstant: time.Now().UTC(),
		Destination:  o.destination,
		Issuer:       &crewjam.Issuer{Value: testSPEntityID},
		NameID:       &crewjam.NameID{Value: o.nameID},
		NotOnOrAfter: o.notOnOrAfter,
	}
	if o.sessionIndex != "" {
		lr.SessionIndex = &crewjam.SessionIndex{Value: o.sessionIndex}
	}
	// crewjam's LogoutRequest.MarshalXML panics on a nil *RelaxedTime
	// (NotOnOrAfter), so serialize via Element() instead — which is also closer
	// to how a real SP emits the request.
	doc := etree.NewDocument()
	doc.SetRoot(lr.Element())
	xmlBytes, err := doc.WriteToBytes()
	if err != nil {
		t.Fatalf("serialize logoutrequest: %v", err)
	}
	var deflated bytes.Buffer
	fw, _ := flate.NewWriter(&deflated, flate.DefaultCompression)
	if _, err := fw.Write(xmlBytes); err != nil {
		t.Fatalf("deflate: %v", err)
	}
	if err := fw.Close(); err != nil {
		t.Fatalf("deflate close: %v", err)
	}
	samlRequest := base64.StdEncoding.EncodeToString(deflated.Bytes())

	encReq := url.QueryEscape(samlRequest)
	rawQuery := "SAMLRequest=" + encReq
	if o.hasRelay {
		rawQuery += "&RelayState=" + url.QueryEscape(o.relayState)
	}
	if o.sign {
		encSigAlg := url.QueryEscape(rsaSHA256SigAlg)
		signed := "SAMLRequest=" + encReq
		if o.hasRelay {
			signed += "&RelayState=" + url.QueryEscape(o.relayState)
		}
		signed += "&SigAlg=" + encSigAlg
		if o.signKey == nil {
			t.Fatal("buildLogoutRedirect: sign=true requires signKey")
		}
		h := sha256.Sum256([]byte(signed))
		sigBytes, serr := rsa.SignPKCS1v15(nil, o.signKey, crypto.SHA256, h[:])
		if serr != nil {
			t.Fatalf("sign: %v", serr)
		}
		rawQuery += "&SigAlg=" + encSigAlg
		rawQuery += "&Signature=" + url.QueryEscape(base64.StdEncoding.EncodeToString(sigBytes))
	}
	req := &http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{Scheme: "https", Host: "idp.example.test", Path: "/saml/slo", RawQuery: rawQuery},
	}
	req.RemoteAddr = "203.0.113.9:5555"
	req.Header = http.Header{"User-Agent": []string{"test-agent/1.0"}}
	return req
}

// buildLogoutPost builds a POST-binding LogoutRequest with an ENVELOPED
// signature over the LogoutRequest element (signed with signKey/cert), then
// base64.StdEncoding (no deflate) as the SAMLRequest form value.
func buildLogoutPost(t *testing.T, o sloReqOpts, signCertDER []byte) *http.Request {
	t.Helper()
	id := o.id
	lr := crewjam.LogoutRequest{
		ID:           id,
		Version:      "2.0",
		IssueInstant: time.Now().UTC(),
		Destination:  o.destination,
		Issuer:       &crewjam.Issuer{Value: testSPEntityID},
		NameID:       &crewjam.NameID{Value: o.nameID},
		NotOnOrAfter: o.notOnOrAfter,
	}
	if o.sessionIndex != "" {
		lr.SessionIndex = &crewjam.SessionIndex{Value: o.sessionIndex}
	}
	el := lr.Element() // crewjam's Element() already sets the SAML "ID" attribute.

	signed, err := signElement(el, o.signKey, signCertDER)
	if err != nil {
		t.Fatalf("sign logoutrequest element: %v", err)
	}
	doc := etree.NewDocument()
	doc.SetRoot(signed)
	xmlBytes, err := doc.WriteToBytes()
	if err != nil {
		t.Fatalf("serialize signed logoutrequest: %v", err)
	}
	form := url.Values{}
	form.Set("SAMLRequest", base64.StdEncoding.EncodeToString(xmlBytes))
	if o.hasRelay {
		form.Set("RelayState", o.relayState)
	}
	req := httptest.NewRequest(http.MethodPost, testSLOURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "203.0.113.9:5555"
	req.Header.Set("User-Agent", "test-agent/1.0")
	return req
}

// mustParseQuery parses a redirect Location URL and returns its query values.
func mustParseQuery(t *testing.T, location string) url.Values {
	t.Helper()
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse Location %q: %v", location, err)
	}
	return u.Query()
}

// decodeRedirectLogoutResponse pulls the LogoutResponse XML out of a 302
// Location's SAMLResponse param (base64 → inflate).
func decodeRedirectLogoutResponse(t *testing.T, location string) []byte {
	t.Helper()
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	enc := u.Query().Get("SAMLResponse")
	if enc == "" {
		t.Fatalf("no SAMLResponse in Location %q", location)
	}
	deflated, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatalf("decode SAMLResponse: %v", err)
	}
	fr := flate.NewReader(bytes.NewReader(deflated))
	raw, err := io.ReadAll(fr)
	_ = fr.Close()
	if err != nil {
		t.Fatalf("inflate SAMLResponse: %v", err)
	}
	return raw
}

// assertLogoutResponseValid reparses the response XML, verifies our signature,
// and checks Status==Success + InResponseTo.
func assertLogoutResponseValid(t *testing.T, h *sloHarness, respXML []byte, wantInResponseTo string) {
	t.Helper()
	doc, err := parseXMLSecure(respXML)
	if err != nil {
		t.Fatalf("parseXMLSecure(LogoutResponse): %v\n%s", err, respXML)
	}
	root := doc.Root()
	if root == nil || root.Tag != "LogoutResponse" {
		t.Fatalf("root is not LogoutResponse: %+v", root)
	}
	if err := verifyElementSignature(root, h.idpCert); err != nil {
		t.Errorf("LogoutResponse signature did not verify: %v", err)
	}
	var resp crewjam.LogoutResponse
	if err := xml.Unmarshal(respXML, &resp); err != nil {
		t.Fatalf("unmarshal LogoutResponse: %v", err)
	}
	if resp.Status.StatusCode.Value != statusSuccess {
		t.Errorf("Status = %q, want Success", resp.Status.StatusCode.Value)
	}
	if resp.InResponseTo != wantInResponseTo {
		t.Errorf("InResponseTo = %q, want %q", resp.InResponseTo, wantInResponseTo)
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestSLOValidRedirectRevokesSession(t *testing.T) {
	h := newSLOHarness(t, sloSP())
	const nameID = "user-nameid-abc"
	sid := h.seedSession(t, 42, nameID, "")

	req := buildLogoutRedirect(t, sloReqOpts{
		id:          "_slo-ok",
		destination: testSLOURL,
		nameID:      nameID,
		hasRelay:    true,
		relayState:  "state-token",
		sign:        true,
		signKey:     h.spKey,
	})
	rec := httptest.NewRecorder()
	h.idp.HandleSLO(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 redirect; body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, testSPSLOLocation) {
		t.Fatalf("Location = %q, want prefix %q", loc, testSPSLOLocation)
	}
	// The LogoutResponse redirect must echo the inbound RelayState per the SAML
	// HTTP-Redirect binding.
	if got := mustParseQuery(t, loc).Get("RelayState"); got != "state-token" {
		t.Errorf("RelayState in Location = %q, want %q", got, "state-token")
	}

	// Session revoked in the real store.
	if h.sessionAlive(t, 42, sid) {
		t.Error("session is still alive; expected it revoked")
	}
	// saml_session row deleted.
	if del := h.q.deletedRows(); len(del) != 1 || del[0] != sid {
		t.Errorf("deleted rows = %v, want [%s]", del, sid)
	}
	// Audit logout record emitted.
	recs := h.auditW.all()
	if len(recs) != 1 {
		t.Fatalf("audit records = %d, want 1", len(recs))
	}
	if recs[0].Factor != audit.FactorSAMLSP || recs[0].Event != audit.EventSessionEnd {
		t.Errorf("audit = %q/%q, want %q/%q", recs[0].Factor, recs[0].Event, audit.FactorSAMLSP, audit.EventSessionEnd)
	}
	if recs[0].AccountID == nil || *recs[0].AccountID != 42 {
		t.Errorf("audit AccountID = %v, want 42", recs[0].AccountID)
	}
	// Signed LogoutResponse returned.
	respXML := decodeRedirectLogoutResponse(t, loc)
	assertLogoutResponseValid(t, h, respXML, "_slo-ok")
}

// TestSLOAlreadyRevokedSessionCleansOrphanRow proves Fix C1+C4: a saml_session
// binding whose underlying IdP session is ALREADY revoked/expired (GetSession
// returns pgx.ErrNoRows) must still produce a signed Success LogoutResponse AND
// delete the orphan binding row — and must NOT be reported as a partial failure
// (the already-gone session is a benign idempotent outcome, not a hard error).
func TestSLOAlreadyRevokedSessionCleansOrphanRow(t *testing.T) {
	h := newSLOHarness(t, sloSP())
	const nameID = "user-nameid-orphan"
	sid := h.seedSession(t, 42, nameID, "")

	// Simulate the session already being revoked: GetSession filters
	// revoked_at IS NULL, so a revoked session returns no row. Drop it from the
	// fake's pgSess map (leaving the saml_session binding row in place) to
	// reproduce the orphan condition.
	h.q.mu.Lock()
	delete(h.q.pgSess, sid)
	h.q.mu.Unlock()

	req := buildLogoutRedirect(t, sloReqOpts{
		id:          "_slo-orphan",
		destination: testSLOURL,
		nameID:      nameID,
		sign:        true,
		signKey:     h.spKey,
	})
	rec := httptest.NewRecorder()
	h.idp.HandleSLO(rec, req)

	// Still a signed Success LogoutResponse.
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 redirect; body=%s", rec.Code, rec.Body.String())
	}
	respXML := decodeRedirectLogoutResponse(t, rec.Header().Get("Location"))
	assertLogoutResponseValid(t, h, respXML, "_slo-orphan")

	// The orphan binding row was deleted (the bug left it forever).
	if del := h.q.deletedRows(); len(del) != 1 || del[0] != sid {
		t.Errorf("deleted rows = %v, want [%s] (orphan row must be cleaned)", del, sid)
	}

	// The already-revoked session is benign: it must NOT be stamped as a partial
	// failure. (The no-live-session path emits no audit record at all, since
	// haveAcctID stays false — assert no spurious "partial" record exists.)
	for _, r := range h.auditW.all() {
		if p, ok := r.Detail["partial"]; ok && p == true {
			t.Errorf("audit marked partial=true on a benign already-revoked session")
		}
	}
}

func TestSLOBadSignatureLeavesSessionUntouched(t *testing.T) {
	h := newSLOHarness(t, sloSP())
	const nameID = "user-nameid-badsig"
	sid := h.seedSession(t, 42, nameID, "")

	wrongKey, _ := testSPKey(t) // different key than the SP's registered cert
	req := buildLogoutRedirect(t, sloReqOpts{
		id:          "_slo-badsig",
		destination: testSLOURL,
		nameID:      nameID,
		sign:        true,
		signKey:     wrongKey,
	})
	rec := httptest.NewRecorder()
	h.idp.HandleSLO(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !h.sessionAlive(t, 42, sid) {
		t.Error("session was revoked on a bad signature; MUST remain untouched")
	}
	if del := h.q.deletedRows(); len(del) != 0 {
		t.Errorf("deleted rows = %v on bad signature, want none", del)
	}
	if recs := h.auditW.all(); len(recs) != 0 {
		t.Errorf("audit records = %d on bad signature, want 0", len(recs))
	}
}

func TestSLOAbsentSignatureRejected(t *testing.T) {
	h := newSLOHarness(t, sloSP())
	const nameID = "user-nameid-nosig"
	sid := h.seedSession(t, 42, nameID, "")

	req := buildLogoutRedirect(t, sloReqOpts{
		id:          "_slo-nosig",
		destination: testSLOURL,
		nameID:      nameID,
		sign:        false,
	})
	rec := httptest.NewRecorder()
	h.idp.HandleSLO(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !h.sessionAlive(t, 42, sid) {
		t.Error("session revoked despite absent signature; MUST remain untouched")
	}
	if del := h.q.deletedRows(); len(del) != 0 {
		t.Errorf("deleted rows = %v, want none", del)
	}
}

func TestSLONoSessionStillSuccess(t *testing.T) {
	h := newSLOHarness(t, sloSP())
	// No seeded session: NameID resolves to nothing → idempotent Success.
	req := buildLogoutRedirect(t, sloReqOpts{
		id:          "_slo-nosession",
		destination: testSLOURL,
		nameID:      "nobody-here",
		sign:        true,
		signKey:     h.spKey,
	})
	rec := httptest.NewRecorder()
	h.idp.HandleSLO(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body=%s", rec.Code, rec.Body.String())
	}
	if del := h.q.deletedRows(); len(del) != 0 {
		t.Errorf("deleted rows = %v, want none (no session to revoke)", del)
	}
	// The no-op path emits NO audit record (an accountless logout would mislead).
	if recs := h.auditW.all(); len(recs) != 0 {
		t.Errorf("audit records = %d on no-session no-op, want 0", len(recs))
	}
	respXML := decodeRedirectLogoutResponse(t, rec.Header().Get("Location"))
	assertLogoutResponseValid(t, h, respXML, "_slo-nosession")
}

func TestSLOPostBindingRevokesSession(t *testing.T) {
	h := newSLOHarness(t, sloSP())
	const nameID = "user-nameid-post"
	sid := h.seedSession(t, 42, nameID, "")

	// Build the SP signing cert DER for the enveloped signature.
	spCertDER := h.spCert.Raw

	req := buildLogoutPost(t, sloReqOpts{
		id:          "_slo-post",
		destination: testSLOURL,
		nameID:      nameID,
		sign:        true,
		signKey:     h.spKey,
	}, spCertDER)
	rec := httptest.NewRecorder()
	h.idp.HandleSLO(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 auto-POST; body=%s", rec.Code, rec.Body.String())
	}
	if h.sessionAlive(t, 42, sid) {
		t.Error("session still alive after POST-binding SLO")
	}
	if del := h.q.deletedRows(); len(del) != 1 || del[0] != sid {
		t.Errorf("deleted rows = %v, want [%s]", del, sid)
	}
	action, respXML := decodeAutoPost(t, rec.Body.String())
	if action != testSPSLOLocation {
		t.Errorf("form action = %q, want %q", action, testSPSLOLocation)
	}
	assertLogoutResponseValid(t, h, respXML, "_slo-post")
}

func TestSLODestinationMismatchRejected(t *testing.T) {
	h := newSLOHarness(t, sloSP())
	const nameID = "user-nameid-dest"
	sid := h.seedSession(t, 42, nameID, "")

	req := buildLogoutRedirect(t, sloReqOpts{
		id:          "_slo-baddest",
		destination: "https://evil.example/saml/slo",
		nameID:      nameID,
		sign:        true,
		signKey:     h.spKey,
	})
	rec := httptest.NewRecorder()
	h.idp.HandleSLO(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !h.sessionAlive(t, 42, sid) {
		t.Error("session revoked despite bad Destination; MUST remain untouched")
	}
}

// TestSLOBadVersionRejected proves Fix B3: a LogoutRequest with Version != "2.0"
// is rejected as malformed (Core §3.2.1) with the session left untouched.
func TestSLOBadVersionRejected(t *testing.T) {
	h := newSLOHarness(t, sloSP())
	const nameID = "user-nameid-ver"
	sid := h.seedSession(t, 42, nameID, "")

	req := buildLogoutRedirect(t, sloReqOpts{
		id:          "_slo-badver",
		destination: testSLOURL,
		nameID:      nameID,
		version:     "1.1",
		sign:        true,
		signKey:     h.spKey,
	})
	rec := httptest.NewRecorder()
	h.idp.HandleSLO(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !h.sessionAlive(t, 42, sid) {
		t.Error("session revoked despite bad Version; MUST remain untouched")
	}
}

func TestSLOExpiredNotOnOrAfterRejected(t *testing.T) {
	h := newSLOHarness(t, sloSP())
	const nameID = "user-nameid-exp"
	sid := h.seedSession(t, 42, nameID, "")

	past := time.Now().Add(-time.Hour)
	req := buildLogoutRedirect(t, sloReqOpts{
		id:           "_slo-expired",
		destination:  testSLOURL,
		nameID:       nameID,
		notOnOrAfter: &past,
		sign:         true,
		signKey:      h.spKey,
	})
	rec := httptest.NewRecorder()
	h.idp.HandleSLO(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !h.sessionAlive(t, 42, sid) {
		t.Error("session revoked despite expired NotOnOrAfter; MUST remain untouched")
	}
}

func TestSLOUnknownSPDirectError(t *testing.T) {
	h := newSLOHarness(t, sloSP())
	h.q.sp.EntityID = "https://someone-else.example/metadata"

	req := buildLogoutRedirect(t, sloReqOpts{
		id:          "_slo-unknownsp",
		destination: testSLOURL,
		nameID:      "whoever",
		sign:        true,
		signKey:     h.spKey,
	})
	rec := httptest.NewRecorder()
	h.idp.HandleSLO(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("unknown-SP must NOT redirect; got Location=%q", loc)
	}
}

func TestSLOSessionIndexFilter(t *testing.T) {
	h := newSLOHarness(t, sloSP())
	const nameID = "user-nameid-idx"
	sidA := h.seedSession(t, 42, nameID, "idx-A")
	sidB := h.seedSession(t, 42, nameID, "idx-B")

	// Logout targets only idx-A.
	req := buildLogoutRedirect(t, sloReqOpts{
		id:           "_slo-idx",
		destination:  testSLOURL,
		nameID:       nameID,
		sessionIndex: "idx-A",
		sign:         true,
		signKey:      h.spKey,
	})
	rec := httptest.NewRecorder()
	h.idp.HandleSLO(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body=%s", rec.Code, rec.Body.String())
	}
	if h.sessionAlive(t, 42, sidA) {
		t.Error("session A (targeted) still alive")
	}
	if !h.sessionAlive(t, 42, sidB) {
		t.Error("session B (not targeted) was revoked")
	}
	if del := h.q.deletedRows(); len(del) != 1 || del[0] != sidA {
		t.Errorf("deleted rows = %v, want [%s]", del, sidA)
	}
}

// TestSLONoMetadataFallback confirms an SP registered WITHOUT metadata still gets
// its session revoked and receives the signed LogoutResponse XML directly.
func TestSLONoMetadataFallback(t *testing.T) {
	sp := sloSP()
	sp.MetadataXml = pgtype.Text{Valid: false}
	h := newSLOHarness(t, sp)
	const nameID = "user-nameid-nometa"
	sid := h.seedSession(t, 42, nameID, "")

	req := buildLogoutRedirect(t, sloReqOpts{
		id:          "_slo-nometa",
		destination: testSLOURL,
		nameID:      nameID,
		sign:        true,
		signKey:     h.spKey,
	})
	rec := httptest.NewRecorder()
	h.idp.HandleSLO(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 direct XML; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/xml") {
		t.Errorf("Content-Type = %q, want text/xml", ct)
	}
	if h.sessionAlive(t, 42, sid) {
		t.Error("session still alive in no-metadata fallback")
	}
	assertLogoutResponseValid(t, h, rec.Body.Bytes(), "_slo-nometa")
}
