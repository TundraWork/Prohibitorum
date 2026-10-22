package migrations

import (
	"reflect"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestColourSchemesMigrationPostgres(t *testing.T) {
	ctx, conn := loginAppearanceSchema(t)
	if err := goose.UpTo(conn, ".", 46); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx,
		"UPDATE instance_settings SET login_appearance = $1::jsonb WHERE id = 1", savedAppearance); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, ".", 51); err != nil {
		t.Fatal(err)
	}
	want := loginAppearance(t, ctx, conn)
	if want["scheme"] != "graphite" {
		t.Fatalf("up: scheme = %v, want graphite", want["scheme"])
	}
	var preferences string
	if err := conn.QueryRowContext(ctx,
		`INSERT INTO account (username, display_name, webauthn_user_handle)
		 VALUES ('ada', 'Ada', '\x01') RETURNING preferences::text`,
	).Scan(&preferences); err != nil {
		t.Fatal(err)
	}
	if preferences != "{}" {
		t.Fatalf("new account preferences = %s, want {}", preferences)
	}

	if err := goose.DownTo(conn, ".", 50); err != nil {
		t.Fatal(err)
	}
	got := loginAppearance(t, ctx, conn)
	if _, ok := got["scheme"]; ok {
		t.Fatalf("down: appearance still has a scheme: %v", got)
	}
	delete(want, "scheme")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("down: appearance = %v, want %v", got, want)
	}
}
