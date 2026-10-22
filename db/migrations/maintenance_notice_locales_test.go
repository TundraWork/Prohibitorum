package migrations

import (
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

// maintenanceNoticeSchema migrates a fresh schema to version 50, ready for the
// caller to seed instance_settings before stepping to 51.
func maintenanceNoticeSchema(t *testing.T) (context.Context, *sql.DB) {
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
	schema := "maintenance_notice_" + hex.EncodeToString(nonce[:])
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
	if err := goose.UpTo(conn, ".", 50); err != nil {
		t.Fatal(err)
	}
	return ctx, conn
}

func readMaintenanceMessages(t *testing.T, ctx context.Context, conn *sql.DB) (map[string]string, string) {
	t.Helper()
	var raw []byte
	var locale string
	if err := conn.QueryRowContext(ctx,
		"SELECT maintenance_messages, maintenance_default_locale FROM instance_settings WHERE id = 1").
		Scan(&raw, &locale); err != nil {
		t.Fatal(err)
	}
	var messages map[string]string
	if err := json.Unmarshal(raw, &messages); err != nil {
		t.Fatalf("maintenance_messages = %s: %v", raw, err)
	}
	return messages, locale
}

func TestMaintenanceNoticeMigrationMovesSavedMessage(t *testing.T) {
	ctx, conn := maintenanceNoticeSchema(t)
	if _, err := conn.ExecContext(ctx,
		"UPDATE instance_settings SET maintenance_message = $1 WHERE id = 1", "Back at 17:00 UTC"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, ".", 51); err != nil {
		t.Fatal(err)
	}

	messages, locale := readMaintenanceMessages(t, ctx, conn)
	if len(messages) != 1 || messages["en"] != "Back at 17:00 UTC" {
		t.Fatalf("maintenance_messages = %v, want {en:Back at 17:00 UTC}", messages)
	}
	if locale != "en" {
		t.Fatalf("maintenance_default_locale = %q, want en", locale)
	}
	if columnExists(t, ctx, conn, "maintenance_message") {
		t.Fatal("column maintenance_message survived the migration")
	}
}

func TestMaintenanceNoticeMigrationWithoutMessage(t *testing.T) {
	ctx, conn := maintenanceNoticeSchema(t)
	// Both shapes an untouched row can hold: NULL, and an emptied-out note.
	for _, setup := range []string{
		"UPDATE instance_settings SET maintenance_message = NULL WHERE id = 1",
		"UPDATE instance_settings SET maintenance_message = '' WHERE id = 1",
	} {
		if _, err := conn.ExecContext(ctx, setup); err != nil {
			t.Fatal(err)
		}
		if err := goose.UpTo(conn, ".", 51); err != nil {
			t.Fatal(err)
		}
		messages, locale := readMaintenanceMessages(t, ctx, conn)
		if len(messages) != 0 {
			t.Fatalf("maintenance_messages = %v, want empty", messages)
		}
		if locale != "en" {
			t.Fatalf("maintenance_default_locale = %q, want en", locale)
		}
		if err := goose.DownTo(conn, ".", 50); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMaintenanceNoticeMigrationDown(t *testing.T) {
	ctx, conn := maintenanceNoticeSchema(t)
	if _, err := conn.ExecContext(ctx,
		"UPDATE instance_settings SET maintenance_message = '回到 17:00' WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, ".", 51); err != nil {
		t.Fatal(err)
	}
	// A notice set per language after the migration: the Down keeps the
	// default language's text as the single notice.
	if _, err := conn.ExecContext(ctx, `
		UPDATE instance_settings
		SET maintenance_messages = '{"en": "Back soon", "zh": "马上回来"}'::jsonb,
		    maintenance_default_locale = 'zh'
		WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if err := goose.DownTo(conn, ".", 50); err != nil {
		t.Fatal(err)
	}

	var message sql.NullString
	if err := conn.QueryRowContext(ctx,
		"SELECT maintenance_message FROM instance_settings WHERE id = 1").Scan(&message); err != nil {
		t.Fatal(err)
	}
	if !message.Valid || message.String != "马上回来" {
		t.Fatalf("maintenance_message after down = %v, want 马上回来", message)
	}
	for _, column := range []string{"maintenance_messages", "maintenance_default_locale"} {
		if columnExists(t, ctx, conn, column) {
			t.Fatalf("column %s survived the down migration", column)
		}
	}
}
