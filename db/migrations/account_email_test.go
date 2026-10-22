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
)

// TestAccountEmailMigrationPostgres exercises 050_account_email: the backfill
// rules (verified → primary, unverified → no primary, duplicate verified
// addresses demote all but the smallest account id, blank emails dropped),
// Down restoring account.email from the primary, and the schema-level
// guarantees the write layer relies on (verified-address uniqueness across
// accounts, per-account address uniqueness, composite-FK primary ownership,
// cascade on account delete).
func TestAccountEmailMigrationPostgres(t *testing.T) {
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
	// 032 needs pg_trgm, which lives in public (mirrors the 049 test's
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
	// Up through 050 (colour schemes): the legacy email columns still exist
	// there, so the fixture rows are seeded between 050 and 051. The goose
	// dance stays two-phase for the pg_trgm 032 dependency.
	if err := goose.UpTo(conn, ".", 50); err != nil {
		t.Fatal(err)
	}

	insertAccount := func(username, email string, verified bool) int32 {
		t.Helper()
		var id int32
		if err := conn.QueryRowContext(ctx, `
			INSERT INTO account (username, display_name, webauthn_user_handle, email, email_verified)
			VALUES ($1, $1, decode($2, 'hex'), $3, $4)
			RETURNING id`, username, hex.EncodeToString([]byte(username)), email, verified).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	verifiedOnly := insertAccount("email-verified-only", "verified@example.test", true)
	unverifiedOnly := insertAccount("email-unverified-only", "unverified@example.test", false)
	// Duplicate verified address with different case: the smaller id wins.
	dupWinner := insertAccount("email-dup-winner", "Shared@Example.test", true)
	dupLoser := insertAccount("email-dup-loser", "shared@example.test", true)
	if dupWinner > dupLoser {
		dupWinner, dupLoser = dupLoser, dupWinner
	}
	blankEmail := insertAccount("email-blank", "   ", true)
	noEmail := insertAccount("email-none", "", false)

	if err := goose.UpTo(conn, ".", 51); err != nil {
		t.Fatal(err)
	}

	readEmail := func(accountID int32) (id int64, address string, verified bool, via any, primary bool) {
		t.Helper()
		var verifiedAt any
		var primaryFlag sql.NullBool
		if err := conn.QueryRowContext(ctx, `
			SELECT ae.id, ae.address, ae.verified_at, ae.verified_via, (a.primary_email_id = ae.id)
			FROM account a
			JOIN account_email ae ON ae.account_id = a.id
			WHERE a.id = $1`, accountID).Scan(&id, &address, &verifiedAt, &via, &primaryFlag); err != nil {
			t.Fatal(err)
		}
		verified = verifiedAt != nil
		primary = primaryFlag.Valid && primaryFlag.Bool
		return
	}

	if _, address, verified, via, primary := readEmail(verifiedOnly); !verified || via != "upstream" || !primary || address != "verified@example.test" {
		t.Errorf("verified-only account: address=%q verified=%v via=%v primary=%v, want verified upstream primary", address, verified, via, primary)
	}
	if _, _, verified, _, primary := readEmail(unverifiedOnly); verified || primary {
		t.Errorf("unverified-only account: verified=%v primary=%v, want unverified and not primary", verified, primary)
	}
	if _, _, verified, _, primary := readEmail(dupWinner); !verified || !primary {
		t.Errorf("duplicate winner: verified=%v primary=%v, want both", verified, primary)
	}
	if _, _, verified, _, primary := readEmail(dupLoser); verified || primary {
		t.Errorf("duplicate loser: verified=%v primary=%v, want demoted unverified and not primary", verified, primary)
	}
	var count int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM account_email WHERE account_id = $1`, blankEmail).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("blank email migrated into %d rows, want 0", count)
	}
	for _, accountID := range []int32{noEmail} {
		if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM account_email WHERE account_id = $1`, accountID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("account %d with no email got %d rows, want 0", accountID, count)
		}
	}

	// account.email/email_verified are gone.
	for _, column := range []string{"email", "email_verified"} {
		var exists bool
		if err := conn.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM information_schema.columns
				WHERE table_schema = $1 AND table_name = 'account' AND column_name = $2)`,
			schema, column).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Errorf("account.%s still exists after 050 Up", column)
		}
	}

	assertRejected := func(name string, statement string, args ...any) {
		t.Helper()
		_, err := conn.ExecContext(ctx, statement, args...)
		if err == nil {
			t.Errorf("%s was accepted, want rejected", name)
		}
	}

	// Verified-address uniqueness across accounts: a second account cannot
	// verify the same lower(address); unverified duplicates coexist.
	assertRejected("second account verifying same address", `
		INSERT INTO account_email (account_id, address, verified_at, verified_via)
		VALUES ($1, 'VERIFIED@example.test', now(), 'admin')`, unverifiedOnly)
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO account_email (account_id, address)
		VALUES ($1, 'verified@example.test')`, unverifiedOnly); err != nil {
		t.Errorf("unverified duplicate across accounts was rejected: %v", err)
	}
	// Per-account uniqueness on lower(address).
	assertRejected("same address twice on one account", `
		INSERT INTO account_email (account_id, address)
		VALUES ($1, 'shared@example.test')`, dupLoser)
	// verified_at set without verified_via (and vice versa) is rejected.
	assertRejected("verified_at without verified_via", `
		INSERT INTO account_email (account_id, address, verified_at)
		VALUES ($1, 'half-verified@example.test', now())`, unverifiedOnly)

	// The composite FK refuses another account's address as primary.
	var otherRow int64
	if err := conn.QueryRowContext(ctx, `SELECT id FROM account_email WHERE account_id = $1`, verifiedOnly).Scan(&otherRow); err != nil {
		t.Fatal(err)
	}
	assertRejected("primary pointing at another account's address", `
		UPDATE account SET primary_email_id = $2 WHERE id = $1`, unverifiedOnly, otherRow)

	// Deleting an account cascades to its addresses (the deferred FK between
	// account and its primary does not block the delete).
	if _, err := conn.ExecContext(ctx, `DELETE FROM account WHERE id = $1`, dupLoser); err != nil {
		t.Errorf("deleting an account with addresses failed: %v", err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM account_email WHERE account_id = $1`, dupLoser).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("addresses survived account delete: %d rows", count)
	}

	if err := goose.DownTo(conn, ".", 50); err != nil {
		t.Fatal(err)
	}
	var version int64
	if err := conn.QueryRowContext(ctx, `SELECT version_id FROM goose_db_version WHERE is_applied ORDER BY id DESC LIMIT 1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 50 {
		t.Fatalf("migration version after down = %d, want 50", version)
	}

	readLegacy := func(accountID int32, wantEmail string, wantVerified bool) {
		t.Helper()
		var email sql.NullString
		var verified bool
		if err := conn.QueryRowContext(ctx, `SELECT email, email_verified FROM account WHERE id = $1`, accountID).Scan(&email, &verified); err != nil {
			t.Fatal(err)
		}
		if wantEmail == "" {
			if email.Valid || verified {
				t.Errorf("account %d after down: email=%v verified=%v, want empty", accountID, email.String, verified)
			}
			return
		}
		if !email.Valid || email.String != wantEmail || !verified {
			t.Errorf("account %d after down: email=%v verified=%v, want %q/true", accountID, email.String, verified, wantEmail)
		}
	}
	readLegacy(verifiedOnly, "verified@example.test", true)
	readLegacy(unverifiedOnly, "", false)
	readLegacy(dupWinner, "Shared@Example.test", true)

	var tableGone bool
	if err := conn.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.tables
			WHERE table_schema = $1 AND table_name = 'account_email')`, schema).Scan(&tableGone); err != nil {
		t.Fatal(err)
	}
	if tableGone {
		t.Error("account_email still exists after down")
	}

}
