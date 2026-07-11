# Request Boundary Hardening Remediation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: use test-driven-development.

**Goal:** Bound every JSON admin mutation and prevent raw internal errors from reaching clients without changing the accepted sudo tier.

**Architecture:** Split request-shape controls from fresh-sudo policy. A reusable JSON/body-limit wrapper applies to all raw admin mutations; the sudo wrapper composes it with the existing fresh-auth gate. Central error rendering logs internal errors and returns the stable `server_error` response.

**Tech Stack:** Go 1.26, chi, Huma.

### Task 1: Decouple admin body controls from sudo

**Files:** Modify `pkg/server/operations.go`, `pkg/server/server.go`, `pkg/server/admin_route_policy_test.go`; add focused wrapper tests beside `operations_test.go`.

**Acceptance Criteria:**
- All raw admin mutation routes enforce JSON content type and a 64 KiB body cap, including SAML CRUD, app-access, and group routes that intentionally remain admin-only.
- Admin-only routes still do not require sudo; current `api.md` tiering remains unchanged.
- Oversized bodies fail with 413 before handler mutation; non-JSON bodies fail with canonical bad-request output.
- `registerSudoOpHTTP` composes body controls exactly once.

**Verify:** `go test ./pkg/server -run 'AdminMutation|TrimmedAdmin|Operations|Body' -count=1` exits 0.

**Steps:**
1. Add a failing real-router test sending 128 KiB JSON to an admin-only SAML route and expecting 413.
2. Add a failing content-type test while retaining the existing no-sudo assertion.
3. Extract `withAdminBodyControls`; make both admin-only raw mutation registration and `withFreshSudo` use it without double wrapping.
4. Register the 16 intentional admin-only mutation routes through the body-controlled helper.
5. Run focused tests to green.

### Task 2: Sanitize non-domain errors

**Files:** Modify `pkg/server/handle_auth.go`; add focused tests in the nearest existing server error test file.

**Acceptance Criteria:**
- `AuthError` status/code/message behavior is unchanged.
- A wrapped DB/KV/crypto error returns HTTP 500 with stable `server_error` output and no internal detail.
- Full internal error detail is emitted only to structured server logs with request context.

**Verify:** `go test ./pkg/server -run 'WriteAuthErr|ServerError' -count=1` exits 0.

**Steps:**
1. Write a failing test whose synthetic DB error contains a secret connection string and assert the response omits it.
2. Replace raw `http.Error(err.Error())` fallback with structured logging plus canonical internal response.
3. Run focused tests to green.
