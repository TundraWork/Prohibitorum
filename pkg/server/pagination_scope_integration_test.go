package server

// Package server — pagination_scope_integration_test.go
//
// Regression coverage for the PHB-71 pagination fix. `pageInput` used to be
// unexported, and huma does not descend into an unexported embedded struct when
// it builds an operation's query parameters. The two fields therefore never
// appeared in the OpenAPI document, generated clients had nothing to send, and
// every list was stuck on the first page of 50 no matter what the caller asked
// for. The struct is exported now; these tests go through the real chi router and
// the real Huma operations against a live database and assert that `limit` and
// `cursor` do what they say.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/federation"
	federationoidc "prohibitorum/pkg/federation/providers/oidc"
	federationsteam "prohibitorum/pkg/federation/providers/steam"
	federationvrchat "prohibitorum/pkg/federation/providers/vrchat"
)

// livePaginationServer holds the scaffolding a paged-list test needs: the real
// router with the real operations registered, a query surface pointed at the
// caller's transaction, and an admin session to drive it.
type livePaginationServer struct {
	t      *testing.T
	router *chi.Mux
	sess   *authn.Session
	server *Server
}

func newLivePaginationServer(t *testing.T, q db.DBTX) *livePaginationServer {
	t.Helper()
	router := chi.NewMux()
	s := &Server{
		router: router,
		api:    humachi.New(router, humaConfig()),
		config: &configx.Config{PublicOrigins: []string{"http://localhost:8080"}},
	}
	// Handlers that open their own transaction (the provider delete) reach for
	// dbPool; supply it whenever the caller handed us a pool rather than a
	// transaction.
	if pool, ok := q.(*pgxpool.Pool); ok {
		s.dbPool = pool
	}
	// The caller passes the transaction its fixtures were written in, so the
	// handlers read uncommitted rows and the test leaves nothing behind.
	s.queries = db.New(q)
	// Without a codec the handlers decode nothing and encode no next cursor, so
	// a paged list would silently look like a single page.
	s.cursorCodec = testCodec()
	// The identity-provider list projects each row through its protocol
	// definition, so the registry has to be populated even though these tests
	// never talk to an upstream. Definitions alone are enough for a view.
	registry := federation.NewRegistry()
	for _, definition := range []federation.Definition{
		federationoidc.Definition{},
		federationsteam.Definition{},
		federationvrchat.Definition{},
	} {
		if err := registry.RegisterDefinition(definition); err != nil {
			t.Fatalf("register definition: %v", err)
		}
	}
	s.federationRegistry = registry
	s.registerOperations()
	return &livePaginationServer{t: t, router: router, sess: adminSession(time.Now()), server: s}
}

// get runs one admin GET through the real router and decodes the JSON body.
func (p *livePaginationServer) get(path string, out any) *httptest.ResponseRecorder {
	p.t.Helper()
	rr := httptest.NewRecorder()
	req := reqWithSession(http.MethodGet, path, "", "", p.sess)
	p.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		p.t.Fatalf("GET %s: status %d, body %s", path, rr.Code, rr.Body.String())
	}
	if out != nil {
		if err := json.Unmarshal(rr.Body.Bytes(), out); err != nil {
			p.t.Fatalf("GET %s: decode: %v (body %s)", path, err, rr.Body.String())
		}
	}
	return rr
}

// TestListPaginationLimitAndCursorPostgres drives two admin lists over HTTP with
// an explicit limit and asserts the three things the bug broke: the page is the
// requested size, a next cursor comes back while more rows exist, and the next
// page returns different rows rather than repeating the first.
//
// Rows are inserted with timestamps far in the future so they sort to the head of
// the created_at DESC list and stay ahead of whatever else the database holds.
func TestListPaginationLimitAndCursorPostgres(t *testing.T) {
	databaseURL := os.Getenv("PROHIBITORUM_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PROHIBITORUM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	prefix := "page_" + hex.EncodeToString(nonce[:])

	// Distinct, descending timestamps so the keyset order is unambiguous.
	base := time.Date(2099, 12, 31, 0, 0, 0, 0, time.UTC)
	const rowCount = 5
	for i := 0; i < rowCount; i++ {
		at := base.Add(-time.Duration(i) * time.Minute)
		if _, err := tx.Exec(ctx, `
			INSERT INTO account (username, display_name, webauthn_user_handle, email, created_at)
			VALUES ($1, $2, $3, $4, $5)`,
			prefix+"_"+hex.EncodeToString([]byte{byte('a' + i)}), "Paged", append(nonce[:], byte(i)), prefix+"@page.test", at); err != nil {
			t.Fatalf("insert account %d: %v", i, err)
		}
	}
	for i := 0; i < rowCount; i++ {
		at := base.Add(-time.Duration(i) * time.Minute)
		// The secret tuple is all-or-nothing: upstream_idp's CHECK requires
		// secret_enc, secret_nonce and key_version to be set together, so a
		// minimal row needs placeholder bytes for all three.
		if _, err := tx.Exec(ctx, `
			INSERT INTO upstream_idp (slug, display_name, mode, protocol, created_at,
				secret_enc, secret_nonce, key_version, secret_status)
			VALUES ($1, $2, 'invite_only', 'steam', $3, $4, $5, 1, 'configured')`,
			prefix+"-"+hex.EncodeToString([]byte{byte('a' + i)}), "Paged", at,
			[]byte{0x01, byte(i)}, nonce[:]); err != nil {
			t.Fatalf("insert provider %d: %v", i, err)
		}
	}

	// The queries run inside the same transaction the rows were written in, so
	// hand the handler a db.Queries bound to this tx rather than the pool.
	s := newLivePaginationServer(t, tx)

	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "accounts", path: "/api/prohibitorum/accounts"},
		{name: "identity providers", path: "/api/prohibitorum/identity-providers"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var first contract.Page[map[string]any]
			s.get(tc.path+"?limit=2", &first)

			if len(first.Items) != 2 {
				t.Fatalf("limit=2 returned %d items, want 2", len(first.Items))
			}
			if first.NextCursor == "" {
				t.Fatalf("limit=2 on %d rows returned no nextCursor", rowCount)
			}

			// The cursor must be opaque to us but meaningful to the server: page
			// two continues where page one stopped.
			var second contract.Page[map[string]any]
			s.get(tc.path+"?limit=2&cursor="+first.NextCursor, &second)
			if len(second.Items) != 2 {
				t.Fatalf("page 2 returned %d items, want 2", len(second.Items))
			}

			seen := map[string]bool{}
			for _, item := range first.Items {
				seen[itemKey(item)] = true
			}
			for _, item := range second.Items {
				if seen[itemKey(item)] {
					t.Errorf("page 2 repeated a row from page 1: %v", itemKey(item))
				}
			}

			// A limit larger than the result set still pages correctly and terms
			// with an empty cursor once the rows run out.
			var all contract.Page[map[string]any]
			s.get(tc.path+"?limit=100", &all)
			if len(all.Items) < rowCount {
				t.Errorf("limit=100 returned %d items, want at least the %d inserted", len(all.Items), rowCount)
			}
		})
	}
}

// TestListPaginationClampsLimitPostgres checks the clamp still applies now that
// the field is caller-reachable: a non-positive limit falls back to the default
// and an oversized one is capped rather than asking the database for everything.
func TestListPaginationClampsLimitPostgres(t *testing.T) {
	databaseURL := os.Getenv("PROHIBITORUM_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PROHIBITORUM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	s := newLivePaginationServer(t, tx)

	for _, limit := range []string{"0", "-5", "1", "1000000"} {
		var page contract.Page[map[string]any]
		s.get("/api/prohibitorum/accounts?limit="+limit, &page)
		if len(page.Items) > 100 {
			t.Errorf("limit=%s returned %d items, want at most the 100 cap", limit, len(page.Items))
		}
	}
}

// TestListPaginationRejectsForeignCursorPostgres confirms a cursor from another
// collection is refused rather than silently ignored, so a caller cannot page one
// list with another list's cursor.
func TestListPaginationRejectsForeignCursorPostgres(t *testing.T) {
	databaseURL := os.Getenv("PROHIBITORUM_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PROHIBITORUM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	s := newLivePaginationServer(t, tx)

	rr := httptest.NewRecorder()
	req := reqWithSession(http.MethodGet, "/api/prohibitorum/accounts?cursor=tampered-not-a-cursor", "", "", s.sess)
	s.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an undecodable cursor; body %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "pagination_cursor_invalid") {
		t.Errorf("body = %s, want pagination_cursor_invalid", rr.Body.String())
	}
}

// itemKey picks the stable identity out of a decoded list item without caring
// which view shape it is: every admin list row carries either `username` (an
// account) or `slug` (a provider).
func itemKey(item map[string]any) string {
	for _, key := range []string{"username", "slug", "clientId", "id"} {
		if v, ok := item[key]; ok {
			return key + "=" + strings.TrimSpace(toString(v))
		}
	}
	return "unknown"
}

func toString(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	default:
		return ""
	}
}
