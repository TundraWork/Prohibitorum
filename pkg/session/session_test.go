package session

import (
	"context"
	"testing"
	"time"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/kv"
)

func newTestStore(t *testing.T, ttl time.Duration) *SessionStore {
	t.Helper()
	return NewSessionStore(kv.NewMemoryStore(), ttl)
}

func TestSession_IssueAndLoad(t *testing.T) {
	s := newTestStore(t, time.Hour)
	ctx := context.Background()

	token, data, err := s.Issue(ctx, 42, "127.0.0.1", "")
	if err != nil {
		t.Fatal(err)
	}
	if data.AccountID != 42 {
		t.Errorf("AccountID = %d, want 42", data.AccountID)
	}
	if len(token) != 43 {
		t.Errorf("token length = %d, want 43", len(token))
	}

	loaded, refreshed, err := s.Load(ctx, 42, token, "127.0.0.1", "")
	if err != nil {
		t.Fatal(err)
	}
	if refreshed {
		t.Error("fresh session should not refresh")
	}
	if loaded.AccountID != 42 {
		t.Errorf("loaded AccountID = %d, want 42", loaded.AccountID)
	}
}

func TestSession_LoadMissingReturnsNoSession(t *testing.T) {
	s := newTestStore(t, time.Hour)
	_, _, err := s.Load(context.Background(), 42, "bogus_token", "127.0.0.1", "")
	if err == nil {
		t.Fatal("missing session should error")
	}
	ae := authn.AsAuthError(err)
	if ae == nil || ae.Code != "no_session" {
		t.Errorf("want no_session error, got %v", err)
	}
}

func TestSession_LoadWrongAccountReturnsNoSession(t *testing.T) {
	s := newTestStore(t, time.Hour)
	ctx := context.Background()
	token, _, _ := s.Issue(ctx, 42, "", "")
	_, _, err := s.Load(ctx, 99, token, "", "") // wrong account id
	if err == nil {
		t.Fatal("loading with wrong account id should fail")
	}
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "no_session" {
		t.Errorf("want no_session, got %v", err)
	}
}

func TestSession_Revoke(t *testing.T) {
	s := newTestStore(t, time.Hour)
	ctx := context.Background()
	token, _, _ := s.Issue(ctx, 42, "", "")
	if err := s.Revoke(ctx, 42, token); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.Load(ctx, 42, token, "", "")
	if err == nil {
		t.Error("revoked session should not load")
	}
}

func TestSession_RevokeAllForAccount(t *testing.T) {
	s := newTestStore(t, time.Hour)
	ctx := context.Background()
	// 3 sessions for account 42, 1 for account 99
	for i := 0; i < 3; i++ {
		if _, _, err := s.Issue(ctx, 42, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.Issue(ctx, 99, "", ""); err != nil {
		t.Fatal(err)
	}

	deleted, err := s.RevokeAllForAccount(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 3 {
		t.Errorf("deleted = %d, want 3", deleted)
	}

	// Verify account 99 still has its session by scanning.
	result, err := s.kv.ScanEntries(ctx, "session:99:*", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 {
		t.Errorf("account 99 should still have 1 session, got %d", len(result.Entries))
	}
}

func TestSession_RefreshTriggersInLastQuarter(t *testing.T) {
	// 100ms TTL means refresh threshold = 25ms. Sleep 80ms => 20ms remaining => refresh fires.
	s := NewSessionStore(kv.NewMemoryStore(), 100*time.Millisecond)
	ctx := context.Background()
	token, _, _ := s.Issue(ctx, 42, "", "")
	time.Sleep(80 * time.Millisecond)
	_, refreshed, err := s.Load(ctx, 42, token, "192.168.1.1", "")
	if err != nil {
		t.Fatal(err)
	}
	if !refreshed {
		t.Error("close-to-expiry load should refresh")
	}
}

func TestSession_NoRefreshEarlyInLifetime(t *testing.T) {
	s := NewSessionStore(kv.NewMemoryStore(), 1*time.Second)
	ctx := context.Background()
	token, _, _ := s.Issue(ctx, 42, "", "")
	// Load immediately — far from expiry, should not refresh.
	_, refreshed, err := s.Load(ctx, 42, token, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if refreshed {
		t.Error("fresh session should not refresh")
	}
}

func TestSession_ExpiredEntryReturnsNoSession(t *testing.T) {
	// Very short TTL — write, sleep past expiry, expect ErrNoSession.
	s := NewSessionStore(kv.NewMemoryStore(), 20*time.Millisecond)
	ctx := context.Background()
	token, _, _ := s.Issue(ctx, 42, "", "")
	time.Sleep(40 * time.Millisecond)
	_, _, err := s.Load(ctx, 42, token, "", "")
	if err == nil {
		t.Fatal("expired session should error")
	}
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "no_session" {
		t.Errorf("want no_session, got %v", err)
	}
}

func TestCookieValue_Roundtrip(t *testing.T) {
	cases := []struct {
		id    int32
		token string
	}{
		{1, "abc"},
		{99999, "xY_Z-1234567890"},
	}
	for _, c := range cases {
		v := CookieValue(c.id, c.token)
		id, tok, ok := ParseCookieValue(v)
		if !ok || id != c.id || tok != c.token {
			t.Errorf("roundtrip %+v: ParseCookieValue(%q) = (%d, %q, %v)", c, v, id, tok, ok)
		}
	}
}

func TestParseCookieValue_Malformed(t *testing.T) {
	bad := []string{
		"",
		".",
		".token",
		"42.",
		"notanint.token",
		"-1.token",
		"0.token",
	}
	for _, v := range bad {
		if _, _, ok := ParseCookieValue(v); ok {
			t.Errorf("%q should not parse", v)
		}
	}
}
