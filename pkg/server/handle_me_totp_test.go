// Package server — handle_me_totp_test.go
//
// Unit tests for the /me/totp/* endpoints: enrollment begin/verify and
// recovery-code regeneration. Conditional-sudo branches (no row / unconfirmed
// row / confirmed row) and the recovery-code first-mint vs. subsequent-
// verify behaviour are both exercised here.
//
// Reuses fakeSudoQueries from handle_sudo_test.go for the DB surface. The
// fake satisfies meTOTPFlowQueries via its GetTOTPCredential method, so we
// just wire it onto s.meTOTPFlowOverride alongside the sudo override.

package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/credential/totp"
	"prohibitorum/pkg/db"
)

// withMeTOTPOverride wires the existing fakeSudoQueries onto
// s.meTOTPFlowOverride so /me/totp/* handlers read from it.
func withMeTOTPOverride(s *Server, f *fakeSudoQueries) {
	s.meTOTPFlowOverride = f
}

func TestMeTOTPBegin_FirstTimeNoSudoRequired(t *testing.T) {
	s, f, _ := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	_, sess := issueSudoTestSession(t, s, accountID)
	// No TOTP row exists — totpRequiresSudo should fall through without
	// gating, and the handler should mint a new secret.

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/totp/begin", "")
	w := httptest.NewRecorder()
	s.handleMeTOTPBeginHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w.Body.Bytes())
	if _, ok := body["secret_base32"].(string); !ok {
		t.Errorf("response missing secret_base32: %v", body)
	}
	if uri, _ := body["otpauth_uri"].(string); uri == "" {
		t.Errorf("response missing otpauth_uri")
	}
	if f.totpRow == nil {
		t.Errorf("totp row not inserted")
	}
	if f.totpRow != nil && f.totpRow.ConfirmedAt.Valid {
		t.Errorf("totp row should be unconfirmed after Begin")
	}
}

func TestMeTOTPBegin_UnconfirmedNoSudoRequired(t *testing.T) {
	s, f, _ := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	// Seed an unconfirmed row directly.
	f.totpRow = &db.TotpCredential{AccountID: accountID, ConfirmedAt: pgtype.Timestamptz{}}
	_, sess := issueSudoTestSession(t, s, accountID)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/totp/begin", "")
	w := httptest.NewRecorder()
	s.handleMeTOTPBeginHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestMeTOTPBegin_ConfirmedRequiresSudo(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	_, sess := issueSudoTestSession(t, s, accountID)
	// Backdate IssuedAt so the recent-auth window doesn't apply; no SudoUntil
	// set — re-enroll must fail.
	sess.Data.IssuedAt = time.Now().Add(-(s.config.Auth.SudoTTL + time.Minute))

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/totp/begin", "")
	w := httptest.NewRecorder()
	s.handleMeTOTPBeginHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w.Body.Bytes())
	if body["code"] != "sudo_required" {
		t.Errorf("code: want sudo_required, got %v", body["code"])
	}
}

func TestMeTOTPBegin_ConfirmedWithSudo(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/totp/begin", "")
	w := httptest.NewRecorder()
	s.handleMeTOTPBeginHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	// The Begin call wiped the prior confirmed row and inserted a fresh
	// unconfirmed one.
	if f.totpRow == nil || f.totpRow.ConfirmedAt.Valid {
		t.Errorf("re-enroll should produce an unconfirmed row, got %+v", f.totpRow)
	}
}

func TestMeTOTPVerify_FirstSuccessReturnsRecoveryCodes(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	// Begin an enrollment so a row exists but is unconfirmed.
	if _, err := s.totpStore.Begin(context.Background(), accountID, "alice"); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	_, sess := issueSudoTestSession(t, s, accountID)

	code := totp.ComputeCodeForTesting(decryptTOTPSecret(t, dek, *f.totpRow, accountID), time.Now().Unix(), 6)
	body := fmt.Sprintf(`{"code":%q}`, code)
	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/totp/verify", body)
	w := httptest.NewRecorder()
	s.handleMeTOTPVerifyHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	decoded := decodeJSON(t, w.Body.Bytes())
	codes, ok := decoded["recovery_codes"].([]any)
	if !ok {
		t.Fatalf("missing recovery_codes: %v", decoded)
	}
	if len(codes) != 10 {
		t.Errorf("recovery_codes: want 10, got %d", len(codes))
	}
	if f.totpRow == nil || !f.totpRow.ConfirmedAt.Valid {
		t.Errorf("first-verify should confirm the row")
	}
}

func TestMeTOTPVerify_SubsequentReturns204(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	// Confirmed row now exists, so verify requires sudo.
	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	// The seed Verify consumed LastStep = stepFor(now). A second Verify at
	// the same wall-clock step would replay. Reset LastStep so a fresh
	// current-step code is accepted; the row is already confirmed, so
	// Verify returns (nil, nil) — handler writes 204.
	f.totpRow.LastStep = 0
	code := totp.ComputeCodeForTesting(decryptTOTPSecret(t, dek, *f.totpRow, accountID), time.Now().Unix(), 6)

	body := fmt.Sprintf(`{"code":%q}`, code)
	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/totp/verify", body)
	w := httptest.NewRecorder()
	s.handleMeTOTPVerifyHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status: want 204, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestMeTOTPVerify_WrongCode(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/totp/verify", `{"code":"000000"}`)
	w := httptest.NewRecorder()
	s.handleMeTOTPVerifyHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w.Body.Bytes())
	if body["code"] != "bad_credentials" {
		t.Errorf("code: want bad_credentials, got %v", body["code"])
	}
}

func TestMeTOTPVerify_EmptyCode(t *testing.T) {
	s, f, _ := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	// No TOTP row → no sudo required.
	_, sess := issueSudoTestSession(t, s, accountID)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/totp/verify", `{"code":""}`)
	w := httptest.NewRecorder()
	s.handleMeTOTPVerifyHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d", w.Code)
	}
}

func TestMeTOTPVerify_FactorLocked(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	// Pin the throttle row into a locked state.
	f.throttle["42:totp"] = db.AuthThrottle{
		AccountID:      accountID,
		Factor:         "totp",
		FailedAttempts: 99,
		LockedUntil:    pgtype.Timestamptz{Time: time.Now().Add(2 * time.Minute), Valid: true},
	}

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/totp/verify", `{"code":"000000"}`)
	w := httptest.NewRecorder()
	s.handleMeTOTPVerifyHTTP(w, r)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status: want 429, got %d (body=%s)", w.Code, w.Body.String())
	}
	if w.Header().Get("Retry-After") == "" {
		t.Errorf("Retry-After header missing on factor_locked response")
	}
	body := decodeJSON(t, w.Body.Bytes())
	if body["code"] != "factor_locked" {
		t.Errorf("code: want factor_locked, got %v", body["code"])
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
