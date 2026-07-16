package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"prohibitorum/pkg/db"
)

func TestListAccountsIdentityFiltersPostgres(t *testing.T) {
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
	prefix := "filter_" + hex.EncodeToString(nonce[:])

	accountIDs := make(map[string]int32)
	createdAt := map[string]time.Time{
		"alice": time.Date(2099, 7, 4, 0, 0, 0, 0, time.UTC),
		"bob":   time.Date(2099, 7, 3, 0, 0, 0, 0, time.UTC),
		"carol": time.Date(2099, 7, 2, 0, 0, 0, 0, time.UTC),
		"dave":  time.Date(2099, 7, 1, 0, 0, 0, 0, time.UTC),
	}
	for index, name := range []string{"alice", "bob", "carol", "dave"} {
		username := prefix + "_" + name
		displayName := name
		if name == "alice" {
			displayName = prefix + " LocalOnly Alice"
		}
		var id int32
		err := tx.QueryRow(ctx, `
			INSERT INTO account (username, display_name, webauthn_user_handle, email, created_at)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id`, username, displayName, append(nonce[:], byte(index)), prefix+"@page.test", createdAt[name]).Scan(&id)
		if err != nil {
			t.Fatalf("insert account %s: %v", name, err)
		}
		accountIDs[name] = id
	}

	providerIDs := make(map[string]int64)
	for _, provider := range []struct {
		slug, displayName, protocol string
	}{
		{prefix + "-steam", "Steam", "steam"},
		{prefix + "-vrchat", "VRChat", "vrchat"},
	} {
		var id int64
		err := tx.QueryRow(ctx, `
			INSERT INTO upstream_idp (
				slug, display_name, protocol, mode, provider_config, secret_status,
				secret_enc, secret_nonce, key_version, disabled
			)
			VALUES ($1, $2, $3, 'auto_provision', '{}'::jsonb, 'unconfigured', NULL, NULL, NULL, false)
			RETURNING id`, provider.slug, provider.displayName, provider.protocol).Scan(&id)
		if err != nil {
			t.Fatalf("insert provider %s: %v", provider.slug, err)
		}
		providerIDs[provider.protocol] = id
	}

	identityIDs := make(map[string]int64)
	insertIdentity := func(name, protocol, subject, email, data string, linkedAt time.Time) {
		t.Helper()
		var id int64
		err := tx.QueryRow(ctx, `
			INSERT INTO account_identity (
				account_id, upstream_idp_id, upstream_iss, upstream_sub,
				upstream_email, upstream_data, confirmed_at, linked_at
			)
			VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6::jsonb, now(), $7)
			RETURNING id`, accountIDs[name], providerIDs[protocol], "https://"+protocol+".example", subject, email, data, linkedAt).Scan(&id)
		if err != nil {
			t.Fatalf("insert %s %s identity: %v", name, protocol, err)
		}
		identityIDs[name+"-"+protocol] = id
	}
	insertIdentity("alice", "steam", prefix+"_steam_alice", "", fmt.Sprintf(`{"steamId":%q,"personaName":%q,"profileUrl":%q,"avatarUrl":%q}`, prefix+"_steam_alice", prefix+" Other", "https://steamcommunity.com/profiles/"+prefix, "https://cdn.example/"+prefix+"/other"), time.Date(2099, 6, 4, 0, 0, 0, 0, time.UTC))
	insertIdentity("bob", "vrchat", prefix+"_usr_bob", prefix+"@vrchat.example", fmt.Sprintf(`{"userId":%q,"displayName":%q,"profileUrl":%q}`, prefix+"_usr_bob", prefix+" AliceVR", "https://vrchat.com/home/user/"+prefix+"_usr_bob"), time.Date(2099, 6, 3, 0, 0, 0, 0, time.UTC))
	insertIdentity("carol", "steam", prefix+"_steam_carol", "", fmt.Sprintf(`{"steamId":%q,"personaName":%q,"profileUrl":%q,"avatarUrl":%q}`, prefix+"_steam_carol", prefix+" Gaben", "https://steamcommunity.com/profiles/"+prefix+"_steam_carol", "https://cdn.example/"+prefix+"/gaben"), time.Date(2099, 6, 2, 0, 0, 0, 0, time.UTC))
	insertIdentity("carol", "vrchat", prefix+"_usr_carol", "", fmt.Sprintf(`{"userId":%q,"displayName":%q,"profileUrl":%q}`, prefix+"_usr_carol", prefix+" Alice World", "https://vrchat.com/home/user/"+prefix+"_usr_carol"), time.Date(2099, 6, 1, 0, 0, 0, 0, time.UTC))

	text := func(value string) pgtype.Text {
		return pgtype.Text{String: value, Valid: value != ""}
	}
	type expectedRow struct {
		name       string
		identities []int64
	}
	assertRows := func(name string, params db.ListAccountsParams, want []expectedRow) []db.ListAccountsRow {
		t.Helper()
		rows, err := queries.ListAccounts(ctx, params)
		if err != nil {
			t.Fatalf("%s query: %v", name, err)
		}
		if len(rows) != len(want) {
			t.Fatalf("%s rows = %d, want %d: %+v", name, len(rows), len(want), rows)
		}
		for index, row := range rows {
			wantUsername := prefix + "_" + want[index].name
			if row.Username != wantUsername {
				t.Fatalf("%s row[%d] username = %q, want %q", name, index, row.Username, wantUsername)
			}
			var identities []struct {
				ID int64 `json:"id"`
			}
			if err := json.Unmarshal([]byte(row.MatchingIdentities), &identities); err != nil {
				t.Fatalf("%s row[%d] matching identities: %v", name, index, err)
			}
			gotIDs := make([]int64, 0, len(identities))
			for _, identity := range identities {
				gotIDs = append(gotIDs, identity.ID)
			}
			if fmt.Sprint(gotIDs) != fmt.Sprint(want[index].identities) {
				t.Fatalf("%s row[%d] identity IDs = %v, want %v", name, index, gotIDs, want[index].identities)
			}
		}
		return rows
	}

	all := []expectedRow{{"alice", nil}, {"bob", nil}, {"carol", nil}, {"dave", nil}}
	assertRows("unfiltered", db.ListAccountsParams{Limit: 4}, all)
	assertRows("local q", db.ListAccountsParams{Q: text(prefix + " LocalOnly"), Limit: 10}, []expectedRow{{"alice", nil}})
	assertRows("subject q", db.ListAccountsParams{Q: text(prefix + "_USR_BOB"), Limit: 10}, []expectedRow{{"bob", []int64{identityIDs["bob-vrchat"]}}})
	assertRows("email q", db.ListAccountsParams{Q: text(prefix + "@VRCHAT.EXAMPLE"), Limit: 10}, []expectedRow{{"bob", []int64{identityIDs["bob-vrchat"]}}})
	assertRows("Steam persona q", db.ListAccountsParams{Q: text(prefix + " GABEN"), Limit: 10}, []expectedRow{{"carol", []int64{identityIDs["carol-steam"]}}})
	assertRows("VRChat display q", db.ListAccountsParams{Q: text(prefix + " ALICE WORLD"), Limit: 10}, []expectedRow{{"carol", []int64{identityIDs["carol-vrchat"]}}})

	steamSlug := prefix + "-steam"
	vrchatSlug := prefix + "-vrchat"
	assertRows("provider only", db.ListAccountsParams{Provider: text(vrchatSlug), Limit: 10}, []expectedRow{
		{"bob", []int64{identityIDs["bob-vrchat"]}},
		{"carol", []int64{identityIDs["carol-vrchat"]}},
	})
	for _, match := range []struct {
		name, operator, value string
	}{
		{"exact", "exact", prefix + " Gaben"},
		{"prefix", "prefix", prefix + " Ga"},
		{"contains", "contains", prefix + " Gab"},
	} {
		assertRows("Steam persona "+match.name, db.ListAccountsParams{
			Provider: text(steamSlug), Field: text("personaName"), Value: text(match.value), Match: text(match.operator), Limit: 10,
		}, []expectedRow{{"carol", []int64{identityIDs["carol-steam"]}}})
	}
	assertRows("metadata exact is case-sensitive", db.ListAccountsParams{
		Provider: text(steamSlug), Field: text("personaName"), Value: text(prefix + " gaben"), Match: text("exact"), Limit: 10,
	}, nil)
	for _, match := range []struct {
		name, operator, value string
	}{
		{"exact", "exact", prefix + " Alice World"},
		{"prefix", "prefix", prefix + " Alice W"},
		{"contains", "contains", "WORLD"},
	} {
		assertRows("VRChat display "+match.name, db.ListAccountsParams{
			Provider: text(vrchatSlug), Field: text("displayName"), Value: text(match.value), Match: text(match.operator), Limit: 10,
		}, []expectedRow{{"carol", []int64{identityIDs["carol-vrchat"]}}})
	}
	assertRows("same identity composition", db.ListAccountsParams{
		Q: text(prefix + " Alice World"), Provider: text(vrchatSlug), Field: text("displayName"), Value: text("World"), Match: text("contains"), Limit: 10,
	}, []expectedRow{{"carol", []int64{identityIDs["carol-vrchat"]}}})
	assertRows("different identity composition", db.ListAccountsParams{
		Q: text(prefix + " Gaben"), Provider: text(vrchatSlug), Field: text("displayName"), Value: text(prefix + " Alice"), Match: text("contains"), Limit: 10,
	}, []expectedRow{{"carol", []int64{identityIDs["carol-steam"], identityIDs["carol-vrchat"]}}})
	assertRows("no match", db.ListAccountsParams{Q: text(prefix + " not-present"), Limit: 10}, nil)

	if _, err := tx.Exec(ctx, `UPDATE upstream_idp SET disabled = true WHERE id = $1`, providerIDs["vrchat"]); err != nil {
		t.Fatal(err)
	}
	assertRows("disabled provider", db.ListAccountsParams{Provider: text(vrchatSlug), Limit: 10}, []expectedRow{
		{"bob", []int64{identityIDs["bob-vrchat"]}},
		{"carol", []int64{identityIDs["carol-vrchat"]}},
	})

	firstPage := assertRows("filtered first page", db.ListAccountsParams{Q: text(prefix + "@page.test"), Limit: 2}, all[:2])
	assertRows("filtered later page", db.ListAccountsParams{
		Q:              text(prefix + "@page.test"),
		AfterCreatedAt: firstPage[1].CreatedAt,
		AfterID:        pgtype.Int4{Int32: firstPage[1].ID, Valid: true},
		Limit:          2,
	}, all[2:])
}

func TestAccountFilterSchemaPostgres(t *testing.T) {
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

	for _, indexName := range []string{
		"account_created_at_id_idx",
		"account_identity_upstream_idp_account_idx",
		"account_search_trgm_idx",
		"account_identity_search_trgm_idx",
	} {
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes
				WHERE schemaname = current_schema() AND indexname = $1
			)`, indexName).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("missing account filter index %q", indexName)
		}
	}

	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		indexName string
		query     string
	}{
		{
			name:      "local text search",
			indexName: "account_search_trgm_idx",
			query: `EXPLAIN (COSTS OFF)
				SELECT id FROM account
				WHERE (
					username || E'\n' || display_name || E'\n' || COALESCE(email, '')
				) ILIKE '%selective-local-needle%'`,
		},
		{
			name:      "identity text search",
			indexName: "account_identity_search_trgm_idx",
			query: `EXPLAIN (COSTS OFF)
				SELECT account_id FROM account_identity
				WHERE (
					upstream_sub || E'\n' ||
					COALESCE(upstream_email, '') || E'\n' ||
					COALESCE(upstream_data->>'personaName', '') || E'\n' ||
					COALESCE(upstream_data->>'displayName', '') || E'\n' ||
					COALESCE(upstream_data->>'profileUrl', '')
				) ILIKE '%selective-identity-needle%'`,
		},
		{
			name:      "provider filter",
			indexName: "account_identity_upstream_idp_account_idx",
			query: `EXPLAIN (COSTS OFF)
				SELECT account_id FROM account_identity WHERE upstream_idp_id = 1`,
		},
		{
			name:      "exact metadata filter",
			indexName: "account_identity_upstream_data_gin_idx",
			query: `EXPLAIN (COSTS OFF)
				SELECT account_id FROM account_identity
				WHERE upstream_data @> '{"displayName":"selective-metadata-needle"}'::jsonb`,
		},
		{
			name:      "keyset page",
			indexName: "account_created_at_id_idx",
			query: `EXPLAIN (COSTS OFF)
				SELECT id FROM account
				WHERE (created_at, id) < (now(), 2147483647)
				ORDER BY created_at DESC, id DESC LIMIT 50`,
		},
	} {
		rows, err := tx.Query(ctx, tc.query)
		if err != nil {
			t.Fatalf("%s explain: %v", tc.name, err)
		}
		var plan strings.Builder
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				rows.Close()
				t.Fatalf("%s explain row: %v", tc.name, err)
			}
			plan.WriteString(line)
			plan.WriteByte('\n')
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatalf("%s explain rows: %v", tc.name, err)
		}
		if !strings.Contains(plan.String(), tc.indexName) {
			t.Errorf("%s plan does not use %s:\n%s", tc.name, tc.indexName, plan.String())
		}
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO upstream_idp (
			slug, display_name, protocol, mode, provider_config, secret_status,
			secret_enc, secret_nonce, key_version, disabled
		)
		VALUES (' whitespace-slug ', 'Whitespace slug', 'vrchat', 'auto_provision',
			'{}'::jsonb, 'unconfigured', NULL, NULL, NULL, false)`)
	if err == nil {
		t.Fatal("whitespace provider slug was accepted")
	}
	var constraintErr *pgconn.PgError
	if !errors.As(err, &constraintErr) || constraintErr.ConstraintName != "upstream_idp_slug_lowercase_check" {
		t.Fatalf("slug rejection = %v, want upstream_idp_slug_lowercase_check", err)
	}
}
