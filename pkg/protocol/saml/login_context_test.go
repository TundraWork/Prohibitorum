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

func TestLoginContextLabel_SPInitiated(t *testing.T) {
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
			label, ok, err := h.idp.LoginContextLabel(context.Background(), spInitiatedContextURL(t, h))
			if err != nil || !ok {
				t.Fatalf("label=%q ok=%v err=%v", label, ok, err)
			}
			if label != tc.label {
				t.Fatalf("label=%q want %q", label, tc.label)
			}
		})
	}
}

func TestLoginContextLabel_SPInitiatedRejections(t *testing.T) {
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
			label, ok, err := h.idp.LoginContextLabel(context.Background(), u)
			if err != nil || ok || label != "" {
				t.Fatalf("label=%q ok=%v err=%v, want no context", label, ok, err)
			}
		})
	}
}

func TestLoginContextLabel_IdPInitiated(t *testing.T) {
	sp := ssoSP()
	sp.DisplayName = "Smoke SP"
	sp.AllowIdpInitiated = true
	h := newSSOHarness(t, sp)
	u := mustParse(t, testIdPOrigin+"/saml/sso/init?sp="+url.QueryEscape(testSPEntityID))
	label, ok, err := h.idp.LoginContextLabel(context.Background(), u)
	if err != nil || !ok || label != "Smoke SP" {
		t.Fatalf("label=%q ok=%v err=%v", label, ok, err)
	}

	h.q.sp.DisplayName = ""
	label, ok, err = h.idp.LoginContextLabel(context.Background(), u)
	if err != nil || !ok || label != testSPEntityID {
		t.Fatalf("fallback label=%q ok=%v err=%v", label, ok, err)
	}

	h.q.sp.AllowIdpInitiated = false
	if _, ok, err = h.idp.LoginContextLabel(context.Background(), u); err != nil || ok {
		t.Fatalf("opted out: ok=%v err=%v", ok, err)
	}

	// A disabled SP is as unregistered on this path as on /saml/sso itself.
	h.q.sp.AllowIdpInitiated = true
	h.q.sp.Disabled = true
	if _, ok, err = h.idp.LoginContextLabel(context.Background(), u); err != nil || ok {
		t.Fatalf("disabled SP: ok=%v err=%v", ok, err)
	}
	if _, ok, err = h.idp.LoginContextLabel(context.Background(), mustParse(t, testIdPOrigin+"/saml/sso/init?sp=https://unknown.example.test")); err != nil || ok {
		t.Fatalf("unknown SP: ok=%v err=%v", ok, err)
	}
	if _, ok, err = h.idp.LoginContextLabel(context.Background(), mustParse(t, testIdPOrigin+"/saml/sso/init")); err != nil || ok {
		t.Fatalf("missing sp param: ok=%v err=%v", ok, err)
	}
}

// Reading the label must leave the single-use AuthnRequest ID spendable: the
// login bounce returns to /saml/sso with the SAME SAMLRequest, and the issue
// path there consumes the ID exactly once.
func TestLoginContextLabel_DoesNotConsumeAuthnRequestID(t *testing.T) {
	sp := ssoSP()
	sp.DisplayName = "Smoke SP"
	h := newSSOHarness(t, sp)
	u := spInitiatedContextURL(t, h)
	label, ok, err := h.idp.LoginContextLabel(context.Background(), u)
	if err != nil || !ok || label != "Smoke SP" {
		t.Fatalf("label=%q ok=%v err=%v", label, ok, err)
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
	if _, ok, err = h.idp.LoginContextLabel(context.Background(), u); err != nil || !ok {
		t.Fatalf("second read: ok=%v err=%v", ok, err)
	}
}

func TestLoginContextLabel_StorageFailure(t *testing.T) {
	sp := ssoSP()
	sp.DisplayName = "Smoke SP"
	sp.AllowIdpInitiated = true
	h := newSSOHarness(t, sp)
	h.q.spErr = errors.New("database down")
	_, _, err := h.idp.LoginContextLabel(context.Background(), mustParse(t, testIdPOrigin+"/saml/sso/init?sp="+url.QueryEscape(testSPEntityID)))
	if err == nil {
		t.Fatal("want storage error")
	}
}
