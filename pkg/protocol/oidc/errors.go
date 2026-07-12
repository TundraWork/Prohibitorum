package oidc

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"

	"prohibitorum/pkg/weberr"
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
	errCodeTemporarilyUnavailable  = "temporarily_unavailable"
	errCodeLoginRequired           = "login_required"
	errCodeConsentRequired         = "consent_required"
	// errCodeInvalidToken is the RFC 6750 §3.1 bearer-token error code used by
	// the /userinfo endpoint when the presented access token is missing,
	// malformed, expired, or otherwise unverifiable. It is distinct from the
	// RFC 6749 token-endpoint set above.
	errCodeInvalidToken = "invalid_token"
)

// writeOIDCError renders an RFC 6749 §5.2 error response as JSON. Used for
// the token endpoint and any direct (non-redirect) error surface. The wire
// format is protocol-native: {error, error_description} — NOT the
// application {code, details, requestId} envelope. The request ID from the
// context is correlated in a structured log so operators can trace a
// protocol error to the public application response, without exposing it
// on the wire (which would violate RFC 6749).
func writeOIDCError(w http.ResponseWriter, r *http.Request, status int, code, desc string) {
	requestID := weberr.RequestIDFromContext(r.Context())
	slog.Warn("oidc protocol error", "error_code", code, "status", status, "request_id", requestID, "path", r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             code,
		"error_description": desc,
	})
}
// writeInvalidClient renders a 401 invalid_client per RFC 6749 §5.2, including
// the WWW-Authenticate challenge when the caller used HTTP Basic auth.
func writeInvalidClient(w http.ResponseWriter, r *http.Request, desc string) {
	if _, _, ok := r.BasicAuth(); ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="oidc"`)
	}
	writeOIDCError(w, r, http.StatusUnauthorized, errCodeInvalidClient, desc)
}

// redirectToErrorPage sends a browser-navigated authorize/logout error to the
// SPA /error page (relative redirect, same origin). Use ONLY on the DIRECT-
// error side of the open-redirect guard (before a trusted redirect_uri exists)
// and for end_session errors — never instead of redirectError, which is the
// RFC-mandated RP error channel once redirect_uri is trusted.
func (p *Provider) redirectToErrorPage(w http.ResponseWriter, r *http.Request, code string) {
	ref := weberr.NewRef()
	slog.Warn("oidc browser-facing flow error", "code", code, "ref", ref, "path", r.URL.Path)
	weberr.RedirectToError(w, r, code, ref)
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
		writeOIDCError(w, r, http.StatusBadRequest, errCodeInvalidRequest, "invalid redirect_uri")
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
