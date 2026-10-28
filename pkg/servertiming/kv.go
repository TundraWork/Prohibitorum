package servertiming

import (
	"context"
	"time"

	"prohibitorum/pkg/kv"
)

// KV wraps a key-value store so each call adds to the timing of the request
// whose context it ran in.
func KV(store kv.Store) kv.Store { return timedKV{store} }

type timedKV struct{ kv.Store }

func timed[T any](ctx context.Context, call func() (T, error)) (T, error) {
	if fromContext(ctx) == nil {
		return call()
	}
	start := time.Now()
	value, err := call()
	add(ctx, metricKV, time.Since(start))
	return value, err
}

func timedErr(ctx context.Context, call func() error) error {
	_, err := timed(ctx, func() (struct{}, error) { return struct{}{}, call() })
	return err
}

func (s timedKV) Get(ctx context.Context, key string) (string, error) {
	return timed(ctx, func() (string, error) { return s.Store.Get(ctx, key) })
}

func (s timedKV) Set(ctx context.Context, key, value string) error {
	return timedErr(ctx, func() error { return s.Store.Set(ctx, key, value) })
}

func (s timedKV) SetEx(ctx context.Context, key, value string, ttl time.Duration) error {
	return timedErr(ctx, func() error { return s.Store.SetEx(ctx, key, value, ttl) })
}

func (s timedKV) TTL(ctx context.Context, key string) (int64, error) {
	return timed(ctx, func() (int64, error) { return s.Store.TTL(ctx, key) })
}

func (s timedKV) Del(ctx context.Context, key string) error {
	return timedErr(ctx, func() error { return s.Store.Del(ctx, key) })
}

func (s timedKV) Pop(ctx context.Context, key string) (string, error) {
	return timed(ctx, func() (string, error) { return s.Store.Pop(ctx, key) })
}

func (s timedKV) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	return timed(ctx, func() (bool, error) { return s.Store.SetNX(ctx, key, value, ttl) })
}

func (s timedKV) CompareAndSwap(ctx context.Context, key, oldValue, newValue string, ttl time.Duration) (bool, error) {
	return timed(ctx, func() (bool, error) { return s.Store.CompareAndSwap(ctx, key, oldValue, newValue, ttl) })
}

func (s timedKV) CompareAndDelete(ctx context.Context, key, expectedValue string) (bool, error) {
	return timed(ctx, func() (bool, error) { return s.Store.CompareAndDelete(ctx, key, expectedValue) })
}

func (s timedKV) FencedCompareAndDelete(ctx context.Context, fenceKey, fenceValue, key, expectedValue string) (bool, error) {
	return timed(ctx, func() (bool, error) {
		return s.Store.FencedCompareAndDelete(ctx, fenceKey, fenceValue, key, expectedValue)
	})
}

func (s timedKV) FencedCompareAndSwap(ctx context.Context, fenceKey, fenceValue, key, oldValue, newValue string, ttl time.Duration) (bool, error) {
	return timed(ctx, func() (bool, error) {
		return s.Store.FencedCompareAndSwap(ctx, fenceKey, fenceValue, key, oldValue, newValue, ttl)
	})
}

func (s timedKV) ScanEntries(ctx context.Context, pattern string, cursor uint64, count int64) (kv.ScanEntriesResult, error) {
	return timed(ctx, func() (kv.ScanEntriesResult, error) { return s.Store.ScanEntries(ctx, pattern, cursor, count) })
}
