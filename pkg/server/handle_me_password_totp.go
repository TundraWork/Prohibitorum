// Package server — handle_me_password_totp.go
//
// Session-scoped password+TOTP enrollment. /me/password/set writes the
// password and commits; a user who leaves at the authenticator step is left
// with a password and no confirmed TOTP, which /auth/password/begin accepts
// while the login page then demands a code their account cannot produce
// (authn.AvailableMethods deliberately does not count that pair as
// password_totp). This pair of endpoints writes both factors in ONE
// transaction, so abandoning the ceremony leaves the account exactly as it
// was — no repair entry point is needed.
//
// Shape mirrors handle_enrollment_password_totp.go with the enrollment
// intents, account creation and session issue removed: the account already
// exists and is identified by the session.
//
//	POST /api/prohibitorum/me/password-totp/begin {password}
//	  → 200 {secret_base32, otpauth_uri}
//	  Fresh sudo required. Hashes the password, generates a TOTP secret (NO DB
//	  write) and stashes both in KV keyed by the opaque session id.
//
//	POST /api/prohibitorum/me/password-totp/verify {code}
//	  → 200 {recovery_codes:[...]}
//	  Fresh sudo required (elevation is taken at begin, so the modal cannot
//	  interrupt code entry — same hoisting as /me/totp/begin). Verifies the
//	  code against the stashed secret, then in ONE tx upserts the password
//	  credential and enrolls confirmed TOTP + fresh recovery codes. Audit runs
//	  post-commit.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/logx"
)

// mePwdTOTPCeremonyTTL bounds the scan-QR-then-type-code window. Matches the
// enrollment ceremony: typing a code from an authenticator app is slower than
// a passkey tap (5m).
const mePwdTOTPCeremonyTTL = 10 * time.Minute

// mePwdTOTPCeremonyKey keys the stash on the opaque SessionData.SessionID
// (never the raw session token), matching addPasskeyCeremonyKey. Callers MUST
// have verified sess.Data != nil.
func mePwdTOTPCeremonyKey(sess *authn.Session) string {
	return "me_pwdtotp:" + sess.Data.SessionID
}

// mePwdTOTPStash is the KV payload between begin and verify. The password is
// an argon2id PHC (never plaintext); the base32 TOTP secret was already
// returned to the client for the QR and is encrypted at rest only once it
// lands in totp_credential.
type mePwdTOTPStash struct {
	PasswordPHC      string `json:"password_phc"`
	TOTPSecretBase32 string `json:"totp_secret_base32"`
}

// ----- POST /me/password-totp/begin (raw chi) ----------------------------

func (s *Server) handleMePasswordTOTPBeginHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if sess == nil || sess.Data == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}
	// Unconditional: verify replaces whatever password and TOTP the account
	// already has, so there is no state in which the caller needs no sudo.
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
	// OWASP 2026 §5.1.1.2 bounds — mirror handleMePasswordSetHTTP.
	if len(body.Password) < 8 || len(body.Password) > 1024 {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	phc, err := s.passwordStore.Hash(body.Password)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("me/password-totp/begin: hash: %w", err))
		return
	}
	enr, err := s.totpStore.GenerateEnrollment(sess.Account.Username)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("me/password-totp/begin: totp: %w", err))
		return
	}

	raw, err := json.Marshal(mePwdTOTPStash{PasswordPHC: phc, TOTPSecretBase32: enr.SecretBase32})
	if err != nil {
		writeAuthErr(w, fmt.Errorf("me/password-totp/begin: marshal: %w", err))
		return
	}
	if err := s.kvStore.SetEx(r.Context(), mePwdTOTPCeremonyKey(sess), string(raw), mePwdTOTPCeremonyTTL); err != nil {
		writeAuthErr(w, fmt.Errorf("me/password-totp/begin: setex: %w", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"secret_base32": enr.SecretBase32,
		"otpauth_uri":   enr.ProvisioningURI,
	})
}

// ----- POST /me/password-totp/verify (raw chi) ---------------------------

func (s *Server) handleMePasswordTOTPVerifyHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if sess == nil || sess.Data == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}
	if s.requireFreshSudo(r.Context(), w, sess) {
		return
	}

	// Non-destructive Get: a wrong code is retryable, the stash TTL is the only
	// bound.
	raw, err := s.kvStore.Get(r.Context(), mePwdTOTPCeremonyKey(sess))
	if err != nil {
		writeAuthErr(w, authn.ErrCeremonyExpired())
		return
	}
	var stash mePwdTOTPStash
	if err := json.Unmarshal([]byte(raw), &stash); err != nil {
		writeAuthErr(w, authn.ErrCeremonyState())
		return
	}

	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Code == "" {
		writeAuthErr(w, authn.ErrBadCredentials())
		return
	}
	matchedStep, ok := s.totpStore.VerifyCandidateSecret(stash.TOTPSecretBase32, body.Code)
	if !ok {
		writeAuthErr(w, authn.ErrBadCredentials())
		return
	}

	tx, err := s.beginEnrollmentTx(r.Context())
	if err != nil {
		writeAuthErr(w, fmt.Errorf("me/password-totp/verify: begin tx: %w", err))
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	qtx := tx.Queries()

	if err := qtx.UpsertPasswordCredential(r.Context(), db.UpsertPasswordCredentialParams{
		AccountID: sess.Account.ID,
		Hash:      stash.PasswordPHC,
	}); err != nil {
		writeAuthErr(w, fmt.Errorf("me/password-totp/verify: set password: %w", err))
		return
	}
	// Wipes any prior TOTP credential and recovery codes, then writes the
	// confirmed TOTP + a fresh batch — the pair lands together or not at all.
	recoveryCodes, err := s.totpStore.EnrollConfirmedForTx(r.Context(), qtx, sess.Account.ID, stash.TOTPSecretBase32, matchedStep)
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

	// Post-commit audit: the credential rows are now visible.
	acctID := sess.Account.ID
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: &acctID, Factor: audit.FactorPassword, Event: audit.EventRegister})
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: &acctID, Factor: audit.FactorTOTP, Event: audit.EventRegister})
	for range recoveryCodes {
		audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: &acctID, Factor: audit.FactorRecoveryCode, Event: audit.EventRegister})
	}

	// Best-effort stash cleanup.
	_ = s.kvStore.Del(r.Context(), mePwdTOTPCeremonyKey(sess))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"recovery_codes": recoveryCodes})
}
