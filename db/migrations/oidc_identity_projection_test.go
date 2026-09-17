package migrations

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
)

func TestOIDCIdentityProjectionMigrationPostgres(t *testing.T) {
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
	schema := "identity_projection_" + hex.EncodeToString(nonce[:])
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
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+quoted+", public"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, ".", 41); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO oidc_client (client_id, display_name, redirect_uris, forward_auth_enabled) VALUES ('oidc', 'OIDC', ARRAY['https://oidc.test/cb'], false), ('fa', 'FA', ARRAY['https://fa.test/cb'], true)`); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, ".", 42); err != nil {
		t.Fatal(err)
	}

	for clientID, want := range map[string]string{"oidc": "sub", "fa": "username"} {
		var source string
		var aliases []byte
		if err := conn.QueryRowContext(ctx, `SELECT principal_source, claim_aliases FROM oidc_client WHERE client_id=$1`, clientID).Scan(&source, &aliases); err != nil {
			t.Fatal(err)
		}
		if source != want || string(aliases) != "{}" {
			t.Fatalf("%s defaults = %q, %s", clientID, source, aliases)
		}
	}
	if _, err := conn.ExecContext(ctx, `UPDATE oidc_client SET principal_source='verified_email', claim_aliases='{"handle":"preferred_username"}'::jsonb WHERE client_id='oidc'`); err != nil {
		t.Fatalf("valid round-trip update: %v", err)
	}
	var source string
	var aliases []byte
	if err := conn.QueryRowContext(ctx, `SELECT principal_source, claim_aliases FROM oidc_client WHERE client_id='oidc'`).Scan(&source, &aliases); err != nil {
		t.Fatal(err)
	}
	if source != "verified_email" || string(aliases) != `{"handle": "preferred_username"}` {
		t.Fatalf("round trip = %q, %s", source, aliases)
	}
	for name, statement := range map[string]string{"source": `UPDATE oidc_client SET principal_source='invalid' WHERE client_id='oidc'`, "aliases": `UPDATE oidc_client SET claim_aliases='[]'::jsonb WHERE client_id='oidc'`} {
		_, err := conn.ExecContext(ctx, statement)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("%s constraint error = %v", name, err)
		}
	}
	if err := goose.DownTo(conn, ".", 41); err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"principal_source", "claim_aliases"} {
		var exists bool
		if err := conn.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema=$1 AND table_name='oidc_client' AND column_name=$2)`, schema, column).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatalf("column %s remains after down migration", column)
		}
	}
}
