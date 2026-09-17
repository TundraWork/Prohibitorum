package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/appaccess"
	"prohibitorum/pkg/audit"
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
	groupsErr   error
}

func (f *fakeEndpointQueries) GetOIDCClient(_ context.Context, clientID string) (db.OidcClient, error) {
	c, ok := f.clients[clientID]
	if !ok {
		return db.OidcClient{}, pgx.ErrNoRows
	}
	return c, nil
}

func (f *fakeEndpointQueries) GetOIDCClientAny(_ context.Context, clientID string) (db.OidcClient, error) {
	c, ok := f.clients[clientID]
	if !ok {
		return db.OidcClient{}, pgx.ErrNoRows
	}
	return c, nil
}

func (f *fakeEndpointQueries) CountVerifiedAccountsByEmail(_ context.Context, email string) (int64, error) {
	var count int64
	for _, account := range f.byID {
		if account.EmailVerified && account.Email.Valid && strings.EqualFold(account.Email.String, email) {
			count++
		}
	}
	return count, nil
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
			testClientID: confidentialClient(t, testClientID, testSecret, "client_secret"),
		},
		byID:        map[int32]db.Account{7: acct},
		bySubject:   map[string]db.Account{testSubject: acct},
		revokedJTIs: map[string]bool{},
	}
	access := &fakeOIDCAuthorizer{evaluate: func(context.Context, int32, string) (appaccess.Decision, error) {
		if q.groupsErr != nil {
			return appaccess.Decision{}, q.groupsErr
		}
		return appaccess.Decision{Allowed: true, Source: appaccess.SourceOpen}, nil
	}}
	ra := &recordingAudit{}
	p := &Provider{
		cfg:     &configx.Config{OIDC: configx.OIDCConfig{Issuer: testIssuer}, PublicOrigins: []string{testIssuer}},
		queries: q,
		kv:      kv.NewMemoryStore(),
		deks:    oidcTestDEKs,
		audit:   ra,
		rl:      authn.NewRateLimiter(),
		keys:    newKeyCache(&fakeSigningKeyQueries{rows: []db.SigningKey{row}}, oidcTestDEKs),
		access:  access,
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

func (h *endpointHarness) mintCurrentAccessToken(t *testing.T, sub, clientID, scope, jti string, exp time.Time) string {
	t.Helper()
	tok, err := h.p.signJWT(context.Background(), map[string]any{
		"iss":                  testIssuer,
		"sub":                  sub,
		"aud":                  testIssuer,
		"client_id":            clientID,
		internalAccountIDClaim: 7,
		"exp":                  exp.Unix(),
		"iat":                  time.Now().Unix(),
		"jti":                  jti,
		"scope":                scope,
	}, "at+jwt")
	if err != nil {
		t.Fatalf("mint current access token: %v", err)
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

func TestUserinfoUsesCurrentProjectionWithoutExposingInternalAccountID(t *testing.T) {
	h := newEndpointHarness(t)
	client := h.q.clients[testClientID]
	client.PrincipalSource = PrincipalSourceUsername
	client.ClaimAliases = []byte(`{"login":"preferred_username"}`)
	h.q.clients[testClientID] = client
	at := h.mintCurrentAccessToken(t, testSubject, testClientID, "openid profile", "jti-current-projection", time.Now().Add(time.Hour))

	rec := httptest.NewRecorder()
	h.p.HandleUserinfo(rec, userinfoReq(at))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["sub"] != "alice" || body["login"] != "alice" {
		t.Fatalf("current projection = %#v", body)
	}
	if _, exposed := body[internalAccountIDClaim]; exposed {
		t.Fatalf("private account claim exposed by userinfo: %#v", body)
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

// TestUserinfoAppPolicyEvaluationFailure_NoInvalidTokenLeak proves that when the groups
// scope is requested but the DB group-load fails, the response does NOT
// claim invalid_token (the token is valid), does NOT leak operation prose
// like "could not load groups", and emits a protocol-conformant server_error
// with request-ID correlation.
func TestUserinfoAppPolicyEvaluationFailure_NoInvalidTokenLeak(t *testing.T) {
	h := newEndpointHarness(t)
	// Inject a failing live app-policy evaluation.
	h.q.groupsErr = errors.New("db: connection refused")

	at := h.mintAccessToken(t, testSubject, testClientID, "openid groups", "jti-grp-fail", time.Now().Add(time.Hour))
	rec := httptest.NewRecorder()
	h.p.HandleUserinfo(rec, userinfoReq(at))

	// The response must NOT be 401 (the token is valid) — it should be 500.
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("status = 401 — a valid token must not be reported as invalid_token\nbody: %s", rec.Body.String())
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500\nbody: %s", rec.Code, rec.Body.String())
	}

	// The body must NOT contain invalid_token or "could not load groups".
	body := rec.Body.String()
	if strings.Contains(body, "invalid_token") {
		t.Errorf("body leaked invalid_token for a valid token: %s", body)
	}
	if strings.Contains(body, "could not load groups") {
		t.Errorf("body leaked operation prose: %s", body)
	}

	// The body must contain a server_error code (RFC 6749 server_error is the
	// protocol-native way to signal an internal failure at /userinfo).
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp["error"] != "server_error" {
		t.Errorf("error = %q, want server_error", resp["error"])
	}
	// error_description must NOT contain the raw DB error.
	if strings.Contains(resp["error_description"], "connection refused") {
		t.Errorf("error_description leaked raw DB error: %s", resp["error_description"])
	}
}

func TestUserinfoProjectsCurrentClientDecisionGroups(t *testing.T) {
	h := newEndpointHarness(t)
	h.p.access = &fakeOIDCAuthorizer{byClient: map[string]appaccess.Decision{
		testClientID: {
			Allowed:      true,
			Source:       appaccess.SourceManualAllow,
			ManualGroups: []appaccess.GroupMatch{{Slug: "manual", Exposed: true}},
			MatchingRuleGroups: []appaccess.GroupMatch{
				{Slug: "zeta", Exposed: true, Matched: true},
				{Slug: "hidden", Matched: true},
				{Slug: "alpha", Exposed: true, Matched: true},
			},
		},
	}}
	at := h.mintAccessToken(t, testSubject, testClientID, "openid groups", "jti-app-groups", time.Now().Add(time.Hour))
	rec := httptest.NewRecorder()

	h.p.HandleUserinfo(rec, userinfoReq(at))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Groups []string `json:"groups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "manual", "zeta"}
	if strings.Join(body.Groups, ",") != strings.Join(want, ",") {
		t.Fatalf("groups = %v, want %v", body.Groups, want)
	}
}

func TestUserinfoAppAccessDenied(t *testing.T) {
	h := newEndpointHarness(t)
	h.p.access = &fakeOIDCAuthorizer{byClient: map[string]appaccess.Decision{
		testClientID: {Source: appaccess.SourceManualDeny},
	}}
	at := h.mintAccessToken(t, testSubject, testClientID, "openid", "jti-denied", time.Now().Add(time.Hour))
	rec := httptest.NewRecorder()

	h.p.HandleUserinfo(rec, userinfoReq(at))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
	if len(h.audit.records) != 1 || h.audit.records[0].Event != audit.EventAccessDenied ||
		h.audit.records[0].Detail["reason"] != "manual_deny" {
		t.Fatalf("audit = %#v, want manual_deny access denial", h.audit.records)
	}
}
