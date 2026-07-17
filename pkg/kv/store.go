package kv

import (
	"context"
	"errors"
	"time"
)

// ErrKeyNotFound is returned when a key does not exist in the store.
var ErrKeyNotFound = errors.New("kv: key not found")

// ErrSetNXInvalidTTL is returned by SetNX when ttl <= 0.
var ErrSetNXInvalidTTL = errors.New("kv: SetNX ttl must be positive")

// ErrCASInvalidTTL is returned by CompareAndSwap when ttl <= 0.
var ErrCASInvalidTTL = errors.New("kv: CAS ttl must be positive")

// KvEntry is a key-value entry with its remaining TTL.
type KvEntry struct {
	Key   string
	Value string
	TTL   int64 // -1 = no expiry, >= 0 = seconds remaining
}

// ScanEntriesResult holds one page of entries returned by ScanEntries.
type ScanEntriesResult struct {
	Entries    []KvEntry
	NextCursor uint64
}

// Store defines a string key-value store with TTL support.
type Store interface {
	// Get returns the value for key. Returns ("", ErrKeyNotFound) if the key
	// does not exist or has expired.
	Get(ctx context.Context, key string) (string, error)

	// Set stores a value with no expiration.
	Set(ctx context.Context, key, value string) error

	// SetEx stores a value with a TTL.
	SetEx(ctx context.Context, key, value string, ttl time.Duration) error

	// TTL returns the remaining time-to-live for a key:
	//   -2: key does not exist or has expired
	//   -1: key exists but has no expiration
	//   >= 0: remaining seconds (ceiling)
	TTL(ctx context.Context, key string) (int64, error)

	// Del deletes a key. No error if the key does not exist.
	Del(ctx context.Context, key string) error

	// Pop atomically retrieves and removes the value at key. Returns
	// ErrKeyNotFound if absent. Implementations MUST guarantee that
	// concurrent Pop calls with the same key return the value to exactly
	// one caller — the others see ErrKeyNotFound. Used by single-use
	// flows (partial-session token consume, sudo intent consume, WebAuthn
	// ceremony stash consume) where a Get-then-Del would race.
	Pop(ctx context.Context, key string) (string, error)

	// SetNX atomically sets key=value with the given ttl ONLY if key does not
	// already exist (an expired key counts as absent). Returns true if it set
	// the key, false if the key already existed. ttl MUST be > 0. A backend
	// error returns (false, err) so callers can fail closed.
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)

	// CompareAndSwap atomically replaces the byte-exact oldValue at key with
	// newValue and resets the TTL to ttl, returning true if the swap
	// succeeded. Returns (false, nil) if the key is absent/expired or its
	// current bytes do not equal oldValue — never an error for a clean
	// mismatch, so callers fail closed. ttl MUST be > 0; a non-positive ttl
	// returns (false, ErrCASInvalidTTL). Implementations MUST serialize CAS
	// against all other mutating operations on the same key (Set, SetEx,
	// Del, Pop, SetNX, other CAS) so concurrent callers with the same
	// expected value produce exactly one winner.
	CompareAndSwap(ctx context.Context, key, oldValue, newValue string, ttl time.Duration) (bool, error)

	// CompareAndDelete atomically deletes key only when its current value is
	// byte-exact expectedValue. It returns (false, nil) when the key is absent,
	// expired, or owned by another value. Implementations MUST serialize it
	// against every mutating operation on the same key.
	CompareAndDelete(ctx context.Context, key, expectedValue string) (bool, error)

	// FencedCompareAndDelete deletes key only when both fenceKey is owned by
	// fenceValue and key contains expectedValue in the same atomic operation.
	FencedCompareAndDelete(ctx context.Context, fenceKey, fenceValue, key, expectedValue string) (bool, error)

	// FencedCompareAndSwap replaces key only when both fenceKey is owned by
	// fenceValue and key contains oldValue in the same atomic operation.
	FencedCompareAndSwap(ctx context.Context, fenceKey, fenceValue, key, oldValue, newValue string, ttl time.Duration) (bool, error)

	// ScanEntries returns entries (key + value + TTL) matching a Redis-style
	// glob pattern. cursor=0 starts a new scan. count is a hint for batch
	// size. Returns entries and the next cursor (0 = complete).
	ScanEntries(ctx context.Context, pattern string, cursor uint64, count int64) (ScanEntriesResult, error)

	// Close releases resources held by the store.
	Close() error
}
