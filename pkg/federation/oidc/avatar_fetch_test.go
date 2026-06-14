package oidc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAvatarFetch_RejectsNonHTTPS(t *testing.T) {
	if _, err := fetchUpstreamAvatar(context.Background(), "http://example.com/a.png", true); err == nil {
		t.Fatal("want error for non-https URL")
	}
}

func TestAvatarFetch_RejectsNonImage(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>"))
	}))
	defer srv.Close()
	if _, err := fetchUpstreamAvatarWithClient(context.Background(), srv.URL, srv.Client()); err == nil || !strings.Contains(err.Error(), "content-type") {
		t.Fatalf("want content-type rejection, got %v", err)
	}
}

func TestAvatarFetch_ReturnsImageBytes(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\n"))
	}))
	defer srv.Close()
	b, err := fetchUpstreamAvatarWithClient(context.Background(), srv.URL, srv.Client())
	if err != nil || len(b) == 0 {
		t.Fatalf("want bytes, got len=%d err=%v", len(b), err)
	}
}
