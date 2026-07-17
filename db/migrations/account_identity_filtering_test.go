package migrations

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
)

func TestAccountIdentityFilteringMigrationPostgres(t *testing.T) {
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
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+quotedSchema+", public"); err != nil {
		t.Fatal(err)
	}

	const legacySlug = "\tVRChat-Main\t"
	const legacySource = "upstream:" + legacySlug
	var accountID int32
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO account (username, display_name, webauthn_user_handle, avatar_source)
		VALUES ('migration-user', 'Migration User', decode('010203', 'hex'), $1)
		RETURNING id`, legacySource).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	var providerID int64
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO upstream_idp (
			slug, display_name, protocol, mode, provider_config, secret_status,
			secret_enc, secret_nonce, key_version, disabled
		)
		VALUES ($1, 'VRChat', 'vrchat', 'auto_provision', '{}'::jsonb,
			'unconfigured', NULL, NULL, NULL, false)
		RETURNING id`, legacySlug).Scan(&providerID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO account_avatar (account_id, bytes, source, content_type, etag, idp_id)
		VALUES ($1, decode('01', 'hex'), $2, 'image/png', 'legacy', $3)`, accountID, legacySource, providerID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO enrollment (token, intent, expected_upstream_idp_slug, expires_at)
		VALUES ('migration-invite', 'invite', $1, now() + interval '1 hour')`, legacySlug); err != nil {
		t.Fatal(err)
	}

	if err := goose.UpTo(conn, ".", 32); err != nil {
		t.Fatal(err)
	}
	const canonicalSlug = "vrchat-main"
	const canonicalSource = "upstream:" + canonicalSlug
	for name, assertion := range map[string]string{
		"provider slug":     `SELECT slug FROM upstream_idp WHERE id = $1`,
		"enrollment slug":   `SELECT expected_upstream_idp_slug FROM enrollment WHERE token = 'migration-invite'`,
		"account pointer":   `SELECT avatar_source FROM account WHERE id = $1`,
		"avatar source key": `SELECT source FROM account_avatar WHERE account_id = $1 AND idp_id = $2`,
	} {
		var got string
		var err error
		switch name {
		case "provider slug":
			err = conn.QueryRowContext(ctx, assertion, providerID).Scan(&got)
		case "account pointer":
			err = conn.QueryRowContext(ctx, assertion, accountID).Scan(&got)
		case "avatar source key":
			err = conn.QueryRowContext(ctx, assertion, accountID, providerID).Scan(&got)
		default:
			err = conn.QueryRowContext(ctx, assertion).Scan(&got)
		}
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := canonicalSlug
		if name == "account pointer" || name == "avatar source key" {
			want = canonicalSource
		}
		if got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}

	if _, err := conn.ExecContext(ctx, `
		INSERT INTO upstream_idp (
			slug, display_name, protocol, mode, provider_config, secret_status,
			secret_enc, secret_nonce, key_version, disabled
		)
		VALUES (E'\tinvalid\t', 'Invalid', 'vrchat', 'auto_provision', '{}'::jsonb,
			'unconfigured', NULL, NULL, NULL, false)`); err == nil {
		t.Fatal("ASCII-whitespace provider slug was accepted after migration")
	}

	if err := goose.DownTo(conn, ".", 31); err != nil {
		t.Fatal(err)
	}
	var version int64
	if err := conn.QueryRowContext(ctx, `SELECT version_id FROM goose_db_version WHERE is_applied ORDER BY id DESC LIMIT 1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 31 {
		t.Fatalf("migration version after down = %d, want 31", version)
	}
	for _, indexName := range []string{
		"account_created_at_id_idx",
		"account_identity_upstream_idp_account_idx",
		"account_search_trgm_idx",
		"account_identity_search_trgm_idx",
	} {
		var exists bool
		if err := conn.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, fmt.Sprintf("%s.%s", schema, indexName)).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Errorf("index %q remains after migration down", indexName)
		}
	}
}
