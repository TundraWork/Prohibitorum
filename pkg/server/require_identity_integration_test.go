package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"prohibitorum/pkg/db"
)

// TestHasConfirmedIdentityForProvidersPostgres pins what a require-identity
// request counts as satisfied: a confirmed link of this account at an enabled
// provider in the list.
func TestHasConfirmedIdentityForProvidersPostgres(t *testing.T) {
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
	prefix := "reqid-" + hex.EncodeToString(nonce[:])
	account := func(suffix string, b byte) int32 {
		t.Helper()
		var id int32
		if err := tx.QueryRow(ctx, `
			INSERT INTO account (username, display_name, webauthn_user_handle)
			VALUES ($1, 'Require identity', $2) RETURNING id`, prefix+suffix, append(append([]byte{}, nonce[:]...), b)).Scan(&id); err != nil {
			t.Fatalf("insert account: %v", err)
		}
		return id
	}
	alice, bob, carol := account("-alice", 1), account("-bob", 2), account("-carol", 3)
	provider := func(slug string, disabled bool) int64 {
		t.Helper()
		var id int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO upstream_idp (
				slug, display_name, protocol, mode, provider_config, secret_status,
				secret_enc, secret_nonce, key_version, disabled
			)
			VALUES ($1, $1, 'steam', 'link_only', '{}'::jsonb, 'unconfigured', NULL, NULL, NULL, $2)
			RETURNING id`, slug, disabled).Scan(&id); err != nil {
			t.Fatalf("insert provider %s: %v", slug, err)
		}
		return id
	}
	enabled, other, off := provider(prefix+"-on", false), provider(prefix+"-other", false), provider(prefix+"-off", true)
	link := func(account int32, idp int64, sub string, confirmed bool) {
		t.Helper()
		if _, err := tx.Exec(ctx, `
			INSERT INTO account_identity (account_id, upstream_idp_id, upstream_iss, upstream_sub, confirmed_at)
			VALUES ($1, $2, 'https://steam.example', $3, CASE WHEN $4 THEN now() END)`, account, idp, sub, confirmed); err != nil {
			t.Fatalf("insert identity %s: %v", sub, err)
		}
	}
	link(alice, enabled, prefix+"-alice", true)
	link(bob, enabled, prefix+"-bob", false)
	link(bob, off, prefix+"-bob-off", true)
	link(carol, other, prefix+"-carol", true)

	for _, tc := range []struct {
		name    string
		account int32
		slugs   []string
		want    bool
	}{
		{"confirmed link at a listed provider", alice, []string{prefix + "-other", prefix + "-on"}, true},
		{"provider not listed", alice, []string{prefix + "-other"}, false},
		{"unconfirmed link", bob, []string{prefix + "-on"}, false},
		{"disabled provider", bob, []string{prefix + "-off"}, false},
		{"another account's link does not count", carol, []string{prefix + "-on"}, false},
		{"own link at the other provider", carol, []string{prefix + "-on", prefix + "-other"}, true},
	} {
		got, err := queries.HasConfirmedIdentityForProviders(ctx, db.HasConfirmedIdentityForProvidersParams{AccountID: tc.account, Slugs: tc.slugs})
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
