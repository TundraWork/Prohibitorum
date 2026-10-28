package webui

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler_TemplatesTitlePerRequest(t *testing.T) {
	name := "Acme SSO"
	h := Handler(func(context.Context) string { return name })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), "<title>Acme SSO</title>") {
		t.Fatalf("title not templated; body head: %.200s", rec.Body.String())
	}

	// A rename must be visible on the very next shell response.
	name = "Renamed"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), "<title>Renamed</title>") {
		t.Fatalf("renamed title not templated; body head: %.200s", rec.Body.String())
	}
}

func TestHandler_EscapesTitle(t *testing.T) {
	h := Handler(func(context.Context) string { return `<b>&` })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(rec.Body.String(), "<title><b>&</title>") {
		t.Fatal("instance name was not HTML-escaped")
	}
	if !strings.Contains(rec.Body.String(), "<title>&lt;b&gt;&amp;</title>") {
		t.Fatalf("instance name not escaped as expected; body head: %.200s", rec.Body.String())
	}
}

func TestHandler_FallsBackWhenNameEmpty(t *testing.T) {
	h := Handler(func(context.Context) string { return "" })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), "<title>Prohibitorum</title>") {
		t.Fatalf("fallback title missing; body head: %.200s", rec.Body.String())
	}
}

func TestHandler_ShellNotCacheable(t *testing.T) {
	h := Handler(func(context.Context) string { return "Acme SSO" })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
	if rec.Header().Get("ETag") != "" {
		t.Fatalf("shell response must not offer an ETag, got %q", rec.Header().Get("ETag"))
	}
}

func TestSecurityHeaders_StyleSrcElem(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler(func(context.Context) string { return "Prohibitorum" }).ServeHTTP(
		rec, httptest.NewRequest(http.MethodGet, "/", nil))
	csp := rec.Header().Get("Content-Security-Policy")

	directives := make(map[string][]string)
	for _, part := range strings.Split(csp, ";") {
		fields := strings.Fields(part)
		if len(fields) > 0 {
			directives[fields[0]] = fields[1:]
		}
	}
	scripts := directives["script-src"]
	if len(scripts) != 1 || scripts[0] != "'self'" {
		t.Errorf("scripts must allow only same-origin resources: %s", csp)
	}
	scrollbarStyle := ".base-ui-disable-scrollbar{scrollbar-width:none}.base-ui-disable-scrollbar::-webkit-scrollbar{display:none}"
	hash := sha256.Sum256([]byte(scrollbarStyle))
	allowedHash := "'sha256-" + base64.StdEncoding.EncodeToString(hash[:]) + "'"
	styles := directives["style-src-elem"]
	if len(styles) != 2 || styles[0] != "'self'" || styles[1] != allowedHash {
		t.Errorf("styles must allow same-origin resources and the Base UI scroll area rule only: %s", csp)
	}
	images := strings.Join(directives["img-src"], " ")
	if images != "'self' data: blob: https://www.bing.com https://images.unsplash.com" {
		t.Errorf("images must allow same-origin, data:, blob: and the two wallpaper hosts only: %s", csp)
	}
}
