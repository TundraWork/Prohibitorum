package oidc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// introspectReq builds a client_secret_basic POST /oauth/introspect carrying
// the given token. clientID/secret default to the harness client.
func introspectReq(token string) *http.Request {
	form := url.Values{}
	form.Set("token", token)
	req := httptest.NewRequest(http.MethodPost, "/oauth/introspect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(testClientID, testSecret)
	return req
}

func decodeIntrospection(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode introspection body %q: %v", rec.Body.String(), err)
	}
	return body
}

func TestIntrospectAccessTokenActive(t *testing.T) {
	h := newEndpointHarness(t)
	at := h.mintAccessToken(t, testSubject, testClientID, "openid profile", "jti-i1", time.Now().Add(time.Hour))

	rec := httptest.NewRecorder()
	h.p.HandleIntrospect(rec, introspectReq(at))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeIntrospection(t, rec)
	if body["active"] != true {
		t.Fatalf("active = %v, want true", body["active"])
	}
	if body["token_type"] != "access_token" {
		t.Fatalf("token_type = %v, want access_token", body["token_type"])
	}
	if body["client_id"] != testClientID {
		t.Fatalf("client_id = %v", body["client_id"])
	}
	if body["sub"] != testSubject {
		t.Fatalf("sub = %v", body["sub"])
	}
	if body["scope"] != "openid profile" {
		t.Fatalf("scope = %v", body["scope"])
	}
}

func TestIntrospectRefreshTokenActive(t *testing.T) {
	h := newEndpointHarness(t)
	ctx := context.Background()

	rt, _, err := issueRefresh(ctx, h.p.kv, refreshFamily{
		ClientID:  testClientID,
		AccountID: 7,
		Scope:     []string{"openid", "offline_access"},
	})
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	rec := httptest.NewRecorder()
	h.p.HandleIntrospect(rec, introspectReq(rt))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	body := decodeIntrospection(t, rec)
	if body["active"] != true {
		t.Fatalf("active = %v, want true", body["active"])
	}
	if body["token_type"] != "refresh_token" {
		t.Fatalf("token_type = %v, want refresh_token", body["token_type"])
	}
	if body["scope"] != "openid offline_access" {
		t.Fatalf("scope = %v", body["scope"])
	}
	if body["sub"] != testSubject {
		t.Fatalf("sub = %v, want %s", body["sub"], testSubject)
	}
}

func TestIntrospectExpiredAccessTokenInactive(t *testing.T) {
	h := newEndpointHarness(t)
	at := h.mintAccessToken(t, testSubject, testClientID, "openid", "jti-i2", time.Now().Add(-time.Minute))

	rec := httptest.NewRecorder()
	h.p.HandleIntrospect(rec, introspectReq(at))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if body := decodeIntrospection(t, rec); body["active"] != false {
		t.Fatalf("active = %v, want false", body["active"])
	}
}

func TestIntrospectGarbageInactive(t *testing.T) {
	h := newEndpointHarness(t)
	rec := httptest.NewRecorder()
	h.p.HandleIntrospect(rec, introspectReq("not-a-token"))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if body := decodeIntrospection(t, rec); body["active"] != false {
		t.Fatalf("active = %v, want false", body["active"])
	}
}

func TestIntrospectOtherClientsAccessTokenInactive(t *testing.T) {
	h := newEndpointHarness(t)
	// Register a second client and mint a token owned by it.
	h.q.clients["other"] = confidentialClient(t, "other", "othersecret", "client_secret_basic")
	at := h.mintAccessToken(t, testSubject, "other", "openid", "jti-i3", time.Now().Add(time.Hour))

	// The harness client (testClientID) introspects another client's token.
	rec := httptest.NewRecorder()
	h.p.HandleIntrospect(rec, introspectReq(at))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if body := decodeIntrospection(t, rec); body["active"] != false {
		t.Fatalf("a token owned by another client must be inactive; active = %v", body["active"])
	}
}

func TestIntrospectOtherClientsRefreshTokenInactive(t *testing.T) {
	h := newEndpointHarness(t)
	ctx := context.Background()
	rt, _, err := issueRefresh(ctx, h.p.kv, refreshFamily{
		ClientID:  "other",
		AccountID: 7,
		Scope:     []string{"openid"},
	})
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	rec := httptest.NewRecorder()
	h.p.HandleIntrospect(rec, introspectReq(rt))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if body := decodeIntrospection(t, rec); body["active"] != false {
		t.Fatalf("another client's refresh token must be inactive; active = %v", body["active"])
	}
}

func TestIntrospectBadClientAuth(t *testing.T) {
	h := newEndpointHarness(t)
	at := h.mintAccessToken(t, testSubject, testClientID, "openid", "jti-i4", time.Now().Add(time.Hour))

	form := url.Values{}
	form.Set("token", at)
	req := httptest.NewRequest(http.MethodPost, "/oauth/introspect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(testClientID, "wrong-secret")

	rec := httptest.NewRecorder()
	h.p.HandleIntrospect(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	if got := decodeError(t, rec); got != errCodeInvalidClient {
		t.Fatalf("error = %q, want %q", got, errCodeInvalidClient)
	}
}

// TestIntrospectPublicClientRejected verifies RFC 7662 §2.1: a public
// (none-auth) client may not introspect. Even though it authenticates as a
// known client (client_id in the form, no secret), the endpoint rejects it
// with invalid_client (401) before any token lookup.
func TestIntrospectPublicClientRejected(t *testing.T) {
	h := newEndpointHarness(t)
	h.q.clients["pub"] = publicClient("pub")
	// Mint a token owned by the public client so a missing public-client guard
	// would otherwise return active:true (proving the guard fires first).
	at := h.mintAccessToken(t, testSubject, "pub", "openid", "jti-i5", time.Now().Add(time.Hour))

	form := url.Values{}
	form.Set("token", at)
	form.Set("client_id", "pub")
	req := httptest.NewRequest(http.MethodPost, "/oauth/introspect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	h.p.HandleIntrospect(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := decodeError(t, rec); got != errCodeInvalidClient {
		t.Fatalf("error = %q, want %q", got, errCodeInvalidClient)
	}
}
