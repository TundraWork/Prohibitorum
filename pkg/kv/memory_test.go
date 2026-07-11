package kv

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestMemoryStore_PopAtomic covers the audit's Critical-2 finding: the
// partial-session / sudo-intent / WebAuthn-ceremony consume paths previously
// did Get-then-Del, which under concurrency let two callers both observe
// the value before either Del fired. Pop uses ttlcache's GetAndDelete which
// holds the cache's internal lock across the lookup-then-remove, so exactly
// one caller observes the value. The losing caller must see ErrKeyNotFound.
func TestMemoryStore_PopAtomic(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	ctx := context.Background()

	const N = 100 // run the race many times to give the scheduler a chance
	for i := 0; i < N; i++ {
		key := "race-key"
		if err := store.Set(ctx, key, "secret-value"); err != nil {
			t.Fatalf("Set: %v", err)
		}

		var wg sync.WaitGroup
		wg.Add(2)
		var hits int32
		var misses int32
		var hitValue string
		var hitMu sync.Mutex

		for j := 0; j < 2; j++ {
			go func() {
				defer wg.Done()
				v, err := store.Pop(ctx, key)
				switch {
				case err == nil:
					atomic.AddInt32(&hits, 1)
					hitMu.Lock()
					hitValue = v
					hitMu.Unlock()
				case errors.Is(err, ErrKeyNotFound):
					atomic.AddInt32(&misses, 1)
				default:
					t.Errorf("unexpected error: %v", err)
				}
			}()
		}
		wg.Wait()

		if hits != 1 {
			t.Fatalf("iteration %d: want exactly 1 hit, got %d (misses=%d)", i, hits, misses)
		}
		if misses != 1 {
			t.Fatalf("iteration %d: want exactly 1 miss, got %d (hits=%d)", i, misses, hits)
		}
		if hitValue != "secret-value" {
			t.Fatalf("iteration %d: hit value %q, want %q", i, hitValue, "secret-value")
		}
	}
}

func TestMemoryStore_PopReturnsKeyNotFoundOnAbsent(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	_, err := store.Pop(context.Background(), "absent")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Pop on absent key: want ErrKeyNotFound, got %v", err)
	}
}

func TestMemoryStore_PopRemovesKey(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	ctx := context.Background()
	if err := store.Set(ctx, "k", "v"); err != nil {
		t.Fatal(err)
	}
	if v, err := store.Pop(ctx, "k"); err != nil || v != "v" {
		t.Fatalf("first Pop: v=%q err=%v", v, err)
	}
	if _, err := store.Get(ctx, "k"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Get after Pop: want ErrKeyNotFound, got %v", err)
	}
}

func TestMemorySetNX(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()
	ok, err := s.SetNX(ctx, "k", "v1", time.Minute)
	if err != nil || !ok {
		t.Fatalf("first SetNX = (%v,%v), want (true,nil)", ok, err)
	}
	ok, err = s.SetNX(ctx, "k", "v2", time.Minute)
	if err != nil || ok {
		t.Fatalf("second SetNX = (%v,%v), want (false,nil)", ok, err)
	}
	if got, _ := s.Get(ctx, "k"); got != "v1" {
		t.Fatalf("value = %q, want v1 (NX must not overwrite)", got)
	}
	if _, err := s.SetNX(ctx, "k2", "v", 0); !errors.Is(err, ErrSetNXInvalidTTL) {
		t.Fatal("SetNX with ttl=0 should return ErrSetNXInvalidTTL")
	}
	if _, err := s.SetNX(ctx, "k3", "v", -time.Second); !errors.Is(err, ErrSetNXInvalidTTL) {
		t.Fatalf("negative ttl err = %v, want ErrSetNXInvalidTTL", err)
	}
}

func TestMemorySetNXExpiredIsAbsent(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()
	if ok, _ := s.SetNX(ctx, "k", "v1", 20*time.Millisecond); !ok {
		t.Fatal("seed SetNX should succeed")
	}
	time.Sleep(40 * time.Millisecond)
	ok, err := s.SetNX(ctx, "k", "v2", time.Minute)
	if err != nil || !ok {
		t.Fatalf("post-expiry SetNX = (%v,%v), want (true,nil)", ok, err)
	}
	if got, _ := s.Get(ctx, "k"); got != "v2" {
		t.Fatalf("post-expiry value = %q, want v2", got)
	}
}

func TestMemorySetNXConcurrentExactlyOneWinner(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()
	const n = 100
	var wins int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ok, err := s.SetNX(ctx, "race", "v", time.Minute)
			if err != nil {
				t.Errorf("unexpected SetNX error: %v", err)
			}
			if ok {
				atomic.AddInt64(&wins, 1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if wins != 1 {
		t.Fatalf("winners = %d, want exactly 1", wins)
	}
}

// TestMemoryStore_CAS_RejectsNonPositiveTTL ensures CAS preserves the
// positive-TTL contract shared with SetNX — a non-positive TTL is a
// programmer error, not a silent no-op.
func TestMemoryStore_CAS_RejectsNonPositiveTTL(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()
	if err := s.Set(ctx, "k", "old"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompareAndSwap(ctx, "k", "old", "new", 0); !errors.Is(err, ErrCASInvalidTTL) {
		t.Fatalf("ttl=0: err=%v, want ErrCASInvalidTTL", err)
	}
	if _, err := s.CompareAndSwap(ctx, "k", "old", "new", -time.Second); !errors.Is(err, ErrCASInvalidTTL) {
		t.Fatalf("ttl<0: err=%v, want ErrCASInvalidTTL", err)
	}
}

// TestMemoryStore_CAS_SwapMatches ensures a matching expected value swaps
// in the new value with the supplied TTL, preserving a positive remaining
// window (not NoTTL).
func TestMemoryStore_CAS_SwapMatches(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()
	if err := s.Set(ctx, "k", "old"); err != nil {
		t.Fatal(err)
	}
	ok, err := s.CompareAndSwap(ctx, "k", "old", "new", time.Minute)
	if err != nil || !ok {
		t.Fatalf("CAS match: ok=%v err=%v, want (true,nil)", ok, err)
	}
	got, err := s.Get(ctx, "k")
	if err != nil || got != "new" {
		t.Fatalf("after CAS: got=%q err=%v, want %q", got, err, "new")
	}
	ttl, err := s.TTL(ctx, "k")
	if err != nil || ttl <= 0 {
		t.Fatalf("after CAS TTL: ttl=%d err=%v, want >0 (positive TTL preserved)", ttl, err)
	}
}

// TestMemoryStore_CAS_MismatchNoSwap ensures a byte-exact mismatch leaves the
// stored value untouched and returns (false, nil).
func TestMemoryStore_CAS_MismatchNoSwap(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()
	if err := s.Set(ctx, "k", "actual"); err != nil {
		t.Fatal(err)
	}
	ok, err := s.CompareAndSwap(ctx, "k", "stale", "new", time.Minute)
	if err != nil || ok {
		t.Fatalf("CAS mismatch: ok=%v err=%v, want (false,nil)", ok, err)
	}
	if got, _ := s.Get(ctx, "k"); got != "actual" {
		t.Fatalf("value after mismatch CAS = %q, want %q (must not swap)", got, "actual")
	}
}

// TestMemoryStore_CAS_MissingKey treats an absent/expired key as a mismatch
// (the expected pending JSON is never present), returning (false, nil) —
// never an error, so the caller fails closed without a backend fault.
func TestMemoryStore_CAS_MissingKey(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()
	ok, err := s.CompareAndSwap(ctx, "absent", "old", "new", time.Minute)
	if err != nil || ok {
		t.Fatalf("CAS missing key: ok=%v err=%v, want (false,nil)", ok, err)
	}
}

// TestMemoryStore_CAS_ByteExact ensures CAS compares raw bytes, not
// semantic JSON equality — a re-ordered or re-marshalled record with the
// same fields but different bytes must NOT match. This is what makes the
// pending→approved swap race-safe: only the exact canonical pending blob
// wins.
func TestMemoryStore_CAS_ByteExact(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()
	// Same semantic content, different bytes (whitespace) — must not match.
	if err := s.Set(ctx, "k", `{"a":1}`); err != nil {
		t.Fatal(err)
	}
	ok, err := s.CompareAndSwap(ctx, "k", `{"a": 1}`, `{"a":2}`, time.Minute)
	if err != nil || ok {
		t.Fatalf("byte-exact mismatch: ok=%v err=%v, want (false,nil)", ok, err)
	}
}

// TestMemoryStore_CAS_ConcurrentExactlyOneWinner is the core race test:
// many goroutines CAS the SAME expected value to distinct new values; only
// one must succeed and the final stored value must be exactly that winner's
// new value. Run under -race to catch lock-order violations.
func TestMemoryStore_CAS_ConcurrentExactlyOneWinner(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()
	const expected = "pending"
	if err := s.Set(ctx, "k", expected); err != nil {
		t.Fatal(err)
	}
	const n = 100
	var wins int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			newVal := fmt.Sprintf("winner-%d", i)
			ok, err := s.CompareAndSwap(ctx, "k", expected, newVal, time.Minute)
			if err != nil {
				t.Errorf("CAS %d: unexpected err %v", i, err)
				return
			}
			if ok {
				atomic.AddInt64(&wins, 1)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if wins != 1 {
		t.Fatalf("winners = %d, want exactly 1", wins)
	}
	got, _ := s.Get(ctx, "k")
	if !strings.HasPrefix(got, "winner-") {
		t.Fatalf("final value = %q, want a winner-* value", got)
	}
}

// TestMemoryStore_CAS_SerializesAgainstSet ensures CAS is serialized against
// a concurrent plain Set on the same key — the final state is deterministic
// (either the CAS won before Set, or Set won before CAS), never a torn write.
func TestMemoryStore_CAS_SerializesAgainstSet(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()
	if err := s.Set(ctx, "k", "pending"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = s.CompareAndSwap(ctx, "k", "pending", "cas-won", time.Minute)
	}()
	go func() {
		defer wg.Done()
		_ = s.Set(ctx, "k", "set-won")
	}()
	wg.Wait()
	got, _ := s.Get(ctx, "k")
	if got != "cas-won" && got != "set-won" {
		t.Fatalf("final value = %q, want either cas-won or set-won", got)
	}
}
