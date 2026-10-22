package email

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"prohibitorum/pkg/kv"
)

// recordingKV records the order of the compare-operations a Verify performs.
type recordingKV struct {
	kv.Store
	mu  sync.Mutex
	ops []string
}

func (r *recordingKV) CompareAndSwap(ctx context.Context, key, oldValue, newValue string, ttl time.Duration) (bool, error) {
	r.mu.Lock()
	r.ops = append(r.ops, "cas")
	r.mu.Unlock()
	return r.Store.CompareAndSwap(ctx, key, oldValue, newValue, ttl)
}

func (r *recordingKV) CompareAndDelete(ctx context.Context, key, expectedValue string) (bool, error) {
	r.mu.Lock()
	r.ops = append(r.ops, "cad")
	r.mu.Unlock()
	return r.Store.CompareAndDelete(ctx, key, expectedValue)
}

func (r *recordingKV) order() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.ops, ",")
}

// TestVerifyClaimsAttemptSlotBeforeConsuming is the deterministic guard for
// the review's B1: a successful verification must first claim an attempt
// slot (CAS increment of attempts) and only then consume the record by
// deleting the *claimed* value. With the old order — compare, then CAD the
// original value — the correct code bypasses the attempt budget entirely:
// it never increments attempts, so the ops sequence is just "cad".
func TestVerifyClaimsAttemptSlotBeforeConsuming(t *testing.T) {
	ctx := context.Background()
	store := &recordingKV{Store: kv.NewMemoryStore()}

	code, err := Issue(ctx, store, 31, 90, "order@example.com")
	if err != nil {
		t.Fatal(err)
	}
	res, _, err := Verify(ctx, store, 31, 90, code)
	if err != nil || res != VerifyOK {
		t.Fatalf("verify correct = %v, %v", res, err)
	}
	order := store.order()
	if !strings.Contains(order, "cas,cad") {
		t.Fatalf("successful verify ops = %q; want a CAS attempt-slot claim (cas) before the consume (cad)", order)
	}
}

// TestVerifyWrongGuessSpendsItsSlot pins the other half of the order: a
// wrong guess must have claimed (CAS) its slot before reporting invalid —
// the budget is spent even when the guess misses.
func TestVerifyWrongGuessSpendsItsSlot(t *testing.T) {
	ctx := context.Background()
	store := &recordingKV{Store: kv.NewMemoryStore()}

	if _, err := Issue(ctx, store, 32, 91, "wrong@example.com"); err != nil {
		t.Fatal(err)
	}
	res, _, err := Verify(ctx, store, 32, 91, "999999")
	if err != nil || res != VerifyInvalid {
		t.Fatalf("verify wrong = %v, %v", res, err)
	}
	if order := store.order(); order != "cas" {
		t.Fatalf("wrong-guess ops = %q; want exactly one CAS slot claim", order)
	}
}
