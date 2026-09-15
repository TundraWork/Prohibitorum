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

// TestOIDCClientAuthMethodMigrationPostgres exercises 034 against stored rows:
// the rename must carry existing clients over, both legacy confidential values
// must collapse to 'client_secret', 'none' must survive untouched, and the new
// CHECK must close the vocabulary. Down must restore the old column name,
// default, and value.
func TestOIDCClientAuthMethodMigrationPostgres(t *testing.T) {
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
	schema := "migration_" + hex.EncodeToString(nonce[:])
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
	conn, err := sql.Open("pgx", schemaURL.String())
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(1)
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := goose.UpTo(conn, ".", 31); err != nil {
		t.Fatal(err)
	}
	// 032 onward reference public extensions, matching the 033 migration test.
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+quotedSchema+", public"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, ".", 33); err != nil {
		t.Fatal(err)
	}

	// Stored rows spanning every pre-034 value, including the one the old
	// default produced and the one that had no write path but was representable.
	insertLegacy := func(clientID, method string) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, `
			INSERT INTO oidc_client (
				client_id, display_name, redirect_uris, token_endpoint_auth_method
			)
			VALUES ($1, $1, ARRAY['https://rp.example.com/cb'], $2)`, clientID, method); err != nil {
			t.Fatal(err)
		}
	}
	insertLegacy("legacy-basic", "client_secret_basic")
	insertLegacy("legacy-post", "client_secret_post")
	insertLegacy("legacy-public", "none")

	// The pre-034 default must be the old value, so the backfill has something
	// to do.
	var legacyDefault string
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO oidc_client (client_id, display_name, redirect_uris)
		VALUES ('legacy-default', 'legacy-default', ARRAY['https://rp.example.com/cb'])
		RETURNING token_endpoint_auth_method`).Scan(&legacyDefault); err != nil {
		t.Fatal(err)
	}
	if legacyDefault != "client_secret_basic" {
		t.Fatalf("pre-034 default = %q, want client_secret_basic", legacyDefault)
	}

	if err := goose.UpTo(conn, ".", 34); err != nil {
		t.Fatal(err)
	}

	for clientID, want := range map[string]string{
		"legacy-basic":   "client_secret",
		"legacy-post":    "client_secret",
		"legacy-public":  "none",
		"legacy-default": "client_secret",
	} {
		var got string
		if err := conn.QueryRowContext(ctx,
			`SELECT client_auth_method FROM oidc_client WHERE client_id = $1`, clientID).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s: client_auth_method = %q, want %q", clientID, got, want)
		}
	}

	// The old column name is gone, not duplicated.
	var oldColumnExists bool
	if err := conn.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = $1 AND table_name = 'oidc_client'
			  AND column_name = 'token_endpoint_auth_method'
		)`, schema).Scan(&oldColumnExists); err != nil {
		t.Fatal(err)
	}
	if oldColumnExists {
		t.Error("token_endpoint_auth_method still exists after 034")
	}

	// The new default applies to rows that omit the column.
	var newDefault string
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO oidc_client (client_id, display_name, redirect_uris)
		VALUES ('post-034-default', 'post-034-default', ARRAY['https://rp.example.com/cb'])
		RETURNING client_auth_method`).Scan(&newDefault); err != nil {
		t.Fatal(err)
	}
	if newDefault != "client_secret" {
		t.Errorf("post-034 default = %q, want client_secret", newDefault)
	}

	// The CHECK closes the vocabulary, so a stale value cannot be written back.
	assertCheckRejected := func(name, statement string, args ...any) {
		t.Helper()
		_, err := conn.ExecContext(ctx, statement, args...)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" ||
			pgErr.ConstraintName != "oidc_client_client_auth_method_check" {
			t.Errorf("%s error = %v, want check violation for oidc_client_client_auth_method_check", name, err)
		}
	}
	assertCheckRejected("legacy value update",
		`UPDATE oidc_client SET client_auth_method = 'client_secret_basic' WHERE client_id = 'legacy-basic'`)
	assertCheckRejected("unknown value insert", `
		INSERT INTO oidc_client (client_id, display_name, redirect_uris, client_auth_method)
		VALUES ('private-key-jwt-rp', 'JWT RP', ARRAY['https://rp.example.com/cb'], 'private_key_jwt')`)
	if _, err := conn.ExecContext(ctx,
		`UPDATE oidc_client SET client_auth_method = 'none' WHERE client_id = 'legacy-basic'`); err != nil {
		t.Errorf("'none' was rejected: %v", err)
	}

	if err := goose.DownTo(conn, ".", 33); err != nil {
		t.Fatal(err)
	}
	var version int64
	if err := conn.QueryRowContext(ctx,
		`SELECT version_id FROM goose_db_version WHERE is_applied ORDER BY id DESC LIMIT 1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 33 {
		t.Fatalf("migration version after down = %d, want 33", version)
	}
	var restored string
	if err := conn.QueryRowContext(ctx,
		`SELECT token_endpoint_auth_method FROM oidc_client WHERE client_id = 'legacy-post'`).Scan(&restored); err != nil {
		t.Fatal(err)
	}
	if restored != "client_secret_basic" {
		// Down cannot recover which of the two legacy values a row started with;
		// it restores the default one. Asserted so the lossiness stays explicit.
		t.Errorf("token_endpoint_auth_method after down = %q, want client_secret_basic", restored)
	}
	var downDefault string
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO oidc_client (client_id, display_name, redirect_uris)
		VALUES ('post-down-default', 'post-down-default', ARRAY['https://rp.example.com/cb'])
		RETURNING token_endpoint_auth_method`).Scan(&downDefault); err != nil {
		t.Fatal(err)
	}
	if downDefault != "client_secret_basic" {
		t.Errorf("default after down = %q, want client_secret_basic", downDefault)
	}
}
