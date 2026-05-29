// Package server — handle_federation.go
//
// Public HTTP entrypoints for upstream OIDC federation login (v0.3, Task 7).
//
//	GET /api/prohibitorum/auth/federation/{slug}/login?return_to=…
//	    → 302 to the upstream OP's /authorize URL.
//
//	GET /api/prohibitorum/auth/federation/{slug}/callback?code=…&state=…&iss=…
//	    → on success: issues a session cookie and 302s to the original return_to.
//	    → on upstream error= : audits + writes ErrUpstreamError.
//	    → on missing/bad state : writes ErrFederationStateInvalid (no audit row —
//	      stray browser hits should not flood the audit log).
//
// The state token is the upstream OP's state parameter. The secret blob is keyed
// by it inside KV (LoginKey(token)), Pop'd once at callback time. All replay /
// iss-mismatch / code-exchange-failure paths collapse onto the single
// federation_state_invalid code to avoid leaking a side channel into the
// federation pipeline.
//
// Route mounting is owned by Task 9 (server.go's registerOperations). The
// Server.federator field is populated there; this file defines only the
// handlers + the return_to validator.
package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	fedoidc "prohibitorum/pkg/federation/oidc"
	sessstore "prohibitorum/pkg/session"
)

// handleFederationLoginHTTP serves
// GET /api/prohibitorum/auth/federation/{slug}/login.
func (s *Server) handleFederationLoginHTTP(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	returnTo, err := s.validateFederationReturnTo(r.URL.Query().Get("return_to"))
	if err != nil {
		writeAuthErr(w, err)
		return
	}

	req, err := s.federator.BeginLogin(r.Context(), slug, returnTo)
	if err != nil {
		if errors.Is(err, fedoidc.ErrUnknownIDP) {
			// Collapse "no such slug" onto the generic state-invalid code so
			// callers can't enumerate configured upstream IdP slugs.
			writeAuthErr(w, authn.ErrFederationStateInvalid())
			return
		}
		writeAuthErr(w, err)
		return
	}

	http.Redirect(w, r, req.AuthorizeURL, http.StatusFound)
}

// handleFederationCallbackHTTP serves
// GET /api/prohibitorum/auth/federation/{slug}/callback.
func (s *Server) handleFederationCallbackHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	upstreamErr := q.Get("error")
	upstreamDesc := q.Get("error_description")
	state := q.Get("state")
	code := q.Get("code")
	iss := q.Get("iss")

	if upstreamErr != "" {
		// Upstream OP refused the user. Surface the upstream code + description
		// in the wire response so admins debugging "I can't log in via X" have
		// something actionable, and emit an audit row (no account_id — we
		// never reached the resolve step).
		_ = s.Audit.Record(r.Context(), audit.Record{
			Factor: audit.FactorFederationOIDC,
			Event:  audit.EventFail,
			Detail: map[string]any{
				"reason":               "upstream_error",
				"upstream_code":        upstreamErr,
				"upstream_description": upstreamDesc,
			},
		})
		writeAuthErr(w, authn.ErrUpstreamError(upstreamErr, upstreamDesc))
		return
	}

	if state == "" || code == "" {
		// Stray browser request or someone pasting the callback URL out of
		// context. No audit — this fires on benign hits (back-button replay,
		// link previews) and would flood the log.
		writeAuthErr(w, authn.ErrFederationStateInvalid())
		return
	}

	result, err := s.federator.HandleCallback(r.Context(), state, code, iss)
	if err != nil {
		// HandleCallback returns structured *authn.AuthError for every
		// expected failure (federation_state_invalid, bad_credentials,
		// email_not_verified, username_collision, …). Forward them straight
		// through — writeAuthErr maps to the right status code. Wrapped
		// non-AuthError values surface as 500 via writeAuthErr's fallback.
		writeAuthErr(w, err)
		return
	}

	ip := sessstore.ClientIP(r, s.config.TrustProxy)
	ua := r.UserAgent()
	// RFC 8176 §2: "federated" indicates a federated authentication assertion.
	// Real upstream OPs commonly omit the amr claim — we backfill with this
	// generic value so the local session always has a meaningful amr.
	amr := result.AMR
	if len(amr) == 0 {
		amr = []string{"federated"}
	}
	// H1-sch: stamp the upstream IdP onto the session row so v0.4 OIDC OP can
	// surface a "federated" discriminator in downstream id_token claims.
	idpID := result.IDPID
	token, _, err := s.sessionStore.Issue(r.Context(), result.AccountID, ip, ua, amr, &idpID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	http.SetCookie(w, sessstore.FreshSessionCookie(s.config, r, result.AccountID, token, s.config.SessionTTL))
	http.Redirect(w, r, result.ReturnTo, http.StatusFound)
}

// validateFederationReturnTo allows only relative URLs starting with "/" (and
// not "//", which would be a protocol-relative scheme injection). v0.3 design
// decision D6: federation callbacks land in this Prohibitorum origin only.
// Empty input defaults to "/". The Server receiver is forward-looking — a
// future task may extend this to read s.config.PublicOrigins.
func (s *Server) validateFederationReturnTo(rt string) (string, error) {
	if rt == "" {
		return "/", nil
	}
	if strings.HasPrefix(rt, "/") && !strings.HasPrefix(rt, "//") {
		return rt, nil
	}
	return "", authn.ErrInvalidReturnTo()
}
