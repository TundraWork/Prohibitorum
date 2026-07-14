package federation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAvatarFetch_RejectsNonHTTPS(t *testing.T) {
	// Production default (allowPrivate=false): http and other schemes are rejected.
	if err := validateAvatarURL("http://example.com/a.png", false); err == nil {
		t.Fatal("want error for http URL when allowPrivate=false")
	}
	if err := validateAvatarURL("ftp://example.com/a.png", false); err == nil {
		t.Fatal("want error for ftp URL when allowPrivate=false")
	}
	if err := validateAvatarURL("ftp://example.com/a.png", true); err == nil {
		t.Fatal("want error for ftp URL even when allowPrivate=true (only http/https are ever allowed)")
	}
	// allowPrivate=true (trusted-internal-IdP / loopback-OP tests) additionally
	// permits plaintext http so a loopback OP can serve the picture.
	if err := validateAvatarURL("http://127.0.0.1:8080/a.png", true); err != nil {
		t.Fatalf("http should be allowed when allowPrivate=true, got %v", err)
	}
	if err := validateAvatarURL("https://example.com/a.png", false); err != nil {
		t.Fatalf("https must always be allowed, got %v", err)
	}
}

func TestAvatarFetch_RejectsNonImage(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>"))
	}))
	defer srv.Close()
	if _, err := fetchUpstreamAvatarWithClient(context.Background(), srv.URL, srv.Client(), false); err == nil || !strings.Contains(err.Error(), "content-type") {
		t.Fatalf("want content-type rejection, got %v", err)
	}
}

func TestAvatarFetch_ReturnsImageBytes(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\n"))
	}))
	defer srv.Close()
	b, err := fetchUpstreamAvatarWithClient(context.Background(), srv.URL, srv.Client(), false)
	if err != nil || len(b) == 0 {
		t.Fatalf("want bytes, got len=%d err=%v", len(b), err)
	}
}
