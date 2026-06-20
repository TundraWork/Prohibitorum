// Package weberr redirects browser-navigated requests to the SPA error page.
//
// Use ONLY for full-page (browser-navigated) dead-ends in OAuth/OIDC/SAML/
// federation flows — never for XHR/API endpoints (those keep JSON). The SPA
// ErrorView maps `error` to a friendly, non-technical message and shows `ref`
// as a support reference; full detail belongs in server logs / the audit trail.
package weberr

import (
	"crypto/rand"
	"encoding/hex"
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
	w.Header().Set("Cache-Control", "no-store")
	u := "/error?error=" + url.QueryEscape(code) + "&ref=" + url.QueryEscape(ref)
	http.Redirect(w, r, u, http.StatusFound)
}
