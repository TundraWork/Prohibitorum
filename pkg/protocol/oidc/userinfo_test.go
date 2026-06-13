package oidc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

// testSubject is the canonical UUID string of the harness account's
// oidc_subject. It must round-trip through pgtype.UUID.Scan.
const testSubject = "11223344-5566-7788-99aa-bbccddeeff00"

func testSubjectUUID(t *testing.T) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(testSubject); err != nil {
		t.Fatalf("scan test subject: %v", err)
	}
	return u
}

// fakeEndpointQueries backs the /userinfo, /introspect, and /revoke harness.
// It overrides client load, account-by-id, account-by-subject, and the jti
// denylist read/write. InsertRevokedJTI records its argument so revoke tests
// can assert the right jti was denylisted.
type fakeEndpointQueries struct {
	db.Querier
	clients     map[string]db.OidcClient
	byID        map[int32]db.Account
	bySubject   map[string]db.Account
	revokedJTIs map[string]bool
	inserted    []db.InsertRevokedJTIParams
}

func (f *fakeEndpointQueries) GetOIDCClient(_ context.Context, clientID string) (db.OidcClient, error) {
	c, ok := f.clients[clientID]
	if !ok {
		return db.OidcClient{}, pgx.ErrNoRows
	}
	return c, nil
}

func (f *fakeEndpointQueries) GetAccountByID(_ context.Context, id int32) (db.Account, error) {
	a, ok := f.byID[id]
	if !ok {
		return db.Account{}, pgx.ErrNoRows
	}
	return a, nil
}

func (f *fakeEndpointQueries) GetAccountByOIDCSubject(_ context.Context, sub pgtype.UUID) (db.Account, error) {
	a, ok := f.bySubject[sub.String()]
	if !ok {
		return db.Account{}, pgx.ErrNoRows
	}
	return a, nil
}

func (f *fakeEndpointQueries) IsJTIRevoked(_ context.Context, jti string) (bool, error) {
	return f.revokedJTIs[jti], nil
}

func (f *fakeEndpointQueries) InsertRevokedJTI(_ context.Context, arg db.InsertRevokedJTIParams) error {
	f.inserted = append(f.inserted, arg)
	if f.revokedJTIs == nil {
		f.revokedJTIs = map[string]bool{}
	}
	f.revokedJTIs[arg.Jti] = true
	return nil
}

// endpointHarness wires a Provider with a working signing key, the fake query
// layer above (registering testClientID + an account at id 7 / testSubject), an
// in-memory KV, a recording audit writer, and a rate limiter.
type endpointHarness struct {
	p     *Provider
	q     *fakeEndpointQueries
	audit *recordingAudit
}

func newEndpointHarness(t *testing.T) *endpointHarness {
	t.Helper()
	row, _ := testSigningKeyRow(t)

	acct := db.Account{
		ID:          7,
		Username:    "alice",
		DisplayName: "Alice",
		Role:        "user",
		OidcSubject: testSubjectUUID(t),
	}
	q := &fakeEndpointQueries{
		clients: map[string]db.OidcClient{
			testClientID: confidentialClient(t, testClientID, testSecret, "client_secret_basic"),
		},
		byID:        map[int32]db.Account{7: acct},
		bySubject:   map[string]db.Account{testSubject: acct},
		revokedJTIs: map[string]bool{},
	}
	ra := &recordingAudit{}
	p := &Provider{
		cfg:     &configx.Config{OIDC: configx.OIDCConfig{Issuer: testIssuer}, PublicOrigins: []string{testIssuer}},
		queries: q,
		kv:      kv.NewMemoryStore(),
		audit:   ra,
		rl:      authn.NewRateLimiter(),
		keys:    newKeyCache(&fakeSigningKeyQueries{rows: []db.SigningKey{row}}, oidcTestDEKs),
	}
	return &endpointHarness{p: p, q: q, audit: ra}
}

// mintAccessToken signs a valid access token (typ at+jwt) for the given subject
// / client / scope with the provided jti and expiry.
func (h *endpointHarness) mintAccessToken(t *testing.T, sub, clientID, scope, jti string, exp time.Time) string {
	t.Helper()
	tok, err := h.p.signJWT(context.Background(), map[string]any{
		"iss":       testIssuer,
		"sub":       sub,
		"aud":       testIssuer,
		"client_id": clientID,
		"exp":       exp.Unix(),
		"iat":       time.Now().Unix(),
		"jti":       jti,
		"scope":     scope,
	}, "at+jwt")
	if err != nil {
		t.Fatalf("mint access token: %v", err)
	}
	return tok
}

// mintIDToken signs a token with the ID-token typ (JWT) — used to prove the
// access-token endpoints reject a non-access typ.
func (h *endpointHarness) mintIDToken(t *testing.T, sub string) string {
	t.Helper()
	tok, err := h.p.signJWT(context.Background(), map[string]any{
		"iss": testIssuer,
		"sub": sub,
		"aud": testClientID,
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}, "JWT")
	if err != nil {
		t.Fatalf("mint id token: %v", err)
	}
	return tok
}

func userinfoReq(token string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/oauth/userinfo", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func TestUserinfoValid(t *testing.T) {
	h := newEndpointHarness(t)
	at := h.mintAccessToken(t, testSubject, testClientID, "openid profile", "jti-1", time.Now().Add(time.Hour))

	rec := httptest.NewRecorder()
	h.p.HandleUserinfo(rec, userinfoReq(at))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["sub"] != testSubject {
		t.Fatalf("sub = %v, want %s", body["sub"], testSubject)
	}
	if body["username"] != "alice" {
		t.Fatalf("expected profile claims (username), got %v", body["username"])
	}
}

func TestUserinfoNoProfileScope(t *testing.T) {
	h := newEndpointHarness(t)
	at := h.mintAccessToken(t, testSubject, testClientID, "openid", "jti-2", time.Now().Add(time.Hour))

	rec := httptest.NewRecorder()
	h.p.HandleUserinfo(rec, userinfoReq(at))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["sub"] != testSubject {
		t.Fatalf("sub = %v", body["sub"])
	}
	if _, ok := body["username"]; ok {
		t.Fatal("username must be omitted without profile scope")
	}
}

func TestUserinfoMissingBearer(t *testing.T) {
	h := newEndpointHarness(t)
	rec := httptest.NewRecorder()
	h.p.HandleUserinfo(rec, userinfoReq(""))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	if wa := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(wa, "Bearer") {
		t.Fatalf("WWW-Authenticate = %q, want Bearer challenge", wa)
	}
	if got := decodeError(t, rec); got != errCodeInvalidToken {
		t.Fatalf("error = %q, want %q", got, errCodeInvalidToken)
	}
}

func TestUserinfoGarbageBearer(t *testing.T) {
	h := newEndpointHarness(t)
	rec := httptest.NewRecorder()
	h.p.HandleUserinfo(rec, userinfoReq("not-a-jwt"))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	if wa := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(wa, "Bearer") {
		t.Fatalf("WWW-Authenticate = %q", wa)
	}
}

func TestUserinfoRejectsIDToken(t *testing.T) {
	h := newEndpointHarness(t)
	idTok := h.mintIDToken(t, testSubject)

	rec := httptest.NewRecorder()
	h.p.HandleUserinfo(rec, userinfoReq(idTok))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("an ID token must be rejected at /userinfo; got %d", rec.Code)
	}
}

func TestUserinfoExpiredToken(t *testing.T) {
	h := newEndpointHarness(t)
	at := h.mintAccessToken(t, testSubject, testClientID, "openid", "jti-3", time.Now().Add(-time.Minute))

	rec := httptest.NewRecorder()
	h.p.HandleUserinfo(rec, userinfoReq(at))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 for expired token, got %d", rec.Code)
	}
}

func TestUserinfoRevokedJTI(t *testing.T) {
	h := newEndpointHarness(t)
	at := h.mintAccessToken(t, testSubject, testClientID, "openid", "jti-revoked", time.Now().Add(time.Hour))
	h.q.revokedJTIs["jti-revoked"] = true

	rec := httptest.NewRecorder()
	h.p.HandleUserinfo(rec, userinfoReq(at))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 for revoked jti, got %d", rec.Code)
	}
}

func TestUserinfoDisabledAccount(t *testing.T) {
	h := newEndpointHarness(t)
	disabled := h.q.bySubject[testSubject]
	disabled.Disabled = true
	h.q.bySubject[testSubject] = disabled

	at := h.mintAccessToken(t, testSubject, testClientID, "openid", "jti-4", time.Now().Add(time.Hour))
	rec := httptest.NewRecorder()
	h.p.HandleUserinfo(rec, userinfoReq(at))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 for disabled account, got %d", rec.Code)
	}
}

// TestUserinfoEmptyPublicOriginsNoPanic asserts that a provider configured with
// PublicOrigins: nil does not panic at /userinfo and falls back to the OIDC
// issuer as the avatar origin. The response must be 200 OK.
func TestUserinfoEmptyPublicOriginsNoPanic(t *testing.T) {
	h := newEndpointHarness(t)
	// Override to nil PublicOrigins — the issuer remains set.
	h.p.cfg = &configx.Config{
		OIDC:          configx.OIDCConfig{Issuer: testIssuer},
		PublicOrigins: nil,
	}

	at := h.mintAccessToken(t, testSubject, testClientID, "openid profile", "jti-nopub", time.Now().Add(time.Hour))
	rec := httptest.NewRecorder()
	h.p.HandleUserinfo(rec, userinfoReq(at))

	if rec.Code != http.StatusOK {
		t.Fatalf("empty PublicOrigins must not panic or error; got %d (%s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode userinfo response: %v", err)
	}
	if body["sub"] != testSubject {
		t.Fatalf("sub = %v, want %s", body["sub"], testSubject)
	}
}
