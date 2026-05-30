package authn

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"prohibitorum/pkg/kv"
)

// ReauthMarkerTTL bounds how long a forced-re-auth demand stays valid. The user
// must complete the login bounce + return within this window.
const ReauthMarkerTTL = 10 * time.Minute

// DemandReauth records a forced-re-authentication demand: it mints a single-use
// nonce and stamps the demanding accountID + demand instant in KV under
// keyPrefix+nonce. The caller embeds the returned nonce in the login return_to;
// ConsumeReauth verifies it on return. Binding the marker to accountID prevents a
// leaked nonce from being satisfied by any fresh session — only the demanding
// account's session can consume it. keyPrefix namespaces the two protocols (e.g.
// "oidc:reauth:" / "saml:reauth:"). The stored value is "<accountID>|<instant>".
func DemandReauth(ctx context.Context, store kv.Store, keyPrefix string, accountID int32) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(buf)
	val := fmt.Sprintf("%d|%s", accountID, time.Now().UTC().Format(time.RFC3339Nano))
	if err := store.SetEx(ctx, keyPrefix+nonce, val, ReauthMarkerTTL); err != nil {
		return "", err
	}
	return nonce, nil
}

// ConsumeReauth verifies (single-use) that the marker recorded under
// keyPrefix+nonce belongs to accountID and that the session's authTime post-dates
// the demand instant. It atomically pops the marker (get-and-delete) so a
// concurrent return trip cannot satisfy the same demand twice. Returns false (not
// an error) when the marker is missing/expired, belongs to a different account, is
// malformed, or when authTime predates the demand — the caller then re-demands.
func ConsumeReauth(ctx context.Context, store kv.Store, keyPrefix, nonce string, accountID int32, authTime time.Time) (bool, error) {
	if nonce == "" {
		return false, nil
	}
	key := keyPrefix + nonce
	val, err := store.Pop(ctx, key) // atomic single-use consume
	if errors.Is(err, kv.ErrKeyNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	sep := strings.IndexByte(val, '|')
	if sep < 0 {
		return false, nil // malformed marker → unsatisfied; caller re-demands
	}
	storedAcct, perr := strconv.ParseInt(val[:sep], 10, 32)
	if perr != nil {
		return false, nil // malformed marker → unsatisfied
	}
	if int32(storedAcct) != accountID {
		return false, nil // bound to a different account → unsatisfied (already consumed)
	}
	demandedAt, perr := time.Parse(time.RFC3339Nano, val[sep+1:])
	if perr != nil {
		return false, nil // corrupt marker → unsatisfied; caller re-demands
	}
	return !authTime.Before(demandedAt), nil
}
