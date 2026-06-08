# Backend Security Hardening (Tier 2) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the documented backend atomicity / lockout / timing gaps: add an atomic `SetNX` KV primitive, make refresh-token rotation race-safe and victim-lockout-proof, make SAML AuthnRequest replay atomic, transaction-wrap + lockout-guard factor removal (both paths), and close the OIDC client-id timing oracle.

**Architecture:** Backend-only Go; no migration, no frontend. One new `kv.Store` method (`SetNX`) underpins the two race fixes. Refresh rotation uses a `SetNX` rotation lock + a previous-token idempotency window stored on the family record (re-mint on benign replay — no cached bearer envelope, consistent with the already-raw-in-KV design). Factor removal serializes on the `account` row via the existing `GetAccountByIDForUpdate` query inside a pgx transaction.

**Tech Stack:** Go, `pkg/kv` (ttlcache + go-redis), `pkg/protocol/oidc`, `pkg/protocol/saml`, `pkg/authn`, sqlc (`pkg/db`), pgx/pgxpool.

**Cross-cutting conventions:**
- Run from repo root `/home/tundra/projects/tundra/prohibitorum`. Commit per task (no remote; commit directly to master). End each commit message with `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- **`go build ./... && go vet ./...` exit 0 is the authoritative gate** over any gopls "undefined" noise — Task 4 runs `sqlc generate`, after which the IDE gopls may falsely flag the new method until reloaded; trust the build.
- Verify per task: `go test ./<pkg>/... ` then at the end `go build/vet ./...` + full `go test ./...` + `cmd/smoke` SMOKE_EXIT=0.

---

### Task 1: `SetNX` KV primitive

**Goal:** Add atomic set-if-absent to `kv.Store` with both backends + concurrency tests.

**Files:**
- Modify: `pkg/kv/store.go` (interface)
- Modify: `pkg/kv/memory.go` (mutex-guarded impl)
- Modify: `pkg/kv/redis.go` (native impl)
- Test: `pkg/kv/memory_test.go`

**Acceptance Criteria:**
- [ ] `SetNX(ctx, key, value, ttl) (bool, error)` on the interface + both backends.
- [ ] Returns `true` iff it set the key; `false` if the key already exists (expired = absent).
- [ ] `ttl <= 0` returns an error.
- [ ] 100 concurrent `SetNX` for the same key → exactly one `true` (memory backend test).

**Verify:** `go test ./pkg/kv/...` → PASS.

**Steps:**

- [ ] **Step 1: Add the interface method** to `pkg/kv/store.go` after `Pop` (line 52):
```go
	// SetNX atomically sets key=value with the given ttl ONLY if key does not
	// already exist (an expired key counts as absent). Returns true if it set
	// the key, false if the key already existed. ttl MUST be > 0. A backend
	// error returns (false, err) so callers can fail closed.
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
```

- [ ] **Step 2: Memory impl.** In `pkg/kv/memory.go`, add `"errors"` and `"sync"` imports, a mutex field, and the method:
```go
// MemoryStore struct gains:
//   mu sync.Mutex
```
Add `mu sync.Mutex` to the `MemoryStore` struct (line 13-15 block). Then:
```go
// SetNX atomically sets key=value with ttl only if key is absent. The mutex
// serialises the Get→Set against other SetNX callers; ttlcache's Get returns
// nil for expired items (WithDisableTouchOnHit means Get has no side effect),
// so an expired key is correctly treated as absent.
func (m *MemoryStore) SetNX(_ context.Context, key, value string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, errors.New("kv: SetNX ttl must be positive")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if item := m.cache.Get(key); item != nil {
		return false, nil
	}
	m.cache.Set(key, value, ttl)
	return true, nil
}
```

- [ ] **Step 3: Redis impl.** In `pkg/kv/redis.go`, add `"errors"` to imports, then:
```go
// SetNX atomically sets key=value with ttl only if key is absent, via Redis
// SET ... NX (go-redis SetNX). An expired key is absent. ttl must be > 0.
func (r *RedisStore) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, errors.New("kv: SetNX ttl must be positive")
	}
	ok, err := r.client.SetNX(ctx, key, value, ttl).Result()
	if err != nil {
		return false, err
	}
	return ok, nil
}
```

- [ ] **Step 4: Tests** in `pkg/kv/memory_test.go` (match the file's existing style — it constructs `NewMemoryStore()`):
```go
func TestMemorySetNX(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()

	ok, err := s.SetNX(ctx, "k", "v1", time.Minute)
	if err != nil || !ok {
		t.Fatalf("first SetNX = (%v,%v), want (true,nil)", ok, err)
	}
	ok, err = s.SetNX(ctx, "k", "v2", time.Minute)
	if err != nil || ok {
		t.Fatalf("second SetNX = (%v,%v), want (false,nil)", ok, err)
	}
	if got, _ := s.Get(ctx, "k"); got != "v1" {
		t.Fatalf("value = %q, want v1 (NX must not overwrite)", got)
	}
	if _, err := s.SetNX(ctx, "k2", "v", 0); err == nil {
		t.Fatal("SetNX with ttl=0 should error")
	}
}

func TestMemorySetNXExpiredIsAbsent(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()
	if ok, _ := s.SetNX(ctx, "k", "v1", 20*time.Millisecond); !ok {
		t.Fatal("seed SetNX should succeed")
	}
	time.Sleep(40 * time.Millisecond)
	ok, err := s.SetNX(ctx, "k", "v2", time.Minute)
	if err != nil || !ok {
		t.Fatalf("post-expiry SetNX = (%v,%v), want (true,nil)", ok, err)
	}
}

func TestMemorySetNXConcurrentExactlyOneWinner(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()
	const n = 100
	var wins int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if ok, _ := s.SetNX(ctx, "race", "v", time.Minute); ok {
				atomic.AddInt64(&wins, 1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if wins != 1 {
		t.Fatalf("winners = %d, want exactly 1", wins)
	}
}
```
Add imports to the test file as needed: `"context"`, `"sync"`, `"sync/atomic"`, `"time"`.

- [ ] **Step 5: Verify + commit.**
```bash
go test ./pkg/kv/... && go build ./... && go vet ./...
git add pkg/kv/store.go pkg/kv/memory.go pkg/kv/redis.go pkg/kv/memory_test.go
git commit -m "feat(kv): add atomic SetNX (set-if-absent) primitive

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Refresh-token rotation — race-safe + idempotency window

**Goal:** Make `rotateRefresh` atomic (one winner) and add a previous-token idempotency window so benign concurrent/retried refreshes return the same successor instead of revoking the family. No bearer envelope is cached; benign replay re-mints the access/ID token and returns the already-stored current refresh token.

**Files:**
- Modify: `pkg/protocol/oidc/refresh.go`
- Test: `pkg/protocol/oidc/refresh_test.go`

**Acceptance Criteria:**
- [ ] Two concurrent exchanges of the same current token: exactly one rotates; the other gets a retryable result with **no family revocation**.
- [ ] Re-presenting a just-rotated token within the window returns the same successor (no second mint, no branch).
- [ ] A token presented after the window (and not current) → family revoked (reuse).
- [ ] `SetNX` error → safe failure (no rotation, no revocation).
- [ ] Idempotent replay audited as `refresh_idempotent_replay`; reuse still audited.

**Verify:** `go test ./pkg/protocol/oidc/...` → PASS.

**Steps:**

- [ ] **Step 1: Extend the family record + constants.** In `pkg/protocol/oidc/refresh.go`, add two fields to `refreshFamily` (after `IssuedAt`, line 52):
```go
	PreviousToken      string    `json:"previous_token,omitempty"`
	PreviousValidUntil time.Time `json:"previous_valid_until,omitempty"`
```
Add near `RefreshTokenTTL` (line 23):
```go
// refreshIdempotencyWindow bounds how long a just-rotated (previous) refresh
// token may be re-presented to receive the SAME successor instead of tripping
// reuse detection. Covers benign client double-submit / network retry. Kept
// short: within this window a stolen previous token could also redeem the
// successor, an accepted tradeoff (the token is already compromised in that
// case, and false family revocation is user-hostile).
const refreshIdempotencyWindow = 10 * time.Second

// refreshLockKey is the SetNX rotation-lock key for a presented token.
func refreshLockKey(token string) string { return "oidc:refresh:lock:" + token }
```
Add a new sentinel near `errRefreshReuse` (line 35):
```go
// errRotationInProgress is returned when a concurrent rotation holds the lock
// for the presented token. It is BENIGN (not reuse): the caller maps it to a
// retryable invalid_grant and does NOT revoke the family.
var errRotationInProgress = errors.New("oidc: refresh rotation in progress")
```

- [ ] **Step 2: Rewrite `rotateRefresh`** (replace the body at lines 155-188). Replace the stale NOTE comment too:
```go
// rotateRefresh performs a single-use exchange of a refresh token, made
// atomic by a per-token SetNX rotation lock and made benign-concurrency-safe
// by a previous-token idempotency window on the family record.
//
//   - Acquire the rotation lock (SetNX). If it is held, a concurrent rotation
//     for this exact token is in flight → errRotationInProgress (retryable, no
//     revoke).
//   - Resolve the family; a miss → errRefreshInvalid.
//   - If the presented token is the family's PreviousToken and we are within
//     PreviousValidUntil → benign idempotent replay: return the already-rotated
//     CurrentToken (caller re-mints access/ID tokens). No second mint.
//   - If the presented token is NOT CurrentToken (and not a valid previous) →
//     superseded/stolen → revoke family, errRefreshReuse.
//   - Otherwise rotate: mint newToken, record PreviousToken=presented +
//     PreviousValidUntil, set CurrentToken=newToken, persist.
//
// The boolean return reports whether this call performed a real rotation
// (true) vs served an idempotent replay (false) — the caller uses it to pick
// the audit reason.
func rotateRefresh(ctx context.Context, store kv.Store, presented string) (fam *refreshFamily, newToken string, rotated bool, err error) {
	got, lockErr := store.SetNX(ctx, refreshLockKey(presented), "1", refreshIdempotencyWindow)
	if lockErr != nil {
		return nil, "", false, lockErr // fail closed: no rotation, no revoke
	}
	if !got {
		return nil, "", false, errRotationInProgress
	}
	// Best-effort release on the rotation path; correctness does not depend on
	// it (the lock self-expires at refreshIdempotencyWindow).
	defer func() { _ = store.Del(ctx, refreshLockKey(presented)) }()

	fam, err = loadFamily(ctx, store, presented)
	if err != nil {
		return nil, "", false, err
	}

	now := time.Now().UTC()
	// Benign idempotent replay of a just-rotated token.
	if presented == fam.PreviousToken && now.Before(fam.PreviousValidUntil) {
		return fam, fam.CurrentToken, false, nil
	}

	if presented != fam.CurrentToken {
		// Superseded/stolen token beyond the window: revoke the whole family.
		if delErr := store.Del(ctx, refreshFamilyKey(fam.FamilyID)); delErr != nil {
			return nil, "", false, delErr
		}
		return nil, "", false, errRefreshReuse
	}

	minted, err := randToken()
	if err != nil {
		return nil, "", false, fmt.Errorf("oidc: generate refresh token: %w", err)
	}
	fam.PreviousToken = presented
	fam.PreviousValidUntil = now.Add(refreshIdempotencyWindow)
	fam.CurrentToken = minted
	if err := putFamily(ctx, store, fam); err != nil {
		return nil, "", false, err
	}
	return fam, minted, true, nil
}
```
NOTE: the old token→family mapping persists (existing behavior) so the previous-token check can `loadFamily(presented)` after rotation.

- [ ] **Step 3: Update `grantRefreshToken`** (line 217) to the new signature + handle `errRotationInProgress` + audit reason. Replace the call + the reuse/err block (lines 221-238):
```go
	fam, newToken, rotated, err := rotateRefresh(ctx, p.kv, presented)
	if errors.Is(err, errRotationInProgress) {
		// Benign concurrency: another rotation for this token is in flight. No
		// revocation; the client retries and the idempotency window serves it.
		writeOIDCError(w, http.StatusBadRequest, errCodeInvalidGrant, "refresh rotation in progress, retry")
		return
	}
	if errors.Is(err, errRefreshReuse) {
		p.auditTokenEvent(ctx, r, audit.EventFail, nil, map[string]any{
			"reason":    "refresh_reuse",
			"client_id": client.ClientID,
		})
		writeOIDCError(w, http.StatusBadRequest, errCodeInvalidGrant, "refresh token reuse detected")
		return
	}
	if err != nil {
		writeOIDCError(w, http.StatusBadRequest, errCodeInvalidGrant, "invalid refresh token")
		return
	}
```
Then at the success audit (line 277-280), pick the reason by `rotated`:
```go
	reason := "refresh_rotated"
	if !rotated {
		reason = "refresh_idempotent_replay"
	}
	acctID := acct.ID
	p.auditTokenEvent(ctx, r, audit.EventUse, &acctID, map[string]any{
		"reason":    reason,
		"client_id": client.ClientID,
	})
```
(The `newToken` returned on an idempotent replay is `fam.CurrentToken`; the access/ID tokens are freshly minted by the existing `mintAccessAndIDTokens` call — correct, no second refresh-token mint.)

- [ ] **Step 4: Tests** in `pkg/protocol/oidc/refresh_test.go` (read the file first to reuse its existing helpers for seeding a family with a `MemoryStore`; mirror their construction). Add:
  - `concurrent rotation`: seed a family; fire 2 goroutines calling `rotateRefresh` with the same current token; assert exactly one returns `rotated==true` and the other returns `errRotationInProgress` OR (if it ran after the winner released the lock) an idempotent replay (`rotated==false`, same token) — in NEITHER case `errRefreshReuse`, and the family still exists.
  - `idempotent replay within window`: rotate once (capture R2); call `rotateRefresh` again with the OLD token within the window → returns `(fam, R2, false, nil)`; family intact; `CurrentToken` unchanged (no second mint).
  - `reuse after window`: rotate; set `fam.PreviousValidUntil` into the past (re-`putFamily`) OR present a never-current token; `rotateRefresh(old)` → `errRefreshReuse` and family deleted.
  - `SetNX error`: use a fake `kv.Store` whose `SetNX` returns an error → `rotateRefresh` returns that error, performs no rotation, no `Del` of the family.
  - Assert (grep-style in the test or by inspection) the success path logs no raw token.

- [ ] **Step 5: Verify + commit.**
```bash
go test ./pkg/protocol/oidc/... && go build ./... && go vet ./...
git add pkg/protocol/oidc/refresh.go pkg/protocol/oidc/refresh_test.go
git commit -m "fix(oidc): race-safe refresh rotation with idempotency window (no victim lockout)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: SAML AuthnRequest replay — atomic, SP-scoped, fail-closed

**Goal:** Replace the racy `Get`→`SetEx` in `consumeAuthnRequestID` with `SetNX`, scope the key by SP entity id, and fail closed on KV error.

**Files:**
- Modify: `pkg/protocol/saml/authnreq.go`
- Modify: the single caller in `pkg/protocol/saml/sso.go` (HandleSSO)
- Test: `pkg/protocol/saml/authnreq_test.go` (or the existing SAML test file covering replay — locate with `grep -rl consumeAuthnRequestID pkg/protocol/saml/*_test.go`; if none, create `authnreq_test.go`)

**Acceptance Criteria:**
- [ ] Replay key is `saml:authn_request_replay:{spEntityID}:{requestID}`.
- [ ] First call sets and returns nil; second within TTL returns `ErrReplayedRequest`.
- [ ] Concurrent first-presentations: exactly one proceeds, the other gets `ErrReplayedRequest`.
- [ ] KV error → reject (fail closed), not proceed.

**Verify:** `go test ./pkg/protocol/saml/...` → PASS.

**Steps:**

- [ ] **Step 1: Change the signature + body** of `consumeAuthnRequestID` (lines 353-361) to take the SP entity id and use `SetNX`:
```go
func (i *IdP) consumeAuthnRequestID(ctx context.Context, spEntityID, id string) error {
	replayKey := "saml:authn_request_replay:" + spEntityID + ":" + id
	ok, err := i.kv.SetNX(ctx, replayKey, "1", AuthnRequestTTL)
	if err != nil {
		return err // fail closed: a KV error must not allow the request through
	}
	if !ok {
		return ErrReplayedRequest
	}
	return nil
}
```
Update the doc comment's "NOTE: the Get→SetEx sequence is NOT atomic…" paragraph (lines 347-352) to state it is now atomic via `SetNX` and scoped by SP entity id.

- [ ] **Step 2: Update the caller** in `pkg/protocol/saml/sso.go`. Find the `consumeAuthnRequestID(ctx, <id>)` call in `HandleSSO` and pass the SP entity id. The SP entity id is the AuthnRequest Issuer (the SP that sent the request) — it is already resolved in `HandleSSO` (the same value used to look up the SP / resolve ACS). Use that value (e.g. `req.Issuer` or the resolved `sp.EntityID` — use whichever the surrounding code already holds for this request). Example:
```go
	if err := i.consumeAuthnRequestID(ctx, spEntityID, req.ID); err != nil { ... }
```
(Read `HandleSSO` to use the exact in-scope variable names for the SP entity id and the request ID.)

- [ ] **Step 3: Tests.** Add/extend a SAML test that constructs an `IdP` with a `MemoryStore` and calls `consumeAuthnRequestID`:
  - first call (`sp1`,`id1`) → nil; second (`sp1`,`id1`) → `ErrReplayedRequest`.
  - same id under a different SP (`sp2`,`id1`) → nil (scoping proven).
  - a fake `kv.Store` whose `SetNX` errors → `consumeAuthnRequestID` returns that error (fail closed).
  - (optional) 50 concurrent (`sp1`,`id1`) → exactly one nil, rest `ErrReplayedRequest`.

- [ ] **Step 4: Verify + commit.**
```bash
go test ./pkg/protocol/saml/... && go build ./... && go vet ./...
git add pkg/protocol/saml/authnreq.go pkg/protocol/saml/sso.go pkg/protocol/saml/*_test.go
git commit -m "fix(saml): atomic SP-scoped AuthnRequest replay check via SetNX, fail closed

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Coarse revoke — transaction + lockout guard + usable-method count

**Goal:** Make `revoke-password-totp` atomic and refuse to remove the last usable sign-in factor; tighten `AvailableMethods` to exclude federation identities whose upstream is disabled.

**Files:**
- Create: a query in `db/queries/account_identity.sql` + run `sqlc generate` (regenerates `pkg/db/*`)
- Modify: `pkg/authn/flow.go` (FlowQueries, AvailableMethods, DisableNonWebAuthnFallbacks, new sentinel + error mapping)
- Modify: `pkg/server/handle_me_revoke_pwd_totp.go` (transaction + account-row lock)
- Modify: `pkg/authn/errors.go` (new `would_remove_last_factor` AuthError) — verify the error-construction pattern there first
- Test: `pkg/authn/flow_test.go` (locate existing; if absent create)

**Acceptance Criteria:**
- [ ] New `CountUsableSignInFederation` query: counts linked identities whose `upstream_idp.disabled = false`.
- [ ] `AvailableMethods` reports `federation_oidc` only when that count > 0.
- [ ] `DisableNonWebAuthnFallbacks` returns `ErrWouldRemoveLastFactor` (→ HTTP 409 `would_remove_last_factor`) when removing password+TOTP would leave 0 usable methods (no passkey AND no usable federation), making no deletes.
- [ ] Deletes run inside one pgx transaction; the handler takes `SELECT … FOR UPDATE` on the account row first (production path).
- [ ] Existing revoke unit tests (using `revokeFlowOverride`, no pool) still pass.

**Verify:** `go test ./pkg/authn/... ./pkg/server/...` → PASS.

**Steps:**

- [ ] **Step 1: Add the query** to `db/queries/account_identity.sql`:
```sql
-- name: CountUsableSignInFederation :one
-- Linked identities the account can actually sign in / step up with: the
-- upstream IdP must still exist and be enabled. (ListAccountIdentitiesByAccount
-- intentionally returns ALL links, incl. disabled-upstream, for display/unlink.)
SELECT COUNT(*) FROM account_identity ai
JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
WHERE ai.account_id = $1 AND NOT ip.disabled;
```
Run `sqlc generate` (regenerates `pkg/db/account_identity.sql.go` + `querier.go`). NOTE: after this, IDE gopls may falsely show the new `CountUsableSignInFederation` as undefined until the language server reloads — `go build ./...` exit 0 is authoritative.

- [ ] **Step 2: Tighten `FlowQueries` + `AvailableMethods`** in `pkg/authn/flow.go`. In the `FlowQueries` interface (lines 34-42): **replace** `ListAccountIdentitiesByAccount(...)` with:
```go
	CountUsableSignInFederation(ctx context.Context, accountID int32) (int64, error)
```
In `AvailableMethods` (lines 75-81) replace the identities block:
```go
	usableFed, err := q.CountUsableSignInFederation(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("AvailableMethods: count usable federation: %w", err)
	}
	if usableFed > 0 {
		methods = append(methods, MethodFederationOIDC)
	}
```
(`*db.Queries` satisfies the new method after Step 1; any other `FlowQueries` consumer/fake must add it — grep `FlowQueries` and update fakes.)

- [ ] **Step 3: Add the sentinel + guard** in `pkg/authn/flow.go`. Near `ErrNoUsableMethod` (line 23):
```go
var ErrWouldRemoveLastFactor = errors.New("authn: removing password+TOTP would leave no usable sign-in method")
```
At the top of `DisableNonWebAuthnFallbacks` (before the recovery delete, line 107), add the guard:
```go
	// Lockout guard: refuse if removing password+TOTP would leave zero usable
	// sign-in methods (no passkey AND no usable federation identity).
	creds, err := q.ListCredentialsByAccount(ctx, accountID)
	if err != nil {
		return fmt.Errorf("DisableNonWebAuthnFallbacks: list webauthn: %w", err)
	}
	usableFed, err := q.CountUsableSignInFederation(ctx, accountID)
	if err != nil {
		return fmt.Errorf("DisableNonWebAuthnFallbacks: count federation: %w", err)
	}
	if len(creds) == 0 && usableFed == 0 {
		return ErrWouldRemoveLastFactor
	}
```
(`ListCredentialsByAccount` is already in `FlowQueries`.)

- [ ] **Step 4: Map the new error.** In `pkg/authn/errors.go`, add an AuthError constructor for code `would_remove_last_factor` (HTTP 409) following the existing pattern in that file (read one neighbouring constructor, e.g. `ErrLastPasskey`, and mirror it — same struct/fields, Chinese user message consistent with siblings). Then in `pkg/server/handle_me_revoke_pwd_totp.go` ensure `writeAuthErr` maps `ErrWouldRemoveLastFactor` → that AuthError. Cleanest: have `DisableNonWebAuthnFallbacks` return the AuthError directly (return `authn.ErrWouldRemoveLastFactor()` as an AuthError instead of a bare error) so `writeAuthErr` handles it uniformly — choose whichever matches how other flow.go errors reach `writeAuthErr` today (verify by reading `writeAuthErr`). Keep `ErrWouldRemoveLastFactor` as the testable sentinel and map it.

- [ ] **Step 5: Transaction + account lock in the handler.** Rewrite `handleMeRevokePwdTOTPHTTP` (`pkg/server/handle_me_revoke_pwd_totp.go:34-44`):
```go
func (s *Server) handleMeRevokePwdTOTPHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := authn.SessionFromContext(ctx)
	if s.requireFreshSudo(ctx, w, sess) {
		return
	}
	acctID := sess.Account.ID

	// Unit-test seam: when no real pool is wired (fake FlowQueries injected via
	// revokeFlowOverride), run without a tx. Production serialises on the
	// account row inside a transaction so concurrent factor mutations can't
	// race the lockout guard.
	if s.dbPool == nil {
		if err := authn.DisableNonWebAuthnFallbacks(ctx, s.revokeFlowQ(), s.Audit, acctID); err != nil {
			writeAuthErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	tx, err := s.dbPool.Begin(ctx)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := s.queries.WithTx(tx)
	if _, err := qtx.GetAccountByIDForUpdate(ctx, acctID); err != nil {
		writeAuthErr(w, err)
		return
	}
	if err := authn.DisableNonWebAuthnFallbacks(ctx, qtx, s.Audit, acctID); err != nil {
		writeAuthErr(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeAuthErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```
(`*db.Queries` from `WithTx` satisfies `authn.FlowQueries`. `writeAuthErr` must tolerate non-AuthError errors — verify it falls back to a 500; if not, wrap pool/commit errors accordingly.)

- [ ] **Step 6: Tests** in `pkg/authn/flow_test.go` (fake `FlowQueries`):
  - guard: fake reports 0 passkeys + 0 usable federation → `DisableNonWebAuthnFallbacks` returns `ErrWouldRemoveLastFactor`, and **no delete** method was called (assert the fake's delete counters are zero).
  - safe (≥1 passkey): deletes invoked in order; returns nil.
  - safe (0 passkeys, ≥1 usable federation): deletes invoked; returns nil.
  - `AvailableMethods`: usable-federation count 0 → no `federation_oidc`; >0 → present.
  Update any existing flow.go fake to implement `CountUsableSignInFederation` and drop `ListAccountIdentitiesByAccount` if it was only there for AvailableMethods.

- [ ] **Step 7: Verify + commit.**
```bash
go test ./pkg/authn/... ./pkg/server/... && go build ./... && go vet ./...
git add db/queries/account_identity.sql pkg/db pkg/authn/flow.go pkg/authn/errors.go pkg/server/handle_me_revoke_pwd_totp.go pkg/authn/flow_test.go
git commit -m "fix(authn): transactional revoke-password-totp with last-factor lockout guard

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Serialize the passkey-delete path on the account row

**Goal:** Make the last-passkey guard TOCTOU-safe by running count+delete inside a transaction that first locks the account row — so it can't race the revoke path (Task 4) into leaving zero usable methods.

**Files:**
- Modify: `pkg/server/handle_me.go` (`handleDeleteMyCredential`, lines 354-384)
- Test: the existing handle_me credential-delete test (locate with `grep -rl handleDeleteMyCredential pkg/server/*_test.go`; extend it, or rely on smoke if it's integration-only)

**Acceptance Criteria:**
- [ ] `handleDeleteMyCredential` takes `SELECT … FOR UPDATE` on the account row, then counts, then deletes — all in one transaction.
- [ ] Last-passkey guard (`count <= 1` → `ErrLastPasskey`) preserved.
- [ ] Behavior otherwise unchanged (same return shape, same not-found mapping).

**Verify:** `go test ./pkg/server/...` → PASS; the credential-delete arc in `cmd/smoke` still green.

**Steps:**

- [ ] **Step 1: Wrap in a transaction.** Replace the body of `handleDeleteMyCredential` (`pkg/server/handle_me.go:354-384`) so count+delete run under a tx with the account-row lock. `s.queries` is the concrete `*db.Queries` (no fake-injection seam here — this handler is integration/smoke-tested), so always use the real pool:
```go
func (s *Server) handleDeleteMyCredential(ctx context.Context, in *deleteMyCredentialIn) (*emptyOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	tx, err := s.dbPool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("delete credential: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := s.queries.WithTx(tx)

	// Serialise factor mutations for this account (vs revoke-password-totp).
	if _, err := q.GetAccountByIDForUpdate(ctx, sess.Account.ID); err != nil {
		return nil, fmt.Errorf("delete credential: lock account: %w", err)
	}
	count, err := q.CountCredentialsByAccount(ctx, sess.Account.ID)
	if err != nil {
		return nil, fmt.Errorf("count credentials: %w", err)
	}
	if count <= 1 {
		return nil, authErrToHuma(authn.ErrLastPasskey())
	}
	n, err := q.DeleteCredentialByID(ctx, db.DeleteCredentialByIDParams{
		ID:        in.Body.ID,
		AccountID: sess.Account.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("delete credential: %w", err)
	}
	if n == 0 {
		return nil, authErrToHuma(authn.ErrCredentialNotFound())
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("delete credential: commit: %w", err)
	}

	logx.WithContext(ctx).WithFields(logrus.Fields{
		"event":         "auth.credential_revoked_self",
		"account_id":    sess.Account.ID,
		"credential_id": in.Body.ID,
	}).Info("auth")
	return &emptyOut{}, nil
}
```

- [ ] **Step 2: Verify + commit.**
```bash
go test ./pkg/server/... && go build ./... && go vet ./...
git add pkg/server/handle_me.go
git commit -m "fix(server): serialize passkey delete on the account row (TOCTOU-safe last-factor)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Close the OIDC client-id timing oracle

**Goal:** On the client-secret-authenticated path, when the client is unknown and a secret was presented, run a dummy argon2id verify (same params as real client-secret hashes) before returning `invalid_client`, so timing no longer distinguishes known vs unknown clients.

**Files:**
- Modify: `pkg/protocol/oidc/client.go`
- Modify: the `Provider` constructor (locate with `grep -rn "func NewProvider\|p := &Provider\|Provider{" pkg/protocol/oidc/*.go`) to compute + hold the dummy PHC; and the call site of `authenticateClient` (`grep -rn authenticateClient pkg/protocol/oidc/*.go`)
- Test: `pkg/protocol/oidc/client_test.go`

**Acceptance Criteria:**
- [ ] On unknown client_id **with a presented secret**, a dummy argon2id verify runs before returning `invalid_client`.
- [ ] No argon2 runs for requests that present no secret.
- [ ] Unknown-client and known-client-wrong-secret both return identical `invalid_client` (status/body/`WWW-Authenticate`).
- [ ] Dummy PHC uses the same argon2id params as real client-secret hashes (so cost matches).

**Verify:** `go test ./pkg/protocol/oidc/...` → PASS.

**Steps:**

- [ ] **Step 1: Provide a fixed dummy PHC with matching params.** Client secrets are hashed via `password.HashRaw(secret, params)` where `params` is `configx.PasswordHashParams` from config. Compute the dummy PHC ONCE where the `Provider` is constructed (it has the config/params in scope) and store it on the struct, e.g. add field `dummyClientSecretPHC string` to `Provider` and in the constructor:
```go
	dummyPHC, err := password.HashRaw("timing-equalizer", <theSamePasswordHashParamsUsedForClientSecrets>)
	if err != nil {
		return nil, fmt.Errorf("oidc: precompute dummy client-secret PHC: %w", err)
	}
	p.dummyClientSecretPHC = dummyPHC
```
(Read the constructor to use the exact params value the provider already holds for client-secret hashing. If the provider doesn't currently hold params, thread the same `configx.PasswordHashParams` used by client-secret creation into it.)

- [ ] **Step 2: Make `authenticateClient` run the dummy verify.** Change `authenticateClient` to a `Provider` method (so it can read `p.dummyClientSecretPHC`) — `func (p *Provider) authenticateClient(ctx context.Context, q clientQueries, r *http.Request) (db.OidcClient, error)` — and update its single call site. At the `loadClient` failure path (lines 87-96), replace the FUTURE-HARDENING comment + bare return with:
```go
	client, err := loadClient(ctx, q, clientID)
	if err != nil {
		// Timing-oracle defense: if a secret was presented, burn an argon2id
		// verify against a fixed dummy PHC (same params as real client-secret
		// hashes) so an unknown client_id costs the same as a known one with a
		// wrong secret. Skip when no secret is presented — don't make
		// unauthenticated requests pay argon2. (This adds no new DoS surface:
		// the known-client wrong-secret path already incurs the same cost.)
		presentedSecret := basicSecret
		if presentedSecret == "" {
			presentedSecret = formSecret
		}
		if presentedSecret != "" {
			_ = password.VerifyRaw(presentedSecret, p.dummyClientSecretPHC)
		}
		return db.OidcClient{}, err
	}
```

- [ ] **Step 3: Confirm uniform error.** Verify the token handler maps every `errInvalidClient` to the same `invalid_client` response (status 401/400, identical body, identical `WWW-Authenticate` if any) for both unknown-client and known-client-wrong-secret. If the unknown-client path currently differs (e.g. a different status), align it. (Read the token endpoint's error handling around the `authenticateClient` call.)

- [ ] **Step 4: Test** in `pkg/protocol/oidc/client_test.go` (reuse its existing fake `clientQueries` + provider setup):
  - unknown client_id + a presented secret → returns `errInvalidClient`; assert the dummy-verify path executed (e.g. spy on a wrapper, or assert via a seam that `VerifyRaw` was called with `p.dummyClientSecretPHC`; simplest: factor the dummy-verify behind a tiny overridable func/var the test can observe, or assert observable behavior — do NOT assert wall-clock timing).
  - unknown client_id + NO secret → no dummy verify (assert the spy was not called).
  - known client_id + wrong secret → `errInvalidClient` (real verify path).
  - both return the same sentinel `errInvalidClient`.

- [ ] **Step 5: Verify + commit.**
```bash
go test ./pkg/protocol/oidc/... && go build ./... && go vet ./...
git add pkg/protocol/oidc/client.go pkg/protocol/oidc/*_test.go
git commit -m "fix(oidc): close client-id timing oracle via dummy argon2 on unknown-client+secret

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Finishing (coordinator)
1. Per-task spec + quality review (two-stage).
2. Final whole-cycle review (opus): cross-cutting — every fail-closed path actually fails closed; no raw bearer tokens logged; the two factor-removal paths truly serialize; SetNX used correctly everywhere.
3. **Done-gate (repo root, all GREEN):** `go build ./... && go vet ./...` exit 0; `go test ./...`; `cmd/smoke` SMOKE_EXIT=0 (extend smoke only if a new externally-observable behavior needs coverage — the refresh/SAML/revoke arcs already exist). No dist rebuild (no frontend).
4. Memory + handoff: note this is the Tier-2 cycle done; next is the Tier-1 self-service cycle.

## Self-review (against the spec)
- **Spec coverage:** §1 SetNX → Task 1; §2 refresh → Task 2; §3 SAML → Task 3; §4 revoke tx+guard+serialize-both → Tasks 4 (revoke) + 5 (passkey-delete); §5 timing oracle → Task 6. All covered.
- **Mechanism note (deliberate):** the spec suggested "tighten `AvailableMethods`"; the plan does so via a dedicated `CountUsableSignInFederation` query rather than altering `ListAccountIdentitiesByAccount` (which must keep returning disabled-upstream links for the connected-accounts display/unlink). Same agreed behavior (count only usable methods); cleaner blast radius.
- **No placeholders:** every code step is concrete. The few "read the file to use exact in-scope names" notes are for caller/constructor wiring where the surrounding identifiers must match — the inserted code itself is complete.
- **Type consistency:** `rotateRefresh` new 4-value signature used consistently in `grantRefreshToken`; `SetNX(ctx,key,value,ttl)(bool,error)` identical across interface + both backends + all callers; `CountUsableSignInFederation` added to both the sqlc layer and `FlowQueries`; `ErrWouldRemoveLastFactor` defined once and mapped.
