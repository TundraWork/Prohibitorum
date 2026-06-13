package oidc

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"time"

	"prohibitorum/pkg/avatar"
	"prohibitorum/pkg/db"
)

// subjectOf returns the canonical 36-char hyphenated UUID string of the
// account's oidc_subject. pgtype.UUID.String() yields the canonical form
// when Valid, and "" when not.
func subjectOf(a db.Account) string {
	return a.OidcSubject.String()
}

// hasScope reports whether want is among the granted scopes.
func hasScope(granted []string, want string) bool {
	for _, s := range granted {
		if s == want {
			return true
		}
	}
	return false
}

// atHash computes the OIDC `at_hash` claim (Core §3.1.3.6) for an RS256
// id_token: base64url (no padding) of the left-most half of the SHA-256
// of the ASCII access token. For SHA-256 that's the first 16 bytes,
// yielding a 22-char value.
func atHash(accessToken string) string {
	sum := sha256.Sum256([]byte(accessToken))
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}

// decodeAttributes decodes the JSONB attributes blob into a map. Returns
// nil for empty/invalid input so callers can omit the claim entirely.
func decodeAttributes(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

// profileClaims returns the scope-gated profile claim block for an account.
// The `attributes` claim is omitted entirely when the account has no
// attributes (rather than emitting null). The `picture` claim is included when
// the account has an avatar and origin is non-empty. Shared by id_token and
// userinfo.
func profileClaims(a db.Account, origin string) map[string]any {
	c := map[string]any{
		"username":    a.Username,
		"displayName": a.DisplayName,
		"role":        a.Role,
	}
	if attrs := decodeAttributes(a.Attributes); attrs != nil {
		c["attributes"] = attrs
	}
	if pic := avatar.AccountURL(a, origin); pic != "" {
		c["picture"] = pic
	}
	return c
}

// idTokenInput carries the per-issuance context for an ID token. It is kept
// separate so idTokenClaims can remain a pure, deterministic function: it
// never reads the clock — all times are passed in.
type idTokenInput struct {
	Issuer       string
	AvatarOrigin string   // public origin for picture URL (may differ from Issuer)
	Audience     string   // client_id; single audience today
	Nonce        string   // optional; claim omitted if ""
	ACR          string   // optional; claim omitted if ""
	SID          string   // session id
	AMR          []string // authentication methods, e.g. ["webauthn"]
	AccessToken  string   // to compute at_hash; at_hash omitted if ""
	Scope        []string // granted scopes; gates the profile block
	IssuedAt     time.Time
	Expiry       time.Time
	AuthTime     time.Time
}

// idTokenClaims projects an account + issuance context into the ID-token
// claim set. Pure and deterministic. Timestamps are emitted as Unix seconds
// (JWT NumericDate). Profile claims are included only when the `profile`
// scope is granted.
func idTokenClaims(a db.Account, in idTokenInput) map[string]any {
	c := map[string]any{
		"iss":       in.Issuer,
		"sub":       subjectOf(a),
		"aud":       in.Audience, // bare string for a single audience
		"exp":       in.Expiry.Unix(),
		"iat":       in.IssuedAt.Unix(),
		"auth_time": in.AuthTime.Unix(),
		"sid":       in.SID,
		"amr":       in.AMR,
	}

	if in.Nonce != "" {
		c["nonce"] = in.Nonce
	}
	if in.ACR != "" {
		c["acr"] = in.ACR
	}
	if in.AccessToken != "" {
		c["at_hash"] = atHash(in.AccessToken)
	}
	// azp is required only when there is more than one audience. Single-aud
	// today, so this never fires — but the conditional is correct for the
	// day multi-aud arrives.
	// (Audience is a single string here; no multi-aud path to set azp.)

	if hasScope(in.Scope, "profile") {
		for k, v := range profileClaims(a, in.AvatarOrigin) {
			c[k] = v
		}
	}
	if hasScope(in.Scope, "email") {
		for k, v := range emailClaims(a) {
			c[k] = v
		}
	}

	return c
}

// userinfoClaims projects an account into the /userinfo response: always
// `sub`, plus the profile block iff `profile` was granted and the email block
// iff `email` was granted. origin is the public origin where the avatar
// endpoint is served (cfg.PublicOrigins[0]); it is forwarded to profileClaims
// to build the picture URL.
func userinfoClaims(a db.Account, scope []string, origin string) map[string]any {
	c := map[string]any{
		"sub": subjectOf(a),
	}
	if hasScope(scope, "profile") {
		for k, v := range profileClaims(a, origin) {
			c[k] = v
		}
	}
	if hasScope(scope, "email") {
		for k, v := range emailClaims(a) {
			c[k] = v
		}
	}
	return c
}

// emailClaims returns the OIDC `email` / `email_verified` claim block (Core
// §5.4), gated on the `email` scope by the caller. Returns nil when the account
// has no email on file (so both claims are omitted rather than emitted empty);
// ranging over a nil map is a no-op. email_verified reflects whether the address
// was asserted by a verified upstream vs set manually.
func emailClaims(a db.Account) map[string]any {
	if !a.Email.Valid || a.Email.String == "" {
		return nil
	}
	return map[string]any{
		"email":          a.Email.String,
		"email_verified": a.EmailVerified,
	}
}
