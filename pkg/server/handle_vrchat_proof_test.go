package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"prohibitorum/pkg/webui"
)

func TestVRChatProofAlwaysServesIdenticalNoStoreSPAShell(t *testing.T) {
	s := &Server{webUIHandler: webui.Handler("Proof Test")}
	paths := []string{
		"/verify/vrchat/random",
		"/verify/vrchat/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"/verify/vrchat/not_base64!!!",
		"/verify/vrchat/expired-looking-proof-token",
	}
	var baseline []byte
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			s.handleVRChatProofHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d", rr.Code)
			}
			if rr.Header().Get("Cache-Control") != "no-store" || rr.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Fatalf("privacy headers = %#v", rr.Header())
			}
			if rr.Header().Get("Content-Type") != "text/html; charset=utf-8" {
				t.Fatalf("content type = %q", rr.Header().Get("Content-Type"))
			}
			if baseline == nil {
				baseline = append([]byte(nil), rr.Body.Bytes()...)
			} else if !bytes.Equal(baseline, rr.Body.Bytes()) {
				t.Fatal("proof path changed SPA response bytes")
			}
		})
	}
}
