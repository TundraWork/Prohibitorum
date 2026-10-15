package oidc

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSActualRequestKeepsHeadersOnErrorResponses(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="prohibitorum"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
	rec := httptest.NewRecorder()
	CORS(inner.ServeHTTP)(rec, httptest.NewRequest(http.MethodPost, "/oauth/token", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", got)
	}
	if got := rec.Header().Get("Access-Control-Expose-Headers"); got != "WWW-Authenticate, Retry-After" {
		t.Errorf("Access-Control-Expose-Headers = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("Access-Control-Allow-Credentials = %q, want unset", got)
	}
}

func TestPreflightPost(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/oauth/token", nil)
	req.Header.Set("Origin", "https://spa.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	Preflight("POST")(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "POST" {
		t.Errorf("Access-Control-Allow-Methods = %q, want POST", got)
	}
	hdrs := rec.Header().Get("Access-Control-Allow-Headers")
	for _, want := range []string{"Authorization", "Content-Type"} {
		found := false
		for _, h := range splitComma(hdrs) {
			if h == want {
				found = true
			}
		}
		if !found {
			t.Errorf("Access-Control-Allow-Headers %q missing %q", hdrs, want)
		}
	}
	if got := rec.Header().Get("Access-Control-Max-Age"); got != "600" {
		t.Errorf("Access-Control-Max-Age = %q, want 600", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("Access-Control-Allow-Credentials = %q, want unset", got)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("preflight body = %q, want empty", rec.Body.String())
	}
}

func TestPreflightUserinfoListsBothMethods(t *testing.T) {
	rec := httptest.NewRecorder()
	Preflight("GET, POST")(rec, httptest.NewRequest(http.MethodOptions, "/oauth/userinfo", nil))
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST" {
		t.Errorf("Access-Control-Allow-Methods = %q, want \"GET, POST\"", got)
	}
}

func splitComma(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			out = append(out, trimSpace(cur))
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, trimSpace(cur))
	}
	return out
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
