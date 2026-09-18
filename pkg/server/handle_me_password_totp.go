// Package server — handle_me_password_totp.go
//
// Signed-in password and TOTP setup. The browser supplies the password, its
// locally generated TOTP secret, and the current code in one request. The
// server validates both candidates before atomically replacing the factors.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/logx"
)

// POST /api/prohibitorum/me/password-totp/verify
// Body: {password, secret_base32, code}
func (s *Server) handleMePasswordTOTPVerifyHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if s.requireFreshSudo(r.Context(), w, sess) {
		return
	}

	var body struct {
		Password     string `json:"password"`
		SecretBase32 string `json:"secret_base32"`
		Code         string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil ||
		len(body.Password) < 8 || len(body.Password) > 1024 ||
		body.SecretBase32 == "" || body.Code == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	matchedStep, ok := s.totpStore.VerifyCandidateSecret(body.SecretBase32, body.Code)
	if !ok {
		writeAuthErr(w, authn.ErrBadCredentials())
		return
	}
	passwordPHC, err := s.passwordStore.Hash(body.Password)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("me/password-totp/verify: hash: %w", err))
		return
	}

	tx, err := s.beginEnrollmentTx(r.Context())
	if err != nil {
		writeAuthErr(w, fmt.Errorf("me/password-totp/verify: begin tx: %w", err))
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	qtx := tx.Queries()
	if _, err := qtx.GetAccountByIDForUpdate(r.Context(), sess.Account.ID); err != nil {
		writeAuthErr(w, fmt.Errorf("me/password-totp/verify: lock account: %w", err))
		return
	}
	if err := qtx.UpsertPasswordCredential(r.Context(), db.UpsertPasswordCredentialParams{
		AccountID: sess.Account.ID,
		Hash:      passwordPHC,
	}); err != nil {
		writeAuthErr(w, fmt.Errorf("me/password-totp/verify: set password: %w", err))
		return
	}
	recoveryCodes, err := s.totpStore.EnrollConfirmedForTx(
		r.Context(), qtx, sess.Account.ID, body.SecretBase32, matchedStep,
	)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("me/password-totp/verify: enroll totp: %w", err))
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeAuthErr(w, fmt.Errorf("me/password-totp/verify: commit: %w", err))
		return
	}

	logx.WithContext(r.Context()).WithFields(logrus.Fields{
		"event":      "auth.password_totp_enrolled",
		"account_id": sess.Account.ID,
		"client_ip":  s.clientIP.IP(r),
	}).Info("auth")
	acctID := sess.Account.ID
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: &acctID, Factor: audit.FactorPassword, Event: audit.EventRegister})
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: &acctID, Factor: audit.FactorTOTP, Event: audit.EventRegister})
	for range recoveryCodes {
		audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: &acctID, Factor: audit.FactorRecoveryCode, Event: audit.EventRegister})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"recovery_codes": recoveryCodes})
}
