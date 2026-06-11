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

// AuthorizationCodeTTL is the DEFAULT lifetime of an authorization code in KV,
// used when oidc.authorization_code_ttl is unset. Codes are exchanged
// immediately at the token endpoint; RFC 6749 §4.1.2 recommends a short
// lifetime (≤10 minutes), and since these are strictly single-use a tight 60s
// window further limits the interception surface. Callers pass the effective
// TTL (p.authCodeTTL()) into mintCode; never read this const directly at a mint
// site.
const AuthorizationCodeTTL = 60 * time.Second

// usedMarkerGrace is how long the replay marker outlives the code's own TTL, so
// a replay arriving just after the code expires is still recognized as a reuse
// attempt rather than an unknown code.
const usedMarkerGrace = 30 * time.Second

// errCodeNotFound is returned when an authorization code is absent from KV —
// either it never existed, it expired, or (single-use) it was already
// consumed. Callers map this to the OAuth invalid_grant error.
var errCodeNotFound = errors.New("oidc: authorization code not found")

// authCode is the state captured at the /authorize step and replayed into the
// token endpoint when the code is exchanged. It is JSON-serialized into KV
// under codeKey and consumed atomically exactly once.
type authCode struct {
	ClientID            string    `json:"client_id"`
	AccountID           int32     `json:"account_id"`
	SessionID           string    `json:"session_id"`
	RedirectURI         string    `json:"redirect_uri"`
	Scope               []string  `json:"scope"`
	Nonce               string    `json:"nonce"`
	CodeChallenge       string    `json:"code_challenge"`
	CodeChallengeMethod string    `json:"code_challenge_method"`
	AuthTime            time.Time `json:"auth_time"`
	AMR                 []string  `json:"amr"`
	ACR                 string    `json:"acr"`
}

// codeKey is the KV key under which a live authorization code is stored.
func codeKey(code string) string { return "oidc:code:" + code }

// usedKey is the KV key under which the replay marker (the refresh family the
// code minted) is recorded after the code is consumed.
func usedKey(code string) string { return "oidc:code:used:" + code }

// mintCode generates a fresh single-use authorization code, JSON-encodes the
// authCode state, and stores it under codeKey with the given ttl (the caller
// passes the effective p.authCodeTTL()). The code is 32 bytes of cryptographic
// randomness, base64url-encoded without padding so it is URL-safe.
func mintCode(ctx context.Context, store kv.Store, ac authCode, ttl time.Duration) (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("oidc: generate authorization code: %w", err)
	}
	code := base64.RawURLEncoding.EncodeToString(buf[:])

	payload, err := json.Marshal(ac)
	if err != nil {
		return "", fmt.Errorf("oidc: marshal authorization code: %w", err)
	}
	if err := store.SetEx(ctx, codeKey(code), string(payload), ttl); err != nil {
		return "", err
	}
	return code, nil
}

// consumeCode atomically retrieves and removes an authorization code,
// returning the decoded authCode. The atomic Pop enforces single-use: a
// second consume of the same code misses and returns errCodeNotFound. A
// malformed stored payload is returned as a wrapped error, distinct from a
// not-found miss, since it signals data corruption rather than reuse.
func consumeCode(ctx context.Context, store kv.Store, code string) (*authCode, error) {
	val, err := store.Pop(ctx, codeKey(code))
	if err != nil {
		if errors.Is(err, kv.ErrKeyNotFound) {
			return nil, errCodeNotFound
		}
		return nil, err
	}
	var ac authCode
	if err := json.Unmarshal([]byte(val), &ac); err != nil {
		return nil, fmt.Errorf("oidc: unmarshal authorization code: %w", err)
	}
	return &ac, nil
}

// markCodeUsed records that a consumed code minted the given refresh family,
// keyed by the code. If the code is later replayed, usedFamily surfaces this
// family so the whole token family can be revoked. The marker outlives the
// code (codeTTL + usedMarkerGrace) to catch replays arriving just after the
// code expires; codeTTL is the same effective TTL the code was minted with.
func markCodeUsed(ctx context.Context, store kv.Store, code, familyID string, codeTTL time.Duration) error {
	return store.SetEx(ctx, usedKey(code), familyID, codeTTL+usedMarkerGrace)
}

// usedFamily reports whether a code has already been consumed and, if so, the
// refresh family it minted. ok is false when no marker exists.
func usedFamily(ctx context.Context, store kv.Store, code string) (string, bool) {
	val, err := store.Get(ctx, usedKey(code))
	if err != nil {
		return "", false
	}
	return val, true
}
