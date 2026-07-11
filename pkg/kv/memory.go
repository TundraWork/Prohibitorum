package kv

import (
	"context"
	"math"
	"path/filepath"
	"sync"
	"time"

	"github.com/jellydator/ttlcache/v3"
)

// MemoryStore implements Store backed by an in-process ttlcache.
type MemoryStore struct {
	mu    sync.Mutex
	cache *ttlcache.Cache[string, string]
}

// NewMemoryStore creates a MemoryStore and starts the background expiration
// goroutine. Call Close to stop it.
func NewMemoryStore() *MemoryStore {
	cache := ttlcache.New(ttlcache.WithDisableTouchOnHit[string, string]())
	go cache.Start()
	return &MemoryStore{cache: cache}
}

func (m *MemoryStore) Get(_ context.Context, key string) (string, error) {
	item := m.cache.Get(key)
	if item == nil {
		return "", ErrKeyNotFound
	}
	return item.Value(), nil
}

func (m *MemoryStore) Set(_ context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache.Set(key, value, ttlcache.NoTTL)
	return nil
}

func (m *MemoryStore) SetEx(_ context.Context, key, value string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache.Set(key, value, ttl)
	return nil
}

func (m *MemoryStore) TTL(_ context.Context, key string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.cache.Get(key)
	if item == nil {
		return -2, nil
	}
	if item.TTL() == ttlcache.NoTTL {
		return -1, nil
	}
	remaining := time.Until(item.ExpiresAt())
	if remaining <= 0 {
		return 0, nil
	}
	return int64(math.Ceil(remaining.Seconds())), nil
}

func (m *MemoryStore) Del(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache.Delete(key)
	return nil
}

// Pop atomically retrieves and removes the value at key. m.mu serializes
// the Get-and-delete against all other mutating ops on the same key so two
// concurrent callers can never both observe the value; ttlcache v3's
// GetAndDelete performs the lookup-then-remove under its own lock too, so
// the guarantee holds even for direct cache access.
func (m *MemoryStore) Pop(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.cache.GetAndDelete(key)
	if !ok || item == nil {
		return "", ErrKeyNotFound
	}
	return item.Value(), nil
}

// SetNX atomically sets key=value with ttl only if key is absent. m.mu
// serializes the Get→Set against other SetNX callers and, now, against all
// other mutating methods; ttlcache's Get returns nil for expired items
// (WithDisableTouchOnHit means Get has no side effect), so an expired key is
// correctly treated as absent.
func (m *MemoryStore) SetNX(_ context.Context, key, value string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, ErrSetNXInvalidTTL
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if item := m.cache.Get(key); item != nil {
		return false, nil
	}
	m.cache.Set(key, value, ttl)
	return true, nil
}

// CompareAndSwap atomically replaces the byte-exact oldValue at key with
// newValue, resetting the TTL to ttl. m.mu serializes the read-compare-write
// against Set, SetEx, Del, Pop, SetNX, and other CAS calls, so concurrent
// callers with the same expected value produce exactly one winner. An
// absent/expired key or a byte mismatch returns (false, nil) — never an
// error — so callers fail closed without a backend fault. ttl must be > 0.
func (m *MemoryStore) CompareAndSwap(_ context.Context, key, oldValue, newValue string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, ErrCASInvalidTTL
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.cache.Get(key)
	if item == nil || item.Value() != oldValue {
		return false, nil
	}
	m.cache.Set(key, newValue, ttl)
	return true, nil
}

// ScanEntries implements Store.ScanEntries by iterating in-memory items
// with glob matching. Returns all matching entries in one shot (NextCursor=0).
func (m *MemoryStore) ScanEntries(_ context.Context, pattern string, _ uint64, _ int64) (ScanEntriesResult, error) {
	var entries []KvEntry
	m.cache.Range(func(item *ttlcache.Item[string, string]) bool {
		key := item.Key()
		matched, err := filepath.Match(pattern, key)
		if err != nil {
			return true // skip malformed pattern — treat as no match
		}
		if !matched {
			return true
		}
		var ttl int64
		if item.TTL() == ttlcache.NoTTL {
			ttl = -1
		} else {
			remaining := time.Until(item.ExpiresAt())
			if remaining <= 0 {
				return true // expired
			}
			ttl = int64(math.Ceil(remaining.Seconds()))
		}
		entries = append(entries, KvEntry{Key: key, Value: item.Value(), TTL: ttl})
		return true
	})
	return ScanEntriesResult{Entries: entries, NextCursor: 0}, nil
}

func (m *MemoryStore) Close() error {
	m.cache.Stop()
	return nil
}
