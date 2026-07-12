package weberr

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
)

// requestIDKey is the context key for the server-generated request correlation
// identifier. Unexported so callers must use RequestIDFromContext.
type requestIDKey struct{}

// HeaderRequestID is the HTTP header carrying the server-generated request ID
// on every response. Client-supplied values on this header are ignored as a
// source of the server ID (they are replaced).
const HeaderRequestID = "X-Request-ID"

// newRequestID generates a 128-bit (16-byte) cryptographically random request
// ID, encoded as base64url without padding. On a crypto/rand failure — which
// should be effectively impossible on a healthy system — it returns a fixed
// sentinel so the request can still proceed; the ID is for correlation, not
// authentication.
func newRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "AAAAAAAAAAAAAAAAAAAAAA"
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// RequestID is the top-level correlation middleware. For every request it
// generates a fresh 128-bit server ID, stores it in the request context (so
// handlers and error writers can include it in diagnostic bodies / logs), and
// sets it on the response's X-Request-ID header. Inbound X-Request-ID values
// are never trusted as the server identifier — they are always replaced.
//
// The header is set before the handler runs so it propagates even if the
// handler panics or writes an error.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set(HeaderRequestID, id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext extracts the server-generated request ID from a
// context. Returns "" when the RequestID middleware did not run (e.g. in a
// test calling writeAuthErr directly without the middleware).
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(requestIDKey{}).(string)
	return v
}
