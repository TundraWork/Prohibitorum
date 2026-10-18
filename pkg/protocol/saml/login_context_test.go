package saml

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"prohibitorum/pkg/db"
)

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// spInitiatedContextURL builds the /saml/sso return_to the login bounce
// carries: the harness's own signed redirect-binding AuthnRequest under this
// IdP's origin.
func spInitiatedContextURL(t *testing.T, h *ssoHarness) *url.URL {
	t.Helper()
	req := h.request(t, "_ctx-label", nil)
	return mustParse(t, req.URL.String())
}

func TestLoginContext_SPInitiated(t *testing.T) {
	cases := []struct {
		name  string
		sp    func() db.SamlSp
		label string
	}{
		{"named SP", func() db.SamlSp { sp := ssoSP(); sp.DisplayName = "Smoke SP"; return sp }, "Smoke SP"},
		{"empty name falls back to entity id", ssoSP, testSPEntityID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newSSOHarness(t, tc.sp())
			app, ok, err := h.idp.LoginContext(context.Background(), spInitiatedContextURL(t, h))
			if err != nil || !ok {
				t.Fatalf("app=%+v ok=%v err=%v", app, ok, err)
			}
			if app.Label != tc.label {
				t.Fatalf("label=%q want %q", app.Label, tc.label)
			}
			if app.Host != "sp.example.test" || app.SPID != 7 {
				t.Fatalf("host=%q spID=%d, want sp.example.test and 7", app.Host, app.SPID)
			}
		})
	}
}

func TestLoginContext_SPInitiatedRejections(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ssoHarness, *url.Values)
	}{
		{"undecodable request", func(_ *ssoHarness, v *url.Values) { v.Set("SAMLRequest", "!!!not-base64!!!") }},
		{"unknown SP", func(_ *ssoHarness, v *url.Values) { v.Set("SAMLRequest", "c2Ftc2xl") }},
		{"disabled SP", func(h *ssoHarness, _ *url.Values) { h.q.sp.Disabled = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sp := ssoSP()
			sp.DisplayName = "Smoke SP"
			h := newSSOHarness(t, sp)
			u := spInitiatedContextURL(t, h)
			query := u.Query()
			tc.mutate(h, &query)
			u.RawQuery = query.Encode()
			app, ok, err := h.idp.LoginContext(context.Background(), u)
			if err != nil || ok || app != (LoginApplication{}) {
				t.Fatalf("app=%+v ok=%v err=%v, want no context", app, ok, err)
			}
		})
	}
}

func TestLoginContext_IdPInitiated(t *testing.T) {
	sp := ssoSP()
	sp.DisplayName = "Smoke SP"
	sp.AllowIdpInitiated = true
	h := newSSOHarness(t, sp)
	u := mustParse(t, testIdPOrigin+"/saml/sso/init?sp="+url.QueryEscape(testSPEntityID))
	app, ok, err := h.idp.LoginContext(context.Background(), u)
	if err != nil || !ok || app.Label != "Smoke SP" {
		t.Fatalf("app=%+v ok=%v err=%v", app, ok, err)
	}
	// IdP-initiated posts to the SP's registered default ACS.
	if app.Host != "sp.example.test" || app.SPID != 7 {
		t.Fatalf("host=%q spID=%d", app.Host, app.SPID)
	}

	h.q.sp.DisplayName = ""
	app, ok, err = h.idp.LoginContext(context.Background(), u)
	if err != nil || !ok || app.Label != testSPEntityID {
		t.Fatalf("fallback app=%+v ok=%v err=%v", app, ok, err)
	}

	// No registered ACS: the application is still named, without a host.
	h.q.acs = nil
	app, ok, err = h.idp.LoginContext(context.Background(), u)
	if err != nil || !ok || app.Host != "" {
		t.Fatalf("no ACS: app=%+v ok=%v err=%v", app, ok, err)
	}

	h.q.sp.AllowIdpInitiated = false
	if _, ok, err = h.idp.LoginContext(context.Background(), u); err != nil || ok {
		t.Fatalf("opted out: ok=%v err=%v", ok, err)
	}

	// A disabled SP is as unregistered on this path as on /saml/sso itself.
	h.q.sp.AllowIdpInitiated = true
	h.q.sp.Disabled = true
	if _, ok, err = h.idp.LoginContext(context.Background(), u); err != nil || ok {
		t.Fatalf("disabled SP: ok=%v err=%v", ok, err)
	}
	if _, ok, err = h.idp.LoginContext(context.Background(), mustParse(t, testIdPOrigin+"/saml/sso/init?sp=https://unknown.example.test")); err != nil || ok {
		t.Fatalf("unknown SP: ok=%v err=%v", ok, err)
	}
	if _, ok, err = h.idp.LoginContext(context.Background(), mustParse(t, testIdPOrigin+"/saml/sso/init")); err != nil || ok {
		t.Fatalf("missing sp param: ok=%v err=%v", ok, err)
	}
}

// Reading the context must leave the single-use AuthnRequest ID spendable: the
// login bounce returns to /saml/sso with the SAME SAMLRequest, and the issue
// path there consumes the ID exactly once.
func TestLoginContext_DoesNotConsumeAuthnRequestID(t *testing.T) {
	sp := ssoSP()
	sp.DisplayName = "Smoke SP"
	h := newSSOHarness(t, sp)
	u := spInitiatedContextURL(t, h)
	app, ok, err := h.idp.LoginContext(context.Background(), u)
	if err != nil || !ok || app.Label != "Smoke SP" {
		t.Fatalf("app=%+v ok=%v err=%v", app, ok, err)
	}
	// The harness mints the request with the ID "_ctx-label" (see
	// spInitiatedContextURL). consumeAuthnRequestID succeeds only on the
	// first spend of the ID, so a nil error here proves the label read left
	// it unconsumed, and a second consume must be refused.
	if cerr := h.idp.consumeAuthnRequestID(context.Background(), testSPEntityID, "_ctx-label"); cerr != nil {
		t.Fatalf("the login-context read consumed the AuthnRequest ID: %v", cerr)
	}
	if cerr := h.idp.consumeAuthnRequestID(context.Background(), testSPEntityID, "_ctx-label"); cerr == nil || !errors.Is(cerr, ErrReplayedRequest) {
		t.Fatalf("second consume: err=%v, want ErrReplayedRequest", cerr)
	}
	// A second label read must still answer; the spent ID does not change it.
	if _, ok, err = h.idp.LoginContext(context.Background(), u); err != nil || !ok {
		t.Fatalf("second read: ok=%v err=%v", ok, err)
	}
}

func TestLoginContext_StorageFailure(t *testing.T) {
	sp := ssoSP()
	sp.DisplayName = "Smoke SP"
	sp.AllowIdpInitiated = true
	h := newSSOHarness(t, sp)
	h.q.spErr = errors.New("database down")
	_, _, err := h.idp.LoginContext(context.Background(), mustParse(t, testIdPOrigin+"/saml/sso/init?sp="+url.QueryEscape(testSPEntityID)))
	if err == nil {
		t.Fatal("want storage error")
	}
}
