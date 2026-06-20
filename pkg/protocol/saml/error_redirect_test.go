package saml

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"prohibitorum/pkg/authn"
)

// This file guards the "cannot-respond-to-SP" → SPA /error conversion: the
// human-facing dead-ends where the IdP cannot safely produce a SAML response for
// the SP (malformed/unparseable request, unknown/disabled SP, idp-init-disabled,
// replay, rate-limit, internal error) now 302-redirect the browser to
// /error?error=<code>&ref=<ref> instead of returning raw plaintext.
//
// The SP-binding responses (auto-POST success, passive/denied <StatusCode>, SLO
// responses) and the existing app-access-denied /error redirect / not-
// authenticated /login redirect are PRESERVED — those are covered by the success
// and passive/denied tests in sso_test.go / sso_init_test.go / slo_test.go,
// which remain the regression guard that SP delivery (auto-POST) is intact (e.g.
// TestSSOHappyPath still renders the auto-POST HTML form).

// assertErrorRedirect verifies the recorder holds a 302 to the SPA /error page
// with the expected error code prefix and a ref query param.
func assertErrorRedirect(t *testing.T, rec *httptest.ResponseRecorder, wantCode string) {
	t.Helper()
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 /error redirect; body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	wantPrefix := "/error?error=" + wantCode + "&ref="
	if !strings.HasPrefix(loc, wantPrefix) {
		t.Fatalf("Location = %q, want prefix %q", loc, wantPrefix)
	}
	// ref must be non-empty (NewRef yields 8 hex chars; sentinel "00000000" still
	// counts as present).
	if !strings.HasPrefix(loc, wantPrefix) || len(loc) <= len(wantPrefix) {
		t.Errorf("Location = %q, want a non-empty ref after %q", loc, wantPrefix)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

// TestSSO_MalformedRequest_RedirectsToErrorPage drives HandleSSO with a
// SAMLRequest that is not valid base64. parseAuthnRequest fails on the untrusted
// side of the open-redirect guard, so ssoParseError now dead-ends the browser at
// /error?error=saml_request_invalid rather than returning a 400 plaintext.
func TestSSO_MalformedRequest_RedirectsToErrorPage(t *testing.T) {
	h := newSSOHarness(t, ssoSP())

	req := httptest.NewRequest(http.MethodGet, testSSOURL+"?SAMLRequest=not-base64", nil)
	req.RemoteAddr = "203.0.113.7:54321"
	req.Header.Set("User-Agent", "test-agent/1.0")
	// Attach a live session: the parse failure must short-circuit BEFORE the
	// session gate, so the malformed request never reaches issuance.
	req = req.WithContext(authn.WithSession(req.Context(), liveSession(testAccount())))
	rec := httptest.NewRecorder()

	h.idp.HandleSSO(rec, req)

	assertErrorRedirect(t, rec, "saml_request_invalid")
	// A malformed request must never issue an assertion.
	if samlResponseRe.MatchString(rec.Body.String()) {
		t.Error("malformed request must not issue a SAMLResponse")
	}
	if rows := h.q.sessions(); len(rows) != 0 {
		t.Errorf("saml_session rows = %d, want 0", len(rows))
	}
}

// TestIdPInit_UnknownSP_RedirectsToErrorPage drives the IdP-initiated launcher
// with an sp= entity ID that resolves to nothing (pgx.ErrNoRows). The unknown SP
// is on the untrusted side of the open-redirect guard and now dead-ends at
// /error?error=saml_sp_unknown rather than a 400 plaintext.
func TestIdPInit_UnknownSP_RedirectsToErrorPage(t *testing.T) {
	sp := ssoSP()
	sp.AllowIdpInitiated = true
	h := newSSOHarness(t, sp)

	req := idpInitRequest("https://stranger.example/metadata", "", liveSession(testAccount()))
	rec := httptest.NewRecorder()
	h.idp.HandleIdPInitiated(rec, req)

	assertErrorRedirect(t, rec, "saml_sp_unknown")
	if samlResponseRe.MatchString(rec.Body.String()) {
		t.Error("unknown SP must not issue a SAMLResponse")
	}
	if rows := h.q.sessions(); len(rows) != 0 {
		t.Errorf("saml_session rows = %d, want 0", len(rows))
	}
}

// TestIdPInit_DisabledSP_RedirectsToErrorPage drives the IdP-initiated launcher
// with a KNOWN SP that is disabled. A disabled SP is treated as unregistered, so
// it collapses to the same saml_sp_unknown dead-end.
func TestIdPInit_DisabledSP_RedirectsToErrorPage(t *testing.T) {
	sp := ssoSP()
	sp.AllowIdpInitiated = true
	sp.Disabled = true
	h := newSSOHarness(t, sp)

	req := idpInitRequest(testSPEntityID, "", liveSession(testAccount()))
	rec := httptest.NewRecorder()
	h.idp.HandleIdPInitiated(rec, req)

	assertErrorRedirect(t, rec, "saml_sp_unknown")
	if samlResponseRe.MatchString(rec.Body.String()) {
		t.Error("disabled SP must not issue a SAMLResponse")
	}
}

// TestIdPInit_NotEnabled_RedirectsToErrorPage drives the IdP-initiated launcher
// with a KNOWN, enabled SP that has NOT opted into IdP-initiated SSO
// (AllowIdpInitiated=false). The opt-in guard now dead-ends the browser at
// /error?error=saml_idp_init_disabled rather than a 403 plaintext.
func TestIdPInit_NotEnabled_RedirectsToErrorPage(t *testing.T) {
	sp := ssoSP()
	sp.AllowIdpInitiated = false // explicit: not opted in
	h := newSSOHarness(t, sp)

	req := idpInitRequest(testSPEntityID, "", liveSession(testAccount()))
	rec := httptest.NewRecorder()
	h.idp.HandleIdPInitiated(rec, req)

	assertErrorRedirect(t, rec, "saml_idp_init_disabled")
	if samlResponseRe.MatchString(rec.Body.String()) {
		t.Error("not-opted-in SP must not issue a SAMLResponse")
	}
	if rows := h.q.sessions(); len(rows) != 0 {
		t.Errorf("saml_session rows = %d, want 0", len(rows))
	}
}

// TestSSO_Replay_RedirectsToErrorPage drives HandleSSO twice with the same
// AuthnRequest ID. The second consume trips ErrReplayedRequest, which now
// dead-ends at /error?error=saml_replayed rather than a 400 plaintext, and must
// not persist a second saml_session row.
func TestSSO_Replay_RedirectsToErrorPage(t *testing.T) {
	h := newSSOHarness(t, ssoSP())
	acct := testAccount()

	req1 := h.request(t, "_sso-replay-err", liveSession(acct))
	rec1 := httptest.NewRecorder()
	h.idp.HandleSSO(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first SSO status = %d, want 200; body=%s", rec1.Code, rec1.Body.String())
	}

	req2 := h.request(t, "_sso-replay-err", liveSession(acct))
	rec2 := httptest.NewRecorder()
	h.idp.HandleSSO(rec2, req2)

	assertErrorRedirect(t, rec2, "saml_replayed")
	if rows := h.q.sessions(); len(rows) != 1 {
		t.Errorf("saml_session rows = %d after replay, want 1", len(rows))
	}
}

// TestSSO_InternalError_RedirectsToErrorPage forces the RBAC predicate to error,
// which fails closed. The internal error now dead-ends at
// /error?error=server_error rather than a 500 plaintext, with no assertion.
func TestSSO_InternalError_RedirectsToErrorPage(t *testing.T) {
	h := newSSOHarness(t, ssoSP())
	h.q.authzErr = errStubPredicate

	req := h.request(t, "_sso-internal-err", liveSession(testAccount()))
	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, req)

	assertErrorRedirect(t, rec, "server_error")
	if rows := h.q.sessions(); len(rows) != 0 {
		t.Errorf("saml_session rows = %d, want 0 (internal error issues nothing)", len(rows))
	}
	if samlResponseRe.MatchString(rec.Body.String()) {
		t.Error("internal error must not issue a SAMLResponse")
	}
}

// TestIdPInit_RateLimited_RedirectsToErrorPage exhausts the per-account/SP rate
// budget on the IdP-initiated launcher; the next request trips the limiter,
// which now dead-ends at /error?error=rate_limited (with Retry-After still set)
// rather than a 429 plaintext, and issues no assertion.
func TestIdPInit_RateLimited_RedirectsToErrorPage(t *testing.T) {
	sp := ssoSP()
	sp.AllowIdpInitiated = true
	h := newSSOHarness(t, sp)
	acct := testAccount()

	for n := 0; n < samlSSORateMax; n++ {
		req := idpInitRequest(testSPEntityID, "", liveSession(acct))
		rec := httptest.NewRecorder()
		h.idp.HandleIdPInitiated(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200 (under limit); body=%s", n, rec.Code, rec.Body.String())
		}
	}

	sessionsBefore := len(h.q.sessions())

	req := idpInitRequest(testSPEntityID, "", liveSession(acct))
	rec := httptest.NewRecorder()
	h.idp.HandleIdPInitiated(rec, req)

	assertErrorRedirect(t, rec, "rate_limited")
	if ra := rec.Header().Get("Retry-After"); ra == "" {
		t.Errorf("over-limit response missing Retry-After header")
	}
	if got := len(h.q.sessions()); got != sessionsBefore {
		t.Errorf("over-limit request persisted a saml_session row: before=%d after=%d", sessionsBefore, got)
	}
}
