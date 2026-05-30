package authn

import (
	"context"
	"testing"
	"time"

	"prohibitorum/pkg/kv"
)

func TestReauthGateStaleSessionFails(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	old := time.Now().Add(-time.Hour) // session authenticated in the past
	nonce, err := DemandReauth(ctx, store, "oidc:reauth:")
	if err != nil {
		t.Fatalf("demand: %v", err)
	}
	ok, err := ConsumeReauth(ctx, store, "oidc:reauth:", nonce, old)
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
	nonce, _ := DemandReauth(ctx, store, "oidc:reauth:")
	ok, err := ConsumeReauth(ctx, store, "oidc:reauth:", nonce, time.Now().Add(time.Second))
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
	nonce, _ := DemandReauth(ctx, store, "oidc:reauth:")
	fresh := time.Now().Add(time.Second)
	if ok, _ := ConsumeReauth(ctx, store, "oidc:reauth:", nonce, fresh); !ok {
		t.Fatal("first consume should pass")
	}
	if ok, _ := ConsumeReauth(ctx, store, "oidc:reauth:", nonce, fresh); ok {
		t.Fatal("second consume of the same nonce must fail (single-use)")
	}
}

func TestReauthGateUnknownNonce(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	if ok, _ := ConsumeReauth(ctx, store, "oidc:reauth:", "never-issued", time.Now()); ok {
		t.Fatal("unknown nonce must not satisfy the gate")
	}
	if ok, _ := ConsumeReauth(ctx, store, "oidc:reauth:", "", time.Now()); ok {
		t.Fatal("empty nonce must not satisfy the gate")
	}
}
