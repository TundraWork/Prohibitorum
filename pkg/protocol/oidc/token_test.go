package oidc

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

// fakeTokenQueries embeds db.Querier (so unimplemented methods panic) and
// overrides the two the token endpoint touches: GetOIDCClient (for client
// authentication) and GetAccountByID (for the subject account).
type fakeTokenQueries struct {
	db.Querier
	clients  map[string]db.OidcClient
	accounts map[int32]db.Account
}

func (f *fakeTokenQueries) GetOIDCClient(_ context.Context, clientID string) (db.OidcClient, error) {
	c, ok := f.clients[clientID]
	if !ok {
		return db.OidcClient{}, pgx.ErrNoRows
	}
	return c, nil
}

func (f *fakeTokenQueries) GetAccountByID(_ context.Context, id int32) (db.Account, error) {
	a, ok := f.accounts[id]
	if !ok {
		return db.Account{}, pgx.ErrNoRows
	}
	return a, nil
}

// tokenHarness wires a Provider with a working signing key, a fake query layer
// registering the named client + account, an in-memory KV, a recording audit
// writer, and a rate limiter.
type tokenHarness struct {
	p     *Provider
	audit *recordingAudit
	row   db.SigningKey
}

const (
	testClientID = "cid"
	testSecret   = "secret"
	testRedirect = "https://rp.example.com/cb"
	testVerifier = "a-very-long-pkce-code-verifier-1234567890abcdef"
)

func testChallenge() string {
	sum := sha256.Sum256([]byte(testVerifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func newTokenHarness(t *testing.T) *tokenHarness {
	t.Helper()
	row, _ := testSigningKeyRow(t)

	acct := db.Account{
		ID:          42,
		Username:    "alice",
		DisplayName: "Alice",
		Role:        "user",
		OidcSubject: pgtype.UUID{Bytes: [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, Valid: true},
	}
	q := &fakeTokenQueries{
		clients: map[string]db.OidcClient{
			testClientID: confidentialClient(t, testClientID, testSecret, "client_secret_basic"),
		},
		accounts: map[int32]db.Account{42: acct},
	}
	ra := &recordingAudit{}
	p := &Provider{
		cfg:     &configx.Config{OIDC: configx.OIDCConfig{Issuer: testIssuer}},
		queries: q,
		kv:      kv.NewMemoryStore(),
		audit:   ra,
		rl:      authn.NewRateLimiter(),
		keys:    newKeyCache(&fakeSigningKeyQueries{rows: []db.SigningKey{row}}),
	}
	return &tokenHarness{p: p, audit: ra, row: row}
}

// mintTestCode stores an authCode and returns the issued code.
func (h *tokenHarness) mintTestCode(t *testing.T, ac authCode) string {
	t.Helper()
	code, err := mintCode(context.Background(), h.p.kv, ac)
	if err != nil {
		t.Fatalf("mintCode: %v", err)
	}
	return code
}

// baseAuthCode is a fully-valid authorization-code state bound to testClientID.
func baseAuthCode() authCode {
	return authCode{
		ClientID:            testClientID,
		AccountID:           42,
		SessionID:           "sid-1",
		RedirectURI:         testRedirect,
		Scope:               []string{"openid", "profile", "offline_access"},
		Nonce:               "n-123",
		CodeChallenge:       testChallenge(),
		CodeChallengeMethod: "S256",
		AuthTime:            time.Now().Add(-time.Minute),
		AMR:                 []string{"webauthn"},
		ACR:                 "urn:acr:1",
	}
}

// tokenReq builds a POST /oauth/token request with the given form values and
// client_secret_basic auth for testClientID/testSecret.
func tokenReq(form url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(testClientID, testSecret)
	return req
}

// codeExchangeForm builds the authorization_code grant form for a given code.
func codeExchangeForm(code, verifier, redirect string) url.Values {
	v := url.Values{}
	v.Set("grant_type", "authorization_code")
	v.Set("code", code)
	v.Set("redirect_uri", redirect)
	v.Set("code_verifier", verifier)
	return v
}

// decodeError extracts the OAuth error code from an error-response recorder.
func decodeError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body %q: %v", rec.Body.String(), err)
	}
	return body["error"]
}

func TestTokenHappyPath(t *testing.T) {
	h := newTokenHarness(t)
	ctx := context.Background()

	code := h.mintTestCode(t, baseAuthCode())
	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(codeExchangeForm(code, testVerifier, testRedirect)))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body %s)", rec.Code, rec.Body.String())
	}
	if cc := rec.Result().Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}

	var resp tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if resp.TokenType != "Bearer" {
		t.Fatalf("token_type = %q, want Bearer", resp.TokenType)
	}
	if resp.ExpiresIn != int(AccessTokenTTL.Seconds()) {
		t.Fatalf("expires_in = %d, want %d", resp.ExpiresIn, int(AccessTokenTTL.Seconds()))
	}
	if resp.Scope != "openid profile offline_access" {
		t.Fatalf("scope = %q", resp.Scope)
	}
	if resp.RefreshToken == "" {
		t.Fatal("expected a refresh token (offline_access granted)")
	}

	// ID token verifies and carries the expected claims.
	idClaims, err := h.p.verifyJWT(ctx, resp.IDToken)
	if err != nil {
		t.Fatalf("verify id token: %v", err)
	}
	if idClaims["sub"] == "" || idClaims["sub"] == nil {
		t.Fatal("id token missing sub")
	}
	if idClaims["aud"] != testClientID {
		t.Fatalf("id token aud = %v, want %s", idClaims["aud"], testClientID)
	}
	if idClaims["nonce"] != "n-123" {
		t.Fatalf("id token nonce = %v", idClaims["nonce"])
	}
	if idClaims["sid"] != "sid-1" {
		t.Fatalf("id token sid = %v", idClaims["sid"])
	}
	if got := idClaims["at_hash"]; got != atHash(resp.AccessToken) {
		t.Fatalf("at_hash = %v, want %v", got, atHash(resp.AccessToken))
	}

	// Access token verifies and carries jti + client_id.
	atClaims, err := h.p.verifyJWT(ctx, resp.AccessToken)
	if err != nil {
		t.Fatalf("verify access token: %v", err)
	}
	if atClaims["client_id"] != testClientID {
		t.Fatalf("access token client_id = %v", atClaims["client_id"])
	}
	if jti, _ := atClaims["jti"].(string); jti == "" {
		t.Fatal("access token missing jti")
	}
	if atClaims["aud"] != testIssuer {
		t.Fatalf("access token aud = %v, want %s", atClaims["aud"], testIssuer)
	}

	// The access token's JOSE typ header must be at+jwt (RFC 9068).
	parsed, err := jwt.ParseSigned(resp.AccessToken, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		t.Fatalf("parse access token header: %v", err)
	}
	if typ, _ := parsed.Headers[0].ExtraHeaders[jose.HeaderType].(string); typ != "at+jwt" {
		t.Fatalf("access token typ = %q, want at+jwt", typ)
	}

	// A token_issued audit record must be emitted.
	var sawIssued bool
	for _, r := range h.audit.records {
		if r.Factor == audit.FactorOIDCClient && r.Detail["reason"] == "token_issued" {
			sawIssued = true
			if r.AccountID == nil || *r.AccountID != 42 {
				t.Fatalf("token_issued AccountID = %v, want 42", r.AccountID)
			}
		}
	}
	if !sawIssued {
		t.Fatal("expected a token_issued audit record")
	}
}

func TestTokenPKCEMismatch(t *testing.T) {
	h := newTokenHarness(t)
	code := h.mintTestCode(t, baseAuthCode())

	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(codeExchangeForm(code, "wrong-verifier", testRedirect)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	if got := decodeError(t, rec); got != errCodeInvalidGrant {
		t.Fatalf("want %s, got %s", errCodeInvalidGrant, got)
	}
}

func TestTokenReplayRevokesFamily(t *testing.T) {
	h := newTokenHarness(t)
	ctx := context.Background()
	code := h.mintTestCode(t, baseAuthCode())

	// First exchange succeeds.
	rec1 := httptest.NewRecorder()
	h.p.HandleToken(rec1, tokenReq(codeExchangeForm(code, testVerifier, testRedirect)))
	if rec1.Code != http.StatusOK {
		t.Fatalf("first exchange want 200, got %d (%s)", rec1.Code, rec1.Body.String())
	}
	var resp tokenResponse
	if err := json.Unmarshal(rec1.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RefreshToken == "" {
		t.Fatal("expected refresh token on first exchange")
	}
	// The minted refresh family resolves before replay.
	if _, ok := lookupRefresh(ctx, h.p.kv, resp.RefreshToken); !ok {
		t.Fatal("refresh family should resolve before replay")
	}

	// Replay the same code.
	rec2 := httptest.NewRecorder()
	h.p.HandleToken(rec2, tokenReq(codeExchangeForm(code, testVerifier, testRedirect)))
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("replay want 400, got %d", rec2.Code)
	}
	if got := decodeError(t, rec2); got != errCodeInvalidGrant {
		t.Fatalf("replay want %s, got %s", errCodeInvalidGrant, got)
	}

	// The refresh family minted by the original exchange must now be revoked.
	if _, ok := lookupRefresh(ctx, h.p.kv, resp.RefreshToken); ok {
		t.Fatal("refresh family should be revoked after code replay")
	}

	// A code_replay audit record must be emitted.
	var sawReplay bool
	for _, r := range h.audit.records {
		if r.Detail["reason"] == "code_replay" {
			sawReplay = true
		}
	}
	if !sawReplay {
		t.Fatal("expected a code_replay audit record")
	}
}

func TestTokenWrongClient(t *testing.T) {
	h := newTokenHarness(t)
	// Code minted for a different client than the authenticated one.
	ac := baseAuthCode()
	ac.ClientID = "other"
	code := h.mintTestCode(t, ac)

	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(codeExchangeForm(code, testVerifier, testRedirect)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	if got := decodeError(t, rec); got != errCodeInvalidGrant {
		t.Fatalf("want %s, got %s", errCodeInvalidGrant, got)
	}
}

func TestTokenRedirectURIMismatch(t *testing.T) {
	h := newTokenHarness(t)
	code := h.mintTestCode(t, baseAuthCode())

	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(codeExchangeForm(code, testVerifier, "https://rp.example.com/other")))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	if got := decodeError(t, rec); got != errCodeInvalidGrant {
		t.Fatalf("want %s, got %s", errCodeInvalidGrant, got)
	}
}

func TestTokenNoOfflineAccessNoRefresh(t *testing.T) {
	h := newTokenHarness(t)
	ac := baseAuthCode()
	ac.Scope = []string{"openid", "profile"} // no offline_access
	code := h.mintTestCode(t, ac)

	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(codeExchangeForm(code, testVerifier, testRedirect)))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RefreshToken != "" {
		t.Fatalf("expected no refresh token, got %q", resp.RefreshToken)
	}

	// Replay of a code that minted NO family → invalid_grant, no family marker.
	rec2 := httptest.NewRecorder()
	h.p.HandleToken(rec2, tokenReq(codeExchangeForm(code, testVerifier, testRedirect)))
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("replay want 400, got %d", rec2.Code)
	}
	if got := decodeError(t, rec2); got != errCodeInvalidGrant {
		t.Fatalf("replay want %s, got %s", errCodeInvalidGrant, got)
	}
	if _, ok := usedFamily(context.Background(), h.p.kv, code); ok {
		t.Fatal("a no-offline_access code must not record a used family")
	}
}

func TestTokenDisabledAccount(t *testing.T) {
	h := newTokenHarness(t)
	// Replace the account with a disabled one.
	h.p.queries.(*fakeTokenQueries).accounts[42] = db.Account{ID: 42, Disabled: true,
		OidcSubject: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}}
	code := h.mintTestCode(t, baseAuthCode())

	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(codeExchangeForm(code, testVerifier, testRedirect)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	if got := decodeError(t, rec); got != errCodeInvalidGrant {
		t.Fatalf("want %s, got %s", errCodeInvalidGrant, got)
	}
}

func TestTokenUnsupportedGrantType(t *testing.T) {
	h := newTokenHarness(t)
	v := url.Values{}
	v.Set("grant_type", "password")
	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(v))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	if got := decodeError(t, rec); got != errCodeUnsupportedGrantType {
		t.Fatalf("want %s, got %s", errCodeUnsupportedGrantType, got)
	}
}

func TestTokenBadClientSecret(t *testing.T) {
	h := newTokenHarness(t)
	code := h.mintTestCode(t, baseAuthCode())

	req := httptest.NewRequest(http.MethodPost, "/oauth/token",
		strings.NewReader(codeExchangeForm(code, testVerifier, testRedirect).Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(testClientID, "wrong-secret")

	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	if got := decodeError(t, rec); got != errCodeInvalidClient {
		t.Fatalf("want %s, got %s", errCodeInvalidClient, got)
	}
}
