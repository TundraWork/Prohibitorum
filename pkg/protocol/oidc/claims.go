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
		// OIDC-standard `profile`-scope claims so any RP (including prohibitorum
		// federating to itself) can read the user's handle and name. The legacy
		// camelCase keys are kept alongside for first-party consumers that already
		// read them.
		"preferred_username": a.Username,
		"name":               a.DisplayName,
		"username":           a.Username,
		"displayName":        a.DisplayName,
		"role":               a.Role,
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
	AvatarOrigin string // public origin for picture URL (may differ from Issuer)
	Audience     string // client_id; single audience today
	Subject      string // public subject computed from the client's current setting
	AccountID    int32  // private local lookup key; never exposed by userinfo
	ClaimAliases map[string]string
	Nonce        string                     // optional; claim omitted if ""
	ACR          string                     // optional; claim omitted if ""
	SID          string                     // session id
	AMR          []string                   // authentication methods, e.g. ["webauthn"]
	AccessToken  string                     // to compute at_hash; at_hash omitted if ""
	Scope        []string                   // granted scopes; gates the profile block
	PrimaryEmail string                     // primary address; "" → email claims omitted
	Groups       []string                   // exposed group slugs; emitted only when "groups" scope granted
	Identities   map[string][]identityClaim // emitted when any identity:<slug> scope was granted (nil otherwise)
	IssuedAt     time.Time
	Expiry       time.Time
	AuthTime     time.Time
}

// idTokenClaims projects an account + issuance context into the ID-token
// claim set. Pure and deterministic. Timestamps are emitted as Unix seconds
// (JWT NumericDate). Profile claims are included only when the `profile`
// scope is granted.
func idTokenClaims(a db.Account, in idTokenInput) map[string]any {
	subject := in.Subject
	if subject == "" {
		subject = subjectOf(a)
	}
	c := map[string]any{
		"iss":       in.Issuer,
		"sub":       subject,
		"aud":       in.Audience, // bare string for a single audience
		"exp":       in.Expiry.Unix(),
		"iat":       in.IssuedAt.Unix(),
		"auth_time": in.AuthTime.Unix(),
		"sid":       in.SID,
		"amr":       in.AMR,
	}
	if in.AccountID > 0 {
		c[internalAccountIDClaim] = in.AccountID
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
		for k, v := range emailClaims(in.PrimaryEmail) {
			c[k] = v
		}
	}
	if hasScope(in.Scope, "groups") {
		c["groups"] = nonNilSlugs(in.Groups)
	}
	if in.Identities != nil {
		c["identities"] = in.Identities
	}
	applyClaimAliases(c, a, in.PrimaryEmail, in.Scope, in.AvatarOrigin, in.ClaimAliases)

	return c
}

// userinfoClaims projects an account into the /userinfo response: always
// `sub`, plus the profile block iff `profile` was granted, the email block
// iff `email` was granted, and the groups block iff `groups` was granted.
// origin is the public origin where the avatar endpoint is served
// (cfg.PublicOrigins[0]); it is forwarded to profileClaims to build the
// picture URL. groups is the caller-supplied list of exposed group slugs
// (nil when the groups scope was not granted).
func userinfoClaims(a db.Account, primaryEmail string, scope []string, origin string, groups []string) map[string]any {
	return projectedUserinfoClaims(a, subjectOf(a), primaryEmail, scope, origin, groups, nil, nil)
}

// projectedUserinfoClaims adds identities when it is non-nil, i.e. when an
// identity:<slug> scope was granted.
func projectedUserinfoClaims(a db.Account, subject string, primaryEmail string, scope []string, origin string, groups []string, identities map[string][]identityClaim, aliases map[string]string) map[string]any {
	c := map[string]any{
		"sub": subject,
	}
	if hasScope(scope, "profile") {
		for k, v := range profileClaims(a, origin) {
			c[k] = v
		}
	}
	if hasScope(scope, "email") {
		for k, v := range emailClaims(primaryEmail) {
			c[k] = v
		}
	}
	if hasScope(scope, "groups") {
		c["groups"] = nonNilSlugs(groups)
	}
	if identities != nil {
		c["identities"] = identities
	}
	applyClaimAliases(c, a, primaryEmail, scope, origin, aliases)
	return c
}

// nonNilSlugs returns a non-nil empty slice so the claim serializes as [] not null.
func nonNilSlugs(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// emailClaims returns the OIDC `email` / `email_verified` claim block (Core
// §5.4), gated on the `email` scope by the caller. primary is the account's
// primary address ("" when none); a primary is verified by construction, so
// email_verified is always true when the block is emitted. No primary →
// both claims omitted rather than emitted empty; ranging over a nil map is
// a no-op.
func emailClaims(primary string) map[string]any {
	if primary == "" {
		return nil
	}
	return map[string]any{
		"email":          primary,
		"email_verified": true,
	}
}
