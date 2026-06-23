package saml

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
)

// ---------------------------------------------------------------------------
// attributeLabels unit test
// ---------------------------------------------------------------------------

func TestAttributeLabels(t *testing.T) {
	mapJSON := []byte(`[
		{"name":"urn:oid:0.9.2342.19200300.100.1.3","friendly_name":"Email","source":"email"},
		{"name":"http://schemas/name","friendly_name":"","source":"display_name"},
		{"name":"urn:oid:0.9.2342.19200300.100.1.3","friendly_name":"Email","source":"email"}
	]`)
	got := attributeLabels(mapJSON)
	want := []string{"Email", "http://schemas/name"}
	if len(got) != len(want) {
		t.Fatalf("labels = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("labels = %v, want %v", got, want)
		}
	}
	if l := attributeLabels([]byte("not json")); len(l) != 0 {
		t.Fatalf("malformed → no labels, got %v", l)
	}
	if l := attributeLabels([]byte("[]")); len(l) != 0 {
		t.Fatalf("empty → no labels, got %v", l)
	}
}

// ---------------------------------------------------------------------------
// HasSAMLConsent on the fake querier.
//
// The SSO handlers only call HasSAMLConsent (UpsertSAMLConsent is the server
// endpoint's job — Task 4 — not the protocol handler's). The fake reports
// "consented" by DEFAULT so every pre-existing issuance test keeps passing; the
// no-consent path is exercised by setting q.noConsent = true. consentErr forces
// the fail-closed (500) path.
// ---------------------------------------------------------------------------

func (f *fakeSSOQueries) HasSAMLConsent(_ context.Context, _ db.HasSAMLConsentParams) (bool, error) {
	if f.consentErr != nil {
		return false, f.consentErr
	}
	return !f.noConsent, nil
}

// ---------------------------------------------------------------------------
// Handler interposition tests for IdP-initiated SSO
// ---------------------------------------------------------------------------

// No advisory ack yet → HandleIdPInitiated must 302 to /saml-consent and issue
// no assertion.
func TestHandleIdPInitiated_NoConsent_RedirectsToScreen(t *testing.T) {
	sp := ssoSP()
	sp.AllowIdpInitiated = true
	h := newSSOHarness(t, sp)
	h.q.noConsent = true

	req := idpInitRequest(testSPEntityID, "", liveSession(testAccount()))
	rec := httptest.NewRecorder()
	h.idp.HandleIdPInitiated(rec, req)

	if rec.Code != 302 {
		t.Fatalf("status = %d, want 302 consent redirect; body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	wantPrefix := testIdPOrigin + "/saml-consent?ticket="
	if !strings.HasPrefix(loc, wantPrefix) {
		t.Fatalf("Location = %q, want prefix %q", loc, wantPrefix)
	}
	if ticket := strings.TrimPrefix(loc, wantPrefix); ticket == "" {
		t.Fatalf("Location %q has empty ticket nonce", loc)
	}
	if samlResponseRe.MatchString(rec.Body.String()) {
		t.Error("no-consent path must not issue a SAMLResponse")
	}
	if rows := h.q.sessions(); len(rows) != 0 {
		t.Errorf("saml_session rows = %d, want 0 (consent not yet given)", len(rows))
	}
}

// With an advisory ack present (the fake's default), HandleIdPInitiated issues
// the unsolicited assertion (auto-POST form carrying a SAMLResponse).
func TestHandleIdPInitiated_WithConsent_Issues(t *testing.T) {
	sp := ssoSP()
	sp.AllowIdpInitiated = true
	h := newSSOHarness(t, sp)

	req := idpInitRequest(testSPEntityID, "", liveSession(testAccount()))
	rec := httptest.NewRecorder()
	h.idp.HandleIdPInitiated(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200 auto-POST (consent present); body=%s", rec.Code, rec.Body.String())
	}
	if !samlResponseRe.MatchString(rec.Body.String()) {
		t.Error("with consent, HandleIdPInitiated must issue a SAMLResponse")
	}
	if rows := h.q.sessions(); len(rows) != 1 {
		t.Errorf("saml_session rows = %d, want 1 (assertion issued)", len(rows))
	}
}

// ---------------------------------------------------------------------------
// Handler interposition tests for SP-initiated SSO (HandleSSO)
// ---------------------------------------------------------------------------

// SP-initiated, non-passive, no advisory ack → HandleSSO must 302 to
// /saml-consent and issue no assertion. Also exercises the `!req.IsPassive`
// branch of the gate.
func TestHandleSSO_NoConsent_RedirectsToScreen(t *testing.T) {
	h := newSSOHarness(t, ssoSP())
	h.q.noConsent = true

	req := h.request(t, "_sso-consent-gate", liveSession(testAccount()))
	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 consent redirect; body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	wantPrefix := testIdPOrigin + "/saml-consent?ticket="
	if !strings.HasPrefix(loc, wantPrefix) {
		t.Fatalf("Location = %q, want prefix %q", loc, wantPrefix)
	}
	if samlResponseRe.MatchString(rec.Body.String()) {
		t.Error("no-consent path must not issue a SAMLResponse")
	}
	if rows := h.q.sessions(); len(rows) != 0 {
		t.Errorf("saml_session rows = %d, want 0 (consent not yet given)", len(rows))
	}
}

// A HasSAMLConsent error must fail closed: 302 to the SPA /error (server_error),
// no assertion. Mirrors the RBAC authzErr fail-closed test.
func TestHandleSSO_ConsentErr_FailsClosed(t *testing.T) {
	h := newSSOHarness(t, ssoSP())
	h.q.consentErr = errStubPredicate

	req := h.request(t, "_sso-consent-err", liveSession(testAccount()))
	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 /error (fail closed); body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/error?error=server_error&ref=") {
		t.Fatalf("Location = %q, want /error?error=server_error prefix", loc)
	}
	if samlResponseRe.MatchString(rec.Body.String()) {
		t.Error("consent error must not issue a SAMLResponse")
	}
	if rows := h.q.sessions(); len(rows) != 0 {
		t.Errorf("saml_session rows = %d, want 0 (failed closed)", len(rows))
	}
}

// ---------------------------------------------------------------------------
// HandleConsentResume tests
//
// These drive the FULL flow rather than poking the IdP's (unexported) KV: with
// the gate armed (noConsent=true), an IdP-initiated SSO 302s to
// /saml-consent?ticket=…; we extract that ticket and replay it against
// HandleConsentResume with a live session. This proves the stash-and-issue path
// emits the assertion from the stashed (gate-validated) issue context — which is
// exactly what makes POST-binding SP-initiated consent work, since the resume
// never re-reads the original request.
// ---------------------------------------------------------------------------

// armConsentTicket runs an IdP-initiated SSO with the consent gate armed and
// returns the opaque ticket nonce the gate stashed (extracted from the
// /saml-consent redirect's query). It asserts the gate actually fired.
func armConsentTicket(t *testing.T, h *ssoHarness, acct *db.Account) string {
	t.Helper()
	h.q.noConsent = true
	req := idpInitRequest(testSPEntityID, "deep-relay", liveSession(acct))
	rec := httptest.NewRecorder()
	h.idp.HandleIdPInitiated(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("arm: status = %d, want 302 consent redirect; body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	wantPrefix := testIdPOrigin + "/saml-consent?ticket="
	if !strings.HasPrefix(loc, wantPrefix) {
		t.Fatalf("arm: Location = %q, want prefix %q", loc, wantPrefix)
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("arm: parse Location %q: %v", loc, err)
	}
	ticket := u.Query().Get("ticket")
	if ticket == "" {
		t.Fatalf("arm: empty ticket in Location %q", loc)
	}
	return ticket
}

// resumeReq builds a GET /saml/sso/resume?ticket=… request with the given
// session attached.
func resumeReq(ticket string, sess *authn.Session) *http.Request {
	target := testIdPOrigin + "/saml/sso/resume?ticket=" + url.QueryEscape(ticket)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.RemoteAddr = "203.0.113.7:54321"
	req.Header.Set("User-Agent", "test-agent/1.0")
	if sess != nil {
		req = req.WithContext(authn.WithSession(req.Context(), sess))
	}
	return req
}

// TestHandleConsentResume_Issues proves the resume path emits the assertion from
// the stashed issue context (the POST-binding scenario: no original request is
// re-read), records the ack, and persists a saml_session row.
func TestHandleConsentResume_Issues(t *testing.T) {
	sp := ssoSP()
	sp.AllowIdpInitiated = true
	h := newSSOHarness(t, sp)
	acct := testAccount()

	ticket := armConsentTicket(t, h, acct)

	rec := httptest.NewRecorder()
	h.idp.HandleConsentResume(rec, resumeReq(ticket, liveSession(acct)))

	if rec.Code != http.StatusOK {
		t.Fatalf("resume status = %d, want 200 auto-POST; body=%s", rec.Code, rec.Body.String())
	}
	if !samlResponseRe.MatchString(rec.Body.String()) {
		t.Error("resume must issue a SAMLResponse (auto-POST)")
	}
	if rows := h.q.sessions(); len(rows) != 1 {
		t.Errorf("saml_session rows = %d, want 1 (assertion issued on resume)", len(rows))
	}
	if acks := h.q.consents(); len(acks) != 1 || acks[0].AccountID != acct.ID || acks[0].SpID != sp.ID {
		t.Errorf("consent upserts = %+v, want 1 row for account %d sp %d", acks, acct.ID, sp.ID)
	}
}

// TestHandleConsentResume_RechecksRBAC proves authorization is re-evaluated at
// resume and fails CLOSED: a denial between the gate and the resume yields a 302
// to /error?reason=app_access_denied and issues nothing.
func TestHandleConsentResume_RechecksRBAC(t *testing.T) {
	sp := ssoSP()
	sp.AllowIdpInitiated = true
	sp.DisplayName = "Acme Cloud"
	h := newSSOHarness(t, sp)
	acct := testAccount()

	ticket := armConsentTicket(t, h, acct)

	// Access is revoked AFTER the gate stashed the ticket.
	h.q.denied = true

	rec := httptest.NewRecorder()
	h.idp.HandleConsentResume(rec, resumeReq(ticket, liveSession(acct)))

	if rec.Code != http.StatusFound {
		t.Fatalf("resume status = %d, want 302 /error (RBAC fail closed); body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	wantPrefix := testIdPOrigin + "/error?reason=app_access_denied"
	if !strings.HasPrefix(loc, wantPrefix) {
		t.Fatalf("resume Location = %q, want prefix %q", loc, wantPrefix)
	}
	if samlResponseRe.MatchString(rec.Body.String()) {
		t.Error("RBAC denial at resume must not issue a SAMLResponse")
	}
	if rows := h.q.sessions(); len(rows) != 0 {
		t.Errorf("saml_session rows = %d, want 0 (denial issues nothing)", len(rows))
	}
	if acks := h.q.consents(); len(acks) != 0 {
		t.Errorf("consent upserts = %d, want 0 (denial must not record an ack)", len(acks))
	}
}

// TestHandleConsentResume_SingleUse proves the resume ticket is single-use: the
// second resume with the same ticket dead-ends at the SPA /error page
// (saml_request_invalid) because the nonce was consumed by the first resume.
func TestHandleConsentResume_SingleUse(t *testing.T) {
	sp := ssoSP()
	sp.AllowIdpInitiated = true
	h := newSSOHarness(t, sp)
	acct := testAccount()

	ticket := armConsentTicket(t, h, acct)

	rec1 := httptest.NewRecorder()
	h.idp.HandleConsentResume(rec1, resumeReq(ticket, liveSession(acct)))
	if rec1.Code != http.StatusOK {
		t.Fatalf("first resume status = %d, want 200; body=%s", rec1.Code, rec1.Body.String())
	}

	rec2 := httptest.NewRecorder()
	h.idp.HandleConsentResume(rec2, resumeReq(ticket, liveSession(acct)))
	if rec2.Code != http.StatusFound {
		t.Fatalf("second resume status = %d, want 302 /error (ticket consumed); body=%s", rec2.Code, rec2.Body.String())
	}
	if loc := rec2.Header().Get("Location"); !strings.HasPrefix(loc, "/error?error=saml_request_invalid&ref=") {
		t.Fatalf("second resume Location = %q, want /error?error=saml_request_invalid prefix", loc)
	}
	// Only the first resume issued.
	if rows := h.q.sessions(); len(rows) != 1 {
		t.Errorf("saml_session rows = %d, want 1 (only the first resume issues)", len(rows))
	}
}
