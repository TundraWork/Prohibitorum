package weberr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

// TestRequestIDMiddleware_ReplacesInboundValue proves a client-supplied
// X-Request-ID never becomes the server ID. The middleware must generate a
// fresh cryptographically random value, place it in the request context AND
// on the response header, and the inbound value must not appear in either.
func TestRequestIDMiddleware_ReplacesInboundValue(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "attacker-controlled")
	rec := httptest.NewRecorder()

	RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := RequestIDFromContext(r.Context())
		if got == "" {
			t.Fatal("RequestIDFromContext returned empty string")
		}
		if got == "attacker-controlled" {
			t.Fatal("server request id matches inbound value — must be server-generated")
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(rec, req)

	respID := rec.Header().Get("X-Request-ID")
	if respID == "" {
		t.Fatal("response missing X-Request-ID header")
	}
	if respID == "attacker-controlled" {
		t.Fatal("response X-Request-ID matches inbound value")
	}
}

// TestRequestIDMiddleware_ContextEqualsHeader proves the value in context
// matches the value on the response header so logs and error bodies can
// correlate.
func TestRequestIDMiddleware_ContextEqualsHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	var ctxID string
	RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxID = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(rec, req)

	respID := rec.Header().Get("X-Request-ID")
	if ctxID == "" || respID == "" {
		t.Fatalf("empty id: ctx=%q header=%q", ctxID, respID)
	}
	if ctxID != respID {
		t.Fatalf("context id %q != header id %q", ctxID, respID)
	}
}

// TestRequestIDMiddleware_NoInboundHeader proves the middleware generates an
// ID when the client sends no X-Request-ID at all.
func TestRequestIDMiddleware_NoInboundHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") == "" {
		t.Fatal("missing X-Request-ID when client sent none")
	}
}

// TestRequestIDMiddleware_Unique proves each request gets a distinct ID.
func TestRequestIDMiddleware_Unique(t *testing.T) {
	gen := func() string {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})).ServeHTTP(rec, req)
		return rec.Header().Get("X-Request-ID")
	}
	a, b := gen(), gen()
	if a == "" || b == "" {
		t.Fatalf("empty id: %q %q", a, b)
	}
	if a == b {
		t.Fatalf("two request ids collided: %q", a)
	}
}

// TestRequestIDFormat proves the ID is 128-bit base64url without padding:
// 16 bytes → 22 base64url characters, no '+' '/' or '='.
var base64urlRE = regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`)

func TestRequestIDFormat(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(rec, req)

	id := rec.Header().Get("X-Request-ID")
	if !base64urlRE.MatchString(id) {
		t.Fatalf("request id %q does not match 128-bit base64url (22 chars, no padding)", id)
	}
}

// TestRequestIDFromContext_EmptyContext proves that a context without the
// middleware's key returns an empty string (no panic, no fabricated value).
func TestRequestIDFromContext_EmptyContext(t *testing.T) {
	if got := RequestIDFromContext(context.Background()); got != "" {
		t.Fatalf("RequestIDFromContext on bare context = %q, want empty", got)
	}
}
