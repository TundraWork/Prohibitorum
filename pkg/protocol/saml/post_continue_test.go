package saml

import (
	"context"
	"encoding/xml"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	crewjam "github.com/crewjam/saml"

	"prohibitorum/pkg/authn"
)

var relayValueRe = regexp.MustCompile(`name="RelayState" value="([^"]*)"`)

// signedPost is a signed HTTP-POST AuthnRequest from the harness's SP.
func (h *ssoHarness) signedPost(t *testing.T, id string, sess *authn.Session, opts ...func(*authnReqOpts)) *http.Request {
	t.Helper()
	o := authnReqOpts{
		id: id, destination: testSSOURL, acsURL: testACSURL,
		hasRelay: true, relayState: "rs-post+state/1",
		sign: true, signKey: h.spKey,
	}
	for _, fn := range opts {
		fn(&o)
	}
	req := buildAuthnPost(t, o, spCertDER(t, h.q.spKeys[0].CertPem))
	req.RemoteAddr = "203.0.113.7:54321"
	req.Header.Set("User-Agent", "test-agent/1.0")
	if sess != nil {
		req = req.WithContext(authn.WithSession(req.Context(), sess))
	}
	return req
}

// loginReturnTo is the return_to of a bounce to the sign-in page.
func loginReturnTo(t *testing.T, rec *httptest.ResponseRecorder) *url.URL {
	t.Helper()
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 to the sign-in page; body=%s", rec.Code, rec.Body.String())
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || loc.Path != "/login" {
		t.Fatalf("Location = %q, want the sign-in page", rec.Header().Get("Location"))
	}
	rt, err := url.Parse(loc.Query().Get("return_to"))
	if err != nil {
		t.Fatal(err)
	}
	return rt
}

// continueAt is the GET the browser makes after signing in.
func continueAt(rt *url.URL, sess *authn.Session) *http.Request {
	req := httptest.NewRequest(http.MethodGet, rt.RequestURI(), nil)
	req.RemoteAddr = "203.0.113.7:54321"
	req.Header.Set("User-Agent", "test-agent/1.0")
	if sess != nil {
		req = req.WithContext(authn.WithSession(req.Context(), sess))
	}
	return req
}

// A POST-binding request that has to wait for a sign-in comes back as the
// same request: the sign-in returns with the key of its kept form, and the
// assertion answers the request the SP sent, with its RelayState.
func TestSSOPostBindingSurvivesSignIn(t *testing.T) {
	h := newSSOHarness(t, ssoSP())

	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, h.signedPost(t, "_post-signin", nil))
	rt := loginReturnTo(t, rec)
	if rt.String() != testIdPOrigin+"/saml/sso?post="+rt.Query().Get("post") || rt.Query().Get("post") == "" {
		t.Fatalf("return_to = %q, want this IdP's /saml/sso with only the kept form's key", rt)
	}

	sess := liveSession(testAccount())
	rec = httptest.NewRecorder()
	h.idp.HandleSSO(rec, continueAt(rt, sess))
	if rec.Code != http.StatusOK {
		t.Fatalf("after sign-in: status = %d, want the auto-POST; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	action, respXML := decodeAutoPost(t, body)
	if action != testACSURL {
		t.Fatalf("form action = %q, want %q", action, testACSURL)
	}
	if m := relayValueRe.FindStringSubmatch(body); m == nil || html.UnescapeString(m[1]) != "rs-post+state/1" {
		t.Errorf("auto-POST lost the SP's RelayState; body=%s", body)
	}
	idpMetaXML, err := h.idp.idpMetadata(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var idpED crewjam.EntityDescriptor
	if err := xml.Unmarshal(idpMetaXML, &idpED); err != nil {
		t.Fatal(err)
	}
	acs, _ := url.Parse(testACSURL)
	sp := crewjam.ServiceProvider{EntityID: testSPEntityID, AcsURL: *acs, IDPMetadata: &idpED}
	if _, err := sp.ParseXMLResponse(respXML, []string{"_post-signin"}, *acs); err != nil {
		t.Fatalf("the SP rejects the Response to its own request: %v", err)
	}

	// The request is single-use however it arrives: once spent, its key is
	// dropped, so following the link again ends on the expired-request page
	// rather than being answered twice.
	rec = httptest.NewRecorder()
	h.idp.HandleSSO(rec, continueAt(rt, sess))
	if samlResponseRe.MatchString(rec.Body.String()) {
		t.Fatal("a second replay of the kept form issued another Response")
	}
	assertErrorRedirect(t, rec, "saml_request_expired")
}

// A key the IdP never issued, or whose form has expired, says the sign-in
// took too long: it is not the app's request that was wrong.
func TestSSOPostContinueUnknownKey(t *testing.T) {
	h := newSSOHarness(t, ssoSP())
	rec := httptest.NewRecorder()
	u, _ := url.Parse(testSSOURL + "?post=not-a-key")
	h.idp.HandleSSO(rec, continueAt(u, liveSession(testAccount())))
	assertErrorRedirect(t, rec, "saml_request_expired")
}

// The replay runs every check again: an SP disabled, or whose signing
// certificate was replaced, while the reader was signing in is refused.
func TestSSOPostReplayRunsChecksAgain(t *testing.T) {
	for name, change := range map[string]func(h *ssoHarness){
		"SP disabled": func(h *ssoHarness) { h.q.sp.Disabled = true },
		"signing certificate replaced": func(h *ssoHarness) {
			_, otherCert := testSPKey(t)
			h.q.spKeys[0].CertPem = otherCert
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newSSOHarness(t, ssoSP())
			rec := httptest.NewRecorder()
			h.idp.HandleSSO(rec, h.signedPost(t, "_post-recheck", nil))
			rt := loginReturnTo(t, rec)
			change(h)
			rec = httptest.NewRecorder()
			h.idp.HandleSSO(rec, continueAt(rt, liveSession(testAccount())))
			if samlResponseRe.MatchString(rec.Body.String()) {
				t.Fatal("the replay issued a Response after the SP changed")
			}
			if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/error?error=") {
				t.Fatalf("status %d Location %q, want the error page", rec.Code, rec.Header().Get("Location"))
			}
		})
	}
}

// Anyone can post before signing in, so what is kept is bounded: a form too
// large to be a real request is refused rather than kept, and the same form
// posted again reuses its entry. RelayState is not signed in the POST
// binding, so the same request with another RelayState is another form.
func TestSSOPostKeptFormIsBounded(t *testing.T) {
	h := newSSOHarness(t, ssoSP())

	big := httptest.NewRequest(http.MethodPost, testSSOURL, strings.NewReader(url.Values{
		"SAMLRequest": {strings.Repeat("A", maxKeptSAMLRequest+4)},
	}.Encode()))
	big.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, err := h.idp.keepPostRequest(context.Background(), withParsedForm(t, big)); !errors.Is(err, ErrOversizeRequest) {
		t.Fatalf("oversized form: err = %v, want ErrOversizeRequest", err)
	}

	// One captured request, posted as it is or with its RelayState swapped.
	captured := h.signedPost(t, "_post-bounded", nil)
	if err := captured.ParseForm(); err != nil {
		t.Fatal(err)
	}
	samlRequest := captured.PostForm.Get("SAMLRequest")
	keyOf := func(relay string) string {
		body := url.Values{"SAMLRequest": {samlRequest}, "RelayState": {relay}}.Encode()
		req := httptest.NewRequest(http.MethodPost, testSSOURL, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.idp.HandleSSO(rec, req)
		return loginReturnTo(t, rec).Query().Get("post")
	}
	first := keyOf("rs-a")
	if again := keyOf("rs-a"); again != first {
		t.Errorf("the same form posted again kept a second entry (%q, %q)", first, again)
	}
	if other := keyOf("rs-b"); other == first {
		t.Error("a form with another RelayState reused the first form's entry")
	}
}

func withParsedForm(t *testing.T, r *http.Request) *http.Request {
	t.Helper()
	if err := r.ParseForm(); err != nil {
		t.Fatal(err)
	}
	return r
}

// ForceAuthn on a POST-binding request sends the reader to sign in again and
// back to the same request, by its kept form's key and the reauth nonce.
func TestSSOPostBindingForceAuthnKeepsRequest(t *testing.T) {
	h := newSSOHarness(t, ssoSP())
	sess := liveSession(testAccount())
	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, h.signedPost(t, "_post-force", sess, func(o *authnReqOpts) { o.forceAuthn = true }))
	rt := loginReturnTo(t, rec)
	if rt.Path != "/saml/sso" || rt.Query().Get("post") == "" || rt.Query().Get("reauth") == "" {
		t.Fatalf("return_to = %q, want /saml/sso with the kept form's key and a reauth nonce", rt)
	}
	// Coming back without the fresh sign-in the nonce asks for bounces again,
	// still to the same kept request.
	rec = httptest.NewRecorder()
	h.idp.HandleSSO(rec, continueAt(rt, sess))
	again := loginReturnTo(t, rec)
	if again.Query().Get("post") != rt.Query().Get("post") {
		t.Fatalf("re-bounce return_to = %q, want the same kept request %q", again, rt.Query().Get("post"))
	}
}

// The sign-in page names the SP of a POST-binding request from its kept
// form, without spending the request.
func TestLoginContextNamesKeptPostRequest(t *testing.T) {
	h := newSSOHarness(t, ssoSP())
	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, h.signedPost(t, "_post-context", nil))
	rt := loginReturnTo(t, rec)

	app, ok, err := h.idp.LoginContext(context.Background(), rt)
	if err != nil || !ok || app.SPID != ssoSP().ID || app.Host != "sp.example.test" {
		t.Fatalf("LoginContext = %+v, %v, %v; want the request's SP", app, ok, err)
	}
	unknown, _ := url.Parse(testSSOURL + "?post=not-a-key")
	if _, ok, err := h.idp.LoginContext(context.Background(), unknown); ok || err != nil {
		t.Fatalf("unknown key: ok=%v err=%v, want no application and no error", ok, err)
	}

	// Naming it did not spend it: the request still answers after sign-in.
	rec = httptest.NewRecorder()
	h.idp.HandleSSO(rec, continueAt(rt, liveSession(testAccount())))
	if !samlResponseRe.MatchString(rec.Body.String()) {
		t.Fatalf("after LoginContext the request no longer issues; status %d", rec.Code)
	}
}
