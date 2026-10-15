package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/weberr"
)

// newCrossOriginMW builds the global cross-origin write protection. It runs
// before LoadSession so a rejected request never touches session state, and
// covers every non-safe method: the JSON Content-Type check only guards
// registerAdminBodyOpHTTP routes, leaving empty-body POSTs unprotected.
//
// The machine endpoints listed here must accept cross-site POSTs by design:
// /oauth/* never reads the session cookie (client credentials, PKCE, or
// bearer token per request) and its responses carry permissive CORS headers,
// while /saml/sso and /saml/slo receive HTTP-POST binding form submits from
// SP pages hosted on other sites.
func newCrossOriginMW(publicOrigins []string) func(http.Handler) http.Handler {
	protection := http.NewCrossOriginProtection()
	for _, origin := range publicOrigins {
		// A misconfigured public_origin must surface at startup, not as a
		// silently unprotected (or broken) deployment.
		if err := protection.AddTrustedOrigin(origin); err != nil {
			panic("cross-origin protection: " + err.Error())
		}
	}
	for _, pattern := range []string{
		"POST /oauth/token",
		"POST /oauth/revoke",
		"POST /oauth/introspect",
		"POST /oauth/userinfo",
		"POST /saml/sso",
		"POST /saml/slo",
	} {
		protection.AddInsecureBypassPattern(pattern)
	}
	protection.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Same JSON error shape as writeAuthErr; RequestID middleware has
		// already set X-Request-ID because protection runs after it.
		requestID := w.Header().Get(weberr.HeaderRequestID)
		ae := authn.ErrCrossOriginRequest()
		weberr.WriteJSON(w, ae.Code, ae.Details, requestID)
	}))
	return protection.Handler
}

// installRequestGuardChain installs the request-scoped prefix every server
// construction shares: X-Request-ID first (rejections carry it), then the
// cross-origin write protection (a rejected request never reaches session
// loading or body processing). Tests building the real route table must use
// this, not assemble their own stack, so the production ordering cannot drift
// from what is asserted.
func installRequestGuardChain(router *chi.Mux, publicOrigins []string) {
	router.Use(weberr.RequestID)
	router.Use(newCrossOriginMW(publicOrigins))
}

// compile-time assertion that the middleware matches chi's Middleware shape.
var _ func(http.Handler) http.Handler = newCrossOriginMW([]string{})
