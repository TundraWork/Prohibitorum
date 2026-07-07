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
	// revokedSessions mirrors the real GetSession's `revoked_at IS NULL` filter:
	// an id present here resolves to pgx.ErrNoRows (revoked/absent). Empty by
	// default so every session reads as live and existing tests are unaffected.
	revokedSessions map[string]bool
	// deniedClients holds client IDs the bound account is NOT authorized for, used
	// to exercise the RBAC re-check at refresh. Empty by default → authorized, so
	// existing refresh tests keep passing. authzErr forces a predicate error.
	deniedClients map[string]bool
	authzErr      error
}

// IsAccountAuthorizedForOIDCClient backs the RBAC re-check on the refresh grant.
// Default (no deniedClients, no authzErr) → authorized=true so existing tests
// are unaffected.
func (f *fakeTokenQueries) IsAccountAuthorizedForOIDCClient(_ context.Context, arg db.IsAccountAuthorizedForOIDCClientParams) (pgtype.Bool, error) {
	if f.authzErr != nil {
		return pgtype.Bool{}, f.authzErr
	}
	return pgtype.Bool{Bool: !f.deniedClients[arg.ClientID], Valid: true}, nil
}

// GetSession mirrors the real query: returns the row only when not revoked.
func (f *fakeTokenQueries) GetSession(_ context.Context, id string) (db.Session, error) {
	if f.revokedSessions[id] {
		return db.Session{}, pgx.ErrNoRows
	}
	return db.Session{ID: id, AccountID: 42}, nil
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
		cfg:     &configx.Config{OIDC: configx.OIDCConfig{Issuer: testIssuer}, PublicOrigins: []string{testIssuer}},
		queries: q,
		kv:      kv.NewMemoryStore(),
		audit:   ra,
		rl:      authn.NewRateLimiter(),
		keys:    newKeyCache(&fakeSigningKeyQueries{rows: []db.SigningKey{row}}, oidcTestDEKs),
	}
	return &tokenHarness{p: p, audit: ra, row: row}
}

// mintTestCode stores an authCode and returns the issued code.
func (h *tokenHarness) mintTestCode(t *testing.T, ac authCode) string {
	t.Helper()
	code, err := mintCode(context.Background(), h.p.kv, ac, AuthorizationCodeTTL)
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

// TestTokenRejectsRevokedSession: if the originating session is revoked (user
// logged out, admin revoked it) within the short code window, the still-single-
// use code must NOT exchange — the resulting tokens would reference a dead
// session (audit OIDC-3).
func TestTokenRejectsRevokedSession(t *testing.T) {
	h := newTokenHarness(t)
	h.p.queries.(*fakeTokenQueries).revokedSessions = map[string]bool{"sid-revoked": true}

	ac := baseAuthCode()
	ac.SessionID = "sid-revoked"
	code := h.mintTestCode(t, ac)

	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(codeExchangeForm(code, testVerifier, testRedirect)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body %s)", rec.Code, rec.Body.String())
	}
	if got := decodeError(t, rec); got != errCodeInvalidGrant {
		t.Fatalf("error = %q, want %q", got, errCodeInvalidGrant)
	}
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
	idClaims, _, err := h.p.verifyJWT(ctx, resp.IDToken)
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
	atClaims, _, err := h.p.verifyJWT(ctx, resp.AccessToken)
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

// TestTokenPlainPKCERejected verifies that a stored code carrying the unsupported
// 'plain' method is rejected at exchange with invalid_grant rather than being
// silently mis-verified by the S256-only verifyPKCE.
func TestTokenPlainPKCERejected(t *testing.T) {
	h := newTokenHarness(t)
	ac := baseAuthCode()
	// A 'plain' challenge is the verifier verbatim (RFC 7636 §4.6).
	ac.CodeChallenge = testVerifier
	ac.CodeChallengeMethod = "plain"
	code := h.mintTestCode(t, ac)

	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(codeExchangeForm(code, testVerifier, testRedirect)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := decodeError(t, rec); got != errCodeInvalidGrant {
		t.Fatalf("want %s, got %s", errCodeInvalidGrant, got)
	}
}

// TestTokenOmittedMethodPKCERejected verifies the defense-in-depth guard also
// catches a stored code whose method was OMITTED (""). Per RFC 7636 §4.3 an
// omitted method means 'plain', so a challenge-present code with an empty method
// must be rejected with invalid_grant rather than mis-verified by the S256-only
// verifyPKCE. (/authorize rejects this at mint time; this is forged-code defense.)
func TestTokenOmittedMethodPKCERejected(t *testing.T) {
	h := newTokenHarness(t)
	ac := baseAuthCode()
	// A 'plain' (omitted-method) challenge is the verifier verbatim.
	ac.CodeChallenge = testVerifier
	ac.CodeChallengeMethod = ""
	code := h.mintTestCode(t, ac)

	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(codeExchangeForm(code, testVerifier, testRedirect)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := decodeError(t, rec); got != errCodeInvalidGrant {
		t.Fatalf("want %s, got %s", errCodeInvalidGrant, got)
	}
}

// TestTokenNoPKCEExchangeSucceeds verifies that a no-PKCE code (no stored
// challenge, empty method — a require_pkce=false client that legitimately sent
// no PKCE) exchanges SUCCESSFULLY. The verifyPKCE call is gated on a present
// challenge, so a missing challenge skips PKCE verification entirely rather than
// failing with "PKCE verification failed". This only affects require_pkce=false
// clients: a require_pkce=true client always has a stored challenge (authorize
// rejects an empty code_challenge at mint time), so PKCE is never bypassed for
// clients that require it.
func TestTokenNoPKCEExchangeSucceeds(t *testing.T) {
	h := newTokenHarness(t)
	ac := baseAuthCode()
	ac.CodeChallenge = ""
	ac.CodeChallengeMethod = ""
	code := h.mintTestCode(t, ac)

	rec := httptest.NewRecorder()
	form := codeExchangeForm(code, "", testRedirect)
	form.Del("code_verifier")
	h.p.HandleToken(rec, tokenReq(form))

	if rec.Code != http.StatusOK {
		t.Fatalf("no-PKCE code must exchange successfully; got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if resp.AccessToken == "" || resp.IDToken == "" {
		t.Fatal("no-PKCE exchange returned empty access/id token")
	}
}

// TestTokenWithChallengeStillRequiresVerifier verifies the converse: a code that
// DOES carry a stored challenge still requires a matching verifier. Omitting the
// verifier (or sending a wrong one) must fail with invalid_grant — the gating
// change must not let a challenge-bearing code through without proof.
func TestTokenWithChallengeStillRequiresVerifier(t *testing.T) {
	h := newTokenHarness(t)
	code := h.mintTestCode(t, baseAuthCode()) // baseAuthCode carries an S256 challenge

	rec := httptest.NewRecorder()
	form := codeExchangeForm(code, "", testRedirect)
	form.Del("code_verifier") // no verifier supplied
	h.p.HandleToken(rec, tokenReq(form))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("challenge-bearing code with no verifier must be rejected; got %d (%s)", rec.Code, rec.Body.String())
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

// TestTokenClientAuthFailAudited verifies that a client-authentication failure
// on the token endpoint emits an oidc_client|fail record with reason
// "client_auth_failed".
func TestTokenClientAuthFailAudited(t *testing.T) {
	h := newTokenHarness(t)
	code := h.mintTestCode(t, baseAuthCode())

	req := httptest.NewRequest(http.MethodPost, "/oauth/token",
		strings.NewReader(codeExchangeForm(code, testVerifier, testRedirect).Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(testClientID, "wrong-secret") // bad credentials

	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}

	var sawFail bool
	for _, r := range h.audit.records {
		if r.Factor == audit.FactorOIDCClient && r.Event == audit.EventFail &&
			r.Detail["reason"] == "client_auth_failed" {
			sawFail = true
		}
	}
	if !sawFail {
		t.Fatal("expected a client_auth_failed audit record")
	}
}

// TestTokenPKCEFailAudited verifies that a PKCE verification failure on the
// token endpoint emits an oidc_client|fail record with reason "pkce_failed"
// and carries the code's AccountID for attribution.
func TestTokenPKCEFailAudited(t *testing.T) {
	h := newTokenHarness(t)
	code := h.mintTestCode(t, baseAuthCode())

	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(codeExchangeForm(code, "wrong-verifier", testRedirect)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}

	var sawFail bool
	for _, r := range h.audit.records {
		if r.Factor == audit.FactorOIDCClient && r.Event == audit.EventFail &&
			r.Detail["reason"] == "pkce_failed" {
			sawFail = true
			if r.AccountID == nil || *r.AccountID != 42 {
				t.Fatalf("pkce_failed AccountID = %v, want 42", r.AccountID)
			}
			if r.Detail["client_id"] != testClientID {
				t.Fatalf("pkce_failed client_id = %v, want %s", r.Detail["client_id"], testClientID)
			}
		}
	}
	if !sawFail {
		t.Fatal("expected a pkce_failed audit record")
	}
}

// TestTokenEmptyPublicOriginsNoPanic asserts that a provider configured with
// PublicOrigins: nil (a valid operator configuration — only the data-encryption
// key is hard-required) does not panic at token exchange and instead falls back
// to the OIDC issuer as the avatar origin. The ID token must still be issued
// successfully (200 OK).
func TestTokenEmptyPublicOriginsNoPanic(t *testing.T) {
	h := newTokenHarness(t)
	// Override to nil PublicOrigins — the issuer remains set.
	h.p.cfg = &configx.Config{
		OIDC:          configx.OIDCConfig{Issuer: testIssuer},
		PublicOrigins: nil,
	}

	ac := baseAuthCode()
	ac.Scope = []string{"openid", "profile", "offline_access"}
	code := h.mintTestCode(t, ac)

	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(codeExchangeForm(code, testVerifier, testRedirect)))

	if rec.Code != http.StatusOK {
		t.Fatalf("empty PublicOrigins must not panic or error; got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if resp.IDToken == "" {
		t.Fatal("expected a non-empty id_token")
	}
}
