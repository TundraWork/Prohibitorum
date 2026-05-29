package oidc

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"prohibitorum/pkg/kv"
)

// sampleFamily builds a refreshFamily with every snapshot field populated,
// including slices and whole-second UTC times, so a KV round-trip can be
// deep-compared without sub-second or location drift. FamilyID/CurrentToken/
// IssuedAt are left zero because issueRefresh sets them.
func sampleFamily() refreshFamily {
	return refreshFamily{
		ClientID:  "client-123",
		AccountID: 42,
		SessionID: "sess-abc",
		Scope:     []string{"openid", "profile", "offline_access"},
		AuthTime:  time.Unix(1700000000, 0).UTC(),
		AMR:       []string{"pwd", "otp"},
		ACR:       "urn:mace:incommon:iap:silver",
	}
}

func TestRefreshIssueAndRotateHappy(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	orig := sampleFamily()
	t0, _, err := issueRefresh(ctx, store, orig)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}
	if t0 == "" {
		t.Fatal("issueRefresh returned empty token")
	}

	// Token→family mapping must exist under the documented key.
	fid, err := store.Get(ctx, refreshTokenKey(t0))
	if err != nil {
		t.Fatalf("expected token mapping at %q: %v", refreshTokenKey(t0), err)
	}
	// Family record must exist under the documented key.
	if _, err := store.Get(ctx, refreshFamilyKey(fid)); err != nil {
		t.Fatalf("expected family record at %q: %v", refreshFamilyKey(fid), err)
	}

	fam, newTok, err := rotateRefresh(ctx, store, t0)
	if err != nil {
		t.Fatalf("rotateRefresh(t0): %v", err)
	}
	if newTok == "" {
		t.Fatal("rotateRefresh returned empty new token")
	}
	if newTok == t0 {
		t.Fatalf("rotateRefresh returned same token as issued: %q", newTok)
	}
	if fam.CurrentToken != newTok {
		t.Errorf("family.CurrentToken: got %q, want %q", fam.CurrentToken, newTok)
	}
	if fam.FamilyID != fid {
		t.Errorf("family.FamilyID: got %q, want %q", fam.FamilyID, fid)
	}

	// Snapshot fields must survive the round-trip.
	if !fam.AuthTime.Equal(orig.AuthTime) {
		t.Errorf("AuthTime: got %v, want %v", fam.AuthTime, orig.AuthTime)
	}
	if fam.ClientID != orig.ClientID {
		t.Errorf("ClientID: got %q, want %q", fam.ClientID, orig.ClientID)
	}
	if fam.AccountID != orig.AccountID {
		t.Errorf("AccountID: got %d, want %d", fam.AccountID, orig.AccountID)
	}
	if fam.SessionID != orig.SessionID {
		t.Errorf("SessionID: got %q, want %q", fam.SessionID, orig.SessionID)
	}
	if !reflect.DeepEqual(fam.Scope, orig.Scope) {
		t.Errorf("Scope: got %v, want %v", fam.Scope, orig.Scope)
	}
	if !reflect.DeepEqual(fam.AMR, orig.AMR) {
		t.Errorf("AMR: got %v, want %v", fam.AMR, orig.AMR)
	}
	if fam.ACR != orig.ACR {
		t.Errorf("ACR: got %q, want %q", fam.ACR, orig.ACR)
	}

	// The old (now superseded) token mapping must be DELIBERATELY KEPT so a
	// later replay of it is detectable as reuse.
	if _, err := store.Get(ctx, refreshTokenKey(t0)); err != nil {
		t.Errorf("old token mapping was removed on rotation, want kept: %v", err)
	}
}

func TestRefreshReuseRevokesFamily(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	t0, _, err := issueRefresh(ctx, store, sampleFamily())
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	_, t1, err := rotateRefresh(ctx, store, t0)
	if err != nil {
		t.Fatalf("rotateRefresh(t0): %v", err)
	}

	// Replaying the superseded token must be detected as reuse.
	fam, tok, err := rotateRefresh(ctx, store, t0)
	if !errors.Is(err, errRefreshReuse) {
		t.Fatalf("rotateRefresh(superseded t0): got %v, want errRefreshReuse", err)
	}
	if fam != nil || tok != "" {
		t.Fatalf("reuse rotate returned non-zero result: fam=%v tok=%q", fam, tok)
	}

	// The reuse revokes the whole family, so the previously-current token is
	// now invalid (family record gone).
	if _, _, err := rotateRefresh(ctx, store, t1); !errors.Is(err, errRefreshInvalid) {
		t.Fatalf("rotateRefresh(t1) after reuse: got %v, want errRefreshInvalid", err)
	}
}

func TestRefreshRevokeFamily(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	t0, fid, err := issueRefresh(ctx, store, sampleFamily())
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	if err := revokeFamily(ctx, store, fid); err != nil {
		t.Fatalf("revokeFamily: %v", err)
	}

	if _, _, err := rotateRefresh(ctx, store, t0); !errors.Is(err, errRefreshInvalid) {
		t.Fatalf("rotateRefresh after revoke: got %v, want errRefreshInvalid", err)
	}
}

func TestRefreshLookup(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	t0, _, err := issueRefresh(ctx, store, sampleFamily())
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	fam, ok := lookupRefresh(ctx, store, t0)
	if !ok {
		t.Fatal("lookupRefresh: ok=false, want true")
	}
	if fam.CurrentToken != t0 {
		t.Errorf("lookupRefresh CurrentToken: got %q, want %q", fam.CurrentToken, t0)
	}

	// lookupRefresh must be READ-ONLY: a second call still returns true and a
	// subsequent rotate of the current token still succeeds (nothing consumed
	// or rotated).
	if _, ok := lookupRefresh(ctx, store, t0); !ok {
		t.Fatal("second lookupRefresh: ok=false, want true (must not mutate)")
	}
	if _, _, err := rotateRefresh(ctx, store, t0); err != nil {
		t.Fatalf("rotate after lookups: %v (lookup must not have mutated state)", err)
	}

	// Never-issued token → (nil, false).
	if got, ok := lookupRefresh(ctx, store, "never-issued"); ok || got != nil {
		t.Errorf("lookupRefresh(never-issued): got (%v, %v), want (nil, false)", got, ok)
	}
}

func TestRefreshLookupAfterRevoke(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	t0, _, err := issueRefresh(ctx, store, sampleFamily())
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}
	fam, ok := lookupRefresh(ctx, store, t0)
	if !ok {
		t.Fatal("lookupRefresh after issue: ok=false, want true")
	}
	if err := revokeFamily(ctx, store, fam.FamilyID); err != nil {
		t.Fatalf("revokeFamily: %v", err)
	}

	if got, ok := lookupRefresh(ctx, store, t0); ok || got != nil {
		t.Errorf("lookupRefresh after revoke: got (%v, %v), want (nil, false)", got, ok)
	}
}

func TestRefreshRotateUnknown(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	fam, tok, err := rotateRefresh(ctx, store, "never-issued")
	if !errors.Is(err, errRefreshInvalid) {
		t.Fatalf("rotateRefresh(unknown): got %v, want errRefreshInvalid", err)
	}
	if fam != nil || tok != "" {
		t.Fatalf("rotateRefresh(unknown) returned non-zero: fam=%v tok=%q", fam, tok)
	}
}

func TestRefreshLookupSupersededToken(t *testing.T) {
	// Documents that lookupRefresh resolves a superseded (post-rotation) token
	// to its live family, because the old token mapping is intentionally retained.
	// /introspect and /revoke (Task 11) rely on this: they must be able to look
	// up any token in the chain, not just the current one.
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	t0, _, err := issueRefresh(ctx, store, sampleFamily())
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	_, t1, err := rotateRefresh(ctx, store, t0)
	if err != nil {
		t.Fatalf("rotateRefresh(t0): %v", err)
	}

	// t0 is now superseded. lookupRefresh must still resolve it to the live family.
	fam, ok := lookupRefresh(ctx, store, t0)
	if !ok {
		t.Fatal("lookupRefresh(superseded t0): ok=false, want true")
	}
	if fam.CurrentToken != t1 {
		t.Errorf("lookupRefresh(superseded t0) CurrentToken: got %q, want t1 %q", fam.CurrentToken, t1)
	}
}

func TestRefreshDistinctTokens(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	a, _, err := issueRefresh(ctx, store, sampleFamily())
	if err != nil {
		t.Fatalf("issueRefresh #1: %v", err)
	}
	b, _, err := issueRefresh(ctx, store, sampleFamily())
	if err != nil {
		t.Fatalf("issueRefresh #2: %v", err)
	}
	if a == b {
		t.Fatalf("issueRefresh produced identical tokens: %q", a)
	}
}
