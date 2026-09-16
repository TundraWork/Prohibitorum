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

// TestOIDCClientPublicRequiresPkceMigrationPostgres exercises 037: the new
// CHECK must reject a public client (client_auth_method = 'none') that opts
// out of PKCE, while 'none' + require_pkce and a confidential client without
// PKCE both stay insertable. Down must drop the constraint, not merely the
// column default, so 'none' + false becomes insertable again.
func TestOIDCClientPublicRequiresPkceMigrationPostgres(t *testing.T) {
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
	if err := goose.UpTo(conn, ".", 36); err != nil {
		t.Fatal(err)
	}

	// Existing rows all carry require_pkce = true (the only historical write
	// path hardcoded it), so the constraint must accept them: a fresh row
	// with default settings inserts fine right after the migration.
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO oidc_client (client_id, display_name, redirect_uris)
		VALUES ('default-row', 'default-row', ARRAY['https://rp.example.com/cb'])`); err != nil {
		t.Fatalf("default row rejected after 037: %v", err)
	}

	if err := goose.UpTo(conn, ".", 37); err != nil {
		t.Fatal(err)
	}

	insertClient := func(name string, method string, requirePkce bool) error {
		t.Helper()
		_, err := conn.ExecContext(ctx, `
			INSERT INTO oidc_client (
				client_id, display_name, redirect_uris, client_auth_method, require_pkce
			)
			VALUES ($1, $1, ARRAY['https://rp.example.com/cb'], $2, $3)`, name, method, requirePkce)
		return err
	}

	// Public client + require_pkce = false is exactly what the constraint
	// exists to prevent.
	var pgErr *pgconn.PgError
	err = insertClient("public-no-pkce", "none", false)
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" ||
		pgErr.ConstraintName != "oidc_client_public_requires_pkce_check" {
		t.Fatalf("public client without PKCE: error = %v, want check violation for oidc_client_public_requires_pkce_check", err)
	}

	// Public client keeping PKCE is fine.
	if err := insertClient("public-pkce", "none", true); err != nil {
		t.Fatalf("'none' with require_pkce = true was rejected: %v", err)
	}
	// Confidential client opting out is the relaxation this card ships.
	if err := insertClient("confidential-no-pkce", "client_secret", false); err != nil {
		t.Fatalf("'client_secret' with require_pkce = false was rejected: %v", err)
	}

	// Down must actually drop the constraint, so the rejected combination
	// becomes insertable again.
	if err := goose.DownTo(conn, ".", 36); err != nil {
		t.Fatal(err)
	}
	if err := insertClient("public-no-pkce-after-down", "none", false); err != nil {
		t.Fatalf("'none' with require_pkce = false after down: %v, want insert to succeed", err)
	}
	var version int64
	if err := conn.QueryRowContext(ctx,
		`SELECT version_id FROM goose_db_version WHERE is_applied ORDER BY id DESC LIMIT 1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 36 {
		t.Fatalf("migration version after down = %d, want 36", version)
	}
}
