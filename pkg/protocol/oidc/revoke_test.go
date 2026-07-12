package oidc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"prohibitorum/pkg/audit"
)

// revokeReq builds a client_secret_basic POST /oauth/revoke carrying token.
func revokeReq(token string) *http.Request {
	form := url.Values{}
	form.Set("token", token)
	req := httptest.NewRequest(http.MethodPost, "/oauth/revoke", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(testClientID, testSecret)
	return req
}

func (h *endpointHarness) sawRevokedAudit(tokenType string) bool {
	for _, r := range h.audit.records {
		if r.Factor == audit.FactorOIDCClient && r.Event == audit.EventRevoke &&
			r.Detail["reason"] == "revoked" && r.Detail["token_type"] == tokenType {
			return true
		}
	}
	return false
}

// revokedAuditAccountID returns the AccountID from the revoke audit record for
// the given tokenType, or nil if no such record was found.
func (h *endpointHarness) revokedAuditAccountID(tokenType string) *int32 {
	for _, r := range h.audit.records {
		if r.Factor == audit.FactorOIDCClient && r.Event == audit.EventRevoke &&
			r.Detail["token_type"] == tokenType {
			return r.AccountID
		}
	}
	return nil
}

func TestRevokeAccessToken(t *testing.T) {
	h := newEndpointHarness(t)
	at := h.mintAccessToken(t, testSubject, testClientID, "openid", "jti-r1", time.Now().Add(time.Hour))

	rec := httptest.NewRecorder()
	h.p.HandleRevoke(rec, revokeReq(at))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if len(h.q.inserted) != 1 {
		t.Fatalf("expected 1 InsertRevokedJTI call, got %d", len(h.q.inserted))
	}
	if h.q.inserted[0].Jti != "jti-r1" {
		t.Fatalf("revoked jti = %q, want jti-r1", h.q.inserted[0].Jti)
	}
	if h.q.inserted[0].Reason.String != "revoke" {
		t.Fatalf("reason = %q, want revoke", h.q.inserted[0].Reason.String)
	}
	if !h.q.inserted[0].ExpiresAt.Valid {
		t.Fatal("expected a valid ExpiresAt on the denylist row")
	}
	if !h.sawRevokedAudit("access_token") {
		t.Fatal("expected a revoked audit record for access_token")
	}
}

func TestRevokeRefreshToken(t *testing.T) {
	h := newEndpointHarness(t)
	ctx := context.Background()

	rt, _, err := issueRefresh(ctx, h.p.kv, refreshFamily{
		ClientID:  testClientID,
		AccountID: 7,
		Scope:     []string{"openid", "offline_access"},
	}, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}
	// Family resolves before revocation.
	if _, ok := lookupRefresh(ctx, h.p.kv, rt); !ok {
		t.Fatal("refresh family should resolve before revoke")
	}

	rec := httptest.NewRecorder()
	h.p.HandleRevoke(rec, revokeReq(rt))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if _, ok := lookupRefresh(ctx, h.p.kv, rt); ok {
		t.Fatal("refresh family should be dead after revoke")
	}
	if len(h.q.inserted) != 0 {
		t.Fatalf("refresh revoke must not touch the jti denylist; got %d inserts", len(h.q.inserted))
	}
	if !h.sawRevokedAudit("refresh_token") {
		t.Fatal("expected a revoked audit record for refresh_token")
	}
}

func TestRevokeGarbageStill200(t *testing.T) {
	h := newEndpointHarness(t)
	rec := httptest.NewRecorder()
	h.p.HandleRevoke(rec, revokeReq("not-a-token"))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 for unknown token, got %d", rec.Code)
	}
	if len(h.q.inserted) != 0 {
		t.Fatalf("unknown token must not be denylisted; got %d inserts", len(h.q.inserted))
	}
}

func TestRevokeEmptyTokenStill200(t *testing.T) {
	h := newEndpointHarness(t)
	form := url.Values{} // no token
	req := httptest.NewRequest(http.MethodPost, "/oauth/revoke", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(testClientID, testSecret)

	rec := httptest.NewRecorder()
	h.p.HandleRevoke(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
}

func TestRevokeOtherClientsAccessTokenNotRevoked(t *testing.T) {
	h := newEndpointHarness(t)
	h.q.clients["other"] = confidentialClient(t, "other", "othersecret", "client_secret_basic")
	at := h.mintAccessToken(t, testSubject, "other", "openid", "jti-r2", time.Now().Add(time.Hour))

	// testClientID tries to revoke another client's token.
	rec := httptest.NewRecorder()
	h.p.HandleRevoke(rec, revokeReq(at))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if len(h.q.inserted) != 0 {
		t.Fatalf("must not revoke another client's token; got %d inserts", len(h.q.inserted))
	}
}

func TestRevokeOtherClientsRefreshNotRevoked(t *testing.T) {
	h := newEndpointHarness(t)
	ctx := context.Background()
	rt, _, err := issueRefresh(ctx, h.p.kv, refreshFamily{
		ClientID:  "other",
		AccountID: 7,
		Scope:     []string{"openid"},
	}, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	rec := httptest.NewRecorder()
	h.p.HandleRevoke(rec, revokeReq(rt))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if _, ok := lookupRefresh(ctx, h.p.kv, rt); !ok {
		t.Fatal("another client's refresh family must remain live")
	}
}

func TestRevokeBadClientAuth(t *testing.T) {
	h := newEndpointHarness(t)
	at := h.mintAccessToken(t, testSubject, testClientID, "openid", "jti-r3", time.Now().Add(time.Hour))

	form := url.Values{}
	form.Set("token", at)
	req := httptest.NewRequest(http.MethodPost, "/oauth/revoke", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(testClientID, "wrong-secret")

	rec := httptest.NewRecorder()
	h.p.HandleRevoke(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	if got := decodeError(t, rec); got != errCodeInvalidClient {
		t.Fatalf("error = %q, want %q", got, errCodeInvalidClient)
	}
}

// TestRevokeAccessTokenCarriesAccountID verifies that revoking an access token
// produces an audit record attributed to the token's subject account (id 7).
func TestRevokeAccessTokenCarriesAccountID(t *testing.T) {
	h := newEndpointHarness(t)
	at := h.mintAccessToken(t, testSubject, testClientID, "openid", "jti-aid1", time.Now().Add(time.Hour))

	rec := httptest.NewRecorder()
	h.p.HandleRevoke(rec, revokeReq(at))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	acctID := h.revokedAuditAccountID("access_token")
	if acctID == nil || *acctID != 7 {
		t.Fatalf("access_token revoke AccountID = %v, want 7", acctID)
	}
}

// TestRevokeRefreshTokenCarriesAccountID verifies that revoking a refresh token
// produces an audit record attributed to the family's AccountID (7).
func TestRevokeRefreshTokenCarriesAccountID(t *testing.T) {
	h := newEndpointHarness(t)
	ctx := context.Background()

	rt, _, err := issueRefresh(ctx, h.p.kv, refreshFamily{
		ClientID:  testClientID,
		AccountID: 7,
		Scope:     []string{"openid", "offline_access"},
	}, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	rec := httptest.NewRecorder()
	h.p.HandleRevoke(rec, revokeReq(rt))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	acctID := h.revokedAuditAccountID("refresh_token")
	if acctID == nil || *acctID != 7 {
		t.Fatalf("refresh_token revoke AccountID = %v, want 7", acctID)
	}
}
