package session

import (
	"context"
	"sync"
	"testing"
)

type fakeRefKeyStore struct {
	mu  sync.Mutex
	key []byte
}

func (f *fakeRefKeyStore) GetAccountRefKey(context.Context) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.key...), nil
}

func (f *fakeRefKeyStore) InitAccountRefKey(_ context.Context, k []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.key == nil {
		f.key = append([]byte(nil), k...)
	}
	return nil
}

func TestAccountRefs_KeyCreatedOnceAndKept(t *testing.T) {
	store := &fakeRefKeyStore{}
	first, err := LoadAccountRefs(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.key) != accountRefKeyLen {
		t.Fatalf("key length = %d, want %d", len(store.key), accountRefKeyLen)
	}
	second, err := LoadAccountRefs(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	if first.Full(12) != second.Full(12) {
		t.Fatal("reference changed across restarts")
	}
}

func TestAccountRefs_ConcurrentInitAgrees(t *testing.T) {
	store := &fakeRefKeyStore{}
	var wg sync.WaitGroup
	refs := make([]*AccountRefs, 8)
	for i := range refs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := LoadAccountRefs(context.Background(), store)
			if err != nil {
				t.Error(err)
				return
			}
			refs[i] = r
		}(i)
	}
	wg.Wait()
	for _, r := range refs[1:] {
		if r.Full(7) != refs[0].Full(7) {
			t.Fatal("instances disagree on the reference key")
		}
	}
}

func TestAccountRefs_ShortsWidenOnCollision(t *testing.T) {
	refs := NewAccountRefs([]byte("fixed test key"))
	if got := len(refs.Full(1)); got != 64 {
		t.Fatalf("Full length = %d", got)
	}
	seen := map[string]int32{}
	var a, b int32
	for id := int32(1); id < 100000; id++ {
		p := refs.Full(id)[:4]
		if other, ok := seen[p]; ok {
			a, b = other, id
			break
		}
		seen[p] = id
	}
	if a == 0 {
		t.Fatal("no 4-digit collision found")
	}
	var other int32
	for id := int32(1); ; id++ {
		if id != a && id != b && refs.Full(id)[:4] != refs.Full(a)[:4] {
			other = id
			break
		}
	}
	got := refs.Shorts([]int32{a, b, other})
	if got[a] != refs.Full(a)[:8] || got[b] != refs.Full(b)[:8] {
		t.Fatalf("colliding accounts must use 8 digits: %v", got)
	}
	if got[other] != refs.Full(other)[:4] {
		t.Fatalf("non-colliding account must use 4 digits: %v", got)
	}
	if alone := refs.Shorts([]int32{a}); alone[a] != refs.Full(a)[:4] {
		t.Fatalf("a lone account uses 4 digits: %v", alone)
	}
}
