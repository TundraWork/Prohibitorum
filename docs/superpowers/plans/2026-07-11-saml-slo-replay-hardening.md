# SAML SLO Replay Hardening Remediation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: use test-driven-development.

**Goal:** Prevent a previously valid signed LogoutRequest from revoking a later session.

**Architecture:** Validate request ID and IssueInstant after signature verification but before session lookup/mutation. Reserve an SP-scoped replay key atomically in the existing KV store with a short freshness TTL, mirroring AuthnRequest replay protection. Preserve signed, idempotent LogoutResponse behavior for a new request whose target session is already gone.

**Tech Stack:** Go 1.26, SAML 2.0, existing KV SetNX.

### Task 1: Enforce freshness and replay rejection

**Files:** Modify `pkg/protocol/saml/slo.go`, `pkg/protocol/saml/slo_test.go`; reuse constants/helpers from `authnreq.go` when their semantics match.

**Acceptance Criteria:**
- Missing/invalid request ID is rejected before mutation.
- IssueInstant outside the accepted skew/freshness window is rejected.
- The first valid signed request reserves `saml:slo:<sp-id>:<request-id>` atomically; a replay is rejected and cannot revoke a newly created session.
- Signature verification precedes replay reservation, preventing unauthenticated cache poisoning.
- A distinct valid request for an already-gone session still returns signed Success.

**Verify:** `go test ./pkg/protocol/saml -run 'SLO|Logout' -count=1` exits 0.

**Steps:**
1. Add failing replay and stale-IssueInstant tests using signed Redirect and POST requests.
2. Add precise SLO errors for stale/replayed requests and map them through existing protocol-safe error handling.
3. Validate ID/IssueInstant and call SetNX only after signature/destination checks and before session lookup.
4. Run focused tests to green, then the complete SAML package tests.
