package pairing

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/kv"
)

// authErrCode returns the *authn.AuthError.Code embedded in err, or "" if
// err is not an AuthError. AuthError constructors return fresh pointers
// each call, so tests match on Code rather than with errors.Is against a
// freshly-allocated sentinel — mirroring the handler convention
// (authn.AsAuthError + ae.Code).
func authErrCode(err error) string {
	if ae := authn.AsAuthError(err); ae != nil {
		return ae.Code
	}
	return ""
}

// newTestStore builds a fresh MemoryStore-backed PairingStore for each test.
func newTestStore(t *testing.T) (*PairingStore, kv.Store) {
	t.Helper()
	mem := kv.NewMemoryStore()
	t.Cleanup(func() { _ = mem.Close() })
	return NewPairingStore(mem), mem
}

// mustNewPairing creates a pending pairing and aborts on error.
func mustNewPairing(t *testing.T, s *PairingStore) *Pairing {
	t.Helper()
	p, err := s.New(context.Background(), "ua/test", "127.0.0.1")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

// TestApprove_ConcurrentDifferentAccountsExactlyOneWinner proves the P1
// race: two different accounts approve the SAME pending pairing. Before the
// fix both Approve calls returned nil (the second overwrote the winner's
// ApprovedFor); after the fix exactly one succeeds and the canonical
// record is bound to that winner's account. The loser receives a
// pairing_state error and must NOT have overwritten the record.
func TestApprove_ConcurrentDifferentAccountsExactlyOneWinner(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	p := mustNewPairing(t, s)

	const acctA, acctB = int32(1), int32(2)
	var wg sync.WaitGroup
	start := make(chan struct{})
	var wins, fails int64
	var winnerAcct int32

	approve := func(acct int32) {
		defer wg.Done()
		<-start
		// Each caller reads its own fresh copy before racing to approve,
		// mirroring how the HTTP handler loads the pairing per request.
		loaded, err := s.LookupByCode(ctx, p.Code)
		if err != nil {
			t.Errorf("LookupByCode acct %d: %v", acct, err)
			return
		}
		if err := s.Approve(ctx, loaded, acct); err != nil {
			if authErrCode(err) == "pairing_state" {
				atomic.AddInt64(&fails, 1)
				return
			}
			t.Errorf("Approve acct %d: unexpected err %v", acct, err)
			return
		}
		atomic.AddInt64(&wins, 1)
		atomic.StoreInt32(&winnerAcct, acct)
	}

	wg.Add(2)
	go approve(acctA)
	go approve(acctB)
	close(start)
	wg.Wait()

	if wins != 1 {
		t.Fatalf("wins = %d, want exactly 1 (fails=%d)", wins, fails)
	}
	if fails != 1 {
		t.Fatalf("fails = %d, want exactly 1 (wins=%d)", fails, wins)
	}

	// The canonical record must reflect exactly the winner's account.
	canonical, err := s.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if canonical.Status != PairingApproved {
		t.Fatalf("canonical status = %q, want approved", canonical.Status)
	}
	if canonical.ApprovedFor != winnerAcct {
		t.Fatalf("canonical ApprovedFor = %d, want winner %d", canonical.ApprovedFor, winnerAcct)
	}
}

// TestApprove_SameAccountIdempotent ensures the winning account can call
// Approve again (after reading the canonical approved state) and it's a
// no-op, while a different account still gets rejected.
func TestApprove_SameAccountIdempotent(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	p := mustNewPairing(t, s)
	const acct = int32(7)
	loaded, err := s.LookupByCode(ctx, p.Code)
	if err != nil {
		t.Fatalf("LookupByCode: %v", err)
	}
	if err := s.Approve(ctx, loaded, acct); err != nil {
		t.Fatalf("first Approve: %v", err)
	}

	// Re-read canonical approved state, approve again — idempotent.
	approved, err := s.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if err := s.Approve(ctx, approved, acct); err != nil {
		t.Fatalf("idempotent re-Approve: %v", err)
	}
	again, err := s.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID after re-approve: %v", err)
	}
	if again.ApprovedFor != acct || again.Status != PairingApproved {
		t.Fatalf("canonical after re-approve = %+v, want acct=%d approved", again, acct)
	}

	// A different account must still be rejected even after the winner's
	// idempotent re-approval.
	if err := s.Approve(ctx, approved, 999); authErrCode(err) != "pairing_state" {
		t.Fatalf("rival Approve after idempotent: err=%v, want ErrPairingState", err)
	}
}

// TestConsume_ConcurrentExactlyOneWinner proves the P1 race: two concurrent
// Consume calls on the SAME approved pairing. Before the fix both returned
// nil (both deleted, but the handler had already issued two sessions before
// the delete); after the fix exactly one Consume succeeds and returns the
// consumed record, the loser sees pairing_not_found.
// TestConsume_ConcurrentExactlyOneWinner proves the P1 race fix: many
// concurrent Consume calls on the SAME approved pairing. Consume CAS-replaces
// the exact approved JSON with a consumed marker, so exactly one caller's CAS
// matches and wins; every rival sees the consumed bytes (a mismatch) and
// fails. The winner receives the consumed pairing record; the code-index key
// is deleted only by the winner. The consumed marker remains in KV until TTL
// to prevent resurrection.
func TestConsume_ConcurrentExactlyOneWinner(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	p := mustNewPairing(t, s)
	const acct = int32(5)
	loaded, err := s.LookupByCode(ctx, p.Code)
	if err != nil {
		t.Fatalf("LookupByCode: %v", err)
	}
	if err := s.Approve(ctx, loaded, acct); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	const n = 50
	var wg sync.WaitGroup
	start := make(chan struct{})
	var wins, fails int64
	var consumedID string
	var mu sync.Mutex

	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			consumed, err := s.Consume(ctx, p.ID)
			if err != nil {
				if authErrCode(err) == "pairing_state" || authErrCode(err) == "pairing_not_found" {
					atomic.AddInt64(&fails, 1)
					return
				}
				t.Errorf("Consume: unexpected err %v", err)
				return
			}
			atomic.AddInt64(&wins, 1)
			mu.Lock()
			consumedID = consumed.ID
			mu.Unlock()
		}()
	}
	close(start)
	wg.Wait()

	if wins != 1 {
		t.Fatalf("consume wins = %d, want exactly 1 (fails=%d)", wins, fails)
	}
	if fails != n-1 {
		t.Fatalf("consume fails = %d, want %d (wins=%d)", fails, n-1, wins)
	}
	if consumedID != p.ID {
		t.Fatalf("consumed ID = %q, want %q", consumedID, p.ID)
	}

	// The canonical key now holds a consumed marker (not deleted) so a
	// second Consume or a re-Approve cannot resurrect the pairing.
	stillThere, err := s.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID after consume: %v (canonical key should hold consumed marker)", err)
	}
	if stillThere.Status != PairingConsumed {
		t.Fatalf("canonical status after consume = %q, want %q", stillThere.Status, PairingConsumed)
	}
	// The code-index key must be gone (deleted by the winner).
	if _, err := s.kv.Get(ctx, "pairing:code:"+p.Code); !errors.Is(err, kv.ErrKeyNotFound) {
		t.Fatalf("code key after consume: err=%v, want ErrKeyNotFound", err)
	}
	// A second Consume on the consumed marker must fail (not resurrect).
	if _, err := s.Consume(ctx, p.ID); err == nil {
		t.Fatal("second Consume after consumed: want error, got nil")
	}
}

// TestConsume_PendingPreservesPairing proves Consume on a pending (not yet
// approved) pairing returns ErrPairingNotApproved WITHOUT mutating the KV —
// the canonical record stays pending and readable so the user can still
// approve it. This is the critical regression guard: a premature /complete
// must not destroy a valid pending ceremony.
func TestConsume_PendingPreservesPairing(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	p := mustNewPairing(t, s)
	_, err := s.Consume(ctx, p.ID)
	if authErrCode(err) != "pairing_not_approved" {
		t.Fatalf("Consume on pending: err=%v, want ErrPairingNotApproved (pairing_not_approved)", err)
	}
	// The pairing must still be present and still pending.
	stillPending, err := s.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID after pending Consume: %v (pairing must be preserved)", err)
	}
	if stillPending.Status != PairingPending {
		t.Fatalf("status after pending Consume = %q, want %q (must not mutate)", stillPending.Status, PairingPending)
	}
	// The code-index key must still be present too.
	if _, err := s.kv.Get(ctx, "pairing:code:"+p.Code); err != nil {
		t.Fatalf("code key after pending Consume: %v (must be preserved)", err)
	}
	// The pairing must still be approvable after the failed Consume.
	loaded, err := s.LookupByCode(ctx, p.Code)
	if err != nil {
		t.Fatalf("LookupByCode after pending Consume: %v", err)
	}
	if err := s.Approve(ctx, loaded, 99); err != nil {
		t.Fatalf("Approve after pending Consume: %v (ceremony must still work)", err)
	}
}

// TestConsume_RejectsMissing proves Consume fails closed (pairing_not_found)
// when the canonical record is absent or already expired.
func TestConsume_RejectsMissing(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	_, err := s.Consume(ctx, "does-not-exist")
	if authErrCode(err) != "pairing_not_found" {
		t.Fatalf("Consume missing: err=%v, want ErrPairingNotFound", err)
	}
}

// TestConsume_RejectsMalformed proves Consume fails closed when the canonical
// record holds garbage bytes (corrupted KV entry). The malformed record is
// not CAS-swapped (the expected approved JSON cannot match garbage), so the
// key remains unchanged and the error surfaces without a crash.
func TestConsume_RejectsMalformed(t *testing.T) {
	s, mem := newTestStore(t)
	ctx := context.Background()
	const id = "malformed"
	if err := mem.SetEx(ctx, "pairing:id:"+id, "not-json", time.Minute); err != nil {
		t.Fatal(err)
	}
	_, err := s.Consume(ctx, id)
	if err == nil {
		t.Fatal("Consume malformed: want error, got nil")
	}
	// The malformed key must still be present — Consume did not Pop or CAS.
	if _, err := mem.Get(ctx, "pairing:id:"+id); err != nil {
		t.Fatalf("malformed canonical key after Consume: %v (CAS must not destroy malformed record)", err)
	}
}

// TestApprove_PreservesTTL ensures the CAS-based Approve does not extend the
// pairing's TTL window — the remaining TTL after approval must be <= the
// original and still positive.
func TestApprove_PreservesTTL(t *testing.T) {
	s, mem := newTestStore(t)
	ctx := context.Background()
	p := mustNewPairing(t, s)
	const acct = int32(3)
	loaded, err := s.LookupByCode(ctx, p.Code)
	if err != nil {
		t.Fatalf("LookupByCode: %v", err)
	}
	if err := s.Approve(ctx, loaded, acct); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	ttl, err := mem.TTL(ctx, "pairing:id:"+p.ID)
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 || ttl > int64(PairingTTL/time.Second) {
		t.Fatalf("TTL after Approve = %d, want (0, %d]", ttl, int64(PairingTTL/time.Second))
	}
}

// TestCancel_AfterApprovePreventsConsume ensures a cancelled (deleted)
// pairing cannot be consumed — the canonical key is gone, so Consume fails
// closed with pairing_not_found.
func TestCancel_AfterApprovePreventsConsume(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	p := mustNewPairing(t, s)
	const acct = int32(11)
	loaded, err := s.LookupByCode(ctx, p.Code)
	if err != nil {
		t.Fatalf("LookupByCode: %v", err)
	}
	if err := s.Approve(ctx, loaded, acct); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := s.Cancel(ctx, loaded); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if _, err := s.Consume(ctx, p.ID); authErrCode(err) != "pairing_not_found" {
		t.Fatalf("Consume after Cancel: err=%v, want ErrPairingNotFound", err)
	}
}
