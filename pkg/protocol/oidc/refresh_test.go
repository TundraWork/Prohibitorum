package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
	sessstore "prohibitorum/pkg/session"
)

// sampleFamily builds a refreshFamily with every snapshot field populated,
// including slices and whole-second UTC times, so a KV round-trip can be
// deep-compared without sub-second or location drift. FamilyID/CurrentHash/
// CreatedAt are left zero because issueRefresh sets them.
func sampleFamily() refreshFamily {
	return refreshFamily{
		Version:   refreshFamilyVersion,
		ClientID:  "client-123",
		AccountID: 42,
		SessionID: "sess-abc",
		Scope:     []string{"openid", "profile", "offline_access"},
		AuthTime:  time.Unix(1700000000, 0).UTC(),
		AMR:       []string{"pwd", "otp"},
		ACR:       "urn:mace:incommon:iap:silver",
	}
}

// testDEKs returns a fresh 32-byte DEK map for unit tests that exercise
// rotation (which seals the successor under the active DEK). Each test gets a
// distinct key so ciphertexts cannot leak across tests.
var testDEKs = oidcTestDEKs

// parseTokenSecret extracts the raw secret bytes from a prt1 token (for test
// assertions that need to verify hashes).
func parseTokenSecret(t *testing.T, token string) []byte {
	t.Helper()
	_, secret, err := parseRefreshToken(token)
	if err != nil {
		t.Fatalf("parseRefreshToken(%q): %v", token, err)
	}
	return secret
}

func TestRefreshIssueAndRotateHappy(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	orig := sampleFamily()
	t0, fid, err := issueRefresh(ctx, store, orig, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}
	if t0 == "" {
		t.Fatal("issueRefresh returned empty token")
	}
	if !strings.HasPrefix(t0, "prt1.") {
		t.Fatalf("issued token %q must have prt1. prefix", t0)
	}

	// Family record must exist under the documented key.
	famRaw, err := store.Get(ctx, refreshFamilyKey(fid))
	if err != nil {
		t.Fatalf("expected family record at %q: %v", refreshFamilyKey(fid), err)
	}
	// The family record must NOT contain the plaintext token.
	if strings.Contains(famRaw, t0) {
		t.Fatalf("family record contains the plaintext token %q", t0)
	}
	// No token→family mapping key may exist — the family ID is embedded in
	// the token, so there is no separate oidc:refresh:<token> key. ScanEntries
	// with a glob matching the token should return zero entries.
	scanRes, _ := store.ScanEntries(ctx, "oidc:refresh:"+t0, 0, 100)
	if len(scanRes.Entries) != 0 {
		t.Fatalf("token mapping key must not exist; found %d entries", len(scanRes.Entries))
	}

	fam, newTok, _, _, err := rotateRefresh(ctx, store, testDEKs, t0, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("rotateRefresh(t0, RefreshTokenTTL, RefreshTokenTTL): %v", err)
	}
	if newTok == "" {
		t.Fatal("rotateRefresh returned empty new token")
	}
	if newTok == t0 {
		t.Fatalf("rotateRefresh returned same token as issued: %q", newTok)
	}
	if !strings.HasPrefix(newTok, "prt1.") {
		t.Fatalf("rotated token %q must have prt1. prefix", newTok)
	}
	if fam.FamilyID != fid {
		t.Errorf("family.FamilyID: got %q, want %q", fam.FamilyID, fid)
	}

	// The new token's secret hash must match CurrentHash.
	newSecret := parseTokenSecret(t, newTok)
	newHash := hashSecret(newSecret)
	if fam.CurrentHash != newHash {
		t.Errorf("family.CurrentHash does not match new token's hash")
	}
	// The old token's secret hash must now match PreviousHash.
	oldSecret := parseTokenSecret(t, t0)
	oldHash := hashSecret(oldSecret)
	if fam.PreviousHash != oldHash {
		t.Errorf("family.PreviousHash does not match old token's hash")
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

	// The encrypted successor must be present and must decrypt to the new token.
	if fam.EncryptedSuccessor == "" {
		t.Fatal("family.EncryptedSuccessor must be set after rotation")
	}
	decrypted, derr := decryptFamilySuccessor(testDEKs, fam)
	if derr != nil {
		t.Fatalf("decryptFamilySuccessor: %v", derr)
	}
	if decrypted != newTok {
		t.Errorf("decrypted successor %q != newTok %q", decrypted, newTok)
	}
}

// TestRefreshNoPlaintextTokenInKV verifies that no plaintext usable refresh
// token appears anywhere in KV — neither in the family record nor as a
// separate mapping key. The family record stores only hashes and an encrypted
// successor.
func TestRefreshNoPlaintextTokenInKV(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	t0, fid, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	// Scan all refresh-related keys and assert the plaintext token is absent.
	scanAndAssertNoPlaintext(t, ctx, store, t0, fid)

	// Rotate and re-check: the old token's plaintext must not appear anywhere.
	_, t1, _, _, err := rotateRefresh(ctx, store, testDEKs, t0, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("rotateRefresh: %v", err)
	}
	scanAndAssertNoPlaintext(t, ctx, store, t0, fid)
	scanAndAssertNoPlaintext(t, ctx, store, t1, fid)
}

// scanAndAssertNoPlaintext scans all oidc:refresh:* keys and asserts that the
// plaintext token does not appear in any value or key.
func scanAndAssertNoPlaintext(t *testing.T, ctx context.Context, store kv.Store, token, fid string) {
	t.Helper()
	// Family record value must not contain the plaintext token.
	raw, err := store.Get(ctx, refreshFamilyKey(fid))
	if err != nil {
		t.Fatalf("family record missing for fid %q: %v", fid, err)
	}
	if strings.Contains(raw, token) {
		t.Errorf("plaintext token %q found in family record value", token)
	}
	// No mapping key may contain the token as a value.
	res, err := store.ScanEntries(ctx, "oidc:refresh:*", 0, 1000)
	if err != nil {
		t.Fatalf("ScanEntries: %v", err)
	}
	for _, e := range res.Entries {
		if strings.Contains(e.Value, token) {
			t.Errorf("plaintext token %q found in KV value at key %q", token, e.Key)
		}
	}
}

func TestRefreshReuseRevokesFamily(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	t0, _, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	_, t1, _, _, err := rotateRefresh(ctx, store, testDEKs, t0, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("rotateRefresh(t0, RefreshTokenTTL, RefreshTokenTTL): %v", err)
	}

	// Force-expire the idempotency window by zeroing PreviousValidUntil.
	{
		fam0, lerr := loadFamilyByFID(ctx, store, mustFID(t, t1))
		if lerr != nil {
			t.Fatalf("loadFamilyByFID for window-clear: %v", lerr)
		}
		fam0.PreviousValidUntil = time.Time{}
		if perr := putFamily(ctx, store, fam0, RefreshTokenTTL); perr != nil {
			t.Fatalf("putFamily for window-clear: %v", perr)
		}
	}

	// Replaying the superseded token must be detected as reuse.
	fam, tok, _, _, err := rotateRefresh(ctx, store, testDEKs, t0, RefreshTokenTTL, RefreshTokenTTL)
	if !errors.Is(err, errRefreshReuse) {
		t.Fatalf("rotateRefresh(superseded t0, RefreshTokenTTL, RefreshTokenTTL): got %v, want errRefreshReuse", err)
	}
	if fam != nil || tok != "" {
		t.Fatalf("reuse rotate returned non-zero result: fam=%v tok=%q", fam, tok)
	}

	// The reuse revokes the whole family, so the previously-current token is
	// now invalid (family record gone).
	if _, _, _, _, err := rotateRefresh(ctx, store, testDEKs, t1, RefreshTokenTTL, RefreshTokenTTL); !errors.Is(err, errRefreshInvalid) {
		t.Fatalf("rotateRefresh(t1, RefreshTokenTTL, RefreshTokenTTL) after reuse: got %v, want errRefreshInvalid", err)
	}
}

func TestRefreshRevokeFamily(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	t0, fid, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	if err := revokeFamily(ctx, store, fid); err != nil {
		t.Fatalf("revokeFamily: %v", err)
	}

	if _, _, _, _, err := rotateRefresh(ctx, store, testDEKs, t0, RefreshTokenTTL, RefreshTokenTTL); !errors.Is(err, errRefreshInvalid) {
		t.Fatalf("rotateRefresh after revoke: got %v, want errRefreshInvalid", err)
	}
}

func TestRefreshLookup(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	t0, _, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	fam, ok := lookupRefresh(ctx, store, t0)
	if !ok {
		t.Fatal("lookupRefresh: ok=false, want true")
	}
	// The current token's hash must match CurrentHash.
	secret := parseTokenSecret(t, t0)
	h := hashSecret(secret)
	if fam.CurrentHash != h {
		t.Errorf("lookupRefresh CurrentHash mismatch")
	}

	// lookupRefresh must be READ-ONLY: a second call still returns true and a
	// subsequent rotate of the current token still succeeds (nothing consumed
	// or rotated).
	if _, ok := lookupRefresh(ctx, store, t0); !ok {
		t.Fatal("second lookupRefresh: ok=false, want true (must not mutate)")
	}
	if _, _, _, _, err := rotateRefresh(ctx, store, testDEKs, t0, RefreshTokenTTL, RefreshTokenTTL); err != nil {
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

	t0, _, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL, RefreshTokenTTL)
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

	fam, tok, _, _, err := rotateRefresh(ctx, store, testDEKs, "never-issued", RefreshTokenTTL, RefreshTokenTTL)
	if !errors.Is(err, errRefreshInvalid) {
		t.Fatalf("rotateRefresh(unknown, RefreshTokenTTL, RefreshTokenTTL): got %v, want errRefreshInvalid", err)
	}
	if fam != nil || tok != "" {
		t.Fatalf("rotateRefresh(unknown, RefreshTokenTTL, RefreshTokenTTL) returned non-zero: fam=%v tok=%q", fam, tok)
	}
}

func TestRefreshLookupSupersededToken(t *testing.T) {
	// Documents that lookupRefresh resolves a superseded (post-rotation) token
	// to its live family, because the family ID is embedded in the token and
	// the family record is still present. /introspect and /revoke rely on this:
	// they must be able to look up any token in the chain, not just the current
	// one.
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	t0, _, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	_, t1, _, _, err := rotateRefresh(ctx, store, testDEKs, t0, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("rotateRefresh(t0, RefreshTokenTTL, RefreshTokenTTL): %v", err)
	}

	// t0 is now superseded. lookupRefresh must still resolve it to the live family.
	fam, ok := lookupRefresh(ctx, store, t0)
	if !ok {
		t.Fatal("lookupRefresh(superseded t0): ok=false, want true")
	}
	// The current hash must correspond to t1, not t0.
	t1Secret := parseTokenSecret(t, t1)
	t1Hash := hashSecret(t1Secret)
	if fam.CurrentHash != t1Hash {
		t.Errorf("lookupRefresh(superseded t0) CurrentHash does not match t1")
	}
}

func TestRefreshDistinctTokens(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	a, _, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh #1: %v", err)
	}
	b, _, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL, RefreshTokenTTL)
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
		Version:   refreshFamilyVersion,
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
	presented, _, err := issueRefresh(ctx, h.p.kv, fam, RefreshTokenTTL, RefreshTokenTTL)
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

	presented, _, err := issueRefresh(ctx, h.p.kv, grantFamily(), RefreshTokenTTL, RefreshTokenTTL)
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
		fam0, lerr := loadFamilyByFID(ctx, h.p.kv, mustFID(t, newToken))
		if lerr != nil {
			t.Fatalf("loadFamilyByFID for window-clear: %v", lerr)
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

	// A refresh_reuse audit record must be emitted and must carry the family's
	// AccountID (42 for grantFamily) for attribution.
	var sawReuse bool
	for _, r := range h.audit.records {
		if r.Detail["reason"] == "refresh_reuse" {
			sawReuse = true
			if r.AccountID == nil || *r.AccountID != 42 {
				t.Fatalf("refresh_reuse AccountID = %v, want 42", r.AccountID)
			}
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
	presented, _, err := issueRefresh(ctx, h.p.kv, fam, RefreshTokenTTL, RefreshTokenTTL)
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

	// The mismatch revokes the family: the token no longer resolves.
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

	presented, _, err := issueRefresh(ctx, h.p.kv, grantFamily(), RefreshTokenTTL, RefreshTokenTTL)
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

// TestTokenRefreshAppAccessDenied verifies the RBAC re-check at refresh: a user
// who is no longer authorized for the bound client (e.g. removed from an
// authorized group, or the client newly restricted) gets invalid_grant AND the
// rotating family is durably revoked, so no further refresh can succeed. The
// denial is audited with EventAccessDenied.
func TestTokenRefreshAppAccessDenied(t *testing.T) {
	h := newTokenHarness(t)
	ctx := context.Background()

	// The account is no longer authorized for testClientID.
	h.p.queries.(*fakeTokenQueries).deniedClients = map[string]bool{testClientID: true}

	presented, _, err := issueRefresh(ctx, h.p.kv, grantFamily(), RefreshTokenTTL, RefreshTokenTTL)
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

	// The family must be durably revoked: the presented token no longer
	// resolves to a live family.
	if _, ok := lookupRefresh(ctx, h.p.kv, presented); ok {
		t.Fatal("denied account's family must be revoked (durable cut)")
	}

	// A denial audit record (access_denied / account 42) must be emitted.
	var sawDenied bool
	for _, r := range h.audit.records {
		if r.Factor == audit.FactorOIDCClient && r.Event == audit.EventAccessDenied && r.Detail["reason"] == "app_access_denied" {
			sawDenied = true
			if r.AccountID == nil || *r.AccountID != 42 {
				t.Fatalf("access_denied AccountID = %v, want 42", r.AccountID)
			}
		}
	}
	if !sawDenied {
		t.Fatal("expected an app_access_denied audit record on refresh denial")
	}
}

// TestTokenRefreshAppAccessPredicateError verifies the refresh re-check fails
// CLOSED on a predicate error: server_error, no new token, and the family is
// preserved (we make no authorization claim, so we do not cut the family).
func TestTokenRefreshAppAccessPredicateError(t *testing.T) {
	h := newTokenHarness(t)
	ctx := context.Background()

	h.p.queries.(*fakeTokenQueries).authzErr = errors.New("db down")

	presented, _, err := issueRefresh(ctx, h.p.kv, grantFamily(), RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(refreshForm(presented)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 (fail closed), got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := decodeError(t, rec); got != errCodeServerError {
		t.Fatalf("want %s, got %s", errCodeServerError, got)
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
	presented, _, err := issueRefresh(ctx, h.p.kv, fam, RefreshTokenTTL, RefreshTokenTTL)
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

	// The family must be revoked: the presented token no longer resolves.
	if _, ok := lookupRefresh(ctx, h.p.kv, presented); ok {
		t.Fatal("deleted account's family should be revoked")
	}
}

// ── idempotency window + concurrency tests ─────────────────────────────────

// TestRotateIdempotentReplay verifies that presenting a just-rotated token
// within the idempotency window returns the SAME successor without a second
// mint, and does not revoke the family.
func TestRotateIdempotentReplay(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	r1, _, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	// First rotation: r1 → r2, rotated=true.
	fam1, r2, rotated1, _, err := rotateRefresh(ctx, store, testDEKs, r1, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("rotateRefresh(r1, RefreshTokenTTL, RefreshTokenTTL) first: %v", err)
	}
	if !rotated1 {
		t.Fatal("first rotateRefresh: want rotated=true")
	}
	if r2 == "" || r2 == r1 {
		t.Fatalf("first rotate: expected distinct new token, got %q", r2)
	}
	if !fam1.isActiveToken(r2, time.Now()) {
		t.Errorf("first rotate: r2 is not the active token")
	}

	// Idempotent replay: present r1 again within the window. CAS rotation
	// already completed; the presented token is now the previous token.
	fam2, tok2, rotated2, _, err := rotateRefresh(ctx, store, testDEKs, r1, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("rotateRefresh(r1, RefreshTokenTTL, RefreshTokenTTL) idempotent replay: %v", err)
	}
	if rotated2 {
		t.Fatal("idempotent replay: want rotated=false (no second mint)")
	}
	if tok2 != r2 {
		t.Fatalf("idempotent replay: got token %q, want same as first rotation %q", tok2, r2)
	}
	if !fam2.isActiveToken(r2, time.Now()) {
		t.Errorf("idempotent replay: r2 is not the active token")
	}

	// Family must still be alive: r2 is still the current token.
	famLive, ok := lookupRefresh(ctx, store, r2)
	if !ok {
		t.Fatal("family should still be live after idempotent replay")
	}
	if !famLive.isActiveToken(r2, time.Now()) {
		t.Errorf("post-replay: r2 is not the active token")
	}
}

// TestRotateReuseAfterWindow verifies that presenting a superseded token after
// the idempotency window has expired trips reuse detection and revokes the family.
func TestRotateReuseAfterWindow(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	r1, _, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	// Rotate r1 → r2.
	_, r2, _, _, err := rotateRefresh(ctx, store, testDEKs, r1, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("rotateRefresh(r1, RefreshTokenTTL, RefreshTokenTTL): %v", err)
	}

	// Force the idempotency window closed by zeroing PreviousValidUntil.
	fam, err := loadFamilyByFID(ctx, store, mustFID(t, r2))
	if err != nil {
		t.Fatalf("loadFamilyByFID(r2): %v", err)
	}
	fam.PreviousValidUntil = time.Time{} // expired: zero time is before any Now()
	if err := putFamily(ctx, store, fam, RefreshTokenTTL); err != nil {
		t.Fatalf("putFamily after window-clear: %v", err)
	}

	// Re-presenting r1 now trips reuse detection.
	famReuse, tokReuse, _, _, err := rotateRefresh(ctx, store, testDEKs, r1, RefreshTokenTTL, RefreshTokenTTL)
	if !errors.Is(err, errRefreshReuse) {
		t.Fatalf("rotateRefresh(r1, RefreshTokenTTL, RefreshTokenTTL) after window: got %v, want errRefreshReuse", err)
	}
	if famReuse != nil || tokReuse != "" {
		t.Fatalf("reuse should return nil fam/token; got fam=%v tok=%q", famReuse, tokReuse)
	}

	// Family must be dead: r2 no longer resolves.
	if _, _, _, _, err := rotateRefresh(ctx, store, testDEKs, r2, RefreshTokenTTL, RefreshTokenTTL); !errors.Is(err, errRefreshInvalid) {
		t.Fatalf("rotateRefresh(r2, RefreshTokenTTL, RefreshTokenTTL) after reuse: got %v, want errRefreshInvalid", err)
	}
}

// TestRotateConcurrentOneWinner verifies that 50 concurrent rotations of the
// same current token result in exactly ONE real rotation and 49 idempotent
// replays (the CAS losers serve the already-rotated successor), with NO
// reuse/revoke. This is the core CAS guarantee: no SetNX lock needed.
func TestRotateConcurrentOneWinner(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	r1, _, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	const n = 50
	type result struct {
		fam     *refreshFamily
		tok     string
		rotated bool
		err     error
	}

	results := make([]result, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			fam, tok, rotated, _, err := rotateRefresh(ctx, store, testDEKs, r1, RefreshTokenTTL, RefreshTokenTTL)
			results[i] = result{fam, tok, rotated, err}
		}(i)
	}
	wg.Wait()

	// Tally outcomes.
	var realRotations, idempotentReplays, reuses, errors_ int
	var successorToken string
	for _, res := range results {
		switch {
		case errors.Is(res.err, errRefreshReuse):
			reuses++
		case res.err == nil && res.rotated:
			realRotations++
			if successorToken == "" {
				successorToken = res.tok
			} else if res.tok != successorToken {
				t.Errorf("multiple distinct successor tokens: %q vs %q", successorToken, res.tok)
			}
		case res.err == nil && !res.rotated:
			idempotentReplays++
			if successorToken == "" {
				successorToken = res.tok
			} else if res.tok != successorToken {
				t.Errorf("idempotent replay returned different successor: %q vs %q", res.tok, successorToken)
			}
		default:
			errors_++
			t.Errorf("unexpected error from concurrent rotateRefresh: %v", res.err)
		}
	}

	// Never a reuse/revoke.
	if reuses != 0 {
		t.Fatalf("got %d errRefreshReuse outcomes; concurrent rotation must NEVER revoke", reuses)
	}
	if errors_ != 0 {
		t.Fatalf("got %d unexpected errors", errors_)
	}
	// Exactly one real rotation.
	if realRotations != 1 {
		t.Fatalf("got %d real rotations, want exactly 1", realRotations)
	}
	// The losers get idempotent replays (not reuse, not errors).
	if idempotentReplays != n-1 {
		t.Fatalf("got %d idempotent replays, want exactly %d", idempotentReplays, n-1)
	}

	// Family must still exist.
	if _, ok := lookupRefresh(ctx, store, r1); !ok {
		t.Fatal("family should still be alive after concurrent rotation")
	}

	// The successor token must be the current token.
	if successorToken == "" {
		t.Fatal("no successor token was produced")
	}
	famLive, ok := lookupRefresh(ctx, store, successorToken)
	if !ok {
		t.Fatal("family should resolve via the successor token")
	}
	if !famLive.isActiveToken(successorToken, time.Now()) {
		t.Error("successor token is not the active token after concurrent rotation")
	}
}

// TestCASFailureClassification verifies the CAS-loss classification logic:
// when a CAS fails because another caller won, the loser either gets the
// idempotent successor (if it was the current token) or reuse detection (if it
// was a stale token). This is driven through the rotateRefresh API.
func TestCASFailureClassification(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	r1, _, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	// Rotate r1 → r2 (the winner).
	_, r2, _, _, err := rotateRefresh(ctx, store, testDEKs, r1, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("rotate r1→r2: %v", err)
	}

	// Within the grace window, re-presenting r1 (the previous token) must
	// return r2 (idempotent replay, not reuse). This is the CAS-loss →
	// concurrent-exchange-won classification.
	fam, tok, rotated, _, err := rotateRefresh(ctx, store, testDEKs, r1, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("re-present r1 within window: got %v, want nil", err)
	}
	if rotated {
		t.Fatal("want rotated=false (idempotent replay)")
	}
	if tok != r2 {
		t.Fatalf("want successor %q, got %q", r2, tok)
	}
	if fam == nil {
		t.Fatal("want non-nil family on idempotent replay")
	}

	// After the window expires, r1 is reuse.
	{
		fam2, lerr := loadFamilyByFID(ctx, store, mustFID(t, r2))
		if lerr != nil {
			t.Fatalf("loadFamilyByFID for window-clear: %v", lerr)
		}
		fam2.PreviousValidUntil = time.Time{}
		if perr := putFamily(ctx, store, fam2, RefreshTokenTTL); perr != nil {
			t.Fatalf("putFamily for window-clear: %v", perr)
		}
	}
	_, _, _, _, err = rotateRefresh(ctx, store, testDEKs, r1, RefreshTokenTTL, RefreshTokenTTL)
	if !errors.Is(err, errRefreshReuse) {
		t.Fatalf("re-present r1 after window: got %v, want errRefreshReuse", err)
	}

	// The family is now revoked; r2 is invalid.
	if _, _, _, _, err := rotateRefresh(ctx, store, testDEKs, r2, RefreshTokenTTL, RefreshTokenTTL); !errors.Is(err, errRefreshInvalid) {
		t.Fatalf("rotate r2 after family revoked: got %v, want errRefreshInvalid", err)
	}
}

// TestReuseOutsideGraceRevokesFamily verifies that presenting a superseded token
// outside the grace window revokes the entire family (not just the token).
func TestReuseOutsideGraceRevokesFamily(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	r1, _, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}
	_, r2, _, _, err := rotateRefresh(ctx, store, testDEKs, r1, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("rotate r1→r2: %v", err)
	}

	// Expire the grace window.
	fam, err := loadFamilyByFID(ctx, store, mustFID(t, r2))
	if err != nil {
		t.Fatalf("loadFamily: %v", err)
	}
	fam.PreviousValidUntil = time.Time{}
	if err := putFamily(ctx, store, fam, RefreshTokenTTL); err != nil {
		t.Fatalf("putFamily: %v", err)
	}

	// Reuse r1 → revoke family.
	_, _, _, reuseAcct, err := rotateRefresh(ctx, store, testDEKs, r1, RefreshTokenTTL, RefreshTokenTTL)
	if !errors.Is(err, errRefreshReuse) {
		t.Fatalf("reuse: got %v, want errRefreshReuse", err)
	}
	if reuseAcct != 42 {
		t.Errorf("reuseAccountID = %d, want 42", reuseAcct)
	}

	// Family is revoked: r2 is now invalid.
	_, _, _, _, err = rotateRefresh(ctx, store, testDEKs, r2, RefreshTokenTTL, RefreshTokenTTL)
	if !errors.Is(err, errRefreshInvalid) {
		t.Fatalf("r2 after reuse revoke: got %v, want errRefreshInvalid", err)
	}
}

// TestDEKRotationOldKeyDecryptsSuccessor verifies that after a DEK rotation,
// the old DEK can still decrypt an encrypted successor sealed under it. This is
// the "DEK rotation" acceptance criterion: the family record stores the DEK
// version, and decryption uses that version (not the active one).
func TestDEKRotationOldKeyDecryptsSuccessor(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	// Start with DEK version 1.
	dekV1 := make([]byte, 32)
	for i := range dekV1 {
		dekV1[i] = 0x42
	}
	deksV1 := map[int][]byte{1: dekV1}

	r1, _, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	// Rotate under DEK v1 — successor is sealed with v1.
	_, r2, _, _, err := rotateRefresh(ctx, store, deksV1, r1, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("rotate under DEK v1: %v", err)
	}

	// Now rotate DEKs to v2 (the "active" DEK changes). The family record still
	// references v1 for its EncryptedSuccessor.
	dekV2 := make([]byte, 32)
	for i := range dekV2 {
		dekV2[i] = 0x99
	}
	deksV2 := map[int][]byte{1: dekV1, 2: dekV2}

	// Re-presenting r1 within the grace window must still decrypt the successor
	// using the OLD DEK (v1), even though the active DEK is now v2.
	_, tok, rotated, _, err := rotateRefresh(ctx, store, deksV2, r1, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("idempotent replay with rotated DEKs: %v", err)
	}
	if rotated {
		t.Fatal("want rotated=false (idempotent replay)")
	}
	if tok != r2 {
		t.Fatalf("want successor %q, got %q", r2, tok)
	}
}

// TestLegacyTokenAlwaysInvalidGrant verifies that tokens without the prt1
// prefix (the old opaque format) are always rejected as invalid_grant after
// deployment, regardless of whether they resolve to a family.
func TestLegacyTokenAlwaysInvalidGrant(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	// A legacy-format token (no prt1 prefix) must return errRefreshInvalid.
	_, _, _, _, err := rotateRefresh(ctx, store, testDEKs, "legacy-opaque-token", RefreshTokenTTL, RefreshTokenTTL)
	if !errors.Is(err, errRefreshInvalid) {
		t.Fatalf("legacy token: got %v, want errRefreshInvalid", err)
	}

	// lookupRefresh must also reject it.
	if _, ok := lookupRefresh(ctx, store, "legacy-opaque-token"); ok {
		t.Fatal("legacy token should not resolve via lookupRefresh")
	}

	// isActiveToken on a family must also reject a legacy token.
	fam := &refreshFamily{CurrentHash: hashSecret([]byte("anything"))}
	if fam.isActiveToken("legacy-opaque-token", time.Now()) {
		t.Fatal("legacy token should not be active")
	}
}

// TestIntrospectionCurrentPreviousSemantics verifies that introspection reports
// active:true for the current token and active:true for the previous token
// within the grace window, but active:false for a superseded token outside the
// window.
func TestIntrospectionCurrentPreviousSemantics(t *testing.T) {
	h := newEndpointHarness(t)
	ctx := context.Background()

	rt0, _, err := issueRefresh(ctx, h.p.kv, refreshFamily{
		ClientID:  testClientID,
		AccountID: 7,
		Scope:     []string{"openid", "offline_access"},
	}, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	// Current token (rt0) → active.
	rec := httptest.NewRecorder()
	h.p.HandleIntrospect(rec, introspectReq(rt0))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if body := decodeIntrospection(t, rec); body["active"] != true {
		t.Fatalf("current token: active = %v, want true", body["active"])
	}

	// Rotate rt0 → rt1. Now rt0 is the in-window previous token.
	_, rt1, _, _, err := rotateRefresh(ctx, h.p.kv, oidcTestDEKs, rt0, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("rotate rt0→rt1: %v", err)
	}

	// rt0 (in-window previous) → still active per RFC 7662 §2.2.
	rec = httptest.NewRecorder()
	h.p.HandleIntrospect(rec, introspectReq(rt0))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if body := decodeIntrospection(t, rec); body["active"] != true {
		t.Fatalf("in-window previous token: active = %v, want true", body["active"])
	}

	// rt1 (current) → active.
	rec = httptest.NewRecorder()
	h.p.HandleIntrospect(rec, introspectReq(rt1))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if body := decodeIntrospection(t, rec); body["active"] != true {
		t.Fatalf("current token: active = %v, want true", body["active"])
	}

	// Expire the grace window: rt0 is now a fully superseded token.
	fam, err := loadFamilyByFID(ctx, h.p.kv, mustFID(t, rt1))
	if err != nil {
		t.Fatalf("loadFamilyByFID: %v", err)
	}
	fam.PreviousValidUntil = time.Time{}
	if err := putFamily(ctx, h.p.kv, fam, RefreshTokenTTL); err != nil {
		t.Fatalf("putFamily: %v", err)
	}

	// rt0 (out-of-window superseded) → inactive.
	rec = httptest.NewRecorder()
	h.p.HandleIntrospect(rec, introspectReq(rt0))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if body := decodeIntrospection(t, rec); body["active"] != false {
		t.Fatalf("superseded token: active = %v, want false", body["active"])
	}
}

// TestRevocationDeletesFamily verifies that revoking a refresh token deletes
// the family record so every token in the chain becomes invalid.
func TestRevocationDeletesFamily(t *testing.T) {
	h := newEndpointHarness(t)
	ctx := context.Background()

	rt0, _, err := issueRefresh(ctx, h.p.kv, refreshFamily{
		ClientID:  testClientID,
		AccountID: 7,
		Scope:     []string{"openid", "offline_access"},
	}, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}
	// Rotate so there's a previous token too.
	_, rt1, _, _, err := rotateRefresh(ctx, h.p.kv, oidcTestDEKs, rt0, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}

	// Both tokens resolve before revocation.
	if _, ok := lookupRefresh(ctx, h.p.kv, rt0); !ok {
		t.Fatal("rt0 should resolve before revoke")
	}
	if _, ok := lookupRefresh(ctx, h.p.kv, rt1); !ok {
		t.Fatal("rt1 should resolve before revoke")
	}

	// Revoke rt1 (the current token).
	rec := httptest.NewRecorder()
	h.p.HandleRevoke(rec, revokeReq(rt1))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}

	// Both tokens must now be invalid (family record deleted).
	if _, ok := lookupRefresh(ctx, h.p.kv, rt0); ok {
		t.Fatal("rt0 should be dead after family revoke")
	}
	if _, ok := lookupRefresh(ctx, h.p.kv, rt1); ok {
		t.Fatal("rt1 should be dead after family revoke")
	}
	if !h.sawRevokedAudit("refresh_token") {
		t.Fatal("expected a revoked audit record for refresh_token")
	}
}

// mustFID extracts the family ID from a prt1 token, failing the test on error.
func mustFID(t *testing.T, token string) string {
	t.Helper()
	fid, _, err := parseRefreshToken(token)
	if err != nil {
		t.Fatalf("parseRefreshToken(%q): %v", token, err)
	}
	return fid
}


// ── Task 12: lifetime + originating-session enforcement ─────────────────────

// sessionTestHarness extends tokenHarness with a live SessionStore so the
// grantRefreshToken session-live check can be exercised.
func sessionTestHarness(t *testing.T) *tokenHarness {
	t.Helper()
	h := newTokenHarness(t)
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })
	h.p.sessions = sessstore.NewSessionStore(store, noopSessionQueriesStub{}, time.Hour)
	return h
}

// noopSessionQueriesStub satisfies session.SessionQueries for the test harness.
type noopSessionQueriesStub struct{}

func (noopSessionQueriesStub) InsertSession(context.Context, db.InsertSessionParams) (db.Session, error) {
	return db.Session{}, nil
}
func (noopSessionQueriesStub) RevokeSession(context.Context, string) error             { return nil }
func (noopSessionQueriesStub) RevokeAllSessionsByAccount(context.Context, int32) error { return nil }

// TestRefreshInactivityExpiry verifies that a family whose InactiveExpiresAt
// has passed is rejected with errRefreshExpiredInactivity (not errRefreshReuse
// or errRefreshInvalid). The family record is deleted on expiry.
func TestRefreshInactivityExpiry(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	t0, fid, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	// Force InactiveExpiresAt into the past while AbsoluteExpiresAt is still future.
	fam, err := loadFamilyByFID(ctx, store, fid)
	if err != nil {
		t.Fatalf("loadFamily: %v", err)
	}
	fam.InactiveExpiresAt = time.Now().Add(-time.Hour)
	fam.AbsoluteExpiresAt = time.Now().Add(24 * time.Hour)
	if err := putFamily(ctx, store, fam, RefreshTokenTTL); err != nil {
		t.Fatalf("putFamily: %v", err)
	}

	_, _, _, _, err = rotateRefresh(ctx, store, testDEKs, t0, RefreshTokenTTL, RefreshTokenTTL)
	if !errors.Is(err, errRefreshExpiredInactivity) {
		t.Fatalf("rotateRefresh(expired inactivity): got %v, want errRefreshExpiredInactivity", err)
	}

	// Family must be deleted.
	if _, err := loadFamilyByFID(ctx, store, fid); !errors.Is(err, errRefreshInvalid) {
		t.Fatalf("loadFamily after inactivity expiry: got %v, want errRefreshInvalid", err)
	}
}

// TestRefreshAbsoluteExpiry verifies that a family whose AbsoluteExpiresAt has
// passed is rejected with errRefreshExpiredAbsolute, even if InactiveExpiresAt
// is still in the future. The family record is deleted.
func TestRefreshAbsoluteExpiry(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	t0, fid, err := issueRefresh(ctx, store, sampleFamily(), RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	fam, err := loadFamilyByFID(ctx, store, fid)
	if err != nil {
		t.Fatalf("loadFamily: %v", err)
	}
	fam.AbsoluteExpiresAt = time.Now().Add(-time.Hour)
	fam.InactiveExpiresAt = time.Now().Add(24 * time.Hour)
	if err := putFamily(ctx, store, fam, RefreshTokenTTL); err != nil {
		t.Fatalf("putFamily: %v", err)
	}

	_, _, _, _, err = rotateRefresh(ctx, store, testDEKs, t0, RefreshTokenTTL, RefreshTokenTTL)
	if !errors.Is(err, errRefreshExpiredAbsolute) {
		t.Fatalf("rotateRefresh(expired absolute): got %v, want errRefreshExpiredAbsolute", err)
	}
	if _, err := loadFamilyByFID(ctx, store, fid); !errors.Is(err, errRefreshInvalid) {
		t.Fatalf("loadFamily after absolute expiry: got %v, want errRefreshInvalid", err)
	}
}

// TestRefreshRotationCapsInactiveAtAbsolute verifies that rotation slides the
// InactiveExpiresAt forward but never past the AbsoluteExpiresAt.
func TestRefreshRotationCapsInactiveAtAbsolute(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	// Issue with a short absolute (1h) and longer inactivity (2h) to force the cap.
	t0, fid, err := issueRefresh(ctx, store, sampleFamily(), 2*time.Hour, 1*time.Hour)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	fam, newTok, rotated, _, err := rotateRefresh(ctx, store, testDEKs, t0, 2*time.Hour, 1*time.Hour)
	if err != nil || !rotated {
		t.Fatalf("rotateRefresh: err=%v rotated=%v", err, rotated)
	}

	// The InactiveExpiresAt must be capped at AbsoluteExpiresAt (now+2h capped at
	// CreatedAt+1h). The AbsoluteExpiresAt must be unchanged.
	origFam, _ := loadFamilyByFID(ctx, store, fid)
	if !fam.AbsoluteExpiresAt.Equal(origFam.AbsoluteExpiresAt) {
		t.Errorf("AbsoluteExpiresAt changed: was %v, now %v (must be immutable)",
			origFam.AbsoluteExpiresAt, fam.AbsoluteExpiresAt)
	}
	if fam.InactiveExpiresAt.After(fam.AbsoluteExpiresAt) {
		t.Errorf("InactiveExpiresAt %v past AbsoluteExpiresAt %v (must be capped)",
			fam.InactiveExpiresAt, fam.AbsoluteExpiresAt)
	}
	_ = newTok
}

// TestTokenRefreshExpiredInactivityAudit verifies that the audit record carries
// the refresh_expired_inactivity reason when the family has been inactive too
// long.
func TestTokenRefreshExpiredInactivityAudit(t *testing.T) {
	h := sessionTestHarness(t)
	ctx := context.Background()

	presented, fid, err := issueRefresh(ctx, h.p.kv, grantFamily(), RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	// Force inactivity expiry.
	fam, _ := loadFamilyByFID(ctx, h.p.kv, fid)
	fam.InactiveExpiresAt = time.Now().Add(-time.Hour)
	fam.AbsoluteExpiresAt = time.Now().Add(24 * time.Hour)
	_ = putFamily(ctx, h.p.kv, fam, RefreshTokenTTL)

	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(refreshForm(presented)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}

	var sawExpiry bool
	for _, r := range h.audit.records {
		if r.Detail["reason"] == "refresh_expired_inactivity" {
			sawExpiry = true
		}
	}
	if !sawExpiry {
		t.Fatal("expected a refresh_expired_inactivity audit record")
	}
}

// TestTokenRefreshExpiredAbsoluteAudit verifies that the audit record carries
// the refresh_expired_absolute reason when the absolute deadline has passed.
func TestTokenRefreshExpiredAbsoluteAudit(t *testing.T) {
	h := sessionTestHarness(t)
	ctx := context.Background()

	presented, fid, err := issueRefresh(ctx, h.p.kv, grantFamily(), RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	fam, _ := loadFamilyByFID(ctx, h.p.kv, fid)
	fam.AbsoluteExpiresAt = time.Now().Add(-time.Hour)
	_ = putFamily(ctx, h.p.kv, fam, RefreshTokenTTL)

	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(refreshForm(presented)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}

	var sawExpiry bool
	for _, r := range h.audit.records {
		if r.Detail["reason"] == "refresh_expired_absolute" {
			sawExpiry = true
		}
	}
	if !sawExpiry {
		t.Fatal("expected a refresh_expired_absolute audit record")
	}
}

// TestTokenRefreshRevokedSessionRejectsAndRevokes verifies that a refresh token
// bound to a revoked originating session is rejected with invalid_grant AND the
// family is durably revoked, with a session_revoked audit reason.
func TestTokenRefreshRevokedSessionRejectsAndRevokes(t *testing.T) {
	h := sessionTestHarness(t)
	ctx := context.Background()

	// Issue a real session so IsSessionIDLive can find it.
	token, data, err := h.p.sessions.Issue(ctx, 42, "127.0.0.1", "", []string{"hwk"}, nil)
	if err != nil {
		t.Fatalf("session.Issue: %v", err)
	}

	// Issue a refresh family bound to that session.
	fam := grantFamily()
	fam.SessionID = data.SessionID
	presented, _, err := issueRefresh(ctx, h.p.kv, fam, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	// First refresh succeeds (session is live).
	rec1 := httptest.NewRecorder()
	h.p.HandleToken(rec1, tokenReq(refreshForm(presented)))
	if rec1.Code != http.StatusOK {
		t.Fatalf("first refresh want 200, got %d (%s)", rec1.Code, rec1.Body.String())
	}

	// Parse the rotated token from the first refresh.
	var resp1 tokenResponse
	_ = json.Unmarshal(rec1.Body.Bytes(), &resp1)

	// Revoke the session.
	if err := h.p.sessions.Revoke(ctx, 42, token); err != nil {
		t.Fatalf("session.Revoke: %v", err)
	}

	// Second refresh must fail: session is revoked.
	rec2 := httptest.NewRecorder()
	h.p.HandleToken(rec2, tokenReq(refreshForm(resp1.RefreshToken)))
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("refresh after session revoke want 400, got %d", rec2.Code)
	}
	if got := decodeError(t, rec2); got != errCodeInvalidGrant {
		t.Fatalf("want %s, got %s", errCodeInvalidGrant, got)
	}

	// A session_revoked audit record must be emitted.
	var sawSessionRevoke bool
	for _, r := range h.audit.records {
		if r.Detail["reason"] == "session_revoked" {
			sawSessionRevoke = true
		}
	}
	if !sawSessionRevoke {
		t.Fatal("expected a session_revoked audit record")
	}

	// The family must be durably revoked.
	if _, ok := lookupRefresh(ctx, h.p.kv, resp1.RefreshToken); ok {
		t.Fatal("family should be revoked after session_revoked denial")
	}
}

// TestTokenRefreshRevokeAllSessionsRejectsRefresh verifies that revoke-all-
// sessions makes the refresh family unusable on the next refresh.
func TestTokenRefreshRevokeAllSessionsRejectsRefresh(t *testing.T) {
	h := sessionTestHarness(t)
	ctx := context.Background()

	token, data, err := h.p.sessions.Issue(ctx, 42, "127.0.0.1", "", []string{"hwk"}, nil)
	if err != nil {
		t.Fatalf("session.Issue: %v", err)
	}

	fam := grantFamily()
	fam.SessionID = data.SessionID
	presented, _, err := issueRefresh(ctx, h.p.kv, fam, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	// First refresh succeeds.
	rec1 := httptest.NewRecorder()
	h.p.HandleToken(rec1, tokenReq(refreshForm(presented)))
	if rec1.Code != http.StatusOK {
		t.Fatalf("first refresh want 200, got %d", rec1.Code)
	}
	var resp1 tokenResponse
	_ = json.Unmarshal(rec1.Body.Bytes(), &resp1)

	// Revoke ALL sessions for the account (e.g. password change, recovery).
	if _, err := h.p.sessions.RevokeAllForAccount(ctx, 42); err != nil {
		t.Fatalf("RevokeAllForAccount: %v", err)
	}
	_ = token

	// Next refresh fails — session is dead.
	rec2 := httptest.NewRecorder()
	h.p.HandleToken(rec2, tokenReq(refreshForm(resp1.RefreshToken)))
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("refresh after revoke-all want 400, got %d", rec2.Code)
	}

	var sawSessionRevoke bool
	for _, r := range h.audit.records {
		if r.Detail["reason"] == "session_revoked" {
			sawSessionRevoke = true
		}
	}
	if !sawSessionRevoke {
		t.Fatal("expected a session_revoked audit record after revoke-all")
	}
}

// TestTokenRefreshDisabledAccountAuditReason verifies the audit reason is
// account_disabled (not account_unavailable) when the account is disabled.
func TestTokenRefreshDisabledAccountAuditReason(t *testing.T) {
	h := sessionTestHarness(t)
	ctx := context.Background()

	// Issue a live session so the session check passes.
	_, data, _ := h.p.sessions.Issue(ctx, 42, "127.0.0.1", "", []string{"hwk"}, nil)
	fam := grantFamily()
	fam.SessionID = data.SessionID
	presented, _, err := issueRefresh(ctx, h.p.kv, fam, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	// First refresh succeeds.
	rec1 := httptest.NewRecorder()
	h.p.HandleToken(rec1, tokenReq(refreshForm(presented)))
	if rec1.Code != http.StatusOK {
		t.Fatalf("first refresh want 200, got %d (%s)", rec1.Code, rec1.Body.String())
	}
	var resp1 tokenResponse
	_ = json.Unmarshal(rec1.Body.Bytes(), &resp1)

	// Disable the account.
	h.p.queries.(*fakeTokenQueries).accounts[42] = db.Account{ID: 42, Disabled: true}

	rec2 := httptest.NewRecorder()
	h.p.HandleToken(rec2, tokenReq(refreshForm(resp1.RefreshToken)))
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("refresh with disabled account want 400, got %d", rec2.Code)
	}

	var sawDisabled bool
	for _, r := range h.audit.records {
		if r.Detail["reason"] == "account_disabled" {
			sawDisabled = true
		}
	}
	if !sawDisabled {
		t.Fatal("expected an account_disabled audit record")
	}
}

// TestTokenRefreshDeletedAccountAuditReason verifies the audit reason is
// account_deleted (not account_unavailable) when the account no longer exists.
func TestTokenRefreshDeletedAccountAuditReason(t *testing.T) {
	h := sessionTestHarness(t)
	ctx := context.Background()

	_, data, _ := h.p.sessions.Issue(ctx, 42, "127.0.0.1", "", []string{"hwk"}, nil)
	fam := grantFamily()
	fam.SessionID = data.SessionID
	presented, _, err := issueRefresh(ctx, h.p.kv, fam, RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}

	rec1 := httptest.NewRecorder()
	h.p.HandleToken(rec1, tokenReq(refreshForm(presented)))
	if rec1.Code != http.StatusOK {
		t.Fatalf("first refresh want 200, got %d", rec1.Code)
	}
	var resp1 tokenResponse
	_ = json.Unmarshal(rec1.Body.Bytes(), &resp1)

	// Delete the account (remove from fake queries).
	delete(h.p.queries.(*fakeTokenQueries).accounts, 42)

	rec2 := httptest.NewRecorder()
	h.p.HandleToken(rec2, tokenReq(refreshForm(resp1.RefreshToken)))
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("refresh with deleted account want 400, got %d", rec2.Code)
	}

	var sawDeleted bool
	for _, r := range h.audit.records {
		if r.Detail["reason"] == "account_deleted" {
			sawDeleted = true
		}
	}
	if !sawDeleted {
		t.Fatal("expected an account_deleted audit record")
	}
}
