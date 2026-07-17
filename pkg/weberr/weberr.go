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

type publicErrorObserver interface {
	ObservePublicError(code string, details map[string]any)
}

func observePublicError(w http.ResponseWriter, code string, details map[string]any) {
	if observer, ok := w.(publicErrorObserver); ok {
		observer.ObservePublicError(code, details)
	}
}

// Canonicalize returns the registered definition, final public code, and only
// detail fields whose keys are allowlisted by that definition. Unknown codes
// fall back to server_error with no details.
func Canonicalize(code string, details map[string]any) (Definition, string, map[string]any) {
	def, ok := DefinitionFor(code)
	if !ok {
		def, _ = DefinitionFor("server_error")
		return def, "server_error", nil
	}
	if len(details) == 0 || len(def.DetailKeys) == 0 {
		return def, code, nil
	}
	for key := range details {
		if _, allowed := def.DetailKeys[key]; !allowed {
			filtered := make(map[string]any, len(details)-1)
			for candidate, value := range details {
				if _, keep := def.DetailKeys[candidate]; keep {
					filtered[candidate] = value
				}
			}
			if len(filtered) == 0 {
				filtered = nil
			}
			return def, code, filtered
		}
	}
	return def, code, details
}

// WriteJSON writes a PublicError envelope to an http.ResponseWriter. The code
// and details are canonicalized against the registry (DefinitionFor +
// DetailKeys), so an unregistered code falls back to "server_error" and
// undeclared detail keys are removed before writing or observation. The
// status is taken from the registered definition, not the caller — callers
// pass code + details, the registry authoritatively maps to status.
//
// The request ID is stamped from the context so the response carries the
// server-generated correlation ID. This is the shared chokepoint for raw chi
// handler error responses — the huma typed path writes the same envelope
// through the adapter in pkg/server/operations.go.
//
// The X-Request-ID response header is set by the RequestID middleware, not
// here, so it appears on every response regardless of success or failure.
func WriteJSON(w http.ResponseWriter, code string, details map[string]any, requestID string) {
	def, code, details := Canonicalize(code, details)
	observePublicError(w, code, details)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(def.Status)
	_ = json.NewEncoder(w).Encode(PublicError{
		Code:      code,
		Details:   details,
		RequestID: requestID,
	})
}
