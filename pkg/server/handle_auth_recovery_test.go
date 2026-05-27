// Package server — handle_auth_recovery_test.go
//
// HTTP-layer integration tests for the recovery ceremony (2026-05-28
// recovery_code hardening). Verifies the contract documented in
// docs/superpowers/specs/2026-05-27-recovery-ceremony-design.md:
//   - /auth/recovery/totp/begin loads (not Pops) the recovery_session_token
//   - /auth/recovery/totp/verify atomically Pops it (single-use under races)
//   - failure modes collapse to 401 recovery_session_invalid
//   - disabled-account re-check fires post-token-load on both endpoints
//   - new TOTP confirmation wipes preserved recovery codes and mints fresh batch
//
// Reuses the newTestServer / fakeAuthQueries scaffolding from
// handle_auth_password_test.go.

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"prohibitorum/pkg/db"
	sessstore "prohibitorum/pkg/session"
)

// stashRecoverySession seeds a recovery_session payload directly in the KV.
// Mirrors stashPartialSession's pattern — bypasses the
// /auth/recovery-code/verify step so we can exercise the ceremony endpoints
// in isolation.
func stashRecoverySession(t *testing.T, s *Server, token string, accountID int32) {
	t.Helper()
	payload, _ := json.Marshal(recoverySession{
		AccountID: accountID,
		IssuedAt:  time.Now().UTC(),
	})
	if err := s.kvStore.SetEx(context.Background(), recoverySessionKey(token), string(payload), recoverySessionTTL); err != nil {
		t.Fatalf("stash recovery_session: %v", err)
	}
}

// --- /auth/recovery/totp/begin --------------------------------------------

func TestRecoveryTOTPBegin_HappyPath(t *testing.T) {
	s, f, dek := newTestServer(t)
	const accountID int32 = 42
	_ = seedConfirmedTOTP(t, s, f, dek, accountID)

	tok := mustToken(t)
	stashRecoverySession(t, s, tok, accountID)
	priorRecoveryCount := len(f.recoveryRows)

	body := fmt.Sprintf(`{"recovery_session_token":%q}`, tok)
	req := httptest.NewRequest(http.MethodPost,
		"/api/prohibitorum/auth/recovery/totp/begin",
		strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:5555"
	w := httptest.NewRecorder()

	s.handleAuthRecoveryTOTPBeginHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["secret_base32"] == "" {
		t.Error("missing secret_base32")
	}
	if !strings.HasPrefix(resp["otpauth_uri"], "otpauth://totp/") {
		t.Errorf("otpauth_uri: want otpauth:// prefix, got %q", resp["otpauth_uri"])
	}
	// Recovery codes preserved (the design invariant: /begin must not wipe).
	if len(f.recoveryRows) != priorRecoveryCount {
		t.Errorf("recovery codes after /begin: want %d preserved, got %d",
			priorRecoveryCount, len(f.recoveryRows))
	}
	// TOTP row is now unconfirmed (fresh enrollment).
	if f.totpRow == nil {
		t.Fatal("totpRow nil after /begin")
	}
	if f.totpRow.ConfirmedAt.Valid {
		t.Error("totpRow should be unconfirmed after /auth/recovery/totp/begin")
	}
	// Token is still valid (Get, not Pop) — the user may retry /begin if
	// they failed to scan the QR.
	if _, err := s.kvStore.Get(context.Background(), recoverySessionKey(tok)); err != nil {
		t.Errorf("recovery_session_token must remain after /begin: %v", err)
	}
}

func TestRecoveryTOTPBegin_InvalidTokenRejected(t *testing.T) {
	s, _, _ := newTestServer(t)
	body := `{"recovery_session_token":"bogus-token"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/prohibitorum/auth/recovery/totp/begin",
		strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:5555"
	w := httptest.NewRecorder()
	s.handleAuthRecoveryTOTPBeginHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
	body2 := decodeJSON(t, w.Body.Bytes())
	if body2["code"] != "recovery_session_invalid" {
		t.Errorf("code: want recovery_session_invalid, got %v", body2["code"])
	}
}

func TestRecoveryTOTPBegin_MissingBodyRejected(t *testing.T) {
	s, _, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost,
		"/api/prohibitorum/auth/recovery/totp/begin",
		strings.NewReader(`{}`))
	req.RemoteAddr = "127.0.0.1:5555"
	w := httptest.NewRecorder()
	s.handleAuthRecoveryTOTPBeginHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d", w.Code)
	}
}

func TestRecoveryTOTPBegin_AccountDisabledMidFlow(t *testing.T) {
	s, f, dek := newTestServer(t)
	const accountID int32 = 42
	_ = seedConfirmedTOTP(t, s, f, dek, accountID)
	tok := mustToken(t)
	stashRecoverySession(t, s, tok, accountID)

	// Admin disables the account between recovery-code redeem and /begin.
	f.accounts[accountID] = db.Account{ID: accountID, Username: "alice", Disabled: true}

	body := fmt.Sprintf(`{"recovery_session_token":%q}`, tok)
	req := httptest.NewRequest(http.MethodPost,
		"/api/prohibitorum/auth/recovery/totp/begin",
		strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:5555"
	w := httptest.NewRecorder()
	s.handleAuthRecoveryTOTPBeginHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
	bj := decodeJSON(t, w.Body.Bytes())
	if bj["code"] != "recovery_session_invalid" {
		t.Errorf("code: want recovery_session_invalid, got %v", bj["code"])
	}
}

// --- /auth/recovery/totp/verify -------------------------------------------

// runRecoveryCeremony drives /begin + /verify against the in-memory test
// server and returns the verify response writer. Splitting this out keeps
// the individual test bodies focused on the assertion, not the setup
// pipeline.
func runRecoveryCeremony(t *testing.T, s *Server, f *fakeAuthQueries, dek []byte, tok string, accountID int32, codeShift time.Duration) *httptest.ResponseRecorder {
	t.Helper()

	// /begin
	beginBody := fmt.Sprintf(`{"recovery_session_token":%q}`, tok)
	beginReq := httptest.NewRequest(http.MethodPost,
		"/api/prohibitorum/auth/recovery/totp/begin",
		strings.NewReader(beginBody))
	beginReq.RemoteAddr = "127.0.0.1:5555"
	beginW := httptest.NewRecorder()
	s.handleAuthRecoveryTOTPBeginHTTP(beginW, beginReq)
	if beginW.Code != http.StatusOK {
		t.Fatalf("ceremony /begin status: want 200, got %d (body=%s)", beginW.Code, beginW.Body.String())
	}

	// /verify with a code computed against the new (post-/begin) secret.
	row := *f.totpRow
	at := time.Now().Add(codeShift)
	code := totpCodeFor(t, dek, row, accountID, at, 0)
	verifyBody := fmt.Sprintf(`{"recovery_session_token":%q,"code":%q}`, tok, code)
	verifyReq := httptest.NewRequest(http.MethodPost,
		"/api/prohibitorum/auth/recovery/totp/verify",
		strings.NewReader(verifyBody))
	verifyReq.RemoteAddr = "127.0.0.1:5555"
	verifyW := httptest.NewRecorder()
	s.handleAuthRecoveryTOTPVerifyHTTP(verifyW, verifyReq)
	return verifyW
}

func TestRecoveryTOTPVerify_HappyPath(t *testing.T) {
	s, f, dek := newTestServer(t)
	const accountID int32 = 42
	_ = seedConfirmedTOTP(t, s, f, dek, accountID)
	// Simulate the redeem step: burn one code, leaving 9.
	f.recoveryRows = f.recoveryRows[:9]

	tok := mustToken(t)
	stashRecoverySession(t, s, tok, accountID)

	// codeShift = 31s puts us past whatever step the seed Verify consumed
	// and past the freshly-inserted (last_step=0) row's range.
	w := runRecoveryCeremony(t, s, f, dek, tok, accountID, 31*time.Second)

	if w.Code != http.StatusOK {
		t.Fatalf("/verify status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	// 10 new recovery codes returned.
	var resp struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.RecoveryCodes) != 10 {
		t.Errorf("recovery_codes: want 10, got %d", len(resp.RecoveryCodes))
	}
	// Old 9 wiped + new 10 inserted → exactly 10 rows now.
	if len(f.recoveryRows) != 10 {
		t.Errorf("recoveryRows after /verify: want 10, got %d", len(f.recoveryRows))
	}
	if !f.totpRow.ConfirmedAt.Valid {
		t.Error("totpRow should be confirmed after /verify")
	}
	// Session cookie set (Set-Cookie header). Use the session package's
	// canonical cookie name so this stays in sync if it ever changes.
	cookieFound := false
	for _, c := range w.Result().Cookies() {
		if c.Name == sessstore.SessionCookieName {
			cookieFound = true
			break
		}
	}
	if !cookieFound {
		t.Errorf("session cookie not set on /verify success; cookies=%v", w.Result().Cookies())
	}
	// Session row recorded with the right amr.
	if len(f.sessions) != 1 {
		t.Fatalf("sessions: want 1, got %d", len(f.sessions))
	}
	wantAmr := []string{"pwd", "otp", "mfa"}
	if !equalAmr(f.sessions[0].Amr, wantAmr) {
		t.Errorf("amr: want %v, got %v", wantAmr, f.sessions[0].Amr)
	}
	// Recovery_session_token consumed.
	if _, err := s.kvStore.Get(context.Background(), recoverySessionKey(tok)); err == nil {
		t.Error("recovery_session_token should be consumed after /verify success")
	}
}

func TestRecoveryTOTPVerify_InvalidTokenRejected(t *testing.T) {
	s, _, _ := newTestServer(t)
	body := `{"recovery_session_token":"bogus","code":"123456"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/prohibitorum/auth/recovery/totp/verify",
		strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:5555"
	w := httptest.NewRecorder()
	s.handleAuthRecoveryTOTPVerifyHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d", w.Code)
	}
	bj := decodeJSON(t, w.Body.Bytes())
	if bj["code"] != "recovery_session_invalid" {
		t.Errorf("code: want recovery_session_invalid, got %v", bj["code"])
	}
}

func TestRecoveryTOTPVerify_WrongCodeConsumesToken(t *testing.T) {
	s, f, dek := newTestServer(t)
	const accountID int32 = 42
	_ = seedConfirmedTOTP(t, s, f, dek, accountID)
	f.recoveryRows = f.recoveryRows[:9]

	tok := mustToken(t)
	stashRecoverySession(t, s, tok, accountID)

	// Run /begin first (so a fresh unconfirmed row is in place) then submit
	// /verify with an obviously-wrong code.
	beginBody := fmt.Sprintf(`{"recovery_session_token":%q}`, tok)
	beginReq := httptest.NewRequest(http.MethodPost,
		"/api/prohibitorum/auth/recovery/totp/begin",
		strings.NewReader(beginBody))
	beginReq.RemoteAddr = "127.0.0.1:5555"
	beginW := httptest.NewRecorder()
	s.handleAuthRecoveryTOTPBeginHTTP(beginW, beginReq)
	if beginW.Code != http.StatusOK {
		t.Fatalf("/begin: %d %s", beginW.Code, beginW.Body.String())
	}

	verifyBody := fmt.Sprintf(`{"recovery_session_token":%q,"code":"000000"}`, tok)
	verifyReq := httptest.NewRequest(http.MethodPost,
		"/api/prohibitorum/auth/recovery/totp/verify",
		strings.NewReader(verifyBody))
	verifyReq.RemoteAddr = "127.0.0.1:5555"
	verifyW := httptest.NewRecorder()
	s.handleAuthRecoveryTOTPVerifyHTTP(verifyW, verifyReq)

	if verifyW.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d", verifyW.Code)
	}
	// Token was Pop'd at the top of /verify — even a wrong code burns it.
	if _, err := s.kvStore.Get(context.Background(), recoverySessionKey(tok)); err == nil {
		t.Error("recovery_session_token should be consumed even on /verify failure (Pop-then-check)")
	}
	// Recovery codes still 9 (NOT wiped — the wipe only happens inside the
	// successful first-confirm tx).
	if len(f.recoveryRows) != 9 {
		t.Errorf("recovery codes after failed /verify: want 9 preserved, got %d", len(f.recoveryRows))
	}
	// No session issued.
	if len(f.sessions) != 0 {
		t.Errorf("sessions: want 0 on /verify failure, got %d", len(f.sessions))
	}
}

func TestRecoveryTOTPVerify_AccountDisabledMidFlow(t *testing.T) {
	s, f, dek := newTestServer(t)
	const accountID int32 = 42
	_ = seedConfirmedTOTP(t, s, f, dek, accountID)
	f.recoveryRows = f.recoveryRows[:9]

	tok := mustToken(t)
	stashRecoverySession(t, s, tok, accountID)

	// /begin succeeds while the account is still enabled (re-enrollment
	// secret is now in place).
	beginBody := fmt.Sprintf(`{"recovery_session_token":%q}`, tok)
	beginReq := httptest.NewRequest(http.MethodPost,
		"/api/prohibitorum/auth/recovery/totp/begin",
		strings.NewReader(beginBody))
	beginReq.RemoteAddr = "127.0.0.1:5555"
	beginW := httptest.NewRecorder()
	s.handleAuthRecoveryTOTPBeginHTTP(beginW, beginReq)
	if beginW.Code != http.StatusOK {
		t.Fatalf("/begin: %d %s", beginW.Code, beginW.Body.String())
	}

	// Now disable the account between /begin and /verify.
	f.accounts[accountID] = db.Account{ID: accountID, Username: "alice", Disabled: true}

	// /verify must collapse to recovery_session_invalid before consuming the
	// TOTP. Note: the recovery_session_token IS already Pop'd before the
	// disabled re-check; that's fine — the user can't proceed anyway, and
	// holding a one-shot token longer doesn't buy anything.
	row := *f.totpRow
	at := time.Now().Add(31 * time.Second)
	code := totpCodeFor(t, dek, row, accountID, at, 0)
	verifyBody := fmt.Sprintf(`{"recovery_session_token":%q,"code":%q}`, tok, code)
	verifyReq := httptest.NewRequest(http.MethodPost,
		"/api/prohibitorum/auth/recovery/totp/verify",
		strings.NewReader(verifyBody))
	verifyReq.RemoteAddr = "127.0.0.1:5555"
	verifyW := httptest.NewRecorder()
	s.handleAuthRecoveryTOTPVerifyHTTP(verifyW, verifyReq)

	if verifyW.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d (body=%s)", verifyW.Code, verifyW.Body.String())
	}
	bj := decodeJSON(t, verifyW.Body.Bytes())
	if bj["code"] != "recovery_session_invalid" {
		t.Errorf("code: want recovery_session_invalid, got %v", bj["code"])
	}
	if len(f.sessions) != 0 {
		t.Errorf("sessions: want 0 (account disabled), got %d", len(f.sessions))
	}
}

// TestRecoveryTOTPVerify_ParallelAtomic exercises the atomic Pop contract:
// two parallel /verify calls with the same token, exactly one wins.
//
// The losing call sees ErrKeyNotFound at the Pop and short-circuits to
// recovery_session_invalid. The winner proceeds; whether the winner's TOTP
// code is accepted depends on race timing (both share the same code), but
// we only need to assert at-most-one consumes the token.
func TestRecoveryTOTPVerify_ParallelAtomic(t *testing.T) {
	s, f, dek := newTestServer(t)
	const accountID int32 = 42
	_ = seedConfirmedTOTP(t, s, f, dek, accountID)
	f.recoveryRows = f.recoveryRows[:9]

	tok := mustToken(t)
	stashRecoverySession(t, s, tok, accountID)

	// /begin so the new unconfirmed row is in place.
	beginReq := httptest.NewRequest(http.MethodPost,
		"/api/prohibitorum/auth/recovery/totp/begin",
		strings.NewReader(fmt.Sprintf(`{"recovery_session_token":%q}`, tok)))
	beginReq.RemoteAddr = "127.0.0.1:5555"
	beginW := httptest.NewRecorder()
	s.handleAuthRecoveryTOTPBeginHTTP(beginW, beginReq)
	if beginW.Code != http.StatusOK {
		t.Fatalf("/begin: %d", beginW.Code)
	}

	row := *f.totpRow
	at := time.Now().Add(31 * time.Second)
	code := totpCodeFor(t, dek, row, accountID, at, 0)
	verifyBody := fmt.Sprintf(`{"recovery_session_token":%q,"code":%q}`, tok, code)

	var (
		wg            sync.WaitGroup
		recoveryInvalid int32
	)
	const parallel = 8
	wg.Add(parallel)
	for i := 0; i < parallel; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost,
				"/api/prohibitorum/auth/recovery/totp/verify",
				strings.NewReader(verifyBody))
			req.RemoteAddr = "127.0.0.1:5555"
			w := httptest.NewRecorder()
			s.handleAuthRecoveryTOTPVerifyHTTP(w, req)
			if w.Code == http.StatusUnauthorized {
				var bj map[string]any
				_ = json.Unmarshal(w.Body.Bytes(), &bj)
				if bj["code"] == "recovery_session_invalid" {
					atomic.AddInt32(&recoveryInvalid, 1)
				}
			}
		}()
	}
	wg.Wait()

	// At most one /verify can have consumed the token successfully; the rest
	// must have observed recovery_session_invalid. Equivalently: at least
	// parallel-1 saw the invalid sentinel.
	if int(recoveryInvalid) < parallel-1 {
		t.Errorf("expected at least %d /verify calls to see recovery_session_invalid (only one winner allowed), got %d",
			parallel-1, recoveryInvalid)
	}
	// Token must be gone after.
	if _, err := s.kvStore.Get(context.Background(), recoverySessionKey(tok)); err == nil {
		t.Error("recovery_session_token should be consumed after parallel /verify")
	}
}
