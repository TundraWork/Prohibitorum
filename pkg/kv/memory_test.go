package kv

import (
	"context"
	"errors"
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
