# Audit Reliability, Documentation, and Toolchain Remediation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: use test-driven-development for code; perform documentation only after behavior checks pass.

**Goal:** Make audit-write failures observable, remove stale policy/config claims, and ensure public artifacts are built with a patched Go toolchain.

**Architecture:** Preserve the current best-effort database audit model but never discard an audit failure silently: centralize error logging at call sites or through a Server helper. Document that semantic audit rows are best-effort and structured logs are the failure fallback. Align policy docs/comments with canonical `api.md`. Pin Go 1.26.5 or newer patch within the 1.26 line because local Go 1.26.4 is flagged by GO-2026-5856, even though ECH is not configured and practical exposure is absent.

**Tech Stack:** Go 1.26.5+, Logrus, mise.

### Task 1: Surface audit-write failures

**Files:** Modify `pkg/server` audit call helper/call sites and focused tests; update protocol packages only where they also silently discard `audit.Record` errors.

**Acceptance Criteria:** A failed audit insert emits a structured error containing factor/event/actor context but no secrets; successful mutations retain current response behavior; no `_ = ...Audit.Record` remains on security-relevant mutation paths without an explicit documented reason.

**Verify:** focused server/protocol tests with a failing audit writer exit 0 and assert one safe error log.

### Task 2: Correct stale public claims and comments

**Files:** Modify `CONFIG.md`, `ARCHITECTURE.md`, `STATUS.md`, `AUDIT.md`, `api.md` only where needed; modify stale package comments in `pkg/server/handle_admin_saml_sps.go`, `handle_admin_app_access.go`, `handle_admin_groups.go`; modify `scripts/dev-federation.sh`, `scripts/dev-forward-auth.sh`.

**Acceptance Criteria:** Removed `PROHIBITORUM_TRUST_PROXY` is not documented as functional; client-IP policy guidance names Admin → Settings and trusted proxy CIDRs; secure-cookie guidance correctly depends on HTTPS `PUBLIC_ORIGIN`; SAML/group/app-access sudo tiers match `api.md`; body-limit claims match the new body-control wrapper; audit guarantees state the failure fallback; obsolete script env vars are removed.

**Verify:** targeted content search finds no functional `TRUST_PROXY` recommendation and focused script syntax checks pass.

### Task 3: Pin patched Go toolchain

**Files:** Modify `mise.toml` and generated `mise.lock` if core-tool locking produces an entry; update `TOOLING.md` only if exact-version semantics change.

**Acceptance Criteria:** local and CI/release toolchain resolve Go >=1.26.5 and <1.27; `govulncheck ./...` no longer reports GO-2026-5856; no dependency files change unexpectedly.

**Verify:** `go version`, `mise run ci:go`, and `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` show the patched compiler and no reachable vulnerabilities.

### Task 4: Remove the unused error-code catalogue

**Files:** Remove `pkg/errorx/errors.go` after symbol-aware reference verification; update package tests only if they intentionally cover the catalogue.

**Acceptance Criteria:** All 11 exported variables in the file have zero references outside their declarations; removing the file does not change the active `errorx.Error` wire type or Huma override; server and errorx tests compile and pass.

**Verify:** LSP references for each exported variable are declaration-only, then `go test ./pkg/errorx ./pkg/server -count=1` exits 0.

**Steps:** Verify references before deletion; remove the file without aliases or deprecation shims; run focused tests.
