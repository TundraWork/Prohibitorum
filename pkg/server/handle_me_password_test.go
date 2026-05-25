// Package server — handle_me_password_test.go
//
// Unit tests for POST /me/password/set. Reuses the fakeSudoQueries +
// newSudoTestServer scaffolding from handle_sudo_test.go since the
// password-set endpoint walks the same wiring (passwordStore + sessionStore
// + sudo gate).

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// grantFreshSudo stamps SudoUntil on the live session record so the next
// requireFreshSudo() call passes. Mirrors stampSudoUntil's effect without
// emitting an audit event or running a ceremony.
func grantFreshSudo(t *testing.T, s *Server, accountID int32, token string) {
	t.Helper()
	current, _, err := s.sessionStore.Load(context.Background(), accountID, token, "127.0.0.1", "ua/test")
	if err != nil {
		t.Fatalf("grantFreshSudo: load: %v", err)
	}
	current.SudoUntil = time.Now().Add(5 * time.Minute)
	if err := s.sessionStore.Save(context.Background(), accountID, token, current); err != nil {
		t.Fatalf("grantFreshSudo: save: %v", err)
	}
}

func TestMePasswordSet_RequiresSudo(t *testing.T) {
	s, _, _ := newSudoTestServer(t)
	const accountID int32 = 42
	_, sess := issueSudoTestSession(t, s, accountID)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/password/set", `{"password":"correct-horse-battery"}`)
	w := httptest.NewRecorder()
	s.handleMePasswordSetHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w.Body.Bytes())
	if body["code"] != "sudo_required" {
		t.Errorf("code: want sudo_required, got %v", body["code"])
	}
}

func TestMePasswordSet_TooShort(t *testing.T) {
	s, _, _ := newSudoTestServer(t)
	const accountID int32 = 42
	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/password/set", `{"password":"short7!"}`)
	w := httptest.NewRecorder()
	s.handleMePasswordSetHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w.Body.Bytes())
	if body["code"] != "bad_request" {
		t.Errorf("code: want bad_request, got %v", body["code"])
	}
}

func TestMePasswordSet_TooLong(t *testing.T) {
	s, _, _ := newSudoTestServer(t)
	const accountID int32 = 42
	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	long := strings.Repeat("a", 1025)
	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/password/set",
		`{"password":"`+long+`"}`)
	w := httptest.NewRecorder()
	s.handleMePasswordSetHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestMePasswordSet_BadJSON(t *testing.T) {
	s, _, _ := newSudoTestServer(t)
	const accountID int32 = 42
	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/password/set", `{`)
	w := httptest.NewRecorder()
	s.handleMePasswordSetHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestMePasswordSet_Success(t *testing.T) {
	s, f, _ := newSudoTestServer(t)
	const accountID int32 = 42
	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/password/set",
		`{"password":"correct-horse-battery-staple"}`)
	w := httptest.NewRecorder()
	s.handleMePasswordSetHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status: want 204, got %d (body=%s)", w.Code, w.Body.String())
	}
	if f.passwordRow == nil || f.passwordRow.AccountID != accountID {
		t.Fatalf("password row not written: %+v", f.passwordRow)
	}
	if len(f.passwordRow.Hash) == 0 {
		t.Errorf("password row has empty hash")
	}

	// Second password/set in the same session must require a fresh sudo —
	// the gate is one-shot. This also exercises requireFreshSudo's clear-
	// after-pass behaviour.
	r2 := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/password/set",
		`{"password":"another-strong-passphrase"}`)
	// Re-read the session from KV: requireFreshSudo cleared SudoUntil
	// during the first call, but our sess in-memory copy still holds the
	// stale value. The handler reads from sess.Data, so reflect the cleared
	// state here.
	loaded, _, err := s.sessionStore.Load(context.Background(), accountID, token, "127.0.0.1", "ua/test")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	sess.Data = loaded
	w2 := httptest.NewRecorder()
	s.handleMePasswordSetHTTP(w2, r2)
	if w2.Code != http.StatusUnauthorized {
		t.Errorf("second set without re-sudo: want 401, got %d (body=%s)", w2.Code, w2.Body.String())
	}
}
