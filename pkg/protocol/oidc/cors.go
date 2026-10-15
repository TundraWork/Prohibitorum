package oidc

import "net/http"

// The OP's machine endpoints (discovery, JWKS, token, revoke, introspect,
// userinfo) are called directly by browser apps, so they are readable from
// any origin. None of them reads the session cookie: every one authenticates
// by client credentials, PKCE, or bearer token carried in the request, so a
// wildcard Allow-Origin discloses nothing the caller could not already
// obtain. Allow-Credentials is never sent, which keeps cookie-authenticated
// cross-origin responses unreadable by scripts.
const (
	corsAllowOrigin  = "*"
	corsAllowHeaders = "Authorization, Content-Type"
	corsMaxAge       = "600"
)

// corsExposeHeaders are response headers scripts need but the CORS-safelisted
// response headers do not include: WWW-Authenticate on userinfo/token errors
// and Retry-After on rate-limited responses.
const corsExposeHeaders = "WWW-Authenticate, Retry-After"

// CORS wraps an OP endpoint handler with the actual-response CORS headers.
// They are set before the handler runs so error responses (401
// WWW-Authenticate, 429 Retry-After) carry them too.
func CORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", corsAllowOrigin)
		h.Set("Access-Control-Expose-Headers", corsExposeHeaders)
		next(w, r)
	}
}

// Preflight answers the OPTIONS preflight for a CORS-enabled OP endpoint with
// the methods that endpoint accepts. 204, no body.
func Preflight(methods string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", corsAllowOrigin)
		h.Set("Access-Control-Allow-Methods", methods)
		// Authorization must be listed explicitly: the wildcard covers only
		// CORS-safelisted request headers, which Authorization is not.
		h.Set("Access-Control-Allow-Headers", corsAllowHeaders)
		h.Set("Access-Control-Max-Age", corsMaxAge)
		w.WriteHeader(http.StatusNoContent)
	}
}
