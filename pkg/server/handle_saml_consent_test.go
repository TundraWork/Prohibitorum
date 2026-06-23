// Package server — handle_saml_consent_test.go
//
// Unit tests for the two SAML advisory consent endpoints:
//
//	GET  /api/prohibitorum/saml-consent?ticket=  (context)
//	POST /api/prohibitorum/saml-consent          (decision)
//
// The harness mirrors the sudo test pattern: a kv.MemoryStore holds the consent
// ticket, a fakeSAMLConsentQ stubs the DB surface (GetEntityIconEtag), and
// sessions are injected via authn.WithSession — no real Postgres or HTTP
// middleware required.

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

type fakeSAMLConsentQ struct{}

func (f *fakeSAMLConsentQ) GetEntityIconEtag(_ context.Context, _ db.GetEntityIconEtagParams) (string, error) {
	return "", pgx.ErrNoRows
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
		ACSURL:      "https://sf.example/acs",
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
		AccountID: 7, SPID: 42, DisplayName: "Salesforce",
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

// TestSAMLConsentDecision_Approve: "approve" PEEKS the ticket (does NOT consume,
// does NOT write the ack) and hands off to the SAML resume endpoint via the
// redirect — that endpoint owns recording the ack and issuing the assertion.
func TestSAMLConsentDecision_Approve(t *testing.T) {
	s, _, kvStore := newSAMLConsentTestServer(t)
	const accountID int32 = 7
	const spID int64 = 42

	nonce := mintTicket(t, kvStore, authn.SAMLConsentTicket{
		AccountID: accountID, SPID: spID, DisplayName: "Salesforce", ACSURL: "https://sf.example/acs",
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
	if want := "/saml/sso/resume?ticket=" + nonce; out.Redirect != want {
		t.Errorf("redirect: want %q, got %q", want, out.Redirect)
	}

	// Approve only PEEKS, so the ticket must still be present for the resume
	// endpoint to consume.
	if _, ok, _ := authn.PeekSAMLConsent(context.Background(), kvStore, nonce, accountID); !ok {
		t.Error("approve must NOT consume the ticket — resume needs it")
	}
}

// TestSAMLConsentDecision_Decline: "decline" → redirect == "/" and the ticket is
// consumed (the user stays signed in but does not enter the app).
func TestSAMLConsentDecision_Decline(t *testing.T) {
	s, _, kvStore := newSAMLConsentTestServer(t)
	const accountID int32 = 7

	nonce := mintTicket(t, kvStore, authn.SAMLConsentTicket{
		AccountID: accountID, SPID: 42, DisplayName: "Salesforce", ACSURL: "https://sf.example/acs",
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
	// Decline consumes the ticket: a second use must find it gone.
	if _, ok, _ := authn.PeekSAMLConsent(context.Background(), kvStore, nonce, accountID); ok {
		t.Error("decline must consume the ticket")
	}
}

// TestSAMLConsentDecision_DeclineSingleUse: a declined nonce is consumed, so a
// second decision on it must return invalid_consent_ticket. (Approve only PEEKS;
// the single-use of the nonce is owned by the resume endpoint, exercised in the
// saml package's HandleConsentResume tests.)
func TestSAMLConsentDecision_DeclineSingleUse(t *testing.T) {
	s, _, kvStore := newSAMLConsentTestServer(t)
	const accountID int32 = 7

	nonce := mintTicket(t, kvStore, authn.SAMLConsentTicket{
		AccountID: accountID, SPID: 42, DisplayName: "Salesforce", ACSURL: "https://sf.example/acs",
	})

	sess := newSAMLConsentSession(accountID)
	body, _ := json.Marshal(contract.SAMLConsentDecision{Ticket: nonce, Decision: "decline"})

	// First use — should succeed (and consume the ticket).
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
// session for account 99 → invalid_consent_ticket (approve PEEK rejects the
// mismatched binding), and the ticket is NOT consumed.
func TestSAMLConsentDecision_CrossAccount(t *testing.T) {
	s, _, kvStore := newSAMLConsentTestServer(t)

	nonce := mintTicket(t, kvStore, authn.SAMLConsentTicket{
		AccountID: 7, SPID: 42, DisplayName: "Salesforce", ACSURL: "https://sf.example/acs",
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
	// The mismatched-account PEEK must NOT pop the (rightful owner's) ticket.
	if _, ok, _ := authn.PeekSAMLConsent(context.Background(), kvStore, nonce, 7); !ok {
		t.Error("cross-account attempt must not consume the rightful owner's ticket")
	}
}

// TestSAMLConsentDecision_BadDecision: unknown decision value → bad_request.
func TestSAMLConsentDecision_BadDecision(t *testing.T) {
	s, _, kvStore := newSAMLConsentTestServer(t)
	const accountID int32 = 7

	nonce := mintTicket(t, kvStore, authn.SAMLConsentTicket{
		AccountID: accountID, SPID: 42, DisplayName: "Salesforce", ACSURL: "https://sf.example/acs",
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
