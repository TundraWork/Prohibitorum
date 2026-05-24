package auth

import (
	"testing"
	"time"
)

func TestRateLimiter_AllowsUpToMax(t *testing.T) {
	rl := NewRateLimiter()
	const max = 3
	for i := 0; i < max; i++ {
		if !rl.Allow("k", max, time.Minute) {
			t.Errorf("hit %d should be allowed", i+1)
		}
	}
	if rl.Allow("k", max, time.Minute) {
		t.Error("hit max+1 should be rejected")
	}
}

func TestRateLimiter_IsolatesPerKey(t *testing.T) {
	rl := NewRateLimiter()
	if !rl.Allow("a", 1, time.Minute) {
		t.Fatal("a:1 allowed")
	}
	if rl.Allow("a", 1, time.Minute) {
		t.Fatal("a:2 must be rejected")
	}
	if !rl.Allow("b", 1, time.Minute) {
		t.Error("b should not be affected by a's bucket")
	}
}

func TestRateLimiter_ResetsAfterWindow(t *testing.T) {
	rl := NewRateLimiter()
	if !rl.Allow("k", 1, 10*time.Millisecond) {
		t.Fatal("first hit allowed")
	}
	if rl.Allow("k", 1, 10*time.Millisecond) {
		t.Fatal("second hit in window must be rejected")
	}
	time.Sleep(15 * time.Millisecond)
	if !rl.Allow("k", 1, 10*time.Millisecond) {
		t.Error("post-window hit should be allowed again")
	}
}

func TestRateLimiter_RetryAfter(t *testing.T) {
	rl := NewRateLimiter()
	rl.Allow("k", 1, 200*time.Millisecond)
	d := rl.RetryAfter("k")
	if d <= 0 || d > 200*time.Millisecond {
		t.Errorf("RetryAfter out of expected range: %v", d)
	}
	// Unknown key
	if rl.RetryAfter("missing") != 0 {
		t.Error("RetryAfter on missing key should be 0")
	}
}

func TestRateLimiter_ZeroMaxAllows(t *testing.T) {
	// Defensive: 0/negative cap or window short-circuit to allow (avoids
	// accidental lockouts from misconfig).
	rl := NewRateLimiter()
	if !rl.Allow("k", 0, time.Minute) {
		t.Error("max=0 should allow")
	}
	if !rl.Allow("k", 10, 0) {
		t.Error("window=0 should allow")
	}
}
