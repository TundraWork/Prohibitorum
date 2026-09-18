package migrations

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
)

func TestApplicationAssignmentRolesMigrationPostgres(t *testing.T) {
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
	schema := "assignment_roles_" + hex.EncodeToString(nonce[:])
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE") //nolint:errcheck
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
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	goose.SetBaseFS(embedMigrations)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, ".", 31); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+quoted+", public"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, ".", 42); err != nil {
		t.Fatal(err)
	}
	mustExec := func(statement string) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	mustExec("INSERT INTO account (username, display_name, webauthn_user_handle, role) VALUES ('assigned', 'Assigned', decode('01','hex'), 'app_manager'), ('unassigned', 'Unassigned', decode('02','hex'), 'app_manager'), ('user', 'User', decode('03','hex'), 'user'), ('admin', 'Admin', decode('04','hex'), 'admin')")
	mustExec("INSERT INTO enrollment (token, intent, template_role, expires_at) VALUES ('manager', 'invite', 'app_manager', now() + interval '1 hour'), ('user', 'invite', 'user', now() + interval '1 hour'), ('admin', 'invite', 'admin', now() + interval '1 hour'), ('bootstrap', 'bootstrap', NULL, now() + interval '1 hour')")
	mustExec("INSERT INTO oidc_client (client_id, display_name, redirect_uris, forward_auth_enabled) VALUES ('oidc', 'OIDC', ARRAY['https://oidc.test/cb'], false), ('fa', 'Forward auth', ARRAY['https://fa.test/cb'], true)")
	mustExec("INSERT INTO saml_sp (entity_id, display_name) VALUES ('https://saml.test', 'SAML')")
	mustExec("INSERT INTO oidc_client_manager (client_id, account_id, created_by) SELECT c.client_id, a.id, creator.id FROM oidc_client c CROSS JOIN account a CROSS JOIN account creator WHERE a.username <> 'unassigned' AND creator.username = 'admin'")
	mustExec("INSERT INTO saml_sp_manager (saml_sp_id, account_id, created_by) SELECT s.id, a.id, creator.id FROM saml_sp s CROSS JOIN account a CROSS JOIN account creator WHERE a.username <> 'unassigned' AND creator.username = 'admin'")

	assignments := func(table string, order string) string {
		t.Helper()
		var snapshot string
		if err := conn.QueryRowContext(ctx, "SELECT jsonb_agg(to_jsonb(m) ORDER BY "+order+")::text FROM "+table+" m").Scan(&snapshot); err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	oidcBefore := assignments("oidc_client_manager", "client_id, account_id")
	samlBefore := assignments("saml_sp_manager", "saml_sp_id, account_id")
	assertPreserved := func() {
		t.Helper()
		if got := assignments("oidc_client_manager", "client_id, account_id"); got != oidcBefore {
			t.Fatalf("OIDC/forward-auth assignments changed: %s, want %s", got, oidcBefore)
		}
		if got := assignments("saml_sp_manager", "saml_sp_id, account_id"); got != samlBefore {
			t.Fatalf("SAML assignments changed: %s, want %s", got, samlBefore)
		}
		for username, want := range map[string]string{"assigned": "user", "unassigned": "user", "user": "user", "admin": "admin"} {
			var got string
			if err := conn.QueryRowContext(ctx, "SELECT role FROM account WHERE username=$1", username).Scan(&got); err != nil || got != want {
				t.Fatalf("account %s role = %q, want %q (err=%v)", username, got, want, err)
			}
		}
		for token, want := range map[string]string{"manager": "user", "user": "user", "admin": "admin", "bootstrap": "<null>"} {
			var got string
			if err := conn.QueryRowContext(ctx, "SELECT COALESCE(template_role, '<null>') FROM enrollment WHERE token=$1", token).Scan(&got); err != nil || got != want {
				t.Fatalf("enrollment %s role = %q, want %q (err=%v)", token, got, want, err)
			}
		}
	}
	rejectRole := func(statement, constraint string) {
		t.Helper()
		_, err := conn.ExecContext(ctx, statement)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != constraint {
			t.Fatalf("expected %s check violation, got %v", constraint, err)
		}
	}
	assertConstraints := func() {
		t.Helper()
		rejectRole("UPDATE account SET role='app_manager' WHERE username='user'", "account_role_check")
		rejectRole("UPDATE enrollment SET template_role='app_manager' WHERE token='user'", "enrollment_template_role_check")
		rejectRole("INSERT INTO account (username, display_name, webauthn_user_handle, role) VALUES ('invalid', 'Invalid', decode('05','hex'), 'app_manager')", "account_role_check")
		rejectRole("INSERT INTO enrollment (token, intent, template_role, expires_at) VALUES ('invalid', 'invite', 'app_manager', now() + interval '1 hour')", "enrollment_template_role_check")
	}
	if err := goose.UpTo(conn, ".", 43); err != nil {
		t.Fatal(err)
	}
	assertPreserved()
	assertConstraints()

	if err := goose.DownTo(conn, ".", 42); err != nil {
		t.Fatal(err)
	}
	assertPreserved()
	// Rollback restores accepted inputs, not the roles that were converted.
	mustExec("UPDATE account SET role='app_manager' WHERE username='assigned'")
	mustExec("UPDATE enrollment SET template_role='app_manager' WHERE token='manager'")
	rejectRole("UPDATE account SET role='invalid' WHERE username='user'", "account_role_check")
	rejectRole("UPDATE enrollment SET template_role='invalid' WHERE token='user'", "enrollment_template_role_check")
	if err := goose.UpTo(conn, ".", 43); err != nil {
		t.Fatal(err)
	}
	assertPreserved()
	assertConstraints()
}
