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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"

	dbgen "prohibitorum/pkg/db"
)

func TestAppManagerRuleGroupsMigrationPostgres(t *testing.T) {
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
	if err := goose.UpTo(conn, ".", 31); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+quotedSchema+", public"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, ".", 33); err != nil {
		t.Fatal(err)
	}
	defer conn.Close()


	var legacyAccountID int32
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO account (username, display_name, webauthn_user_handle)
		VALUES ('legacy-member', 'Legacy Member', decode('010203', 'hex'))
		RETURNING id`).Scan(&legacyAccountID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO oidc_client (client_id, display_name, redirect_uris, access_restricted)
		VALUES ('legacy-oidc', 'Legacy OIDC', ARRAY['https://legacy.example/callback'], true)`); err != nil {
		t.Fatal(err)
	}
	var legacySAMLID int64
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO saml_sp (entity_id, display_name, allow_idp_initiated, access_restricted)
		VALUES ('https://legacy.example/saml', 'Legacy SAML', true, true)
		RETURNING id`).Scan(&legacySAMLID); err != nil {
		t.Fatal(err)
	}
	var legacyGroupID int32
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO user_group (slug, display_name)
		VALUES ('legacy', 'Legacy Group')
		RETURNING id`).Scan(&legacyGroupID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO group_member (group_id, account_id) VALUES ($1, $2)`, legacyGroupID, legacyAccountID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO oidc_client_access (client_id, group_id) VALUES ('legacy-oidc', $1)`, legacyGroupID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO oidc_client_access (client_id, account_id) VALUES ('legacy-oidc', $1)`, legacyAccountID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO saml_sp_access (saml_sp_id, group_id) VALUES ($1, $2)`, legacySAMLID, legacyGroupID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO saml_sp_access (saml_sp_id, account_id) VALUES ($1, $2)`, legacySAMLID, legacyAccountID); err != nil {
		t.Fatal(err)
	}

	if err := goose.UpTo(conn, ".", 34); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{"group_member", "oidc_client_access", "saml_sp_access"} {
		var exists bool
		err := conn.QueryRowContext(ctx, `
			SELECT to_regclass(current_schema() || '.' || $1) IS NOT NULL`, table,
		).Scan(&exists)
		if err != nil || exists {
			t.Fatalf("legacy table %s still exists: exists=%v err=%v", table, exists, err)
		}
	}

	var groupCount int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM user_group`).Scan(&groupCount); err != nil {
		t.Fatal(err)
	}
	if groupCount != 0 {
		t.Fatalf("new user_group retained %d legacy rows, want 0", groupCount)
	}

	var oidcRestricted, samlRestricted bool
	if err := conn.QueryRowContext(ctx, `SELECT access_restricted FROM oidc_client WHERE client_id = 'legacy-oidc'`).Scan(&oidcRestricted); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT access_restricted FROM saml_sp WHERE entity_id = 'https://legacy.example/saml'`).Scan(&samlRestricted); err != nil {
		t.Fatal(err)
	}
	if oidcRestricted || samlRestricted {
		t.Fatal("cutover must reset every app open")
	}

	assertConstraintRejected := func(name, code, constraint, statement string, args ...any) {
		t.Helper()
		_, err := conn.ExecContext(ctx, statement, args...)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != code || pgErr.ConstraintName != constraint {
			t.Errorf("%s error = %v, want PostgreSQL %s from %s", name, err, code, constraint)
		}
	}

	var managerID int32
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO account (username, display_name, webauthn_user_handle, role)
		VALUES ('manager', 'Manager', decode('aabb', 'hex'), 'app_manager')
		RETURNING id`).Scan(&managerID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO enrollment (token, intent, template_role, expires_at)
		VALUES ('manager-invite', 'invite', 'app_manager', now() + interval '1 hour')`); err != nil {
		t.Fatal(err)
	}
	assertConstraintRejected("invalid account role", "23514", "account_role_check", `
		INSERT INTO account (username, display_name, webauthn_user_handle, role)
		VALUES ('invalid-role', 'Invalid Role', decode('ccdd', 'hex'), 'owner')`)
	assertConstraintRejected("invalid enrollment role", "23514", "enrollment_template_role_check", `
		INSERT INTO enrollment (token, intent, template_role, expires_at)
		VALUES ('invalid-role', 'invite', 'owner', now() + interval '1 hour')`)

	var oidcManualID, samlManualID, oidcRuleID int32
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO user_group (kind, slug, display_name, oidc_client_id)
		VALUES ('manual', 'manual', 'Manual', 'legacy-oidc')
		RETURNING id`).Scan(&oidcManualID); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO user_group (kind, slug, display_name, saml_sp_id)
		VALUES ('manual', 'manual', 'Manual', $1)
		RETURNING id`, legacySAMLID).Scan(&samlManualID); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO user_group (kind, slug, display_name, rule, oidc_client_id)
		VALUES ('rule', 'passkeys', 'Passkeys', '{"version":1,"condition":{"fact":"login_method","method":"passkey"}}', 'legacy-oidc')
		RETURNING id`).Scan(&oidcRuleID); err != nil {
		t.Fatal(err)
	}

	queryConn, err := pgx.Connect(ctx, schemaURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer queryConn.Close(ctx) //nolint:errcheck
	if _, err := queryConn.Exec(ctx, "SET search_path TO "+quotedSchema+", public"); err != nil {
		t.Fatal(err)
	}
	queries := dbgen.New(queryConn)
	updatedGroup, err := queries.UpdateAppGroup(ctx, dbgen.UpdateAppGroupParams{
		Slug:                "passkeys-updated",
		DisplayName:         "Updated Passkeys",
		Description:         pgtype.Text{},
		ExposedToDownstream: true,
		Rule:                []byte(`{"version":1,"condition":{"fact":"login_method","method":"passkey"}}`),
		GroupID:             oidcRuleID,
		OidcClientID:        pgtype.Text{String: "legacy-oidc", Valid: true},
		SamlSpID:            pgtype.Int8{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updatedGroup.Slug != "passkeys-updated" {
		t.Fatalf("updated group slug = %q, want passkeys-updated", updatedGroup.Slug)
	}
	_, err = queries.UpdateAppGroup(ctx, dbgen.UpdateAppGroupParams{
		Slug:                "cross-app-update",
		DisplayName:         "Cross App Update",
		Description:         pgtype.Text{},
		ExposedToDownstream: true,
		Rule:                []byte(`{"version":1,"condition":{"fact":"login_method","method":"passkey"}}`),
		GroupID:             oidcRuleID,
		OidcClientID:        pgtype.Text{},
		SamlSpID:            pgtype.Int8{Int64: legacySAMLID, Valid: true},
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-app group update error = %v, want pgx.ErrNoRows", err)
	}

	assertConstraintRejected("second OIDC manual group", "23505", "user_group_oidc_manual_uq", `
		INSERT INTO user_group (kind, slug, display_name, oidc_client_id)
		VALUES ('manual', 'second', 'Second', 'legacy-oidc')`)
	assertConstraintRejected("second SAML manual group", "23505", "user_group_saml_manual_uq", `
		INSERT INTO user_group (kind, slug, display_name, saml_sp_id)
		VALUES ('manual', 'second', 'Second', $1)`, legacySAMLID)
	assertConstraintRejected("duplicate OIDC slug", "23505", "user_group_oidc_slug_uq", `
		INSERT INTO user_group (kind, slug, display_name, rule, oidc_client_id)
		VALUES ('rule', 'passkeys-updated', 'Duplicate', '{}', 'legacy-oidc')`)
	assertConstraintRejected("duplicate SAML slug", "23505", "user_group_saml_slug_uq", `
		INSERT INTO user_group (kind, slug, display_name, rule, saml_sp_id)
		VALUES ('rule', 'manual', 'Duplicate', '{}', $1)`, legacySAMLID)
	assertConstraintRejected("invalid group slug", "23514", "user_group_slug_check", `
		INSERT INTO user_group (kind, slug, display_name, rule, oidc_client_id)
		VALUES ('rule', 'not_valid', 'Invalid Slug', '{}', 'legacy-oidc')`)
	assertConstraintRejected("invalid group kind", "23514", "user_group_kind_check", `
		INSERT INTO user_group (kind, slug, display_name, rule, oidc_client_id)
		VALUES ('dynamic', 'dynamic', 'Dynamic', '{}', 'legacy-oidc')`)
	assertConstraintRejected("group without app binding", "23514", "user_group_app_binding_check", `
		INSERT INTO user_group (kind, slug, display_name, rule)
		VALUES ('rule', 'unbound', 'Unbound', '{}')`)
	assertConstraintRejected("group with two app bindings", "23514", "user_group_app_binding_check", `
		INSERT INTO user_group (kind, slug, display_name, rule, oidc_client_id, saml_sp_id)
		VALUES ('rule', 'two-apps', 'Two Apps', '{}', 'legacy-oidc', $1)`, legacySAMLID)
	assertConstraintRejected("manual group with a rule", "23514", "user_group_rule_shape_check", `
		INSERT INTO user_group (kind, slug, display_name, rule, saml_sp_id)
		VALUES ('manual', 'manual-rule', 'Manual Rule', '{}', $1)`, legacySAMLID)
	assertConstraintRejected("rule group without a rule", "23514", "user_group_rule_shape_check", `
		INSERT INTO user_group (kind, slug, display_name, oidc_client_id)
		VALUES ('rule', 'empty-rule', 'Empty Rule', 'legacy-oidc')`)
	assertConstraintRejected("rule group with a scalar rule", "23514", "user_group_rule_shape_check", `
		INSERT INTO user_group (kind, slug, display_name, rule, oidc_client_id)
		VALUES ('rule', 'scalar-rule', 'Scalar Rule', 'true'::jsonb, 'legacy-oidc')`)
	assertConstraintRejected("manual decision on rule group", "23503", "group_manual_decision_group_fkey", `
		INSERT INTO group_manual_decision (group_id, account_id, effect)
		VALUES ($1, $2, 'allow')`, oidcRuleID, legacyAccountID)
	assertConstraintRejected("invalid manual decision effect", "23514", "group_manual_decision_effect_check", `
		INSERT INTO group_manual_decision (group_id, account_id, effect)
		VALUES ($1, $2, 'maybe')`, oidcManualID, legacyAccountID)

	var secondaryManagerID int32
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO account (username, display_name, webauthn_user_handle)
		VALUES ('secondary-manager', 'Secondary Manager', decode('eeff', 'hex'))
		RETURNING id`).Scan(&secondaryManagerID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO oidc_client_manager (client_id, account_id, created_by)
		VALUES ('legacy-oidc', $1, $1), ('legacy-oidc', $2, $1)`, managerID, secondaryManagerID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO saml_sp_manager (saml_sp_id, account_id, created_by)
		VALUES ($1, $2, $2)`, legacySAMLID, managerID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO group_manual_decision (group_id, account_id, effect, created_by)
		VALUES ($1, $3, 'allow', $3), ($2, $3, 'deny', $3)`, oidcManualID, samlManualID, legacyAccountID); err != nil {
		t.Fatal(err)
	}

	if _, err := conn.ExecContext(ctx, `DELETE FROM account WHERE id = $1`, legacyAccountID); err != nil {
		t.Fatal(err)
	}
	var decisionCount int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM group_manual_decision`).Scan(&decisionCount); err != nil {
		t.Fatal(err)
	}
	if decisionCount != 0 {
		t.Fatalf("manual decisions after account deletion = %d, want 0", decisionCount)
	}

	var cascadeTargetID int32
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO account (username, display_name, webauthn_user_handle)
		VALUES ('cascade-target', 'Cascade Target', decode('1122', 'hex'))
		RETURNING id`).Scan(&cascadeTargetID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO group_manual_decision (group_id, account_id, effect, created_by)
		VALUES ($1, $3, 'allow', $4), ($2, $3, 'deny', $4)`, oidcManualID, samlManualID, cascadeTargetID, managerID); err != nil {
		t.Fatal(err)
	}

	if _, err := conn.ExecContext(ctx, `DELETE FROM account WHERE id = $1`, managerID); err != nil {
		t.Fatal(err)
	}
	var oidcManagerCount, samlManagerCount, nullCreatorCount int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM oidc_client_manager`).Scan(&oidcManagerCount); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM saml_sp_manager`).Scan(&samlManagerCount); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM group_manual_decision WHERE created_by IS NULL`).Scan(&nullCreatorCount); err != nil {
		t.Fatal(err)
	}
	if oidcManagerCount != 1 || samlManagerCount != 0 || nullCreatorCount != 2 {
		t.Fatalf("account cascades/set-null = OIDC managers %d, SAML managers %d, null decision creators %d; want 1, 0, 2", oidcManagerCount, samlManagerCount, nullCreatorCount)
	}

	if _, err := conn.ExecContext(ctx, `DELETE FROM oidc_client WHERE client_id = 'legacy-oidc'`); err != nil {
		t.Fatal(err)
	}
	var oidcGroupCount, oidcDecisionCount int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM user_group WHERE oidc_client_id = 'legacy-oidc'`).Scan(&oidcGroupCount); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM group_manual_decision WHERE group_id = $1`, oidcManualID).Scan(&oidcDecisionCount); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM oidc_client_manager`).Scan(&oidcManagerCount); err != nil {
		t.Fatal(err)
	}
	if oidcGroupCount != 0 || oidcDecisionCount != 0 || oidcManagerCount != 0 {
		t.Fatalf("OIDC app cascades = groups %d, decisions %d, managers %d; want 0, 0, 0", oidcGroupCount, oidcDecisionCount, oidcManagerCount)
	}

	if _, err := conn.ExecContext(ctx, `DELETE FROM saml_sp WHERE id = $1`, legacySAMLID); err != nil {
		t.Fatal(err)
	}
	var samlGroupCount, samlDecisionCount int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM user_group WHERE saml_sp_id = $1`, legacySAMLID).Scan(&samlGroupCount); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM group_manual_decision WHERE group_id = $1`, samlManualID).Scan(&samlDecisionCount); err != nil {
		t.Fatal(err)
	}
	if samlGroupCount != 0 || samlDecisionCount != 0 {
		t.Fatalf("SAML app cascades = groups %d, decisions %d; want 0, 0", samlGroupCount, samlDecisionCount)
	}

	if _, err := conn.ExecContext(ctx, `
		INSERT INTO oidc_client (client_id, display_name, redirect_uris, access_restricted)
		VALUES ('rollback-oidc', 'Rollback OIDC', ARRAY['https://rollback.example/callback'], true)`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO saml_sp (entity_id, display_name, access_restricted)
		VALUES ('https://rollback.example/saml', 'Rollback SAML', true)`); err != nil {
		t.Fatal(err)
	}

	if err := goose.DownTo(conn, ".", 33); err != nil {
		t.Fatal(err)
	}
	var version int64
	if err := conn.QueryRowContext(ctx, `
		SELECT version_id
		FROM goose_db_version
		WHERE is_applied
		ORDER BY id DESC
		LIMIT 1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 33 {
		t.Fatalf("migration version after down = %d, want 33", version)
	}

	var rollbackOIDCRestricted, rollbackSAMLRestricted bool
	if err := conn.QueryRowContext(ctx, `
		SELECT access_restricted
		FROM oidc_client
		WHERE client_id = 'rollback-oidc'`).Scan(&rollbackOIDCRestricted); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, `
		SELECT access_restricted
		FROM saml_sp
		WHERE entity_id = 'https://rollback.example/saml'`).Scan(&rollbackSAMLRestricted); err != nil {
		t.Fatal(err)
	}
	if rollbackOIDCRestricted || rollbackSAMLRestricted {
		t.Fatal("down migration must reset apps open when legacy grants cannot be restored")
	}

	for _, table := range []string{"user_group", "group_member", "oidc_client_access", "saml_sp_access"} {
		var exists bool
		err := conn.QueryRowContext(ctx, `
			SELECT to_regclass(current_schema() || '.' || $1) IS NOT NULL`, table,
		).Scan(&exists)
		if err != nil || !exists {
			t.Fatalf("legacy table %s was not restored: exists=%v err=%v", table, exists, err)
		}
	}
	for _, table := range []string{"group_manual_decision", "oidc_client_manager", "saml_sp_manager"} {
		var exists bool
		err := conn.QueryRowContext(ctx, `
			SELECT to_regclass(current_schema() || '.' || $1) IS NOT NULL`, table,
		).Scan(&exists)
		if err != nil || exists {
			t.Fatalf("new table %s remains after down: exists=%v err=%v", table, exists, err)
		}
	}

	var kindColumnExists bool
	if err := conn.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'user_group'
			  AND column_name = 'kind'
		)`).Scan(&kindColumnExists); err != nil {
		t.Fatal(err)
	}
	if kindColumnExists {
		t.Fatal("down migration retained app-bound user_group columns")
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO user_group (slug, display_name)
		VALUES ('legacy-again', 'Legacy Again')`); err != nil {
		t.Fatalf("restored legacy user_group shape rejected an insert: %v", err)
	}

	var managerInviteRole string
	if err := conn.QueryRowContext(ctx, `
		SELECT template_role
		FROM enrollment
		WHERE token = 'manager-invite'`).Scan(&managerInviteRole); err != nil {
		t.Fatal(err)
	}
	if managerInviteRole != "user" {
		t.Fatalf("app_manager enrollment role after down = %q, want user", managerInviteRole)
	}
	assertConstraintRejected("app_manager account role after down", "23514", "account_role_check", `
		INSERT INTO account (username, display_name, webauthn_user_handle, role)
		VALUES ('manager-after-down', 'Manager After Down', decode('3344', 'hex'), 'app_manager')`)
	assertConstraintRejected("app_manager enrollment role after down", "23514", "enrollment_template_role_check", `
		INSERT INTO enrollment (token, intent, template_role, expires_at)
		VALUES ('manager-after-down', 'invite', 'app_manager', now() + interval '1 hour')`)
}
