package server

import (
	"context"
	"encoding/base32"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/credential/totp"
	"prohibitorum/pkg/db"
)

// fakePwdTOTPTxRunner snapshots the in-memory query fake so handler tests can
// assert transaction rollback. Other TOTP handler tests share this seam.
type fakePwdTOTPTxRunner struct {
	q         *fakeSudoQueries
	commitErr error
}

func (r *fakePwdTOTPTxRunner) BeginEnrollmentTx(context.Context) (enrollmentTx, error) {
	var passwordRow *db.PasswordCredential
	if r.q.passwordRow != nil {
		row := *r.q.passwordRow
		passwordRow = &row
	}
	var totpRow *db.TotpCredential
	if r.q.totpRow != nil {
		row := *r.q.totpRow
		row.SecretEnc = append([]byte(nil), row.SecretEnc...)
		row.SecretNonce = append([]byte(nil), row.SecretNonce...)
		totpRow = &row
	}
	return &fakePwdTOTPTx{
		q: r.q, commitErr: r.commitErr, passwordRow: passwordRow, totpRow: totpRow,
		recoveryRows: append([]db.RecoveryCode(nil), r.q.recoveryRows...), nextRecID: r.q.nextRecID,
	}, nil
}

type fakePwdTOTPTx struct {
	q            *fakeSudoQueries
	commitErr    error
	committed    bool
	passwordRow  *db.PasswordCredential
	totpRow      *db.TotpCredential
	recoveryRows []db.RecoveryCode
	nextRecID    int32
}

func (tx *fakePwdTOTPTx) Queries() db.Querier { return tx.q }
func (tx *fakePwdTOTPTx) Commit(context.Context) error {
	if tx.commitErr != nil {
		return tx.commitErr
	}
	tx.committed = true
	return nil
}
func (tx *fakePwdTOTPTx) Rollback(context.Context) error {
	if tx.committed {
		return nil
	}
	tx.q.passwordRow = tx.passwordRow
	tx.q.totpRow = tx.totpRow
	tx.q.recoveryRows = append([]db.RecoveryCode(nil), tx.recoveryRows...)
	tx.q.nextRecID = tx.nextRecID
	return nil
}

func newPwdTOTPTestServer(t *testing.T) (*Server, *fakeSudoQueries, []byte) {
	t.Helper()
	s, f, dek := newSudoTestServer(t)
	s.enrollmentTxRunnerOverride = &fakePwdTOTPTxRunner{q: f}
	return s, f, dek
}

func backdateSession(s *Server, sess *authn.Session) {
	sess.Data.IssuedAt = time.Now().Add(-(s.config.Auth.SudoTTL + time.Minute))
}

func codeForSecret(t *testing.T, secretB32 string) string {
	t.Helper()
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secretB32)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	return totp.ComputeCodeForTesting(secret, time.Now().Unix(), 6)
}

func passwordTOTPCandidate(t *testing.T, s *Server) (string, string) {
	t.Helper()
	secret := browserTOTPSecret(t)
	return secret, codeForSecret(t, secret)
}

func passwordTOTPVerify(t *testing.T, s *Server, sess *authn.Session, password, secret, code string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"password":%q,"secret_base32":%q,"code":%q}`, password, secret, code)
	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/password-totp/verify", body)
	w := httptest.NewRecorder()
	s.handleMePasswordTOTPVerifyHTTP(w, r)
	return w
}

func TestMePasswordTOTPVerify_RequiresFreshSudo(t *testing.T) {
	s, _, _ := newPwdTOTPTestServer(t)
	_, sess := issueSudoTestSession(t, s, 42)
	backdateSession(s, sess)
	secret, code := passwordTOTPCandidate(t, s)

	w := passwordTOTPVerify(t, s, sess, "correct horse battery staple", secret, code)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", w.Code, w.Body.String())
	}
	if got := decodeJSON(t, w.Body.Bytes())["code"]; got != "sudo_required" {
		t.Fatalf("code = %v, want sudo_required", got)
	}
}

func TestMePasswordTOTPVerify_RequiresCompleteValidBody(t *testing.T) {
	s, _, _ := newPwdTOTPTestServer(t)
	token, sess := issueSudoTestSession(t, s, 42)
	grantFreshSudo(t, s, 42, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)
	secret, code := passwordTOTPCandidate(t, s)

	for _, tc := range []struct {
		name string
		body string
	}{
		{"missing password", fmt.Sprintf(`{"secret_base32":%q,"code":%q}`, secret, code)},
		{"short password", fmt.Sprintf(`{"password":"short","secret_base32":%q,"code":%q}`, secret, code)},
		{"long password", fmt.Sprintf(`{"password":%q,"secret_base32":%q,"code":%q}`, strings.Repeat("x", 1025), secret, code)},
		{"missing secret", `{"password":"correct horse","code":"123456"}`},
		{"missing code", fmt.Sprintf(`{"password":"correct horse","secret_base32":%q}`, secret)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/password-totp/verify", tc.body)
			w := httptest.NewRecorder()
			s.handleMePasswordTOTPVerifyHTTP(w, r)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestMePasswordTOTPVerify_InvalidCandidateLeavesFactorsUnchanged(t *testing.T) {
	s, f, dek := newPwdTOTPTestServer(t)
	_ = seedConfirmedTOTPSudo(t, s, f, dek, 42)
	seedPassword(t, s, 42, "old-password-value")
	oldPassword := *f.passwordRow
	oldTOTP := *f.totpRow
	oldCodes := append([]db.RecoveryCode(nil), f.recoveryRows...)
	token, sess := issueSudoTestSession(t, s, 42)
	grantFreshSudo(t, s, 42, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)
	secret, _ := passwordTOTPCandidate(t, s)

	w := passwordTOTPVerify(t, s, sess, "correct horse battery staple", secret, "000000")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", w.Code, w.Body.String())
	}
	if f.passwordRow.Hash != oldPassword.Hash || !slices.Equal(f.totpRow.SecretEnc, oldTOTP.SecretEnc) || !slices.Equal(f.recoveryRows, oldCodes) {
		t.Fatal("invalid candidate changed existing factors")
	}
}

func TestMePasswordTOTPVerify_EnrollsPasswordAndTOTPTogether(t *testing.T) {
	s, f, _ := newPwdTOTPTestServer(t)
	token, sess := issueSudoTestSession(t, s, 42)
	grantFreshSudo(t, s, 42, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)
	const password = "correct horse battery staple"
	secret, code := passwordTOTPCandidate(t, s)

	w := passwordTOTPVerify(t, s, sess, password, secret, code)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	codes, ok := decodeJSON(t, w.Body.Bytes())["recovery_codes"].([]any)
	if !ok || len(codes) != 10 {
		t.Fatalf("recovery_codes = %v, want 10", codes)
	}
	if err := s.passwordStore.Verify(context.Background(), 42, password); err != nil {
		t.Fatalf("password not usable: %v", err)
	}
	if f.totpRow == nil || !f.totpRow.ConfirmedAt.Valid || f.totpRow.LastStep <= 0 {
		t.Fatalf("confirmed TOTP with seeded last_step not stored: %+v", f.totpRow)
	}
	methods, err := authn.AvailableMethods(context.Background(), f, 42)
	if err != nil || !slices.Contains(methods, authn.MethodPasswordTOTP) {
		t.Fatalf("AvailableMethods = %v, err = %v", methods, err)
	}
}

func TestMePasswordTOTPVerify_ReplacesExistingFactors(t *testing.T) {
	s, f, dek := newPwdTOTPTestServer(t)
	_ = seedConfirmedTOTPSudo(t, s, f, dek, 42)
	seedPassword(t, s, 42, "old-password-value")
	oldSecret := append([]byte(nil), decryptTOTPSecret(t, dek, *f.totpRow, 42)...)
	oldCodes := append([]db.RecoveryCode(nil), f.recoveryRows...)
	token, sess := issueSudoTestSession(t, s, 42)
	grantFreshSudo(t, s, 42, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)
	secret, code := passwordTOTPCandidate(t, s)

	w := passwordTOTPVerify(t, s, sess, "correct horse battery staple", secret, code)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if err := s.passwordStore.Verify(context.Background(), 42, "correct horse battery staple"); err != nil {
		t.Fatalf("new password not usable: %v", err)
	}
	if err := s.passwordStore.Verify(context.Background(), 42, "old-password-value"); err == nil {
		t.Fatal("old password remains usable")
	}
	if slices.Equal(decryptTOTPSecret(t, dek, *f.totpRow, 42), oldSecret) || slices.Equal(f.recoveryRows, oldCodes) {
		t.Fatal("TOTP or recovery codes were not replaced")
	}
}

func TestMePasswordTOTPVerify_CommitFailureRollsBackAllFactors(t *testing.T) {
	s, f, dek := newPwdTOTPTestServer(t)
	_ = seedConfirmedTOTPSudo(t, s, f, dek, 42)
	seedPassword(t, s, 42, "old-password-value")
	oldPassword := *f.passwordRow
	oldTOTP := *f.totpRow
	oldTOTP.SecretEnc = append([]byte(nil), oldTOTP.SecretEnc...)
	oldTOTP.SecretNonce = append([]byte(nil), oldTOTP.SecretNonce...)
	oldCodes := append([]db.RecoveryCode(nil), f.recoveryRows...)
	oldEvents := len(f.events)
	token, sess := issueSudoTestSession(t, s, 42)
	grantFreshSudo(t, s, 42, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)
	secret, code := passwordTOTPCandidate(t, s)
	s.enrollmentTxRunnerOverride.(*fakePwdTOTPTxRunner).commitErr = errors.New("commit failed")

	w := passwordTOTPVerify(t, s, sess, "correct horse battery staple", secret, code)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", w.Code, w.Body.String())
	}
	if f.passwordRow.Hash != oldPassword.Hash || !slices.Equal(f.totpRow.SecretEnc, oldTOTP.SecretEnc) ||
		!slices.Equal(f.totpRow.SecretNonce, oldTOTP.SecretNonce) || !slices.Equal(f.recoveryRows, oldCodes) {
		t.Fatal("commit failure did not restore all factors")
	}
	if len(f.events) != oldEvents {
		t.Fatal("commit failure emitted audit events")
	}
}
