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
	"prohibitorum/pkg/db"
)

func TestMeRevokePwdTOTP_RequiresSudo(t *testing.T) {
	s, _, _ := newSudoTestServer(t)
	const accountID int32 = 42
	_, sess := issueSudoTestSession(t, s, accountID)
	// Backdate IssuedAt so the recent-auth window doesn't apply; no SudoUntil
	// set, so the gate must deny with sudo_required.
	sess.Data.IssuedAt = time.Now().Add(-(s.config.Auth.SudoTTL + time.Minute))

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
	// Seed: passkey (so the lockout guard passes) + password + confirmed TOTP + recovery codes.
	f.webauthnRows = append(f.webauthnRows, db.WebauthnCredential{ID: 1, AccountID: accountID})
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
	// Account has a passkey so the lockout guard passes; no password/TOTP/recovery
	// codes — the deletes are no-ops and the call returns 204 (idempotent).
	f.webauthnRows = append(f.webauthnRows, db.WebauthnCredential{ID: 1, AccountID: accountID})

	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/auth/revoke-password-totp", "")
	w := httptest.NewRecorder()
	s.handleMeRevokePwdTOTPHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("first call: want 204, got %d (body=%s)", w.Code, w.Body.String())
	}

	// Second call with the same session — gate is multi-use (time-windowed),
	// so no re-grant is needed. SudoUntil was not cleared by the first call.
	r2 := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/auth/revoke-password-totp", "")
	w2 := httptest.NewRecorder()
	s.handleMeRevokePwdTOTPHTTP(w2, r2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("second call: want 204 (multi-use gate), got %d (body=%s)", w2.Code, w2.Body.String())
	}
}

// TestMeRevokePwdTOTP_409WouldRemoveLastFactor verifies that the handler
// surfaces HTTP 409 / would_remove_last_factor when the lockout guard fires.
// The fake has zero passkeys (webauthnRows empty by default) and
// CountUsableSignInFederation returns 0 — the guard should abort before any
// deletes and the handler must return 409 with code would_remove_last_factor.
func TestMeRevokePwdTOTP_409WouldRemoveLastFactor(t *testing.T) {
	s, f, _ := newSudoTestServer(t)
	s.revokeFlowOverride = f
	const accountID int32 = 42
	// No webauthn rows and no usable federation (both zero by default in fake).

	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/auth/revoke-password-totp", "")
	w := httptest.NewRecorder()
	s.handleMeRevokePwdTOTPHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("status: want 409, got %d (body=%s)", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w.Body.Bytes())
	if body["code"] != "would_remove_last_factor" {
		t.Errorf("code: want would_remove_last_factor, got %v", body["code"])
	}
}

// Compile-time check: the fake must satisfy the revoke flow surface.
var _ authn.FlowQueries = (*fakeSudoQueries)(nil)
