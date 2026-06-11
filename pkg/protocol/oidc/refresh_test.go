package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sync"
	"testing"
	"time"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/db"
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
	t0, _, err := issueRefresh(ctx, store, orig, RefreshTokenTTL)
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

	fam, newTok, _, err := rotateRefresh(ctx, store, t0, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("rotateRefresh(t0, RefreshTokenTTL): %v", err)
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

	t0, _, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	_, t1, _, err := rotateRefresh(ctx, store, t0, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("rotateRefresh(t0, RefreshTokenTTL): %v", err)
	}

	// Wait out the idempotency window so t0 is treated as genuine reuse, not replay.
	// Force-expire the window by zeroing PreviousValidUntil on the family record.
	{
		fam0, lerr := loadFamily(ctx, store, t1)
		if lerr != nil {
			t.Fatalf("loadFamily for window-clear: %v", lerr)
		}
		fam0.PreviousValidUntil = time.Time{}
		if perr := putFamily(ctx, store, fam0, RefreshTokenTTL); perr != nil {
			t.Fatalf("putFamily for window-clear: %v", perr)
		}
	}

	// Replaying the superseded token must be detected as reuse.
	fam, tok, _, err := rotateRefresh(ctx, store, t0, RefreshTokenTTL)
	if !errors.Is(err, errRefreshReuse) {
		t.Fatalf("rotateRefresh(superseded t0, RefreshTokenTTL): got %v, want errRefreshReuse", err)
	}
	if fam != nil || tok != "" {
		t.Fatalf("reuse rotate returned non-zero result: fam=%v tok=%q", fam, tok)
	}

	// The reuse revokes the whole family, so the previously-current token is
	// now invalid (family record gone).
	if _, _, _, err := rotateRefresh(ctx, store, t1, RefreshTokenTTL); !errors.Is(err, errRefreshInvalid) {
		t.Fatalf("rotateRefresh(t1, RefreshTokenTTL) after reuse: got %v, want errRefreshInvalid", err)
	}
}

func TestRefreshRevokeFamily(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	t0, fid, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	if err := revokeFamily(ctx, store, fid); err != nil {
		t.Fatalf("revokeFamily: %v", err)
	}

	if _, _, _, err := rotateRefresh(ctx, store, t0, RefreshTokenTTL); !errors.Is(err, errRefreshInvalid) {
		t.Fatalf("rotateRefresh after revoke: got %v, want errRefreshInvalid", err)
	}
}

func TestRefreshLookup(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	t0, _, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL)
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
	if _, _, _, err := rotateRefresh(ctx, store, t0, RefreshTokenTTL); err != nil {
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

	t0, _, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL)
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

	fam, tok, _, err := rotateRefresh(ctx, store, "never-issued", RefreshTokenTTL)
	if !errors.Is(err, errRefreshInvalid) {
		t.Fatalf("rotateRefresh(unknown, RefreshTokenTTL): got %v, want errRefreshInvalid", err)
	}
	if fam != nil || tok != "" {
		t.Fatalf("rotateRefresh(unknown, RefreshTokenTTL) returned non-zero: fam=%v tok=%q", fam, tok)
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

	t0, _, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	_, t1, _, err := rotateRefresh(ctx, store, t0, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("rotateRefresh(t0, RefreshTokenTTL): %v", err)
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

	a, _, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh #1: %v", err)
	}
	b, _, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh #2: %v", err)
	}
	if a == b {
		t.Fatalf("issueRefresh produced identical tokens: %q", a)
	}
}

// ── refresh_token grant (Task 10) ────────────────────────────────────────────

// grantFamily is a refreshFamily bound to the token harness's client (testClientID)
// and account (id 42), with offline_access granted, ready to be issued.
func grantFamily() refreshFamily {
	return refreshFamily{
		ClientID:  testClientID,
		AccountID: 42,
		SessionID: "sid-refresh",
		Scope:     []string{"openid", "profile", "offline_access"},
		AuthTime:  time.Unix(1700000000, 0).UTC(),
		AMR:       []string{"webauthn"},
		ACR:       "urn:acr:1",
	}
}

// refreshForm builds the refresh_token grant form for a presented token.
func refreshForm(token string) url.Values {
	v := url.Values{}
	v.Set("grant_type", "refresh_token")
	v.Set("refresh_token", token)
	return v
}

func TestTokenRefreshRotateHappy(t *testing.T) {
	h := newTokenHarness(t)
	ctx := context.Background()

	fam := grantFamily()
	presented, _, err := issueRefresh(ctx, h.p.kv, fam, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(refreshForm(presented)))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if cc := rec.Result().Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}

	var resp tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.TokenType != "Bearer" {
		t.Fatalf("token_type = %q", resp.TokenType)
	}
	if resp.ExpiresIn != int(AccessTokenTTL.Seconds()) {
		t.Fatalf("expires_in = %d", resp.ExpiresIn)
	}
	if resp.Scope != "openid profile offline_access" {
		t.Fatalf("scope = %q", resp.Scope)
	}
	// A NEW refresh token, distinct from the presented one.
	if resp.RefreshToken == "" || resp.RefreshToken == presented {
		t.Fatalf("expected a rotated refresh token != presented; got %q", resp.RefreshToken)
	}

	// Access + ID tokens verify.
	if _, _, err := h.p.verifyJWT(ctx, resp.AccessToken); err != nil {
		t.Fatalf("verify access token: %v", err)
	}
	idClaims, _, err := h.p.verifyJWT(ctx, resp.IDToken)
	if err != nil {
		t.Fatalf("verify id token: %v", err)
	}
	if idClaims["sid"] != fam.SessionID {
		t.Fatalf("id token sid = %v, want %v", idClaims["sid"], fam.SessionID)
	}
	if at, ok := idClaims["auth_time"].(float64); !ok || int64(at) != fam.AuthTime.Unix() {
		t.Fatalf("id token auth_time = %v, want %d", idClaims["auth_time"], fam.AuthTime.Unix())
	}
	amr, ok := idClaims["amr"].([]any)
	if !ok || len(amr) != 1 || amr[0] != "webauthn" {
		t.Fatalf("id token amr = %v, want [webauthn]", idClaims["amr"])
	}
	// Refresh grant snapshots no nonce → claim must be absent.
	if _, present := idClaims["nonce"]; present {
		t.Fatalf("id token must omit nonce on refresh grant, got %v", idClaims["nonce"])
	}

	// A refresh_rotated audit record (account 42) must be emitted.
	var sawRotated bool
	for _, r := range h.audit.records {
		if r.Factor == audit.FactorOIDCClient && r.Detail["reason"] == "refresh_rotated" {
			sawRotated = true
			if r.AccountID == nil || *r.AccountID != 42 {
				t.Fatalf("refresh_rotated AccountID = %v, want 42", r.AccountID)
			}
		}
	}
	if !sawRotated {
		t.Fatal("expected a refresh_rotated audit record")
	}
}

func TestTokenRefreshReuseRevokesFamily(t *testing.T) {
	h := newTokenHarness(t)
	ctx := context.Background()

	presented, _, err := issueRefresh(ctx, h.p.kv, grantFamily(), RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	// First rotation succeeds and yields a new (now-current) token.
	rec1 := httptest.NewRecorder()
	h.p.HandleToken(rec1, tokenReq(refreshForm(presented)))
	if rec1.Code != http.StatusOK {
		t.Fatalf("first rotate want 200, got %d (%s)", rec1.Code, rec1.Body.String())
	}
	var resp1 tokenResponse
	if err := json.Unmarshal(rec1.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("decode: %v", err)
	}
	newToken := resp1.RefreshToken

	// Expire the idempotency window so re-presenting `presented` is genuine reuse.
	{
		fam0, lerr := loadFamily(ctx, h.p.kv, newToken)
		if lerr != nil {
			t.Fatalf("loadFamily for window-clear: %v", lerr)
		}
		fam0.PreviousValidUntil = time.Time{}
		if perr := putFamily(ctx, h.p.kv, fam0, RefreshTokenTTL); perr != nil {
			t.Fatalf("putFamily for window-clear: %v", perr)
		}
	}

	// Replaying the OLD (superseded) token trips reuse detection.
	rec2 := httptest.NewRecorder()
	h.p.HandleToken(rec2, tokenReq(refreshForm(presented)))
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("reuse want 400, got %d", rec2.Code)
	}
	if got := decodeError(t, rec2); got != errCodeInvalidGrant {
		t.Fatalf("reuse want %s, got %s", errCodeInvalidGrant, got)
	}

	// A refresh_reuse audit record must be emitted (no account context on reuse).
	var sawReuse bool
	for _, r := range h.audit.records {
		if r.Detail["reason"] == "refresh_reuse" {
			sawReuse = true
		}
	}
	if !sawReuse {
		t.Fatal("expected a refresh_reuse audit record")
	}

	// The family is dead: rotating the previously-current token now fails.
	rec3 := httptest.NewRecorder()
	h.p.HandleToken(rec3, tokenReq(refreshForm(newToken)))
	if rec3.Code != http.StatusBadRequest {
		t.Fatalf("post-reuse rotate of new token want 400, got %d", rec3.Code)
	}
	if got := decodeError(t, rec3); got != errCodeInvalidGrant {
		t.Fatalf("post-reuse want %s, got %s", errCodeInvalidGrant, got)
	}
}

func TestTokenRefreshWrongClient(t *testing.T) {
	h := newTokenHarness(t)
	ctx := context.Background()

	// Family bound to a DIFFERENT client than the one that will authenticate.
	fam := grantFamily()
	fam.ClientID = "other-client"
	presented, _, err := issueRefresh(ctx, h.p.kv, fam, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	// Authenticate as testClientID (the harness client) — a mismatch.
	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(refreshForm(presented)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := decodeError(t, rec); got != errCodeInvalidGrant {
		t.Fatalf("want %s, got %s", errCodeInvalidGrant, got)
	}

	// The mismatch revokes the family: the rotated token no longer resolves.
	// (rotateRefresh ran first, so the presented token is superseded; the live
	// token is the family's CurrentToken, which lookupRefresh would resolve had
	// the family survived. Asserting via a fresh rotate of the presented token
	// would only trip reuse, so check the family record directly.)
	famNow, ok := lookupRefresh(ctx, h.p.kv, presented)
	if ok {
		t.Fatalf("family should be revoked after client mismatch; resolved to %+v", famNow)
	}
}

func TestTokenRefreshDisabledAccount(t *testing.T) {
	h := newTokenHarness(t)
	ctx := context.Background()

	// Disable the account the family is bound to.
	h.p.queries.(*fakeTokenQueries).accounts[42] = db.Account{ID: 42, Disabled: true}

	presented, _, err := issueRefresh(ctx, h.p.kv, grantFamily(), RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(refreshForm(presented)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	if got := decodeError(t, rec); got != errCodeInvalidGrant {
		t.Fatalf("want %s, got %s", errCodeInvalidGrant, got)
	}

	// A disabled account's family must be revoked.
	if _, ok := lookupRefresh(ctx, h.p.kv, presented); ok {
		t.Fatal("disabled account's family should be revoked")
	}
}

func TestTokenRefreshUnknownToken(t *testing.T) {
	h := newTokenHarness(t)

	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(refreshForm("never-issued-garbage-token")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	if got := decodeError(t, rec); got != errCodeInvalidGrant {
		t.Fatalf("want %s, got %s", errCodeInvalidGrant, got)
	}
}

func TestTokenRefreshAccountNotFound(t *testing.T) {
	h := newTokenHarness(t)
	ctx := context.Background()

	// Issue a family bound to account 999, which is absent from fakeTokenQueries
	// (GetAccountByID returns pgx.ErrNoRows for unknown IDs).
	fam := grantFamily()
	fam.AccountID = 999
	presented, _, err := issueRefresh(ctx, h.p.kv, fam, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(refreshForm(presented)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := decodeError(t, rec); got != errCodeInvalidGrant {
		t.Fatalf("want %s, got %s", errCodeInvalidGrant, got)
	}

	// The family must be revoked: the presented token (now superseded after
	// rotation inside grantRefreshToken) no longer resolves.
	if _, ok := lookupRefresh(ctx, h.p.kv, presented); ok {
		t.Fatal("deleted account's family should be revoked")
	}
}

// ── idempotency window + concurrency tests (Task 2) ─────────────────────────

// TestRotateIdempotentReplay verifies that presenting a just-rotated token
// within the idempotency window returns the SAME successor without a second
// mint, and does not revoke the family.
func TestRotateIdempotentReplay(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	r1, _, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	// First rotation: r1 → r2, rotated=true.
	fam1, r2, rotated1, err := rotateRefresh(ctx, store, r1, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("rotateRefresh(r1, RefreshTokenTTL) first: %v", err)
	}
	if !rotated1 {
		t.Fatal("first rotateRefresh: want rotated=true")
	}
	if r2 == "" || r2 == r1 {
		t.Fatalf("first rotate: expected distinct new token, got %q", r2)
	}
	if fam1.CurrentToken != r2 {
		t.Errorf("first rotate: family.CurrentToken=%q, want r2=%q", fam1.CurrentToken, r2)
	}

	// Idempotent replay: present r1 again within the window.
	// rotateRefresh deleted its own lock after the first call, so SetNX re-acquires.
	fam2, tok2, rotated2, err := rotateRefresh(ctx, store, r1, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("rotateRefresh(r1, RefreshTokenTTL) idempotent replay: %v", err)
	}
	if rotated2 {
		t.Fatal("idempotent replay: want rotated=false (no second mint)")
	}
	if tok2 != r2 {
		t.Fatalf("idempotent replay: got token %q, want same as first rotation %q", tok2, r2)
	}
	if fam2.CurrentToken != r2 {
		t.Errorf("idempotent replay: family.CurrentToken=%q, want r2=%q", fam2.CurrentToken, r2)
	}

	// Family must still be alive: r2 is still the current token.
	famLive, ok := lookupRefresh(ctx, store, r2)
	if !ok {
		t.Fatal("family should still be live after idempotent replay")
	}
	if famLive.CurrentToken != r2 {
		t.Errorf("post-replay family.CurrentToken=%q, want %q", famLive.CurrentToken, r2)
	}
}

// TestRotateReuseAfterWindow verifies that presenting a superseded token after
// the idempotency window has expired trips reuse detection and revokes the family.
func TestRotateReuseAfterWindow(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	r1, _, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	// Rotate r1 → r2.
	_, r2, _, err := rotateRefresh(ctx, store, r1, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("rotateRefresh(r1, RefreshTokenTTL): %v", err)
	}

	// Force the idempotency window closed by zeroing PreviousValidUntil.
	fam, err := loadFamily(ctx, store, r2)
	if err != nil {
		t.Fatalf("loadFamily(r2): %v", err)
	}
	fam.PreviousValidUntil = time.Time{} // expired: zero time is before any Now()
	if err := putFamily(ctx, store, fam, RefreshTokenTTL); err != nil {
		t.Fatalf("putFamily after window-clear: %v", err)
	}

	// Re-presenting r1 now trips reuse detection.
	famReuse, tokReuse, _, err := rotateRefresh(ctx, store, r1, RefreshTokenTTL)
	if !errors.Is(err, errRefreshReuse) {
		t.Fatalf("rotateRefresh(r1, RefreshTokenTTL) after window: got %v, want errRefreshReuse", err)
	}
	if famReuse != nil || tokReuse != "" {
		t.Fatalf("reuse should return nil fam/token; got fam=%v tok=%q", famReuse, tokReuse)
	}

	// Family must be dead: r2 no longer resolves.
	if _, _, _, err := rotateRefresh(ctx, store, r2, RefreshTokenTTL); !errors.Is(err, errRefreshInvalid) {
		t.Fatalf("rotateRefresh(r2, RefreshTokenTTL) after reuse: got %v, want errRefreshInvalid", err)
	}
}

// TestRotateConcurrent verifies that two concurrent rotations of the same
// current token result in exactly one real rotation and one benign outcome
// (either errRotationInProgress or an idempotent replay), with NO reuse/revoke.
func TestRotateConcurrent(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	r1, _, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	type result struct {
		fam     *refreshFamily
		tok     string
		rotated bool
		err     error
	}

	results := make([]result, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for i := range 2 {
		go func(i int) {
			defer wg.Done()
			fam, tok, rotated, err := rotateRefresh(ctx, store, r1, RefreshTokenTTL)
			results[i] = result{fam, tok, rotated, err}
		}(i)
	}
	wg.Wait()

	// Tally outcomes.
	var realRotations, inProgress, idempotentReplays, reuses int
	for _, res := range results {
		switch {
		case errors.Is(res.err, errRefreshReuse):
			reuses++
		case errors.Is(res.err, errRotationInProgress):
			inProgress++
		case res.err == nil && res.rotated:
			realRotations++
		case res.err == nil && !res.rotated:
			idempotentReplays++
		default:
			t.Errorf("unexpected error from concurrent rotateRefresh: %v", res.err)
		}
	}

	// Never a reuse/revoke.
	if reuses != 0 {
		t.Fatalf("got %d errRefreshReuse outcomes; concurrent rotation must NEVER revoke", reuses)
	}
	// Exactly one real rotation.
	if realRotations != 1 {
		t.Fatalf("got %d real rotations, want exactly 1", realRotations)
	}
	// The loser gets inProgress or idempotent replay, not reuse.
	benignLoser := inProgress + idempotentReplays
	if benignLoser != 1 {
		t.Fatalf("got %d benign-loser outcomes (inProgress=%d, idempotent=%d), want exactly 1",
			benignLoser, inProgress, idempotentReplays)
	}

	// Family must still exist.
	if _, ok := lookupRefresh(ctx, store, r1); !ok {
		t.Fatal("family should still be alive after concurrent rotation")
	}
}

// errFakeSetNX is the sentinel error the fakeSetNXStore injects.
var errFakeSetNX = errors.New("kv: injected SetNX error")

// fakeSetNXStore wraps a MemoryStore and replaces SetNX with a failing stub.
type fakeSetNXStore struct {
	*kv.MemoryStore
}

func (f *fakeSetNXStore) SetNX(_ context.Context, _, _ string, _ time.Duration) (bool, error) {
	return false, errFakeSetNX
}

// TestRotateSetNXError verifies that a SetNX backend error causes rotateRefresh
// to return that error without mutating the family record.
func TestRotateSetNXError(t *testing.T) {
	ctx := context.Background()
	real := kv.NewMemoryStore()
	t.Cleanup(func() { _ = real.Close() })
	store := &fakeSetNXStore{real}

	r1, _, err := issueRefresh(ctx, real, sampleFamily(), RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	// rotateRefresh must fail closed: return the injected error, no rotation, no revoke.
	fam, tok, rotated, err := rotateRefresh(ctx, store, r1, RefreshTokenTTL)
	if !errors.Is(err, errFakeSetNX) {
		t.Fatalf("want errFakeSetNX, got %v", err)
	}
	if fam != nil || tok != "" || rotated {
		t.Fatalf("SetNX error should produce nil fam/token and rotated=false; got fam=%v tok=%q rotated=%v",
			fam, tok, rotated)
	}

	// Family must still be intact and loadable via the original token.
	famLive, ok := lookupRefresh(ctx, real, r1)
	if !ok {
		t.Fatal("family should still be intact after SetNX error")
	}
	if famLive.CurrentToken != r1 {
		t.Errorf("family.CurrentToken=%q, want r1=%q", famLive.CurrentToken, r1)
	}
}
