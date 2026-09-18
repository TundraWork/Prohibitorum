package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"

	"prohibitorum/pkg/db"
)

func appPolicyLockSuffix(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(raw)
}

func TestAppPolicyReplacementLocksApplicationPostgres(t *testing.T) {
	pool := enrollmentLockTestPool(t)
	ctx := context.Background()
	suffix := appPolicyLockSuffix(t)

	t.Run("OIDC", func(t *testing.T) {
		clientID := "policy-lock-" + suffix
		if _, err := pool.Exec(ctx, `
			INSERT INTO oidc_client (client_id, display_name, redirect_uris)
			VALUES ($1, 'Policy lock', ARRAY['https://example.test/callback'])`, clientID); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM oidc_client WHERE client_id = $1`, clientID)
		})

		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx) //nolint:errcheck
		if _, err := db.New(tx).GetOIDCClientAnyForUpdate(ctx, clientID); err != nil {
			t.Fatal(err)
		}

		updater, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer updater.Release()
		updated := make(chan error, 1)
		go func() {
			_, err := updater.Exec(ctx, `UPDATE oidc_client SET display_name = 'Updated' WHERE client_id = $1`, clientID)
			updated <- err
		}()
		waitForPostgresBlock(t, pool, updater.Conn().PgConn().PID())
		select {
		case err := <-updated:
			t.Fatalf("OIDC update crossed application lock: %v", err)
		default:
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-updated; err != nil {
			t.Fatal(err)
		}
	})

	t.Run("SAML", func(t *testing.T) {
		var spID int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO saml_sp (entity_id, display_name)
			VALUES ($1, 'Policy lock') RETURNING id`, "https://example.test/policy-lock/"+suffix).Scan(&spID); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM saml_sp WHERE id = $1`, spID) })

		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx) //nolint:errcheck
		if _, err := db.New(tx).GetSAMLSPByIDForUpdate(ctx, spID); err != nil {
			t.Fatal(err)
		}

		updater, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer updater.Release()
		updated := make(chan error, 1)
		go func() {
			_, err := updater.Exec(ctx, `UPDATE saml_sp SET display_name = 'Updated' WHERE id = $1`, spID)
			updated <- err
		}()
		waitForPostgresBlock(t, pool, updater.Conn().PgConn().PID())
		select {
		case err := <-updated:
			t.Fatalf("SAML update crossed application lock: %v", err)
		default:
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-updated; err != nil {
			t.Fatal(err)
		}
	})
}
