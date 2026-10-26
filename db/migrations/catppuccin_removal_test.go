package migrations

import (
	"testing"

	"github.com/pressly/goose/v3"

	"prohibitorum/pkg/branding"
)

// catppuccinAppearance is a sign-in appearance saved with the Catppuccin
// scheme, with non-default values everywhere else so the test can tell they
// survive.
const catppuccinAppearance = `{
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
  "scheme": "catppuccin-gray"
}`

func TestCatppuccinRemovalMigrationPostgres(t *testing.T) {
	ctx, conn := loginAppearanceSchema(t)
	if err := goose.UpTo(conn, ".", 55); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx,
		"UPDATE instance_settings SET login_appearance = $1::jsonb WHERE id = 1", catppuccinAppearance); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO account (username, display_name, webauthn_user_handle, preferences) VALUES
		 ('ada', 'Ada', '\x01', '{"appearance": {"scheme": "catppuccin", "mode": "dark"}}'),
		 ('bob', 'Bob', '\x02', '{"appearance": {"scheme": "nord", "mode": null}}')`); err != nil {
		t.Fatal(err)
	}
	// Without 056 the build cannot read the saved look at all, and the store
	// would silently show the default instead.
	if _, err := branding.DecodeAppearance(rawLoginAppearance(t, ctx, conn)); err == nil {
		t.Fatal("before 056: a document naming catppuccin-gray decoded; the check below proves nothing")
	}
	if err := goose.UpTo(conn, ".", 56); err != nil {
		t.Fatal(err)
	}
	decoded, err := branding.DecodeAppearance(rawLoginAppearance(t, ctx, conn))
	if err != nil {
		t.Fatalf("after 056: the saved document does not decode: %v", err)
	}
	if decoded.Scheme != branding.DefaultScheme || decoded.Background.Unsplash.Query != "sea" || decoded.CardPosition != "left" {
		t.Fatalf("after 056: decoded %+v, want the default scheme with the other saved values kept", decoded)
	}

	for username, want := range map[string]string{
		"ada": `{"appearance": {"mode": "dark", "scheme": null}}`,
		"bob": `{"appearance": {"mode": null, "scheme": "nord"}}`,
	} {
		var got string
		if err := conn.QueryRowContext(ctx,
			"SELECT preferences::text FROM account WHERE username = $1", username).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("after 056: %s preferences = %s, want %s", username, got, want)
		}
	}

	if err := goose.DownTo(conn, ".", 55); err != nil {
		t.Fatal(err)
	}
	if got := rawLoginAppearance(t, ctx, conn); len(got) == 0 {
		t.Fatal("down: the saved appearance is gone")
	}
}
