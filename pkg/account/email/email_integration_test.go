package email

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

// withTestDB runs fn inside one rolled-back transaction, mirroring the
// server integration tests' fixture.
func withTestDB(t *testing.T, fn func(ctx context.Context, tx pgx.Tx, q *db.Queries)) {
	t.Helper()
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
	fn(ctx, tx, db.New(tx))
}

func insertTestAccount(ctx context.Context, tx pgx.Tx, t *testing.T, label string) int32 {
	t.Helper()
	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	var id int32
	if err := tx.QueryRow(ctx, `
		INSERT INTO account (username, display_name, webauthn_user_handle)
		VALUES ($1, $1, $2) RETURNING id`,
		"eml_"+hex.EncodeToString(nonce[:])+"_"+label, append(nonce[:], label...)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestValidateAddress(t *testing.T) {
	ok := func(in, want string) {
		t.Helper()
		got, err := ValidateAddress(in)
		if err != nil || got != want {
			t.Errorf("ValidateAddress(%q) = %q, %v; want %q, nil", in, got, err, want)
		}
	}
	bad := func(in string) {
		t.Helper()
		if _, err := ValidateAddress(in); err == nil {
			t.Errorf("ValidateAddress(%q) accepted", in)
		}
	}
	ok(" alice@example.com ", "alice@example.com")
	ok("a.l@Example.COM", "a.l@example.com") // IDNA case mapping to ASCII lower
	ok("用户@例子.测试", "用户@xn--fsqu00a.xn--0zwm56d")
	bad("")
	bad("no at sign")
	bad("Display <alice@example.com>")
	bad("alice@example.com extra")
	bad(strings.Repeat("a", 250) + "@example.com")
}

func TestAddCapDuplicateAndPrimaryRules(t *testing.T) {
	withTestDB(t, func(ctx context.Context, tx pgx.Tx, q *db.Queries) {
		acct := insertTestAccount(ctx, tx, t, "cap")
		other := insertTestAccount(ctx, tx, t, "other")

		// Cap: MaxPerAccount addresses, the next Add fails.
		for i := range MaxPerAccount {
			if _, err := Add(ctx, q, acct, string(rune('a'+i))+"@cap.example"); err != nil {
				t.Fatalf("Add %d: %v", i, err)
			}
		}
		if _, err := Add(ctx, q, acct, "overflow@cap.example"); !errors.Is(err, ErrCapReached) {
			t.Errorf("Add over cap = %v, want ErrCapReached", err)
		}

		// In-account duplicate is case-insensitive (on a non-full account;
		// the cap check runs first by design, and acct is at the cap).
		dupAccount := insertTestAccount(ctx, tx, t, "dup")
		if _, err := Add(ctx, q, dupAccount, "dup@example.test"); err != nil {
			t.Fatal(err)
		}
		if _, err := Add(ctx, q, dupAccount, "DUP@Example.TEST"); !errors.Is(err, ErrDuplicate) {
			t.Errorf("duplicate Add = %v, want ErrDuplicate", err)
		}

		// Primary requires verified.
		row, err := Add(ctx, q, other, "primary@unverified.test")
		if err != nil {
			t.Fatal(err)
		}
		if err := SetPrimary(ctx, q, other, row.ID); !errors.Is(err, ErrNotVerifiedForPrimary) {
			t.Errorf("SetPrimary unverified = %v, want ErrNotVerifiedForPrimary", err)
		}

		// Self-service delete of the primary is refused.
		verified, err := AssertVerified(ctx, q, other, row.ID, "code", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = verified
		if _, err := Remove(ctx, q, other, row.ID, false); !errors.Is(err, ErrPrimaryUndeletable) {
			t.Errorf("Remove primary (self) = %v, want ErrPrimaryUndeletable", err)
		}

		// Admin delete of the primary promotes the earliest-verified other.
		second, err := Add(ctx, q, other, "second@unverified.test")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := AssertVerified(ctx, q, other, second.ID, "code", nil, nil); err != nil {
			t.Fatal(err)
		}
		fallback, err := Remove(ctx, q, other, row.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		if fallback == nil || *fallback != "second@unverified.test" {
			t.Errorf("fallback primary = %v, want second@unverified.test", fallback)
		}
		primary, err := q.GetAccountPrimaryEmail(ctx, other)
		if err != nil || primary != "second@unverified.test" {
			t.Errorf("primary after admin delete = %q, %v", primary, err)
		}

		// Admin delete with no candidate clears the pointer.
		fallback, err = Remove(ctx, q, other, second.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		if fallback != nil {
			t.Errorf("fallback with no candidate = %v, want nil", fallback)
		}
		if _, err := q.GetAccountPrimaryEmail(ctx, other); !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("primary after last delete: err = %v, want ErrNoRows", err)
		}
	})
}

func TestCrossAccountVerifiedConflict(t *testing.T) {
	withTestDB(t, func(ctx context.Context, tx pgx.Tx, q *db.Queries) {
		acct := insertTestAccount(ctx, tx, t, "winner")
		rival := insertTestAccount(ctx, tx, t, "rival")

		row, err := Add(ctx, q, acct, "shared@conflict.test")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := AssertVerified(ctx, q, acct, row.ID, "code", nil, nil); err != nil {
			t.Fatal(err)
		}

		// The rival holds an unverified copy…
		copy, err := Add(ctx, q, rival, "shared@conflict.test")
		if err != nil {
			t.Fatal(err)
		}
		// …which can no longer verify.
		if _, err := AssertVerified(ctx, q, rival, copy.ID, "code", nil, nil); !errors.Is(err, ErrTaken) {
			t.Errorf("rival verify = %v, want ErrTaken", err)
		}
		// Admin assertion on the rival also reports the conflict.
		if _, err := AdminAdd(ctx, q, rival, "shared@conflict.test", true, false, rival); !errors.Is(err, ErrTaken) {
			t.Errorf("rival admin assert = %v, want ErrTaken", err)
		}
		// primary without verified is rejected.
		if _, err := AdminAdd(ctx, q, rival, "x@conflict.test", false, true, rival); !errors.Is(err, ErrNotVerifiedForPrimary) {
			t.Errorf("primary without verified = %v, want ErrNotVerifiedForPrimary", err)
		}
	})
}

func TestAbsorbUpstreamBranches(t *testing.T) {
	withTestDB(t, func(ctx context.Context, tx pgx.Tx, q *db.Queries) {
		acct := insertTestAccount(ctx, tx, t, "absorb")
		rival := insertTestAccount(ctx, tx, t, "absorbrival")

		var idpID int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO upstream_idp (slug, display_name, protocol, mode, provider_config, secret_status, secret_enc, secret_nonce, key_version)
			VALUES ('absorb-idp', 'Absorb', 'oidc', 'auto_provision', '{}'::jsonb, 'unconfigured', NULL, NULL, NULL)
			RETURNING id`).Scan(&idpID); err != nil {
			t.Fatal(err)
		}

		// added + becomes primary when none exists
		outcome, _, err := AbsorbUpstream(ctx, q, acct, "absorbed@upstream.test", idpID)
		if err != nil || outcome != AbsorbAdded {
			t.Fatalf("absorb new = %v, %v; want added", outcome, err)
		}
		if p, err := q.GetAccountPrimaryEmail(ctx, acct); err != nil || p != "absorbed@upstream.test" {
			t.Errorf("primary after absorb = %q, %v", p, err)
		}

		// existing verified: no change
		outcome, _, err = AbsorbUpstream(ctx, q, acct, "absorbed@upstream.test", idpID)
		if err != nil || outcome != AbsorbExisting {
			t.Errorf("re-absorb = %v, %v; want existing", outcome, err)
		}

		// unverified local copy gets bumped to upstream-verified
		local, err := Add(ctx, q, acct, "local@upstream.test")
		if err != nil {
			t.Fatal(err)
		}
		_ = local
		outcome, _, err = AbsorbUpstream(ctx, q, acct, "local@upstream.test", idpID)
		if err != nil || outcome != AbsorbVerified {
			t.Errorf("absorb over unverified = %v, %v; want verified", outcome, err)
		}

		// taken: rival has it verified → skip, sign-in unaffected
		row, err := Add(ctx, q, rival, "taken@upstream.test")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := AssertVerified(ctx, q, rival, row.ID, "code", nil, nil); err != nil {
			t.Fatal(err)
		}
		outcome, _, err = AbsorbUpstream(ctx, q, acct, "taken@upstream.test", idpID)
		if err != nil || outcome != AbsorbSkipTaken {
			t.Errorf("absorb taken = %v, %v; want skip_taken", outcome, err)
		}

		// full: fill to the cap, absorb skips
		filler := insertTestAccount(ctx, tx, t, "full")
		for i := range MaxPerAccount {
			if _, err := Add(ctx, q, filler, string(rune('a'+i))+"@full.test"); err != nil {
				t.Fatal(err)
			}
		}
		outcome, _, err = AbsorbUpstream(ctx, q, filler, "onemore@full.test", idpID)
		if err != nil || outcome != AbsorbSkipFull {
			t.Errorf("absorb at cap = %v, %v; want skip_full", outcome, err)
		}
	})
}

func TestCodes(t *testing.T) {
	store := kv.NewMemoryStore()
	ctx := context.Background()

	code, err := Issue(ctx, store, 7, 42, "x@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 6 {
		t.Fatalf("code %q is not 6 digits", code)
	}

	// correct
	res, _, err := Verify(ctx, store, 7, 42, code)
	if err != nil || res != VerifyOK {
		t.Fatalf("verify correct = %v, %v", res, err)
	}
	// consumed: second submit fails
	res, _, _ = Verify(ctx, store, 7, 42, code)
	if res != VerifyExpired {
		t.Errorf("replay = %v, want expired", res)
	}

	// wrong 5 times kills the code
	code, _ = Issue(ctx, store, 8, 42, "y@example.com")
	for i := range 4 {
		res, remaining, _ := Verify(ctx, store, 8, 42, "000000")
		if res != VerifyInvalid || remaining != 4-i {
			t.Errorf("wrong %d = %v (%d left)", i, res, remaining)
		}
	}
	res, _, _ = Verify(ctx, store, 8, 42, "000000")
	if res != VerifyExpired {
		t.Errorf("sixth wrong = %v, want expired", res)
	}
	// even the right code no longer works
	res, _, _ = Verify(ctx, store, 8, 42, code)
	if res != VerifyExpired {
		t.Errorf("correct after exhaustion = %v, want expired", res)
	}

	// resend invalidates the old code
	old, _ := Issue(ctx, store, 9, 42, "z@example.com")
	newCode, _ := Issue(ctx, store, 9, 42, "z@example.com")
	if old == newCode {
		t.Fatal("resend produced the same code; test can't distinguish invalidation")
	}
	if res, _, _ := Verify(ctx, store, 9, 42, old); res == VerifyOK {
		t.Errorf("old code after resend still verifies")
	}
	if res, _, _ := Verify(ctx, store, 9, 42, newCode); res != VerifyOK {
		t.Errorf("new code after resend = %v, want ok", res)
	}

	// account binding: another account's code fails
	acode, _ := Issue(ctx, store, 10, 43, "w@example.com")
	if res, _, _ := Verify(ctx, store, 10, 44, acode); res != VerifyExpired {
		t.Errorf("cross-account verify = %v, want expired", res)
	}
}

func TestSendLimits(t *testing.T) {
	store := kv.NewMemoryStore()
	limits, err := NewSendLimits(store)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// cooldown: second send within 60s refused, reset time positive
	if _, ok := limits.AllowCodeSend(ctx, 1, 1, "a@lim.test"); !ok {
		t.Fatal("first send refused")
	}
	wait, ok := limits.AllowCodeSend(ctx, 1, 1, "a@lim.test")
	if ok || wait <= 0 || wait > time.Minute {
		t.Errorf("cooldown send = ok:%v wait:%v", ok, wait)
	}
	if d := limits.CooldownRemaining(ctx, 1); d <= 0 || d > time.Minute {
		t.Errorf("CooldownRemaining = %v", d)
	}

	// account hourly: 10 per hour
	acct := int32(41)
	for i := range 10 {
		if _, ok := limits.AllowCodeSend(ctx, acct, int64(100+i), fmt.Sprintf("acct%d@lim.test", i)); !ok {
			t.Fatalf("account send %d refused", i)
		}
	}
	if _, ok := limits.AllowCodeSend(ctx, acct, 200, "another@lim.test"); ok {
		t.Error("11th account send allowed")
	}

	// address hourly across accounts: 5 per lower(address)
	addr := "cross@lim.test"
	for i := range 5 {
		if _, ok := limits.AllowCodeSend(ctx, int32(300+i), int64(300+i), addr); !ok {
			t.Fatalf("address send %d refused", i)
		}
	}
	if _, ok := limits.AllowCodeSend(ctx, 999, 999, addr); ok {
		t.Error("6th cross-account address send allowed")
	}

	// test mail: 5 per minute per admin
	for i := range 5 {
		if _, ok := limits.AllowTestMail(ctx, 55); !ok {
			t.Fatalf("test mail %d refused", i)
		}
	}
	if _, ok := limits.AllowTestMail(ctx, 55); ok {
		t.Error("6th test mail allowed")
	}
}
