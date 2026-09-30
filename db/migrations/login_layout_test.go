package migrations

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/pressly/goose/v3"
)

// savedAppearance is a document as migration 45 left it: no card position and
// no theme, with non-default values elsewhere so the test can tell they survive.
const savedAppearance = `{
  "background": {
    "source": "gradient",
    "color": "#0a1b2c",
    "gradient": "ember",
    "bing": {"market": "en-US", "showCaption": false},
    "unsplash": {"query": "sea"},
    "images": {"order": "carousel", "intervalSeconds": 30}
  },
  "card": {"translucent": true, "opacity": 55, "blur": false},
  "capsules": {"translucent": false, "opacity": 70, "blur": true}
}`

func loginAppearance(t *testing.T, ctx context.Context, conn *sql.DB) map[string]any {
	t.Helper()
	var raw []byte
	if err := conn.QueryRowContext(ctx, "SELECT login_appearance FROM instance_settings WHERE id = 1").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw == nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func decodeJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestLoginLayoutMigrationPostgres(t *testing.T) {
	ctx, conn := loginAppearanceSchema(t)
	if err := goose.UpTo(conn, ".", 45); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx,
		"UPDATE instance_settings SET login_appearance = $1::jsonb WHERE id = 1", savedAppearance); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, ".", 46); err != nil {
		t.Fatal(err)
	}
	want := decodeJSON(t, savedAppearance)
	want["cardPosition"] = "center"
	want["theme"] = "switchable"
	if got := loginAppearance(t, ctx, conn); !reflect.DeepEqual(got, want) {
		t.Fatalf("up: appearance = %v, want %v", got, want)
	}

	if err := goose.DownTo(conn, ".", 45); err != nil {
		t.Fatal(err)
	}
	if got, want := loginAppearance(t, ctx, conn), decodeJSON(t, savedAppearance); !reflect.DeepEqual(got, want) {
		t.Fatalf("down: appearance = %v, want %v", got, want)
	}
}

func TestLoginLayoutMigrationWithoutAppearancePostgres(t *testing.T) {
	ctx, conn := loginAppearanceSchema(t)
	if err := goose.UpTo(conn, ".", 46); err != nil {
		t.Fatal(err)
	}
	if got := loginAppearance(t, ctx, conn); got != nil {
		t.Fatalf("appearance = %v, want NULL", got)
	}
}
