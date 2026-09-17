package migrations

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"

	dbgen "prohibitorum/pkg/db"
)

func TestGlobalUserGroupsMigrationPostgres(t *testing.T) {
	baseURL := os.Getenv("PROHIBITORUM_TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("PROHIBITORUM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	schema := "global_groups_" + hex.EncodeToString(nonce[:])
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE") //nolint:errcheck

	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	conn, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	goose.SetBaseFS(embedMigrations)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, ".", 39); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+quoted+", public"); err != nil {
		t.Fatal(err)
	}

	var accountID int32
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO account (username, display_name, webauthn_user_handle)
		VALUES ('global-member', 'Global Member', decode('aabbcc', 'hex')) RETURNING id`,
	).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO oidc_client (client_id, display_name, redirect_uris, access_restricted) VALUES
		  ('oidc-one', 'OIDC One', ARRAY['https://one.example/cb'], true),
		  ('forward-one', 'Forward One', ARRAY['https://forward.example/cb'], true);
		UPDATE oidc_client SET forward_auth_enabled=true, forward_auth_host='forward.example' WHERE client_id='forward-one'`); err != nil {
		t.Fatal(err)
	}
	var samlID int64
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO saml_sp (entity_id, display_name, allow_idp_initiated, access_restricted)
		VALUES ('https://saml.example/sp', 'SAML One', true, true) RETURNING id`,
	).Scan(&samlID); err != nil {
		t.Fatal(err)
	}

	var oidcManual, oidcRule, forwardRule, samlRule int32
	if err := conn.QueryRowContext(ctx, `INSERT INTO user_group (kind,slug,display_name,oidc_client_id) VALUES ('manual','shared','Manual','oidc-one') RETURNING id`).Scan(&oidcManual); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		id  *int32
		sql string
		arg any
	}{
		{&oidcRule, `INSERT INTO user_group (kind,slug,display_name,rule,oidc_client_id) VALUES ('rule','duplicate','OIDC Rule','{}','oidc-one') RETURNING id`, nil},
		{&forwardRule, `INSERT INTO user_group (kind,slug,display_name,rule,oidc_client_id) VALUES ('rule','duplicate','Forward Rule','{}','forward-one') RETURNING id`, nil},
		{&samlRule, `INSERT INTO user_group (kind,slug,display_name,rule,saml_sp_id) VALUES ('rule','duplicate','SAML Rule','{}',$1) RETURNING id`, samlID},
	} {
		var scanErr error
		if row.arg == nil {
			scanErr = conn.QueryRowContext(ctx, row.sql).Scan(row.id)
		} else {
			scanErr = conn.QueryRowContext(ctx, row.sql, row.arg).Scan(row.id)
		}
		if scanErr != nil {
			t.Fatal(scanErr)
		}
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO group_manual_decision (group_id,account_id,effect) VALUES ($1,$2,'deny')`, oidcManual, accountID); err != nil {
		t.Fatal(err)
	}

	if err := goose.UpTo(conn, ".", 40); err != nil {
		t.Fatal(err)
	}
	queryConn, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer queryConn.Close(ctx) //nolint:errcheck
	if _, err := queryConn.Exec(ctx, "SET search_path TO "+quoted+", public"); err != nil {
		t.Fatal(err)
	}
	queries := dbgen.New(queryConn)
	selected, err := queries.ReplaceOIDCAppGroups(ctx, dbgen.ReplaceOIDCAppGroupsParams{OidcClientID: "oidc-one", GroupIds: []int32{oidcManual, forwardRule}})
	if err != nil || len(selected) != 2 {
		t.Fatalf("replace selected groups = %+v, err=%v", selected, err)
	}
	before, err := queries.ListOIDCAppGroups(ctx, "oidc-one")
	if err != nil {
		t.Fatal(err)
	}
	_, err = queries.ReplaceOIDCAppGroups(ctx, dbgen.ReplaceOIDCAppGroupsParams{OidcClientID: "oidc-one", GroupIds: []int32{oidcManual, 999999}})
	var replaceErr *pgconn.PgError
	if !errors.As(err, &replaceErr) || replaceErr.Code != "23503" {
		t.Fatalf("unknown group replacement error = %v", err)
	}
	after, err := queries.ListOIDCAppGroups(ctx, "oidc-one")
	if err != nil || len(after) != len(before) {
		t.Fatalf("links changed after failed replacement: before=%+v after=%+v err=%v", before, after, err)
	}
	for table, want := range map[string]int{"oidc_client_group": 3, "saml_sp_group": 1} {
		var got int
		if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&got); err != nil || got != want {
			t.Fatalf("%s count = %d, want %d (err=%v)", table, got, want, err)
		}
	}
	var decisionEffect string
	if err := conn.QueryRowContext(ctx, `SELECT effect FROM group_manual_decision WHERE group_id=$1 AND account_id=$2`, oidcManual, accountID).Scan(&decisionEffect); err != nil || decisionEffect != "deny" {
		t.Fatalf("manual decision = %q, want deny (err=%v)", decisionEffect, err)
	}

	if _, err := conn.ExecContext(ctx, `INSERT INTO oidc_client_group (client_id,group_id) VALUES ('forward-one',$1)`, oidcManual); err != nil {
		t.Fatal(err)
	}
	if err := goose.DownTo(conn, ".", 39); err == nil || !strings.Contains(err.Error(), "exactly one application") {
		t.Fatalf("shared-group Down error = %v, want lossless downgrade refusal", err)
	}
	var version int64
	if err := conn.QueryRowContext(ctx, `SELECT max(version_id) FILTER (WHERE is_applied) FROM goose_db_version`).Scan(&version); err != nil || version != 40 {
		t.Fatalf("version after refused Down = %d, want 40 (err=%v)", version, err)
	}

	if _, err := conn.ExecContext(ctx, `DELETE FROM oidc_client_group WHERE client_id='forward-one' AND group_id=$1`, oidcManual); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM oidc_client WHERE client_id='oidc-one'`); err != nil {
		t.Fatal(err)
	}
	var retained bool
	if err := conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_group WHERE id=$1)`, oidcManual).Scan(&retained); err != nil || !retained {
		t.Fatalf("group retained after app deletion = %t, want true (err=%v)", retained, err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO oidc_client_group (client_id,group_id) VALUES ('forward-one',$1)`, oidcManual); err != nil {
		t.Fatal(err)
	}
	_, err = conn.ExecContext(ctx, `DELETE FROM user_group WHERE id=$1`, oidcManual)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23001" {
		t.Fatalf("referenced group deletion error = %v, want foreign-key rejection", err)
	}
}

func TestGlobalUserGroupsMigrationDownWhenLossless(t *testing.T) {
	baseURL := os.Getenv("PROHIBITORUM_TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("PROHIBITORUM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	schema := "global_groups_down_" + hex.EncodeToString(nonce[:])
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE") //nolint:errcheck
	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	conn, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	goose.SetBaseFS(embedMigrations)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, ".", 39); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+quoted+", public"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO oidc_client (client_id,display_name,redirect_uris) VALUES ('lossless','Lossless',ARRAY['https://lossless.example/cb'])`); err != nil {
		t.Fatal(err)
	}
	var groupID int32
	if err := conn.QueryRowContext(ctx, `INSERT INTO user_group (kind,slug,display_name,rule,oidc_client_id) VALUES ('rule','lossless','Lossless','{}','lossless') RETURNING id`).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, ".", 40); err != nil {
		t.Fatal(err)
	}
	if err := goose.DownTo(conn, ".", 39); err != nil {
		t.Fatal(err)
	}
	var clientID string
	if err := conn.QueryRowContext(ctx, `SELECT oidc_client_id FROM user_group WHERE id=$1`, groupID).Scan(&clientID); err != nil || clientID != "lossless" {
		t.Fatalf("restored binding = %q, want lossless (err=%v)", clientID, err)
	}
}
