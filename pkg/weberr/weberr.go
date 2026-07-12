// Package weberr provides the typed public-error registry, HTTP request
// correlation middleware, and browser-error redirect helpers.
//
// Application JSON errors use the {code, details?, requestId} envelope defined
// by PublicError — never a localized message. Browser-navigated flow dead-ends
// redirect to the SPA /error page via RedirectToError. The two paths are
// mutually exclusive: XHR/API endpoints keep JSON; full-page redirects use the
// SPA ErrorView.
package weberr

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
)

// NewRef returns a short random correlation reference (8 hex chars). Shown to
// the user and logged/audited server-side so support can correlate the two.
// Best-effort: a rand failure yields a fixed sentinel rather than an error,
// because the redirect must still happen.
func NewRef() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(b)
}

// RedirectToError 302-redirects the browser to /error?error=<code>&ref=<ref>.
// code is a stable, non-technical code the SPA maps to copy; ref comes from
// NewRef. Caller is responsible for logging/auditing the failure with ref.
func RedirectToError(w http.ResponseWriter, r *http.Request, code, ref string) {
	RedirectToErrorWithReturn(w, r, code, ref, "")
}

// RedirectToErrorWithReturn is RedirectToError plus a return_to hint the SPA's
// error page uses for its "go back" link, so a user who hit a dead-end mid-flow
// (e.g. linking an identity from /connected) can return where they came from.
// returnTo MUST already be a server-validated, same-origin value (the SPA also
// re-guards it through safeReturnTo); pass "" when there is no safe origin. An
// empty returnTo omits the param entirely, matching RedirectToError.
func RedirectToErrorWithReturn(w http.ResponseWriter, r *http.Request, code, ref, returnTo string) {
	w.Header().Set("Cache-Control", "no-store")
	u := "/error?error=" + url.QueryEscape(code) + "&ref=" + url.QueryEscape(ref)
	if returnTo != "" {
		u += "&return_to=" + url.QueryEscape(returnTo)
	}
	http.Redirect(w, r, u, http.StatusFound)
}

// WriteJSON writes a PublicError envelope to an http.ResponseWriter with the
// given HTTP status. The request ID is stamped from the context so the
// response carries the server-generated correlation ID. This is the shared
// chokepoint for raw chi handler error responses — the huma typed path writes
// the same envelope through the adapter in pkg/server/operations.go.
//
// The X-Request-ID response header is set by the RequestID middleware, not
// here, so it appears on every response regardless of success or failure.
func WriteJSON(w http.ResponseWriter, status int, code string, details map[string]any, requestID string) {
	var detailsArg map[string]any
	if len(details) > 0 {
		detailsArg = details
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(PublicError{
		Code:      code,
		Details:   detailsArg,
		RequestID: requestID,
	})
}
