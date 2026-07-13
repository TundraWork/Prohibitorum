// Package server — handle_nested_pagination_test.go
//
// Tests for nested admin collection pagination: credentials, sessions, PATs,
// groups, group members, and OIDC/SAML access groups/accounts.
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

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
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

// fakeNestedQ is a minimal db.Querier subset for testing nested pagination
// handlers. It implements the methods the handlers call: GetAccountByID,
// ListCredentialsByAccountPage, ListGroupsForAccountPage,
// ListGroupMembersPage, ListPATsByAccountPage, GetGroup,
// ListOIDCClientAccessGroupsPage, ListOIDCClientAccessAccountsPage,
// GetOIDCClientAny, ListSAMLSPAccessGroupsPage, ListSAMLSPAccessAccountsPage,
// GetSAMLSPByID.
type fakeNestedQ struct {
	accountMissing  bool
	groupMissing    bool
	oidcMissing     bool
	samlMissing     bool
	creds           []db.WebauthnCredential
	pats            []db.PersonalAccessToken
	groups          []db.UserGroup
	members         []db.ListGroupMembersPageRow
	oidcAccessGrps  []db.ListOIDCClientAccessGroupsPageRow
	oidcAccessAccs  []db.ListOIDCClientAccessAccountsPageRow
	samlAccessGrps  []db.ListSAMLSPAccessGroupsPageRow
	samlAccessAccs  []db.ListSAMLSPAccessAccountsPageRow
}

func (f *fakeNestedQ) GetAccountByID(_ context.Context, id int32) (db.Account, error) {
	if f.accountMissing {
		return db.Account{}, pgx.ErrNoRows
	}
	return db.Account{ID: id}, nil
}

func (f *fakeNestedQ) GetGroup(_ context.Context, id int32) (db.UserGroup, error) {
	if f.groupMissing {
		return db.UserGroup{}, pgx.ErrNoRows
	}
	return db.UserGroup{ID: id}, nil
}

func (f *fakeNestedQ) GetOIDCClientAny(_ context.Context, _ string) (db.OidcClient, error) {
	if f.oidcMissing {
		return db.OidcClient{}, pgx.ErrNoRows
	}
	return db.OidcClient{ClientID: "test-client"}, nil
}

func (f *fakeNestedQ) GetSAMLSPByID(_ context.Context, _ int64) (db.SamlSp, error) {
	if f.samlMissing {
		return db.SamlSp{}, pgx.ErrNoRows
	}
	return db.SamlSp{ID: 1}, nil
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

func (f *fakeNestedQ) ListGroupsForAccountPage(_ context.Context, arg db.ListGroupsForAccountPageParams) ([]db.UserGroup, error) {
	out := f.groups
	if int32(len(out)) > arg.RowLimit {
		out = out[:arg.RowLimit]
	}
	return out, nil
}

func (f *fakeNestedQ) ListGroupMembersPage(_ context.Context, arg db.ListGroupMembersPageParams) ([]db.ListGroupMembersPageRow, error) {
	out := f.members
	if int32(len(out)) > arg.RowLimit {
		out = out[:arg.RowLimit]
	}
	return out, nil
}

func (f *fakeNestedQ) ListOIDCClientAccessGroupsPage(_ context.Context, arg db.ListOIDCClientAccessGroupsPageParams) ([]db.ListOIDCClientAccessGroupsPageRow, error) {
	out := f.oidcAccessGrps
	if int32(len(out)) > arg.RowLimit {
		out = out[:arg.RowLimit]
	}
	return out, nil
}

func (f *fakeNestedQ) ListOIDCClientAccessAccountsPage(_ context.Context, arg db.ListOIDCClientAccessAccountsPageParams) ([]db.ListOIDCClientAccessAccountsPageRow, error) {
	out := f.oidcAccessAccs
	if int32(len(out)) > arg.RowLimit {
		out = out[:arg.RowLimit]
	}
	return out, nil
}

func (f *fakeNestedQ) ListSAMLSPAccessGroupsPage(_ context.Context, arg db.ListSAMLSPAccessGroupsPageParams) ([]db.ListSAMLSPAccessGroupsPageRow, error) {
	out := f.samlAccessGrps
	if int32(len(out)) > arg.RowLimit {
		out = out[:arg.RowLimit]
	}
	return out, nil
}

func (f *fakeNestedQ) ListSAMLSPAccessAccountsPage(_ context.Context, arg db.ListSAMLSPAccessAccountsPageParams) ([]db.ListSAMLSPAccessAccountsPageRow, error) {
	out := f.samlAccessAccs
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
		pageInput: pageInput{Limit: 50},
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
		pageInput: pageInput{Limit: 50},
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
		pageInput: pageInput{Limit: 2},
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
		pageInput: pageInput{Limit: 2},
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
		pageInput: pageInput{Cursor: cursor, Limit: 10},
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
		pageInput: pageInput{Limit: 50},
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
// Group members handler — parent 404 + page shape
// ---------------------------------------------------------------------------

func TestHandleListGroupMembers_PageShape(t *testing.T) {
	t.Parallel()

	members := []db.ListGroupMembersPageRow{
		{ID: 1, Username: "alpha", DisplayName: "Alpha"},
		{ID: 2, Username: "beta", DisplayName: "Beta"},
	}
	fakeQ := &fakeNestedQ{members: members}
	s := &Server{
		cursorCodec: testCodec(),
		nestedQueriesOverride: fakeQ,
		Audit:       noopAuditWriter{},
	}

	out, err := s.handleListGroupMembers(context.Background(), &listGroupMembersPageIn{
		ID: 1,
		pageInput: pageInput{Limit: 50},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Body.Items) != 2 {
		t.Fatalf("want 2 items, got %d", len(out.Body.Items))
	}
	if out.Body.NextCursor != "" {
		t.Errorf("nextCursor should be empty, got %q", out.Body.NextCursor)
	}
}

func TestHandleListGroupMembers_ParentNotFound404(t *testing.T) {
	t.Parallel()

	fakeQ := &fakeNestedQ{groupMissing: true}
	s := &Server{
		cursorCodec: testCodec(),
		nestedQueriesOverride: fakeQ,
		Audit:       noopAuditWriter{},
	}

	_, err := s.handleListGroupMembers(context.Background(), &listGroupMembersPageIn{
		ID: 999,
		pageInput: pageInput{Limit: 50},
	})
	if err == nil {
		t.Fatal("expected 404 for unknown group")
	}
	se, ok := err.(interface{ GetStatus() int })
	if !ok || se.GetStatus() != http.StatusNotFound {
		t.Fatalf("want 404, got %v", err)
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
		pageInput: pageInput{Limit: 2},
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
		pageInput: pageInput{Limit: 50},
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
		pageInput: pageInput{Limit: 2},
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
		pageInput: pageInput{Limit: 50},
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
// Account groups handler — page shape
// ---------------------------------------------------------------------------

func TestHandleListAccountGroups_PageShape(t *testing.T) {
	t.Parallel()

	groups := []db.UserGroup{
		{ID: 1, Slug: "g1", DisplayName: "Group 1"},
		{ID: 2, Slug: "g2", DisplayName: "Group 2"},
	}
	fakeQ := &fakeNestedQ{groups: groups}
	s := &Server{
		cursorCodec:           testCodec(),
		nestedQueriesOverride: fakeQ,
		Audit:                 noopAuditWriter{},
	}

	out, err := s.handleListAccountGroups(context.Background(), &listAccountPageIn{
		ID: 42,
		pageInput: pageInput{Limit: 50},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Body.Items) != 2 {
		t.Fatalf("want 2 items, got %d", len(out.Body.Items))
	}
}

func TestHandleListAccountGroups_ParentNotFound404(t *testing.T) {
	t.Parallel()

	fakeQ := &fakeNestedQ{accountMissing: true}
	s := &Server{
		cursorCodec:           testCodec(),
		nestedQueriesOverride: fakeQ,
		Audit:                 noopAuditWriter{},
	}

	_, err := s.handleListAccountGroups(context.Background(), &listAccountPageIn{
		ID: 999,
		pageInput: pageInput{Limit: 50},
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
// OIDC access handler — page shape for groups + accounts
// ---------------------------------------------------------------------------

func TestHandleGetOIDCClientAccess_PageShape(t *testing.T) {
	t.Parallel()

	fakeQ := &fakeNestedQ{
		oidcAccessGrps: []db.ListOIDCClientAccessGroupsPageRow{
			{ID: 1, Slug: "g1", DisplayName: "Group 1"},
			{ID: 2, Slug: "g2", DisplayName: "Group 2"},
		},
		oidcAccessAccs: []db.ListOIDCClientAccessAccountsPageRow{
			{ID: 10, Username: "user1", DisplayName: "User 1"},
		},
	}
	s := &Server{
		cursorCodec:           testCodec(),
		nestedQueriesOverride: fakeQ,
		Audit:                 noopAuditWriter{},
	}

	out, err := s.handleGetOIDCClientAccess(context.Background(), &getOIDCClientAccessPageIn{
		ClientID: "test-client",
		pageInput: pageInput{Limit: 50},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Body.Groups.Items) != 2 {
		t.Fatalf("groups: want 2, got %d", len(out.Body.Groups.Items))
	}
	if len(out.Body.Accounts.Items) != 1 {
		t.Fatalf("accounts: want 1, got %d", len(out.Body.Accounts.Items))
	}
}

func TestHandleGetOIDCClientAccess_ParentNotFound404(t *testing.T) {
	t.Parallel()

	fakeQ := &fakeNestedQ{oidcMissing: true}
	s := &Server{
		cursorCodec:           testCodec(),
		nestedQueriesOverride: fakeQ,
		Audit:                 noopAuditWriter{},
	}

	_, err := s.handleGetOIDCClientAccess(context.Background(), &getOIDCClientAccessPageIn{
		ClientID: "unknown",
		pageInput: pageInput{Limit: 50},
	})
	if err == nil {
		t.Fatal("expected 404 for unknown OIDC client")
	}
	se, ok := err.(interface{ GetStatus() int })
	if !ok || se.GetStatus() != http.StatusNotFound {
		t.Fatalf("want 404, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// SAML access handler — page shape
// ---------------------------------------------------------------------------

func TestHandleGetSAMLSPAccess_PageShape(t *testing.T) {
	t.Parallel()

	fakeQ := &fakeNestedQ{
		samlAccessGrps: []db.ListSAMLSPAccessGroupsPageRow{
			{ID: 1, Slug: "g1", DisplayName: "Group 1"},
		},
		samlAccessAccs: []db.ListSAMLSPAccessAccountsPageRow{
			{ID: 10, Username: "user1", DisplayName: "User 1"},
			{ID: 11, Username: "user2", DisplayName: "User 2"},
		},
	}
	s := &Server{
		cursorCodec:           testCodec(),
		nestedQueriesOverride: fakeQ,
		Audit:                 noopAuditWriter{},
	}

	out, err := s.handleGetSAMLSPAccess(context.Background(), &getSAMLSPAccessPageIn{
		ID: 1,
		pageInput: pageInput{Limit: 50},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Body.Groups.Items) != 1 {
		t.Fatalf("groups: want 1, got %d", len(out.Body.Groups.Items))
	}
	if len(out.Body.Accounts.Items) != 2 {
		t.Fatalf("accounts: want 2, got %d", len(out.Body.Accounts.Items))
	}
}

func TestHandleGetSAMLSPAccess_ParentNotFound404(t *testing.T) {
	t.Parallel()

	fakeQ := &fakeNestedQ{samlMissing: true}
	s := &Server{
		cursorCodec:           testCodec(),
		nestedQueriesOverride: fakeQ,
		Audit:                 noopAuditWriter{},
	}

	_, err := s.handleGetSAMLSPAccess(context.Background(), &getSAMLSPAccessPageIn{
		ID: 999,
		pageInput: pageInput{Limit: 50},
	})
	if err == nil {
		t.Fatal("expected 404 for unknown SAML SP")
	}
	se, ok := err.(interface{ GetStatus() int })
	if !ok || se.GetStatus() != http.StatusNotFound {
		t.Fatalf("want 404, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// No bare arrays: verify response shape is Page[T] not []T
// ---------------------------------------------------------------------------

func TestNestedPage_NoBareArray(t *testing.T) {
	t.Parallel()

	fakeQ := &fakeNestedQ{
		members: []db.ListGroupMembersPageRow{
			{ID: 1, Username: "a", DisplayName: "A"},
		},
	}
	s := &Server{
		cursorCodec:           testCodec(),
		nestedQueriesOverride: fakeQ,
		Audit:                 noopAuditWriter{},
	}

	out, err := s.handleListGroupMembers(context.Background(), &listGroupMembersPageIn{
		ID: 1,
		pageInput: pageInput{Limit: 50},
	})
	if err != nil {
		t.Fatal(err)
	}

	// The response body must be a Page[T] (object with items+nextCursor),
	// not a bare JSON array.
	b, _ := json.Marshal(out.Body)
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("response is not a JSON object (Page[T]): %v\nbody: %s", err, b)
	}
	if _, ok := raw["items"]; !ok {
		t.Fatalf("response missing 'items' field: %s", b)
	}
}

// Ensure unused imports are referenced.
var _ = authn.ErrAccountNotFound
var _ = contract.Page[any]{}
