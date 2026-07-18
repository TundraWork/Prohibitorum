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

func TestVRChatLinkOnlyEnrollmentMigrationPostgres(t *testing.T) {
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
	if err := goose.UpTo(conn, ".", 32); err != nil {
		t.Fatal(err)
	}

	var accountID int32
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO account (username, display_name, webauthn_user_handle)
		VALUES ('migration-user', 'Migration User', decode('010203', 'hex'))
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
	vrchatID := insertProvider("vrchat-main", "VRChat", "vrchat", "auto_provision")
	oidcID := insertProvider("oidc-main", "OIDC", "oidc", "auto_provision")

	if _, err := conn.ExecContext(ctx, `
		INSERT INTO enrollment (token, intent, expires_at)
		VALUES ('legacy-bootstrap', 'bootstrap', now() + interval '1 hour')`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO enrollment (token, intent, template_role, expires_at)
		VALUES ('legacy-invite', 'invite', 'user', now() + interval '1 hour')`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO enrollment (token, intent, target_account_id, expires_at)
		VALUES ('legacy-reset', 'reset', $1, now() + interval '1 hour')`, accountID); err != nil {
		t.Fatal(err)
	}

	if err := goose.UpTo(conn, ".", 33); err != nil {
		t.Fatal(err)
	}

	var mode string
	if err := conn.QueryRowContext(ctx, `SELECT mode FROM upstream_idp WHERE id = $1`, vrchatID).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "link_only" {
		t.Fatalf("migrated VRChat mode = %q, want link_only", mode)
	}
	var legacyEnrollmentCount int
	if err := conn.QueryRowContext(ctx, `
		SELECT count(*) FROM enrollment
		WHERE token IN ('legacy-bootstrap', 'legacy-invite', 'legacy-reset')`).Scan(&legacyEnrollmentCount); err != nil {
		t.Fatal(err)
	}
	if legacyEnrollmentCount != 3 {
		t.Fatalf("legacy enrollment count after migration = %d, want 3", legacyEnrollmentCount)
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

	assertCheckRejected("VRChat auto_provision insert", "upstream_idp_vrchat_link_only_check", `
		INSERT INTO upstream_idp (
			slug, display_name, protocol, mode, provider_config, secret_status,
			secret_enc, secret_nonce, key_version
		)
		VALUES (
			'vrchat-auto', 'VRChat Auto', 'vrchat', 'auto_provision', '{}'::jsonb,
			'unconfigured', NULL, NULL, NULL
		)`)
	assertCheckRejected("VRChat invite_only insert", "upstream_idp_vrchat_link_only_check", `
		INSERT INTO upstream_idp (
			slug, display_name, protocol, mode, provider_config, secret_status,
			secret_enc, secret_nonce, key_version
		)
		VALUES (
			'vrchat-invite', 'VRChat Invite', 'vrchat', 'invite_only', '{}'::jsonb,
			'unconfigured', NULL, NULL, NULL
		)`)
	assertCheckRejected("VRChat invalid update", "upstream_idp_vrchat_link_only_check", `UPDATE upstream_idp SET mode = 'auto_provision' WHERE id = $1`, vrchatID)
	assertCheckRejected("VRChat invite_only update", "upstream_idp_vrchat_link_only_check", `UPDATE upstream_idp SET mode = 'invite_only' WHERE id = $1`, vrchatID)
	assertAccepted("OIDC invite_only update", `UPDATE upstream_idp SET mode = 'invite_only' WHERE id = $1`, oidcID)
	assertAccepted("OIDC link_only update", `UPDATE upstream_idp SET mode = 'link_only' WHERE id = $1`, oidcID)
	assertAccepted("non-VRChat auto_provision insert", `
		INSERT INTO upstream_idp (
			slug, display_name, protocol, mode, provider_config, secret_status,
			secret_enc, secret_nonce, key_version
		)
		VALUES (
			'oidc-auto', 'OIDC Auto', 'oidc', 'auto_provision', '{}'::jsonb,
			'unconfigured', NULL, NULL, NULL
		)`)

	const validFederatedInsert = `
		INSERT INTO enrollment (
			token, intent, expires_at, federated_upstream_idp_id,
			federated_upstream_idp_slug, federated_upstream_iss,
			federated_upstream_sub, federated_display_name,
			federated_upstream_data, federated_avatar_url
		)
		VALUES ($1, 'federated_register', now() + interval '1 hour', $2,
			'vrchat-main', 'https://vrchat.com', 'usr_123', 'VRChat User',
			'{"userId":"usr_123"}'::jsonb, 'https://api.vrchat.cloud/api/1/file/avatar')`
	assertAccepted("valid federated registration", validFederatedInsert, "federated-valid", vrchatID)
	assertAccepted("federated registration without avatar", `
		INSERT INTO enrollment (
			token, intent, expires_at, federated_upstream_idp_id,
			federated_upstream_idp_slug, federated_upstream_iss,
			federated_upstream_sub, federated_display_name, federated_upstream_data
		)
		VALUES (
			'federated-no-avatar', 'federated_register', now() + interval '1 hour', $1,
			'vrchat-main', 'https://vrchat.com', 'usr_no_avatar', 'No Avatar',
			'{"userId":"usr_no_avatar"}'::jsonb
		)`, vrchatID)
	assertCheckRejected("federated registration missing snapshot", "enrollment_federated_snapshot_check", `
		INSERT INTO enrollment (token, intent, expires_at)
		VALUES ('federated-empty', 'federated_register', now() + interval '1 hour')`)
	assertCheckRejected("federated registration array metadata", "enrollment_federated_snapshot_check", strings.Replace(validFederatedInsert, `'{"userId":"usr_123"}'::jsonb`, `'[]'::jsonb`, 1), "federated-array", vrchatID)
	assertCheckRejected("federated registration oversized metadata", "enrollment_federated_snapshot_check", strings.Replace(validFederatedInsert, `'{"userId":"usr_123"}'::jsonb`, `jsonb_build_object('data', repeat('x', 4097))`, 1), "federated-oversized", vrchatID)
	assertCheckRejected("federated registration with target account", "enrollment_federated_snapshot_check", `
		INSERT INTO enrollment (
			token, intent, target_account_id, expires_at, federated_upstream_idp_id,
			federated_upstream_idp_slug, federated_upstream_iss,
			federated_upstream_sub, federated_display_name, federated_upstream_data
		)
		VALUES (
			$1, 'federated_register', $2, now() + interval '1 hour', $3,
			'vrchat-main', 'https://vrchat.com', 'usr_123', 'VRChat User',
			'{"userId":"usr_123"}'::jsonb
		)`, "federated-target", accountID, vrchatID)
	assertCheckRejected("non-federated snapshot", "enrollment_federated_snapshot_check", `
		INSERT INTO enrollment (token, intent, expires_at, federated_upstream_idp_slug)
		VALUES ('bootstrap-snapshot', 'bootstrap', now() + interval '1 hour', 'vrchat-main')`)
	assertCheckRejected("bootstrap template data", "enrollment_template_intent_check", `
		INSERT INTO enrollment (token, intent, template_role, expires_at)
		VALUES ('bootstrap-template', 'bootstrap', 'user', now() + interval '1 hour')`)
	assertCheckRejected("invite target account", "enrollment_intent_target_check", `
		INSERT INTO enrollment (token, intent, target_account_id, expires_at)
		VALUES ('invite-target', 'invite', $1, now() + interval '1 hour')`, accountID)

	assertAccepted("provider-sourced reset", `
		INSERT INTO enrollment (
			token, intent, target_account_id, recovery_source_upstream_idp_id, expires_at
		)
		VALUES ('provider-reset', 'reset', $1, $2, now() + interval '1 hour')`, accountID, vrchatID)
	assertCheckRejected("recovery source on invite", "enrollment_recovery_source_check", `
		INSERT INTO enrollment (token, intent, recovery_source_upstream_idp_id, expires_at)
		VALUES ('invite-recovery', 'invite', $1, now() + interval '1 hour')`, vrchatID)
	assertCheckRejected("recovery source reset without target", "enrollment_recovery_source_check", `
		INSERT INTO enrollment (token, intent, recovery_source_upstream_idp_id, expires_at)
		VALUES ('reset-without-target', 'reset', $1, now() + interval '1 hour')`, vrchatID)

	queryConn, err := pgx.Connect(ctx, schemaURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer queryConn.Close(ctx) //nolint:errcheck
	queries := dbgen.New(queryConn)
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO account_identity (
			account_id, upstream_idp_id, upstream_iss, upstream_sub, upstream_data
		)
		VALUES ($1, $2, 'https://vrchat.com', 'usr_count', '{}'::jsonb)`,
		accountID, vrchatID); err != nil {
		t.Fatal(err)
	}
	federationCount, err := queries.CountUsableSignInFederation(ctx, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if federationCount != 0 {
		t.Errorf("usable federation count with only VRChat identity = %d, want 0", federationCount)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO account_identity (
			account_id, upstream_idp_id, upstream_iss, upstream_sub, upstream_data
		)
		VALUES ($1, $2, 'https://oidc.example.com', 'oidc_count', '{}'::jsonb)`,
		accountID, oidcID); err != nil {
		t.Fatal(err)
	}
	federationCount, err = queries.CountUsableSignInFederation(ctx, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if federationCount != 1 {
		t.Errorf("usable federation count with VRChat and OIDC identities = %d, want 1", federationCount)
	}
	if _, err := conn.ExecContext(ctx, `
		DELETE FROM account_identity WHERE upstream_idp_id = $1`, vrchatID); err != nil {
		t.Fatal(err)
	}

	if _, err := conn.ExecContext(ctx, `DELETE FROM upstream_idp WHERE id = $1`, vrchatID); err != nil {
		t.Fatal(err)
	}
	var providerBoundCount int
	if err := conn.QueryRowContext(ctx, `
		SELECT count(*) FROM enrollment
		WHERE token IN ('federated-valid', 'federated-no-avatar', 'provider-reset')`).Scan(&providerBoundCount); err != nil {
		t.Fatal(err)
	}
	if providerBoundCount != 0 {
		t.Errorf("provider-bound enrollment count after provider delete = %d, want 0", providerBoundCount)
	}

	if err := goose.DownTo(conn, ".", 32); err != nil {
		t.Fatal(err)
	}
	var version int64
	if err := conn.QueryRowContext(ctx, `SELECT version_id FROM goose_db_version WHERE is_applied ORDER BY id DESC LIMIT 1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 32 {
		t.Fatalf("migration version after down = %d, want 32", version)
	}
	for _, column := range []string{
		"federated_upstream_idp_id",
		"federated_upstream_idp_slug",
		"federated_upstream_iss",
		"federated_upstream_sub",
		"federated_display_name",
		"federated_upstream_data",
		"federated_avatar_url",
		"recovery_source_upstream_idp_id",
	} {
		var exists bool
		if err := conn.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = $1 AND table_name = 'enrollment' AND column_name = $2
			)`, schema, column).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Errorf("column %q remains after migration down", column)
		}
	}
	for _, constraint := range []string{
		"upstream_idp_vrchat_link_only_check",
		"enrollment_federated_snapshot_check",
		"enrollment_recovery_source_check",
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
	assertCheckRejected("federated intent after down", "enrollment_intent_check", `
		INSERT INTO enrollment (token, intent, expires_at)
		VALUES ('federated-after-down', 'federated_register', now() + interval '1 hour')`)
	assertAccepted("VRChat non-link-only mode after down", `
		INSERT INTO upstream_idp (
			slug, display_name, protocol, mode, provider_config, secret_status,
			secret_enc, secret_nonce, key_version
		)
		VALUES (
			'vrchat-after-down', 'VRChat After Down', 'vrchat', 'invite_only', '{}'::jsonb,
			'unconfigured', NULL, NULL, NULL
		)`)
}
