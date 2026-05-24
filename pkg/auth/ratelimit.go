// Package auth — ratelimit.go
//
// In-process per-key fixed-window rate limiter. Used by the auth-sensitive
// endpoints (login, enrollment register, pair begin/lookup/approve, sudo)
// to bound the cost of brute-force / spam regardless of how cheap each
// individual request is.
//
// Design choices:
//   - In-process is appropriate for single-instance prohibitorum. A horizontally
//     scaled deployment should swap this for a Redis-backed counter (the
//     interface is small enough).
//   - Fixed window (not sliding) — simpler, accurate enough at human time
//     scales. Window slop is up to 2x the configured limit at the edge,
//     acceptable for "is this a sustained attack?" gating.
//   - Buckets self-prune lazily on Allow when the bucket map exceeds a soft
//     ceiling. Memory bounded by the active key count.
package auth

import (
	"sync"
	"time"
)

// RateLimiter is the in-process limiter. Safe for concurrent use.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rlBucket
	// pruneSize triggers a sweep when len(buckets) exceeds it. 1024 buckets
	// ≈ tens of KB; sweeping is O(N) but rare.
	pruneSize int
}

type rlBucket struct {
	count int
	reset time.Time
}

// NewRateLimiter returns a fresh limiter.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		buckets:   make(map[string]*rlBucket),
		pruneSize: 1024,
	}
}

// Allow records one hit under key with a window of size `window` and a cap
// of `max` hits per window. Returns true when the hit is within budget.
// false means the caller should reject the request (e.g. 429).
//
// Key composition is the caller's responsibility — typically something like
// "login:ip:1.2.3.4" or "pair:account:42".
func (r *RateLimiter) Allow(key string, max int, window time.Duration) bool {
	if max <= 0 || window <= 0 {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if len(r.buckets) >= r.pruneSize {
		r.pruneLocked(now)
	}
	b, ok := r.buckets[key]
	if !ok || now.After(b.reset) {
		r.buckets[key] = &rlBucket{count: 1, reset: now.Add(window)}
		return true
	}
	if b.count >= max {
		return false
	}
	b.count++
	return true
}

// RetryAfter returns the duration until the bucket for key resets. Used to
// populate the Retry-After header on a 429. Returns 0 when the key isn't
// tracked or is already expired.
func (r *RateLimiter) RetryAfter(key string) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.buckets[key]
	if !ok {
		return 0
	}
	rem := time.Until(b.reset)
	if rem < 0 {
		return 0
	}
	return rem
}

func (r *RateLimiter) pruneLocked(now time.Time) {
	for k, b := range r.buckets {
		if now.After(b.reset) {
			delete(r.buckets, k)
		}
	}
}
