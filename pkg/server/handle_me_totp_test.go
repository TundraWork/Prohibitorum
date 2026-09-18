package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
)

func withMeTOTPOverride(s *Server, f *fakeSudoQueries) {
	s.meTOTPFlowOverride = f
	s.enrollmentTxRunnerOverride = &fakePwdTOTPTxRunner{q: f}
}

func newCandidate(t *testing.T, s *Server) (string, string) {
	t.Helper()
	secret := browserTOTPSecret(t)
	return secret, codeForSecret(t, secret)
}

func meTOTPVerify(t *testing.T, s *Server, sess *authn.Session, secret, code string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"secret_base32":%q,"code":%q}`, secret, code)
	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/totp/verify", body)
	w := httptest.NewRecorder()
	s.handleMeTOTPVerifyHTTP(w, r)
	return w
}

func TestMeTOTPVerify_RequiresFreshSudo(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	_ = seedConfirmedTOTPSudo(t, s, f, dek, 42)
	_, sess := issueSudoTestSession(t, s, 42)
	backdateSession(s, sess)
	secret, code := newCandidate(t, s)
	w := meTOTPVerify(t, s, sess, secret, code)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", w.Code, w.Body.String())
	}
	if got := decodeJSON(t, w.Body.Bytes())["code"]; got != "sudo_required" {
		t.Fatalf("code = %v, want sudo_required", got)
	}
}

func TestMeTOTPVerify_RejectsFirstTimeAndUnconfirmedSetup(t *testing.T) {
	for _, tc := range []struct {
		name string
		seed func(*fakeSudoQueries)
	}{
		{"missing", func(*fakeSudoQueries) {}},
		{"unconfirmed", func(f *fakeSudoQueries) { f.totpRow = &db.TotpCredential{AccountID: 42} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f, _ := newSudoTestServer(t)
			withMeTOTPOverride(s, f)
			tc.seed(f)
			token, sess := issueSudoTestSession(t, s, 42)
			grantFreshSudo(t, s, 42, token)
			sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)
			secret, code := newCandidate(t, s)
			w := meTOTPVerify(t, s, sess, secret, code)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
			}
			if f.totpRow != nil && f.totpRow.ConfirmedAt.Valid {
				t.Fatal("reset endpoint created a confirmed credential")
			}
			if len(f.recoveryRows) != 0 {
				t.Fatal("reset endpoint created recovery codes")
			}
		})
	}
}

func TestMeTOTPVerify_InvalidCandidateDoesNotMutate(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	_ = seedConfirmedTOTPSudo(t, s, f, dek, 42)
	oldTOTP := *f.totpRow
	oldCodes := append([]db.RecoveryCode(nil), f.recoveryRows...)
	token, sess := issueSudoTestSession(t, s, 42)
	grantFreshSudo(t, s, 42, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)
	secret, _ := newCandidate(t, s)
	w := meTOTPVerify(t, s, sess, secret, "000000")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", w.Code, w.Body.String())
	}
	if !slices.Equal(f.totpRow.SecretEnc, oldTOTP.SecretEnc) || !slices.Equal(f.recoveryRows, oldCodes) {
		t.Fatal("invalid candidate changed credentials")
	}
}

func TestMeTOTPVerify_ReplacesConfirmedTOTPAndSeedsReplayStep(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	_ = seedConfirmedTOTPSudo(t, s, f, dek, 42)
	oldSecret := append([]byte(nil), decryptTOTPSecret(t, dek, *f.totpRow, 42)...)
	oldCodes := append([]db.RecoveryCode(nil), f.recoveryRows...)
	token, sess := issueSudoTestSession(t, s, 42)
	grantFreshSudo(t, s, 42, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)
	secret, code := newCandidate(t, s)
	w := meTOTPVerify(t, s, sess, secret, code)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	codes, ok := decodeJSON(t, w.Body.Bytes())["recovery_codes"].([]any)
	if !ok || len(codes) != 10 {
		t.Fatalf("recovery_codes = %v, want 10", codes)
	}
	if got := decryptTOTPSecret(t, dek, *f.totpRow, 42); slices.Equal(got, oldSecret) {
		t.Fatal("TOTP secret was not replaced")
	}
	if slices.Equal(f.recoveryRows, oldCodes) {
		t.Fatal("recovery codes were not replaced")
	}
	if f.totpRow.LastStep <= 0 {
		t.Fatalf("last_step = %d, want confirming step", f.totpRow.LastStep)
	}
}

func TestMeTOTPVerify_CommitFailureRollsBack(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	_ = seedConfirmedTOTPSudo(t, s, f, dek, 42)
	oldTOTP := *f.totpRow
	oldTOTP.SecretEnc = append([]byte(nil), oldTOTP.SecretEnc...)
	oldTOTP.SecretNonce = append([]byte(nil), oldTOTP.SecretNonce...)
	oldCodes := append([]db.RecoveryCode(nil), f.recoveryRows...)
	oldEventCount := len(f.events)
	token, sess := issueSudoTestSession(t, s, 42)
	grantFreshSudo(t, s, 42, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)
	secret, code := newCandidate(t, s)
	s.enrollmentTxRunnerOverride.(*fakePwdTOTPTxRunner).commitErr = errors.New("commit failed")
	w := meTOTPVerify(t, s, sess, secret, code)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", w.Code, w.Body.String())
	}
	if !slices.Equal(f.totpRow.SecretEnc, oldTOTP.SecretEnc) || !slices.Equal(f.totpRow.SecretNonce, oldTOTP.SecretNonce) || !slices.Equal(f.recoveryRows, oldCodes) {
		t.Fatal("commit failure did not restore credentials")
	}
	if len(f.events) != oldEventCount {
		t.Fatal("commit failure emitted audit events")
	}
}

func TestMeTOTPVerify_RequiresCompleteBody(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	_ = seedConfirmedTOTPSudo(t, s, f, dek, 42)
	token, sess := issueSudoTestSession(t, s, 42)
	grantFreshSudo(t, s, 42, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)
	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/totp/verify", `{"code":"123456"}`)
	w := httptest.NewRecorder()
	s.handleMeTOTPVerifyHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestMeRegenerateRecoveryCodes_RequiresSudo(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	_, sess := issueSudoTestSession(t, s, accountID)
	// Backdate IssuedAt so the recent-auth window doesn't apply; no SudoUntil
	// set, so the gate must deny with sudo_required.
	sess.Data.IssuedAt = time.Now().Add(-(s.config.Auth.SudoTTL + time.Minute))

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/recovery-codes/regenerate", "")
	w := httptest.NewRecorder()
	s.handleMeRegenerateRecoveryCodesHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w.Body.Bytes())
	if body["code"] != "sudo_required" {
		t.Errorf("code: want sudo_required, got %v", body["code"])
	}
}

func TestMeRegenerateRecoveryCodes_RequiresConfirmedTOTP(t *testing.T) {
	s, f, _ := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	// No TOTP row at all. Even with sudo, the precondition rejects.
	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/recovery-codes/regenerate", "")
	w := httptest.NewRecorder()
	s.handleMeRegenerateRecoveryCodesHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestMeRegenerateRecoveryCodes_UnconfirmedTOTPRejected(t *testing.T) {
	s, f, _ := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	// Unconfirmed row — same rejection path as missing.
	f.totpRow = &db.TotpCredential{AccountID: accountID}
	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/recovery-codes/regenerate", "")
	w := httptest.NewRecorder()
	s.handleMeRegenerateRecoveryCodesHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestMeRegenerateRecoveryCodes_Success(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	originalCodes := seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/recovery-codes/regenerate", "")
	w := httptest.NewRecorder()
	s.handleMeRegenerateRecoveryCodesHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w.Body.Bytes())
	codes, ok := body["recovery_codes"].([]any)
	if !ok {
		t.Fatalf("recovery_codes missing: %v", body)
	}
	if len(codes) != 10 {
		t.Errorf("recovery_codes: want 10, got %d", len(codes))
	}
	// New codes must differ from the seed set.
	overlap := 0
	for _, c := range codes {
		got := c.(string)
		for _, o := range originalCodes {
			if got == o {
				overlap++
			}
		}
	}
	if overlap > 0 {
		t.Errorf("regenerated codes overlap with originals (%d matches)", overlap)
	}
	// Storage rowset should now be 10 unused, no leftover used-but-active
	// rows from the prior set.
	live, _ := f.ListRecoveryCodesByAccount(context.Background(), accountID)
	if len(live) != 10 {
		t.Errorf("live recovery rows: want 10, got %d", len(live))
	}
}
