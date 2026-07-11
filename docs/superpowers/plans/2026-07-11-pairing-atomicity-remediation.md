# Pairing Atomicity Remediation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: use test-driven-development. Do not modify unrelated pairing UX.

**Goal:** Make device-pairing approval and completion single-winner operations across memory and Redis KV backends.

**Architecture:** Add an atomic compare-and-swap operation to the KV contract. Pairing approval replaces the exact pending JSON value with approved JSON; pairing completion atomically pops the canonical approved record before loading the account or issuing a session. Secondary code-index cleanup remains post-consume and cannot permit a second completion.

**Tech Stack:** Go 1.26, ttlcache, go-redis, Vitest-independent Go tests.

### Task 1: Add KV compare-and-swap

**Files:** Modify `pkg/kv/store.go`, `pkg/kv/memory.go`, `pkg/kv/redis.go`, `pkg/kv/memory_test.go`; create or modify Redis-focused tests where the existing convention permits.

**Acceptance Criteria:**
- Two concurrent CAS calls with the same expected value produce exactly one success.
- CAS preserves the supplied positive TTL and rejects non-positive TTL.
- Redis uses one Lua/EVAL operation comparing current bytes and setting the replacement with millisecond expiry.
- MemoryStore serializes CAS against Set, SetEx, Del, Pop, SetNX, and other CAS calls through the same mutex.

**Verify:** `go test ./pkg/kv -count=1` exits 0.

**Steps:**
1. Add failing concurrency, mismatch, missing-key, and TTL tests.
2. Run the focused tests and observe the missing CAS behavior fail.
3. Add `CompareAndSwap(ctx, key, oldValue, newValue string, ttl time.Duration) (bool, error)` to `Store` and both implementations.
4. Run the focused tests to green, then run `go test -race ./pkg/kv -count=1`.

### Task 2: Make pairing transitions atomic

**Files:** Modify `pkg/credential/pairing/pairing.go`, `pkg/server/handle_pairing.go`; create `pkg/credential/pairing/pairing_test.go`; add focused server pairing tests following `pkg/server` conventions.

**Acceptance Criteria:**
- Concurrent approvals by different accounts yield one success; the loser receives `pairing_state` and cannot overwrite the winner.
- Re-approval by the winning account is idempotent.
- Concurrent completion calls yield one consumed pairing and one `pairing_not_found`; only one session is issued.
- Completion trusts the atomically popped canonical pairing, not a stale pre-pop object.
- Cancel and code-index cleanup cannot resurrect or re-consume a pairing.

**Verify:** `go test -race ./pkg/credential/pairing ./pkg/server -run 'Pairing|Pair' -count=1` exits 0.

**Steps:**
1. Write concurrent approval and consume tests that fail because both callers currently return success.
2. Change `Approve` to marshal expected/new states and CAS the canonical key with remaining TTL.
3. Change `Consume` to accept an ID, atomically `Pop` the canonical record, decode and require approved state, then best-effort remove the code index; return the consumed record.
4. Move account lookup and session issuance in `handlePairCompleteHTTP` after successful atomic consume.
5. Run focused race tests to green and verify the existing smoke pairing arc.
