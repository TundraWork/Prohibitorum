// Package server — handle_invite_federation.go
//
// Public HTTP entrypoint for the invite_only federation flow (stage 2
// of the invite_only chunk).
//
//	GET /api/prohibitorum/enrollments/{token}/start-federation?return_to=…
//	    → 302 to the upstream OP's /authorize URL, with the invite token
//	      stashed in FedState so the callback can dispatch to
//	      applyInviteOnly atomically.
//
// Parallel to /enrollments/{token}/register/begin (WebAuthn enrollment
// ceremony); the route is public — the bearer of the URL is the principal.
//
// All "invite isn't redeemable" branches collapse onto authn.ErrInviteRequired
// inside Federator.BeginInviteRedemption — the handler is a thin shim.
// Audit-burn from a brute-force enumerator is bounded by the audit writer's
// own backoff (per the M5 audit fix, no IP-based rate limit lives here).
package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/authn"
	sessstore "prohibitorum/pkg/session"
)

// handleEnrollmentStartFederationHTTP serves
// GET /api/prohibitorum/enrollments/{token}/start-federation.
func (s *Server) handleEnrollmentStartFederationHTTP(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		// Collapse onto the same opaque code BeginInviteRedemption uses for
		// every other "no good" branch — don't reveal that empty token is
		// rejected at a different layer than unknown token.
		redirectAuthErrToError(w, r, authn.ErrInviteRequired())
		return
	}

	returnTo, rterr := s.validateFederationReturnTo(r.URL.Query().Get("return_to"))
	if rterr != nil {
		redirectAuthErrToError(w, r, rterr)
		return
	}

	req, err := s.federationService.BeginInvite(r.Context(), token, returnTo)
	if err != nil {
		redirectAuthErrToError(w, r, err)
		return
	}

	// Drop the Referer header sent to the upstream so the invite token in
	// our URL doesn't leak via Referer. Defense in depth — the token is
	// already stashed in FedState by this point; race-bound by atomic
	// ConsumeEnrollment + short admin-set TTL.
	w.Header().Set("Referrer-Policy", "no-referrer")

	// Invite redemption shares the federation /callback, so bind the flow to
	// this browser with the same anti-forgery cookie the login flow uses (N4).
	http.SetCookie(w, sessstore.FedStateCookie(s.config, r, req.BrowserToken))
	http.Redirect(w, r, req.Action.URL, http.StatusFound)
}
