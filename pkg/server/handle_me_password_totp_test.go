// Package server — handle_me_password_totp_test.go
//
// Unit tests for POST /me/password-totp/{begin,verify}. Reuses the sudo test
// scaffolding (newSudoTestServer / issueSudoTestSession / sudoReq /
// seedConfirmedTOTPSudo / seedPassword) plus a trivial enrollmentTx runner
// that hands the handler the same fake db.Querier, so the transaction body can
// be exercised without a pgx pool.

package server

import (
	"context"
	"encoding/base32"
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

// fakePwdTOTPTxRunner is the enrollmentTx seam over fakeSudoQueries. Commit /
// Rollback are no-ops: the fake has no journal, and every failure path in the
// handler stops before commit anyway.
type fakePwdTOTPTxRunner struct{ q db.Querier }

func (r *fakePwdTOTPTxRunner) BeginEnrollmentTx(context.Context) (enrollmentTx, error) {
	return &fakePwdTOTPTx{q: r.q}, nil
}

type fakePwdTOTPTx struct{ q db.Querier }

func (tx *fakePwdTOTPTx) Queries() db.Querier            { return tx.q }
func (tx *fakePwdTOTPTx) Commit(context.Context) error   { return nil }
func (tx *fakePwdTOTPTx) Rollback(context.Context) error { return nil }

func newPwdTOTPTestServer(t *testing.T) (*Server, *fakeSudoQueries, []byte) {
	t.Helper()
	s, f, dek := newSudoTestServer(t)
	s.enrollmentTxRunnerOverride = &fakePwdTOTPTxRunner{q: f}
	return s, f, dek
}

// backdateSession pushes the session outside the recent-auth window so
// hasFreshSudo fails without a stamped SudoUntil.
func backdateSession(s *Server, sess *authn.Session) {
	sess.Data.IssuedAt = time.Now().Add(-(s.config.Auth.SudoTTL + time.Minute))
}

func pwdTOTPBegin(t *testing.T, s *Server, sess *authn.Session, password string) string {
	t.Helper()
	body := fmt.Sprintf(`{"password":%q}`, password)
	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/password-totp/begin", body)
	w := httptest.NewRecorder()
	s.handleMePasswordTOTPBeginHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("begin status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	decoded := decodeJSON(t, w.Body.Bytes())
	secret, _ := decoded["secret_base32"].(string)
	if secret == "" {
		t.Fatalf("begin response missing secret_base32: %v", decoded)
	}
	if uri, _ := decoded["otpauth_uri"].(string); uri == "" {
		t.Errorf("begin response missing otpauth_uri")
	}
	return secret
}

func pwdTOTPVerify(t *testing.T, s *Server, sess *authn.Session, code string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"code":%q}`, code)
	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/password-totp/verify", body)
	w := httptest.NewRecorder()
	s.handleMePasswordTOTPVerifyHTTP(w, r)
	return w
}

func codeForSecret(t *testing.T, secretB32 string) string {
	t.Helper()
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secretB32)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	return totp.ComputeCodeForTesting(secret, time.Now().Unix(), 6)
}

func TestMePasswordTOTP_RequiresFreshSudo(t *testing.T) {
	s, _, _ := newPwdTOTPTestServer(t)
	const accountID int32 = 42
	_, sess := issueSudoTestSession(t, s, accountID)
	backdateSession(s, sess)

	for _, tc := range []struct {
		name   string
		handle func(*httptest.ResponseRecorder)
	}{
		{"begin", func(w *httptest.ResponseRecorder) {
			r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/password-totp/begin", `{"password":"correct horse"}`)
			s.handleMePasswordTOTPBeginHTTP(w, r)
		}},
		{"verify", func(w *httptest.ResponseRecorder) {
			r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/password-totp/verify", `{"code":"123456"}`)
			s.handleMePasswordTOTPVerifyHTTP(w, r)
		}},
	} {
		w := httptest.NewRecorder()
		tc.handle(w)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: status want 401, got %d (body=%s)", tc.name, w.Code, w.Body.String())
			continue
		}
		if got := decodeJSON(t, w.Body.Bytes())["code"]; got != "sudo_required" {
			t.Errorf("%s: code want sudo_required, got %v", tc.name, got)
		}
	}
}

func TestMePasswordTOTPBegin_PasswordBounds(t *testing.T) {
	s, _, _ := newPwdTOTPTestServer(t)
	const accountID int32 = 42
	_, sess := issueSudoTestSession(t, s, accountID)

	for _, tc := range []struct {
		name string
		pw   string
	}{
		{"too short", "short"},
		{"too long", string(make([]byte, 1025))},
	} {
		r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/password-totp/begin",
			fmt.Sprintf(`{"password":%q}`, tc.pw))
		w := httptest.NewRecorder()
		s.handleMePasswordTOTPBeginHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status want 400, got %d (body=%s)", tc.name, w.Code, w.Body.String())
		}
	}
}

func TestMePasswordTOTPBegin_WritesOnlyKVStash(t *testing.T) {
	s, f, _ := newPwdTOTPTestServer(t)
	const accountID int32 = 42
	_, sess := issueSudoTestSession(t, s, accountID)

	secret := pwdTOTPBegin(t, s, sess, "correct horse battery staple")

	if f.passwordRow != nil {
		t.Errorf("begin must not write a password row, got %+v", f.passwordRow)
	}
	if f.totpRow != nil {
		t.Errorf("begin must not write a totp row, got %+v", f.totpRow)
	}
	raw, err := s.kvStore.Get(context.Background(), mePwdTOTPCeremonyKey(sess))
	if err != nil {
		t.Fatalf("ceremony stash missing after begin: %v", err)
	}
	if want := fmt.Sprintf(`"totp_secret_base32":%q`, secret); !strings.Contains(raw, want) {
		t.Errorf("stash does not carry the returned secret; raw=%s", raw)
	}
}

func TestMePasswordTOTPVerify_WrongCodeRetriesThenSucceeds(t *testing.T) {
	s, f, _ := newPwdTOTPTestServer(t)
	const accountID int32 = 42
	_, sess := issueSudoTestSession(t, s, accountID)
	secret := pwdTOTPBegin(t, s, sess, "correct horse battery staple")

	w := pwdTOTPVerify(t, s, sess, "000000")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong code: status want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
	if got := decodeJSON(t, w.Body.Bytes())["code"]; got != "bad_credentials" {
		t.Errorf("wrong code: code want bad_credentials, got %v", got)
	}
	if f.passwordRow != nil || f.totpRow != nil {
		t.Error("a rejected code must leave the account untouched")
	}
	if _, err := s.kvStore.Get(context.Background(), mePwdTOTPCeremonyKey(sess)); err != nil {
		t.Fatalf("stash must survive a wrong code: %v", err)
	}

	w = pwdTOTPVerify(t, s, sess, codeForSecret(t, secret))
	if w.Code != http.StatusOK {
		t.Fatalf("correct code: status want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestMePasswordTOTPVerify_EnrollsPasswordAndTOTPTogether(t *testing.T) {
	s, f, _ := newPwdTOTPTestServer(t)
	const accountID int32 = 42
	_, sess := issueSudoTestSession(t, s, accountID)
	const pw = "correct horse battery staple"
	secret := pwdTOTPBegin(t, s, sess, pw)

	w := pwdTOTPVerify(t, s, sess, codeForSecret(t, secret))
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	codes, ok := decodeJSON(t, w.Body.Bytes())["recovery_codes"].([]any)
	if !ok || len(codes) != 10 {
		t.Fatalf("recovery_codes: want 10 entries, got %v", codes)
	}

	if err := s.passwordStore.Verify(context.Background(), accountID, pw); err != nil {
		t.Errorf("password not usable after verify: %v", err)
	}
	if f.totpRow == nil || !f.totpRow.ConfirmedAt.Valid {
		t.Fatalf("totp credential not confirmed: %+v", f.totpRow)
	}
	if len(f.recoveryRows) != 10 {
		t.Errorf("recovery rows: want 10, got %d", len(f.recoveryRows))
	}
	methods, err := authn.AvailableMethods(context.Background(), f, accountID)
	if err != nil {
		t.Fatalf("AvailableMethods: %v", err)
	}
	if !slices.Contains(methods, authn.MethodPasswordTOTP) {
		t.Errorf("AvailableMethods = %v, want password_totp", methods)
	}
	if _, err := s.kvStore.Get(context.Background(), mePwdTOTPCeremonyKey(sess)); err == nil {
		t.Error("ceremony stash should be deleted after a successful verify")
	}
}

func TestMePasswordTOTPVerify_MissingStash(t *testing.T) {
	s, _, _ := newPwdTOTPTestServer(t)
	const accountID int32 = 42
	_, sess := issueSudoTestSession(t, s, accountID)

	w := pwdTOTPVerify(t, s, sess, "123456")
	// ceremony_expired is the shared ceremony AuthError (400), same as the
	// enrollment-stash miss on /enrollments/{token}/password-totp/verify.
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
	if got := decodeJSON(t, w.Body.Bytes())["code"]; got != "ceremony_expired" {
		t.Errorf("code want ceremony_expired, got %v", got)
	}
}

func TestMePasswordTOTPVerify_ReplacesExistingFactors(t *testing.T) {
	s, f, dek := newPwdTOTPTestServer(t)
	const accountID int32 = 42
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	seedPassword(t, s, accountID, "old-password-value")
	oldSecret := append([]byte(nil), decryptTOTPSecret(t, dek, *f.totpRow, accountID)...)
	oldCodes := make([]string, len(f.recoveryRows))
	for i, r := range f.recoveryRows {
		oldCodes[i] = r.Hash
	}

	_, sess := issueSudoTestSession(t, s, accountID)
	const pw = "correct horse battery staple"
	secret := pwdTOTPBegin(t, s, sess, pw)
	w := pwdTOTPVerify(t, s, sess, codeForSecret(t, secret))
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}

	if err := s.passwordStore.Verify(context.Background(), accountID, pw); err != nil {
		t.Errorf("new password not usable: %v", err)
	}
	if err := s.passwordStore.Verify(context.Background(), accountID, "old-password-value"); err == nil {
		t.Error("old password still usable after verify")
	}
	if newSecret := decryptTOTPSecret(t, dek, *f.totpRow, accountID); slices.Equal(newSecret, oldSecret) {
		t.Error("totp secret not replaced")
	}
	if len(f.recoveryRows) != 10 {
		t.Errorf("recovery rows: want 10, got %d", len(f.recoveryRows))
	}
	rotated := 0
	for _, r := range f.recoveryRows {
		if !slices.Contains(oldCodes, r.Hash) {
			rotated++
		}
	}
	if rotated != 10 {
		t.Errorf("recovery codes not fully rotated: %d of 10 new", rotated)
	}
}
