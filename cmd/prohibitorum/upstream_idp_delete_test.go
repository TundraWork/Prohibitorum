package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"prohibitorum/db/migrations"
	"prohibitorum/pkg/db"
	protocoloidc "prohibitorum/pkg/protocol/oidc"
	"prohibitorum/pkg/server"
)

// cliDeleteTestPool gives the test an isolated schema (fresh migrations) on the
// shared test Postgres, so the CLI delete commits against a database only this
// test touched.
func cliDeleteTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	baseURL := os.Getenv("PROHIBITORUM_TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("PROHIBITORUM_TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	basePool, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}

	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		basePool.Close()
		t.Fatal(err)
	}
	schema := "cli_delete_" + hex.EncodeToString(nonce[:])
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := basePool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		basePool.Close()
		t.Fatal(err)
	}
	// 032 installs pg_trgm into the first schema on the search_path, so a
	// leftover schema breaks later tests' migrations; drop immediately on any
	// fixture failure below, not just on test success.
	t.Cleanup(func() {
		_, _ = basePool.Exec(context.Background(), "DROP SCHEMA "+quotedSchema+" CASCADE")
		basePool.Close()
	})
	if _, err := basePool.Exec(ctx, `CREATE TABLE `+quotedSchema+`.goose_db_version (
		id serial PRIMARY KEY,
		version_id bigint NOT NULL,
		is_applied boolean NOT NULL,
		tstamp timestamp NULL DEFAULT now()
	)`); err != nil {
		t.Fatalf("create schema migration metadata: %v", err)
	}
	if _, err := basePool.Exec(ctx, `INSERT INTO `+quotedSchema+`.goose_db_version (version_id, is_applied) VALUES (0, true)`); err != nil {
		t.Fatalf("initialize schema migration metadata: %v", err)
	}

	schemaURL, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := schemaURL.Query()
	query.Set("search_path", schema+",public")
	schemaURL.RawQuery = query.Encode()
	if _, err := migrations.UpWithResult(schemaURL.String()); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	pool, err := pgxpool.New(ctx, schemaURL.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestDeleteUpstreamIDPCLIWithLinkedAccountPostgres drives the CLI delete path
// on a provider with a linked account, a pending invitation riding its verified
// snapshot, and a client + consent carrying its identity scope. The RESTRICT
// foreign key on account_identity makes the pre-fix delete fail here.
func TestDeleteUpstreamIDPCLIWithLinkedAccountPostgres(t *testing.T) {
	pool := cliDeleteTestPool(t)
	ctx := context.Background()
	suffix := hex.EncodeToString([]byte(t.Name()))[:8]
	slug := "cli-del-" + suffix

	var providerID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO upstream_idp (slug, display_name, mode, protocol,
			secret_enc, secret_nonce, key_version, secret_status)
		VALUES ($1, 'CLI Delete SSO', 'link_only', 'steam', $2, $3, 1, 'configured')
		RETURNING id`, slug, []byte{0x02}, []byte{0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03}).Scan(&providerID); err != nil {
		t.Fatal(err)
	}

	var accountID int32
	if err := pool.QueryRow(ctx, `
		INSERT INTO account (username, display_name, webauthn_user_handle, email)
		VALUES ($1, 'CLI Delete Account', $2, $3) RETURNING id`,
		slug+"_acct", append([]byte(suffix), 0x01), slug+"@cli-del.test").Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO account_identity (account_id, upstream_idp_id, upstream_iss, upstream_sub)
		VALUES ($1, $2, 'https://issuer.example.test', 'sub-1')`, accountID, providerID); err != nil {
		t.Fatal(err)
	}

	// A pending invitation whose verified snapshot points at the provider; the
	// snapshot FK cascades it away with the provider.
	if _, err := pool.Exec(ctx, `
		INSERT INTO enrollment (token, intent, expires_at,
			federated_upstream_idp_id, federated_upstream_idp_slug,
			federated_upstream_iss, federated_upstream_sub,
			federated_display_name, federated_upstream_data)
		VALUES ($1, 'invite', now() + interval '1 hour', $2, $3,
			'https://issuer.example.test', 'sub-1', 'Invited', '{}'::jsonb)`,
		"tok_"+suffix, providerID, slug); err != nil {
		t.Fatal(err)
	}

	// A client allows this provider's identity scope and an account consented
	// to it; the delete must take the scope from both without touching the
	// client's other scopes.
	identityScope := "identity:" + slug
	queries := db.New(pool)
	params, _, err := protocoloidc.BuildClientParams(protocoloidc.ClientOptions{
		ClientID: slug + "-rp", DisplayName: slug + "-rp",
		RedirectURIs: []string{"https://rp.example.test/cb"},
		Scopes:       []string{"openid", identityScope, "profile"},
		RequirePKCE:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := queries.InsertOIDCClient(ctx, params); err != nil {
		t.Fatal(err)
	}
	if err := queries.UpsertConsent(ctx, db.UpsertConsentParams{
		AccountID: accountID, ClientID: slug + "-rp", GrantedScopes: []string{"openid", identityScope},
	}); err != nil {
		t.Fatal(err)
	}

	summary, err := runUpstreamIDPDelete(ctx, pool, slug)
	if err != nil {
		t.Fatalf("CLI delete failed on a provider with a linked account: %v", err)
	}
	if summary.Slug != slug || summary.ProviderID != providerID {
		t.Errorf("summary = %+v, want slug %q provider %d", summary, slug, providerID)
	}
	if summary.UnlinkedAccounts != 1 {
		t.Errorf("unlinkedAccounts = %d, want 1", summary.UnlinkedAccounts)
	}
	if summary.DeletedPendingInvitations != 1 {
		t.Errorf("deletedPendingInvitations = %d, want 1", summary.DeletedPendingInvitations)
	}
	if summary.RemovedClientScopes != 1 || summary.RemovedConsentScopes != 1 {
		t.Errorf("removedClientScopes = %d, removedConsentScopes = %d, want 1 and 1",
			summary.RemovedClientScopes, summary.RemovedConsentScopes)
	}

	var providerCount, identityCount, invitationCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM upstream_idp WHERE id = $1`, providerID).Scan(&providerCount); err != nil {
		t.Fatal(err)
	}
	if providerCount != 0 {
		t.Errorf("provider still present after CLI delete")
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM account_identity WHERE upstream_idp_id = $1`, providerID).Scan(&identityCount); err != nil {
		t.Fatal(err)
	}
	if identityCount != 0 {
		t.Errorf("%d identities survived the CLI delete, want 0", identityCount)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM enrollment WHERE token = $1`, "tok_"+suffix).Scan(&invitationCount); err != nil {
		t.Fatal(err)
	}
	if invitationCount != 0 {
		t.Errorf("pending invitation survived the CLI delete, want 0")
	}

	var clientScopes, consentScopes []string
	if err := pool.QueryRow(ctx, `SELECT allowed_scopes FROM oidc_client WHERE client_id = $1`, slug+"-rp").Scan(&clientScopes); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(clientScopes, []string{"openid", "profile"}) {
		t.Errorf("client scopes after CLI delete = %v, want [openid profile]", clientScopes)
	}
	if err := pool.QueryRow(ctx, `SELECT granted_scopes FROM oidc_consent WHERE client_id = $1`, slug+"-rp").Scan(&consentScopes); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(consentScopes, []string{"openid"}) {
		t.Errorf("consent scopes after CLI delete = %v, want [openid]", consentScopes)
	}

	var accountExists int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM account WHERE id = $1`, accountID).Scan(&accountExists); err != nil {
		t.Fatal(err)
	}
	if accountExists != 1 {
		t.Errorf("account was deleted along with the provider")
	}
}

// TestDeleteUpstreamIDPCLIRollsBackOnFailurePostgres fails the provider delete
// mid-transaction (a BEFORE DELETE trigger on upstream_idp) and asserts the
// transaction rolled back: the linked account_identity row must survive. This
// is the test that catches the delete path running its statements outside one
// transaction — with autocommit, the identities are already gone when the
// provider delete fails.
func TestDeleteUpstreamIDPCLIRollsBackOnFailurePostgres(t *testing.T) {
	pool := cliDeleteTestPool(t)
	ctx := context.Background()
	suffix := hex.EncodeToString([]byte(t.Name()))[:8]
	slug := "cli-rb-" + suffix

	var providerID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO upstream_idp (slug, display_name, mode, protocol,
			secret_enc, secret_nonce, key_version, secret_status)
		VALUES ($1, 'Rollback SSO', 'link_only', 'steam', $2, $3, 1, 'configured')
		RETURNING id`, slug, []byte{0x02}, []byte{0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03}).Scan(&providerID); err != nil {
		t.Fatal(err)
	}
	var accountID int32
	if err := pool.QueryRow(ctx, `
		INSERT INTO account (username, display_name, webauthn_user_handle, email)
		VALUES ($1, 'Rollback Account', $2, $3) RETURNING id`,
		slug+"_acct", append([]byte(suffix), 0x02), slug+"@cli-rb.test").Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO account_identity (account_id, upstream_idp_id, upstream_iss, upstream_sub)
		VALUES ($1, $2, 'https://issuer.example.test', 'sub-1')`, accountID, providerID); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `CREATE FUNCTION phb133_fail_delete() RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION 'phb133: provider delete refused';
		END; $$ LANGUAGE plpgsql`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE TRIGGER phb133_fail_delete BEFORE DELETE ON upstream_idp
		FOR EACH ROW EXECUTE FUNCTION phb133_fail_delete()`); err != nil {
		t.Fatal(err)
	}

	if _, err := runUpstreamIDPDelete(ctx, pool, slug); err == nil {
		t.Fatal("delete succeeded although the provider delete raised; want failure")
	}

	var identityCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM account_identity WHERE upstream_idp_id = $1`, providerID).Scan(&identityCount); err != nil {
		t.Fatal(err)
	}
	if identityCount != 1 {
		t.Fatalf("account_identity rows after failed delete = %d, want 1: the identity delete did not roll back with the failed provider delete", identityCount)
	}
	var providerCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM upstream_idp WHERE id = $1`, providerID).Scan(&providerCount); err != nil {
		t.Fatal(err)
	}
	if providerCount != 1 {
		t.Errorf("provider gone after failed delete, want still present")
	}
}

// The summary the CLI prints must name every class of removed data, mirroring
// the admin dialog's confirmation copy.
func TestUpstreamIDPDeleteSummaryText(t *testing.T) {
	summary := server.UpstreamIDPDeleteSummary{
		Slug:                      "corp",
		ProviderID:                7,
		IdentityScope:             "identity:corp",
		UnlinkedAccounts:          2,
		DeletedPendingInvitations: 1,
		RemovedClientScopes:       3,
		RemovedConsentScopes:      4,
	}
	got := summary.String()
	for _, want := range []string{"corp", "identity:corp", "2", "1", "3", "4"} {
		if !strings.Contains(got, want) {
			t.Errorf("delete summary %q missing %q", got, want)
		}
	}
}
