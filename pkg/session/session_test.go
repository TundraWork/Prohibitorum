package session

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

// noopSessionQueries lets tests exercise the KV path without a live Postgres.
// All methods succeed without persisting; the PG-session row's only consumer
// is v0.4+ OIDC, which has no test coverage here yet.
type noopSessionQueries struct{}

func (noopSessionQueries) InsertSession(context.Context, db.InsertSessionParams) (db.Session, error) {
	return db.Session{}, nil
}
func (noopSessionQueries) RevokeSession(context.Context, string) error             { return nil }
func (noopSessionQueries) RevokeAllSessionsByAccount(context.Context, int32) error { return nil }

// failingSessionQueries simulates a PG-side failure on InsertSession so the
// SessionStore.Issue rollback path (delete KV row, return error) can be
// exercised by a unit test.
type failingSessionQueries struct{ noopSessionQueries }

func (failingSessionQueries) InsertSession(context.Context, db.InsertSessionParams) (db.Session, error) {
	return db.Session{}, errors.New("simulated PG failure")
}

func newTestStore(t *testing.T, ttl time.Duration) *SessionStore {
	t.Helper()
	return NewSessionStore(kv.NewMemoryStore(), noopSessionQueries{}, ttl)
}

func TestSession_IssueAndLoad(t *testing.T) {
	s := newTestStore(t, time.Hour)
	ctx := context.Background()

	token, data, err := s.Issue(ctx, 42, "127.0.0.1", "", []string{"hwk"}, nil)
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
	token, _, _ := s.Issue(ctx, 42, "", "", []string{"hwk"}, nil)
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
	token, _, _ := s.Issue(ctx, 42, "", "", []string{"hwk"}, nil)
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
		if _, _, err := s.Issue(ctx, 42, "", "", []string{"hwk"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.Issue(ctx, 99, "", "", []string{"hwk"}, nil); err != nil {
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
	s := NewSessionStore(kv.NewMemoryStore(), noopSessionQueries{}, 100*time.Millisecond)
	ctx := context.Background()
	token, _, _ := s.Issue(ctx, 42, "", "", []string{"hwk"}, nil)
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
	s := NewSessionStore(kv.NewMemoryStore(), noopSessionQueries{}, 1*time.Second)
	ctx := context.Background()
	token, _, _ := s.Issue(ctx, 42, "", "", []string{"hwk"}, nil)
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
	s := NewSessionStore(kv.NewMemoryStore(), noopSessionQueries{}, 20*time.Millisecond)
	ctx := context.Background()
	token, _, _ := s.Issue(ctx, 42, "", "", []string{"hwk"}, nil)
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

func TestSession_IssueRollsBackKVWhenPGInsertFails(t *testing.T) {
	mem := kv.NewMemoryStore()
	s := NewSessionStore(mem, failingSessionQueries{}, time.Hour)
	ctx := context.Background()

	_, _, err := s.Issue(ctx, 42, "127.0.0.1", "", []string{"hwk"}, nil)
	if err == nil {
		t.Fatal("Issue should return error when InsertSession fails")
	}

	// Inspect KV directly: no session:42:* entry should remain.
	result, scanErr := mem.ScanEntries(ctx, fmt.Sprintf("session:%d:*", 42), 0, 100)
	if scanErr != nil {
		t.Fatalf("scan: %v", scanErr)
	}
	if len(result.Entries) != 0 {
		t.Errorf("expected zero KV entries after rollback, got %d: %+v",
			len(result.Entries), result.Entries)
	}
}

// recordingSessionQueries captures the params passed to InsertSession so tests
// can assert which fields (notably UpstreamIdpID) were set.
type recordingSessionQueries struct {
	noopSessionQueries
	last db.InsertSessionParams
}

func (r *recordingSessionQueries) InsertSession(_ context.Context, arg db.InsertSessionParams) (db.Session, error) {
	r.last = arg
	return db.Session{}, nil
}

// TestSession_IssueStampsUpstreamIDPID guards H1-sch: federation callers must
// be able to attach the upstream_idp_id to the session row, while local
// (non-federation) callers leave the column NULL by passing nil.
func TestSession_IssueStampsUpstreamIDPID(t *testing.T) {
	t.Run("federated", func(t *testing.T) {
		rec := &recordingSessionQueries{}
		s := NewSessionStore(kv.NewMemoryStore(), rec, time.Hour)
		var idpID int64 = 42
		if _, _, err := s.Issue(context.Background(), 1, "", "", []string{"federated"}, &idpID); err != nil {
			t.Fatalf("Issue: %v", err)
		}
		if rec.last.UpstreamIdpID == nil || *rec.last.UpstreamIdpID != 42 {
			t.Errorf("UpstreamIdpID: want *42, got %v", rec.last.UpstreamIdpID)
		}
	})

	t.Run("local-pwd-totp", func(t *testing.T) {
		rec := &recordingSessionQueries{}
		s := NewSessionStore(kv.NewMemoryStore(), rec, time.Hour)
		if _, _, err := s.Issue(context.Background(), 1, "", "", []string{"pwd", "otp", "mfa"}, nil); err != nil {
			t.Fatalf("Issue: %v", err)
		}
		if rec.last.UpstreamIdpID != nil {
			t.Errorf("UpstreamIdpID: want nil for local login, got *%d", *rec.last.UpstreamIdpID)
		}
	})
}

func TestSession_IssueRejectsEmptyAMR(t *testing.T) {
	s := newTestStore(t, time.Hour)
	_, _, err := s.Issue(context.Background(), 42, "", "", nil, nil)
	if err == nil {
		t.Fatal("Issue should reject empty amr")
	}
	if err == nil || err.Error() == "" {
		t.Fatalf("expected non-empty error, got %v", err)
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
