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
)

func TestPATAccessLevelsMigrationPostgres(t *testing.T) {
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
	schema := "pat_access_" + hex.EncodeToString(nonce[:])
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE") //nolint:errcheck
	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	conn, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	goose.SetBaseFS(embedMigrations)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, ".", 31); err != nil {
		t.Fatal(err)
	}
	// Later migrations use extension operators installed in public.
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+quoted+", public"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, ".", 43); err != nil {
		t.Fatal(err)
	}
	mustExec := func(statement string) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	mustExec("INSERT INTO account (username, display_name, webauthn_user_handle, role) VALUES ('owner', 'Owner', decode('01','hex'), 'user')")
	mustExec("INSERT INTO oidc_client (client_id, display_name, redirect_uris, forward_auth_enabled, forward_auth_scopes) VALUES ('fa_a', 'A', ARRAY['https://a.test/cb'], true, '[\"read\"]'), ('fa_b', 'B', ARRAY['https://b.test/cb'], true, '[]')")
	mustExec(`INSERT INTO personal_access_token (account_id, name, token_hash, token_hint, all_apps, app_grants)
SELECT id, 'all', decode('aa','hex'), 'h', true, '{}' FROM account WHERE username = 'owner'`)
	mustExec(`INSERT INTO personal_access_token (account_id, name, token_hash, token_hint, all_apps, app_grants)
SELECT id, 'some', decode('bb','hex'), 'h', false, '{"fa_a":["read"],"fa_gone":["x"]}' FROM account WHERE username = 'owner'`)

	if err := goose.UpTo(conn, ".", 44); err != nil {
		t.Fatal(err)
	}
	access := func(name string) string {
		t.Helper()
		var v string
		if err := conn.QueryRowContext(ctx, "SELECT access FROM personal_access_token WHERE name = $1", name).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	if got := access("all"); got != "all_apps" {
		t.Fatalf("all: access = %q", got)
	}
	if got := access("some"); got != "selected_apps" {
		t.Fatalf("some: access = %q", got)
	}
	var apps string
	if err := conn.QueryRowContext(ctx, "SELECT COALESCE(string_agg(a.client_id, ',' ORDER BY a.client_id), '') FROM personal_access_token_app a JOIN personal_access_token p ON p.id = a.pat_id WHERE p.name = 'some'").Scan(&apps); err != nil {
		t.Fatal(err)
	}
	if apps != "fa_a" {
		t.Fatalf("some apps = %q, want only the surviving application", apps)
	}
	var n int
	if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM personal_access_token_app a JOIN personal_access_token p ON p.id = a.pat_id WHERE p.name = 'all'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("all_apps token has %d app rows, err %v", n, err)
	}
	if _, err := conn.ExecContext(ctx, "UPDATE personal_access_token SET access = 'bogus'"); err == nil {
		t.Fatal("access check constraint accepted an unknown level")
	}
	mustExec("UPDATE personal_access_token SET access = 'sudo' WHERE name = 'all'")

	if err := goose.DownTo(conn, ".", 43); err != nil {
		t.Fatal(err)
	}
	var allApps bool
	var grants string
	if err := conn.QueryRowContext(ctx, "SELECT all_apps, app_grants::text FROM personal_access_token WHERE name = 'all'").Scan(&allApps, &grants); err != nil || !allApps {
		t.Fatalf("down all: all_apps=%v err=%v", allApps, err)
	}
	if err := conn.QueryRowContext(ctx, "SELECT all_apps, app_grants::text FROM personal_access_token WHERE name = 'some'").Scan(&allApps, &grants); err != nil {
		t.Fatal(err)
	}
	if allApps || grants != `{"fa_a": []}` {
		t.Fatalf("down some: all_apps=%v grants=%s", allApps, grants)
	}
	var scopes string
	if err := conn.QueryRowContext(ctx, "SELECT forward_auth_scopes::text FROM oidc_client WHERE client_id = 'fa_a'").Scan(&scopes); err != nil || scopes != "[]" {
		t.Fatalf("down scopes = %q err=%v", scopes, err)
	}
}
