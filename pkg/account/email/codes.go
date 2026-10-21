// codes.go — six-digit verification codes stored in KV, keyed by email id.
// The stored value is JSON {accountId, addressLower, hash, attempts}; the
// code itself never lands in KV in the clear (hash = SHA-256(emailID ‖ ":"
// ‖ code)). Every submission first claims an attempt slot through a
// byte-exact CAS increment of attempts, and only the slot winner compares
// its guess; a correct guess consumes the record via CompareAndDelete on the
// post-increment value. A submission that loses the slot (a rival incremented
// or the record vanished) never compares, so the attempt budget cannot be
// raced past by concurrent submissions.
package email

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"prohibitorum/pkg/kv"
)

// CodeTTL is how long a code stays valid.
const CodeTTL = 15 * time.Minute

// MaxCodeAttempts bounds wrong guesses per code.
const MaxCodeAttempts = 5

// VerifyResult classifies a Verify call.
type VerifyResult int

const (
	VerifyOK VerifyResult = iota
	// VerifyInvalid: wrong code; attempts remain.
	VerifyInvalid
	// VerifyExpired: no code, expired, or attempts exhausted. The caller
	// tells the user to resend.
	VerifyExpired
)

var codeDigits = big.NewInt(10)

type codeRecord struct {
	AccountID    int32  `json:"accountId"`
	AddressLower string `json:"addressLower"`
	Hash         string `json:"hash"`
	Attempts     int    `json:"attempts"`
}

func codeKey(emailID int64) string { return fmt.Sprintf("email-code:%d", emailID) }

func hashCode(emailID int64, code string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", emailID, code)))
	return hex.EncodeToString(sum[:])
}

func randomCode() (string, error) {
	s := make([]byte, 6)
	for i := range s {
		n, err := rand.Int(rand.Reader, codeDigits)
		if err != nil {
			return "", err
		}
		s[i] = byte('0' + n.Int64())
	}
	return string(s), nil
}

// Issue stores a fresh code for the address, replacing (and thereby
// invalidating) any previous one, and returns the plaintext for the mail.
func Issue(ctx context.Context, store kv.Store, emailID int64, accountID int32, addressLower string) (string, error) {
	code, err := randomCode()
	if err != nil {
		return "", fmt.Errorf("email: random code: %w", err)
	}
	rec := codeRecord{AccountID: accountID, AddressLower: addressLower, Hash: hashCode(emailID, code)}
	raw, err := json.Marshal(rec)
	if err != nil {
		return "", fmt.Errorf("email: marshal code record: %w", err)
	}
	if err := store.SetEx(ctx, codeKey(emailID), string(raw), CodeTTL); err != nil {
		return "", fmt.Errorf("email: store code: %w", err)
	}
	return code, nil
}

// Drop removes the code (used when the mail failed to send).
func Drop(ctx context.Context, store kv.Store, emailID int64) {
	_ = store.Del(ctx, codeKey(emailID)) //nolint:errcheck // best-effort invalidation
}

// Verify checks a submitted code. Each call must first win an attempt slot:
// a byte-exact CAS increments Attempts, and a caller whose CAS lost (a rival
// incremented or the record vanished) reports expired without ever comparing
// its guess. Only the slot winner compares; a correct guess deletes the
// post-increment record, so of N concurrent correct submissions exactly one
// succeeds. attemptsRemaining is meaningful only for VerifyInvalid.
func Verify(ctx context.Context, store kv.Store, emailID int64, accountID int32, code string) (VerifyResult, int, error) {
	raw, err := store.Get(ctx, codeKey(emailID))
	if err != nil {
		if errors.Is(err, kv.ErrKeyNotFound) {
			return VerifyExpired, 0, nil
		}
		return VerifyExpired, 0, fmt.Errorf("email: read code: %w", err)
	}
	var rec codeRecord
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		return VerifyExpired, 0, fmt.Errorf("email: unmarshal code record: %w", err)
	}
	if rec.AccountID != accountID {
		return VerifyExpired, 0, nil
	}

	// Claim the attempt slot BEFORE looking at the guess: the budget is
	// spent whether or not the guess is right, and a rival that won the
	// slot means this submission never compares at all.
	if rec.Attempts >= MaxCodeAttempts {
		return VerifyExpired, 0, nil
	}
	rec.Attempts++
	claimed, err := json.Marshal(rec)
	if err != nil {
		return VerifyExpired, 0, fmt.Errorf("email: marshal code record: %w", err)
	}
	if ok, _ := store.CompareAndSwap(ctx, codeKey(emailID), raw, string(claimed), CodeTTL); !ok {
		// A concurrent submission won the slot (or consumed the record).
		return VerifyExpired, 0, nil
	}

	if subtle.ConstantTimeCompare([]byte(rec.Hash), []byte(hashCode(emailID, code))) == 1 {
		// Consume exactly once: only the caller whose claimed bytes still
		// match wins the delete.
		if ok, _ := store.CompareAndDelete(ctx, codeKey(emailID), string(claimed)); !ok {
			return VerifyExpired, 0, nil
		}
		return VerifyOK, 0, nil
	}

	if rec.Attempts >= MaxCodeAttempts {
		_, _ = store.CompareAndDelete(ctx, codeKey(emailID), string(claimed)) //nolint:errcheck // exhausted either way
		return VerifyExpired, 0, nil
	}
	return VerifyInvalid, MaxCodeAttempts - rec.Attempts, nil
}
