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
	if _, ok, err = h.idp.LoginContextLabel(context.Background(), mustParse(t, testIdPOrigin+"/saml/sso/init?sp=https://unknown.example.test")); err != nil || ok {
		t.Fatalf("unknown SP: ok=%v err=%v", ok, err)
	}
	if _, ok, err = h.idp.LoginContextLabel(context.Background(), mustParse(t, testIdPOrigin+"/saml/sso/init")); err != nil || ok {
		t.Fatalf("missing sp param: ok=%v err=%v", ok, err)
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
