package oidc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"prohibitorum/pkg/kv"
)

// RefreshTokenTTL bounds the lifetime of a refresh token (and its family
// record) in KV. 30 days is a conventional refresh lifetime; rotation slides
// this window forward on each successful exchange so an actively-used family
// persists, while an abandoned one expires.
const RefreshTokenTTL = 30 * 24 * time.Hour

// errRefreshInvalid is returned when a presented refresh token does not resolve
// to a live family — it never existed, expired, or the family was revoked
// (explicitly or by a reuse-detection trip). Callers map this to the OAuth
// invalid_grant error.
var errRefreshInvalid = errors.New("oidc: refresh token invalid")

// errRefreshReuse is returned when a superseded (already-rotated) refresh token
// is presented. This is the reuse-detection trip: the entire family is revoked
// as a side effect, defeating a stolen-token replay (OAuth 2.0 Security BCP
// §4.13.2). Callers map this to invalid_grant.
var errRefreshReuse = errors.New("oidc: refresh token reuse detected")

// refreshFamily is the per-family record for a chain of rotated refresh tokens.
// CurrentToken names the single token that may be exchanged; every prior token
// in the chain resolves to this family but, being != CurrentToken, trips reuse
// detection if presented. The remaining fields are the authentication snapshot
// carried forward into each refreshed access/ID token.
type refreshFamily struct {
	FamilyID     string    `json:"family_id"`
	CurrentToken string    `json:"current_token"`
	ClientID     string    `json:"client_id"`
	AccountID    int32     `json:"account_id"`
	SessionID    string    `json:"session_id"`
	Scope        []string  `json:"scope"`
	AuthTime     time.Time `json:"auth_time"`
	AMR          []string  `json:"amr"`
	ACR          string    `json:"acr"`
	IssuedAt     time.Time `json:"issued_at"`
}

// refreshFamilyKey is the KV key under which a family record is stored.
func refreshFamilyKey(fid string) string { return "oidc:refresh:family:" + fid }

// refreshTokenKey is the KV key under which a token→family-id mapping is stored.
func refreshTokenKey(token string) string { return "oidc:refresh:" + token }

// randToken returns 32 bytes of cryptographic randomness, base64url-encoded
// without padding so it is URL-safe. Used for both opaque refresh tokens and
// family identifiers. (Kept local to refresh.go to keep this change scoped;
// codes.go inlines the equivalent.)
func randToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// putFamily writes the family record (JSON) and a token→family-id mapping for
// fam.CurrentToken, both with TTL RefreshTokenTTL. It is used both to seed a
// new family and to extend (slide) an existing one on rotation.
func putFamily(ctx context.Context, store kv.Store, fam *refreshFamily) error {
	payload, err := json.Marshal(fam)
	if err != nil {
		return fmt.Errorf("oidc: marshal refresh family: %w", err)
	}
	if err := store.SetEx(ctx, refreshFamilyKey(fam.FamilyID), string(payload), RefreshTokenTTL); err != nil {
		return err
	}
	if err := store.SetEx(ctx, refreshTokenKey(fam.CurrentToken), fam.FamilyID, RefreshTokenTTL); err != nil {
		return err
	}
	return nil
}

// loadFamily resolves a token→family mapping and loads the named family record.
// A miss on either lookup (token unknown/expired, or family revoked/expired)
// returns errRefreshInvalid; a malformed family payload is a wrapped decode
// error, distinct from a miss, since it signals corruption rather than reuse.
func loadFamily(ctx context.Context, store kv.Store, presented string) (*refreshFamily, error) {
	fid, err := store.Get(ctx, refreshTokenKey(presented))
	if err != nil {
		if errors.Is(err, kv.ErrKeyNotFound) {
			return nil, errRefreshInvalid
		}
		return nil, err
	}
	raw, err := store.Get(ctx, refreshFamilyKey(fid))
	if err != nil {
		if errors.Is(err, kv.ErrKeyNotFound) {
			return nil, errRefreshInvalid
		}
		return nil, err
	}
	var fam refreshFamily
	if err := json.Unmarshal([]byte(raw), &fam); err != nil {
		return nil, fmt.Errorf("oidc: unmarshal refresh family: %w", err)
	}
	return &fam, nil
}

// issueRefresh seeds a NEW refresh-token family. It takes fam by value,
// generates a random FamilyID and an opaque CurrentToken, stamps IssuedAt, and
// writes both the family record (oidc:refresh:family:<fid>) and the
// token→family mapping (oidc:refresh:<token>) with TTL RefreshTokenTTL. It
// returns the freshly minted token and the generated family id. The caller need
// not pre-populate FamilyID/CurrentToken/IssuedAt; they are set here.
func issueRefresh(ctx context.Context, store kv.Store, fam refreshFamily) (token string, familyID string, err error) {
	fid, err := randToken()
	if err != nil {
		return "", "", fmt.Errorf("oidc: generate refresh family id: %w", err)
	}
	token, err = randToken()
	if err != nil {
		return "", "", fmt.Errorf("oidc: generate refresh token: %w", err)
	}
	fam.FamilyID = fid
	fam.CurrentToken = token
	fam.IssuedAt = time.Now().UTC()

	if err := putFamily(ctx, store, &fam); err != nil {
		return "", "", err
	}
	return token, fid, nil
}

// rotateRefresh performs a single-use exchange of a refresh token.
//
//   - The presented token is resolved to its family; a miss (unknown/expired
//     token, or revoked/expired family) returns errRefreshInvalid.
//   - If the presented token is NOT the family's CurrentToken it is a
//     superseded token — REUSE. The family record is deleted (revoking the
//     whole chain) and errRefreshReuse is returned.
//   - Otherwise a new opaque token is minted, set as CurrentToken, and the
//     family record is rewritten with a fresh sliding TTL alongside the new
//     token→family mapping. The updated family and new token are returned.
//
// On a successful rotation the OLD token mapping is deliberately NOT deleted:
// it remains resolvable so a later replay trips the reuse branch above. It
// expires naturally at its own original TTL.
func rotateRefresh(ctx context.Context, store kv.Store, presented string) (*refreshFamily, string, error) {
	// NOTE: The Get→compare→SetEx sequence below is NOT atomic across KV ops.
	// Two concurrent rotations presenting the same current token could both pass
	// the presented == CurrentToken check. This race is accepted: a legitimate
	// client never rotates concurrently, and an attacker holding the current
	// token gains nothing — a fully atomic compare-and-swap would require a
	// Redis WATCH/Lua primitive that the kv.Store interface does not expose.
	fam, err := loadFamily(ctx, store, presented)
	if err != nil {
		return nil, "", err
	}

	if presented != fam.CurrentToken {
		// Superseded token replayed: revoke the entire family.
		if delErr := store.Del(ctx, refreshFamilyKey(fam.FamilyID)); delErr != nil {
			return nil, "", delErr
		}
		return nil, "", errRefreshReuse
	}

	newToken, err := randToken()
	if err != nil {
		return nil, "", fmt.Errorf("oidc: generate refresh token: %w", err)
	}
	fam.CurrentToken = newToken
	if err := putFamily(ctx, store, fam); err != nil {
		return nil, "", err
	}
	return fam, newToken, nil
}

// lookupRefresh resolves a presented token to its family WITHOUT mutating
// anything — no rotation, no revocation, no TTL change. It returns
// (family, true) when both the token mapping and family record resolve, else
// (nil, false). Used by /introspect and /revoke, which must not consume the
// token. Note this does not check presented == CurrentToken; a superseded
// token still resolves to its (live) family here.
func lookupRefresh(ctx context.Context, store kv.Store, presented string) (*refreshFamily, bool) {
	fam, err := loadFamily(ctx, store, presented)
	if err != nil {
		return nil, false
	}
	return fam, true
}

// revokeFamily deletes a family record by id, invalidating every token in the
// chain (subsequent rotate/lookup of any of them resolves the token mapping but
// misses the family → errRefreshInvalid / false). Deleting an absent family is
// a no-op.
func revokeFamily(ctx context.Context, store kv.Store, familyID string) error {
	return store.Del(ctx, refreshFamilyKey(familyID))
}
