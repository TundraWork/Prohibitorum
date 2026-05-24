package kv

import (
	"context"
	"errors"
	"time"
)

// ErrKeyNotFound is returned when a key does not exist in the store.
var ErrKeyNotFound = errors.New("kv: key not found")

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

	// ScanEntries returns entries (key + value + TTL) matching a Redis-style
	// glob pattern. cursor=0 starts a new scan. count is a hint for batch
	// size. Returns entries and the next cursor (0 = complete).
	ScanEntries(ctx context.Context, pattern string, cursor uint64, count int64) (ScanEntriesResult, error)

	// Close releases resources held by the store.
	Close() error
}
