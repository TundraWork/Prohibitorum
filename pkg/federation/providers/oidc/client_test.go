package oidc_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	federationoidc "prohibitorum/pkg/federation/providers/oidc"

	"prohibitorum/cmd/smoke/mockop"

	oidclib "github.com/zitadel/oidc/v3/pkg/oidc"
)

// --- helpers ----------------------------------------------------------------

// noRedirectClient refuses to follow redirects; the /authorize handler
// returns a 302 we want to inspect, not chase.
func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// pkceVerifierAndChallenge returns (verifier, challenge) where challenge
// is the SHA-256 base64url-encoded transform of verifier (S256).
func pkceVerifierAndChallenge() (string, string) {
	verifier := "test-verifier-0123456789abcdefghijklmnopqrstuvwxyz"
	h := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(h[:])
}

// newMockOP spins up the in-process mock OP and returns the httptest
// server along with the Server wrapper for fixturing claims. Caller
// must not Close ts manually; t.Cleanup handles it.
func newMockOP(t *testing.T) (*httptest.Server, *mockop.Server) {
	t.Helper()
	op, err := mockop.New("")
	if err != nil {
		t.Fatalf("mockop.New: %v", err)
	}
	ts := httptest.NewServer(op.Routes())
	op.SetBase(ts.URL)
	t.Cleanup(ts.Close)
	return ts, op
}

// driveAuthorize hits /authorize on the mock OP using the URL emitted by
// Client.AuthURL, and returns the code from the redirect Location.
func driveAuthorize(t *testing.T, authURL string) string {
	t.Helper()
	resp, err := noRedirectClient().Get(authURL)
	if err != nil {
		t.Fatalf("GET /authorize: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("authorize status = %d, want 302; body=%s", resp.StatusCode, body)
	}
	loc, err := resp.Location()
	if err != nil {
		t.Fatalf("authorize Location: %v", err)
	}
	code := loc.Query().Get("code")
	if code == "" {
		t.Fatalf("authorize redirect missing code: %s", loc.String())
	}
	return code
}

// newClient constructs a Client against the given mock OP with the
// canonical test scopes and a redirect URI that doesn't need to be a
// real server (the mock OP redirects to it but our tests use
// noRedirectClient).
func newClient(t *testing.T, ts *httptest.Server) *federationoidc.Client {
	t.Helper()
	resolved, err := federationoidc.ResolveConfig(context.Background(), federationoidc.Config{IssuerURL: ts.URL, ClientID: "test-client", Scopes: []string{"openid", "profile", "email"}, UsernameClaim: "preferred_username", DisplayNameClaim: "name", EmailClaim: "email", PictureClaim: "picture", SubjectClaim: "sub", ConfigurationMode: "discovery", TokenAuthMethod: "discovery", PKCEMethod: "S256", AllowPrivateNetwork: true})
	if err != nil {
		t.Fatal(err)
	}
	c, err := federationoidc.NewClient(context.Background(), "test-client", "test-secret", "https://rp.example.test/callback", resolved, nil, true)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// --- tests ------------------------------------------------------------------

func TestClient_NewClientFetchesDiscovery(t *testing.T) {
	ts, _ := newMockOP(t)
	c := newClient(t, ts)

	if got, want := c.Issuer(), ts.URL; got != want {
		t.Errorf("Issuer() = %q, want %q", got, want)
	}
	if got, want := c.TokenEndpoint(), ts.URL+"/token"; got != want {
		t.Errorf("TokenEndpoint() = %q, want %q", got, want)
	}
}

func TestClient_AuthURLContainsPKCEAndState(t *testing.T) {
	ts, _ := newMockOP(t)
	c := newClient(t, ts)

	_, challenge := pkceVerifierAndChallenge()
	state := "test-state-xyz"
	nonce := "test-nonce-abc"

	raw := c.AuthURL(state, nonce, challenge)
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse AuthURL output: %v", err)
	}

	if got, want := u.Scheme+"://"+u.Host+u.Path, ts.URL+"/authorize"; got != want {
		t.Errorf("AuthURL endpoint = %q, want %q", got, want)
	}
	q := u.Query()
	if q.Get("state") != state {
		t.Errorf("state = %q, want %q", q.Get("state"), state)
	}
	if q.Get("nonce") != nonce {
		t.Errorf("nonce = %q, want %q", q.Get("nonce"), nonce)
	}
	if q.Get("code_challenge") != challenge {
		t.Errorf("code_challenge = %q, want %q", q.Get("code_challenge"), challenge)
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", q.Get("code_challenge_method"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q, want code", q.Get("response_type"))
	}
	if q.Get("client_id") != "test-client" {
		t.Errorf("client_id = %q, want test-client", q.Get("client_id"))
	}
	if scope := q.Get("scope"); !strings.Contains(scope, "openid") ||
		!strings.Contains(scope, "profile") ||
		!strings.Contains(scope, "email") {
		t.Errorf("scope = %q, want openid+profile+email", scope)
	}
}

func TestClient_ExchangeHappyPath(t *testing.T) {
	ts, op := newMockOP(t)
	op.SetClaims("sub-123", "alice@example.test", true, "alice", "Alice Example")
	op.SetAMR([]string{"pwd", "mfa"})

	c := newClient(t, ts)

	verifier, challenge := pkceVerifierAndChallenge()
	state := "exch-state-1"
	nonce := "exch-nonce-1"
	authURL := c.AuthURL(state, nonce, challenge)
	code := driveAuthorize(t, authURL)

	toks, err := c.Exchange(context.Background(), code, verifier, ts.URL, nonce)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	if toks.Subject != "sub-123" {
		t.Errorf("Subject = %q, want sub-123", toks.Subject)
	}
	if toks.Email != "alice@example.test" {
		t.Errorf("Email = %q, want alice@example.test", toks.Email)
	}
	if !toks.EmailVerified {
		t.Error("EmailVerified = false, want true")
	}
	if toks.PreferredUsername != "alice" {
		t.Errorf("PreferredUsername = %q, want alice", toks.PreferredUsername)
	}
	if toks.Name != "Alice Example" {
		t.Errorf("Name = %q, want Alice Example", toks.Name)
	}
	if toks.Issuer != ts.URL {
		t.Errorf("Issuer = %q, want %q", toks.Issuer, ts.URL)
	}
	if toks.Nonce != nonce {
		t.Errorf("Nonce = %q, want %q", toks.Nonce, nonce)
	}
	if toks.IDToken == "" {
		t.Error("IDToken is empty")
	}
	if len(toks.AMR) != 2 || toks.AMR[0] != "pwd" || toks.AMR[1] != "mfa" {
		t.Errorf("AMR = %v, want [pwd mfa]", toks.AMR)
	}

	// Raw claims map MUST hoist the OIDC-typed standard claims under their
	// JSON-tag keys so admins who set username_claim="preferred_username"
	// (the schema default) get the same value the old typed-field path
	// returned. This is what makes the per-IdP override mechanism
	// backwards-compatible for OPs that ship the OIDC defaults.
	if toks.Raw == nil {
		t.Fatal("Raw is nil; want populated claims map")
	}
	if toks.Raw["preferred_username"] != "alice" {
		t.Errorf("raw[preferred_username] = %v, want alice", toks.Raw["preferred_username"])
	}
	if toks.Raw["name"] != "Alice Example" {
		t.Errorf("raw[name] = %v, want Alice Example", toks.Raw["name"])
	}
	if toks.Raw["email"] != "alice@example.test" {
		t.Errorf("raw[email] = %v, want alice@example.test", toks.Raw["email"])
	}
	if toks.Raw["sub"] != "sub-123" {
		t.Errorf("raw[sub] = %v, want sub-123", toks.Raw["sub"])
	}
}

func TestClaimString_PrefersExplicitKey(t *testing.T) {
	raw := map[string]any{"upn": "alice@corp", "preferred_username": "alice"}
	if got := federationoidc.ClaimString(raw, "upn"); got != "alice@corp" {
		t.Errorf("override-key not honored: %q", got)
	}
	if got := federationoidc.ClaimString(raw, "preferred_username"); got != "alice" {
		t.Errorf("default-key path broken: %q", got)
	}
}

func TestClaimString_FallbackToEmpty(t *testing.T) {
	raw := map[string]any{}
	if got := federationoidc.ClaimString(raw, "preferred_username"); got != "" {
		t.Errorf("missing key should yield empty: %q", got)
	}
}

func TestClaimString_NonStringValueYieldsEmpty(t *testing.T) {
	raw := map[string]any{"upn": 42}
	if got := federationoidc.ClaimString(raw, "upn"); got != "" {
		t.Errorf("non-string value should yield empty: %q", got)
	}
}

func TestClaimString_EmptyKeyYieldsEmpty(t *testing.T) {
	if got := federationoidc.ClaimString(nil, ""); got != "" {
		t.Errorf("empty key should yield empty: %q", got)
	}
	if got := federationoidc.ClaimString(map[string]any{"foo": "bar"}, ""); got != "" {
		t.Errorf("empty key with non-nil map should yield empty: %q", got)
	}
}

func TestClient_ExchangeRejectsIssuerMismatch(t *testing.T) {
	ts, op := newMockOP(t)
	op.SetClaims("sub-x", "x@example.test", true, "x", "X")
	op.OverrideIssuer("https://attacker.example.test")

	c := newClient(t, ts)

	verifier, challenge := pkceVerifierAndChallenge()
	authURL := c.AuthURL("st", "no", challenge)
	code := driveAuthorize(t, authURL)

	_, err := c.Exchange(context.Background(), code, verifier, ts.URL, "no")
	if err == nil {
		t.Fatal("Exchange succeeded; want issuer-mismatch error")
	}
	// The library's verifier rejects the id_token first because its iss
	// doesn't match the discovery issuer; whatever fails, the error
	// should at least mention issuer somewhere in the chain.
	if !strings.Contains(strings.ToLower(err.Error()), "issuer") &&
		!strings.Contains(strings.ToLower(err.Error()), "iss") {
		t.Errorf("error %q does not mention issuer/iss", err)
	}
}

func TestClient_ExchangeRejectsNonceMismatch(t *testing.T) {
	ts, op := newMockOP(t)
	op.SetClaims("sub-n", "n@example.test", true, "n", "N")

	c := newClient(t, ts)

	verifier, challenge := pkceVerifierAndChallenge()
	authURL := c.AuthURL("st", "real-nonce", challenge)
	code := driveAuthorize(t, authURL)

	// Call Exchange with the WRONG expectedNonce.
	_, err := c.Exchange(context.Background(), code, verifier, ts.URL, "wrong-nonce")
	if err == nil {
		t.Fatal("Exchange succeeded; want nonce-mismatch error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "nonce") {
		t.Errorf("error %q does not mention nonce", err)
	}
}

// TestClient_ExchangePictureHoisted verifies that when the upstream OP includes
// a picture claim in the id_token, Exchange hoists it into Tokens.Raw["picture"]
// so ClaimString can read it via the per-IdP picture_claim override.
func TestClient_ExchangePictureHoisted(t *testing.T) {
	ts, op := newMockOP(t)
	op.SetClaims("sub-pic", "pic@example.test", true, "pic", "Pic User")
	op.SetPicture("https://cdn.example.test/avatar/pic.jpg")

	c := newClient(t, ts)

	verifier, challenge := pkceVerifierAndChallenge()
	state := "pic-state-1"
	nonce := "pic-nonce-1"
	authURL := c.AuthURL(state, nonce, challenge)
	code := driveAuthorize(t, authURL)

	toks, err := c.Exchange(context.Background(), code, verifier, ts.URL, nonce)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if toks.Raw == nil {
		t.Fatal("Raw is nil; want populated claims map")
	}
	if got, want := toks.Raw["picture"], "https://cdn.example.test/avatar/pic.jpg"; got != want {
		t.Errorf("raw[picture] = %v, want %q", got, want)
	}
}

// TestClient_ExchangeNoPictureClaimAbsent verifies that when the upstream OP
// does not include a picture claim, Raw["picture"] is absent (not empty string),
// so ClaimString returns "" and no spurious empty-string picture URL is stored.
func TestClient_ExchangeNoPictureClaimAbsent(t *testing.T) {
	ts, op := newMockOP(t)
	op.SetClaims("sub-nopic", "nopic@example.test", true, "nopic", "No Pic User")
	// Deliberately do NOT call op.SetPicture — picture claim should be absent.

	c := newClient(t, ts)

	verifier, challenge := pkceVerifierAndChallenge()
	state := "nopic-state-1"
	nonce := "nopic-nonce-1"
	authURL := c.AuthURL(state, nonce, challenge)
	code := driveAuthorize(t, authURL)

	toks, err := c.Exchange(context.Background(), code, verifier, ts.URL, nonce)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if toks.Raw == nil {
		t.Fatal("Raw is nil; want populated claims map")
	}
	if _, present := toks.Raw["picture"]; present {
		t.Errorf("raw[picture] = %v; want key absent when OP emits no picture claim", toks.Raw["picture"])
	}
}

// TestUserInfoToRaw exercises the exported UserInfoToRaw helper directly
// (no live server needed). It verifies that typed UserInfoProfile fields are
// hoisted under their JSON-tag keys, and that extras from Claims pass through.
func TestUserInfoToRaw(t *testing.T) {
	info := &oidclib.UserInfo{
		Subject: "sub-uir",
		UserInfoProfile: oidclib.UserInfoProfile{
			Picture:           "https://pic/x",
			PreferredUsername: "uir-user",
			Name:              "UIR User",
		},
		UserInfoEmail: oidclib.UserInfoEmail{
			Email: "uir@example.test",
		},
	}
	// Inject an extra claim that would only be in Claims (e.g. a custom claim).
	info.Claims = map[string]any{"custom_claim": "cval"}

	raw := federationoidc.UserInfoToRaw(info)

	if raw == nil {
		t.Fatal("UserInfoToRaw returned nil")
	}
	if got, want := raw["picture"], "https://pic/x"; got != want {
		t.Errorf("raw[picture] = %v, want %q", got, want)
	}
	if got, want := raw["name"], "UIR User"; got != want {
		t.Errorf("raw[name] = %v, want %q", got, want)
	}
	if got, want := raw["preferred_username"], "uir-user"; got != want {
		t.Errorf("raw[preferred_username] = %v, want %q", got, want)
	}
	if got, want := raw["email"], "uir@example.test"; got != want {
		t.Errorf("raw[email] = %v, want %q", got, want)
	}
	if got, want := raw["custom_claim"], "cval"; got != want {
		t.Errorf("raw[custom_claim] = %v, want %q", got, want)
	}
}

// --- no-id_token upstream (userinfo fallback) -------------------------------

// fallbackOP is a self-built upstream for the no-id_token scenarios: its token
// endpoint answers without an id_token (the GitHub OAuth App shape). The token
// response content type and userinfo endpoint presence are configurable so one
// fixture serves every fallback scenario, and every request is recorded.
type fallbackOP struct {
	base string

	userinfoEnabled bool
	tokenBody       string
	tokenType       string
	userinfoBody    string
	// userinfoRedirect, when set, returns each userinfo response's Location
	// (empty = no redirect) — used to pin the redirect-policy carryover.
	userinfoRedirect func(base string) string

	mu               sync.Mutex
	tokenAccept      string
	tokenRequests    int
	userinfoRequests int
	userinfoAuth     string
}

func newFallbackOP(t *testing.T) (*httptest.Server, *fallbackOP) {
	t.Helper()
	op := &fallbackOP{
		userinfoEnabled: true,
		tokenType:       "application/json",
		tokenBody:       `{"access_token":"at_fallback","token_type":"Bearer","scope":"openid"}`,
		userinfoBody:    `{"id":67890,"login":"octocat","name":"Octo Cat","email":"octo@example.test","email_verified":false,"picture":"https://cdn.example.test/octo.png"}`,
	}
	ts := httptest.NewServer(http.HandlerFunc(op.serve))
	op.base = ts.URL
	t.Cleanup(ts.Close)
	return ts, op
}

func (op *fallbackOP) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		doc := map[string]any{
			"issuer":                                op.base,
			"authorization_endpoint":                op.base + "/authorize",
			"token_endpoint":                        op.base + "/token",
			"jwks_uri":                              op.base + "/jwks",
			"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
		}
		if op.userinfoEnabled {
			doc["userinfo_endpoint"] = op.base + "/userinfo"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(doc)
	case "/token":
		op.mu.Lock()
		op.tokenRequests++
		op.tokenAccept = r.Header.Get("Accept")
		op.mu.Unlock()
		w.Header().Set("Content-Type", op.tokenType)
		w.Write([]byte(op.tokenBody))
	case "/userinfo":
		op.mu.Lock()
		op.userinfoRequests++
		op.userinfoAuth = r.Header.Get("Authorization")
		location := ""
		if op.userinfoRedirect != nil {
			location = op.userinfoRedirect(op.base)
		}
		op.mu.Unlock()
		if location != "" {
			http.Redirect(w, r, location, http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(op.userinfoBody))
	case "/jwks":
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"keys":[]}`))
	default:
		http.NotFound(w, r)
	}
}

func (op *fallbackOP) tokenStats() (accept string, requests int) {
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.tokenAccept, op.tokenRequests
}

func (op *fallbackOP) userinfoStats() (auth string, requests int) {
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.userinfoAuth, op.userinfoRequests
}

// newFallbackClient builds a Client against a fallbackOP using
// client_secret_post so the mock token endpoint needs no Authorization header.
func newFallbackClient(t *testing.T, ts *httptest.Server) *federationoidc.Client {
	t.Helper()
	resolved, err := federationoidc.ResolveConfig(context.Background(), federationoidc.Config{IssuerURL: ts.URL, ClientID: "fallback-client", Scopes: []string{"openid"}, UsernameClaim: "preferred_username", DisplayNameClaim: "name", EmailClaim: "email", PictureClaim: "picture", SubjectClaim: "sub", ConfigurationMode: "discovery", TokenAuthMethod: "client_secret_post", PKCEMethod: "S256", AllowPrivateNetwork: true})
	if err != nil {
		t.Fatal(err)
	}
	c, err := federationoidc.NewClient(context.Background(), "fallback-client", "secret", "https://rp.example.test/callback", resolved, nil, true)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// TestClient_ExchangeFormEncodedWithoutIDTokenFails pins the form-encoded
// failure: without Accept: application/json a GitHub-style token response comes
// back urlencoded, the missing id_token surfaces as "" instead of nil, and
// zitadel tries to parse it as a JWT.
func TestClient_ExchangeFormEncodedWithoutIDTokenFails(t *testing.T) {
	ts, op := newFallbackOP(t)
	op.tokenType = "application/x-www-form-urlencoded"
	op.tokenBody = "access_token=at_form&token_type=bearer&scope=openid"
	c := newFallbackClient(t, ts)

	_, err := c.Exchange(context.Background(), "code", "verifier", ts.URL, "nonce")
	if err == nil {
		t.Fatal("Exchange succeeded; want failure for form-encoded response without id_token")
	}
	if !strings.Contains(err.Error(), "parsing of request failed: token contains an invalid number of segments") {
		t.Fatalf("err = %v, want invalid-segments parse failure", err)
	}
}

// TestClient_ExchangeJSONWithoutIDTokenReturnsFallback verifies the GitHub
// OAuth App shape: a JSON token response with no id_token yields a usable
// fallback result — access token and token type preserved, IDToken empty — and
// the token request went out asking for JSON.
func TestClient_ExchangeJSONWithoutIDTokenReturnsFallback(t *testing.T) {
	ts, op := newFallbackOP(t)
	c := newFallbackClient(t, ts)

	toks, err := c.Exchange(context.Background(), "code", "verifier", ts.URL, "nonce")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if toks.IDToken != "" {
		t.Errorf("IDToken = %q, want empty", toks.IDToken)
	}
	if toks.AccessToken == "" {
		t.Error("AccessToken is empty; fallback needs it for userinfo")
	}
	if toks.TokenType != "Bearer" {
		t.Errorf("TokenType = %q, want Bearer", toks.TokenType)
	}
	accept, _ := op.tokenStats()
	if accept != "application/json" {
		t.Errorf("token request Accept = %q, want application/json", accept)
	}
}

// TestClient_UserInfoRawPreservesRedirectPolicy pins the outbound-policy
// carryover: the hardened client's CheckRedirect (max 5 hops, https-only
// redirect targets) must survive the accept-header re-wrap, so a userinfo
// handler that chains 8 redirects fails fast instead of loading forever.
func TestClient_UserInfoRawPreservesRedirectPolicy(t *testing.T) {
	ts, op := newFallbackOP(t)
	op.userinfoBody = ""
	c := newFallbackClient(t, ts)
	hops := 0
	op.userinfoRedirect = func(base string) string {
		hops++
		if hops > 8 {
			return base
		}
		return base + "/userinfo"
	}

	_, err := c.UserInfoRaw(context.Background(), "at_fallback", "Bearer")
	if err == nil {
		t.Fatal("UserInfoRaw succeeded; want too-many-redirects failure")
	}
	if !strings.Contains(err.Error(), "too many redirects (>5)") {
		t.Fatalf("err = %v, want redirect-policy failure", err)
	}
}

// TestClient_UserInfoRawFetchesClaims verifies the raw fallback fetch: claims
// come back verbatim, numeric identifiers stay json.Number with their exact
// text, and the request carries the access token's Authorization header.
func TestClient_UserInfoRawFetchesClaims(t *testing.T) {
	ts, op := newFallbackOP(t)
	c := newFallbackClient(t, ts)

	claims, err := c.UserInfoRaw(context.Background(), "at_fallback", "Bearer")
	if err != nil {
		t.Fatalf("UserInfoRaw: %v", err)
	}
	if got := claims["login"]; got != "octocat" {
		t.Errorf("login = %v, want octocat", got)
	}
	id, ok := claims["id"].(json.Number)
	if !ok {
		t.Fatalf("id = %T(%v), want json.Number", claims["id"], claims["id"])
	}
	if id.String() != "67890" {
		t.Errorf("id = %s, want exact text 67890", id)
	}
	auth, requests := op.userinfoStats()
	if requests != 1 {
		t.Fatalf("userinfo requests = %d, want 1", requests)
	}
	if auth != "Bearer at_fallback" {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer at_fallback")
	}
}

// TestClient_UserInfoRawEmptyTokenTypeDefaultsToBearer verifies the token type
// default for upstreams whose token response omits token_type.
func TestClient_UserInfoRawEmptyTokenTypeDefaultsToBearer(t *testing.T) {
	ts, op := newFallbackOP(t)
	c := newFallbackClient(t, ts)

	if _, err := c.UserInfoRaw(context.Background(), "at_fallback", ""); err != nil {
		t.Fatalf("UserInfoRaw: %v", err)
	}
	auth, _ := op.userinfoStats()
	if auth != "Bearer at_fallback" {
		t.Errorf("Authorization = %q, want Bearer prefix", auth)
	}
}

// TestClient_UserInfoRawWithoutEndpoint verifies the guard: with no resolved
// userinfo endpoint (GitHub discovery, or manual mode leaving userinfo null)
// the call fails without any network request.
func TestClient_UserInfoRawWithoutEndpoint(t *testing.T) {
	ts, op := newFallbackOP(t)
	op.userinfoEnabled = false
	c := newFallbackClient(t, ts)

	_, err := c.UserInfoRaw(context.Background(), "at_fallback", "Bearer")
	if err == nil {
		t.Fatal("UserInfoRaw succeeded; want error without a resolved endpoint")
	}
	if !strings.Contains(err.Error(), "no userinfo endpoint resolved") {
		t.Fatalf("err = %v, want missing-endpoint error", err)
	}
	if _, requests := op.userinfoStats(); requests != 0 {
		t.Errorf("userinfo requests = %d, want 0", requests)
	}
}

// TestClaimIdentifier covers the subject-extraction contract: strings pass
// through, integer json.Number keeps exact text, everything else yields "".
func TestClaimIdentifier(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  string
	}{
		{"string", "sub-1", "sub-1"},
		{"integer number", json.Number("67890"), "67890"},
		{"integer number leading zeros", json.Number("0042"), "0042"},
		{"float number", json.Number("6.78"), ""},
		{"exponent number", json.Number("1e6"), ""},
		{"bool", true, ""},
		{"object", map[string]any{"a": 1}, ""},
		{"array", []any{1, 2}, ""},
		{"nil", nil, ""},
		{"missing", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := map[string]any{}
			if tc.name != "missing" {
				raw["claim"] = tc.value
			}
			if got := federationoidc.ClaimIdentifier(raw, "claim"); got != tc.want {
				t.Errorf("ClaimIdentifier = %q, want %q", got, tc.want)
			}
		})
	}

	if got := federationoidc.ClaimIdentifier(map[string]any{"claim": "x"}, ""); got != "" {
		t.Errorf("empty name = %q, want empty", got)
	}
}

// TestClaimBool pins the boolean reader: only true/false pass; strings,
// json.Number and absence all read as false — which is what keeps a missing
// email_verified treated as unverified rather than an error.
func TestClaimBool(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  bool
	}{
		{"true", true, true},
		{"false", false, false},
		{"string truthy", "true", false},
		{"number one", json.Number("1"), false},
		{"nil", nil, false},
		{"missing", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := map[string]any{}
			if tc.name != "missing" {
				raw["claim"] = tc.value
			}
			if got := federationoidc.ClaimBool(raw, "claim"); got != tc.want {
				t.Errorf("ClaimBool = %v, want %v", got, tc.want)
			}
		})
	}
}
