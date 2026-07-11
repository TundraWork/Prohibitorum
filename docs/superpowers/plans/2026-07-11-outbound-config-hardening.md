# Outbound and Configuration Hardening Remediation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: use test-driven-development.

**Goal:** Fail closed on malformed encryption keys and insecure outbound redirect/fetch behavior already exposed by existing features.

**Architecture:** Validate cryptographic material at config parse time. Reuse one exported outbound-client policy for operator metadata fetches and federation/avatar traffic, with per-hop scheme and resolved-IP checks plus existing size/time limits.

**Tech Stack:** Go 1.26, net/http, crypto/aes.

### Task 1: Validate DEK length at startup

**Files:** Modify `pkg/configx/configx.go`, `pkg/configx/configx_test.go`.

**Acceptance Criteria:** Every configured DEK decodes to exactly 32 bytes; malformed length names the version and length; valid multi-version keyrings remain accepted.

**Verify:** `go test ./pkg/configx -count=1` exits 0.

**Steps:** Add a failing 4-byte-key test; enforce length immediately after base64 decode; update the stale comment; run focused tests.

### Task 2: Reject insecure redirect downgrades

**Files:** Modify `pkg/federation/oidc/httpclient.go`; add tests in `pkg/federation/oidc/httpclient_test.go` or `avatar_fetch_test.go`.

**Acceptance Criteria:** Production outbound clients reject HTTPS-to-HTTP redirects; every redirected hop still receives resolved-IP screening and hop limits; trusted private-network mode permits HTTP only where the existing initial-URL contract permits it.

**Verify:** `go test ./pkg/federation/oidc -run 'Redirect|Avatar|HTTPClient' -count=1` exits 0.

**Steps:** Add a failing redirect-downgrade test; add per-hop scheme enforcement using `allowPrivate`; keep IP screening in the transport; run focused tests.

### Task 3: Harden CLI metadata fetch

**Files:** Modify `cmd/prohibitorum/main.go`; add focused CLI/package tests in `cmd/prohibitorum`.

**Acceptance Criteria:** `saml-sp create --metadata-url` accepts only absolute HTTPS URLs with a domain host and no userinfo/IP literal, rejects redirects to HTTP/internal addresses, caps redirects and response bytes, and preserves context timeout/cancellation. Inline metadata-file behavior is unchanged.

**Verify:** `go test ./cmd/prohibitorum -run 'Metadata|Fetch' -count=1` exits 0.

**Steps:** Add failing tests for HTTP, loopback/IP literal, and redirect downgrade; expose/reuse the hardened outbound client without coupling CLI code to unexported internals; run focused tests.
