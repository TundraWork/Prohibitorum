package oidc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
)

func TestErrorsWriteOIDCError(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", nil)
	writeOIDCError(rec, req, http.StatusBadRequest, errCodeInvalidGrant, "code expired")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != errCodeInvalidGrant {
		t.Fatalf("error = %q, want %q", body["error"], errCodeInvalidGrant)
	}
	if body["error_description"] != "code expired" {
		t.Fatalf("error_description = %q, want %q", body["error_description"], "code expired")
	}
}

func TestErrorsRedirectError(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
	redirectError(rec, req, "https://rp.example/cb", errCodeAccessDenied, "user said no", "xyz123", "https://idp.example")

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location %q: %v", loc, err)
	}
	if u.Scheme != "https" || u.Host != "rp.example" || u.Path != "/cb" {
		t.Fatalf("Location target = %q, want https://rp.example/cb", loc)
	}
	q := u.Query()
	if q.Get("error") != errCodeAccessDenied {
		t.Fatalf("error param = %q, want %q", q.Get("error"), errCodeAccessDenied)
	}
	if q.Get("error_description") != "user said no" {
		t.Fatalf("error_description param = %q, want %q", q.Get("error_description"), "user said no")
	}
	if q.Get("state") != "xyz123" {
		t.Fatalf("state param = %q, want %q", q.Get("state"), "xyz123")
	}
	if q.Get("iss") != "https://idp.example" {
		t.Fatalf("iss param = %q, want %q (RFC 9207)", q.Get("iss"), "https://idp.example")
	}
}

func TestErrorsRedirectErrorOmitsEmptyState(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
	redirectError(rec, req, "https://rp.example/cb", errCodeInvalidRequest, "", "", "")

	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location %q: %v", loc, err)
	}
	q := u.Query()
	if _, ok := q["state"]; ok {
		t.Fatalf("state should be omitted when empty, got Location %q", loc)
	}
	if _, ok := q["error_description"]; ok {
		t.Fatalf("error_description should be omitted when empty, got Location %q", loc)
	}
	if q.Get("error") != errCodeInvalidRequest {
		t.Fatalf("error param = %q, want %q", q.Get("error"), errCodeInvalidRequest)
	}
}

func TestErrorsRedirectErrorBadURI(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
	// A control character makes url.Parse fail, exercising the fallback path.
	redirectError(rec, req, "http://\x7f", errCodeInvalidRequest, "bad", "s", "https://idp.example")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (JSON fallback)", rec.Code, http.StatusBadRequest)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != errCodeInvalidRequest {
		t.Fatalf("error = %q, want %q", body["error"], errCodeInvalidRequest)
	}
}

func TestDiscoverySurface(t *testing.T) {
	cfg := &configx.Config{}
	cfg.OIDC.Issuer = "https://idp.example"
	p := &Provider{cfg: cfg}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil)
	p.HandleDiscovery(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode doc: %v", err)
	}

	if doc["introspection_endpoint"] != "https://idp.example/oauth/introspect" {
		t.Fatalf("introspection_endpoint = %v", doc["introspection_endpoint"])
	}
	if doc["revocation_endpoint"] != "https://idp.example/oauth/revoke" {
		t.Fatalf("revocation_endpoint = %v", doc["revocation_endpoint"])
	}
	if doc["authorization_response_iss_parameter_supported"] != true {
		t.Fatalf("authorization_response_iss_parameter_supported = %v, want true", doc["authorization_response_iss_parameter_supported"])
	}

	scopes := toStringSet(t, doc["scopes_supported"])
	if !scopes["offline_access"] {
		t.Fatalf("scopes_supported missing offline_access: %v", doc["scopes_supported"])
	}
	if !scopes["openid"] {
		t.Fatalf("scopes_supported missing openid: %v", doc["scopes_supported"])
	}

	claims := toStringSet(t, doc["claims_supported"])
	if !claims["sid"] {
		t.Fatalf("claims_supported missing sid: %v", doc["claims_supported"])
	}
	if !claims["at_hash"] {
		t.Fatalf("claims_supported missing at_hash: %v", doc["claims_supported"])
	}
}

// toStringSet converts a decoded JSON array (of strings) into a set.
func toStringSet(t *testing.T, v any) map[string]bool {
	t.Helper()
	arr, ok := v.([]any)
	if !ok {
		t.Fatalf("value %v is not a JSON array (%T)", v, v)
	}
	set := make(map[string]bool, len(arr))
	for _, e := range arr {
		s, ok := e.(string)
		if !ok {
			t.Fatalf("array element %v is not a string", e)
		}
		set[s] = true
	}
	return set
}

func TestDiscoveryMissingIssuer(t *testing.T) {
	p := &Provider{cfg: &configx.Config{}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil)
	p.HandleDiscovery(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 when issuer unset", rec.Code)
	}
}

func TestJWKSServesActiveKey(t *testing.T) {
	row, _ := testSigningKeyRow(t)
	fake := &fakeSigningKeyQueries{rows: []db.SigningKey{row}}
	p := &Provider{keys: newKeyCache(fake, oidcTestDEKs)}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/jwks", nil)
	p.HandleJWKS(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var set struct {
		Keys []map[string]any `json:"keys"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &set); err != nil {
		t.Fatalf("decode jwks: %v", err)
	}
	if len(set.Keys) != 1 {
		t.Fatalf("jwks: got %d keys, want 1", len(set.Keys))
	}
	if set.Keys[0]["kty"] != "RSA" {
		t.Fatalf("jwks key kty = %v, want RSA", set.Keys[0]["kty"])
	}
}
