package session

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
)

// AccountRefQueries is the instance_settings surface AccountRefs needs.
type AccountRefQueries interface {
	GetAccountRefKey(ctx context.Context) ([]byte, error)
	InitAccountRefKey(ctx context.Context, accountRefKey []byte) error
}

// AccountRefs computes the short account reference `u` that a tab carries in
// its URL. It is an HMAC of the account ID so the address bar and history
// never show the ID, and every device computes the same value for an account.
// The server only ever issues these; it never resolves one back to an account.
type AccountRefs struct {
	key []byte
}

const accountRefKeyLen = 32

// LoadAccountRefs reads the instance's reference key, creating it on first
// start. Instances that start together each try the conditional update and
// then read back whichever key won, so they all end up with the same one.
func LoadAccountRefs(ctx context.Context, q AccountRefQueries) (*AccountRefs, error) {
	key, err := q.GetAccountRefKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("session: read account ref key: %w", err)
	}
	if len(key) == 0 {
		fresh := make([]byte, accountRefKeyLen)
		if _, err := rand.Read(fresh); err != nil {
			return nil, fmt.Errorf("session: account ref key: %w", err)
		}
		if err := q.InitAccountRefKey(ctx, fresh); err != nil {
			return nil, fmt.Errorf("session: init account ref key: %w", err)
		}
		if key, err = q.GetAccountRefKey(ctx); err != nil {
			return nil, fmt.Errorf("session: read account ref key: %w", err)
		}
	}
	if len(key) == 0 {
		return nil, errors.New("session: account ref key missing after init")
	}
	return NewAccountRefs(key), nil
}

// NewAccountRefs builds references from a known key (tests, openapi).
func NewAccountRefs(key []byte) *AccountRefs {
	return &AccountRefs{key: append([]byte(nil), key...)}
}

// Full returns the 64-hex-digit reference for accountID.
func (a *AccountRefs) Full(accountID int32) string {
	mac := hmac.New(sha256.New, a.key)
	mac.Write([]byte("account-ref:" + strconv.FormatInt(int64(accountID), 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

// Shorts returns the `u` for each account in ids: the first 4 hex digits, or
// the first 8 for accounts whose 4-digit form collides with another account
// in the same set. The set is one browser's accounts, so an old 4-digit link
// still matches both colliding accounts (and lands on the chooser), while the
// 8-digit form picks one.
func (a *AccountRefs) Shorts(ids []int32) map[int32]string {
	full := make(map[int32]string, len(ids))
	count := make(map[string]int, len(ids))
	for _, id := range ids {
		if _, seen := full[id]; seen {
			continue
		}
		f := a.Full(id)
		full[id] = f
		count[f[:4]]++
	}
	out := make(map[int32]string, len(full))
	for id, f := range full {
		if count[f[:4]] > 1 {
			out[id] = f[:8]
		} else {
			out[id] = f[:4]
		}
	}
	return out
}
