package migrations

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
)

// loginAppearanceSchema migrates a fresh schema to version 44, ready for the
// caller to seed instance_settings before stepping to 45.
func loginAppearanceSchema(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	baseURL := os.Getenv("PROHIBITORUM_TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("PROHIBITORUM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	schema := "login_appearance_" + hex.EncodeToString(nonce[:])
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE") }) //nolint:errcheck
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
	t.Cleanup(func() { conn.Close() })
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
	if err := goose.UpTo(conn, ".", 44); err != nil {
		t.Fatal(err)
	}
	return ctx, conn
}

func columnExists(t *testing.T, ctx context.Context, conn *sql.DB, column string) bool {
	t.Helper()
	var n int
	if err := conn.QueryRowContext(ctx,
		`SELECT count(*) FROM information_schema.columns
		  WHERE table_schema = current_schema() AND table_name = 'instance_settings' AND column_name = $1`,
		column).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func TestLoginAppearanceMigrationMovesBackgroundPostgres(t *testing.T) {
	ctx, conn := loginAppearanceSchema(t)
	picture := []byte("\x89PNG\r\n\x1a\nnot-really-a-png")
	if _, err := conn.ExecContext(ctx,
		"UPDATE instance_settings SET login_bg = $1, login_bg_etag = 'old-etag' WHERE id = 1", picture); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, ".", 45); err != nil {
		t.Fatal(err)
	}

	var data []byte
	var etag string
	if err := conn.QueryRowContext(ctx, "SELECT data, etag FROM login_background_image").Scan(&data, &etag); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, picture) || etag != "old-etag" {
		t.Fatalf("moved image = %q / %q, want the original bytes and etag", data, etag)
	}
	var raw []byte
	if err := conn.QueryRowContext(ctx, "SELECT login_appearance FROM instance_settings WHERE id = 1").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Background struct {
			Source string `json:"source"`
			Images struct {
				Order string `json:"order"`
			} `json:"images"`
		} `json:"background"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Background.Source != "images" || got.Background.Images.Order != "random" {
		t.Fatalf("appearance = %s, want images/random", raw)
	}
	for _, column := range []string{"login_bg", "login_bg_etag"} {
		if columnExists(t, ctx, conn, column) {
			t.Fatalf("column %s survived the migration", column)
		}
	}
	// The key columns are all set or all empty.
	if _, err := conn.ExecContext(ctx, "UPDATE instance_settings SET unsplash_key_enc = 'x' WHERE id = 1"); err == nil {
		t.Fatal("check constraint accepted a key without nonce and version")
	}
}

func TestLoginAppearanceMigrationWithoutBackgroundPostgres(t *testing.T) {
	ctx, conn := loginAppearanceSchema(t)
	if err := goose.UpTo(conn, ".", 45); err != nil {
		t.Fatal(err)
	}
	var appearance []byte
	if err := conn.QueryRowContext(ctx, "SELECT login_appearance FROM instance_settings WHERE id = 1").Scan(&appearance); err != nil {
		t.Fatal(err)
	}
	if appearance != nil {
		t.Fatalf("appearance = %s, want NULL", appearance)
	}
	var n int
	if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM login_background_image").Scan(&n); err != nil || n != 0 {
		t.Fatalf("images = %d, err %v; want none", n, err)
	}
}

func TestLoginAppearanceMigrationDownPostgres(t *testing.T) {
	ctx, conn := loginAppearanceSchema(t)
	if err := goose.UpTo(conn, ".", 45); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx,
		"INSERT INTO login_background_image (data, etag) VALUES ('first', 'e1'), ('second', 'e2')"); err != nil {
		t.Fatal(err)
	}
	if err := goose.DownTo(conn, ".", 44); err != nil {
		t.Fatal(err)
	}
	var data, etag string
	if err := conn.QueryRowContext(ctx, "SELECT login_bg, login_bg_etag FROM instance_settings WHERE id = 1").Scan(&data, &etag); err != nil {
		t.Fatal(err)
	}
	if data != "first" || etag != "e1" {
		t.Fatalf("down restored %q / %q, want the earliest image", data, etag)
	}
	if columnExists(t, ctx, conn, "login_appearance") {
		t.Fatal("login_appearance survived the down migration")
	}
}
