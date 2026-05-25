// Package server — handle_me_revoke_pwd_totp_test.go
//
// Unit tests for POST /me/auth/revoke-password-totp. Verifies the sudo gate
// and the idempotent delete-all-three behaviour. The endpoint delegates to
// authn.DisableNonWebAuthnFallbacks, which has its own broader coverage in
// pkg/authn/flow_test.go — these tests focus on the HTTP-level wiring (sudo
// gate, status codes, that the queries surface is reached).
//
// Tests inject a fake authn.FlowQueries via s.revokeFlowOverride. The
// handler reads through revokeFlowQ() (defined in
// handle_me_revoke_pwd_totp.go).

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"prohibitorum/pkg/authn"
)

func TestMeRevokePwdTOTP_RequiresSudo(t *testing.T) {
	s, _, _ := newSudoTestServer(t)
	const accountID int32 = 42
	_, sess := issueSudoTestSession(t, s, accountID)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/auth/revoke-password-totp", "")
	w := httptest.NewRecorder()
	s.handleMeRevokePwdTOTPHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w.Body.Bytes())
	if body["code"] != "sudo_required" {
		t.Errorf("code: want sudo_required, got %v", body["code"])
	}
}

func TestMeRevokePwdTOTP_Success(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	s.revokeFlowOverride = f
	const accountID int32 = 42
	// Seed: password + confirmed TOTP + 10 recovery codes.
	seedPassword(t, s, accountID, "correct-horse-battery")
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)

	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/auth/revoke-password-totp", "")
	w := httptest.NewRecorder()
	s.handleMeRevokePwdTOTPHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status: want 204, got %d (body=%s)", w.Code, w.Body.String())
	}
	if f.passwordRow != nil {
		t.Errorf("password row should be deleted, got %+v", f.passwordRow)
	}
	if f.totpRow != nil {
		t.Errorf("totp row should be deleted, got %+v", f.totpRow)
	}
	remaining, _ := f.ListRecoveryCodesByAccount(context.Background(), accountID)
	if len(remaining) != 0 {
		t.Errorf("recovery codes should be deleted, %d remain", len(remaining))
	}

	// Audit events for password / totp / recovery_code revoke must each
	// appear.
	wantFactors := map[string]bool{"password": false, "totp": false, "recovery_code": false}
	for _, ev := range f.events {
		if ev.Event == "revoke" {
			if _, ok := wantFactors[ev.Factor]; ok {
				wantFactors[ev.Factor] = true
			}
		}
	}
	for factor, seen := range wantFactors {
		if !seen {
			t.Errorf("missing revoke audit event for factor=%s; events=%+v", factor, f.events)
		}
	}
}

func TestMeRevokePwdTOTP_Idempotent(t *testing.T) {
	s, f, _ := newSudoTestServer(t)
	s.revokeFlowOverride = f
	const accountID int32 = 42
	// No password, no TOTP, no recovery codes — clean slate.

	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/auth/revoke-password-totp", "")
	w := httptest.NewRecorder()
	s.handleMeRevokePwdTOTPHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("first call: want 204, got %d (body=%s)", w.Code, w.Body.String())
	}

	// Re-grant sudo (the first call consumed it) and call again — same 204.
	grantFreshSudo(t, s, accountID, token)
	// Reload session data so the in-memory sess sees the fresh SudoUntil
	// (the first handler call cleared the in-memory copy via
	// requireFreshSudo's one-shot consume).
	loaded, _, err := s.sessionStore.Load(context.Background(), accountID, token, "127.0.0.1", "ua/test")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	sess.Data = loaded

	r2 := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/auth/revoke-password-totp", "")
	w2 := httptest.NewRecorder()
	s.handleMeRevokePwdTOTPHTTP(w2, r2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("second call: want 204, got %d (body=%s)", w2.Code, w2.Body.String())
	}
}

// Compile-time check: the fake must satisfy the revoke flow surface.
var _ authn.FlowQueries = (*fakeSudoQueries)(nil)
