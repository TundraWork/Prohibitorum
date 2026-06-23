package saml

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
