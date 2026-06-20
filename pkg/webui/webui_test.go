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
