// Package server — handle_nested_pagination_test.go
//
// Tests for retained nested admin collection pagination: credentials, sessions,
// and personal access tokens.
//
// These tests verify the cursor page contract on nested collections:
//   - Parent-not-found returns 404 (not 200 + empty page).
//   - Exact page boundary: limit exactly matching item count returns no next cursor.
//   - Concurrent insertion ordering: deterministic (created_at, id) keyset.
//   - Session scan bounds: ListPageByAccount returns at most `limit` items.
//   - Cursor parent-ID binding mismatch: a cursor from account A is rejected
//     when used against account B.

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
	"prohibitorum/pkg/pagination"
	sessstore "prohibitorum/pkg/session"
)

// testCodec is defined in pagination_test.go — reused here.

// ---------------------------------------------------------------------------
// Session KV pagination — ListPageByAccount
// ---------------------------------------------------------------------------

func TestSessionListPageByAccount_BoundedScanRespectsLimit(t *testing.T) {
	t.Parallel()

	mem := kv.NewMemoryStore()
	store := sessstore.NewSessionStore(mem, noopSessionQueriesForServer{}, time.Hour)
	ctx := context.Background()
	// Issue 5 sessions.
	for i := 0; i < 5; i++ {
		if _, _, err := store.Issue(ctx, 42, "127.0.0.1", "ua", []string{"hwk"}, nil); err != nil {
			t.Fatal(err)
		}
	}

	// Request page of 2.
	page, hasMore, err := store.ListPageByAccount(ctx, 42, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 {
		t.Fatalf("page size = %d, want 2", len(page))
	}
	if !hasMore {
		t.Fatal("hasMore should be true when there are remaining sessions")
	}

	// Request page of 5 (all).
	page2, hasMore2, err := store.ListPageByAccount(ctx, 42, nil, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 5 {
		t.Fatalf("page2 size = %d, want 5", len(page2))
	}
	if hasMore2 {
		t.Fatal("hasMore should be false when all sessions fit in one page")
	}

	// Request page of 10 (more than available).
	page3, hasMore3, err := store.ListPageByAccount(ctx, 42, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page3) != 5 {
		t.Fatalf("page3 size = %d, want 5", len(page3))
	}
	if hasMore3 {
		t.Fatal("hasMore should be false when limit exceeds available")
	}
}

func TestSessionListPageByAccount_CursorAdvancesPages(t *testing.T) {
	t.Parallel()

	mem := kv.NewMemoryStore()
	store := sessstore.NewSessionStore(mem, noopSessionQueriesForServer{}, time.Hour)
	ctx := context.Background()

	// Issue 4 sessions.
	for i := 0; i < 4; i++ {
		if _, _, err := store.Issue(ctx, 42, "127.0.0.1", "", []string{"hwk"}, nil); err != nil {
			t.Fatal(err)
		}
	}

	// Page 1: 2 items.
	page1, hasMore, err := store.ListPageByAccount(ctx, 42, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 2 || !hasMore {
		t.Fatalf("page1: len=%d hasMore=%v, want 2/true", len(page1), hasMore)
	}

	// Cursor from last item of page1.
	cursor := &sessstore.SessionPageCursor{
		IssuedAt:  page1[1].Data.IssuedAt,
		SessionID: page1[1].Data.SessionID,
	}

	// Page 2: 2 items, no more.
	page2, hasMore2, err := store.ListPageByAccount(ctx, 42, cursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 2 || hasMore2 {
		t.Fatalf("page2: len=%d hasMore=%v, want 2/false", len(page2), hasMore2)
	}

	// Verify no overlap: page1 session IDs differ from page2.
	seen := map[string]bool{}
	for _, r := range page1 {
		seen[r.Data.SessionID] = true
	}
	for _, r := range page2 {
		if seen[r.Data.SessionID] {
			t.Fatalf("session %s appeared in both pages", r.Data.SessionID)
		}
	}
}

func TestSessionListPageByAccount_StableOrdering(t *testing.T) {
	t.Parallel()

	mem := kv.NewMemoryStore()
	store := sessstore.NewSessionStore(mem, noopSessionQueriesForServer{}, time.Hour)
	ctx := context.Background()

	// Issue 3 sessions.
	for i := 0; i < 3; i++ {
		if _, _, err := store.Issue(ctx, 42, "", "", []string{"hwk"}, nil); err != nil {
			t.Fatal(err)
		}
	}

	// Two reads should return the same order.
	page1, _, _ := store.ListPageByAccount(ctx, 42, nil, 10)
	page2, _, _ := store.ListPageByAccount(ctx, 42, nil, 10)

	if len(page1) != len(page2) {
		t.Fatalf("page lengths differ: %d vs %d", len(page1), len(page2))
	}
	for i := range page1 {
		if page1[i].Data.SessionID != page2[i].Data.SessionID {
			t.Fatalf("position %d: %s vs %s (unstable ordering)",
				i, page1[i].Data.SessionID, page2[i].Data.SessionID)
		}
	}
}

func TestSessionListPageByAccount_AccountIsolation(t *testing.T) {
	t.Parallel()

	mem := kv.NewMemoryStore()
	store := sessstore.NewSessionStore(mem, noopSessionQueriesForServer{}, time.Hour)
	ctx := context.Background()

	// Issue sessions for two accounts.
	if _, _, err := store.Issue(ctx, 42, "", "", []string{"hwk"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Issue(ctx, 99, "", "", []string{"hwk"}, nil); err != nil {
		t.Fatal(err)
	}

	page42, _, _ := store.ListPageByAccount(ctx, 42, nil, 10)
	page99, _, _ := store.ListPageByAccount(ctx, 99, nil, 10)

	if len(page42) != 1 {
		t.Fatalf("account 42: want 1 session, got %d", len(page42))
	}
	if len(page99) != 1 {
		t.Fatalf("account 99: want 1 session, got %d", len(page99))
	}
}

func TestSessionListPageByAccount_EmptyAccount(t *testing.T) {
	t.Parallel()

	mem := kv.NewMemoryStore()
	store := sessstore.NewSessionStore(mem, noopSessionQueriesForServer{}, time.Hour)
	ctx := context.Background()

	page, hasMore, err := store.ListPageByAccount(ctx, 42, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 0 {
		t.Fatalf("want 0 sessions, got %d", len(page))
	}
	if hasMore {
		t.Fatal("hasMore should be false for empty account")
	}
}

// ---------------------------------------------------------------------------
// Cursor parent-ID binding mismatch
// ---------------------------------------------------------------------------

func TestNestedCursor_ParentIDBindingMismatch(t *testing.T) {
	t.Parallel()

	codec := testCodec()

	// Issue a cursor bound to accountId=42.
	cursor, err := codec.Encode(pagination.CursorPayload{
		Collection: "account_credentials",
		Filters:    map[string]string{"accountId": "42"},
		Sort:       "created_at",
		Keys:       []string{"2024-01-01T00:00:00Z", "1"},
		IssuedAt:   time.Now(),
		ExpiresAt:  time.Now().Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Decoding against the same accountId should succeed.
	_, err = codec.Decode(cursor, "account_credentials", "created_at", map[string]string{"accountId": "42"})
	if err != nil {
		t.Fatalf("decode with matching parent ID: %v", err)
	}

	// Decoding against a different accountId (99) must fail.
	_, err = codec.Decode(cursor, "account_credentials", "created_at", map[string]string{"accountId": "99"})
	if err == nil {
		t.Fatal("cursor with accountId=42 should be rejected for accountId=99")
	}
}

// ---------------------------------------------------------------------------
// Account credentials page — parent-not-found 404
// ---------------------------------------------------------------------------

// fakeNestedQ is the minimal query subset used by retained nested pagination
// handlers: account existence, credential pages, and PAT pages.
type fakeNestedQ struct {
	accountMissing bool
	creds          []db.WebauthnCredential
	pats           []db.PersonalAccessToken
}

func (f *fakeNestedQ) GetAccountByID(_ context.Context, id int32) (db.Account, error) {
	if f.accountMissing {
		return db.Account{}, pgx.ErrNoRows
	}
	return db.Account{ID: id}, nil
}


func (f *fakeNestedQ) ListCredentialsByAccountPage(_ context.Context, arg db.ListCredentialsByAccountPageParams) ([]db.WebauthnCredential, error) {
	out := f.creds
	if int32(len(out)) > arg.RowLimit {
		out = out[:arg.RowLimit]
	}
	return out, nil
}

func (f *fakeNestedQ) ListPATsByAccountPage(_ context.Context, arg db.ListPATsByAccountPageParams) ([]db.PersonalAccessToken, error) {
	out := f.pats
	if int32(len(out)) > arg.RowLimit {
		out = out[:arg.RowLimit]
	}
	return out, nil
}


// noopSessionQueriesForServer is a no-op SessionQueries for server tests.
type noopSessionQueriesForServer struct{}

func (noopSessionQueriesForServer) InsertSession(context.Context, db.InsertSessionParams) (db.Session, error) {
	return db.Session{}, nil
}
func (noopSessionQueriesForServer) RevokeSession(context.Context, string) error { return nil }
func (noopSessionQueriesForServer) RevokeAllSessionsByAccount(context.Context, int32) error {
	return nil
}

// ---------------------------------------------------------------------------
// Account credentials handler — parent 404 + page boundary
// ---------------------------------------------------------------------------

func TestHandleListAccountCredentials_PageShape(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	creds := []db.WebauthnCredential{
		{ID: 1, AccountID: 42, CredentialID: []byte("cred1"), CreatedAt: pgtype.Timestamptz{Time: now, Valid: true}},
		{ID: 2, AccountID: 42, CredentialID: []byte("cred2"), CreatedAt: pgtype.Timestamptz{Time: now.Add(time.Second), Valid: true}},
	}
	fakeQ := &fakeNestedQ{creds: creds}
	s := &Server{
		cursorCodec: testCodec(),
		nestedQueriesOverride: fakeQ,
		Audit:       noopAuditWriter{},
	}

	out, err := s.handleListAccountCredentials(context.Background(), &listAccountPageIn{
		ID: 42,
		PageInput: PageInput{Limit: 50},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify page shape: items + nextCursor.
	var raw map[string]any
	b, _ := json.Marshal(out.Body)
	_ = json.Unmarshal(b, &raw)
	if _, ok := raw["items"]; !ok {
		t.Fatal("response must have 'items' field")
	}
	if _, ok := raw["nextCursor"]; !ok {
		t.Fatal("response must have 'nextCursor' field")
	}
	if raw["nextCursor"] != "" {
		t.Errorf("nextCursor should be empty when all items fit, got %v", raw["nextCursor"])
	}
}

func TestHandleListAccountCredentials_ParentNotFound404(t *testing.T) {
	t.Parallel()

	fakeQ := &fakeNestedQ{accountMissing: true}
	s := &Server{
		cursorCodec: testCodec(),
		nestedQueriesOverride: fakeQ,
		Audit:       noopAuditWriter{},
	}

	_, err := s.handleListAccountCredentials(context.Background(), &listAccountPageIn{
		ID: 999,
		PageInput: PageInput{Limit: 50},
	})
	if err == nil {
		t.Fatal("expected 404 for unknown account")
	}
	se, ok := err.(interface{ GetStatus() int })
	if !ok || se.GetStatus() != http.StatusNotFound {
		t.Fatalf("want 404, got %v", err)
	}
}

func TestHandleListAccountCredentials_LimitClamp(t *testing.T) {
	t.Parallel()

	creds := make([]db.WebauthnCredential, 3)
	now := time.Now().UTC()
	for i := range creds {
		creds[i] = db.WebauthnCredential{
			ID: int32(i + 1), AccountID: 42, CredentialID: []byte(fmt.Sprintf("c%d", i)),
			CreatedAt: pgtype.Timestamptz{Time: now.Add(time.Duration(i) * time.Second), Valid: true},
		}
	}
	fakeQ := &fakeNestedQ{creds: creds}
	s := &Server{
		cursorCodec: testCodec(),
		nestedQueriesOverride: fakeQ,
		Audit:       noopAuditWriter{},
	}

	// Request limit=2 with 3 items → should get 2 items + nextCursor.
	out, err := s.handleListAccountCredentials(context.Background(), &listAccountPageIn{
		ID: 42,
		PageInput: PageInput{Limit: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Body.Items) != 2 {
		t.Fatalf("items: want 2, got %d", len(out.Body.Items))
	}
	if out.Body.NextCursor == "" {
		t.Fatal("nextCursor should be non-empty when there are more items")
	}
}

func TestHandleListAccountCredentials_ExactPageBoundary(t *testing.T) {
	t.Parallel()

	creds := make([]db.WebauthnCredential, 2)
	now := time.Now().UTC()
	for i := range creds {
		creds[i] = db.WebauthnCredential{
			ID: int32(i + 1), AccountID: 42, CredentialID: []byte(fmt.Sprintf("c%d", i)),
			CreatedAt: pgtype.Timestamptz{Time: now.Add(time.Duration(i) * time.Second), Valid: true},
		}
	}
	fakeQ := &fakeNestedQ{creds: creds}
	s := &Server{
		cursorCodec: testCodec(),
		nestedQueriesOverride: fakeQ,
		Audit:       noopAuditWriter{},
	}

	// Request limit=2 with exactly 2 items → no nextCursor.
	out, err := s.handleListAccountCredentials(context.Background(), &listAccountPageIn{
		ID: 42,
		PageInput: PageInput{Limit: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Body.Items) != 2 {
		t.Fatalf("items: want 2, got %d", len(out.Body.Items))
	}
	if out.Body.NextCursor != "" {
		t.Errorf("nextCursor should be empty at exact boundary, got %q", out.Body.NextCursor)
	}
}

func TestHandleListAccountCredentials_CursorParentIDMismatch(t *testing.T) {
	t.Parallel()

	codec := testCodec()
	creds := make([]db.WebauthnCredential, 3)
	now := time.Now().UTC()
	for i := range creds {
		creds[i] = db.WebauthnCredential{
			ID: int32(i + 1), AccountID: 42, CredentialID: []byte(fmt.Sprintf("c%d", i)),
			CreatedAt: pgtype.Timestamptz{Time: now.Add(time.Duration(i) * time.Second), Valid: true},
		}
	}

	// Issue a cursor for account 42.
	cursor, err := codec.Encode(pagination.CursorPayload{
		Collection: "account_credentials",
		Filters:    map[string]string{"accountId": "42"},
		Sort:       "created_at",
		Keys:       []string{now.Format(time.RFC3339Nano), "1"},
		IssuedAt:   time.Now(),
		ExpiresAt:  time.Now().Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	fakeQ := &fakeNestedQ{creds: creds, accountMissing: true}
	s := &Server{
		cursorCodec: codec,
		nestedQueriesOverride: fakeQ,
		Audit:       noopAuditWriter{},
	}

	// Use the cursor (bound to accountId=42) against account 99 — the
	// account-existence check fires first and returns 404.
	_, err = s.handleListAccountCredentials(context.Background(), &listAccountPageIn{
		ID: 99,
		PageInput: PageInput{Cursor: cursor, Limit: 10},
	})
	if err == nil {
		t.Fatal("expected error for unknown account 99")
	}
}

func TestHandleListAccountCredentials_EmptyResultShape(t *testing.T) {
	t.Parallel()

	fakeQ := &fakeNestedQ{}
	s := &Server{
		cursorCodec: testCodec(),
		nestedQueriesOverride: fakeQ,
		Audit:       noopAuditWriter{},
	}

	out, err := s.handleListAccountCredentials(context.Background(), &listAccountPageIn{
		ID: 42,
		PageInput: PageInput{Limit: 50},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Empty page must be items:[] not null, nextCursor:""
	if out.Body.Items == nil {
		t.Fatal("Items should be non-nil (empty page must be [] not null)")
	}
	if len(out.Body.Items) != 0 {
		t.Fatalf("want 0 items, got %d", len(out.Body.Items))
	}
	if out.Body.NextCursor != "" {
		t.Errorf("nextCursor should be empty, got %q", out.Body.NextCursor)
	}
}


// ---------------------------------------------------------------------------
// Account sessions handler — page shape via KV
// ---------------------------------------------------------------------------

func TestHandleListAccountSessions_PageShape(t *testing.T) {
	t.Parallel()

	mem := kv.NewMemoryStore()
	store := sessstore.NewSessionStore(mem, noopSessionQueriesForServer{}, time.Hour)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, _, err := store.Issue(ctx, 42, "127.0.0.1", "", []string{"hwk"}, nil); err != nil {
			t.Fatal(err)
		}
	}

	fakeQ := &fakeNestedQ{}
	s := &Server{
		cursorCodec:           testCodec(),
		nestedQueriesOverride: fakeQ,
		sessionStore:          store,
		Audit:                 noopAuditWriter{},
	}

	out, err := s.handleListAccountSessions(ctx, &listAccountPageIn{
		ID: 42,
		PageInput: PageInput{Limit: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Body.Items) != 2 {
		t.Fatalf("want 2 items, got %d", len(out.Body.Items))
	}
	if out.Body.NextCursor == "" {
		t.Fatal("nextCursor should be non-empty when there are more sessions")
	}
}

func TestHandleListAccountSessions_ParentNotFound404(t *testing.T) {
	t.Parallel()

	mem := kv.NewMemoryStore()
	store := sessstore.NewSessionStore(mem, noopSessionQueriesForServer{}, time.Hour)

	fakeQ := &fakeNestedQ{accountMissing: true}
	s := &Server{
		cursorCodec:           testCodec(),
		nestedQueriesOverride: fakeQ,
		sessionStore:          store,
		Audit:                 noopAuditWriter{},
	}

	_, err := s.handleListAccountSessions(context.Background(), &listAccountPageIn{
		ID: 999,
		PageInput: PageInput{Limit: 50},
	})
	if err == nil {
		t.Fatal("expected 404 for unknown account")
	}
	se, ok := err.(interface{ GetStatus() int })
	if !ok || se.GetStatus() != http.StatusNotFound {
		t.Fatalf("want 404, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// PATs handler — page shape
// ---------------------------------------------------------------------------

func TestHandleListAccountTokens_PageShape(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	pats := []db.PersonalAccessToken{
		{ID: 1, AccountID: 42, Name: "t1", TokenHint: "a...z", AllApps: false, AppGrants: []byte(`{}`), CreatedAt: pgtype.Timestamptz{Time: now, Valid: true}},
		{ID: 2, AccountID: 42, Name: "t2", TokenHint: "b...y", AllApps: true, AppGrants: []byte(`{}`), CreatedAt: pgtype.Timestamptz{Time: now.Add(time.Second), Valid: true}},
		{ID: 3, AccountID: 42, Name: "t3", TokenHint: "c...x", AllApps: false, AppGrants: []byte(`{}`), CreatedAt: pgtype.Timestamptz{Time: now.Add(2 * time.Second), Valid: true}},
	}
	fakeQ := &fakeNestedQ{pats: pats}
	s := &Server{
		cursorCodec:           testCodec(),
		nestedQueriesOverride: fakeQ,
		Audit:                 noopAuditWriter{},
	}

	// Limit=2 with 3 items → 2 items + nextCursor.
	out, err := s.handleListAccountTokens(context.Background(), &listAccountPageIn{
		ID: 42,
		PageInput: PageInput{Limit: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Body.Items) != 2 {
		t.Fatalf("want 2 items, got %d", len(out.Body.Items))
	}
	if out.Body.NextCursor == "" {
		t.Fatal("nextCursor should be non-empty when there are more PATs")
	}
}

func TestHandleListAccountTokens_ParentNotFound404(t *testing.T) {
	t.Parallel()

	fakeQ := &fakeNestedQ{accountMissing: true}
	s := &Server{
		cursorCodec:           testCodec(),
		nestedQueriesOverride: fakeQ,
		Audit:                 noopAuditWriter{},
	}

	_, err := s.handleListAccountTokens(context.Background(), &listAccountPageIn{
		ID: 999,
		PageInput: PageInput{Limit: 50},
	})
	if err == nil {
		t.Fatal("expected 404 for unknown account")
	}
	se, ok := err.(interface{ GetStatus() int })
	if !ok || se.GetStatus() != http.StatusNotFound {
		t.Fatalf("want 404, got %v", err)
	}
}


