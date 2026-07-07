// Package server — handle_auth_webauthn_test.go
//
// Unit tests for the WebAuthn login handler's audit emissions (Task 4).
// Only the ceremony-failure branches that return before calling
// s.webauthn.FinishPasskeyLogin are covered here; the FinishPasskeyLogin,
// no_account, account_disabled, clone_warning, and success paths require a
// real WebAuthn ceremony (CBOR-encoded authenticator data + signatures) and
// are covered by the final smoke test (Task 14).

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"prohibitorum/pkg/audit"
)

// newWebAuthnTestServer builds the minimum Server needed to exercise the
// ceremony-failure branches of handleLoginCompleteHTTP. The webauthn field
// is left nil because those branches return before touching it.
func newWebAuthnTestServer(t *testing.T) (*Server, *fakeAuthQueries) {
	t.Helper()
	s, f, _ := newTestServer(t)
	return s, f
}

// postWebAuthnComplete fires POST /auth/webauthn/login/complete with the given
// cookie and body, returning the response recorder.
func postWebAuthnComplete(t *testing.T, s *Server, cerCookieValue string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	u := &url.URL{Path: "/auth/webauthn/login/complete"}
	req := httptest.NewRequest(http.MethodPost, u.String(), nil)
	if cerCookieValue != "" {
		req.AddCookie(&http.Cookie{Name: "prohibitorum_ceremony", Value: cerCookieValue})
	}
	req.Body = http.NoBody
	_ = body // ceremony body not consumed by early-exit branches
	s.handleLoginCompleteHTTP(rec, req)
	return rec
}

// TestWebAuthnLoginAudit_CeremonyMissing verifies that a request with a
// ceremony cookie whose key is absent from the KV store emits a
// webauthn|fail record with reason=ceremony_missing.
func TestWebAuthnLoginAudit_CeremonyMissing(t *testing.T) {
	t.Parallel()
	s, f := newWebAuthnTestServer(t)

	// Use a ceremony cookie value that was never stashed → KV Pop returns ErrKeyNotFound.
	rec := postWebAuthnComplete(t, s, "no-such-key", nil)

	// ceremony_missing → ErrCeremonyExpired() → 400 Bad Request
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rec.Code)
	}

	if len(f.events) != 1 {
		t.Fatalf("audit events: got %d, want 1", len(f.events))
	}
	ev := f.events[0]
	if ev.Factor != string(audit.FactorWebAuthn) {
		t.Errorf("Factor: got %q, want %q", ev.Factor, audit.FactorWebAuthn)
	}
	if ev.Event != audit.EventFail {
		t.Errorf("Event: got %q, want %q", ev.Event, audit.EventFail)
	}
	if ev.AccountID != nil {
		t.Errorf("AccountID: got %v, want nil", ev.AccountID)
	}
	var detail map[string]any
	if err := json.Unmarshal(ev.Detail, &detail); err != nil {
		t.Fatalf("detail unmarshal: %v", err)
	}
	if reason, _ := detail["reason"].(string); reason != "ceremony_missing" {
		t.Errorf("detail.reason: got %q, want %q", reason, "ceremony_missing")
	}
}

// TestWebAuthnLoginAudit_CeremonyCorrupt verifies that a KV entry containing
// invalid JSON (corrupt ceremony state) emits a webauthn|fail record with
// reason=ceremony_corrupt.
func TestWebAuthnLoginAudit_CeremonyCorrupt(t *testing.T) {
	t.Parallel()
	s, f := newWebAuthnTestServer(t)

	// Stash a corrupt (non-JSON) ceremony state so Pop succeeds but Unmarshal fails.
	stashCtx := httptest.NewRequest(http.MethodPost, "/", nil).Context()
	if err := s.kvStore.SetEx(
		stashCtx,
		"webauthn_ceremony:login:corrupt-key",
		"not-valid-json",
		ceremonyTTL,
	); err != nil {
		t.Fatalf("kvStore.SetEx: %v", err)
	}

	rec := postWebAuthnComplete(t, s, "corrupt-key", nil)

	// ceremony_corrupt → ErrCeremonyState() → 500 Internal Server Error
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", rec.Code)
	}

	if len(f.events) != 1 {
		t.Fatalf("audit events: got %d, want 1", len(f.events))
	}
	ev := f.events[0]
	if ev.Factor != string(audit.FactorWebAuthn) {
		t.Errorf("Factor: got %q, want %q", ev.Factor, audit.FactorWebAuthn)
	}
	if ev.Event != audit.EventFail {
		t.Errorf("Event: got %q, want %q", ev.Event, audit.EventFail)
	}
	if ev.AccountID != nil {
		t.Errorf("AccountID: got %v, want nil", ev.AccountID)
	}
	var detail map[string]any
	if err := json.Unmarshal(ev.Detail, &detail); err != nil {
		t.Fatalf("detail unmarshal: %v", err)
	}
	if reason, _ := detail["reason"].(string); reason != "ceremony_corrupt" {
		t.Errorf("detail.reason: got %q, want %q", reason, "ceremony_corrupt")
	}
}
