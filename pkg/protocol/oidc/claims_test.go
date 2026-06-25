package oidc

import (
	"testing"
	"time"

	"prohibitorum/pkg/db"

	"github.com/jackc/pgx/v5/pgtype"
)

// testAccount builds an Account with a fixed subject UUID and profile fields.
func testAccount(t *testing.T) db.Account {
	t.Helper()
	var b [16]byte
	for i := range b {
		b[i] = byte(i) // 00 01 02 ... 0f → "00010203-0405-0607-0809-0a0b0c0d0e0f"
	}
	return db.Account{
		Username:    "alice",
		DisplayName: "Alice Example",
		OidcSubject: pgtype.UUID{Bytes: b, Valid: true},
		Role:        "admin",
		Attributes:  []byte(`{"team":"x"}`),
	}
}

func TestClaimsSubjectOf(t *testing.T) {
	a := testAccount(t)
	sub := subjectOf(a)
	want := "00010203-0405-0607-0809-0a0b0c0d0e0f"
	if sub != want {
		t.Fatalf("subjectOf = %q, want %q", sub, want)
	}
	if len(sub) != 36 {
		t.Fatalf("subjectOf len = %d, want 36", len(sub))
	}
	// Hyphens in the canonical positions.
	for _, pos := range []int{8, 13, 18, 23} {
		if sub[pos] != '-' {
			t.Fatalf("subjectOf missing hyphen at %d: %q", pos, sub)
		}
	}

	// Invalid UUID → empty string.
	if got := subjectOf(db.Account{}); got != "" {
		t.Fatalf("subjectOf(invalid) = %q, want \"\"", got)
	}
}

func TestClaimsHasScope(t *testing.T) {
	granted := []string{"openid", "profile"}
	if !hasScope(granted, "openid") {
		t.Fatal("hasScope openid = false, want true")
	}
	if !hasScope(granted, "profile") {
		t.Fatal("hasScope profile = false, want true")
	}
	if hasScope(granted, "email") {
		t.Fatal("hasScope email = true, want false")
	}
	if hasScope(nil, "openid") {
		t.Fatal("hasScope(nil) = true, want false")
	}
}

func TestClaimsAtHash(t *testing.T) {
	const tok = "an-access-token"
	h := atHash(tok)
	if len(h) != 22 {
		t.Fatalf("atHash len = %d, want 22", len(h))
	}
	// Deterministic.
	if atHash(tok) != h {
		t.Fatal("atHash not deterministic")
	}
	// Known vector (independently computed): base64url(left-most 16 bytes of
	// SHA-256("an-access-token")), no padding. Hardcoded rather than re-derived
	// so a parallel mistake (wrong half, padded encoding) can't pass silently.
	const want = "YiHPD2T9DaX5B837XJXtow"
	if h != want {
		t.Fatalf("atHash = %q, want %q", h, want)
	}
}

func baseInput() idTokenInput {
	iat := time.Unix(1_700_000_000, 0)
	return idTokenInput{
		Issuer:      "https://idp.example",
		Audience:    "client-abc",
		Nonce:       "nonce-123",
		ACR:         "urn:acr:high",
		SID:         "sess-1",
		AMR:         []string{"webauthn"},
		AccessToken: "the-access-token",
		Scope:       []string{"openid", "profile"},
		IssuedAt:    iat,
		Expiry:      iat.Add(time.Hour),
		AuthTime:    iat.Add(-5 * time.Minute),
	}
}

// TestClaimsEmailScope guards T3.2: the email/email_verified claims are emitted
// iff the `email` scope is granted AND the account has an email; never leaked
// under other scopes, and omitted entirely for an account with no email.
func TestClaimsEmailScope(t *testing.T) {
	a := testAccount(t)
	a.Email = pgtype.Text{String: "alice@example.com", Valid: true}
	a.EmailVerified = true

	// id_token WITH email scope → both claims present.
	in := baseInput()
	in.Scope = []string{"openid", "email"}
	c := idTokenClaims(a, in)
	if c["email"] != "alice@example.com" {
		t.Errorf("email claim = %v, want alice@example.com", c["email"])
	}
	if c["email_verified"] != true {
		t.Errorf("email_verified = %v, want true", c["email_verified"])
	}

	// id_token WITHOUT email scope → omitted (even though the account has one).
	in.Scope = []string{"openid", "profile"}
	c = idTokenClaims(a, in)
	if _, ok := c["email"]; ok {
		t.Error("email claim leaked without the email scope")
	}

	// email scope but NO email on the account → omitted (not emitted empty).
	in.Scope = []string{"openid", "email"}
	c = idTokenClaims(testAccount(t), in)
	if _, ok := c["email"]; ok {
		t.Error("email claim emitted for an account with no email")
	}

	// userinfo parity.
	u := userinfoClaims(a, []string{"openid", "email"}, "", nil)
	if u["email"] != "alice@example.com" || u["email_verified"] != true {
		t.Errorf("userinfo email block = %v/%v, want alice@example.com/true", u["email"], u["email_verified"])
	}
	if _, ok := userinfoClaims(a, []string{"openid"}, "", nil)["email"]; ok {
		t.Error("userinfo leaked email without the email scope")
	}
}

func TestClaimsIDTokenWithProfile(t *testing.T) {
	a := testAccount(t)
	in := baseInput()
	c := idTokenClaims(a, in)

	// Base claims.
	if c["iss"] != in.Issuer {
		t.Fatalf("iss = %v, want %v", c["iss"], in.Issuer)
	}
	if c["sub"] != subjectOf(a) {
		t.Fatalf("sub = %v, want %v", c["sub"], subjectOf(a))
	}
	if c["aud"] != "client-abc" {
		t.Fatalf("aud = %v, want client-abc (bare string)", c["aud"])
	}
	if c["exp"] != in.Expiry.Unix() {
		t.Fatalf("exp = %v, want %v", c["exp"], in.Expiry.Unix())
	}
	if c["iat"] != in.IssuedAt.Unix() {
		t.Fatalf("iat = %v, want %v", c["iat"], in.IssuedAt.Unix())
	}
	if c["auth_time"] != in.AuthTime.Unix() {
		t.Fatalf("auth_time = %v, want %v", c["auth_time"], in.AuthTime.Unix())
	}
	if c["sid"] != "sess-1" {
		t.Fatalf("sid = %v, want sess-1", c["sid"])
	}
	amr, ok := c["amr"].([]string)
	if !ok || len(amr) != 1 || amr[0] != "webauthn" {
		t.Fatalf("amr = %v, want [webauthn]", c["amr"])
	}
	if c["nonce"] != "nonce-123" {
		t.Fatalf("nonce = %v, want nonce-123", c["nonce"])
	}
	if c["acr"] != "urn:acr:high" {
		t.Fatalf("acr = %v, want urn:acr:high", c["acr"])
	}
	if h, ok := c["at_hash"].(string); !ok || len(h) != 22 {
		t.Fatalf("at_hash = %v, want 22-char string", c["at_hash"])
	}
	if _, ok := c["azp"]; ok {
		t.Fatalf("azp present, want absent for single audience: %v", c["azp"])
	}

	// Profile block.
	if c["username"] != "alice" {
		t.Fatalf("username = %v, want alice", c["username"])
	}
	if c["displayName"] != "Alice Example" {
		t.Fatalf("displayName = %v, want Alice Example", c["displayName"])
	}
	// OIDC-standard aliases emitted alongside the legacy keys.
	if c["preferred_username"] != "alice" {
		t.Fatalf("preferred_username = %v, want alice", c["preferred_username"])
	}
	if c["name"] != "Alice Example" {
		t.Fatalf("name = %v, want Alice Example", c["name"])
	}
	if c["role"] != "admin" {
		t.Fatalf("role = %v, want admin", c["role"])
	}
	attrs, ok := c["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes type = %T, want map[string]any", c["attributes"])
	}
	if attrs["team"] != "x" {
		t.Fatalf("attributes.team = %v, want x", attrs["team"])
	}
}

func TestClaimsIDTokenWithoutProfile(t *testing.T) {
	a := testAccount(t)
	in := baseInput()
	in.Scope = []string{"openid"}
	c := idTokenClaims(a, in)

	// Base claims still present.
	if c["sub"] != subjectOf(a) {
		t.Fatalf("sub missing without profile")
	}
	if c["iss"] != in.Issuer {
		t.Fatalf("iss missing without profile")
	}

	// Profile claims absent.
	for _, k := range []string{"username", "preferred_username", "displayName", "name", "role", "attributes"} {
		if _, ok := c[k]; ok {
			t.Fatalf("claim %q present without profile scope", k)
		}
	}
}

func TestClaimsIDTokenOmitsOptionalWhenEmpty(t *testing.T) {
	a := testAccount(t)
	in := baseInput()
	in.Nonce = ""
	in.ACR = ""
	in.AccessToken = ""
	c := idTokenClaims(a, in)

	if _, ok := c["nonce"]; ok {
		t.Fatalf("nonce present when empty")
	}
	if _, ok := c["acr"]; ok {
		t.Fatalf("acr present when empty")
	}
	if _, ok := c["at_hash"]; ok {
		t.Fatalf("at_hash present when access token empty")
	}
}

func TestClaimsIDTokenOmitsAttributesWhenEmpty(t *testing.T) {
	a := testAccount(t)
	a.Attributes = nil
	in := baseInput()
	c := idTokenClaims(a, in)

	if _, ok := c["attributes"]; ok {
		t.Fatalf("attributes present when account has none")
	}
	// Other profile claims still present.
	if c["username"] != "alice" {
		t.Fatalf("username missing")
	}
}

func TestClaimsUserinfoWithProfile(t *testing.T) {
	a := testAccount(t)
	c := userinfoClaims(a, []string{"openid", "profile"}, "", nil)

	if c["sub"] != subjectOf(a) {
		t.Fatalf("sub = %v, want %v", c["sub"], subjectOf(a))
	}
	if c["username"] != "alice" {
		t.Fatalf("username = %v, want alice", c["username"])
	}
	if c["displayName"] != "Alice Example" {
		t.Fatalf("displayName = %v, want Alice Example", c["displayName"])
	}
	// OIDC-standard aliases emitted alongside the legacy keys.
	if c["preferred_username"] != "alice" {
		t.Fatalf("preferred_username = %v, want alice", c["preferred_username"])
	}
	if c["name"] != "Alice Example" {
		t.Fatalf("name = %v, want Alice Example", c["name"])
	}
	if c["role"] != "admin" {
		t.Fatalf("role = %v, want admin", c["role"])
	}
	attrs, ok := c["attributes"].(map[string]any)
	if !ok || attrs["team"] != "x" {
		t.Fatalf("attributes = %v, want {team:x}", c["attributes"])
	}
	// No id_token-only base claims in userinfo.
	for _, k := range []string{"iss", "aud", "exp", "iat", "sid", "amr"} {
		if _, ok := c[k]; ok {
			t.Fatalf("userinfo unexpectedly has %q", k)
		}
	}
}

func TestClaimsUserinfoWithoutProfile(t *testing.T) {
	a := testAccount(t)
	c := userinfoClaims(a, []string{"openid"}, "", nil)

	if c["sub"] != subjectOf(a) {
		t.Fatalf("sub = %v, want %v", c["sub"], subjectOf(a))
	}
	if len(c) != 1 {
		t.Fatalf("userinfo without profile has %d claims, want 1 (sub only): %v", len(c), c)
	}
}

func TestProfileClaims_Picture(t *testing.T) {
	var u pgtype.UUID
	if err := u.Scan("11111111-2222-3333-4444-555555555555"); err != nil {
		t.Fatalf("scan uuid: %v", err)
	}
	a := db.Account{
		Username: "u", DisplayName: "U", Role: "user",
		OidcSubject: u,
		AvatarEtag:  pgtype.Text{String: "deadbeefcafe", Valid: true},
	}
	c := profileClaims(a, "https://auth.example.com")
	if c["picture"] != "https://auth.example.com/avatar/11111111-2222-3333-4444-555555555555?v=deadbeef" {
		t.Fatalf("picture = %v", c["picture"])
	}
	a.AvatarEtag = pgtype.Text{}
	if _, ok := profileClaims(a, "https://auth.example.com")["picture"]; ok {
		t.Fatal("picture must be absent without an avatar")
	}
}

// TestClaimsGroupsScope guards Task 8: the `groups` claim is emitted in both
// id_token and userinfo only when the `groups` scope is granted; it is a
// non-nil []string (serializes as []) when granted but the user has no groups.
func TestClaimsGroupsScope(t *testing.T) {
	a := testAccount(t)

	// --- id_token ---

	// groups scope + non-empty Groups → claim present with the slugs.
	in := baseInput()
	in.Scope = []string{"openid", "groups"}
	in.Groups = []string{"alpha", "beta"}
	c := idTokenClaims(a, in)
	gs, ok := c["groups"].([]string)
	if !ok {
		t.Fatalf("groups claim type = %T, want []string", c["groups"])
	}
	if len(gs) != 2 || gs[0] != "alpha" || gs[1] != "beta" {
		t.Errorf("groups = %v, want [alpha beta]", gs)
	}

	// groups scope + nil Groups → claim present as non-nil empty slice ([] not null).
	in.Groups = nil
	c = idTokenClaims(a, in)
	gs, ok = c["groups"].([]string)
	if !ok {
		t.Fatalf("groups claim (nil input) type = %T, want []string", c["groups"])
	}
	if gs == nil {
		t.Fatal("groups claim is nil, want non-nil empty slice")
	}
	if len(gs) != 0 {
		t.Errorf("groups len = %d, want 0", len(gs))
	}

	// groups scope + empty (non-nil) Groups → same non-nil empty slice.
	in.Groups = []string{}
	c = idTokenClaims(a, in)
	gs, ok = c["groups"].([]string)
	if !ok {
		t.Fatalf("groups claim (empty input) type = %T, want []string", c["groups"])
	}
	if gs == nil {
		t.Fatal("groups claim is nil, want non-nil empty slice")
	}

	// no groups scope → claim entirely absent.
	in.Scope = []string{"openid", "profile"}
	in.Groups = []string{"alpha"}
	c = idTokenClaims(a, in)
	if _, ok := c["groups"]; ok {
		t.Error("groups claim present without the groups scope")
	}

	// --- userinfo ---

	// groups scope + non-empty → present.
	u := userinfoClaims(a, []string{"openid", "groups"}, "", []string{"gamma"})
	ugs, ok := u["groups"].([]string)
	if !ok {
		t.Fatalf("userinfo groups type = %T, want []string", u["groups"])
	}
	if len(ugs) != 1 || ugs[0] != "gamma" {
		t.Errorf("userinfo groups = %v, want [gamma]", ugs)
	}

	// groups scope + nil → non-nil empty slice.
	u = userinfoClaims(a, []string{"openid", "groups"}, "", nil)
	ugs, ok = u["groups"].([]string)
	if !ok {
		t.Fatalf("userinfo groups (nil) type = %T, want []string", u["groups"])
	}
	if ugs == nil {
		t.Fatal("userinfo groups is nil, want non-nil empty slice")
	}
	if len(ugs) != 0 {
		t.Errorf("userinfo groups len = %d, want 0", len(ugs))
	}

	// no groups scope → absent.
	u = userinfoClaims(a, []string{"openid", "profile"}, "", []string{"gamma"})
	if _, ok := u["groups"]; ok {
		t.Error("userinfo groups present without the groups scope")
	}
}

// TestIDTokenPictureUsesAvatarOrigin asserts that when Issuer and AvatarOrigin
// differ (e.g. PROHIBITORUM_OIDC_ISSUER != public origin), the picture URL in
// the id_token is built from AvatarOrigin, not Issuer.
func TestIDTokenPictureUsesAvatarOrigin(t *testing.T) {
	var u pgtype.UUID
	if err := u.Scan("11111111-2222-3333-4444-555555555555"); err != nil {
		t.Fatalf("scan uuid: %v", err)
	}
	a := db.Account{
		Username: "u", DisplayName: "U", Role: "user",
		OidcSubject: u,
		AvatarEtag:  pgtype.Text{String: "deadbeefcafe", Valid: true},
	}
	in := baseInput()
	in.Issuer = "https://sso.internal"        // issuer may differ from public origin
	in.AvatarOrigin = "https://auth.example.com" // public origin where /avatar is served
	in.Scope = []string{"openid", "profile"}

	c := idTokenClaims(a, in)

	// iss must still use the issuer, not AvatarOrigin.
	if c["iss"] != "https://sso.internal" {
		t.Fatalf("iss = %v, want https://sso.internal", c["iss"])
	}
	// picture must use AvatarOrigin, not Issuer.
	wantPic := "https://auth.example.com/avatar/11111111-2222-3333-4444-555555555555?v=deadbeef"
	if c["picture"] != wantPic {
		t.Fatalf("picture = %v, want %v", c["picture"], wantPic)
	}
}
