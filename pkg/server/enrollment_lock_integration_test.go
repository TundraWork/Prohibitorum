package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"prohibitorum/pkg/db"
)

func waitForPostgresBlock(t *testing.T, pool *pgxpool.Pool, pid uint32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blockers int
		if err := pool.QueryRow(ctx, `SELECT cardinality(pg_blocking_pids($1))`, pid).Scan(&blockers); err != nil {
			t.Fatalf("query blocking pids: %v", err)
		}
		if blockers > 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("concurrent update did not block on enrollment row lock")
		case <-ticker.C:
		}
	}
}

func enrollmentLockTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("PROHIBITORUM_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PROHIBITORUM_TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func enrollmentLockNonce(t *testing.T) ([]byte, string) {
	t.Helper()
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return raw, hex.EncodeToString(raw)
}

func TestEnrollmentProviderGateLockBlocksConcurrentDisablePostgres(t *testing.T) {
	pool := enrollmentLockTestPool(t)
	ctx := context.Background()
	raw, suffix := enrollmentLockNonce(t)
	slug := "enrollment-lock-" + suffix
	var providerID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO upstream_idp (
			slug, display_name, protocol, mode, provider_config, secret_status,
			secret_enc, secret_nonce, key_version, disabled
		)
		VALUES ($1, 'VRChat', 'vrchat', 'link_only', '{}'::jsonb, 'valid', $2, $2, 1, false)
		RETURNING id`, slug, raw).Scan(&providerID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM upstream_idp WHERE id = $1`, providerID) })

	gateTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer gateTx.Rollback(ctx) //nolint:errcheck
	provider, err := db.New(gateTx).GetUpstreamIDPByIDForUpdate(ctx, providerID)
	if err != nil || provider.Disabled {
		t.Fatalf("locked provider load = disabled %v, err %v", provider.Disabled, err)
	}

	updater, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer updater.Release()
	updated := make(chan error, 1)
	go func() {
		_, err := db.New(updater).SetUpstreamIDPDisabled(ctx, db.SetUpstreamIDPDisabledParams{Slug: slug, Disabled: true})
		updated <- err
	}()
	waitForPostgresBlock(t, pool, updater.Conn().PgConn().PID())
	select {
	case err := <-updated:
		t.Fatalf("provider disable crossed locked transaction before commit: %v", err)
	default:
	}
	if err := gateTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-updated; err != nil {
		t.Fatal(err)
	}
}

func TestEnrollmentRecoveryAccountLockBlocksConcurrentDisablePostgres(t *testing.T) {
	pool := enrollmentLockTestPool(t)
	ctx := context.Background()
	raw, suffix := enrollmentLockNonce(t)
	var accountID int32
	if err := pool.QueryRow(ctx, `
		INSERT INTO account (username, display_name, webauthn_user_handle)
		VALUES ($1, 'Enrollment lock account', $2)
		RETURNING id`, "enrollment_lock_"+suffix, raw).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM account WHERE id = $1`, accountID) })

	recoveryTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer recoveryTx.Rollback(ctx) //nolint:errcheck
	account, err := db.New(recoveryTx).GetAccountByIDForUpdate(ctx, accountID)
	if err != nil || account.Disabled {
		t.Fatalf("locked account load = disabled %v, err %v", account.Disabled, err)
	}

	updater, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer updater.Release()
	updated := make(chan error, 1)
	go func() {
		_, err := db.New(updater).SetAccountDisabled(ctx, db.SetAccountDisabledParams{ID: accountID, Disabled: true})
		updated <- err
	}()
	waitForPostgresBlock(t, pool, updater.Conn().PgConn().PID())
	select {
	case err := <-updated:
		t.Fatalf("account disable crossed locked transaction before commit: %v", err)
	default:
	}
	if err := recoveryTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-updated; err != nil {
		t.Fatal(err)
	}
}
