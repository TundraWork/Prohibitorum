package steam

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestBuildAuthURL(t *testing.T) {
	got := BuildAuthURL("https://idp.example.com", "https://idp.example.com/cb?state=abc")
	if !strings.HasPrefix(got, "https://steamcommunity.com/openid/login?") {
		t.Fatalf("wrong endpoint: %s", got)
	}
	u, _ := url.Parse(got)
	q := u.Query()
	if q.Get("openid.mode") != "checkid_setup" || q.Get("openid.claimed_id") != identifierSelect ||
		q.Get("openid.return_to") != "https://idp.example.com/cb?state=abc" {
		t.Fatalf("bad params: %v", q)
	}
}

func steamMock(t *testing.T, valid bool) (*httptest.Server, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.FormValue("openid.mode") != "check_authentication" {
			t.Errorf("mock got mode %q, want check_authentication", r.FormValue("openid.mode"))
		}
		if valid {
			_, _ = w.Write([]byte("ns:http://specs.openid.net/auth/2.0\nis_valid:true\n"))
		} else {
			_, _ = w.Write([]byte("ns:http://specs.openid.net/auth/2.0\nis_valid:false\n"))
		}
	}))
	restore := SetEndpoints(srv.URL, srv.URL)
	return srv, func() { srv.Close(); restore() }
}

func validParams(returnTo, claimedID string) url.Values {
	return url.Values{
		"openid.mode":       {"id_res"},
		"openid.return_to":  {returnTo},
		"openid.claimed_id": {claimedID},
		"openid.identity":   {claimedID},
		"openid.sig":        {"whatever"},
		"openid.signed":     {"mode,return_to,claimed_id,identity"},
	}
}

func TestVerify(t *testing.T) {
	const rt = "https://idp.example.com/cb?state=abc"
	const good = "https://steamcommunity.com/openid/id/76561198000000000"

	t.Run("valid", func(t *testing.T) {
		_, done := steamMock(t, true)
		defer done()
		id, err := Verify(context.Background(), http.DefaultClient, validParams(rt, good), rt)
		if err != nil || id != "76561198000000000" {
			t.Fatalf("id=%q err=%v", id, err)
		}
	})
	t.Run("is_valid false", func(t *testing.T) {
		_, done := steamMock(t, false)
		defer done()
		if _, err := Verify(context.Background(), http.DefaultClient, validParams(rt, good), rt); err == nil {
			t.Fatal("expected error on is_valid:false")
		}
	})
	t.Run("return_to mismatch", func(t *testing.T) {
		_, done := steamMock(t, true)
		defer done()
		if _, err := Verify(context.Background(), http.DefaultClient, validParams(rt, good), "https://evil.example/cb"); err == nil {
			t.Fatal("expected return_to mismatch error")
		}
	})
	t.Run("spoofed claimed_id host", func(t *testing.T) {
		_, done := steamMock(t, true)
		defer done()
		bad := "https://steamcommunity.com.evil.example/openid/id/76561198000000000"
		if _, err := Verify(context.Background(), http.DefaultClient, validParams(rt, bad), rt); err == nil {
			t.Fatal("expected claimed_id host rejection")
		}
	})
	t.Run("non-numeric claimed_id", func(t *testing.T) {
		_, done := steamMock(t, true)
		defer done()
		bad := "https://steamcommunity.com/openid/id/notanid"
		if _, err := Verify(context.Background(), http.DefaultClient, validParams(rt, bad), rt); err == nil {
			t.Fatal("expected claimed_id format rejection")
		}
	})
	t.Run("wrong mode", func(t *testing.T) {
		_, done := steamMock(t, true)
		defer done()
		p := validParams(rt, good)
		p.Set("openid.mode", "cancel")
		if _, err := Verify(context.Background(), http.DefaultClient, p, rt); err == nil {
			t.Fatal("expected wrong-mode rejection")
		}
	})
	t.Run("signed omits claimed_id", func(t *testing.T) {
		// is_valid:true but openid.signed does not cover claimed_id — the RP
		// must reject this per OpenID 2.0 §11.4.2 to prevent SteamID spoofing.
		_, done := steamMock(t, true)
		defer done()
		p := validParams(rt, good)
		p.Set("openid.signed", "mode,return_to") // identity fields absent
		if _, err := Verify(context.Background(), http.DefaultClient, p, rt); err == nil {
			t.Fatal("expected error when openid.signed omits claimed_id/identity")
		}
	})
	t.Run("signed omits identity", func(t *testing.T) {
		// Covers the case where claimed_id is signed but identity is not.
		_, done := steamMock(t, true)
		defer done()
		p := validParams(rt, good)
		p.Set("openid.signed", "mode,return_to,claimed_id") // identity absent
		if _, err := Verify(context.Background(), http.DefaultClient, p, rt); err == nil {
			t.Fatal("expected error when openid.signed omits identity")
		}
	})
}
