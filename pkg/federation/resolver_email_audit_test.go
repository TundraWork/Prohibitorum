package federation_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/db"
	federation "prohibitorum/pkg/federation"
)

// TestAutoProvisionAbsorptionAuditSurvivesTheTransaction pins the review's
// B5: the email/verified audit row for an absorbed upstream address must go
// through the transaction-scoped writer. With the pool-level writer, the
// credential_event FK to account.id cannot see the uncommitted account row,
// the insert fails, and the audit row is silently dropped — after this test's
// Resolve returns, no factor=email row exists for the new account.
func TestAutoProvisionAbsorptionAuditSurvivesTheTransaction(t *testing.T) {
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

	q := db.New(tx)
	// A real upstream_idp row: the absorbed address records which provider
	// verified it (verified_by_idp_id FK). It must be COMMITTED before the
	// resolver runs — the resolver opens its own transaction, which otherwise
	// cannot see an idp row living only in this test transaction.
	var idpID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO upstream_idp (slug, display_name, protocol, mode, provider_config, secret_status, secret_enc, secret_nonce, key_version)
		VALUES ('absorb-audit-idp-' || md5(random()::text), 'Audit IdP', 'oidc', 'auto_provision', '{}'::jsonb, 'unconfigured', NULL, NULL, NULL)
		RETURNING id`).Scan(&idpID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM upstream_idp WHERE id = $1`, idpID) //nolint:errcheck
	// The outer writer binds to a SEPARATE pool connection, exactly like the
	// production writer does: it cannot see the resolver's transaction.
	outerConn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer outerConn.Release()
	outerWriter := audit.NewWriter(db.New(outerConn))

	// Unique per run: the resolver commits its own transaction, so prior runs
	// leave a committed account_identity behind keyed on (iss, sub).
	nonce := fmt.Sprintf("%d", time.Now().UnixNano())
	idp := &federation.Provider{
		ID: idpID, Slug: "absorb-audit-idp-" + nonce, DisplayName: "Audit IdP", Protocol: "oidc",
		Mode: federation.ModeAutoProvision,
		Config: json.RawMessage(`{
			"issuerUrl": "https://audit-idp.example",
			"clientId": "cid",
			"usernameClaim": "preferred_username",
			"displayNameClaim": "name",
			"emailClaim": "email",
			"requireVerifiedEmail": false
		}`),
	}
	identity := &federation.VerifiedIdentity{
		Issuer: "https://audit-idp.example/" + nonce, Subject: "audit-sub-" + nonce,
		Email: strptr("absorb-audit-" + nonce + "@example.test"), EmailVerified: true,
		Username: "absorb_audit_" + nonce, DisplayName: "Absorb Audit",
		AMR:          []string{"pwd"},
		UpstreamData: map[string]string{"providerUserId": "usr_audit"},
	}

	out, err := federation.Resolve(ctx, q, outerWriter, idp, identity, pool)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !out.IsNew {
		t.Fatalf("want a freshly provisioned account, got existing id %d", out.AccountID)
	}

	// The uncommitted tx still holds the account; the email audit row must
	// have been written through the SAME tx to pass the FK check. Read it on
	// the tx connection (it is uncommitted until the test rolls back).
	var emailEvents int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM credential_event
		WHERE account_id = $1 AND factor = 'email' AND event = 'verified'`,
		out.AccountID).Scan(&emailEvents); err != nil {
		t.Fatal(err)
	}
	if emailEvents != 1 {
		t.Fatalf("factor=email verified audit rows for the new account = %d, want 1 (the pool-level writer's FK check drops it)", emailEvents)
	}
}

func strptr(v string) *string { return &v }
