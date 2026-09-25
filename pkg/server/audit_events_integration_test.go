package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"prohibitorum/pkg/db"
)

// TestListCredentialEventsAccountUsernamePostgres checks that the audit list
// query carries the account's username through its LEFT JOIN: present for a
// live account, absent for a system event and for an account deleted since
// (ON DELETE SET NULL clears the reference), while the factor and since
// filters still select the same rows.
func TestListCredentialEventsAccountUsernamePostgres(t *testing.T) {
	databaseURL := os.Getenv("PROHIBITORUM_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PROHIBITORUM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	queries := db.New(tx)

	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	prefix := "audit_" + hex.EncodeToString(nonce[:])
	factor := prefix + "_factor"

	accountIDs := make(map[string]int32)
	for index, name := range []string{"alice", "bob"} {
		var id int32
		err := tx.QueryRow(ctx, `
			INSERT INTO account (username, display_name, webauthn_user_handle, email)
			VALUES ($1, $2, $3, $4)
			RETURNING id`, prefix+"_"+name, name, append(nonce[:], byte(index)), prefix+"_"+name+"@audit.test").Scan(&id)
		if err != nil {
			t.Fatalf("insert account %s: %v", name, err)
		}
		accountIDs[name] = id
	}

	insertEvent := func(accountID *int32, factor string, at time.Time) int64 {
		t.Helper()
		var id int64
		err := tx.QueryRow(ctx, `
			INSERT INTO credential_event (account_id, factor, event, at)
			VALUES ($1, $2, 'use', $3)
			RETURNING id`, accountID, factor, at).Scan(&id)
		if err != nil {
			t.Fatalf("insert event: %v", err)
		}
		return id
	}
	alice, bob := accountIDs["alice"], accountIDs["bob"]
	insertEvent(&alice, factor, time.Date(2099, 6, 1, 0, 0, 0, 0, time.UTC)) // before since
	insertEvent(&alice, prefix+"_other", time.Date(2099, 7, 4, 0, 0, 0, 0, time.UTC))
	aliceEvent := insertEvent(&alice, factor, time.Date(2099, 7, 3, 0, 0, 0, 0, time.UTC))
	systemEvent := insertEvent(nil, factor, time.Date(2099, 7, 2, 0, 0, 0, 0, time.UTC))
	bobEvent := insertEvent(&bob, factor, time.Date(2099, 7, 1, 0, 0, 0, 0, time.UTC))

	if _, err := tx.Exec(ctx, `DELETE FROM account WHERE id = $1`, bob); err != nil {
		t.Fatalf("delete bob: %v", err)
	}

	rows, err := queries.ListCredentialEvents(ctx, db.ListCredentialEventsParams{
		Factor: pgtype.Text{String: factor, Valid: true},
		Since:  pgtype.Timestamptz{Time: time.Date(2099, 6, 15, 0, 0, 0, 0, time.UTC), Valid: true},
		Lim:    10,
	})
	if err != nil {
		t.Fatalf("ListCredentialEvents: %v", err)
	}

	want := []struct {
		id       int64
		username string
	}{
		{bobEvent, ""},
		{systemEvent, ""},
		{aliceEvent, prefix + "_alice"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %d, want %d: %+v", len(rows), len(want), rows)
	}
	for i, w := range want {
		view := auditEventView(rows[i])
		if view.ID != w.id {
			t.Errorf("row %d: id = %d, want %d", i, view.ID, w.id)
		}
		if view.AccountUsername != w.username {
			t.Errorf("row %d: accountUsername = %q, want %q", i, view.AccountUsername, w.username)
		}
		if w.username == "" && view.AccountID != nil {
			t.Errorf("row %d: accountId = %d, want none", i, *view.AccountID)
		}
		if view.Factor != factor {
			t.Errorf("row %d: factor = %q, want %q", i, view.Factor, factor)
		}
	}
	if view := auditEventView(rows[2]); view.AccountID == nil || *view.AccountID != alice {
		t.Errorf("alice's event: accountId = %v, want %d", view.AccountID, alice)
	}
}
