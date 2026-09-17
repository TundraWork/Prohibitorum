package migrations

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"

	dbgen "prohibitorum/pkg/db"
)

func TestInvitationUsernameGroupsMigrationPostgres(t *testing.T) {
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
	schema := "invitation_groups_" + hex.EncodeToString(nonce[:])
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
	if err := goose.UpTo(conn, ".", 41); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+quoted+", public"); err != nil {
		t.Fatal(err)
	}

	var creatorID int32
	if err := conn.QueryRowContext(ctx, "INSERT INTO account (username, display_name, webauthn_user_handle) VALUES ('invite-creator', 'Invite Creator', decode('010203', 'hex')) RETURNING id").Scan(&creatorID); err != nil {
		t.Fatal(err)
	}
	var manualID, ruleID int32
	if err := conn.QueryRowContext(ctx, "INSERT INTO user_group (kind,slug,display_name) VALUES ('manual','manual','Manual') RETURNING id").Scan(&manualID); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, "INSERT INTO user_group (kind,slug,display_name,rule) VALUES ('rule','rule','Rule','{}') RETURNING id").Scan(&ruleID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO enrollment (token,intent,expires_at,template_role,template_username,group_ids,created_by_account_id) VALUES ('invite-with-groups','invite',now()+interval '1 hour','user','alice',ARRAY[$1,$2,999999],$3)", manualID, ruleID, creatorID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO enrollment (token,intent,expires_at) VALUES ('bootstrap-empty-groups','bootstrap',now()+interval '1 hour')"); err != nil {
		t.Fatalf("ordinary enrollment rejected after migration: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO enrollment (token,intent,expires_at,group_ids) VALUES ('bad-bootstrap','bootstrap',now()+interval '1 hour',ARRAY[$1])", manualID); err == nil {
		t.Fatal("non-invite enrollment accepted invitation groups")
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
	groups, err := queries.ListInvitationGroups(ctx, []int32{manualID, ruleID})
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].ID != manualID {
		t.Fatalf("group summaries = %+v, want only manual group %d", groups, manualID)
	}

	tx, err := queryConn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	txQueries := dbgen.New(tx)
	enrollmentRow, err := txQueries.ConsumeInviteEnrollment(ctx, "invite-with-groups")
	if err != nil {
		t.Fatal(err)
	}
	account, err := txQueries.InsertAccount(ctx, dbgen.InsertAccountParams{
		Username: "alice", DisplayName: "Alice", WebauthnUserHandle: []byte{4, 5, 6}, Role: "user", Attributes: []byte("{}"),
	})
	if err != nil {
		t.Fatal(err)
	}
	applied, err := txQueries.ApplyInvitationGroups(ctx, dbgen.ApplyInvitationGroupsParams{
		GroupIds: enrollmentRow.GroupIds, AccountID: account.ID, CreatedBy: enrollmentRow.CreatedByAccountID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || applied[0] != manualID {
		t.Fatalf("applied groups = %v, want only manual group %d", applied, manualID)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var consumed bool
	if err := conn.QueryRowContext(ctx, "SELECT consumed_at IS NOT NULL FROM enrollment WHERE token='invite-with-groups'").Scan(&consumed); err != nil || consumed {
		t.Fatalf("invitation consumed after rollback = %t (err=%v)", consumed, err)
	}
	var accountCount int
	if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM account WHERE username='alice'").Scan(&accountCount); err != nil || accountCount != 0 {
		t.Fatalf("accounts after rollback = %d (err=%v)", accountCount, err)
	}
}
