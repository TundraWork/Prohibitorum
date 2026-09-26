package server

// Package server — admin_write_fixes_integration_test.go
//
// Coverage for the three PHB-71 backend write fixes, each of which was a
// response that disagreed with what the database actually held:
//
//  1. OIDC application PUT validated launchUrl after UpdateOIDCClient had already
//     run, so a rejected URL left the other fields committed and answered 400 —
//     a half-applied write reported as a failure.
//  2. Forward-auth PUT and set-disabled built their view without the stored
//     principal_source, so the response always claimed the default source and a
//     client caching it would show the wrong value until the next full read.
//  3. Deleting an identity provider with linked identities failed against the
//     account_identity foreign key instead of removing provider and links
//     together.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"prohibitorum/pkg/contract"
)

func writeFixSuffix(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(raw)
}

// writeFixServer returns the Server behind the fixture, with a session that
// carries a fresh sudo grant so the sudo-gated raw handlers run their bodies.
func writeFixServer(base *writeFixFixture) *Server {
	base.server.sess = adminSession(time.Now().Add(time.Hour))
	return base.server.server
}

// TestUpdateOIDCApplicationRejectsBadLaunchUrlWithoutWritingPostgres pins the
// order of the PUT: the launchUrl check runs before the row is touched, so a 400
// leaves every column as it was.
func TestUpdateOIDCApplicationRejectsBadLaunchUrlWithoutWritingPostgres(t *testing.T) {
	base := newWriteFixFixture(t)
	ctx := context.Background()
	suffix := writeFixSuffix(t)
	clientID := "order-fix-" + suffix

	if _, err := base.pool.Exec(ctx, `
		INSERT INTO oidc_client (client_id, display_name, redirect_uris, allowed_scopes)
		VALUES ($1, 'Before', ARRAY['https://example.test/callback'], ARRAY['openid'])`, clientID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = base.pool.Exec(context.Background(), `DELETE FROM oidc_client WHERE client_id = $1`, clientID)
	})

	// A deliberately invalid launchUrl alongside a display-name change and a
	// scope change: if the update ran first, both of those would be committed.
	body := `{"displayName":"After","redirectUris":["https://example.test/callback"],
		"postLogoutRedirectUris":[],"allowedScopes":["openid","profile"],
		"requireConsent":true,"disabled":true,"launchUrl":"not-a-url","requirePkce":true}`

	s := writeFixServer(base)
	rr := httptest.NewRecorder()
	req := reqWithSession(http.MethodPut, "/api/prohibitorum/oidc-applications/"+clientID, body, "", base.server.sess)
	req = withClientID(req, clientID)
	s.handleUpdateOIDCApplicationHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", rr.Code, rr.Body.String())
	}

	var displayName string
	var scopes []string
	var disabled, requireConsent bool
	if err := base.pool.QueryRow(ctx, `
		SELECT display_name, allowed_scopes, disabled, require_consent
		FROM oidc_client WHERE client_id = $1`, clientID,
	).Scan(&displayName, &scopes, &disabled, &requireConsent); err != nil {
		t.Fatal(err)
	}
	if displayName != "Before" {
		t.Errorf("display_name = %q, want %q — the rejected PUT wrote through", displayName, "Before")
	}
	if len(scopes) != 1 || scopes[0] != "openid" {
		t.Errorf("allowed_scopes = %v, want [openid] — the rejected PUT wrote through", scopes)
	}
	if disabled || requireConsent {
		t.Errorf("disabled=%v require_consent=%v, want both false — the rejected PUT wrote through", disabled, requireConsent)
	}
}

// TestUpdateOIDCApplicationAcceptsValidLaunchUrlPostgres is the companion: a
// valid launchUrl still stores and the response reports it.
func TestUpdateOIDCApplicationAcceptsValidLaunchUrlPostgres(t *testing.T) {
	base := newWriteFixFixture(t)
	ctx := context.Background()
	suffix := writeFixSuffix(t)
	clientID := "order-ok-" + suffix

	if _, err := base.pool.Exec(ctx, `
		INSERT INTO oidc_client (client_id, display_name, redirect_uris, allowed_scopes)
		VALUES ($1, 'Before', ARRAY['https://example.test/callback'], ARRAY['openid'])`, clientID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = base.pool.Exec(context.Background(), `DELETE FROM oidc_client WHERE client_id = $1`, clientID)
	})

	body := `{"displayName":"After","redirectUris":["https://example.test/callback"],
		"postLogoutRedirectUris":[],"allowedScopes":["openid"],
		"requireConsent":false,"disabled":false,"launchUrl":"https://app.example.test/start","requirePkce":true}`

	s := writeFixServer(base)
	rr := httptest.NewRecorder()
	req := reqWithSession(http.MethodPut, "/api/prohibitorum/oidc-applications/"+clientID, body, "", base.server.sess)
	req = withClientID(req, clientID)
	s.handleUpdateOIDCApplicationHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rr.Code, rr.Body.String())
	}
	var view contract.OIDCApplicationView
	if err := json.Unmarshal(rr.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.LaunchURL == nil || *view.LaunchURL != "https://app.example.test/start" {
		t.Errorf("response launchUrl = %v, want the stored URL", view.LaunchURL)
	}
	if view.DisplayName != "After" {
		t.Errorf("response displayName = %q, want After", view.DisplayName)
	}
}

// TestForwardAuthResponsesCarryStoredRemoteUserSourcePostgres covers both write
// paths that used to answer with the default source regardless of what the row
// held.
func TestForwardAuthResponsesCarryStoredRemoteUserSourcePostgres(t *testing.T) {
	base := newWriteFixFixture(t)
	ctx := context.Background()
	suffix := writeFixSuffix(t)
	clientID := "fa-source-" + suffix

	if _, err := base.pool.Exec(ctx, `
		INSERT INTO oidc_client (client_id, display_name, redirect_uris, allowed_scopes,
			forward_auth_enabled, forward_auth_host, principal_source)
		VALUES ($1, 'FA', ARRAY['https://app.example.test/callback'], ARRAY['openid'],
			true, 'app.example.test', 'verified_email')`, clientID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = base.pool.Exec(context.Background(), `DELETE FROM oidc_client WHERE client_id = $1`, clientID)
	})

	s := writeFixServer(base)

	// PUT: the response must echo the source the row holds, not the default.
	putBody := `{"displayName":"FA renamed","host":"app.example.test","scopes":[]}`
	rr := httptest.NewRecorder()
	req := reqWithSession(http.MethodPut, "/api/prohibitorum/forward-auth-apps/"+clientID, putBody, "", base.server.sess)
	req = withClientID(req, clientID)
	s.handleUpdateForwardAuthAppHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200; body %s", rr.Code, rr.Body.String())
	}
	var putView contract.ForwardAuthAppView
	if err := json.Unmarshal(rr.Body.Bytes(), &putView); err != nil {
		t.Fatal(err)
	}
	if putView.RemoteUserSource != "verified_email" {
		t.Errorf("PUT remoteUserSource = %q, want verified_email", putView.RemoteUserSource)
	}

	// set-disabled: same expectation on the other write path.
	disableBody := `{"clientId":"` + clientID + `","disabled":true}`
	rr = httptest.NewRecorder()
	req = reqWithSession(http.MethodPost, "/api/prohibitorum/forward-auth-apps/set-disabled", disableBody, "", base.server.sess)
	s.handleSetForwardAuthAppDisabledHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("set-disabled status = %d, want 200; body %s", rr.Code, rr.Body.String())
	}
	var disabledView contract.ForwardAuthAppView
	if err := json.Unmarshal(rr.Body.Bytes(), &disabledView); err != nil {
		t.Fatal(err)
	}
	if disabledView.RemoteUserSource != "verified_email" {
		t.Errorf("set-disabled remoteUserSource = %q, want verified_email", disabledView.RemoteUserSource)
	}
	if !disabledView.Disabled {
		t.Error("set-disabled response did not report the new disabled state")
	}
}

// TestDeleteIdentityProviderRemovesLinkedIdentitiesPostgres is the #960 q3
// behaviour: the provider and its identities go together, the accounts stay, and
// the single-provider GET reports the linked count the confirmation dialog needs.
func TestDeleteIdentityProviderRemovesLinkedIdentitiesPostgres(t *testing.T) {
	base := newWriteFixFixture(t)
	ctx := context.Background()
	suffix := writeFixSuffix(t)
	slug := "del-provider-" + suffix

	var providerID int64
	if err := base.pool.QueryRow(ctx, `
		INSERT INTO upstream_idp (slug, display_name, mode, protocol,
			secret_enc, secret_nonce, key_version, secret_status)
		VALUES ($1, 'Doomed SSO', 'link_only', 'steam', $2, $3, 1, 'configured')
		RETURNING id`, slug, []byte{0x02}, bytes.Repeat([]byte{0x03}, 12)).Scan(&providerID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = base.pool.Exec(context.Background(), `DELETE FROM upstream_idp WHERE id = $1`, providerID)
	})

	// Two accounts, one of them linked twice (two subjects) so the distinct
	// count proves the query counts accounts rather than identity rows.
	accountIDs := make([]int32, 0, 2)
	for i := 0; i < 2; i++ {
		var id int32
		if err := base.pool.QueryRow(ctx, `
			INSERT INTO account (username, display_name, webauthn_user_handle, email)
			VALUES ($1, 'Linked', $2, $3) RETURNING id`,
			slug+"_acct_"+hex.EncodeToString([]byte{byte('a' + i)}),
			append([]byte(suffix), byte(i)), slug+"_"+hex.EncodeToString([]byte{byte('a' + i)})+"@del.test",
		).Scan(&id); err != nil {
			t.Fatal(err)
		}
		accountIDs = append(accountIDs, id)
		t.Cleanup(func() {
			_, _ = base.pool.Exec(context.Background(), `DELETE FROM account WHERE id = $1`, id)
		})
	}
	subjects := []string{"s1", "s2", "s3"}
	owners := []int32{accountIDs[0], accountIDs[0], accountIDs[1]}
	for i, sub := range subjects {
		if _, err := base.pool.Exec(ctx, `
			INSERT INTO account_identity (account_id, upstream_idp_id, upstream_iss, upstream_sub)
			VALUES ($1, $2, $3, $4)`, owners[i], providerID, "https://issuer.example.test", sub); err != nil {
			t.Fatal(err)
		}
	}

	s := writeFixServer(base)

	// The detail read reports two distinct accounts, not three identity rows.
	getRR := httptest.NewRecorder()
	getReq := reqWithSession(http.MethodGet, "/api/prohibitorum/identity-providers/"+slug, "", "", base.server.sess)
	getReq = withPathParam(getReq, "slug", slug)
	out, err := s.handleGetIdentityProvider(getReq.Context(), &getIdentityProviderIn{Slug: slug})
	if err != nil {
		t.Fatalf("handleGetIdentityProvider: %v", err)
	}
	if out.Body.LinkedAccountCount == nil {
		t.Fatal("linkedAccountCount missing from the single-provider read")
	}
	if got := *out.Body.LinkedAccountCount; got != 2 {
		t.Errorf("linkedAccountCount = %d, want 2 distinct accounts", got)
	}
	_ = getRR

	// The delete succeeds despite the links and takes the identities with it.
	delRR := httptest.NewRecorder()
	delReq := reqWithSession(http.MethodPost, "/api/prohibitorum/identity-providers/delete",
		`{"slug":"`+slug+`"}`, "", base.server.sess)
	s.handleDeleteIdentityProviderHTTP(delRR, delReq)
	if delRR.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204; body %s", delRR.Code, delRR.Body.String())
	}

	var providerCount, identityCount int
	if err := base.pool.QueryRow(ctx, `SELECT COUNT(*) FROM upstream_idp WHERE id = $1`, providerID).Scan(&providerCount); err != nil {
		t.Fatal(err)
	}
	if providerCount != 0 {
		t.Errorf("provider still present after delete")
	}
	if err := base.pool.QueryRow(ctx, `SELECT COUNT(*) FROM account_identity WHERE upstream_idp_id = $1`, providerID).Scan(&identityCount); err != nil {
		t.Fatal(err)
	}
	if identityCount != 0 {
		t.Errorf("%d identities survived the provider delete, want 0", identityCount)
	}
	for _, id := range accountIDs {
		var exists int
		if err := base.pool.QueryRow(ctx, `SELECT COUNT(*) FROM account WHERE id = $1`, id).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists != 1 {
			t.Errorf("account %d was deleted along with the provider", id)
		}
	}
}

// TestDeleteIdentityProviderWithoutLinksPostgres covers the other branch of the
// confirmation copy: no links, so the count is zero.
func TestDeleteIdentityProviderWithoutLinksPostgres(t *testing.T) {
	base := newWriteFixFixture(t)
	ctx := context.Background()
	suffix := writeFixSuffix(t)
	slug := "del-empty-" + suffix

	if _, err := base.pool.Exec(ctx, `
		INSERT INTO upstream_idp (slug, display_name, mode, protocol,
			secret_enc, secret_nonce, key_version, secret_status)
		VALUES ($1, 'Unused SSO', 'link_only', 'steam', $2, $3, 1, 'configured')`,
		slug, []byte{0x04}, bytes.Repeat([]byte{0x05}, 12)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = base.pool.Exec(context.Background(), `DELETE FROM upstream_idp WHERE slug = $1`, slug)
	})

	s := writeFixServer(base)
	out, err := s.handleGetIdentityProvider(ctx, &getIdentityProviderIn{Slug: slug})
	if err != nil {
		t.Fatalf("handleGetIdentityProvider: %v", err)
	}
	if out.Body.LinkedAccountCount == nil || *out.Body.LinkedAccountCount != 0 {
		t.Errorf("linkedAccountCount = %v, want 0", out.Body.LinkedAccountCount)
	}

	rr := httptest.NewRecorder()
	req := reqWithSession(http.MethodPost, "/api/prohibitorum/identity-providers/delete",
		`{"slug":"`+slug+`"}`, "", base.server.sess)
	s.handleDeleteIdentityProviderHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204; body %s", rr.Code, rr.Body.String())
	}
}

// writeFixFixture pairs the live server with the pool its rows are committed to,
// so a test can read back what a handler wrote. These handlers commit through
// s.queries, so the rows have to be real and cleaned up explicitly.
type writeFixFixture struct {
	server *livePaginationServer
	pool   *pgxpool.Pool
}

func newWriteFixFixture(t *testing.T) *writeFixFixture {
	t.Helper()
	pool := enrollmentLockTestPool(t)
	return &writeFixFixture{server: newLivePaginationServer(t, pool), pool: pool}
}

// withPathParam injects a chi URL parameter, the way the real router does.
func withPathParam(req *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}
