package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"prohibitorum/pkg/db"
)

// TestListConfirmedIdentitiesForProvidersPostgres pins the rows behind the
// identities claim: confirmed links only, at enabled providers in the list,
// oldest link first.
func TestListConfirmedIdentitiesForProvidersPostgres(t *testing.T) {
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
	prefix := "idscope-" + hex.EncodeToString(nonce[:])

	var accountID int32
	if err := tx.QueryRow(ctx, `
		INSERT INTO account (username, display_name, webauthn_user_handle)
		VALUES ($1, 'Identity scope', $2) RETURNING id`, prefix, nonce[:]).Scan(&accountID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	providerIDs := map[string]int64{}
	for _, provider := range []struct {
		slug     string
		disabled bool
	}{{prefix + "-a", false}, {prefix + "-b", false}, {prefix + "-off", true}} {
		var id int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO upstream_idp (
				slug, display_name, protocol, mode, provider_config, secret_status,
				secret_enc, secret_nonce, key_version, disabled
			)
			VALUES ($1, $1, 'steam', 'auto_provision', '{}'::jsonb, 'unconfigured', NULL, NULL, NULL, $2)
			RETURNING id`, provider.slug, provider.disabled).Scan(&id); err != nil {
			t.Fatalf("insert provider %s: %v", provider.slug, err)
		}
		providerIDs[provider.slug] = id
	}
	link := func(slug, sub string, confirmed bool, linkedAt time.Time) {
		t.Helper()
		if _, err := tx.Exec(ctx, `
			INSERT INTO account_identity (account_id, upstream_idp_id, upstream_iss, upstream_sub, upstream_data, confirmed_at, linked_at)
			VALUES ($1, $2, 'https://'||$3||'.example', $4, '{"personaName":"x"}'::jsonb, CASE WHEN $5 THEN now() END, $6)`,
			accountID, providerIDs[slug], slug, sub, confirmed, linkedAt); err != nil {
			t.Fatalf("insert identity %s: %v", sub, err)
		}
	}
	day := func(d int) time.Time { return time.Date(2099, 6, d, 0, 0, 0, 0, time.UTC) }
	link(prefix+"-a", "a-newer", true, day(2))
	link(prefix+"-a", "a-older", true, day(1))
	link(prefix+"-a", "a-unconfirmed", false, day(3))
	link(prefix+"-b", "b-not-requested", true, day(1))
	link(prefix+"-off", "off-disabled", true, day(1))

	rows, err := queries.ListConfirmedIdentitiesForProviders(ctx, db.ListConfirmedIdentitiesForProvidersParams{
		AccountID: accountID, Slugs: []string{prefix + "-a", prefix + "-off", prefix + "-missing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var subs []string
	for _, row := range rows {
		if row.IdpSlug != prefix+"-a" || string(row.UpstreamData) == "" {
			t.Fatalf("unexpected row %+v", row)
		}
		subs = append(subs, row.UpstreamSub)
	}
	if want := []string{"a-older", "a-newer"}; !slices.Equal(subs, want) {
		t.Fatalf("subs = %v, want %v", subs, want)
	}
}
