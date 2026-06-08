# Spec — Backend security hardening (Tier 2)

**Date:** 2026-06-08
**Branch:** `master` (commit directly; no remote, no worktree).
**Status:** approved design, ready for plan.

First of the Tier-1+2 backend cycles (decomposed: **Hardening → Self-service/reads → D enrollment**;
hardening first by user choice). **Backend-only — no migration, no frontend.** Closes the
documented atomicity/lockout/timing gaps from the backend backlog
(`docs/superpowers/notes/2026-06-08-backend-backlog.md` Tier 2). Each item was reviewed; the
design below reflects that review (notably: refresh-token idempotency without caching bearer
material, and TOCTOU-safe factor removal).

Contracts/behaviors verified against the real code (`pkg/protocol/oidc/refresh.go`,
`pkg/protocol/saml/authnreq.go`, `pkg/authn/flow.go`, `pkg/kv/*`, `pkg/protocol/oidc/client.go`).

## Five changes

### 1. New KV primitive: `SetNX`
Add to the `kv.Store` interface (`pkg/kv/store.go`):
```go
// SetNX atomically sets key=value with the given ttl ONLY if key does not
// already exist (an expired key counts as absent). Returns true if it set the
// key, false if the key already existed. ttl must be > 0. A backend error
// returns (false, err) — callers fail closed.
SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
```
- **Redis** (`pkg/kv/redis.go`): `client.SetNX(ctx, key, value, ttl)` (go-redis native; emits `SET key val NX PX/EX`). Map the bool result; non-nil err → `(false, err)`.
- **Memory** (`pkg/kv/memory.go`): check-and-insert **under the same lock** as the rest of the store (ttlcache); an expired entry is treated as absent. Must be atomic against concurrent `SetNX`.
- **Tests:** 100 concurrent `SetNX` for the same key → **exactly one** returns `true`, all others `false` (both backends); expired key → next `SetNX` succeeds; `ttl<=0` rejected.

### 2. Refresh-token rotation — race-safe + victim-lockout-proof (no bearer caching)
Today `rotateRefresh` (`refresh.go:155`) does a non-atomic `loadFamily`→compare→`putFamily`;
two concurrent exchanges of the same current token both mint, orphaning the loser's token →
its next use trips reuse detection → **family revoked → victim lockout**.

**Fix — atomic single-winner election + a previous-token idempotency window. No response
envelope is cached** (refresh tokens are already stored raw in KV; we add no new bearer
material and re-mint on-demand tokens instead).

Extend `refreshFamily` (`refresh.go:42`):
```go
PreviousToken      string    `json:"previous_token,omitempty"`
PreviousValidUntil time.Time `json:"previous_valid_until,omitempty"`
```
Add a constant: `const refreshIdempotencyWindow = 10 * time.Second`.

`rotateRefresh(ctx, store, presented)` becomes:
1. **Acquire rotation lock:** `ok, _ := store.SetNX(ctx, "oidc:refresh:lock:"+presented, "1", refreshIdempotencyWindow)`.
   - If `!ok` (a rotation for this exact token is in flight): return a new sentinel
     `errRotationInProgress` **without revoking**. The caller maps it to a retryable
     `invalid_grant` ("rotation in progress, retry"). (The client retries; by then the
     window/PreviousToken path below serves it.)
2. `loadFamily(presented)` — miss → `errRefreshInvalid` (release lock best-effort).
3. **Benign idempotent replay:** if `presented == fam.PreviousToken` and
   `now < fam.PreviousValidUntil` → return `(fam, fam.CurrentToken, nil)` (the *already
   rotated* successor). The caller re-mints a fresh access/ID token and returns
   `fam.CurrentToken` as the refresh token. **No second successor minted; no branch.**
4. **Reuse:** if `presented != fam.CurrentToken` (and not the valid-previous case above) →
   superseded/stolen token → `revokeFamily` → `errRefreshReuse`.
5. **Normal rotation:** `presented == fam.CurrentToken` → mint `newToken`; set
   `fam.PreviousToken = presented`, `fam.PreviousValidUntil = now + refreshIdempotencyWindow`,
   `fam.CurrentToken = newToken`; `putFamily`. Return `(fam, newToken, nil)`.

Lock lifecycle: the lock auto-expires at `refreshIdempotencyWindow`; do not rely on explicit
release for correctness, but `Del` it after a successful rotation is fine. The old token
mapping continues to persist (existing behavior, `refresh.go:152`) so the previous-token check
can resolve the family.

**`grantRefreshToken` (`refresh.go:217`)** maps `errRotationInProgress` → 400 `invalid_grant`
"rotation in progress" (retryable; **no family revocation, no audit-fail**). On a benign replay
(step 3) it proceeds normally and re-mints — `newToken` returned is `fam.CurrentToken`.

**Security tradeoff (documented):** within the ~10s window a stolen previous token can redeem
the same successor. Acceptable: the window is short, the token is already compromised in that
scenario, benign client double-submit/retry is common, and false family revocation is
user-hostile. Genuine stale reuse after the window still revokes the family.

**Audit/metrics:** emit a low-severity `refresh_rotation_idempotent_replay` on step 3 and keep
the high-signal `refresh_reuse_detected` (family revoked) on step 4, so retry noise is
distinguishable from real reuse.

### 3. SAML AuthnRequest replay — atomic + scoped + fail-closed
`consumeAuthnRequestID` (`authnreq.go:353`) currently does `Get`→`SetEx` (racy) and is
effectively fail-open on KV error. Replace with:
```go
replayKey := "saml:authn_request_replay:" + spEntityID + ":" + requestID
ok, err := i.kv.SetNX(ctx, replayKey, "1", AuthnRequestTTL)
if err != nil { return <fail-closed error> }   // reject, do NOT proceed
if !ok { return ErrReplayedRequest }
```
Scope the key by **SP entity id + request id** (the AuthnRequest Issuer gives the SP entity id;
confirm it's in scope at this call site). KV error ⇒ **reject** (fail closed).

### 4. Coarse revoke — transaction + TOCTOU-safe lockout guard (both paths)
`DisableNonWebAuthnFallbacks` (`flow.go:106`) runs 3 unwrapped deletes and has **no lockout
guard**. Today an account could in principle remove password+TOTP leaving zero usable methods.

- **Transaction:** wrap recovery→TOTP→password deletes in one pgx transaction (begin/commit;
  rollback on any error). Keep the existing safe ordering + audit emission.
- **Lockout guard:** before deleting, count **usable** remaining sign-in methods *excluding*
  the password+TOTP being removed; refuse with a new error code **`would_remove_last_factor`**
  (HTTP 409) if it would reach zero. "Usable" = ≥1 WebAuthn credential **or** ≥1 federation
  identity whose **upstream IdP still exists and is not disabled**. NOTE: `AvailableMethods`
  currently counts federation identities **without** the upstream-enabled check
  (`flow.go:75-81`) — so the guard must compute usable-federation precisely (join
  `account_identity` → `upstream_idp.disabled`), or `AvailableMethods` is tightened to do so.
  Decision: **tighten `AvailableMethods`** to exclude disabled-upstream federation (it's the
  correct meaning of "available" and benefits `/me/sudo/methods` + login too) — verify the two
  consumers still behave (a federation-only account with a disabled upstream genuinely has no
  usable method → admin recovery, which is correct).
- **Serialize factor mutations per account (both paths):** add `SELECT … FOR UPDATE` on the
  `account` row at the start of the transaction in **both** the revoke path AND the
  passkey-delete path (`handleDeleteMyCredential`, `handle_me.go` — same count-then-delete
  shape with its last-passkey guard). This makes the "never zero usable methods" invariant hold
  under concurrent removals across the two endpoints. Add a `GetAccountForUpdate` query if one
  doesn't exist.

### 5. OIDC client-id timing oracle
`loadClient` miss returns before any argon2 (`client.go:87-96`), distinguishing known vs unknown
confidential clients by timing. Fix: on the **client-secret-authenticated** path, when the
client is unknown **and a secret was presented**, run a dummy argon2id verify against a fixed
valid PHC constant (same params as real client-secret hashes) before returning `invalid_client`.
- Only on the secret-present path (do not argon2 requests with no secret — and note this adds no
  new DoS class: confidential auth already incurs argon2 for known-client+wrong-secret).
- Response must stay uniform: unknown client and known-client-wrong-secret both return
  `invalid_client` with identical status/body/`WWW-Authenticate` (verify current uniformity).
- The fixed PHC is a compile-time constant produced once from the real argon2id params.

## Testing & done-gate
Go unit/integration tests:
- **SetNX:** 100-concurrent exactly-one-true (memory + redis); expired→absent; ttl<=0 rejected.
- **Refresh rotation:** single rotate; concurrent double-submit of current token → loser gets
  retryable `rotation_in_progress`, **no revoke**; lost-response retry with old token within
  window → same successor, **no second mint**; old token after window → family revoked; stale
  third-party token → revoked; `SetNX` error → safe failure (no rotation, no revoke); assert no
  raw token written to logs.
- **SAML replay:** replay rejected; key scoped by SP entity+id; KV error → fail closed.
- **Revoke:** transaction rolls back on mid-delete failure; `would_remove_last_factor` rejects
  when no other usable method (incl. the disabled-upstream-federation case); concurrent
  passkey-delete + revoke cannot reach zero usable methods (serialized).
- **Timing oracle:** unknown-client+secret path executes the dummy verifier; known-client+wrong
  secret executes the real verifier; both return identical `invalid_client`. Assert the dummy
  path is *executed* — do not assert wall-clock timing equality.

Gate (from repo root): `go build ./... && go vet ./...` exit 0; full `go test ./...`;
`cmd/smoke` **SMOKE_EXIT=0** (refresh/SAML/revoke arcs already covered — extend smoke only if a
new externally-observable behavior needs it). No dist rebuild (no frontend changes).

## Plan shape (~6 tasks, subagent-driven)
1. `SetNX` primitive (interface + redis + memory + concurrency tests).
2. Refresh-rotation idempotency (family fields + `rotateRefresh` rewrite + `grantRefreshToken` mapping + audit + tests).
3. SAML replay via `SetNX` (scoped key + fail-closed + tests).
4. Coarse-revoke transaction + lockout guard + `AvailableMethods` tightening (tests).
5. Per-account serialization on the passkey-delete path (`SELECT FOR UPDATE` + `GetAccountForUpdate`) + tests.
6. Client-id timing oracle (fixed PHC + dummy verify + uniform response + tests).
Then final review + done-gate. (Tasks 4–5 are closely related — may merge.)

## Out of scope (this cycle)
Token-at-rest encryption / HMAC-fingerprint keying of the refresh+code+SAML KV (a valid but
separate, whole-subsystem hardening — not bolted onto one record). Breach-list (not
implementing). Account-existence privacy (no leaking endpoint exists). Tier-1 self-service
endpoints + D enrollment (their own cycles).
