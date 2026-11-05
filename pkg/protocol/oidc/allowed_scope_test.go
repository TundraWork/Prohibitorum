package oidc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"prohibitorum/pkg/audit"
)

// narrowTokenClient replaces the token harness client's allowed_scopes, as an
// admin edit of the application does.
func narrowTokenClient(h *tokenHarness, allowed ...string) {
	q := h.p.queries.(*fakeTokenQueries)
	c := q.clients[testClientID]
	c.AllowedScopes = allowed
	q.clients[testClientID] = c
}

func narrowEndpointClient(h *endpointHarness, allowed ...string) {
	c := h.q.clients[testClientID]
	c.AllowedScopes = allowed
	h.q.clients[testClientID] = c
}

func refreshOK(t *testing.T, h *tokenHarness, presented string) tokenResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(refreshForm(presented)))
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

func sawAuditReason(h *tokenHarness, reason string) bool {
	return slices.ContainsFunc(h.audit.records, func(r audit.Record) bool {
		return r.Event == audit.EventFail && r.Detail["reason"] == reason
	})
}

// TestTokenRefreshLeavesOutScopesTakenOffClient verifies that a refresh after
// an admin takes scopes off the client issues tokens without them, on every
// rotation, and that putting a scope back restores it to the existing grant.
func TestTokenRefreshLeavesOutScopesTakenOffClient(t *testing.T) {
	h := newTokenHarness(t)
	ctx := context.Background()
	h.p.queries.(*fakeTokenQueries).knownSlugs = []string{"vrchat"}

	fam := grantFamily()
	fam.Scope = []string{"openid", "profile", "email", "groups", "offline_access", "identity:vrchat"}
	presented, _, err := issueRefresh(ctx, h.p.kv, fam, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	narrowTokenClient(h, "openid", "profile", "offline_access")

	resp := refreshOK(t, h, presented)
	if resp.Scope != "openid profile offline_access" {
		t.Fatalf("scope = %q, want openid profile offline_access", resp.Scope)
	}
	atClaims, _, err := h.p.verifyJWT(ctx, resp.AccessToken)
	if err != nil {
		t.Fatalf("verify access token: %v", err)
	}
	if atClaims["scope"] != "openid profile offline_access" {
		t.Fatalf("access token scope = %v", atClaims["scope"])
	}
	idClaims, _, err := h.p.verifyJWT(ctx, resp.IDToken)
	if err != nil {
		t.Fatalf("verify id token: %v", err)
	}
	for _, claim := range []string{"email", "groups", "identities"} {
		if _, ok := idClaims[claim]; ok {
			t.Fatalf("id token carries %s after its scope was taken off the client: %v", claim, idClaims[claim])
		}
	}
	if idClaims["preferred_username"] == nil {
		t.Fatal("id token lost the profile claims the client is still allowed")
	}

	second := refreshOK(t, h, resp.RefreshToken)
	if second.Scope != "openid profile offline_access" {
		t.Fatalf("second rotation scope = %q", second.Scope)
	}

	narrowTokenClient(h, "openid", "profile", "email", "offline_access")
	third := refreshOK(t, h, second.RefreshToken)
	if third.Scope != "openid profile email offline_access" {
		t.Fatalf("scope after email is allowed again = %q", third.Scope)
	}
}

// TestTokenRefreshRefusedWithoutRequiredScope verifies that once the client
// is no longer allowed openid or offline_access the refresh is refused with
// invalid_grant, the family revoked and the refusal audited.
func TestTokenRefreshRefusedWithoutRequiredScope(t *testing.T) {
	for _, tc := range []struct {
		name    string
		allowed []string
	}{
		{"offline_access removed", []string{"openid", "profile"}},
		{"openid removed", []string{"profile", "offline_access"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTokenHarness(t)
			ctx := context.Background()
			presented, _, err := issueRefresh(ctx, h.p.kv, grantFamily(), RefreshTokenTTL, RefreshTokenTTL)
			if err != nil {
				t.Fatalf("issueRefresh: %v", err)
			}
			narrowTokenClient(h, tc.allowed...)

			rec := httptest.NewRecorder()
			h.p.HandleToken(rec, tokenReq(refreshForm(presented)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			if got := decodeError(t, rec); got != errCodeInvalidGrant {
				t.Fatalf("want %s, got %s", errCodeInvalidGrant, got)
			}
			if _, ok := lookupRefresh(ctx, h.p.kv, presented); ok {
				t.Fatal("the family should be revoked")
			}
			if !sawAuditReason(h, "scope_not_allowed") {
				t.Fatal("expected a scope_not_allowed audit record")
			}
		})
	}
}

// TestTokenRefreshDeletedProviderStillNamed verifies that deleting a provider,
// which also takes its identity scope off the client, is still refused as a
// deleted provider rather than narrowed away.
func TestTokenRefreshDeletedProviderStillNamed(t *testing.T) {
	h := newTokenHarness(t)
	ctx := context.Background()
	h.p.queries.(*fakeTokenQueries).knownSlugs = []string{"steam"}
	narrowTokenClient(h, "openid", "profile", "offline_access")

	presented, _, err := issueRefresh(ctx, h.p.kv, identityFamily("corp"), RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(refreshForm(presented)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !sawAuditReason(h, "identity_provider_deleted") {
		t.Fatal("expected an identity_provider_deleted audit record")
	}
}

// TestTokenCodeExchangeLeavesOutScopesTakenOffClient covers a scope taken off
// the client between /authorize and the code exchange.
func TestTokenCodeExchangeLeavesOutScopesTakenOffClient(t *testing.T) {
	h := newTokenHarness(t)
	code := h.mintTestCode(t, baseAuthCode())
	narrowTokenClient(h, "openid", "profile")

	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(codeExchangeForm(code, testVerifier, testRedirect)))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Scope != "openid profile" {
		t.Fatalf("scope = %q, want openid profile", resp.Scope)
	}
	if resp.RefreshToken != "" {
		t.Fatal("no refresh token once offline_access is taken off the client")
	}
}

func TestTokenCodeExchangeRefusedWithoutOpenID(t *testing.T) {
	h := newTokenHarness(t)
	code := h.mintTestCode(t, baseAuthCode())
	narrowTokenClient(h, "profile", "offline_access")

	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(codeExchangeForm(code, testVerifier, testRedirect)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := decodeError(t, rec); got != errCodeInvalidGrant {
		t.Fatalf("want %s, got %s", errCodeInvalidGrant, got)
	}
	if !sawAuditReason(h, "scope_not_allowed") {
		t.Fatal("expected a scope_not_allowed audit record")
	}
}

// TestUserinfoLeavesOutScopesTakenOffClient covers an access token issued
// before the scope was taken off the client.
func TestUserinfoLeavesOutScopesTakenOffClient(t *testing.T) {
	h := newEndpointHarness(t)
	at := h.mintAccessToken(t, testSubject, testClientID, "openid profile email groups", "jti-narrow", time.Now().Add(time.Hour))
	narrowEndpointClient(h, "openid", "profile")

	rec := httptest.NewRecorder()
	h.p.HandleUserinfo(rec, userinfoReq(at))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, claim := range []string{"email", "email_verified", "groups"} {
		if _, ok := body[claim]; ok {
			t.Fatalf("userinfo carries %s after its scope was taken off the client: %v", claim, body[claim])
		}
	}
	if body["username"] != "alice" {
		t.Fatalf("userinfo lost the profile claims: %v", body)
	}
}

func TestIntrospectAccessTokenLeavesOutScopesTakenOffClient(t *testing.T) {
	h := newEndpointHarness(t)
	at := h.mintAccessToken(t, testSubject, testClientID, "openid profile email", "jti-i-narrow", time.Now().Add(time.Hour))
	narrowEndpointClient(h, "openid", "profile")

	rec := httptest.NewRecorder()
	h.p.HandleIntrospect(rec, introspectReq(at))
	body := decodeIntrospection(t, rec)
	if body["active"] != true || body["scope"] != "openid profile" {
		t.Fatalf("introspection = %v, want active with scope openid profile", body)
	}
}

func TestIntrospectRefreshTokenLeavesOutScopesTakenOffClient(t *testing.T) {
	h := newEndpointHarness(t)
	ctx := context.Background()
	rt, _, err := issueRefresh(ctx, h.p.kv, refreshFamily{
		ClientID:  testClientID,
		AccountID: 7,
		Scope:     []string{"openid", "email", "offline_access"},
	}, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	narrowEndpointClient(h, "openid", "offline_access")
	rec := httptest.NewRecorder()
	h.p.HandleIntrospect(rec, introspectReq(rt))
	body := decodeIntrospection(t, rec)
	if body["active"] != true || body["scope"] != "openid offline_access" {
		t.Fatalf("introspection = %v, want active with scope openid offline_access", body)
	}

	narrowEndpointClient(h, "openid", "email")
	rec = httptest.NewRecorder()
	h.p.HandleIntrospect(rec, introspectReq(rt))
	if body := decodeIntrospection(t, rec); body["active"] != false {
		t.Fatalf("introspection = %v, want inactive once offline_access is taken off the client", body)
	}
}
