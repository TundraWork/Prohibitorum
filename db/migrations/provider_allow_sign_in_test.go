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

func TestProviderAllowSignInMigrationPostgres(t *testing.T) {
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
	schema := "migration_" + hex.EncodeToString(nonce[:])
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "DROP SCHEMA "+quotedSchema+" CASCADE") //nolint:errcheck

	schemaURL, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := schemaURL.Query()
	query.Set("search_path", schema)
	schemaURL.RawQuery = query.Encode()

	goose.SetBaseFS(embedMigrations)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	conn, err := sql.Open("pgx", schemaURL.String())
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(1)
	// 032 needs pg_trgm, which lives in public (mirrors the 033 test's
	// two-phase search_path dance).
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := goose.UpTo(conn, ".", 31); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+quotedSchema+", public"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, ".", 48); err != nil {
		t.Fatal(err)
	}

	var accountID int32
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO account (username, display_name, webauthn_user_handle)
		VALUES ('allow-sign-in-user', 'Allow Sign In User', decode('010203', 'hex'))
		RETURNING id`).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	insertProvider := func(slug, displayName, protocol, mode string) int64 {
		t.Helper()
		var id int64
		if err := conn.QueryRowContext(ctx, `
			INSERT INTO upstream_idp (
				slug, display_name, protocol, mode, provider_config, secret_status,
				secret_enc, secret_nonce, key_version, disabled
			)
			VALUES ($1, $2, $3, $4, '{}'::jsonb, 'unconfigured', NULL, NULL, NULL, false)
			RETURNING id`, slug, displayName, protocol, mode).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	oidcID := insertProvider("oidc-signin", "OIDC", "oidc", "auto_provision")
	vrchatID := insertProvider("vrchat-signin", "VRChat", "vrchat", "link_only")

	// At 048 both special enrollment shapes still exist; 049 must delete them
	// while leaving plain invites untouched.
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO enrollment (token, intent, expires_at, federated_upstream_idp_id,
			federated_upstream_idp_slug, federated_upstream_iss, federated_upstream_sub,
			federated_display_name, federated_upstream_data)
		VALUES ('pending-register', 'federated_register', now() + interval '1 hour', $1,
			'vrchat-signin', 'https://vrchat.com', 'usr_pending', 'Pending User',
			'{"userId":"usr_pending"}'::jsonb)`, vrchatID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO enrollment (token, intent, target_account_id, recovery_source_upstream_idp_id, expires_at)
		VALUES ('pending-recovery', 'reset', $1, $2, now() + interval '1 hour')`,
		accountID, vrchatID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO enrollment (token, intent, template_role, expires_at)
		VALUES ('plain-invite', 'invite', 'user', now() + interval '1 hour')`); err != nil {
		t.Fatal(err)
	}

	if err := goose.UpTo(conn, ".", 49); err != nil {
		t.Fatal(err)
	}

	readAllowSignIn := func(id int64) bool {
		t.Helper()
		var allow bool
		if err := conn.QueryRowContext(ctx, `SELECT allow_sign_in FROM upstream_idp WHERE id = $1`, id).Scan(&allow); err != nil {
			t.Fatal(err)
		}
		return allow
	}
	if readAllowSignIn(vrchatID) {
		t.Errorf("migrated VRChat allow_sign_in = true, want false")
	}
	if !readAllowSignIn(oidcID) {
		t.Errorf("migrated OIDC allow_sign_in = false, want true")
	}
	for _, token := range []string{"pending-register", "pending-recovery"} {
		var count int
		if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM enrollment WHERE token = $1`, token).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("enrollment %q survived migration 049", token)
		}
	}
	var count int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM enrollment WHERE token = 'plain-invite'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("plain invite count after migration = %d, want 1", count)
	}

	assertCheckRejected := func(name, constraint, statement string, args ...any) {
		t.Helper()
		_, err := conn.ExecContext(ctx, statement, args...)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != constraint {
			t.Errorf("%s error = %v, want check violation for %s", name, err, constraint)
		}
	}
	assertAccepted := func(name, statement string, args ...any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, statement, args...); err != nil {
			t.Errorf("%s was rejected: %v", name, err)
		}
	}

	assertCheckRejected("auto_provision with sign-in disabled", "upstream_idp_sign_in_mode_check", `
		UPDATE upstream_idp SET allow_sign_in = false WHERE id = $1`, oidcID)
	assertAccepted("VRChat to invite_only", `UPDATE upstream_idp SET mode = 'invite_only' WHERE id = $1`, vrchatID)
	assertAccepted("VRChat sign-in enabled", `UPDATE upstream_idp SET allow_sign_in = true WHERE id = $1`, vrchatID)
	assertAccepted("VRChat sign-in disabled again", `UPDATE upstream_idp SET allow_sign_in = false WHERE id = $1`, vrchatID)

	assertCheckRejected("snapshot on non-invite", "enrollment_federated_snapshot_check", `
		INSERT INTO enrollment (token, intent, expires_at, federated_upstream_idp_slug)
		VALUES ('bootstrap-snapshot', 'bootstrap', now() + interval '1 hour', 'oidc-signin')`)
	const inviteSnapshotInsert = `
		INSERT INTO enrollment (
			token, intent, template_role, expires_at, federated_upstream_idp_id,
			federated_upstream_idp_slug, federated_upstream_iss, federated_upstream_sub,
			federated_display_name, federated_upstream_data
		)
		VALUES ($1, 'invite', 'user', now() + interval '1 hour', $2,
			'vrchat-signin', 'https://vrchat.com', $3, 'Invited User',
			$4::jsonb)`
	assertAccepted("full snapshot on invite", inviteSnapshotInsert,
		"invite-snapshot", vrchatID, "usr_invite", `{"userId":"usr_invite"}`)
	assertCheckRejected("partial snapshot on invite", "enrollment_federated_snapshot_check", `
		INSERT INTO enrollment (
			token, intent, template_role, expires_at, federated_upstream_idp_slug
		)
		VALUES ('invite-partial', 'invite', 'user', now() + interval '1 hour', 'vrchat-signin')`)
	assertCheckRejected("require_local_credential on non-invite", "enrollment_require_local_credential_check", `
		INSERT INTO enrollment (token, intent, expires_at, require_local_credential)
		VALUES ('bootstrap-local', 'bootstrap', now() + interval '1 hour', true)`)
	assertAccepted("require_local_credential on invite", `
		INSERT INTO enrollment (token, intent, template_role, expires_at, require_local_credential)
		VALUES ('invite-local', 'invite', 'user', now() + interval '1 hour', true)`)
	assertCheckRejected("federated_register intent after up", "enrollment_intent_check", `
		INSERT INTO enrollment (token, intent, expires_at)
		VALUES ('register-after-up', 'federated_register', now() + interval '1 hour')`)

	if err := goose.DownTo(conn, ".", 48); err != nil {
		t.Fatal(err)
	}
	var version int64
	if err := conn.QueryRowContext(ctx, `SELECT version_id FROM goose_db_version WHERE is_applied ORDER BY id DESC LIMIT 1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 48 {
		t.Fatalf("migration version after down = %d, want 48", version)
	}
	for _, column := range []string{"allow_sign_in", "require_local_credential"} {
		var onUpstream, onEnrollment bool
		if err := conn.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM information_schema.columns
				WHERE table_schema = $1 AND table_name = 'upstream_idp' AND column_name = $2)`,
			schema, column).Scan(&onUpstream); err != nil {
			t.Fatal(err)
		}
		if err := conn.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM information_schema.columns
				WHERE table_schema = $1 AND table_name = 'enrollment' AND column_name = $2)`,
			schema, column).Scan(&onEnrollment); err != nil {
			t.Fatal(err)
		}
		if onUpstream || onEnrollment {
			t.Errorf("column %q remains after migration down (upstream=%v enrollment=%v)", column, onUpstream, onEnrollment)
		}
	}
	// The recovery-source column is part of the 033 shape Down restores.
	var recoveryColumn bool
	if err := conn.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.columns
			WHERE table_schema = $1 AND table_name = 'enrollment' AND column_name = 'recovery_source_upstream_idp_id')`,
		schema).Scan(&recoveryColumn); err != nil {
		t.Fatal(err)
	}
	if !recoveryColumn {
		t.Error("recovery_source_upstream_idp_id missing after migration down")
	}
	for _, constraint := range []string{
		"upstream_idp_sign_in_mode_check",
		"enrollment_require_local_credential_check",
	} {
		var exists bool
		if err := conn.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_constraint c
				JOIN pg_class t ON t.oid = c.conrelid
				JOIN pg_namespace n ON n.oid = t.relnamespace
				WHERE n.nspname = $1 AND t.relname IN ('upstream_idp', 'enrollment') AND c.conname = $2
			)`, schema, constraint).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Errorf("constraint %q remains after migration down", constraint)
		}
	}
	var vrchatMode string
	if err := conn.QueryRowContext(ctx, `SELECT mode FROM upstream_idp WHERE id = $1`, vrchatID).Scan(&vrchatMode); err != nil {
		t.Fatal(err)
	}
	if vrchatMode != "link_only" {
		t.Errorf("VRChat mode after down = %q, want link_only", vrchatMode)
	}
	assertCheckRejected("VRChat non-link-only mode after down", "upstream_idp_vrchat_link_only_check", `
		UPDATE upstream_idp SET mode = 'invite_only' WHERE id = $1`, vrchatID)
	// At 033 semantics a snapshot-less federated_register row violates the
	// snapshot CHECK (which requires the full snapshot for that intent);
	// that is the constraint that fires first.
	assertCheckRejected("snapshot-less federated_register after down", "enrollment_federated_snapshot_check", `
		INSERT INTO enrollment (token, intent, expires_at)
		VALUES ('register-after-down', 'federated_register', now() + interval '1 hour')`)
}
