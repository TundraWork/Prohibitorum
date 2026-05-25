// Package server — handle_me_revoke_pwd_totp.go
//
// /me/auth/revoke-password-totp is always sudo-gated. It deletes the
// password credential, TOTP credential, and every recovery code for the
// account, leaving only WebAuthn (passkeys) as a usable factor. Idempotent
// — calling on an account that already has no non-WebAuthn factors is a
// 204 no-op.
//
// v0.2 does not run these deletes inside a single Postgres transaction; see
// authn.DisableNonWebAuthnFallbacks for the rationale (partial failure is
// recoverable by retrying the endpoint; full atomicity is a v0.3+ harden).

package server

import (
	"net/http"

	"prohibitorum/pkg/authn"
)

// revokeFlowQ returns the FlowQueries surface used by the revoke handler.
// Defaults to s.queries; tests inject a fake via s.revokeFlowOverride.
// Same shape as sudoFlowQ() / meTOTPFlowQ() — keeps the query surface a
// narrow interface for unit testing while production wires the concrete
// *db.Queries.
func (s *Server) revokeFlowQ() authn.FlowQueries {
	if s.revokeFlowOverride != nil {
		return s.revokeFlowOverride
	}
	return s.queries
}

// POST /api/prohibitorum/me/auth/revoke-password-totp
func (s *Server) handleMeRevokePwdTOTPHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if s.requireFreshSudo(r.Context(), w, sess) {
		return
	}
	if err := authn.DisableNonWebAuthnFallbacks(r.Context(), s.revokeFlowQ(), s.Audit, sess.Account.ID); err != nil {
		writeAuthErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
