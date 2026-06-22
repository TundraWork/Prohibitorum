// Package server — handle_me_password.go
//
// /me/password/set is always sudo-gated. The endpoint sets or replaces the
// account's password_credential row, going through the password.Store so
// argon2id parameters and audit emission stay consistent with the
// login path. There is no /me/password/delete — full revocation of the
// non-WebAuthn fallback factors goes through /me/auth/revoke-password-totp,
// which deletes password + TOTP + recovery codes atomically.

package server

import (
	"encoding/json"
	"net/http"

	"prohibitorum/pkg/authn"
)

// POST /api/prohibitorum/me/password/set
func (s *Server) handleMePasswordSetHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if s.requireFreshSudo(r.Context(), w, sess) {
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	// OWASP 2026 §5.1.1.2: 8-char floor. Upper bound caps argon2id input
	// length so a single request can't spin the hasher for an unbounded
	// stretch — the per-request memory/iter budget is fixed by configx, but
	// the input byte length still feeds into the KDF.
	if len(body.Password) < 8 || len(body.Password) > 1024 {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if err := s.passwordStore.Set(r.Context(), sess.Account.ID, body.Password); err != nil {
		writeAuthErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
