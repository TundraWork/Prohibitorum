// Package server — handle_me_revoke_pwd_totp.go
//
// /me/auth/revoke-password-totp is always sudo-gated. It deletes the
// password credential, TOTP credential, and every recovery code for the
// account, leaving only WebAuthn (passkeys) or federation as a usable factor.
//
// A lockout guard in authn.DisableNonWebAuthnFallbacks prevents the delete
// when doing so would leave the account with zero usable sign-in methods.
//
// Production path: the deletes run inside a pgx transaction; the account row
// is locked with SELECT ... FOR UPDATE before the guard check so concurrent
// factor mutations cannot race the guard. Unit tests inject a fake via
// revokeFlowOverride (s.dbPool == nil), bypassing the tx wrap.

package server

import (
	"net/http"

	"prohibitorum/pkg/audit"
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
	ctx := r.Context()
	sess := authn.SessionFromContext(ctx)
	if s.requireFreshSudo(ctx, w, sess) {
		return
	}
	acctID := sess.Account.ID

	// Unit-test seam: when no real pool is wired (fake injected via
	// revokeFlowOverride), run without a tx. Production serialises on the
	// account row so concurrent factor mutations can't race the lockout guard.
	// The FOR UPDATE row lock (the race-safety mechanism vs a concurrent
	// passkey-delete) runs only in this production path; unit tests cover the
	// guard logic but not the concurrency property (smoke covers the wired path).
	if s.dbPool == nil {
		if err := authn.DisableNonWebAuthnFallbacks(ctx, s.revokeFlowQ(), s.Audit, acctID); err != nil {
			writeAuthErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	tx, err := s.dbPool.Begin(ctx)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := s.queries.WithTx(tx)
	if _, err := qtx.GetAccountByIDForUpdate(ctx, acctID); err != nil {
		writeAuthErr(w, err)
		return
	}
	// Audit MUST be written on the tx connection (audit.NewWriter(qtx)), NOT the
	// pool-bound s.Audit. credential_event has an FK to account(id), so the audit
	// INSERT needs FOR KEY SHARE on the account row — which conflicts with the
	// FOR UPDATE this tx already holds. On a separate pool connection that would
	// deadlock at the application layer (PG can't detect it: the holder is
	// idle-in-transaction). Writing audit through qtx keeps it in the same tx
	// (no self-conflict) and makes the audit atomic with the deletes.
	if err := authn.DisableNonWebAuthnFallbacks(ctx, qtx, audit.NewWriter(qtx), acctID); err != nil {
		writeAuthErr(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeAuthErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
