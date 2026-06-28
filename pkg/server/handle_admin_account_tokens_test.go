// Package server — handle_admin_account_tokens_test.go
//
// Unit tests for:
//   GET  /accounts/{id}/tokens  (handleListAccountTokens)
//   POST /accounts/tokens/revoke (handleRevokeAccountTokenHTTP)
//
// Design note: both handlers rely on patQueriesFn() / s.queries — we inject a
// fake via patQueriesOverride (for list) and a minimal stub for revoke. The list
// handler goes through huma's typed path, so tests call the handler directly and
// build a minimal admin context. The revoke handler is raw HTTP.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
)

// ---------------------------------------------------------------------------
// fake patQueries for the list handler (extends fakePATQ from me_tokens tests
// but declared here independently to avoid cross-file dependency).
// ---------------------------------------------------------------------------

// adminFakePATQ is a minimal patQueries implementation for admin-token tests.
type adminFakePATQ struct {
	rows []db.PersonalAccessToken
	// accountMissing makes GetAccountByID return pgx.ErrNoRows, exercising the
	// account-existence 404 guard on handleListAccountTokens.
	accountMissing bool
}

func (f *adminFakePATQ) GetAccountByID(_ context.Context, id int32) (db.Account, error) {
	if f.accountMissing {
		return db.Account{}, pgx.ErrNoRows
	}
	return db.Account{ID: id}, nil
}

func (f *adminFakePATQ) ListPATsByAccount(_ context.Context, _ int32) ([]db.PersonalAccessToken, error) {
	return f.rows, nil
}

func (f *adminFakePATQ) InsertPAT(_ context.Context, _ db.InsertPATParams) (db.PersonalAccessToken, error) {
	return db.PersonalAccessToken{}, nil
}

func (f *adminFakePATQ) RevokePAT(_ context.Context, _ db.RevokePATParams) (int64, error) {
	return 0, nil
}

func (f *adminFakePATQ) ListAuthorizedForwardAuthAppsForAccount(_ context.Context, _ pgtype.Int4) ([]db.ListAuthorizedForwardAuthAppsForAccountRow, error) {
	return nil, nil
}

// The revoke handler calls s.queries.RevokePATByID directly (concrete *db.Queries,
// not an interface), so the not-found / successful-revoke revoke paths require a
// live DB and are covered by the end-to-end smoke tests. The unit tests here cover
// the request-parsing guard paths (bad-request) which return before any DB call,
// plus the list handler's account-existence 404 guard via the patQueries seam.

// ---------------------------------------------------------------------------
// handleListAccountTokens — mapping invariants
// ---------------------------------------------------------------------------

// TestHandleListAccountTokens_MapsRowsToViews verifies that patView is applied
// correctly: AllApps and AppGrants are reflected, no token secret is present.
func TestHandleListAccountTokens_MapsRowsToViews(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	rows := []db.PersonalAccessToken{
		{
			ID:        1,
			AccountID: 42,
			Name:      "ci-token",
			TokenHint: "abc...xyz",
			AllApps:   false,
			AppGrants: []byte(`{"svc":["repo:read"]}`),
			CreatedAt: pgtype.Timestamptz{Time: now, Valid: true},
		},
		{
			ID:        2,
			AccountID: 42,
			Name:      "all-apps-token",
			TokenHint: "def...uvw",
			AllApps:   true,
			AppGrants: []byte(`{}`),
			CreatedAt: pgtype.Timestamptz{Time: now, Valid: true},
		},
	}

	fakeQ := &adminFakePATQ{rows: rows}
	s := &Server{patQueriesOverride: fakeQ, Audit: noopAuditWriter{}}

	out, err := s.handleListAccountTokens(context.Background(), &getAccountIn{ID: 42})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Body) != 2 {
		t.Fatalf("len(out.Body) = %d; want 2", len(out.Body))
	}

	// First token: app-grant, no allApps.
	v0 := out.Body[0]
	if v0.ID != 1 {
		t.Errorf("v0.ID = %d; want 1", v0.ID)
	}
	if v0.AllApps {
		t.Error("v0.AllApps: want false")
	}
	if scopes, ok := v0.AppGrants["svc"]; !ok || len(scopes) != 1 || scopes[0] != "repo:read" {
		t.Errorf("v0.AppGrants[svc] = %v; want [repo:read]", v0.AppGrants["svc"])
	}
	if v0.TokenHint == "" {
		t.Error("v0.TokenHint: want non-empty display aid")
	}

	// Second token: allApps=true.
	v1 := out.Body[1]
	if !v1.AllApps {
		t.Error("v1.AllApps: want true")
	}
	if len(v1.AppGrants) != 0 {
		t.Errorf("v1.AppGrants: want empty, got %v", v1.AppGrants)
	}
}

// TestHandleListAccountTokens_NoSecret verifies that the view never exposes the
// raw token (the token_hash is a []byte in the DB row; TokenHint is fine).
func TestHandleListAccountTokens_NoSecret(t *testing.T) {
	t.Parallel()

	secretHash := []byte("this-is-the-hash-and-must-not-appear")
	row := db.PersonalAccessToken{
		ID:        99,
		AccountID: 7,
		Name:      "secret-test",
		TokenHint: "tok...end",
		TokenHash: secretHash,
		AllApps:   true,
		AppGrants: []byte(`{}`),
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}

	fakeQ := &adminFakePATQ{rows: []db.PersonalAccessToken{row}}
	s := &Server{patQueriesOverride: fakeQ, Audit: noopAuditWriter{}}

	out, err := s.handleListAccountTokens(context.Background(), &getAccountIn{ID: 7})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b, _ := json.Marshal(out.Body)
	if strings.Contains(string(b), "this-is-the-hash-and-must-not-appear") {
		t.Error("response JSON must not contain the raw token hash")
	}
}

// TestHandleListAccountTokens_UnknownAccount404 verifies the account-existence
// guard: a garbage id (GetAccountByID → pgx.ErrNoRows) returns a 404 huma
// StatusError, not 200+empty. Mirrors the sibling GET /accounts/{id}/* handlers.
func TestHandleListAccountTokens_UnknownAccount404(t *testing.T) {
	t.Parallel()

	fakeQ := &adminFakePATQ{accountMissing: true}
	s := &Server{patQueriesOverride: fakeQ, Audit: noopAuditWriter{}}

	_, err := s.handleListAccountTokens(context.Background(), &getAccountIn{ID: 999})
	if err == nil {
		t.Fatal("expected a 404 error for an unknown account id, got nil")
	}
	se, ok := err.(huma.StatusError)
	if !ok || se.GetStatus() != http.StatusNotFound {
		t.Fatalf("want 404 huma StatusError (account_not_found), got %T %v", err, err)
	}
}

// ---------------------------------------------------------------------------
// handleRevokeAccountTokenHTTP — bad-request guard paths
// ---------------------------------------------------------------------------

// buildRevokeTokenRequest builds a POST request to /accounts/tokens/revoke.
func buildRevokeTokenRequest(bodyJSON string) *http.Request {
	var bodyReader *bytes.Reader
	if bodyJSON == "" {
		bodyReader = bytes.NewReader(nil)
	} else {
		bodyReader = bytes.NewReader([]byte(bodyJSON))
	}
	req := httptest.NewRequest("POST", "/api/prohibitorum/accounts/tokens/revoke", bodyReader)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// TestHandleRevokeAccountToken_ZeroID verifies that id=0 (or missing) is
// rejected as bad_request before any DB call.
func TestHandleRevokeAccountToken_ZeroID(t *testing.T) {
	t.Parallel()

	s := &Server{Audit: noopAuditWriter{}} // queries nil; handler must not reach it
	rr := httptest.NewRecorder()
	req := buildRevokeTokenRequest(`{"id":0}`)
	s.handleRevokeAccountTokenHTTP(rr, req)

	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("status = %d; want 4xx for id=0", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "bad_request") {
		t.Errorf("body = %q; want bad_request", rr.Body.String())
	}
}

// TestHandleRevokeAccountToken_MalformedJSON verifies that non-JSON body is
// rejected as bad_request before any DB call.
func TestHandleRevokeAccountToken_MalformedJSON(t *testing.T) {
	t.Parallel()

	s := &Server{Audit: noopAuditWriter{}}
	rr := httptest.NewRecorder()
	req := buildRevokeTokenRequest("this is not json")
	s.handleRevokeAccountTokenHTTP(rr, req)

	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("status = %d; want 4xx for malformed JSON", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "bad_request") {
		t.Errorf("body = %q; want bad_request", rr.Body.String())
	}
}

// TestHandleRevokeAccountToken_EmptyBody verifies that an empty body is
// rejected as bad_request.
func TestHandleRevokeAccountToken_EmptyBody(t *testing.T) {
	t.Parallel()

	s := &Server{Audit: noopAuditWriter{}}
	rr := httptest.NewRecorder()
	req := buildRevokeTokenRequest("")
	s.handleRevokeAccountTokenHTTP(rr, req)

	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("status = %d; want 4xx for empty body", rr.Code)
	}
}

// TestHandleRevokeAccountToken_UnknownID and successful-revoke paths require
// s.queries.RevokePATByID (concrete *db.Queries, not an interface) and therefore
// a live database. Those paths are covered by the end-to-end smoke tests.

// ---------------------------------------------------------------------------
// Response shape
// ---------------------------------------------------------------------------

// TestHandleListAccountTokens_EmptySlice verifies that an account with no PATs
// returns an empty slice (not nil), which serializes to [] not null.
func TestHandleListAccountTokens_EmptySlice(t *testing.T) {
	t.Parallel()

	fakeQ := &adminFakePATQ{rows: nil}
	s := &Server{patQueriesOverride: fakeQ, Audit: noopAuditWriter{}}

	out, err := s.handleListAccountTokens(context.Background(), &getAccountIn{ID: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Body == nil {
		t.Error("Body: want non-nil empty slice, got nil (would serialize as JSON null)")
	}
	if len(out.Body) != 0 {
		t.Errorf("Body: want 0 items, got %d", len(out.Body))
	}
}

// Ensure authn package is referenced (writeAuthErr uses it transitively).
var _ = authn.ErrBadRequest()
