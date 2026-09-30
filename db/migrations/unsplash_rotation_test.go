package migrations

import (
	"reflect"
	"testing"

	"github.com/pressly/goose/v3"
)

// layoutAppearance is a document as migration 46 left it: the Unsplash options
// hold only the query, and the rest carries non-default values so the test can
// tell they survive.
const layoutAppearance = `{
  "background": {
    "source": "unsplash",
    "color": "#0a1b2c",
    "gradient": "ember",
    "bing": {"market": "en-US", "showCaption": false},
    "unsplash": {"query": "sea"},
    "images": {"order": "carousel", "intervalSeconds": 30}
  },
  "card": {"translucent": true, "opacity": 55, "blur": false},
  "capsules": {"translucent": false, "opacity": 70, "blur": true},
  "cardPosition": "left",
  "theme": "dark"
}`

func TestUnsplashRotationMigrationPostgres(t *testing.T) {
	ctx, conn := loginAppearanceSchema(t)
	if err := goose.UpTo(conn, ".", 46); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx,
		"UPDATE instance_settings SET login_appearance = $1::jsonb WHERE id = 1", layoutAppearance); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, ".", 47); err != nil {
		t.Fatal(err)
	}
	want := decodeJSON(t, layoutAppearance)
	unsplash := want["background"].(map[string]any)["unsplash"].(map[string]any)
	unsplash["order"] = "random"
	unsplash["intervalSeconds"] = float64(10)
	if got := loginAppearance(t, ctx, conn); !reflect.DeepEqual(got, want) {
		t.Fatalf("up: appearance = %v, want %v", got, want)
	}

	if err := goose.DownTo(conn, ".", 46); err != nil {
		t.Fatal(err)
	}
	if got, want := loginAppearance(t, ctx, conn), decodeJSON(t, layoutAppearance); !reflect.DeepEqual(got, want) {
		t.Fatalf("down: appearance = %v, want %v", got, want)
	}
}

func TestUnsplashRotationMigrationWithoutAppearancePostgres(t *testing.T) {
	ctx, conn := loginAppearanceSchema(t)
	if err := goose.UpTo(conn, ".", 47); err != nil {
		t.Fatal(err)
	}
	if got := loginAppearance(t, ctx, conn); got != nil {
		t.Fatalf("appearance = %v, want NULL", got)
	}
}
