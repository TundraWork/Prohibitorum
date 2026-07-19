package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

// noopSessionQueries lets tests exercise the KV path without a live Postgres.
// All methods succeed without persisting; the PG-session row's only consumer
// is OIDC, which has no test coverage here yet.
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

type blockingRevokeAllQueries struct {
	noopSessionQueries
	entered chan struct{}
	proceed chan struct{}
}

func (q *blockingRevokeAllQueries) RevokeAllSessionsByAccount(ctx context.Context, _ int32) error {
	close(q.entered)
	select {
	case <-q.proceed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func newTestStore(t *testing.T, ttl time.Duration) *SessionStore {
	t.Helper()
	return NewSessionStore(kv.NewMemoryStore(), noopSessionQueries{}, ttl)
}

type pauseAfterSessionReadStore struct {
	kv.Store
	key     string
	read    chan struct{}
	resume  chan struct{}
	readOne sync.Once
}

func (s *pauseAfterSessionReadStore) Get(ctx context.Context, key string) (string, error) {
	raw, err := s.Store.Get(ctx, key)
	if err == nil && key == s.key {
		s.readOne.Do(func() { close(s.read) })
		select {
		case <-s.resume:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return raw, err
}

func TestSession_RevokeAllFencesPausedLoadMutation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*authn.SessionData)
	}{
		{
			name: "sliding refresh",
			mutate: func(data *authn.SessionData) {
				data.ExpiresAt = time.Now().Add(time.Hour)
			},
		},
		{
			name: "schema backfill",
			mutate: func(data *authn.SessionData) {
				data.SessionID = ""
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			base := kv.NewMemoryStore()
			t.Cleanup(func() { _ = base.Close() })
			const ttl = 8 * time.Hour
			issuer := NewSessionStore(base, noopSessionQueries{}, ttl)
			token, _, err := issuer.Issue(ctx, 42, "", "", []string{"hwk"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			key := sessionKey(42, token)
			raw, err := base.Get(ctx, key)
			if err != nil {
				t.Fatal(err)
			}
			var data authn.SessionData
			if err := json.Unmarshal([]byte(raw), &data); err != nil {
				t.Fatal(err)
			}
			test.mutate(&data)
			mutated, err := json.Marshal(&data)
			if err != nil {
				t.Fatal(err)
			}
			if err := base.SetEx(ctx, key, string(mutated), time.Until(data.ExpiresAt)); err != nil {
				t.Fatal(err)
			}

			gated := &pauseAfterSessionReadStore{
				Store:  base,
				key:    key,
				read:   make(chan struct{}),
				resume: make(chan struct{}),
			}
			store := NewSessionStore(gated, noopSessionQueries{}, ttl)
			loadDone := make(chan error, 1)
			go func() {
				_, _, err := store.Load(ctx, 42, token, "", "current-agent")
				loadDone <- err
			}()
			<-gated.read

			if _, err := store.RevokeAllForAccount(ctx, 42); err != nil {
				t.Fatalf("RevokeAllForAccount: %v", err)
			}
			close(gated.resume)
			if err := <-loadDone; err != nil {
				t.Fatalf("paused Load: %v", err)
			}
			if _, err := base.Get(ctx, key); !errors.Is(err, kv.ErrKeyNotFound) {
				t.Fatalf("session key was recreated after revoke-all: %v", err)
			}
			if _, _, err := store.Load(ctx, 42, token, "", ""); authn.AsAuthError(err) == nil {
				t.Fatal("revoked session loaded after paused mutation resumed")
			}
		})
	}
}

type fenceResultStore struct {
	kv.Store
	swapped bool
	err     error
}

func (s *fenceResultStore) FencedCompareAndSwap(context.Context, string, string, string, string, string, time.Duration) (bool, error) {
	return s.swapped, s.err
}

type scanErrorStore struct {
	kv.Store
	err error
}

func (s *scanErrorStore) ScanEntries(context.Context, string, uint64, int64) (kv.ScanEntriesResult, error) {
	return kv.ScanEntriesResult{}, s.err
}

type replaceLeaseBeforeReleaseStore struct {
	kv.Store
	replacement string
}

func (s *replaceLeaseBeforeReleaseStore) FencedCompareAndSwap(ctx context.Context, fenceKey, fenceValue, key, oldValue, newValue string, ttl time.Duration) (bool, error) {
	swapped, err := s.Store.FencedCompareAndSwap(ctx, fenceKey, fenceValue, key, oldValue, newValue, ttl)
	if err != nil || !swapped {
		return swapped, err
	}
	if err := s.Store.Del(ctx, fenceKey); err != nil {
		return false, err
	}
	acquired, err := s.Store.SetNX(ctx, fenceKey, s.replacement, sessionMutationLeaseTTL)
	if err != nil || !acquired {
		return false, err
	}
	return true, nil
}

type loseLeaseOnRenewStore struct {
	kv.Store
	leaseKey    string
	replacement string
}

func (s *loseLeaseOnRenewStore) CompareAndSwap(ctx context.Context, key, oldValue, newValue string, ttl time.Duration) (bool, error) {
	if key != s.leaseKey {
		return s.Store.CompareAndSwap(ctx, key, oldValue, newValue, ttl)
	}
	if err := s.Store.Del(ctx, key); err != nil {
		return false, err
	}
	acquired, err := s.Store.SetNX(ctx, key, s.replacement, ttl)
	if err != nil || !acquired {
		return false, err
	}
	return false, nil
}

func rewriteSessionExpiry(t *testing.T, store kv.Store, accountID int32, token string, expiresAt time.Time) string {
	t.Helper()
	ctx := context.Background()
	key := sessionKey(accountID, token)
	raw, err := store.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	var data authn.SessionData
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		t.Fatal(err)
	}
	data.ExpiresAt = expiresAt
	payload, err := json.Marshal(&data)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetEx(ctx, key, string(payload), time.Until(expiresAt)); err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func TestSession_RevokeAllFailsIfLeaseOwnershipIsLostDuringPGRevocation(t *testing.T) {
	ctx := context.Background()
	base := kv.NewMemoryStore()
	t.Cleanup(func() { _ = base.Close() })
	queries := &blockingRevokeAllQueries{
		entered: make(chan struct{}),
		proceed: make(chan struct{}),
	}
	store := NewSessionStore(base, queries, time.Hour)
	result := make(chan error, 1)
	go func() {
		_, err := store.RevokeAllForAccount(ctx, 42)
		result <- err
	}()
	<-queries.entered

	leaseKey := sessionMutationLeaseKey(42)
	if err := base.Del(ctx, leaseKey); err != nil {
		t.Fatal(err)
	}
	if acquired, err := base.SetNX(ctx, leaseKey, "replacement-owner", time.Minute); err != nil || !acquired {
		t.Fatalf("replace expired lease: acquired=%v err=%v", acquired, err)
	}
	close(queries.proceed)
	if err := <-result; !errors.Is(err, errSessionMutationLeaseUnavailable) {
		t.Fatalf("RevokeAllForAccount error = %v, want lost lease", err)
	}
	if owner, err := base.Get(ctx, leaseKey); err != nil || owner != "replacement-owner" {
		t.Fatalf("replacement lease after revoke-all = (%q, %v)", owner, err)
	}
}

func TestSession_MutationLeaseFailuresFailClosedAndPreserveOwnership(t *testing.T) {
	t.Run("revoke-all leaves a competing owner untouched", func(t *testing.T) {
		ctx := context.Background()
		base := kv.NewMemoryStore()
		t.Cleanup(func() { _ = base.Close() })
		store := NewSessionStore(base, noopSessionQueries{}, time.Hour)
		leaseKey := sessionMutationLeaseKey(42)
		if acquired, err := base.SetNX(ctx, leaseKey, "competing-owner", time.Minute); err != nil || !acquired {
			t.Fatalf("seed competing lease: acquired=%v err=%v", acquired, err)
		}
		if _, err := store.RevokeAllForAccount(ctx, 42); !errors.Is(err, errSessionMutationLeaseUnavailable) {
			t.Fatalf("RevokeAllForAccount error = %v, want lease unavailable", err)
		}
		if owner, err := base.Get(ctx, leaseKey); err != nil || owner != "competing-owner" {
			t.Fatalf("competing lease after revoke-all = (%q, %v)", owner, err)
		}
	})

	t.Run("canceled revoke-all does not acquire a lease", func(t *testing.T) {
		base := kv.NewMemoryStore()
		t.Cleanup(func() { _ = base.Close() })
		store := NewSessionStore(base, noopSessionQueries{}, time.Hour)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := store.RevokeAllForAccount(ctx, 42); !errors.Is(err, context.Canceled) {
			t.Fatalf("RevokeAllForAccount error = %v, want context canceled", err)
		}
		if _, err := base.Get(context.Background(), sessionMutationLeaseKey(42)); !errors.Is(err, kv.ErrKeyNotFound) {
			t.Fatalf("canceled revoke-all left mutation lease: %v", err)
		}
	})

	t.Run("revoke-all fails when its lease expires and preserves the new owner", func(t *testing.T) {
		ctx := context.Background()
		base := kv.NewMemoryStore()
		t.Cleanup(func() { _ = base.Close() })
		leaseKey := sessionMutationLeaseKey(42)
		const replacement = "post-expiry-owner"
		store := NewSessionStore(&loseLeaseOnRenewStore{
			Store:       base,
			leaseKey:    leaseKey,
			replacement: replacement,
		}, noopSessionQueries{}, time.Hour)
		if _, err := store.RevokeAllForAccount(ctx, 42); !errors.Is(err, errSessionMutationLeaseUnavailable) {
			t.Fatalf("RevokeAllForAccount error = %v, want lost lease", err)
		}
		if owner, err := base.Get(ctx, leaseKey); err != nil || owner != replacement {
			t.Fatalf("replacement lease after expiry = (%q, %v)", owner, err)
		}
	})

	t.Run("revoke-all releases its lease after a scan failure", func(t *testing.T) {
		ctx := context.Background()
		base := kv.NewMemoryStore()
		t.Cleanup(func() { _ = base.Close() })
		store := NewSessionStore(&scanErrorStore{Store: base, err: errors.New("scan unavailable")}, noopSessionQueries{}, time.Hour)
		if _, err := store.RevokeAllForAccount(ctx, 42); err == nil {
			t.Fatal("RevokeAllForAccount succeeded after scan failure")
		}
		if _, err := base.Get(ctx, sessionMutationLeaseKey(42)); !errors.Is(err, kv.ErrKeyNotFound) {
			t.Fatalf("scan failure left mutation lease: %v", err)
		}
	})

	t.Run("refresh skips a failed fenced write", func(t *testing.T) {
		ctx := context.Background()
		base := kv.NewMemoryStore()
		t.Cleanup(func() { _ = base.Close() })
		const ttl = 10 * time.Second
		issuer := NewSessionStore(base, noopSessionQueries{}, ttl)
		token, _, err := issuer.Issue(ctx, 42, "", "", []string{"hwk"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		before := rewriteSessionExpiry(t, base, 42, token, time.Now().Add(time.Second))
		store := NewSessionStore(&fenceResultStore{Store: base, err: errors.New("fence unavailable")}, noopSessionQueries{}, ttl)
		if _, refreshed, err := store.Load(ctx, 42, token, "", ""); err != nil || refreshed {
			t.Fatalf("Load = refreshed %v, err %v; want valid without refresh", refreshed, err)
		}
		after, err := base.Get(ctx, sessionKey(42, token))
		if err != nil || after != before {
			t.Fatalf("session changed after failed fence: err=%v", err)
		}
	})

	t.Run("release deletes only its own lease", func(t *testing.T) {
		ctx := context.Background()
		base := kv.NewMemoryStore()
		t.Cleanup(func() { _ = base.Close() })
		const ttl = 10 * time.Second
		issuer := NewSessionStore(base, noopSessionQueries{}, ttl)
		token, _, err := issuer.Issue(ctx, 42, "", "", []string{"hwk"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		rewriteSessionExpiry(t, base, 42, token, time.Now().Add(time.Second))
		const replacement = "replacement-owner"
		store := NewSessionStore(&replaceLeaseBeforeReleaseStore{Store: base, replacement: replacement}, noopSessionQueries{}, ttl)
		if _, refreshed, err := store.Load(ctx, 42, token, "", ""); err != nil || !refreshed {
			t.Fatalf("Load = refreshed %v, err %v; want successful refresh", refreshed, err)
		}
		if owner, err := base.Get(ctx, sessionMutationLeaseKey(42)); err != nil || owner != replacement {
			t.Fatalf("replacement lease after release = (%q, %v)", owner, err)
		}
	})
}

func TestSession_SuccessfulFencedRefreshRestoresFullTTL(t *testing.T) {
	ctx := context.Background()
	base := kv.NewMemoryStore()
	t.Cleanup(func() { _ = base.Close() })
	const ttl = 10 * time.Second
	store := NewSessionStore(base, noopSessionQueries{}, ttl)
	token, _, err := store.Issue(ctx, 42, "", "", []string{"hwk"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rewriteSessionExpiry(t, base, 42, token, time.Now().Add(time.Second))
	if _, refreshed, err := store.Load(ctx, 42, token, "", ""); err != nil || !refreshed {
		t.Fatalf("Load = refreshed %v, err %v; want successful refresh", refreshed, err)
	}
	remaining, err := base.TTL(ctx, sessionKey(42, token))
	if err != nil || remaining < 9 {
		t.Fatalf("refreshed TTL = %d, err=%v; want at least 9 seconds", remaining, err)
	}
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

// TestSession_KVKeyHidesRawToken guards N1: the raw cookie token must never
// appear as the KV key suffix. A KV read (SCAN / dump / backup) must not yield
// a usable cookie. The stored key suffix is SHA-256(token); the cookie keeps
// the raw token.
func TestSession_KVKeyHidesRawToken(t *testing.T) {
	mem := kv.NewMemoryStore()
	s := NewSessionStore(mem, noopSessionQueries{}, time.Hour)
	ctx := context.Background()

	token, _, err := s.Issue(ctx, 42, "", "", []string{"hwk"}, nil)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	result, err := mem.ScanEntries(ctx, "session:42:*", 0, 100)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("want 1 session entry, got %d", len(result.Entries))
	}
	key := result.Entries[0].Key
	if strings.Contains(key, token) {
		t.Fatalf("KV key %q leaks the raw token %q", key, token)
	}
	if want := "session:42:" + hashToken(token); key != want {
		t.Errorf("KV key = %q, want %q (hashed suffix)", key, want)
	}

	// The raw token must still load the session (cookie half is unchanged).
	if _, _, err := s.Load(ctx, 42, token, "", ""); err != nil {
		t.Errorf("Load with raw token failed: %v", err)
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

// TestSession_IsSessionIDLive verifies that IsSessionIDLive returns true for a
// live session and false after the session is revoked (KV entry deleted). The
// PG session row persists (soft-deleted), so this check must not rely on it.
func TestSession_IsSessionIDLive(t *testing.T) {
	s := newTestStore(t, time.Hour)
	ctx := context.Background()

	token, data, err := s.Issue(ctx, 42, "", "", []string{"hwk"}, nil)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Live session → true.
	live, err := s.IsSessionIDLive(ctx, 42, data.SessionID)
	if err != nil {
		t.Fatalf("IsSessionIDLive: %v", err)
	}
	if !live {
		t.Error("IsSessionIDLive: want true for live session, got false")
	}

	// Wrong account → false (session belongs to 42, not 99).
	live, _ = s.IsSessionIDLive(ctx, 99, data.SessionID)
	if live {
		t.Error("IsSessionIDLive: want false for wrong account, got true")
	}

	// Unknown session ID → false.
	live, _ = s.IsSessionIDLive(ctx, 42, "never-issued-sid")
	if live {
		t.Error("IsSessionIDLive: want false for unknown session, got true")
	}

	// Empty session ID → false.
	live, _ = s.IsSessionIDLive(ctx, 42, "")
	if live {
		t.Error("IsSessionIDLive: want false for empty session ID, got true")
	}

	// Revoke the session → false.
	if err := s.Revoke(ctx, 42, token); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	live, _ = s.IsSessionIDLive(ctx, 42, data.SessionID)
	if live {
		t.Error("IsSessionIDLive: want false after revoke, got true")
	}
}

// TestSession_IsSessionIDLiveAfterRevokeAll verifies that IsSessionIDLive
// returns false for every session after RevokeAllForAccount.
func TestSession_IsSessionIDLiveAfterRevokeAll(t *testing.T) {
	s := newTestStore(t, time.Hour)
	ctx := context.Background()

	token1, data1, _ := s.Issue(ctx, 42, "", "", []string{"hwk"}, nil)
	_, data2, _ := s.Issue(ctx, 42, "", "", []string{"hwk"}, nil)

	// Both live.
	if live, _ := s.IsSessionIDLive(ctx, 42, data1.SessionID); !live {
		t.Fatal("data1 should be live")
	}
	if live, _ := s.IsSessionIDLive(ctx, 42, data2.SessionID); !live {
		t.Fatal("data2 should be live")
	}

	// Revoke all → both dead.
	if _, err := s.RevokeAllForAccount(ctx, 42); err != nil {
		t.Fatalf("RevokeAllForAccount: %v", err)
	}
	if live, _ := s.IsSessionIDLive(ctx, 42, data1.SessionID); live {
		t.Error("data1 should be dead after revoke-all")
	}
	if live, _ := s.IsSessionIDLive(ctx, 42, data2.SessionID); live {
		t.Error("data2 should be dead after revoke-all")
	}
	_ = token1
}
