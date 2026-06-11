package oidc

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"prohibitorum/pkg/kv"
)

// sampleAuthCode builds an authCode with every field populated, including
// slices and a whole-second UTC AuthTime, so round-trips can be deep-compared
// without sub-second or location drift.
func sampleAuthCode() authCode {
	return authCode{
		ClientID:            "client-123",
		AccountID:           42,
		SessionID:           "sess-abc",
		RedirectURI:         "https://rp.example.com/callback",
		Scope:               []string{"openid", "profile", "email"},
		Nonce:               "nonce-xyz",
		CodeChallenge:       "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		CodeChallengeMethod: "S256",
		AuthTime:            time.Unix(1700000000, 0).UTC(),
		AMR:                 []string{"pwd", "otp"},
		ACR:                 "urn:mace:incommon:iap:silver",
	}
}

func TestCodesMintConsumeRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	orig := sampleAuthCode()
	code, err := mintCode(ctx, store, orig, AuthorizationCodeTTL)
	if err != nil {
		t.Fatalf("mintCode: %v", err)
	}
	if code == "" {
		t.Fatal("mintCode returned empty code")
	}

	// The minted code must be stored under the documented key format.
	if _, err := store.Get(ctx, codeKey(code)); err != nil {
		t.Fatalf("expected code stored at %q, Get failed: %v", codeKey(code), err)
	}

	got, err := consumeCode(ctx, store, code)
	if err != nil {
		t.Fatalf("consumeCode: %v", err)
	}
	if got == nil {
		t.Fatal("consumeCode returned nil authCode")
	}

	// AuthTime needs a value-equality check (reflect.DeepEqual on time.Time
	// can differ by monotonic/location); compare it separately then blank it.
	if !got.AuthTime.Equal(orig.AuthTime) {
		t.Errorf("AuthTime: got %v, want %v", got.AuthTime, orig.AuthTime)
	}
	got.AuthTime = orig.AuthTime
	if !reflect.DeepEqual(*got, orig) {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", *got, orig)
	}
}

func TestCodesSingleUse(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	code, err := mintCode(ctx, store, sampleAuthCode(), AuthorizationCodeTTL)
	if err != nil {
		t.Fatalf("mintCode: %v", err)
	}

	if _, err := consumeCode(ctx, store, code); err != nil {
		t.Fatalf("first consumeCode: %v", err)
	}

	// A second consume of the same code must miss (single-use).
	_, err = consumeCode(ctx, store, code)
	if !errors.Is(err, errCodeNotFound) {
		t.Fatalf("second consumeCode: got %v, want errCodeNotFound", err)
	}
}

func TestCodesConsumeUnknown(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	got, err := consumeCode(ctx, store, "no-such-code")
	if !errors.Is(err, errCodeNotFound) {
		t.Fatalf("got err %v, want errCodeNotFound", err)
	}
	if got != nil {
		t.Fatalf("got %+v, want nil authCode", got)
	}
}

func TestCodesMarkAndReadUsedFamily(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	const code = "consumed-code"
	const family = "family-99"

	if err := markCodeUsed(ctx, store, code, family, AuthorizationCodeTTL); err != nil {
		t.Fatalf("markCodeUsed: %v", err)
	}

	gotFamily, ok := usedFamily(ctx, store, code)
	if !ok {
		t.Fatal("usedFamily: ok=false, want true")
	}
	if gotFamily != family {
		t.Errorf("usedFamily: got %q, want %q", gotFamily, family)
	}
}

func TestCodesUsedFamilyUnmarked(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	gotFamily, ok := usedFamily(ctx, store, "never-marked")
	if ok {
		t.Errorf("usedFamily: ok=true, want false")
	}
	if gotFamily != "" {
		t.Errorf("usedFamily: got %q, want empty", gotFamily)
	}
}

func TestCodesDistinctCodes(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	c1, err := mintCode(ctx, store, sampleAuthCode(), AuthorizationCodeTTL)
	if err != nil {
		t.Fatalf("mintCode #1: %v", err)
	}
	c2, err := mintCode(ctx, store, sampleAuthCode(), AuthorizationCodeTTL)
	if err != nil {
		t.Fatalf("mintCode #2: %v", err)
	}
	if c1 == c2 {
		t.Fatalf("mintCode produced identical codes: %q", c1)
	}
}
