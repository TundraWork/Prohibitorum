package pat

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseHeader(t *testing.T) {
	tok := Prefix + "abcDEF123_-"
	ok, err := ParseHeader([]string{tok})
	if err != nil || ok != tok {
		t.Fatalf("valid header: %q, %v", ok, err)
	}
	bad := map[string][]string{
		"missing":         nil,
		"empty value":     {""},
		"empty list item": {},
		"multiple":        {tok, tok},
		"wrong prefix":    {"Bearer " + tok},
		"other prefix":    {"prohibitorum_x_abc"},
		"leading space":   {" " + tok},
		"trailing space":  {tok + " "},
		"inner space":     {tok + " x"},
		"tab":             {tok + "\t"},
		"comma":           {tok + "," + tok},
	}
	for name, v := range bad {
		if _, err := ParseHeader(v); err == nil {
			t.Errorf("%s: accepted %q", name, v)
		}
	}
}

func TestSelectsManagementAuth(t *testing.T) {
	cases := []struct {
		path   string
		header bool
		want   bool
	}{
		{"/api/prohibitorum/me", true, true},
		{"/api/prohibitorum/forward-auth-apps", true, true},
		{"/api/prohibitorum/forward-auth/verify", true, false},
		{"/oauth/userinfo", true, false},
		{"/api/prohibitorum/me", false, false},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", c.path, nil)
		if c.header {
			r.Header[http.CanonicalHeaderKey(HeaderName)] = []string{""}
		}
		if got := SelectsManagementAuth(r); got != c.want {
			t.Errorf("%s header=%v: got %v, want %v", c.path, c.header, got, c.want)
		}
	}
}
