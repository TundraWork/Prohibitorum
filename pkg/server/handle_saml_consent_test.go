// Package server — handle_saml_consent_test.go
//
// Unit tests for the two SAML advisory consent endpoints:
//
//	GET  /api/prohibitorum/saml-consent?ticket=  (context)
//	POST /api/prohibitorum/saml-consent          (decision)
//
// The harness mirrors the sudo test pattern: a kv.MemoryStore holds the consent
// ticket, a fakeSAMLConsentQ stubs the DB surface (GetEntityIconEtag,
// UpsertSAMLConsent), and sessions are injected via authn.WithSession — no real
// Postgres or HTTP middleware required.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

// --- fake DB ------------------------------------------------------------------

type fakeSAMLConsentQ struct {
	// upserted records AccountID+SpID pairs that were approved.
	upserted []db.UpsertSAMLConsentParams
}

func (f *fakeSAMLConsentQ) GetEntityIconEtag(_ context.Context, _ db.GetEntityIconEtagParams) (string, error) {
	return "", pgx.ErrNoRows
}

func (f *fakeSAMLConsentQ) UpsertSAMLConsent(_ context.Context, arg db.UpsertSAMLConsentParams) error {
	f.upserted = append(f.upserted, arg)
	return nil
}

// --- test helpers -------------------------------------------------------------

// newSAMLConsentTestServer returns a Server wired with a memory KV store and a
// fake DB, ready to exercise the two SAML consent HTTP handlers.
func newSAMLConsentTestServer(t *testing.T) (*Server, *fakeSAMLConsentQ, kv.Store) {
	t.Helper()
	fq := &fakeSAMLConsentQ{}
	kvStore := kv.NewMemoryStore()
	s := &Server{
		kvStore:             kvStore,
		samlConsentOverride: fq,
	}
	return s, fq, kvStore
}

// newSAMLConsentSession builds a minimal authn.Session for the given accountID.
func newSAMLConsentSession(accountID int32) *authn.Session {
	acct := &db.Account{ID: accountID, Username: "alice", DisplayName: "Alice Liddell"}
	data := &authn.SessionData{AccountID: accountID}
	return &authn.Session{Account: acct, Data: data}
}

// mintTicket stores a SAMLConsentTicket in KV and returns the opaque nonce.
func mintTicket(t *testing.T, kvStore kv.Store, ticket authn.SAMLConsentTicket) string {
	t.Helper()
	nonce, err := authn.DemandSAMLConsent(context.Background(), kvStore, ticket)
	if err != nil {
		t.Fatalf("DemandSAMLConsent: %v", err)
	}
	return nonce
}

// samlConsentGETReq builds an HTTP GET request for the context endpoint.
func samlConsentGETReq(t *testing.T, sess *authn.Session, ticketParam string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/prohibitorum/saml-consent?ticket=%s", ticketParam), nil)
	r = r.WithContext(authn.WithSession(r.Context(), sess))
	return r
}

// samlConsentPOSTReq builds an HTTP POST request for the decision endpoint.
func samlConsentPOSTReq(t *testing.T, sess *authn.Session, body string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/saml-consent", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(authn.WithSession(r.Context(), sess))
	return r
}

// decodeJSONMap is a small helper shared by these tests.
func decodeJSONMapSAML(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode JSON: %v (raw=%s)", err, string(body))
	}
	return m
}

// --- Context (GET) tests ------------------------------------------------------

// TestSAMLConsentContext_HappyPath: mint a ticket, GET with correct session,
// assert SP.displayName and attributes round-trip correctly.
func TestSAMLConsentContext_HappyPath(t *testing.T) {
	s, _, kvStore := newSAMLConsentTestServer(t)
	const accountID int32 = 7

	nonce := mintTicket(t, kvStore, authn.SAMLConsentTicket{
		AccountID:   accountID,
		SPID:        42,
		EntityID:    "https://sf.example/meta",
		DisplayName: "Salesforce",
		Attributes:  []string{"Email", "Groups"},
		ReturnTo:    "https://idp.example/saml/sso?x=1",
	})

	sess := newSAMLConsentSession(accountID)
	r := samlConsentGETReq(t, sess, nonce)
	w := httptest.NewRecorder()
	s.handleSAMLConsentContextHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}

	var out contract.SAMLConsentContext
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.SP.DisplayName != "Salesforce" {
		t.Errorf("SP.DisplayName: want Salesforce, got %q", out.SP.DisplayName)
	}
	if len(out.Attributes) != 2 || out.Attributes[0] != "Email" || out.Attributes[1] != "Groups" {
		t.Errorf("Attributes: want [Email Groups], got %v", out.Attributes)
	}
	if out.Account.DisplayName != "Alice Liddell" {
		t.Errorf("Account.DisplayName: want Alice Liddell, got %q", out.Account.DisplayName)
	}
}

// TestSAMLConsentContext_MissingTicket: no ticket → invalid_consent_ticket.
func TestSAMLConsentContext_MissingTicket(t *testing.T) {
	s, _, _ := newSAMLConsentTestServer(t)
	sess := newSAMLConsentSession(1)
	r := samlConsentGETReq(t, sess, "")
	w := httptest.NewRecorder()
	s.handleSAMLConsentContextHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", w.Code)
	}
	m := decodeJSONMapSAML(t, w.Body.Bytes())
	if m["code"] != "invalid_consent_ticket" {
		t.Errorf("code: want invalid_consent_ticket, got %v", m["code"])
	}
}

// TestSAMLConsentContext_WrongAccount: ticket belongs to account 7, but session
// is for account 99 → invalid_consent_ticket.
func TestSAMLConsentContext_WrongAccount(t *testing.T) {
	s, _, kvStore := newSAMLConsentTestServer(t)

	nonce := mintTicket(t, kvStore, authn.SAMLConsentTicket{
		AccountID: 7, SPID: 42, DisplayName: "Salesforce", ReturnTo: "/",
	})

	sess := newSAMLConsentSession(99) // different account
	r := samlConsentGETReq(t, sess, nonce)
	w := httptest.NewRecorder()
	s.handleSAMLConsentContextHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", w.Code)
	}
	m := decodeJSONMapSAML(t, w.Body.Bytes())
	if m["code"] != "invalid_consent_ticket" {
		t.Errorf("code: want invalid_consent_ticket, got %v", m["code"])
	}
}

// --- Decision (POST) tests ----------------------------------------------------

// TestSAMLConsentDecision_Approve: consume ticket with "approve" → row upserted,
// redirect == ReturnTo.
func TestSAMLConsentDecision_Approve(t *testing.T) {
	s, fq, kvStore := newSAMLConsentTestServer(t)
	const accountID int32 = 7
	const spID int64 = 42
	const returnTo = "https://idp.example/saml/sso?x=1"

	nonce := mintTicket(t, kvStore, authn.SAMLConsentTicket{
		AccountID: accountID, SPID: spID, DisplayName: "Salesforce", ReturnTo: returnTo,
	})

	sess := newSAMLConsentSession(accountID)
	body, _ := json.Marshal(contract.SAMLConsentDecision{Ticket: nonce, Decision: "approve"})
	r := samlConsentPOSTReq(t, sess, string(body))
	w := httptest.NewRecorder()
	s.handleSAMLConsentDecisionHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}

	var out contract.ConsentResult
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Redirect != returnTo {
		t.Errorf("redirect: want %q, got %q", returnTo, out.Redirect)
	}

	// Verify UpsertSAMLConsent was called with the right params.
	if len(fq.upserted) != 1 {
		t.Fatalf("upserted: want 1 row, got %d", len(fq.upserted))
	}
	if fq.upserted[0].AccountID != accountID || fq.upserted[0].SpID != spID {
		t.Errorf("upserted params: want {%d %d}, got %+v", accountID, spID, fq.upserted[0])
	}
}

// TestSAMLConsentDecision_Decline: "decline" → redirect == "/", no row written.
func TestSAMLConsentDecision_Decline(t *testing.T) {
	s, fq, kvStore := newSAMLConsentTestServer(t)
	const accountID int32 = 7

	nonce := mintTicket(t, kvStore, authn.SAMLConsentTicket{
		AccountID: accountID, SPID: 42, DisplayName: "Salesforce", ReturnTo: "https://idp.example/sso",
	})

	sess := newSAMLConsentSession(accountID)
	body, _ := json.Marshal(contract.SAMLConsentDecision{Ticket: nonce, Decision: "decline"})
	r := samlConsentPOSTReq(t, sess, string(body))
	w := httptest.NewRecorder()
	s.handleSAMLConsentDecisionHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}

	var out contract.ConsentResult
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Redirect != "/" {
		t.Errorf("redirect: want /, got %q", out.Redirect)
	}
	if len(fq.upserted) != 0 {
		t.Errorf("upserted: want 0 rows on decline, got %d", len(fq.upserted))
	}
}

// TestSAMLConsentDecision_SingleUse: the same nonce cannot be used twice.
// The second POST must return invalid_consent_ticket because ConsumeSAMLConsent
// pops the key.
func TestSAMLConsentDecision_SingleUse(t *testing.T) {
	s, _, kvStore := newSAMLConsentTestServer(t)
	const accountID int32 = 7

	nonce := mintTicket(t, kvStore, authn.SAMLConsentTicket{
		AccountID: accountID, SPID: 42, DisplayName: "Salesforce", ReturnTo: "/",
	})

	sess := newSAMLConsentSession(accountID)
	body, _ := json.Marshal(contract.SAMLConsentDecision{Ticket: nonce, Decision: "approve"})

	// First use — should succeed.
	r1 := samlConsentPOSTReq(t, sess, string(body))
	w1 := httptest.NewRecorder()
	s.handleSAMLConsentDecisionHTTP(w1, r1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first use: want 200, got %d (body=%s)", w1.Code, w1.Body.String())
	}

	// Second use — ticket already consumed, must fail.
	r2 := samlConsentPOSTReq(t, sess, string(body))
	w2 := httptest.NewRecorder()
	s.handleSAMLConsentDecisionHTTP(w2, r2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("second use: want 400, got %d (body=%s)", w2.Code, w2.Body.String())
	}
	m := decodeJSONMapSAML(t, w2.Body.Bytes())
	if m["code"] != "invalid_consent_ticket" {
		t.Errorf("code: want invalid_consent_ticket, got %v", m["code"])
	}
}

// TestSAMLConsentDecision_CrossAccount: ticket minted for account 7, POST with
// session for account 99 → invalid_consent_ticket, no row written.
func TestSAMLConsentDecision_CrossAccount(t *testing.T) {
	s, fq, kvStore := newSAMLConsentTestServer(t)

	nonce := mintTicket(t, kvStore, authn.SAMLConsentTicket{
		AccountID: 7, SPID: 42, DisplayName: "Salesforce", ReturnTo: "/",
	})

	sess := newSAMLConsentSession(99) // wrong account
	body, _ := json.Marshal(contract.SAMLConsentDecision{Ticket: nonce, Decision: "approve"})
	r := samlConsentPOSTReq(t, sess, string(body))
	w := httptest.NewRecorder()
	s.handleSAMLConsentDecisionHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
	m := decodeJSONMapSAML(t, w.Body.Bytes())
	if m["code"] != "invalid_consent_ticket" {
		t.Errorf("code: want invalid_consent_ticket, got %v", m["code"])
	}
	if len(fq.upserted) != 0 {
		t.Errorf("upserted: want 0 rows on cross-account attempt, got %d", len(fq.upserted))
	}
}

// TestSAMLConsentDecision_BadDecision: unknown decision value → bad_request.
func TestSAMLConsentDecision_BadDecision(t *testing.T) {
	s, _, kvStore := newSAMLConsentTestServer(t)
	const accountID int32 = 7

	nonce := mintTicket(t, kvStore, authn.SAMLConsentTicket{
		AccountID: accountID, SPID: 42, DisplayName: "Salesforce", ReturnTo: "/",
	})

	sess := newSAMLConsentSession(accountID)
	body, _ := json.Marshal(contract.SAMLConsentDecision{Ticket: nonce, Decision: "maybe"})
	r := samlConsentPOSTReq(t, sess, string(body))
	w := httptest.NewRecorder()
	s.handleSAMLConsentDecisionHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
	m := decodeJSONMapSAML(t, w.Body.Bytes())
	if m["code"] != "bad_request" {
		t.Errorf("code: want bad_request, got %v", m["code"])
	}
}
