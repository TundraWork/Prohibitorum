package migrations

import (
	"reflect"
	"testing"

	"github.com/pressly/goose/v3"
)

// pixabaySourceAppearance is a document as migration 50 left it: no pixabay
// object, and non-default values everywhere else so the test can tell they
// survive. (Migrations 51 and 52 touch other tables.)
const pixabaySourceAppearance = `{
  "background": {
    "source": "unsplash",
    "color": "#0a1b2c",
    "gradient": "ember",
    "bing": {"market": "en-US", "showCaption": false},
    "unsplash": {"query": "sea", "order": "carousel", "intervalSeconds": 30},
    "images": {"order": "carousel", "intervalSeconds": 30}
  },
  "card": {"translucent": true, "opacity": 55, "blur": false},
  "capsules": {"translucent": false, "opacity": 70, "blur": true},
  "cardPosition": "left",
  "theme": "dark",
  "scheme": "nord"
}`

func TestPixabaySourceMigrationPostgres(t *testing.T) {
	ctx, conn := loginAppearanceSchema(t)
	if err := goose.UpTo(conn, ".", 52); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx,
		"UPDATE instance_settings SET login_appearance = $1::jsonb WHERE id = 1", pixabaySourceAppearance); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, ".", 53); err != nil {
		t.Fatal(err)
	}
	want := decodeJSON(t, pixabaySourceAppearance)
	want["background"].(map[string]any)["pixabay"] = map[string]any{
		"query":           "",
		"lang":            "zh",
		"order":           "random",
		"intervalSeconds": float64(10),
	}
	if got := loginAppearance(t, ctx, conn); !reflect.DeepEqual(got, want) {
		t.Fatalf("up: appearance = %v, want %v", got, want)
	}

	// The key columns are all set or all empty.
	if _, err := conn.ExecContext(ctx, "UPDATE instance_settings SET pixabay_key_enc = 'x' WHERE id = 1"); err == nil {
		t.Fatal("check constraint accepted a key without nonce and version")
	}

	if err := goose.DownTo(conn, ".", 52); err != nil {
		t.Fatal(err)
	}
	if got, want := loginAppearance(t, ctx, conn), decodeJSON(t, pixabaySourceAppearance); !reflect.DeepEqual(got, want) {
		t.Fatalf("down: appearance = %v, want %v", got, want)
	}
	for _, column := range []string{"pixabay_key_enc", "pixabay_key_nonce", "pixabay_key_version"} {
		if columnExists(t, ctx, conn, column) {
			t.Fatalf("column %s survived the migration", column)
		}
	}
}

func TestPixabaySourceMigrationWithoutAppearancePostgres(t *testing.T) {
	ctx, conn := loginAppearanceSchema(t)
	if err := goose.UpTo(conn, ".", 53); err != nil {
		t.Fatal(err)
	}
	if got := loginAppearance(t, ctx, conn); got != nil {
		t.Fatalf("appearance = %v, want NULL", got)
	}
}
