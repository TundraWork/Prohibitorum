// Package server — handle_me_tokens_test.go
//
// Unit tests for GET /me/tokens (handleListMyTokens), POST /me/tokens
// (handleCreateMyToken) and POST /me/tokens/revoke (handleRevokeMyToken).
//
// Design: DB-free. A minimal fake querier implements only the three PAT methods
// the handlers exercise plus InsertCredentialEvent (audit no-op). All other
// db.Querier methods are left to the embedded nil interface — calling them
// panics, catching accidental over-reach. The fake is wired via the
// patQueriesOverride seam so no real DB or *db.Queries is needed.
//
// Sudo gating lives in the route middleware (registerSudoOp) and is NOT
// exercised here — these tests call handlers directly, bypassing middleware.
// The sudo gate is covered by admin_route_policy_test.go.

package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
)

// ---------------------------------------------------------------------------
// Fake querier
// ---------------------------------------------------------------------------

// fakePATQ implements db.Querier by embedding the nil interface and overriding
// only the PAT methods the handlers call. Calling any other method panics.
type fakePATQ struct {
	db.Querier // embedded nil — unimplemented methods panic if called

	rows   []db.PersonalAccessToken // seed + mutated state
	nextID int32                    // auto-increment for inserts
}

func (f *fakePATQ) InsertPAT(_ context.Context, arg db.InsertPATParams) (db.PersonalAccessToken, error) {
	f.nextID++
	row := db.PersonalAccessToken{
		ID:               f.nextID,
		AccountID:        arg.AccountID,
		Name:             arg.Name,
		TokenHash:        arg.TokenHash,
		TokenHint:        arg.TokenHint,
		UpstreamScopes:   arg.UpstreamScopes,
		AllowedClientIds: arg.AllowedClientIds,
		CreatedAt:        pgtype.Timestamptz{Time: time.Now(), Valid: true},
		ExpiresAt:        arg.ExpiresAt,
	}
	f.rows = append(f.rows, row)
	return row, nil
}

func (f *fakePATQ) ListPATsByAccount(_ context.Context, accountID int32) ([]db.PersonalAccessToken, error) {
	var out []db.PersonalAccessToken
	for _, r := range f.rows {
		if r.AccountID == accountID && !r.RevokedAt.Valid {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakePATQ) RevokePAT(_ context.Context, arg db.RevokePATParams) (int64, error) {
	for i := range f.rows {
		if f.rows[i].ID == arg.ID && f.rows[i].AccountID == arg.AccountID && !f.rows[i].RevokedAt.Valid {
			f.rows[i].RevokedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
			return 1, nil
		}
	}
	return 0, nil
}

// InsertCredentialEvent is a no-op audit sink.
func (f *fakePATQ) InsertCredentialEvent(_ context.Context, _ db.InsertCredentialEventParams) error {
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newPATServer builds the smallest Server that can run the PAT handlers.
func newPATServer(q *fakePATQ) *Server {
	return &Server{
		patQueriesOverride: q,
		Audit:              audit.NewWriter(q),
	}
}

// patCtx returns a context with a minimal authenticated session for accountID.
func patCtx(accountID int32) context.Context {
	acct := &db.Account{ID: accountID, Username: "alice", DisplayName: "Alice"}
	sess := &authn.Session{Account: acct}
	return authn.WithSession(context.Background(), sess)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestHandleCreateMyToken_HappyPath — create returns a non-empty plaintext
// token and a hint; the view never includes the hash; list confirms the row.
func TestHandleCreateMyToken_HappyPath(t *testing.T) {
	t.Parallel()

	q := &fakePATQ{}
	s := newPATServer(q)
	ctx := patCtx(1)

	in := &createMyTokenIn{}
	in.Body.Name = "ci-runner"

	out, err := s.handleCreateMyToken(ctx, in)
	if err != nil {
		t.Fatalf("handleCreateMyToken: %v", err)
	}
	if out.Body.Token == "" {
		t.Error("Token: want non-empty plaintext, got empty string")
	}
	if out.Body.PAT.TokenHint == "" {
		t.Error("PAT.TokenHint: want non-empty hint, got empty string")
	}
	if out.Body.PAT.Name != "ci-runner" {
		t.Errorf("PAT.Name: want %q, got %q", "ci-runner", out.Body.PAT.Name)
	}
	// The plaintext must not equal the hint (hint is a prefix+suffix stub, not
	// the raw token).
	if out.Body.Token == out.Body.PAT.TokenHint {
		t.Error("Token == TokenHint; hint must not expose the raw secret")
	}
	// The response struct has no TokenHash field — verify via the underlying row.
	if len(q.rows) != 1 {
		t.Fatalf("InsertPAT call count: want 1 row, got %d", len(q.rows))
	}
	row := q.rows[0]
	if len(row.TokenHash) == 0 {
		t.Error("row.TokenHash: want non-empty hash stored, got empty")
	}
	// Stored hash must not be the raw token byte-for-byte.
	if string(row.TokenHash) == out.Body.Token {
		t.Error("row.TokenHash is the raw token — must store a hash, not the plaintext")
	}
}

// TestHandleCreateMyToken_BlankName — empty name returns bad_request without
// inserting any row.
func TestHandleCreateMyToken_BlankName(t *testing.T) {
	t.Parallel()

	q := &fakePATQ{}
	s := newPATServer(q)
	ctx := patCtx(1)

	in := &createMyTokenIn{}
	in.Body.Name = "   " // whitespace only

	_, err := s.handleCreateMyToken(ctx, in)
	if err == nil {
		t.Fatal("expected error for blank name, got nil")
	}
	if code := codeFromErr(t, err); code != "bad_request" {
		t.Errorf("code: want bad_request, got %s", code)
	}
	if len(q.rows) != 0 {
		t.Errorf("InsertPAT must not be called for blank name; got %d row(s)", len(q.rows))
	}
}

// TestHandleCreateMyToken_NameTooLong — a name longer than 128 chars returns
// bad_request without inserting any row.
func TestHandleCreateMyToken_NameTooLong(t *testing.T) {
	t.Parallel()

	q := &fakePATQ{}
	s := newPATServer(q)
	ctx := patCtx(1)

	in := &createMyTokenIn{}
	in.Body.Name = strings.Repeat("x", 129)

	_, err := s.handleCreateMyToken(ctx, in)
	if err == nil {
		t.Fatal("expected error for over-long name, got nil")
	}
	if code := codeFromErr(t, err); code != "bad_request" {
		t.Errorf("code: want bad_request, got %s", code)
	}
	if len(q.rows) != 0 {
		t.Errorf("InsertPAT must not be called for over-long name; got %d row(s)", len(q.rows))
	}
}

// TestHandleCreateMyToken_NegativeExpiry — a negative expiresInDays must be
// rejected (it must NOT silently fall through to a no-expiry immortal token).
func TestHandleCreateMyToken_NegativeExpiry(t *testing.T) {
	t.Parallel()

	q := &fakePATQ{}
	s := newPATServer(q)
	ctx := patCtx(1)

	days := -7
	in := &createMyTokenIn{}
	in.Body.Name = "negative-expiry"
	in.Body.ExpiresInDays = &days

	_, err := s.handleCreateMyToken(ctx, in)
	if err == nil {
		t.Fatal("expected error for negative expiresInDays, got nil")
	}
	if code := codeFromErr(t, err); code != "bad_request" {
		t.Errorf("code: want bad_request, got %s", code)
	}
	if len(q.rows) != 0 {
		t.Errorf("InsertPAT must not be called for negative expiry; got %d row(s)", len(q.rows))
	}
}

// TestHandleCreateMyToken_ExpiryOverCap — an expiresInDays beyond the 3650-day
// (~10 year) sanity cap returns bad_request without inserting a row.
func TestHandleCreateMyToken_ExpiryOverCap(t *testing.T) {
	t.Parallel()

	q := &fakePATQ{}
	s := newPATServer(q)
	ctx := patCtx(1)

	days := 4000
	in := &createMyTokenIn{}
	in.Body.Name = "too-long-lived"
	in.Body.ExpiresInDays = &days

	_, err := s.handleCreateMyToken(ctx, in)
	if err == nil {
		t.Fatal("expected error for expiresInDays over cap, got nil")
	}
	if code := codeFromErr(t, err); code != "bad_request" {
		t.Errorf("code: want bad_request, got %s", code)
	}
	if len(q.rows) != 0 {
		t.Errorf("InsertPAT must not be called for over-cap expiry; got %d row(s)", len(q.rows))
	}
}

// TestHandleListMyTokens_ReturnsOnlyActiveTokens — list after create includes
// the row; list after revoke excludes it. The plaintext is never in list output.
func TestHandleListMyTokens_ReturnsOnlyActiveTokens(t *testing.T) {
	t.Parallel()

	q := &fakePATQ{}
	s := newPATServer(q)
	ctx := patCtx(7)

	// Create one token.
	createIn := &createMyTokenIn{}
	createIn.Body.Name = "my-token"
	createOut, err := s.handleCreateMyToken(ctx, createIn)
	if err != nil {
		t.Fatalf("handleCreateMyToken: %v", err)
	}
	createdID := createOut.Body.PAT.ID

	// List: should contain exactly one token.
	listOut, err := s.handleListMyTokens(ctx, nil)
	if err != nil {
		t.Fatalf("handleListMyTokens: %v", err)
	}
	if len(listOut.Body) != 1 {
		t.Fatalf("list count: want 1, got %d", len(listOut.Body))
	}
	item := listOut.Body[0]
	if item.ID != createdID {
		t.Errorf("list item ID: want %d, got %d", createdID, item.ID)
	}
	if item.Name != "my-token" {
		t.Errorf("list item Name: want %q, got %q", "my-token", item.Name)
	}

	// Revoke the token.
	revokeIn := &revokeMyTokenIn{}
	revokeIn.Body.ID = createdID
	if _, err := s.handleRevokeMyToken(ctx, revokeIn); err != nil {
		t.Fatalf("handleRevokeMyToken: %v", err)
	}

	// List after revoke: should be empty.
	listOut2, err := s.handleListMyTokens(ctx, nil)
	if err != nil {
		t.Fatalf("handleListMyTokens after revoke: %v", err)
	}
	if len(listOut2.Body) != 0 {
		t.Errorf("list count after revoke: want 0, got %d", len(listOut2.Body))
	}
}

// TestHandleRevokeMyToken_ForeignID — revoking an ID that belongs to a
// different account (or doesn't exist) returns credential_not_found.
func TestHandleRevokeMyToken_ForeignID(t *testing.T) {
	t.Parallel()

	q := &fakePATQ{}
	s := newPATServer(q)

	// Create a token for account 1.
	ctx1 := patCtx(1)
	createIn := &createMyTokenIn{}
	createIn.Body.Name = "account-1-token"
	createOut, err := s.handleCreateMyToken(ctx1, createIn)
	if err != nil {
		t.Fatalf("handleCreateMyToken: %v", err)
	}
	createdID := createOut.Body.PAT.ID

	// Account 2 tries to revoke account 1's token.
	ctx2 := patCtx(2)
	revokeIn := &revokeMyTokenIn{}
	revokeIn.Body.ID = createdID
	_, err = s.handleRevokeMyToken(ctx2, revokeIn)
	if err == nil {
		t.Fatal("expected error when revoking another account's token, got nil")
	}
	if code := codeFromErr(t, err); code != "credential_not_found" {
		t.Errorf("code: want credential_not_found, got %s", code)
	}

	// The token must still be active (not actually revoked).
	listOut, err := s.handleListMyTokens(ctx1, nil)
	if err != nil {
		t.Fatalf("handleListMyTokens: %v", err)
	}
	if len(listOut.Body) != 1 {
		t.Errorf("token must still be active after failed foreign-account revoke; list count = %d", len(listOut.Body))
	}
}

// TestHandleRevokeMyToken_DoubleRevoke — revoking an already-revoked token
// returns credential_not_found (idempotent revoke is not guaranteed).
func TestHandleRevokeMyToken_DoubleRevoke(t *testing.T) {
	t.Parallel()

	q := &fakePATQ{}
	s := newPATServer(q)
	ctx := patCtx(3)

	// Create and revoke.
	createIn := &createMyTokenIn{}
	createIn.Body.Name = "short-lived"
	createOut, err := s.handleCreateMyToken(ctx, createIn)
	if err != nil {
		t.Fatalf("handleCreateMyToken: %v", err)
	}
	revokeIn := &revokeMyTokenIn{}
	revokeIn.Body.ID = createOut.Body.PAT.ID
	if _, err := s.handleRevokeMyToken(ctx, revokeIn); err != nil {
		t.Fatalf("first revoke: %v", err)
	}

	// Second revoke: the row is already revoked_at, so RevokePAT returns 0 rows.
	_, err = s.handleRevokeMyToken(ctx, revokeIn)
	if err == nil {
		t.Fatal("expected error on double-revoke, got nil")
	}
	if code := codeFromErr(t, err); code != "credential_not_found" {
		t.Errorf("code: want credential_not_found, got %s", code)
	}
}

// TestHandleListMyTokens_NeverExposesPlaintextOrHash — patView must not copy
// the plaintext or hash into the wire view.
func TestHandleListMyTokens_NeverExposesPlaintextOrHash(t *testing.T) {
	t.Parallel()

	q := &fakePATQ{}
	s := newPATServer(q)
	ctx := patCtx(5)

	// Create a token.
	createIn := &createMyTokenIn{}
	createIn.Body.Name = "secret-guard"
	createOut, err := s.handleCreateMyToken(ctx, createIn)
	if err != nil {
		t.Fatalf("handleCreateMyToken: %v", err)
	}
	rawToken := createOut.Body.Token

	// List and verify the view shape.
	listOut, err := s.handleListMyTokens(ctx, nil)
	if err != nil {
		t.Fatalf("handleListMyTokens: %v", err)
	}
	if len(listOut.Body) != 1 {
		t.Fatalf("list count: want 1, got %d", len(listOut.Body))
	}
	view := listOut.Body[0]
	// The view struct has no Token or TokenHash field. Verify the hint ≠ raw token.
	if view.TokenHint == rawToken {
		t.Error("TokenHint exposes the raw token plaintext — must be a non-secret display aid")
	}
	// Sanity: hint is non-empty.
	if view.TokenHint == "" {
		t.Error("TokenHint: want non-empty, got empty")
	}
}
