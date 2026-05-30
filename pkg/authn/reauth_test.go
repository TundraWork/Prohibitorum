package authn

import (
	"context"
	"testing"
	"time"

	"prohibitorum/pkg/kv"
)

// acct is the demanding account used across the single-account tests.
const acct = int32(42)

func TestReauthGateStaleSessionFails(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	old := time.Now().Add(-time.Hour) // session authenticated in the past
	nonce, err := DemandReauth(ctx, store, "oidc:reauth:", acct)
	if err != nil {
		t.Fatalf("demand: %v", err)
	}
	ok, err := ConsumeReauth(ctx, store, "oidc:reauth:", nonce, acct, old)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if ok {
		t.Fatal("stale auth_time must NOT satisfy the re-auth demand")
	}
}

func TestReauthGateFreshSessionPasses(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	nonce, _ := DemandReauth(ctx, store, "oidc:reauth:", acct)
	ok, err := ConsumeReauth(ctx, store, "oidc:reauth:", nonce, acct, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if !ok {
		t.Fatal("auth_time after the demand must satisfy it")
	}
}

func TestReauthGateSingleUse(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	nonce, _ := DemandReauth(ctx, store, "oidc:reauth:", acct)
	fresh := time.Now().Add(time.Second)
	if ok, _ := ConsumeReauth(ctx, store, "oidc:reauth:", nonce, acct, fresh); !ok {
		t.Fatal("first consume should pass")
	}
	if ok, _ := ConsumeReauth(ctx, store, "oidc:reauth:", nonce, acct, fresh); ok {
		t.Fatal("second consume of the same nonce must fail (single-use)")
	}
}

func TestReauthGateUnknownNonce(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	if ok, _ := ConsumeReauth(ctx, store, "oidc:reauth:", "never-issued", acct, time.Now()); ok {
		t.Fatal("unknown nonce must not satisfy the gate")
	}
	if ok, _ := ConsumeReauth(ctx, store, "oidc:reauth:", "", acct, time.Now()); ok {
		t.Fatal("empty nonce must not satisfy the gate")
	}
}

// TestReauthGateAccountMismatch: a marker demanded for account A must not be
// satisfied by a fresh session belonging to a different account B, even with the
// correct nonce. This closes the leaked-nonce footgun. The mismatched consume
// still consumes the single-use marker (atomic Pop), so a follow-up by A fails.
func TestReauthGateAccountMismatch(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	const acctA = int32(42)
	const acctB = int32(99)

	nonce, err := DemandReauth(ctx, store, "oidc:reauth:", acctA)
	if err != nil {
		t.Fatalf("demand: %v", err)
	}
	fresh := time.Now().Add(time.Second) // fresh authTime — only the account binding rejects it
	ok, err := ConsumeReauth(ctx, store, "oidc:reauth:", nonce, acctB, fresh)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if ok {
		t.Fatal("a marker bound to account A must NOT be satisfied by account B")
	}
	// The mismatched attempt already consumed the single-use marker.
	if ok, _ := ConsumeReauth(ctx, store, "oidc:reauth:", nonce, acctA, fresh); ok {
		t.Fatal("marker must be single-use even after a mismatched consume")
	}
}
