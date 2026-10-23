package migrations

import (
	"reflect"
	"testing"

	"github.com/pressly/goose/v3"
)

// openverseSourceAppearance is a document as migration 53 left it: no
// openverse object, and non-default values everywhere else so the test can
// tell they survive.
const openverseSourceAppearance = `{
  "background": {
    "source": "unsplash",
    "color": "#0a1b2c",
    "gradient": "ember",
    "bing": {"market": "en-US", "showCaption": false},
    "unsplash": {"query": "sea", "order": "carousel", "intervalSeconds": 30},
    "pixabay": {"query": "雪山", "lang": "zh", "order": "carousel", "intervalSeconds": 30},
    "images": {"order": "carousel", "intervalSeconds": 30}
  },
  "card": {"translucent": true, "opacity": 55, "blur": false},
  "capsules": {"translucent": false, "opacity": 70, "blur": true},
  "cardPosition": "left",
  "theme": "dark",
  "scheme": "nord"
}`

func TestOpenverseSourceMigrationPostgres(t *testing.T) {
	ctx, conn := loginAppearanceSchema(t)
	if err := goose.UpTo(conn, ".", 53); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx,
		"UPDATE instance_settings SET login_appearance = $1::jsonb WHERE id = 1", openverseSourceAppearance); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, ".", 54); err != nil {
		t.Fatal(err)
	}
	want := decodeJSON(t, openverseSourceAppearance)
	want["background"].(map[string]any)["openverse"] = map[string]any{
		"query":           "",
		"order":           "random",
		"intervalSeconds": float64(10),
	}
	if got := loginAppearance(t, ctx, conn); !reflect.DeepEqual(got, want) {
		t.Fatalf("up: appearance = %v, want %v", got, want)
	}

	// 055 rolls the feature back: the filled-in object is dropped again, and
	// every other saved value stays. Down through 055 is a no-op, so the
	// document below is also what the DownTo below sees.
	if err := goose.UpTo(conn, ".", 55); err != nil {
		t.Fatal(err)
	}
	if got, want := loginAppearance(t, ctx, conn), decodeJSON(t, openverseSourceAppearance); !reflect.DeepEqual(got, want) {
		t.Fatalf("rollback: appearance = %v, want %v", got, want)
	}

	if err := goose.DownTo(conn, ".", 53); err != nil {
		t.Fatal(err)
	}
	if got, want := loginAppearance(t, ctx, conn), decodeJSON(t, openverseSourceAppearance); !reflect.DeepEqual(got, want) {
		t.Fatalf("down: appearance = %v, want %v", got, want)
	}
}

func TestOpenverseSourceMigrationWithoutAppearancePostgres(t *testing.T) {
	ctx, conn := loginAppearanceSchema(t)
	if err := goose.UpTo(conn, ".", 55); err != nil {
		t.Fatal(err)
	}
	if got := loginAppearance(t, ctx, conn); got != nil {
		t.Fatalf("appearance = %v, want NULL", got)
	}
}
