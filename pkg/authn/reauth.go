package authn

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"prohibitorum/pkg/kv"
)

// ReauthMarkerTTL bounds how long a forced-re-auth demand stays valid. The user
// must complete the login bounce + return within this window.
const ReauthMarkerTTL = 10 * time.Minute

// DemandReauth records a forced-re-authentication demand: it mints a single-use
// nonce and stamps the demand instant in KV under keyPrefix+nonce. The caller
// embeds the returned nonce in the login return_to; ConsumeReauth verifies it on
// return. keyPrefix namespaces the two protocols (e.g. "oidc:reauth:" /
// "saml:reauth:").
func DemandReauth(ctx context.Context, store kv.Store, keyPrefix string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(buf)
	if err := store.SetEx(ctx, keyPrefix+nonce, time.Now().UTC().Format(time.RFC3339Nano), ReauthMarkerTTL); err != nil {
		return "", err
	}
	return nonce, nil
}

// ConsumeReauth verifies (single-use) that the session's authTime post-dates the
// demand recorded under keyPrefix+nonce. It deletes the marker regardless of the
// outcome (single-use). Returns false (not an error) when the marker is missing
// or expired, or when authTime predates the demand — the caller then re-demands.
func ConsumeReauth(ctx context.Context, store kv.Store, keyPrefix, nonce string, authTime time.Time) (bool, error) {
	if nonce == "" {
		return false, nil
	}
	key := keyPrefix + nonce
	val, err := store.Get(ctx, key)
	if errors.Is(err, kv.ErrKeyNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if derr := store.Del(ctx, key); derr != nil { // single-use: consume before evaluating
		return false, derr
	}
	demandedAt, perr := time.Parse(time.RFC3339Nano, val)
	if perr != nil {
		return false, nil // corrupt marker → unsatisfied; caller re-demands
	}
	return !authTime.Before(demandedAt), nil
}
