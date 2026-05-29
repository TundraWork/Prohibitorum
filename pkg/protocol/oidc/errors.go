package oidc

import (
	"encoding/json"
	"net/http"
	"net/url"
)

// OAuth 2.0 / OIDC error codes — the string values that populate the
// `error` member of an error response (RFC 6749 §4.1.2.1, §5.2;
// OIDC Core §3.1.2.6). These are wire codes, distinct from the
// errInvalidClient sentinel VALUE in client.go.
const (
	errCodeInvalidRequest          = "invalid_request"
	errCodeInvalidClient           = "invalid_client"
	errCodeInvalidGrant            = "invalid_grant"
	errCodeUnauthorizedClient      = "unauthorized_client"
	errCodeUnsupportedGrantType    = "unsupported_grant_type"
	errCodeUnsupportedResponseType = "unsupported_response_type"
	errCodeInvalidScope            = "invalid_scope"
	errCodeAccessDenied            = "access_denied"
	errCodeServerError             = "server_error"
	errCodeLoginRequired           = "login_required"
	errCodeConsentRequired         = "consent_required"
)

// writeOIDCError renders an RFC 6749 §5.2 error response as JSON. Used for
// the token endpoint and any direct (non-redirect) error surface.
func writeOIDCError(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             code,
		"error_description": desc,
	})
}

// redirectError 302-redirects to the client's redirect_uri with the OAuth
// error encoded as query params (RFC 6749 §4.1.2.1). state is echoed only
// when non-empty. iss is included when non-empty (RFC 9207 — the discovery
// doc advertises authorization_response_iss_parameter_supported, so error
// redirects must also carry the issuer). If redirectURI fails to parse, falls
// back to a direct invalid_request JSON error rather than redirecting to an
// invalid target.
func redirectError(w http.ResponseWriter, r *http.Request, redirectURI, code, desc, state, iss string) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		writeOIDCError(w, http.StatusBadRequest, errCodeInvalidRequest, "invalid redirect_uri")
		return
	}
	q := u.Query()
	q.Set("error", code)
	if desc != "" {
		q.Set("error_description", desc)
	}
	if state != "" {
		q.Set("state", state)
	}
	if iss != "" {
		q.Set("iss", iss)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}
