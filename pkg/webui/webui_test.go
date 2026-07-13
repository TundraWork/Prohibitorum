package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler_TemplatesTitle(t *testing.T) {
	h := Handler("Acme SSO")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), "<title>Acme SSO</title>") {
		t.Fatalf("title not templated; body head: %.200s", rec.Body.String())
	}
}

func TestHandler_EscapesTitle(t *testing.T) {
	h := Handler(`<x>`)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(rec.Body.String(), "<title><x></title>") {
		t.Fatal("instance name was not HTML-escaped")
	}
}

func TestSecurityHeaders_StyleSrcElem(t *testing.T) {
	rec := httptest.NewRecorder()
	setSecurityHeaders(rec)
	csp := rec.Header().Get("Content-Security-Policy")

	// The production browser inserts exactly one inline <style> element from
	// Reka UI's Select viewport (scrollbar-hiding rule). Its browser-confirmed
	// hash is sha256-60LHlRjW/B3CtzIoE/Lf1/NEDvko9efWMFaGVhHu/cs=, and CSP must
	// allow that hash — and only that hash — within style-src-elem, rather than
	// weakening the element channel with 'unsafe-inline'.
	const wantHash = `'sha256-60LHlRjW/B3CtzIoE/Lf1/NEDvko9efWMFaGVhHu/cs='`

	var styleSrcElem string
	for _, part := range strings.Split(csp, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "style-src-elem ") {
			styleSrcElem = part
			break
		}
	}
	if styleSrcElem == "" {
		t.Fatalf("style-src-elem directive missing from CSP: %s", csp)
	}
	if !strings.Contains(styleSrcElem, "'self'") {
		t.Errorf("style-src-elem must contain 'self': %s", styleSrcElem)
	}
	if !strings.Contains(styleSrcElem, wantHash) {
		t.Errorf("style-src-elem must contain %s: %s", wantHash, styleSrcElem)
	}
	if strings.Contains(styleSrcElem, "unsafe-inline") {
		t.Errorf("style-src-elem must not contain unsafe-inline: %s", styleSrcElem)
	}
}
