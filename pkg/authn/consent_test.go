package authn

import (
	"context"
	"testing"
	"time"

	"prohibitorum/pkg/kv"
)

func TestConsentTicket_RoundTripPeekAndConsume(t *testing.T) {
	store := kv.NewMemoryStore()
	ctx := context.Background()
	tkt := ConsentTicket{AccountID: 7, ClientID: "rp1", Scopes: []string{"openid", "profile"}, RedirectURI: "https://rp/cb", State: "xyz"}

	nonce, err := DemandConsent(ctx, store, tkt)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := PeekConsent(ctx, store, nonce, 7)
	if err != nil || !ok {
		t.Fatalf("peek: ok=%v err=%v", ok, err)
	}
	if got.ClientID != "rp1" || len(got.Scopes) != 2 || got.State != "xyz" {
		t.Errorf("peek payload mismatch: %+v", got)
	}
	got2, ok, _ := PeekConsent(ctx, store, nonce, 7)
	if !ok || got2.ClientID != "rp1" {
		t.Error("second peek should still succeed (no consume)")
	}
	if _, ok, _ := PeekConsent(ctx, store, nonce, 99); ok {
		t.Error("peek with wrong account must fail")
	}
	c, ok, err := ConsumeConsent(ctx, store, nonce, 7)
	if err != nil || !ok || c.ClientID != "rp1" {
		t.Fatalf("consume: ok=%v err=%v c=%+v", ok, err, c)
	}
	if _, ok, _ := ConsumeConsent(ctx, store, nonce, 7); ok {
		t.Error("second consume must fail (single-use)")
	}
}

func TestConsentTicket_MissingAndMalformed(t *testing.T) {
	store := kv.NewMemoryStore()
	ctx := context.Background()
	if _, ok, _ := PeekConsent(ctx, store, "", 1); ok {
		t.Error("empty nonce must not peek")
	}
	if _, ok, _ := ConsumeConsent(ctx, store, "nope", 1); ok {
		t.Error("missing nonce must not consume")
	}

	// malformed JSON payload under a known nonce → treated as absent.
	if err := store.SetEx(ctx, consentKeyPrefix+"badkey", "{not valid json", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := PeekConsent(ctx, store, "badkey", 1); ok {
		t.Error("malformed JSON must not peek")
	}
	if _, ok, _ := ConsumeConsent(ctx, store, "badkey", 1); ok {
		t.Error("malformed JSON must not consume")
	}
}
