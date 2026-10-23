package migrations

import (
	"reflect"
	"testing"

	"github.com/pressly/goose/v3"
)

// openverseRollbackAppearance is a document as migration 54 left it: the
// openverse object filled in, the source chosen as openverse, and
// non-default values everywhere else so the test can tell they survive.
const openverseRollbackAppearance = `{
  "background": {
    "source": "openverse",
    "color": "#0a1b2c",
    "gradient": "ember",
    "bing": {"market": "en-US", "showCaption": false},
    "unsplash": {"query": "sea", "order": "carousel", "intervalSeconds": 30},
    "pixabay": {"query": "雪山", "lang": "zh", "order": "carousel", "intervalSeconds": 30},
    "openverse": {"query": "forest", "order": "carousel", "intervalSeconds": 30},
    "images": {"order": "carousel", "intervalSeconds": 30}
  },
  "card": {"translucent": true, "opacity": 55, "blur": false},
  "capsules": {"translucent": false, "opacity": 70, "blur": true},
  "cardPosition": "left",
  "theme": "dark",
  "scheme": "nord"
}`

// openverseRollbackKeptAppearance is the same save with a different source:
// the filled-in openverse object is still there, and 055 must drop it too.
const openverseRollbackKeptAppearance = `{
  "background": {
    "source": "unsplash",
    "color": "#0a1b2c",
    "gradient": "ember",
    "bing": {"market": "en-US", "showCaption": false},
    "unsplash": {"query": "sea", "order": "carousel", "intervalSeconds": 30},
    "pixabay": {"query": "雪山", "lang": "zh", "order": "carousel", "intervalSeconds": 30},
    "openverse": {"query": "forest", "order": "carousel", "intervalSeconds": 30},
    "images": {"order": "carousel", "intervalSeconds": 30}
  },
  "card": {"translucent": true, "opacity": 55, "blur": false},
  "capsules": {"translucent": false, "opacity": 70, "blur": true},
  "cardPosition": "left",
  "theme": "dark",
  "scheme": "nord"
}`

func TestOpenverseRollbackMigrationPostgres(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  string
	}{
		{name: "source-openverse", doc: openverseRollbackAppearance},
		{name: "object-only", doc: openverseRollbackKeptAppearance},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, conn := loginAppearanceSchema(t)
			if err := goose.UpTo(conn, ".", 54); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.ExecContext(ctx,
				"UPDATE instance_settings SET login_appearance = $1::jsonb WHERE id = 1", tc.doc); err != nil {
				t.Fatal(err)
			}
			if err := goose.UpTo(conn, ".", 55); err != nil {
				t.Fatal(err)
			}
			want := decodeJSON(t, tc.doc)
			background := want["background"].(map[string]any)
			delete(background, "openverse")
			if tc.name == "source-openverse" {
				background["source"] = "none"
			}
			if got := loginAppearance(t, ctx, conn); !reflect.DeepEqual(got, want) {
				t.Fatalf("up: appearance = %v, want %v", got, want)
			}

			// Down is a no-op by design: the feature is gone from the build,
			// so stepping back must leave the cleaned document alone.
			if err := goose.DownTo(conn, ".", 54); err != nil {
				t.Fatal(err)
			}
			if got := loginAppearance(t, ctx, conn); !reflect.DeepEqual(got, want) {
				t.Fatalf("down: appearance = %v, want %v", got, want)
			}
		})
	}
}
