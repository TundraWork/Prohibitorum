package migrations

import (
	"database/sql"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
)

func TestAvatarSyncFetchMigrationPostgres(t *testing.T) {
	ctx, conn := loginAppearanceSchema(t)
	if err := goose.UpTo(conn, ".", 47); err != nil {
		t.Fatal(err)
	}
	var providerID int64
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO upstream_idp (
			slug, display_name, protocol, mode, provider_config, secret_status,
			secret_enc, secret_nonce, key_version, disabled
		)
		VALUES ('mockop', 'Mock OP', 'oidc', 'auto_provision', '{}'::jsonb,
			'unconfigured', NULL, NULL, NULL, false)
		RETURNING id`).Scan(&providerID); err != nil {
		t.Fatal(err)
	}
	updatedAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	accounts := []struct {
		name         string
		source       sql.NullString
		sources      []string
		wantSelected bool
	}{
		{name: "none", source: sql.NullString{String: "none", Valid: true}, wantSelected: true},
		{name: "upstream-over-upload", source: sql.NullString{String: "upstream:mockop", Valid: true},
			sources: []string{"user", "upstream:mockop"}, wantSelected: true},
		{name: "upload", source: sql.NullString{String: "user", Valid: true}, sources: []string{"user"}},
		{name: "upstream-only", source: sql.NullString{String: "upstream:mockop", Valid: true},
			sources: []string{"upstream:mockop"}},
		{name: "never-set"},
	}
	ids := make([]int32, len(accounts))
	for i, acct := range accounts {
		if err := conn.QueryRowContext(ctx, `
			INSERT INTO account (username, display_name, webauthn_user_handle, avatar_source, updated_at)
			VALUES ($1, $1, decode(md5($1), 'hex'), $2, $3)
			RETURNING id`, acct.name, acct.source, updatedAt).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
		for _, source := range acct.sources {
			var idpID sql.NullInt64
			if source != "user" {
				idpID = sql.NullInt64{Int64: providerID, Valid: true}
			}
			if _, err := conn.ExecContext(ctx, `
				INSERT INTO account_avatar (account_id, bytes, source, content_type, etag, idp_id)
				VALUES ($1, decode('01', 'hex'), $2, 'image/png', 'etag', $3)`, ids[i], source, idpID); err != nil {
				t.Fatal(err)
			}
		}
	}

	if err := goose.UpTo(conn, ".", 48); err != nil {
		t.Fatal(err)
	}
	for i, acct := range accounts {
		var selectedAt sql.NullTime
		if err := conn.QueryRowContext(ctx,
			`SELECT avatar_selected_at FROM account WHERE id = $1`, ids[i]).Scan(&selectedAt); err != nil {
			t.Fatal(err)
		}
		if selectedAt.Valid != acct.wantSelected {
			t.Errorf("%s: avatar_selected_at = %v, want set=%v", acct.name, selectedAt, acct.wantSelected)
		}
		if selectedAt.Valid && !selectedAt.Time.Equal(updatedAt) {
			t.Errorf("%s: avatar_selected_at = %v, want %v", acct.name, selectedAt.Time, updatedAt)
		}
	}
	var withURL int
	if err := conn.QueryRowContext(ctx,
		`SELECT count(*) FROM account_avatar WHERE upstream_url IS NOT NULL`).Scan(&withURL); err != nil {
		t.Fatal(err)
	}
	if withURL != 0 {
		t.Errorf("rows with upstream_url = %d, want 0", withURL)
	}

	if err := goose.DownTo(conn, ".", 47); err != nil {
		t.Fatal(err)
	}
	for table, column := range map[string]string{"account": "avatar_selected_at", "account_avatar": "upstream_url"} {
		var n int
		if err := conn.QueryRowContext(ctx,
			`SELECT count(*) FROM information_schema.columns
			  WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2`,
			table, column).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("down: %s.%s still exists", table, column)
		}
	}
}
