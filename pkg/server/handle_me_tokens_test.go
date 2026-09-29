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
	"net/http"
	"net/http/httptest"

	"context"
	"github.com/danielgtaylor/huma/v2"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/appaccess"
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
	// apps holds (pat, client) pairs written by InsertPATApp; names maps
	// client_id to the display name the list join would return.
	apps  []db.InsertPATAppParams
	names map[string]string
}

func (f *fakePATQ) InsertPATApp(_ context.Context, arg db.InsertPATAppParams) error {
	f.apps = append(f.apps, arg)
	return nil
}

func (f *fakePATQ) ListPATAppsByPATIDs(_ context.Context, ids []int32) ([]db.ListPATAppsByPATIDsRow, error) {
	var out []db.ListPATAppsByPATIDsRow
	for _, a := range f.apps {
		for _, id := range ids {
			if a.PatID == id {
				name := f.names[a.ClientID]
				if name == "" {
					name = a.ClientID
				}
				out = append(out, db.ListPATAppsByPATIDsRow{PatID: a.PatID, ClientID: a.ClientID, DisplayName: name})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].DisplayName < out[j].DisplayName })
	return out, nil
}

func (f *fakePATQ) InsertPAT(_ context.Context, arg db.InsertPATParams) (db.PersonalAccessToken, error) {
	f.nextID++
	row := db.PersonalAccessToken{
		ID:        f.nextID,
		AccountID: arg.AccountID,
		Name:      arg.Name,
		TokenHash: arg.TokenHash,
		TokenHint: arg.TokenHint,
		Access:    arg.Access,
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		ExpiresAt: arg.ExpiresAt,
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
		appLister: &fakeAppLister{apps: []appaccess.AppSummary{
			{
				Ref:         appaccess.AppRef{Kind: appaccess.KindForwardAuth, OIDCClientID: "svc"},
				DisplayName: "Service",
			},
			{
				Ref:         appaccess.AppRef{Kind: appaccess.KindForwardAuth, OIDCClientID: "wiki"},
				DisplayName: "Wiki",
			},
			{
				Ref:         appaccess.AppRef{Kind: appaccess.KindOIDC, OIDCClientID: "oidc-only"},
				DisplayName: "OIDC only",
			},
		}},
		Audit: audit.NewWriter(q),
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
	in.Body.Access = "all_apps"

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
	createIn.Body.Access = "all_apps"
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
	createIn.Body.Access = "all_apps"
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
	createIn.Body.Access = "all_apps"
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
	createIn.Body.Access = "all_apps"
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

// ---------------------------------------------------------------------------
// Access levels and application selection
// ---------------------------------------------------------------------------

func createWith(s *Server, access string, clientIDs []string) (*createMyTokenOut, error) {
	in := &createMyTokenIn{}
	in.Body.Name = "tok"
	in.Body.Access = access
	in.Body.AppClientIDs = clientIDs
	return s.handleCreateMyToken(patCtx(1), in)
}

// Every level is created with the right stored access; only selected_apps
// writes application rows.
func TestHandleCreateMyToken_EachAccessLevel(t *testing.T) {
	t.Parallel()

	for _, access := range []string{"all_apps", "full", "sudo"} {
		q := &fakePATQ{}
		out, err := createWith(newPATServer(q), access, nil)
		if err != nil {
			t.Fatalf("%s: %v", access, err)
		}
		if out.Body.PAT.Access != access || len(out.Body.PAT.Apps) != 0 || out.Body.PAT.Apps == nil {
			t.Errorf("%s: view = %+v (apps must be an empty, non-nil list)", access, out.Body.PAT)
		}
		if len(q.rows) != 1 || q.rows[0].Access != access || len(q.apps) != 0 {
			t.Errorf("%s: rows=%+v apps=%+v", access, q.rows, q.apps)
		}
	}

	q := &fakePATQ{names: map[string]string{"svc": "Service", "wiki": "Wiki"}}
	out, err := createWith(newPATServer(q), "selected_apps", []string{"wiki", "svc"})
	if err != nil {
		t.Fatalf("selected_apps: %v", err)
	}
	apps := out.Body.PAT.Apps
	if out.Body.PAT.Access != "selected_apps" || len(apps) != 2 || apps[0].DisplayName != "Service" || apps[1].ClientID != "wiki" {
		t.Errorf("selected_apps view = %+v (apps sorted by display name)", out.Body.PAT)
	}
	if len(q.apps) != 2 {
		t.Errorf("app rows = %+v", q.apps)
	}
}

func TestHandleCreateMyToken_RejectsBadSelections(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		access    string
		clientIDs []string
	}{
		{"selected without apps", "selected_apps", nil},
		{"selected with empty list", "selected_apps", []string{}},
		{"duplicate app", "selected_apps", []string{"svc", "svc"}},
		{"app not offered", "selected_apps", []string{"not-authorized"}},
		{"non forward-auth app", "selected_apps", []string{"oidc-only"}},
		{"one good one bad", "selected_apps", []string{"svc", "nope"}},
		{"all_apps with apps", "all_apps", []string{"svc"}},
		{"full with apps", "full", []string{"svc"}},
		{"sudo with apps", "sudo", []string{"svc"}},
		{"all_apps with empty list", "all_apps", []string{}},
		{"sudo with empty list", "sudo", []string{}},
		{"unknown access", "root", nil},
		{"empty access", "", nil},
		{"wrong case", "Full", nil},
	}
	for _, tc := range cases {
		q := &fakePATQ{}
		_, err := createWith(newPATServer(q), tc.access, tc.clientIDs)
		if err == nil {
			t.Errorf("%s: expected error", tc.name)
			continue
		}
		if code := codeFromErr(t, err); code != "bad_request" {
			t.Errorf("%s: code = %s, want bad_request", tc.name, code)
		}
		if len(q.rows) != 0 || len(q.apps) != 0 {
			t.Errorf("%s: wrote rows=%d apps=%d", tc.name, len(q.rows), len(q.apps))
		}
	}
}

// Access is evaluated live when the PAT is created. An app that was available
// when the picker loaded but is now denied must be rejected before insertion.
func TestHandleCreateMyToken_NowDeniedApp(t *testing.T) {
	t.Parallel()

	q := &fakePATQ{}
	s := newPATServer(q)
	s.appLister = &fakeAppLister{apps: []appaccess.AppSummary{}}
	_, err := createWith(s, "selected_apps", []string{"svc"})
	if err == nil {
		t.Fatal("expected error for now-denied app, got nil")
	}
	if code := codeFromErr(t, err); code != "bad_request" {
		t.Errorf("code: want bad_request, got %s", code)
	}
	if len(q.rows) != 0 {
		t.Errorf("InsertPAT must not be called for denied app; got %d row(s)", len(q.rows))
	}
}

// The list view carries each token's apps, ordered by display name.
func TestHandleListMyTokens_IncludesApps(t *testing.T) {
	t.Parallel()

	q := &fakePATQ{names: map[string]string{"svc": "Service", "wiki": "Wiki"}}
	s := newPATServer(q)
	if _, err := createWith(s, "selected_apps", []string{"wiki", "svc"}); err != nil {
		t.Fatal(err)
	}
	if _, err := createWith(s, "full", nil); err != nil {
		t.Fatal(err)
	}
	out, err := s.handleListMyTokens(patCtx(1), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Body) != 2 {
		t.Fatalf("len = %d", len(out.Body))
	}
	for _, v := range out.Body {
		switch v.Access {
		case "selected_apps":
			if len(v.Apps) != 2 || v.Apps[0].ClientID != "svc" || v.Apps[1].ClientID != "wiki" {
				t.Errorf("selected apps = %+v", v.Apps)
			}
		case "full":
			if v.Apps == nil || len(v.Apps) != 0 {
				t.Errorf("full apps = %#v, want empty non-nil", v.Apps)
			}
		default:
			t.Errorf("unexpected access %q", v.Access)
		}
	}
}

// TestHandleListMyForwardAuthApps — returns the caller's authorized FA apps
// for the create picker.
func TestHandleListMyForwardAuthApps(t *testing.T) {
	t.Parallel()

	q := &fakePATQ{}
	s := newPATServer(q)
	ctx := patCtx(1)

	out, err := s.handleListMyForwardAuthApps(ctx, nil)
	if err != nil {
		t.Fatalf("handleListMyForwardAuthApps: %v", err)
	}
	if len(out.Body) != 2 {
		t.Fatalf("app count: want 2, got %d", len(out.Body))
	}
	if app := out.Body[0]; app.ClientID != "svc" || app.DisplayName != "Service" {
		t.Errorf("app identity: got %q / %q", app.ClientID, app.DisplayName)
	}
}

// The wire schema is closed: the removed allApps/appGrants fields, a missing
// access, and an access outside the enum are all schema failures (422), and
// nothing reaches the handler.
func TestCreateMyTokenRoute_SchemaRejectsLegacyAndInvalidBodies(t *testing.T) {
	router, _ := realAdminOnlyRouter(t)
	sess := adminSession(time.Now().Add(time.Hour)) // fresh sudo
	cases := map[string]struct{ body, location string }{
		"allApps field":    {`{"name":"x","access":"full","allApps":true}`, ""},
		"appGrants field":  {`{"name":"x","access":"full","appGrants":{"svc":["r"]}}`, ""},
		"legacy body only": {`{"name":"x","allApps":true}`, ""},
		"missing access":   {`{"name":"x"}`, "required property access"},
		"unknown access":   {`{"name":"x","access":"root"}`, "body.access"},
		"wrong case":       {`{"name":"x","access":"FULL"}`, "body.access"},
	}
	for name, tc := range cases {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, reqWithSession("POST", "/api/prohibitorum/me/tokens", tc.body, "", sess))
		if rr.Code != http.StatusUnprocessableEntity || !strings.Contains(rr.Body.String(), "validation_failed") {
			t.Errorf("%s: status=%d body=%s; want 422 validation_failed", name, rr.Code, rr.Body.String())
			continue
		}
		if tc.location != "" && !strings.Contains(rr.Body.String(), tc.location) {
			t.Errorf("%s: body=%s; want location %s", name, rr.Body.String(), tc.location)
		}
	}
}

// The OpenAPI document advertises the PAT header scheme, and only on routes a
// PAT may call.
func TestOpenAPISecuritySchemes_PAT(t *testing.T) {
	api := NewHuma()
	doc := api.OpenAPI()
	scheme := doc.Components.SecuritySchemes["prohibitorumPAT"]
	if scheme == nil || scheme.Type != "apiKey" || scheme.In != "header" || scheme.Name != "X-Prohibitorum-PAT" {
		t.Fatalf("prohibitorumPAT scheme = %+v", scheme)
	}
	has := func(op *huma.Operation, name string) bool {
		for _, req := range op.Security {
			if _, ok := req[name]; ok {
				return true
			}
		}
		return false
	}
	get := doc.Paths["/api/prohibitorum/me/tokens"].Post
	if get == nil || !has(get, "prohibitorumSession") || !has(get, "prohibitorumPAT") {
		t.Errorf("createMyToken security = %+v, want session and PAT", get)
	}
	if pub := doc.Paths["/api/prohibitorum/auth/status"].Get; pub == nil || len(pub.Security) != 0 {
		t.Errorf("public op security = %+v, want none", pub)
	}
}
