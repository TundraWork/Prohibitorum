package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"prohibitorum/pkg/kv"
)

const ForwardAuthPathPrefix = "/.prohibitorum-forward-auth"
const forwardAuthCookieBase = "prohibitorum_forward_auth"

// faSession is the payload stored in KV for a forward-auth session cookie.
type faSession struct {
	AccountID int32  `json:"account_id"`
	ClientID  string `json:"client_id"`
}

func faSessionKey(token string) string { return "fa:session:" + token }

// mintFASession stores a forward-auth session in KV and returns the opaque token.
func mintFASession(ctx context.Context, store kv.Store, s faSession, ttl time.Duration) (string, error) {
	tok, err := faRandToken()
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	if err := store.SetEx(ctx, faSessionKey(tok), string(payload), ttl); err != nil {
		return "", err
	}
	return tok, nil
}

// loadFASession retrieves a forward-auth session from KV. Returns nil when the
// token is absent, expired, or malformed.
func loadFASession(ctx context.Context, store kv.Store, token string) *faSession {
	if token == "" {
		return nil
	}
	raw, err := store.Get(ctx, faSessionKey(token))
	if err != nil || raw == "" {
		return nil
	}
	var s faSession
	if json.Unmarshal([]byte(raw), &s) != nil {
		return nil
	}
	return &s
}

// faState is the payload stored in KV for a single-use OAuth2 state parameter,
// carrying the original request URL plus the PKCE verifier.
type faState struct {
	OriginalURL string `json:"original_url"`
	ClientID    string `json:"client_id"`
	Verifier    string `json:"verifier"`
}

func faStateKey(id string) string { return "fa:state:" + id }

// mintFAState stores a single-use forward-auth state in KV and returns the
// opaque state ID.
func mintFAState(ctx context.Context, store kv.Store, s faState, ttl time.Duration) (string, error) {
	id, err := faRandToken()
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	if err := store.SetEx(ctx, faStateKey(id), string(payload), ttl); err != nil {
		return "", err
	}
	return id, nil
}

// popFAState atomically retrieves and removes a forward-auth state from KV,
// enforcing single-use. Returns nil when the state is absent or malformed.
func popFAState(ctx context.Context, store kv.Store, id string) *faState {
	if id == "" {
		return nil
	}
	raw, err := store.Pop(ctx, faStateKey(id))
	if err != nil || raw == "" {
		return nil
	}
	var s faState
	if json.Unmarshal([]byte(raw), &s) != nil {
		return nil
	}
	return &s
}

// faRandToken returns 32 bytes of cryptographic randomness encoded as
// base64url without padding — the same approach used by mintCode in codes.go.
func faRandToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// pkceChallengeS256 computes the RFC 7636 S256 code challenge for a verifier.
// The vector in Appendix B:
//
//	verifier  = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
//	challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
func pkceChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// faCookieName returns the cookie name for forward-auth. When secure is true
// the __Host- prefix is used (host-only binding per RFC 6265bis).
func faCookieName(secure bool) string {
	if secure {
		return "__Host-" + forwardAuthCookieBase
	}
	return forwardAuthCookieBase
}

// faCookie builds a forward-auth session cookie. When secure is true the
// __Host- prefix and Secure flag are set; Domain is always empty (host-only).
func faCookie(secure bool, token string) *http.Cookie {
	return &http.Cookie{
		Name:     faCookieName(secure),
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		// Domain intentionally omitted for host-only binding.
	}
}

// writeIdentityHeaders sets the Traefik/nginx ForwardAuth identity headers on w.
// Remote-Email is omitted when email is empty.
func writeIdentityHeaders(w http.ResponseWriter, user, name, email string, groups []string) {
	w.Header().Set("Remote-User", user)
	w.Header().Set("Remote-Name", name)
	if email != "" {
		w.Header().Set("Remote-Email", email)
	}
	w.Header().Set("Remote-Groups", strings.Join(groups, ","))
}
