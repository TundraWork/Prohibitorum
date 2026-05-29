package oidc

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"time"

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
// attributes (rather than emitting null). Shared by id_token and userinfo.
func profileClaims(a db.Account) map[string]any {
	c := map[string]any{
		"username":    a.Username,
		"displayName": a.DisplayName,
		"role":        a.Role,
	}
	if attrs := decodeAttributes(a.Attributes); attrs != nil {
		c["attributes"] = attrs
	}
	return c
}

// idTokenInput carries the per-issuance context for an ID token. It is kept
// separate so idTokenClaims can remain a pure, deterministic function: it
// never reads the clock — all times are passed in.
type idTokenInput struct {
	Issuer      string
	Audience    string   // client_id; single audience today
	Nonce       string   // optional; claim omitted if ""
	ACR         string   // optional; claim omitted if ""
	SID         string   // session id
	AMR         []string // authentication methods, e.g. ["webauthn"]
	AccessToken string   // to compute at_hash; at_hash omitted if ""
	Scope       []string // granted scopes; gates the profile block
	IssuedAt    time.Time
	Expiry      time.Time
	AuthTime    time.Time
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
		for k, v := range profileClaims(a) {
			c[k] = v
		}
	}

	return c
}

// userinfoClaims projects an account into the /userinfo response: always
// `sub`, plus the same profile block iff the `profile` scope was granted.
func userinfoClaims(a db.Account, scope []string) map[string]any {
	c := map[string]any{
		"sub": subjectOf(a),
	}
	if hasScope(scope, "profile") {
		for k, v := range profileClaims(a) {
			c[k] = v
		}
	}
	return c
}
