// Package server — handle_me_totp.go
//
// Signed-in TOTP reset and recovery-code regeneration. Both operations require
// fresh sudo and an existing confirmed TOTP credential. First-time setup is
// completed together with password setup through /me/password-totp/verify.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/logx"
)

type meTOTPFlowQueries interface {
	GetTOTPCredential(ctx context.Context, accountID int32) (db.TotpCredential, error)
}

func (s *Server) meTOTPFlowQ() meTOTPFlowQueries {
	if s.meTOTPFlowOverride != nil {
		return s.meTOTPFlowOverride
	}
	return s.queries
}

// POST /api/prohibitorum/me/totp/verify
// Body: {secret_base32, code}
func (s *Server) handleMeTOTPVerifyHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if s.requireFreshSudo(r.Context(), w, sess) {
		return
	}

	var body struct {
		SecretBase32 string `json:"secret_base32"`
		Code         string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SecretBase32 == "" || body.Code == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	matchedStep, ok := s.totpStore.VerifyCandidateSecret(body.SecretBase32, body.Code)
	if !ok {
		writeAuthErr(w, authn.ErrBadCredentials())
		return
	}

	// Reject first-time and half-configured accounts. Repeat this check under
	// the account-row lock so a concurrent mutation cannot turn reset into setup.
	current, err := s.meTOTPFlowQ().GetTOTPCredential(r.Context(), sess.Account.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrBadRequest())
			return
		}
		writeAuthErr(w, err)
		return
	}
	if !current.ConfirmedAt.Valid {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	tx, err := s.beginEnrollmentTx(r.Context())
	if err != nil {
		writeAuthErr(w, fmt.Errorf("me/totp/verify: begin tx: %w", err))
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	qtx := tx.Queries()
	if _, err := qtx.GetAccountByIDForUpdate(r.Context(), sess.Account.ID); err != nil {
		writeAuthErr(w, fmt.Errorf("me/totp/verify: lock account: %w", err))
		return
	}
	current, err = qtx.GetTOTPCredential(r.Context(), sess.Account.ID)
	if err != nil || !current.ConfirmedAt.Valid {
		if errors.Is(err, pgx.ErrNoRows) || err == nil {
			writeAuthErr(w, authn.ErrBadRequest())
			return
		}
		writeAuthErr(w, fmt.Errorf("me/totp/verify: get current totp: %w", err))
		return
	}
	oldRecovery, err := qtx.ListRecoveryCodesByAccount(r.Context(), sess.Account.ID)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("me/totp/verify: list recovery codes: %w", err))
		return
	}
	recoveryCodes, err := s.totpStore.EnrollConfirmedForTx(r.Context(), qtx, sess.Account.ID, body.SecretBase32, matchedStep)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("me/totp/verify: enroll totp: %w", err))
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeAuthErr(w, fmt.Errorf("me/totp/verify: commit: %w", err))
		return
	}

	acctID := sess.Account.ID
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: &acctID, Factor: audit.FactorTOTP, Event: audit.EventRevoke, Detail: map[string]any{"reason": "reenroll"}})
	for range oldRecovery {
		audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: &acctID, Factor: audit.FactorRecoveryCode, Event: audit.EventRevoke, Detail: map[string]any{"reason": "reenroll"}})
	}
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: &acctID, Factor: audit.FactorTOTP, Event: audit.EventRegister})
	for range recoveryCodes {
		audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: &acctID, Factor: audit.FactorRecoveryCode, Event: audit.EventRegister})
	}
	logx.WithContext(r.Context()).WithFields(logrus.Fields{"event": "auth.totp_reenrolled", "account_id": acctID, "client_ip": s.clientIP.IP(r)}).Info("auth")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"recovery_codes": recoveryCodes})
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
