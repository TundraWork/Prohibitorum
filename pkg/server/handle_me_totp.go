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
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/logx"
)

const meTOTPCeremonyTTL = 10 * time.Minute

func meTOTPCeremonyKey(sess *authn.Session) string {
	return "me_totp:" + sess.Data.SessionID
}

type meTOTPStash struct {
	TOTPSecretBase32 string `json:"totp_secret_base32"`
	ExpiresAtUnixMS  int64  `json:"expires_at_unix_ms"`
	Claimed          bool   `json:"claimed,omitempty"`
}

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
	if sess == nil || sess.Data == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}
	if s.totpRequiresSudo(r.Context(), w, sess) {
		return
	}
	enr, err := s.totpStore.GenerateEnrollment(sess.Account.Username)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("me/totp/begin: generate: %w", err))
		return
	}
	raw, err := json.Marshal(meTOTPStash{
		TOTPSecretBase32: enr.SecretBase32,
		ExpiresAtUnixMS:  time.Now().Add(meTOTPCeremonyTTL).UnixMilli(),
	})
	if err != nil {
		writeAuthErr(w, fmt.Errorf("me/totp/begin: marshal: %w", err))
		return
	}
	if err := s.kvStore.SetEx(r.Context(), meTOTPCeremonyKey(sess), string(raw), meTOTPCeremonyTTL); err != nil {
		writeAuthErr(w, fmt.Errorf("me/totp/begin: setex: %w", err))
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
	if sess == nil || sess.Data == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}
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
	raw, err := s.kvStore.Get(r.Context(), meTOTPCeremonyKey(sess))
	if err != nil {
		writeAuthErr(w, authn.ErrCeremonyExpired())
		return
	}
	var stash meTOTPStash
	if err := json.Unmarshal([]byte(raw), &stash); err != nil {
		writeAuthErr(w, authn.ErrCeremonyState())
		return
	}
	if stash.Claimed {
		writeAuthErr(w, authn.ErrCeremonyExpired())
		return
	}
	remainingTTL := time.Until(time.UnixMilli(stash.ExpiresAtUnixMS))
	if remainingTTL <= 0 {
		_, _ = s.kvStore.CompareAndDelete(r.Context(), meTOTPCeremonyKey(sess), raw)
		writeAuthErr(w, authn.ErrCeremonyExpired())
		return
	}
	matchedStep, ok := s.totpStore.VerifyCandidateSecret(stash.TOTPSecretBase32, body.Code)
	if !ok {
		writeAuthErr(w, authn.ErrBadCredentials())
		return
	}

	claimed := stash
	claimed.Claimed = true
	claimedBytes, err := json.Marshal(claimed)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("me/totp/verify: marshal claim: %w", err))
		return
	}
	claimedRaw := string(claimedBytes)
	claimedOK, err := s.kvStore.CompareAndSwap(
		r.Context(), meTOTPCeremonyKey(sess), raw, claimedRaw, remainingTTL,
	)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("me/totp/verify: claim candidate: %w", err))
		return
	}
	if !claimedOK {
		writeAuthErr(w, authn.ErrCeremonyExpired())
		return
	}
	committed := false
	defer func() {
		if !committed {
			restoreTTL := time.Until(time.UnixMilli(stash.ExpiresAtUnixMS))
			if restoreTTL <= 0 {
				_, _ = s.kvStore.CompareAndDelete(r.Context(), meTOTPCeremonyKey(sess), claimedRaw)
				return
			}
			_, _ = s.kvStore.CompareAndSwap(
				r.Context(), meTOTPCeremonyKey(sess), claimedRaw, raw, restoreTTL,
			)
		}
	}()

	hadConfirmedTOTP := false
	tx, err := s.beginEnrollmentTx(r.Context())
	if err != nil {
		writeAuthErr(w, fmt.Errorf("me/totp/verify: begin tx: %w", err))
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	qtx := tx.Queries()
	if old, getErr := qtx.GetTOTPCredential(r.Context(), sess.Account.ID); getErr == nil {
		hadConfirmedTOTP = old.ConfirmedAt.Valid
	} else if !errors.Is(getErr, pgx.ErrNoRows) {
		writeAuthErr(w, fmt.Errorf("me/totp/verify: get current totp: %w", getErr))
		return
	}
	oldRecovery, err := qtx.ListRecoveryCodesByAccount(r.Context(), sess.Account.ID)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("me/totp/verify: list recovery codes: %w", err))
		return
	}
	oldRecoveryCount := len(oldRecovery)
	recoveryCodes, err := s.totpStore.EnrollConfirmedForTx(
		r.Context(), qtx, sess.Account.ID, stash.TOTPSecretBase32, matchedStep,
	)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("me/totp/verify: enroll totp: %w", err))
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeAuthErr(w, fmt.Errorf("me/totp/verify: commit: %w", err))
		return
	}
	committed = true

	acctID := sess.Account.ID
	if hadConfirmedTOTP {
		audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
			AccountID: &acctID, Factor: audit.FactorTOTP, Event: audit.EventRevoke,
			Detail: map[string]any{"reason": "reenroll"},
		})
	}
	for range oldRecoveryCount {
		audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
			AccountID: &acctID, Factor: audit.FactorRecoveryCode, Event: audit.EventRevoke,
			Detail: map[string]any{"reason": "reenroll"},
		})
	}
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: &acctID, Factor: audit.FactorTOTP, Event: audit.EventRegister})
	for range recoveryCodes {
		audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: &acctID, Factor: audit.FactorRecoveryCode, Event: audit.EventRegister})
	}
	logEvent := "auth.totp_enrolled"
	if hadConfirmedTOTP {
		logEvent = "auth.totp_reenrolled"
	}
	logx.WithContext(r.Context()).WithFields(logrus.Fields{
		"event": logEvent, "account_id": acctID, "client_ip": s.clientIP.IP(r),
	}).Info("auth")

	_, _ = s.kvStore.CompareAndDelete(r.Context(), meTOTPCeremonyKey(sess), claimedRaw)
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
