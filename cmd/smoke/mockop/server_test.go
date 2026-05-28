package mockop

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// --- Test harness -------------------------------------------------------

type serverHarness struct {
	s  *Server
	ts *httptest.Server
}

// newHarness creates a Server, wires it into an httptest.Server, and
// late-binds the base via SetBase. The caller is responsible for closing
// the httptest.Server (use t.Cleanup).
func newHarness(t *testing.T) *serverHarness {
	t.Helper()
	s, err := New("")
	if err != nil {
		t.Fatalf("mockop.New: %v", err)
	}
	ts := httptest.NewServer(s.Routes())
	s.SetBase(ts.URL)
	t.Cleanup(ts.Close)
	return &serverHarness{s: s, ts: ts}
}

// noRedirectClient returns an *http.Client that refuses to follow
// redirects so the test can inspect the 302 Location header directly.
func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// pkceVerifierAndChallenge returns a (verifier, challenge) pair where the
// challenge is the S256 transform of the verifier.
func pkceVerifierAndChallenge() (string, string) {
	verifier := "test-verifier-0123456789abcdefghijklmnopqrstuvwxyz"
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge
}

// drive_authorize executes the /authorize step with valid params and
// returns the redirect Location URL. The verifier returned must be passed
// to /token. The redirect_uri argument controls where the OP says it would
// redirect (the redirect itself is not followed).
func driveAuthorize(t *testing.T, h *serverHarness, clientID, redirectURI, state, nonce string) (*url.URL, string) {
	t.Helper()
	verifier, challenge := pkceVerifierAndChallenge()

	authURL := h.ts.URL + "/authorize?" + url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"state":                 {state},
		"nonce":                 {nonce},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"response_type":         {"code"},
		"scope":                 {"openid profile email"},
	}.Encode()

	c := noRedirectClient()
	resp, err := c.Get(authURL)
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
	return loc, verifier
}

// decodeJWT splits a compact JWS and returns (headerJSON, claimsJSON).
// It does NOT verify the signature; that's not the point of these tests
// (signature verification is exercised end-to-end in Task 3).
func decodeJWT(t *testing.T, tok string) (map[string]any, map[string]any) {
	t.Helper()
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt: want 3 parts, got %d", len(parts))
	}
	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("jwt header b64: %v", err)
	}
	cb, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("jwt claims b64: %v", err)
	}
	sb, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("jwt sig b64: %v", err)
	}
	if len(sb) != 64 {
		t.Errorf("ES256 signature length = %d, want 64 (r||s, 32 each)", len(sb))
	}
	var hdr, claims map[string]any
	if err := json.Unmarshal(hb, &hdr); err != nil {
		t.Fatalf("jwt header json: %v", err)
	}
	if err := json.Unmarshal(cb, &claims); err != nil {
		t.Fatalf("jwt claims json: %v", err)
	}
	return hdr, claims
}

// --- Tests --------------------------------------------------------------

func TestMockOP_DiscoveryServes(t *testing.T) {
	h := newHarness(t)

	resp, err := http.Get(h.ts.URL + "/.well-known/openid-configuration")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("discovery status = %d", resp.StatusCode)
	}

	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}

	if got, want := doc["issuer"], h.ts.URL; got != want {
		t.Errorf("issuer = %q, want %q", got, want)
	}
	if got, want := doc["authorization_endpoint"], h.ts.URL+"/authorize"; got != want {
		t.Errorf("authorization_endpoint = %q, want %q", got, want)
	}
	if got, want := doc["token_endpoint"], h.ts.URL+"/token"; got != want {
		t.Errorf("token_endpoint = %q, want %q", got, want)
	}
	if got, want := doc["jwks_uri"], h.ts.URL+"/jwks"; got != want {
		t.Errorf("jwks_uri = %q, want %q", got, want)
	}
	if !containsString(doc["id_token_signing_alg_values_supported"], "ES256") {
		t.Errorf("ES256 missing from signing algs: %v", doc["id_token_signing_alg_values_supported"])
	}
	if !containsString(doc["code_challenge_methods_supported"], "S256") {
		t.Errorf("S256 missing from challenge methods: %v", doc["code_challenge_methods_supported"])
	}
}

func TestMockOP_JWKSServes(t *testing.T) {
	h := newHarness(t)

	resp, err := http.Get(h.ts.URL + "/jwks")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("jwks status = %d", resp.StatusCode)
	}

	var doc struct {
		Keys []map[string]any `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Keys) != 1 {
		t.Fatalf("keys length = %d, want 1", len(doc.Keys))
	}
	k := doc.Keys[0]
	if k["kty"] != "EC" {
		t.Errorf("kty = %v, want EC", k["kty"])
	}
	if k["crv"] != "P-256" {
		t.Errorf("crv = %v, want P-256", k["crv"])
	}
	if k["alg"] != "ES256" {
		t.Errorf("alg = %v, want ES256", k["alg"])
	}
	if k["use"] != "sig" {
		t.Errorf("use = %v, want sig", k["use"])
	}
	if k["kid"] == "" {
		t.Error("kid missing")
	}
	// base64.RawURLEncoding of 32 bytes is exactly 43 chars (no padding).
	for _, field := range []string{"x", "y"} {
		v, ok := k[field].(string)
		if !ok {
			t.Errorf("%s missing or not a string", field)
			continue
		}
		if len(v) != 43 {
			t.Errorf("%s length = %d, want 43 (32-byte base64url)", field, len(v))
		}
		b, err := base64.RawURLEncoding.DecodeString(v)
		if err != nil {
			t.Errorf("%s b64 decode: %v", field, err)
			continue
		}
		if len(b) != 32 {
			t.Errorf("%s decoded length = %d, want 32", field, len(b))
		}
	}
}

func TestMockOP_AuthorizeRedirectsWithCodeAndIss(t *testing.T) {
	h := newHarness(t)
	h.s.SetClaims("sub-redir", "u@example.com", true, "u", "User")

	loc, _ := driveAuthorize(t, h, "client-1", "https://rp.example/callback", "state-xyz", "nonce-abc")

	if loc.Scheme != "https" || loc.Host != "rp.example" || loc.Path != "/callback" {
		t.Errorf("redirect target wrong: %s", loc)
	}
	q := loc.Query()
	if q.Get("code") == "" {
		t.Error("code missing from redirect")
	}
	if got, want := q.Get("state"), "state-xyz"; got != want {
		t.Errorf("state = %q, want %q", got, want)
	}
	if got, want := q.Get("iss"), h.ts.URL; got != want {
		t.Errorf("iss = %q, want %q", got, want)
	}
}

func TestMockOP_TokenExchange_PKCEHappyPath(t *testing.T) {
	h := newHarness(t)
	h.s.SetClaims("sub-happy", "happy@example.com", true, "happy", "Happy User")
	h.s.SetAMR([]string{"pwd", "mfa"})

	const (
		clientID    = "client-1"
		redirectURI = "https://rp.example/cb"
		state       = "state-1"
		nonce       = "nonce-1"
	)

	loc, verifier := driveAuthorize(t, h, clientID, redirectURI, state, nonce)
	code := loc.Query().Get("code")
	if code == "" {
		t.Fatal("no code in authorize redirect")
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"code_verifier": {verifier},
	}
	resp, err := http.PostForm(h.ts.URL+"/token", form)
	if err != nil {
		t.Fatalf("POST /token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("token status = %d; body=%s", resp.StatusCode, body)
	}

	var tok struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
		IDToken     string `json:"id_token"`
		Scope       string `json:"scope"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		t.Fatalf("token json: %v", err)
	}
	if tok.AccessToken == "" || tok.IDToken == "" {
		t.Fatalf("missing tokens in response: %+v", tok)
	}
	if tok.TokenType != "Bearer" {
		t.Errorf("token_type = %q, want Bearer", tok.TokenType)
	}

	hdr, claims := decodeJWT(t, tok.IDToken)
	if hdr["alg"] != "ES256" {
		t.Errorf("alg = %v, want ES256", hdr["alg"])
	}
	if hdr["typ"] != "JWT" {
		t.Errorf("typ = %v, want JWT", hdr["typ"])
	}
	if hdr["kid"] == "" {
		t.Error("kid missing from header")
	}
	if got, want := claims["iss"], h.ts.URL; got != want {
		t.Errorf("iss = %v, want %v", got, want)
	}
	if got, want := claims["aud"], clientID; got != want {
		t.Errorf("aud = %v, want %v", got, want)
	}
	if got, want := claims["nonce"], nonce; got != want {
		t.Errorf("nonce = %v, want %v", got, want)
	}
	if got, want := claims["sub"], "sub-happy"; got != want {
		t.Errorf("sub = %v, want %v", got, want)
	}
	if got, want := claims["email"], "happy@example.com"; got != want {
		t.Errorf("email = %v, want %v", got, want)
	}
	if got := claims["email_verified"]; got != true {
		t.Errorf("email_verified = %v, want true", got)
	}
	if got, want := claims["preferred_username"], "happy"; got != want {
		t.Errorf("preferred_username = %v, want %v", got, want)
	}
	if got, want := claims["name"], "Happy User"; got != want {
		t.Errorf("name = %v, want %v", got, want)
	}
	amr, ok := claims["amr"].([]any)
	if !ok || len(amr) != 2 || amr[0] != "pwd" || amr[1] != "mfa" {
		t.Errorf("amr = %v, want [pwd mfa]", claims["amr"])
	}
}

func TestMockOP_TokenExchange_RejectsPKCEMismatch(t *testing.T) {
	h := newHarness(t)
	h.s.SetClaims("sub-x", "x@example.com", true, "x", "X")

	const (
		clientID    = "client-1"
		redirectURI = "https://rp.example/cb"
	)

	loc, _ := driveAuthorize(t, h, clientID, redirectURI, "state", "nonce")
	code := loc.Query().Get("code")

	// Submit a verifier that doesn't match the challenge we issued.
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"code_verifier": {"a-wrong-verifier-xxxxxxxxxxxxxxxxxxxxxxxxxxxx"},
	}
	resp, err := http.PostForm(h.ts.URL+"/token", form)
	if err != nil {
		t.Fatalf("POST /token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("token status = %d, want 400; body=%s", resp.StatusCode, body)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "invalid_grant") {
		t.Errorf("body = %q, want it to contain invalid_grant", string(body))
	}
}

func TestMockOP_AuthorizeWithErrorOverride(t *testing.T) {
	h := newHarness(t)
	h.s.FailWithError("access_denied", "user said no")

	// First /authorize: should redirect with error params.
	loc, _ := driveAuthorize(t, h, "client-1", "https://rp.example/cb", "state-1", "nonce-1")
	q := loc.Query()
	if got, want := q.Get("error"), "access_denied"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
	if got, want := q.Get("error_description"), "user said no"; got != want {
		t.Errorf("error_description = %q, want %q", got, want)
	}
	if got, want := q.Get("state"), "state-1"; got != want {
		t.Errorf("state = %q, want %q", got, want)
	}
	if q.Get("code") != "" {
		t.Errorf("code should be absent in error redirect; got %q", q.Get("code"))
	}

	// Second /authorize: error hook is single-shot; should succeed normally.
	h.s.SetClaims("sub-2", "u2@example.com", true, "u2", "U2")
	loc2, _ := driveAuthorize(t, h, "client-1", "https://rp.example/cb", "state-2", "nonce-2")
	q2 := loc2.Query()
	if q2.Get("error") != "" {
		t.Errorf("second authorize should not carry error; got %q", q2.Get("error"))
	}
	if q2.Get("code") == "" {
		t.Error("second authorize missing code")
	}
}

func TestMockOP_OverrideIssuerEndsUpInIDToken(t *testing.T) {
	h := newHarness(t)
	const attackerIss = "https://attacker.example.com"
	h.s.SetClaims("sub-mix", "mix@example.com", true, "mix", "Mix Up")
	h.s.OverrideIssuer(attackerIss)

	const (
		clientID    = "client-1"
		redirectURI = "https://rp.example/cb"
		state       = "state-mix"
		nonce       = "nonce-mix"
	)

	loc, verifier := driveAuthorize(t, h, clientID, redirectURI, state, nonce)

	// 1. Redirect iss must be the override.
	if got := loc.Query().Get("iss"); got != attackerIss {
		t.Errorf("redirect iss = %q, want %q", got, attackerIss)
	}

	code := loc.Query().Get("code")
	if code == "" {
		t.Fatal("no code in authorize redirect")
	}

	// 2. ID-token iss claim must also be the override.
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"code_verifier": {verifier},
	}
	resp, err := http.PostForm(h.ts.URL+"/token", form)
	if err != nil {
		t.Fatalf("POST /token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("token status = %d; body=%s", resp.StatusCode, body)
	}
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		t.Fatalf("token json: %v", err)
	}
	_, claims := decodeJWT(t, tok.IDToken)
	if got, want := claims["iss"], attackerIss; got != want {
		t.Errorf("id_token iss = %v, want %v", got, want)
	}
}

func TestMockOP_UserinfoStubReturns501(t *testing.T) {
	h := newHarness(t)

	resp, err := http.Get(h.ts.URL + "/userinfo")
	if err != nil {
		t.Fatalf("GET /userinfo: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("userinfo status = %d, want 501", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "userinfo_not_implemented") {
		t.Errorf("body = %q, want it to contain userinfo_not_implemented", string(body))
	}
}

func TestMockOP_TokenExchange_RejectsCodeReplay(t *testing.T) {
	h := newHarness(t)
	h.s.SetClaims("sub-replay", "replay@example.com", true, "replay", "Replay User")

	const (
		clientID    = "client-1"
		redirectURI = "https://rp.example/cb"
		state       = "state-replay"
		nonce       = "nonce-replay"
	)

	loc, verifier := driveAuthorize(t, h, clientID, redirectURI, state, nonce)
	code := loc.Query().Get("code")
	if code == "" {
		t.Fatal("no code in authorize redirect")
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"code_verifier": {verifier},
	}

	// First exchange: must succeed and return an id_token.
	resp1, err := http.PostForm(h.ts.URL+"/token", form)
	if err != nil {
		t.Fatalf("first POST /token: %v", err)
	}
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp1.Body)
		t.Fatalf("first token status = %d, want 200; body=%s", resp1.StatusCode, body)
	}
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(resp1.Body).Decode(&tok); err != nil {
		t.Fatalf("first token json: %v", err)
	}
	if tok.IDToken == "" {
		t.Fatal("first exchange returned empty id_token")
	}

	// Second exchange with the same code: must be rejected as invalid_grant
	// (code is single-use; this is what RP-client tests will rely on).
	resp2, err := http.PostForm(h.ts.URL+"/token", form)
	if err != nil {
		t.Fatalf("second POST /token: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp2.Body)
		t.Fatalf("second token status = %d, want 400; body=%s", resp2.StatusCode, body)
	}
	body, _ := io.ReadAll(resp2.Body)
	if !strings.Contains(string(body), "invalid_grant") {
		t.Errorf("second token body = %q, want it to contain invalid_grant", string(body))
	}
}

// --- Small helpers ------------------------------------------------------

// containsString reports whether v (expected to be a []any of strings, as
// produced by JSON decoding into map[string]any) contains the target.
func containsString(v any, target string) bool {
	arr, ok := v.([]any)
	if !ok {
		return false
	}
	for _, e := range arr {
		if s, ok := e.(string); ok && s == target {
			return true
		}
	}
	return false
}

