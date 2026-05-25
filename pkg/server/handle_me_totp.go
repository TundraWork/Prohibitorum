// Package server — handle_me_totp.go
//
// TOTP enrollment endpoints. begin / verify are sudo-gated ONLY when a
// confirmed TOTP credential already exists (the re-enroll path). First-time
// enrollment by an authenticated user is not gated — they have no other
// TOTP to step up with, and login itself already proved possession of an
// enrolled factor.
//
// recovery-codes/regenerate is always sudo-gated and additionally requires
// a confirmed TOTP — recovery codes mean nothing if the second factor they
// back is absent.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
)

// meTOTPFlowQueries is the narrow read surface the /me/totp/* handlers need
// to decide whether sudo gating applies. Declared separately from
// sudoFlowQueries so tests can stub it without pulling in the recovery-
// code list query that conditional sudo doesn't care about.
type meTOTPFlowQueries interface {
	GetTOTPCredential(ctx context.Context, accountID int32) (db.TotpCredential, error)
}

func (s *Server) meTOTPFlowQ() meTOTPFlowQueries {
	if s.meTOTPFlowOverride != nil {
		return s.meTOTPFlowOverride
	}
	return s.queries
}

// totpRequiresSudo returns true (and writes the sudo-required response) IFF
// a confirmed totp_credential exists for the account. For accounts with no
// row at all, or an unconfirmed row, no sudo is required — the caller can
// proceed. Returns true when the handler should stop (sudo failed, sudo
// missing, or the lookup itself errored).
func (s *Server) totpRequiresSudo(ctx context.Context, w http.ResponseWriter, sess *authn.Session) bool {
	row, err := s.meTOTPFlowQ().GetTOTPCredential(ctx, sess.Account.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false
		}
		writeAuthErr(w, err)
		return true
	}
	if !row.ConfirmedAt.Valid {
		return false
	}
	return s.requireFreshSudo(ctx, w, sess)
}

// POST /api/prohibitorum/me/totp/begin
func (s *Server) handleMeTOTPBeginHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if s.totpRequiresSudo(r.Context(), w, sess) {
		return
	}
	enr, err := s.totpStore.Begin(r.Context(), sess.Account.ID, sess.Account.Username)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"secret_base32": enr.SecretBase32,
		"otpauth_uri":   enr.ProvisioningURI,
	})
}

// POST /api/prohibitorum/me/totp/verify
func (s *Server) handleMeTOTPVerifyHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if s.totpRequiresSudo(r.Context(), w, sess) {
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Code == "" {
		writeAuthErr(w, authn.ErrBadCredentials())
		return
	}
	codes, err := s.totpStore.Verify(r.Context(), sess.Account.ID, body.Code)
	if err != nil {
		// factor_locked (and any other *AuthError) passes through with its
		// own status + Retry-After. Sentinels (invalid/replay/not-set)
		// collapse to bad_credentials so the response doesn't distinguish
		// "no row yet" from "wrong code".
		if ae := authn.AsAuthError(err); ae != nil {
			writeAuthErr(w, ae)
			return
		}
		writeAuthErr(w, authn.ErrBadCredentials())
		return
	}
	if codes != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"recovery_codes": codes})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/prohibitorum/me/recovery-codes/regenerate
func (s *Server) handleMeRegenerateRecoveryCodesHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if s.requireFreshSudo(r.Context(), w, sess) {
		return
	}
	// Precondition: a confirmed TOTP row must exist. Recovery codes are
	// only meaningful as a backup for the second factor — minting them when
	// no second factor is enrolled would be a footgun.
	row, err := s.meTOTPFlowQ().GetTOTPCredential(r.Context(), sess.Account.ID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, err)
			return
		}
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if !row.ConfirmedAt.Valid {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	codes, err := s.totpStore.RegenerateRecoveryCodes(r.Context(), sess.Account.ID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"recovery_codes": codes})
}
