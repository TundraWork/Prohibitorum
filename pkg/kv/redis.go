package kv

import (
	"context"
	"math"
	"time"

	"github.com/redis/go-redis/v9"
)

// ScanEntries implements Store.ScanEntries using a single Redis pipeline:
// SCAN to discover keys, then MGET + batched TTL for values and TTLs.
func (r *RedisStore) ScanEntries(ctx context.Context, pattern string, cursor uint64, count int64) (ScanEntriesResult, error) {
	if count <= 0 {
		count = 100
	}

	keys, nextCursor, err := r.client.Scan(ctx, cursor, pattern, count).Result()
	if err != nil {
		return ScanEntriesResult{}, err
	}

	if len(keys) == 0 {
		return ScanEntriesResult{NextCursor: nextCursor}, nil
	}

	pipe := r.client.Pipeline()
	valCmds := make([]*redis.StringCmd, len(keys))
	ttlCmds := make([]*redis.DurationCmd, len(keys))
	for i, k := range keys {
		valCmds[i] = pipe.Get(ctx, k)
		ttlCmds[i] = pipe.TTL(ctx, k)
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return ScanEntriesResult{}, err
	}

	entries := make([]KvEntry, 0, len(keys))
	for i, k := range keys {
		val, err := valCmds[i].Result()
		if err == redis.Nil {
			continue // expired between SCAN and GET
		}
		if err != nil {
			return ScanEntriesResult{}, err
		}

		d := ttlCmds[i].Val()
		var ttl int64
		switch {
		case d == -2*time.Second:
			continue // expired
		case d == -1*time.Second:
			ttl = -1
		default:
			secs := d.Seconds()
			if secs <= 0 {
				ttl = 0
			} else {
				ttl = int64(math.Ceil(secs))
			}
		}

		entries = append(entries, KvEntry{Key: k, Value: val, TTL: ttl})
	}

	return ScanEntriesResult{Entries: entries, NextCursor: nextCursor}, nil
}

// RedisStore implements Store backed by a Redis server.
type RedisStore struct {
	client *redis.Client
}

// NewRedisStore creates a RedisStore. url is a host:port address
// (e.g. "localhost:6379"). The constructor pings the server to verify
// connectivity.
func NewRedisStore(url string) (*RedisStore, error) {
	client := redis.NewClient(&redis.Options{Addr: url})
	if err := client.Ping(context.Background()).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return &RedisStore{client: client}, nil
}

func (r *RedisStore) Get(ctx context.Context, key string) (string, error) {
	val, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", ErrKeyNotFound
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

func (r *RedisStore) Set(ctx context.Context, key, value string) error {
	return r.client.Set(ctx, key, value, 0).Err()
}

func (r *RedisStore) SetEx(ctx context.Context, key, value string, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}

func (r *RedisStore) TTL(ctx context.Context, key string) (int64, error) {
	d, err := r.client.TTL(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	switch {
	case d == -2*time.Second:
		return -2, nil
	case d == -1*time.Second:
		return -1, nil
	default:
		secs := d.Seconds()
		if secs <= 0 {
			return 0, nil
		}
		return int64(math.Ceil(secs)), nil
	}
}

func (r *RedisStore) Del(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

// Pop atomically retrieves and removes the value at key. Uses Redis's
// GETDEL (≥6.2) which performs the read-and-remove in a single command,
// guaranteeing exactly one concurrent caller observes the value.
func (r *RedisStore) Pop(ctx context.Context, key string) (string, error) {
	val, err := r.client.GetDel(ctx, key).Result()
	if err == redis.Nil {
		return "", ErrKeyNotFound
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

// SetNX atomically sets key=value with ttl only if key is absent, via Redis
// SET ... NX (go-redis SetNX). An expired key is absent. ttl must be > 0.
func (r *RedisStore) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, ErrSetNXInvalidTTL
	}
	ok, err := r.client.SetNX(ctx, key, value, ttl).Result()
	if err != nil {
		return false, err
	}
	return ok, nil
}

func (r *RedisStore) Close() error {
	return r.client.Close()
}

