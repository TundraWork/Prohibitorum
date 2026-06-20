package saml

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	crewjam "github.com/crewjam/saml"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
)

// idpInitRequest builds a GET /saml/sso/init request for the given sp entity ID
// (and optional RelayState), attaching the supplied session to the context.
func idpInitRequest(sp, relayState string, sess *authn.Session) *http.Request {
	target := testIdPOrigin + "/saml/sso/init?sp=" + url.QueryEscape(sp)
	if relayState != "" {
		target += "&RelayState=" + url.QueryEscape(relayState)
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.RemoteAddr = "203.0.113.7:54321"
	req.Header.Set("User-Agent", "test-agent/1.0")
	if sess != nil {
		req = req.WithContext(authn.WithSession(req.Context(), sess))
	}
	return req
}

// TestSSOInitAppAccessDenied verifies the RBAC gate on the IdP-initiated path:
// a user not authorized for a restricted SP is 302-redirected to the IdP's OWN
// /error page (IdP-initiated is always interactive, so there is never a terminal
// SAML Response), no assertion is issued, and the denial is audited.
func TestSSOInitAppAccessDenied(t *testing.T) {
	sp := ssoSP()
	sp.AllowIdpInitiated = true
	sp.DisplayName = "Acme Cloud"
	h := newSSOHarness(t, sp)
	h.q.denied = true

	req := idpInitRequest(testSPEntityID, "deep", liveSession(testAccount()))
	rec := httptest.NewRecorder()
	h.idp.HandleIdPInitiated(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 /error redirect; body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	wantPrefix := testIdPOrigin + "/error?reason=app_access_denied"
	if !strings.HasPrefix(loc, wantPrefix) {
		t.Fatalf("Location = %q, want prefix %q", loc, wantPrefix)
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if got := u.Query().Get("app"); got != "Acme Cloud" {
		t.Fatalf("error page app= should be the SP display name, got %q", got)
	}
	// No assertion issued.
	if rows := h.q.sessions(); len(rows) != 0 {
		t.Errorf("saml_session rows = %d, want 0 (denial issues no assertion)", len(rows))
	}
	if samlResponseRe.MatchString(rec.Body.String()) {
		t.Error("denial must not issue a SAMLResponse")
	}

	// The denial must be audited (saml_sp / access_denied / account 42).
	recs := h.auditW.all()
	if len(recs) != 1 {
		t.Fatalf("audit records = %d, want 1", len(recs))
	}
	if recs[0].Factor != audit.FactorSAMLSP || recs[0].Event != audit.EventAccessDenied {
		t.Errorf("audit = factor %q event %q, want %q/%q", recs[0].Factor, recs[0].Event, audit.FactorSAMLSP, audit.EventAccessDenied)
	}
	if recs[0].Detail["reason"] != "app_access_denied" {
		t.Errorf("audit reason = %v, want app_access_denied", recs[0].Detail["reason"])
	}
}

func TestSSOInitOptInIssuesUnsolicited(t *testing.T) {
	sp := ssoSP()
	sp.AllowIdpInitiated = true
	h := newSSOHarness(t, sp)
	acct := testAccount()

	req := idpInitRequest(testSPEntityID, "deep", liveSession(acct))
	rec := httptest.NewRecorder()
	h.idp.HandleIdPInitiated(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 auto-POST; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// (a) The auto-POST form targets the SP's DEFAULT ACS, echoes RelayState
	// "deep" verbatim, and carries a SAMLResponse.
	action, respXML := decodeAutoPost(t, body)
	if action != testACSURL {
		t.Errorf("form action = %q, want default ACS %q", action, testACSURL)
	}
	rm := relayStateRe.FindStringSubmatch(body)
	if rm == nil {
		t.Fatal("no RelayState hidden input in auto-POST form")
	}
	if rm[1] != "deep" {
		t.Errorf("RelayState = %q, want %q (verbatim deep-link)", rm[1], "deep")
	}

	// (b) UNSOLICITED shape: NO InResponseTo on the Response element AND none on
	// the SubjectConfirmationData. Assert at the etree level (attribute truly
	// absent), not just the unmarshalled struct.
	doc, err := parseXMLSecure(respXML)
	if err != nil {
		t.Fatalf("parseXMLSecure(respXML): %v", err)
	}
	responseEl := doc.Root()
	if responseEl == nil || responseEl.Tag != "Response" {
		t.Fatalf("root element is not Response: %+v", responseEl)
	}
	if attr := responseEl.SelectAttr("InResponseTo"); attr != nil {
		t.Errorf("Response has InResponseTo=%q; unsolicited Response must omit it", attr.Value)
	}
	if err := verifyElementSignature(responseEl, h.idpCert); err != nil {
		t.Errorf("unsolicited Response signature did not verify: %v", err)
	}

	assertionEl := childByLocalName(responseEl, "Assertion")
	if assertionEl == nil {
		t.Fatal("no Assertion child in unsolicited Response")
	}
	subjectEl := childByLocalName(assertionEl, "Subject")
	if subjectEl == nil {
		t.Fatal("no Subject in assertion")
	}
	scd := childByLocalName(subjectEl, "SubjectConfirmation")
	if scd == nil {
		t.Fatal("no SubjectConfirmation in assertion")
	}
	scdData := childByLocalName(scd, "SubjectConfirmationData")
	if scdData == nil {
		t.Fatal("no SubjectConfirmationData in assertion")
	}
	if attr := scdData.SelectAttr("InResponseTo"); attr != nil {
		t.Errorf("SubjectConfirmationData has InResponseTo=%q; unsolicited assertion must omit it", attr.Value)
	}
	// The Recipient (delivery target) must be the default ACS.
	if rcpt := scdData.SelectAttr("Recipient"); rcpt == nil || rcpt.Value != testACSURL {
		t.Errorf("SubjectConfirmationData Recipient = %v, want default ACS %q", rcpt, testACSURL)
	}

	// (c) crewjam SP-side parse with InResponseTo="" (unsolicited) accepts it and
	// recovers a non-empty NameID. Passing []string{""} models the SP accepting
	// an unsolicited assertion.
	var resp crewjam.Response
	if err := xml.Unmarshal(respXML, &resp); err != nil {
		t.Fatalf("unmarshal Response: %v", err)
	}
	if resp.InResponseTo != "" {
		t.Errorf("Response.InResponseTo = %q, want empty (unsolicited)", resp.InResponseTo)
	}
	if resp.Destination != testACSURL {
		t.Errorf("Response.Destination = %q, want default ACS %q", resp.Destination, testACSURL)
	}

	// (d) a saml_session row was persisted.
	rows := h.q.sessions()
	if len(rows) != 1 {
		t.Fatalf("saml_session rows = %d, want 1", len(rows))
	}
	if rows[0].SessionID != "sess-abc" || rows[0].SpID != 7 {
		t.Errorf("persisted saml_session = %+v, want SessionID=sess-abc SpID=7", rows[0])
	}
	if !rows[0].NotOnOrAfter.Valid {
		t.Error("saml_session NotOnOrAfter must be Valid")
	}
	if rows[0].NameID == "" {
		t.Error("saml_session NameID is empty")
	}

	// (e) an audit record with reason idp_initiated.
	recs := h.auditW.all()
	if len(recs) != 1 {
		t.Fatalf("audit records = %d, want 1", len(recs))
	}
	if recs[0].Factor != "saml_sp" || recs[0].Event != "use" {
		t.Errorf("audit record = factor %q event %q, want saml_sp/use", recs[0].Factor, recs[0].Event)
	}
	if recs[0].AccountID == nil || *recs[0].AccountID != acct.ID {
		t.Errorf("audit AccountID = %v, want %d", recs[0].AccountID, acct.ID)
	}
	if reason, _ := recs[0].Detail["reason"].(string); reason != "idp_initiated" {
		t.Errorf("audit Detail[reason] = %v, want idp_initiated", recs[0].Detail["reason"])
	}
}

// TestSSOInitRejectsOversizeRelayState guards N7: a RelayState longer than the
// spec's 80-byte limit is rejected before any assertion is issued, even for an
// opted-in SP with a live session. The oversize RelayState is a malformed
// request the IdP cannot safely answer, so it dead-ends at the SPA /error page.
func TestSSOInitRejectsOversizeRelayState(t *testing.T) {
	sp := ssoSP()
	sp.AllowIdpInitiated = true
	h := newSSOHarness(t, sp)
	acct := testAccount()

	oversize := strings.Repeat("x", maxRelayStateBytes+1)
	req := idpInitRequest(testSPEntityID, oversize, liveSession(acct))
	rec := httptest.NewRecorder()
	h.idp.HandleIdPInitiated(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 /error for oversize RelayState; body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/error?error=saml_request_invalid&ref=") {
		t.Fatalf("Location = %q, want /error?error=saml_request_invalid prefix", loc)
	}
}

func TestSSOInitOptOutForbidden(t *testing.T) {
	sp := ssoSP()
	sp.AllowIdpInitiated = false // explicit: not opted in
	h := newSSOHarness(t, sp)

	req := idpInitRequest(testSPEntityID, "", liveSession(testAccount()))
	rec := httptest.NewRecorder()
	h.idp.HandleIdPInitiated(rec, req)

	// An SP that did not opt into IdP-initiated SSO is a human-facing dead-end
	// (the IdP refuses to emit an unsolicited assertion), so it 302-redirects to
	// the SPA /error page rather than returning a 403 plaintext.
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 /error (SP not opted in); body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/error?error=saml_idp_init_disabled&ref=") {
		t.Fatalf("Location = %q, want /error?error=saml_idp_init_disabled prefix", loc)
	}
	if body := rec.Body.String(); samlResponseRe.MatchString(body) {
		t.Errorf("opt-out SP must not get a SAMLResponse; body=%s", body)
	}
	if rows := h.q.sessions(); len(rows) != 0 {
		t.Errorf("saml_session rows = %d, want 0 (no assertion issued)", len(rows))
	}
}

func TestSSOInitNoSessionRedirectsToLogin(t *testing.T) {
	sp := ssoSP()
	sp.AllowIdpInitiated = true
	h := newSSOHarness(t, sp)

	req := idpInitRequest(testSPEntityID, "", nil) // no session
	rec := httptest.NewRecorder()
	h.idp.HandleIdPInitiated(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 login bounce; body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, testIdPOrigin+"/login?return_to=") {
		t.Fatalf("Location = %q, want %q login bounce", loc, testIdPOrigin+"/login")
	}
	// Must NOT bounce to any SP.
	if strings.Contains(loc, "sp.example.test/saml/acs") {
		t.Errorf("login bounce must not target an SP ACS; got %q", loc)
	}
}

func TestSSOInitUnknownSPDirect400(t *testing.T) {
	sp := ssoSP()
	sp.AllowIdpInitiated = true
	h := newSSOHarness(t, sp)

	req := idpInitRequest("https://stranger.example/metadata", "", liveSession(testAccount()))
	rec := httptest.NewRecorder()
	h.idp.HandleIdPInitiated(rec, req)

	// An unknown sp is on the untrusted side of the open-redirect guard: it
	// dead-ends at the SPA /error page (saml_sp_unknown) and MUST NOT redirect to
	// any SP-supplied URL.
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 /error (unknown sp); body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/error?error=saml_sp_unknown&ref=") {
		t.Fatalf("Location = %q, want /error?error=saml_sp_unknown prefix", loc)
	}
	if strings.Contains(loc, "stranger.example") || strings.Contains(loc, "sp.example.test") {
		t.Errorf("unknown-SP error must NOT redirect to any SP; got Location=%q", loc)
	}
}

func TestSSOInitEmptySPParamDirect400(t *testing.T) {
	sp := ssoSP()
	sp.AllowIdpInitiated = true
	h := newSSOHarness(t, sp)

	// No sp= query param at all.
	req := httptest.NewRequest(http.MethodGet, testIdPOrigin+"/saml/sso/init", nil)
	req = req.WithContext(authn.WithSession(req.Context(), liveSession(testAccount())))
	rec := httptest.NewRecorder()
	h.idp.HandleIdPInitiated(rec, req)

	// An empty sp param is a malformed launcher request the IdP cannot answer; it
	// dead-ends at the SPA /error page (saml_request_invalid), never an SP URL.
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 /error (empty sp param); body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/error?error=saml_request_invalid&ref=") {
		t.Fatalf("Location = %q, want /error?error=saml_request_invalid prefix", loc)
	}
	if strings.Contains(loc, "sp.example.test") {
		t.Errorf("empty-sp error must NOT redirect to any SP; got Location=%q", loc)
	}
}

func TestSSOInitDisabledSentinelRedirectsToLogin(t *testing.T) {
	sp := ssoSP()
	sp.AllowIdpInitiated = true
	h := newSSOHarness(t, sp)

	// All flavours of the disabled/not-authenticated sentinel must bounce to
	// login WITHOUT panicking on a sess.Account / sess.Data deref, and WITHOUT
	// issuing an assertion. Mirrors sso_test.go's TestSSODisabledSentinelRedirectsToLogin.
	cases := []struct {
		name string
		sess *authn.Session
	}{
		{
			name: "nil Data sentinel",
			sess: &authn.Session{Account: testAccount(), Data: nil},
		},
		{
			name: "nil Account",
			sess: &authn.Session{Account: nil, Data: &authn.SessionData{SessionID: "x", AccountID: 42}},
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
			req := idpInitRequest(testSPEntityID, "", tc.sess)
			rec := httptest.NewRecorder()
			h.idp.HandleIdPInitiated(rec, req) // must not panic
			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302 login bounce; body=%s", rec.Code, rec.Body.String())
			}
			if !strings.HasPrefix(rec.Header().Get("Location"), testIdPOrigin+"/login?return_to=") {
				t.Fatalf("Location = %q, want login bounce", rec.Header().Get("Location"))
			}
			if body := rec.Body.String(); samlResponseRe.MatchString(body) {
				t.Errorf("not-authenticated sentinel must not get a SAMLResponse; body=%s", body)
			}
			if rows := h.q.sessions(); len(rows) != 0 {
				t.Errorf("saml_session rows = %d, want 0 (no assertion issued)", len(rows))
			}
		})
	}
}

func TestSSOInitRateLimited(t *testing.T) {
	sp := ssoSP()
	sp.AllowIdpInitiated = true
	h := newSSOHarness(t, sp)
	acct := testAccount()

	// samlSSORateMax (60) issuances per account/SP per window are allowed; the
	// next one must be refused with 429 + Retry-After, and must NOT issue an
	// assertion or persist a saml_session row. The harness's real RateLimiter
	// (authn.NewRateLimiter) records every Allow call, so driving past the cap
	// exercises the same guard HandleSSO uses.
	for n := 0; n < samlSSORateMax; n++ {
		req := idpInitRequest(testSPEntityID, "", liveSession(acct))
		rec := httptest.NewRecorder()
		h.idp.HandleIdPInitiated(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200 (under limit); body=%s", n, rec.Code, rec.Body.String())
		}
	}

	sessionsBefore := len(h.q.sessions())

	// The (max+1)th request trips the limit. Over-limit is a browser-navigated
	// dead-end (no SAML response can be safely issued), so it 302-redirects to the
	// SPA /error page (rate_limited) while still setting Retry-After.
	req := idpInitRequest(testSPEntityID, "", liveSession(acct))
	rec := httptest.NewRecorder()
	h.idp.HandleIdPInitiated(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("over-limit status = %d, want 302 /error; body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/error?error=rate_limited&ref=") {
		t.Fatalf("Location = %q, want /error?error=rate_limited prefix", loc)
	}
	if ra := rec.Header().Get("Retry-After"); ra == "" {
		t.Errorf("over-limit response missing Retry-After header")
	}
	if body := rec.Body.String(); samlResponseRe.MatchString(body) {
		t.Errorf("over-limit request must not get a SAMLResponse; body=%s", body)
	}
	if got := len(h.q.sessions()); got != sessionsBefore {
		t.Errorf("over-limit request persisted a saml_session row: before=%d after=%d", sessionsBefore, got)
	}
}
