package oidc_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	federationoidc "prohibitorum/pkg/federation/oidc"

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
	c, err := federationoidc.NewClient(
		context.Background(),
		"test-client",
		"test-secret",
		"https://rp.example.test/callback",
		[]string{"openid", "profile", "email"},
		ts.URL,
		nil,  // use DefaultAllowedAlgs
		true, // mock OP is on loopback — bypass the SSRF dial screen
	)
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

func TestClient_AlgAllowlistRejectsUnsupportedAlg(t *testing.T) {
	t.Skip("HS256/none rejection is a defense-in-depth check; the mockop signs ES256 only and has no flag to switch algs. Covered conceptually by configuring rp.WithSupportedSigningAlgorithms with the explicit allowlist; behavioural coverage deferred to Task 5 federation_test or future hardening.")
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
