package migrations

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"

	dbgen "prohibitorum/pkg/db"
)

func TestApplicationListsFilterAssignmentsBeforePaginationPostgres(t *testing.T) {
	baseURL := os.Getenv("PROHIBITORUM_TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("PROHIBITORUM_TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	schema := "application_lists_" + hex.EncodeToString(nonce[:])
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "DROP SCHEMA "+quotedSchema+" CASCADE") //nolint:errcheck

	schemaURL, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := schemaURL.Query()
	query.Set("search_path", schema)
	schemaURL.RawQuery = query.Encode()

	goose.SetBaseFS(embedMigrations)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	migrationConn, err := sql.Open("pgx", schemaURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer migrationConn.Close()
	migrationConn.SetMaxOpenConns(1)
	if err := goose.UpTo(migrationConn, ".", 31); err != nil {
		t.Fatal(err)
	}
	if _, err := migrationConn.ExecContext(ctx, "SET search_path TO "+quotedSchema+", public"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(migrationConn, "."); err != nil {
		t.Fatal(err)
	}

	queryConn, err := pgx.Connect(ctx, schemaURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer queryConn.Close(ctx) //nolint:errcheck
	if _, err := queryConn.Exec(ctx, "SET search_path TO "+quotedSchema+", public"); err != nil {
		t.Fatal(err)
	}

	var assignedAccountID, otherAccountID, adminAccountID int32
	for _, account := range []struct {
		username string
		handle   string
		role     string
		id       *int32
	}{
		{username: "assigned", handle: "01", role: "user", id: &assignedAccountID},
		{username: "other", handle: "02", role: "user", id: &otherAccountID},
		{username: "admin", handle: "03", role: "admin", id: &adminAccountID},
	} {
		if err := queryConn.QueryRow(ctx, `
			INSERT INTO account (username, display_name, webauthn_user_handle, role)
			VALUES ($1, $1, decode($2, 'hex'), $3)
			RETURNING id`, account.username, account.handle, account.role).Scan(account.id); err != nil {
			t.Fatal(err)
		}
	}

	for _, app := range []struct {
		id          string
		forwardAuth bool
		createdAt   string
	}{
		{id: "oidc-hidden-new", createdAt: "2026-09-18T10:00:00Z"},
		{id: "oidc-hidden-middle", createdAt: "2026-09-18T09:00:00Z"},
		{id: "oidc-assigned-later", createdAt: "2026-09-18T03:00:00Z"},
		{id: "oidc-assigned-old", createdAt: "2026-09-18T02:00:00Z"},
		{id: "fa-hidden-new", forwardAuth: true, createdAt: "2026-09-18T11:00:00Z"},
		{id: "fa-assigned", forwardAuth: true, createdAt: "2026-09-18T01:00:00Z"},
	} {
		if _, err := queryConn.Exec(ctx, `
			INSERT INTO oidc_client (client_id, display_name, redirect_uris, forward_auth_enabled, created_at)
			VALUES ($1, $1, ARRAY['https://example.test/callback'], $2, $3::timestamptz)`,
			app.id, app.forwardAuth, app.createdAt); err != nil {
			t.Fatal(err)
		}
	}
	for _, clientID := range []string{"oidc-assigned-later", "oidc-assigned-old", "fa-assigned"} {
		if _, err := queryConn.Exec(ctx, `
			INSERT INTO oidc_client_manager (client_id, account_id, created_by)
			VALUES ($1, $2, $3)`, clientID, assignedAccountID, adminAccountID); err != nil {
			t.Fatal(err)
		}
	}

	samlIDs := make(map[string]int64)
	for _, app := range []struct {
		name      string
		createdAt string
	}{
		{name: "saml-hidden-new", createdAt: "2026-09-18T10:00:00Z"},
		{name: "saml-hidden-middle", createdAt: "2026-09-18T09:00:00Z"},
		{name: "saml-assigned-later", createdAt: "2026-09-18T03:00:00Z"},
		{name: "saml-assigned-old", createdAt: "2026-09-18T02:00:00Z"},
	} {
		var id int64
		if err := queryConn.QueryRow(ctx, `
			INSERT INTO saml_sp (entity_id, display_name, created_at)
			VALUES ('https://example.test/' || $1, $1, $2::timestamptz)
			RETURNING id`, app.name, app.createdAt).Scan(&id); err != nil {
			t.Fatal(err)
		}
		samlIDs[app.name] = id
	}
	for _, name := range []string{"saml-assigned-later", "saml-assigned-old"} {
		if _, err := queryConn.Exec(ctx, `
			INSERT INTO saml_sp_manager (saml_sp_id, account_id, created_by)
			VALUES ($1, $2, $3)`, samlIDs[name], assignedAccountID, adminAccountID); err != nil {
			t.Fatal(err)
		}
	}

	queries := dbgen.New(queryConn)
	oidcRows, err := queries.ListNonForwardAuthOIDCClients(ctx, dbgen.ListNonForwardAuthOIDCClientsParams{
		AccountID: assignedAccountID,
		Limit:     2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := oidcClientIDs(oidcRows); !equalStrings(got, []string{"oidc-assigned-later", "oidc-assigned-old"}) {
		t.Fatalf("assigned OIDC clients = %v", got)
	}

	forwardAuthRows, err := queries.ListForwardAuthClients(ctx, dbgen.ListForwardAuthClientsParams{
		AccountID: assignedAccountID,
		Limit:     2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(forwardAuthRows) != 1 || forwardAuthRows[0].ClientID != "fa-assigned" {
		t.Fatalf("assigned forward-auth clients = %v", forwardAuthClientIDs(forwardAuthRows))
	}

	samlRows, err := queries.ListSAMLSPs(ctx, dbgen.ListSAMLSPsParams{
		AccountID: assignedAccountID,
		Limit:     2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := samlDisplayNames(samlRows); !equalStrings(got, []string{"saml-assigned-later", "saml-assigned-old"}) {
		t.Fatalf("assigned SAML applications = %v", got)
	}

	emptyRows, err := queries.ListNonForwardAuthOIDCClients(ctx, dbgen.ListNonForwardAuthOIDCClientsParams{
		AccountID: otherAccountID,
		Limit:     10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(emptyRows) != 0 {
		t.Fatalf("unassigned account received OIDC clients: %v", oidcClientIDs(emptyRows))
	}

	allOIDCRows, err := queries.ListNonForwardAuthOIDCClients(ctx, dbgen.ListNonForwardAuthOIDCClientsParams{
		IncludeAll: true,
		Limit:      10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(allOIDCRows) != 4 {
		t.Fatalf("admin OIDC client count = %d, want 4", len(allOIDCRows))
	}
	allForwardAuthRows, err := queries.ListForwardAuthClients(ctx, dbgen.ListForwardAuthClientsParams{
		IncludeAll: true,
		Limit:      10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(allForwardAuthRows) != 2 {
		t.Fatalf("admin forward-auth client count = %d, want 2", len(allForwardAuthRows))
	}
	allSAMLRows, err := queries.ListSAMLSPs(ctx, dbgen.ListSAMLSPsParams{
		IncludeAll: true,
		Limit:      10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(allSAMLRows) != 4 {
		t.Fatalf("admin SAML application count = %d, want 4", len(allSAMLRows))
	}
}

func oidcClientIDs(rows []dbgen.ListNonForwardAuthOIDCClientsRow) []string {
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.ClientID
	}
	return ids
}

func forwardAuthClientIDs(rows []dbgen.ListForwardAuthClientsRow) []string {
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.ClientID
	}
	return ids
}

func samlDisplayNames(rows []dbgen.SamlSp) []string {
	names := make([]string, len(rows))
	for i, row := range rows {
		names[i] = row.DisplayName
	}
	return names
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
