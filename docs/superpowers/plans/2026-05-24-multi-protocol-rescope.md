# Multi-protocol rescope — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land the skeleton commit for the multi-protocol rescope — picotera vocabulary stripped, package layout reorganized to three-layer (credential / federation / protocol), all five audit-driven migrations in place, stub files for v0.2+ functionality, user-facing docs aligned. `go build ./...` clean at HEAD; `mise run db:up` applies cleanly to a fresh Postgres; existing tests pass after refactor.

**Architecture:** Approach A three-layer split per `2026-05-24-multi-protocol-rescope-design.md` §Architecture. Schema and behavioral policies derived from three protocol-audit reports (OIDC / credentials / SAML) committed alongside the spec. No business logic for v0.2+ in this commit — stubs only.

**Tech Stack:** Go 1.26, postgres (pgx/v5), sqlc 1.30, goose 3.27, chi v5, huma v2, go-webauthn, golang-jwt. (zitadel/oidc/v3 for v0.3+; crewjam/saml for v0.5+ — referenced in stubs, not yet imported.)

**Plan scope:** This plan covers the skeleton commit only. v0.2+ implementation gets its own plan once this lands and smoke-tests pass.

---

## File structure

The skeleton commit produces or modifies:

```
db/migrations/
  001_initial.sql                    REWRITE — account, session, webauthn_credential, enrollment, credential_event, auth_throttle
  002_oidc.sql                       REWRITE — signing_key (unified), oidc_client (extended), revoked_jti
  003_password_totp.sql              NEW
  004_federation.sql                 NEW
  005_saml.sql                       NEW
db/queries/
  account.sql                        REWRITE — attributes jsonb; no can_*
  enrollment.sql                     REWRITE — template_attributes; expected_upstream_idp_slug
  webauthn_credential.sql            REWRITE — user_handle, cose_alg, uv_initialized, clone_warning_at
  oidc.sql                           REWRITE — signing_key (unified), expanded oidc_client
  session.sql                        NEW
  credential_event.sql               NEW
  auth_throttle.sql                  NEW
  revoked_jti.sql                    NEW
  password_credential.sql            NEW
  totp_credential.sql                NEW
  recovery_code.sql                  NEW
  upstream_idp.sql                   NEW
  account_identity.sql               NEW
  saml_sp.sql                        NEW (covers saml_sp + child tables)
sqlc.yaml                            MODIFY — extend column overrides for new SERIAL ids
pkg/db/                              REGEN — sqlc output
pkg/                                 RESTRUCTURE per file-move map below
  account/                           NEW (was pkg/auth/account.go)
  credential/
    webauthn/                        NEW (was pkg/auth/webauthn*.go)
    password/                        NEW STUB
    totp/                            NEW STUB
    pairing/                         NEW (was pkg/auth/pairing.go)
    enrollment/                      NEW (was pkg/auth/enrollment.go)
  federation/
    oidc/                            NEW STUB
  session/                           NEW (was pkg/auth/session.go + part of middleware.go)
  authn/                             NEW (was pkg/auth/{ratelimit,sudo}.go + part of middleware.go)
  protocol/
    oidc/                            NEW (was pkg/oidc/)
    saml/                            NEW STUB
  audit/                             NEW STUB (credential_event writer)
  contract/auth.go                   MODIFY — drop Permission/Permissions; add Attributes
  configx/configx.go                 MODIFY — add DataEncryptionKeys, PasswordHashParams, TOTP, SAML, OIDC, Federation, WebAuthn.RPDisplayName
  errorx/                            MODIFY — rename PicoTeraError → Error
  server/                            MODIFY — update imports + handler bodies for attributes; remove permission references
DESIGN.md                            REWRITE
STATUS.md                            REWRITE
AUDIT.md                             REWRITE
INTEGRATION.md                       REWRITE
README.md                            REWRITE
```

**File-move map** (atomic in Task 1):

| From | To |
|---|---|
| `pkg/auth/account.go` (+ test) | `pkg/account/account.go` (+ test) |
| `pkg/auth/webauthn.go`, `webauthn_errors.go` | `pkg/credential/webauthn/` |
| `pkg/auth/enrollment.go` (+ test) | `pkg/credential/enrollment/` |
| `pkg/auth/pairing.go` | `pkg/credential/pairing/` |
| `pkg/auth/session.go` (+ test) | `pkg/session/session.go` |
| `pkg/auth/middleware.go` (+ test) | split: `pkg/session/middleware.go` (session loading) + `pkg/authn/middleware.go` (require-auth, role/attribute checks) |
| `pkg/auth/ratelimit.go` (+ test) | `pkg/authn/ratelimit.go` |
| `pkg/auth/sudo.go` | `pkg/authn/sudo.go` |
| `pkg/auth/errors.go` | split into per-package sentinel-error files |
| `pkg/oidc/oidc.go` | `pkg/protocol/oidc/oidc.go` |

**Reference spec:** `docs/superpowers/specs/2026-05-24-multi-protocol-rescope-design.md`. Detailed SQL for migrations 001–005 lives there. The plan steps cite line ranges when the SQL is too long to inline.

---

## Task 0: Baseline — go mod tidy + green build

**Goal:** Establish a known-good starting point. Lock the indirect-dependency graph from picotera-lifted code.

**Files:**
- Modify: `go.sum` (auto-updated)

**Acceptance Criteria:**
- [ ] `go mod tidy` completes without errors
- [ ] `go build ./...` succeeds
- [ ] `go test ./...` succeeds (or documents pre-existing failures)
- [ ] `go.sum` committed if changed

**Verify:** `go build ./... && go test ./...` → no errors, no panics

**Steps:**

- [ ] **Step 1: Run go mod tidy**

```bash
cd /home/tundra/projects/tundra/prohibitorum
go mod tidy
```

Expected: completes silently or with download messages; no error output.

- [ ] **Step 2: Build everything**

```bash
go build ./...
```

Expected: no output (success). If errors, capture them — they indicate v0.1-skeleton bugs that need fixing before refactor.

- [ ] **Step 3: Run tests**

```bash
go test ./... 2>&1 | tee /tmp/prohibitorum-baseline-tests.log
```

Expected: all tests pass. If any fail, save the log and fix them as part of this task before proceeding — green baseline is non-negotiable.

- [ ] **Step 4: Commit if go.sum changed**

```bash
git status --short
# If go.sum is in the diff:
git add go.sum
git commit -m "$(cat <<'EOF'
chore: go mod tidy

Locks indirect dependency graph carried over from picotera.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

If `go.sum` is unchanged, skip the commit. The point of this task is establishing the green baseline; an empty commit isn't useful.

---

## Task 1: Atomic package reorganization

**Goal:** Move pkg/auth/* and pkg/oidc/ into the three-layer layout per the file-move map. No semantic changes — identifier names, function bodies, schema references all unchanged. Build remains green.

**Files:**
- Move: 11 source files + their tests per the map above
- Modify: all `import` lines in `pkg/server/*.go`, `cmd/prohibitorum/main.go`, and any moved file that imports another moved package
- The `pkg/auth/` directory is fully deleted at the end

**Acceptance Criteria:**
- [ ] `pkg/auth/` directory no longer exists
- [ ] `pkg/oidc/` directory no longer exists
- [ ] `go build ./...` succeeds
- [ ] `go test ./...` passes — same set as Task 0 baseline
- [ ] No file has `package auth` outside of files explicitly intended to keep that package name (none should)

**Verify:** `go build ./... && go test ./... && ! test -d pkg/auth && ! test -d pkg/oidc`

**Steps:**

- [ ] **Step 1: Create destination directories**

```bash
mkdir -p pkg/account pkg/credential/{webauthn,password,totp,pairing,enrollment} \
         pkg/federation/oidc pkg/session pkg/authn \
         pkg/protocol/oidc pkg/protocol/saml pkg/audit
```

- [ ] **Step 2: Move files with git mv (preserves history)**

```bash
git mv pkg/auth/account.go            pkg/account/account.go
git mv pkg/auth/account_test.go       pkg/account/account_test.go
git mv pkg/auth/webauthn.go           pkg/credential/webauthn/webauthn.go
git mv pkg/auth/webauthn_errors.go    pkg/credential/webauthn/errors.go
git mv pkg/auth/enrollment.go         pkg/credential/enrollment/enrollment.go
git mv pkg/auth/enrollment_test.go    pkg/credential/enrollment/enrollment_test.go
git mv pkg/auth/pairing.go            pkg/credential/pairing/pairing.go
git mv pkg/auth/session.go            pkg/session/session.go
git mv pkg/auth/session_test.go       pkg/session/session_test.go
git mv pkg/auth/ratelimit.go          pkg/authn/ratelimit.go
git mv pkg/auth/ratelimit_test.go     pkg/authn/ratelimit_test.go
git mv pkg/auth/sudo.go               pkg/authn/sudo.go
git mv pkg/oidc/oidc.go               pkg/protocol/oidc/oidc.go
```

- [ ] **Step 3: Split middleware.go into session/middleware.go + authn/middleware.go**

Read the current `pkg/auth/middleware.go` (now still in pkg/auth at this point — git mv hasn't moved it). Identify two responsibilities:
1. Session loading middleware (reads cookie, calls session.Load, attaches to context).
2. Authentication-required gate (checks for session presence, role, attributes).

```bash
# Read first, then split. The file is small (~150 LOC).
cat pkg/auth/middleware.go
```

Manually split into:
- `pkg/session/middleware.go` — `LoadSession`, session context helpers (`SessionFromCtx`, etc.)
- `pkg/authn/middleware.go` — `RequireAuth`, `RequireAdmin`, attribute-based gates

Then remove the original:

```bash
git rm pkg/auth/middleware.go
git rm pkg/auth/middleware_test.go
```

If the test file has tests for both responsibilities, split it too: tests for session loading → `pkg/session/middleware_test.go`; tests for require-auth → `pkg/authn/middleware_test.go`.

- [ ] **Step 4: Split errors.go**

The `pkg/auth/errors.go` file holds sentinel errors. Distribute them by consumer:

```bash
cat pkg/auth/errors.go
```

- Errors used by enrollment (`ErrEnrollmentExpired`, `ErrEnrollmentConsumed`, etc.) → `pkg/credential/enrollment/errors.go`
- Errors used by pairing → `pkg/credential/pairing/errors.go`
- Errors used by session → `pkg/session/errors.go`
- Errors used by webauthn (already in `pkg/credential/webauthn/errors.go` from Step 2) — merge any auth-side webauthn errors here.

```bash
git rm pkg/auth/errors.go
```

- [ ] **Step 5: Rewrite package declarations**

For each moved file, the `package auth` declaration becomes the new package name. Use sed for the batch:

```bash
sed -i 's/^package auth$/package account/'       pkg/account/*.go
sed -i 's/^package auth$/package webauthn/'      pkg/credential/webauthn/*.go
sed -i 's/^package auth$/package enrollment/'    pkg/credential/enrollment/*.go
sed -i 's/^package auth$/package pairing/'       pkg/credential/pairing/*.go
sed -i 's/^package auth$/package session/'       pkg/session/*.go
sed -i 's/^package auth$/package authn/'         pkg/authn/*.go
sed -i 's/^package oidc$/package oidc/'          pkg/protocol/oidc/*.go  # no-op but verifies
```

For `pkg/protocol/oidc/oidc.go`, the package name stays `oidc` (no change). But importers will use `prohibitorum/pkg/protocol/oidc` instead of `prohibitorum/pkg/oidc`.

- [ ] **Step 6: Rewrite imports across the codebase**

```bash
# Picotera's identifiers all use "prohibitorum/pkg/auth" — replace per the map.
grep -rl 'prohibitorum/pkg/auth' pkg/ cmd/ | while read -r f; do
  # account-related imports
  sed -i 's|prohibitorum/pkg/auth"|prohibitorum/pkg/account"|g' "$f"
done
```

The single-replace above is wrong because `pkg/auth` mapped to MULTIPLE packages. Don't use a global sed. Instead, fix imports per-file by hand:

For each file in `pkg/server/*.go` and `cmd/prohibitorum/main.go`:
1. Look at what auth identifiers it uses: `auth.Account`, `auth.Session`, `auth.IssueEnrollment`, `auth.RateLimiter`, etc.
2. Replace each with the new package: `account.Account`, `session.Session`, `enrollment.Issue`, `authn.RateLimiter`, etc.
3. Update the import block.

```bash
# Find what each importer uses:
grep -n 'auth\.' pkg/server/*.go cmd/prohibitorum/main.go
grep -n '"prohibitorum/pkg/auth"' pkg/server/*.go cmd/prohibitorum/main.go
grep -n '"prohibitorum/pkg/oidc"' pkg/server/*.go cmd/prohibitorum/main.go
```

For each grep hit, edit the file: replace `auth.<Symbol>` with `<newpackage>.<Symbol>` and add/replace the import line. Symbol-to-package map:

| Symbol | Now lives in |
|---|---|
| `auth.Account`, `auth.LoadAccount`, `auth.ListAccounts`, `auth.PermissionsView` (delete in Task 3) | `account` |
| `auth.WebAuthn`, `auth.NewWebAuthn`, `auth.WebAuthnCredentials`, `auth.RegistrationOptions`, etc. | `webauthn` (alias `webauthnauth` in importers to avoid clash with `go-webauthn/webauthn`) |
| `auth.IssueEnrollment`, `auth.LoadEnrollment`, `auth.ConsumeEnrollment`, `auth.EnrollmentTemplate`, `auth.Intent*`, `auth.ErrEnrollment*` | `enrollment` |
| `auth.Pairing*`, `auth.ErrPairing*` | `pairing` |
| `auth.Session`, `auth.LoadSession`, `auth.SessionData`, `auth.FreshSessionCookie` | `session` |
| `auth.RateLimiter`, `auth.NewRateLimiter`, `auth.RequireFreshSudo` | `authn` |
| `oidc.Provider`, `oidc.NewProvider` | `prohibitorum/pkg/protocol/oidc` (alias unchanged) |

Within moved files themselves: if `pkg/account/account.go` references functions that were in `pkg/auth/middleware.go`, update those internal references too.

- [ ] **Step 7: Delete empty pkg/auth and pkg/oidc directories**

```bash
ls pkg/auth/ pkg/oidc/  # should be empty
rmdir pkg/auth pkg/oidc
```

- [ ] **Step 8: Build and test**

```bash
go build ./...
go test ./...
```

Expected: both green. If any test fails because of import path drift, fix it. Do not commit until both are clean.

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "$(cat <<'EOF'
refactor: split pkg/auth into account / credential / session / authn

Atomic reorganization per Approach A in 2026-05-24-multi-protocol-
rescope-design.md §Architecture. No semantic changes — only file moves,
package declarations, and import paths. Identifier names, function
bodies, and schema references unchanged.

pkg/auth/ and pkg/oidc/ are deleted. pkg/protocol/oidc/ now houses
the OIDC OP code.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Cosmetic decoupling — errorx.Error, RPDisplayName via config

**Goal:** Rename `errorx.PicoTeraError` to `errorx.Error` everywhere. Lift the hardcoded `RPDisplayName: "PicoTera"` constant in `pkg/credential/webauthn/webauthn.go` into `configx.Config.WebAuthn.RPDisplayName` (default `"Prohibitorum"`). Remove or update remaining "picotera" doc comments in the moved code.

**Files:**
- Modify: `pkg/errorx/errorx.go` — rename type, all method receivers, helper functions
- Modify: every file that references `PicoTeraError` — update to `Error`
- Modify: `pkg/configx/configx.go` — add `WebAuthn` substruct with `RPDisplayName string`
- Modify: `pkg/credential/webauthn/webauthn.go` — read RPDisplayName from config
- Modify: `pkg/server/server.go` — drop or rewrite the "mirrors picotera's…" comment
- Modify: `db/migrations/001_initial.sql` — drop "lifted from picotera" header (this file will be fully rewritten in Task 3 anyway, but make it picotera-free now to keep doc state consistent)

**Acceptance Criteria:**
- [ ] `grep -rn PicoTeraError pkg/ cmd/` returns no hits
- [ ] `grep -rn 'PicoTera"' pkg/` returns no hits
- [ ] `configx.Config` has `WebAuthn.RPDisplayName` with default `"Prohibitorum"`
- [ ] `pkg/credential/webauthn/webauthn.go` reads `cfg.WebAuthn.RPDisplayName`
- [ ] `go build ./...` succeeds; `go test ./...` passes

**Verify:** `! grep -rn PicoTera pkg/ cmd/ db/ && go test ./...`

**Steps:**

- [ ] **Step 1: Rename PicoTeraError → Error in pkg/errorx**

Open `pkg/errorx/errorx.go`. The file defines `PicoTeraError struct`, method receivers on it (`Error()`, `GetStatus()`, etc.), and helper functions (`newErr` or similar). Replace all `PicoTeraError` with `Error`:

```bash
sed -i 's/PicoTeraError/Error/g' pkg/errorx/*.go
```

This collapses the type name. There may already be an `Error` method on the struct (since the type implements the `error` interface). After sed, you may have `func (e *Error) Error() string` — that's still valid (type named Error with method Error). Verify by reading the file.

- [ ] **Step 2: Update all call sites**

```bash
grep -rl PicoTeraError pkg/ cmd/ | xargs -r sed -i 's/PicoTeraError/Error/g'
grep -rn PicoTeraError pkg/ cmd/   # expect no output
```

- [ ] **Step 3: Add WebAuthn substruct to configx**

Open `pkg/configx/configx.go`. Find the top-level `Config` struct. Add a `WebAuthn` substruct:

```go
type WebAuthnConfig struct {
    RPID          string
    RPDisplayName string
    RPOrigins     []string
}
```

Add field on `Config`:
```go
WebAuthn WebAuthnConfig `envPrefix:"WEBAUTHN_"`
```

(Match the existing field-tag convention in `configx.go` — read the file first to see whether it uses `env`, `envPrefix`, or some other tag scheme.)

In the `LoadConfig` function (or whatever loads defaults), set `RPDisplayName` default to `"Prohibitorum"` if the env var is empty:

```go
if cfg.WebAuthn.RPDisplayName == "" {
    cfg.WebAuthn.RPDisplayName = "Prohibitorum"
}
```

- [ ] **Step 4: Wire RPDisplayName from config in webauthn**

Open `pkg/credential/webauthn/webauthn.go`. Find the constant or literal `"PicoTera"` (it's currently in `NewWebAuthn`'s construction of `webauthn.Config`). Change the function signature to accept `*configx.Config` (or just the `WebAuthnConfig` substruct), and read `RPDisplayName` from there:

```go
func NewWebAuthn(cfg configx.WebAuthnConfig) (*webauthn.WebAuthn, error) {
    return webauthn.New(&webauthn.Config{
        RPID:          cfg.RPID,
        RPDisplayName: cfg.RPDisplayName,
        RPOrigins:     cfg.RPOrigins,
    })
}
```

Update all callers of `NewWebAuthn` (probably `pkg/server/server.go` and tests) to pass the config struct.

- [ ] **Step 5: Update doc comments**

Search for "picotera" / "PicoTera" mentions in comments (case-insensitive) and rewrite them to describe Prohibitorum on its own terms:

```bash
grep -rni picotera pkg/ cmd/ db/
```

For each hit, edit the file. Comments to actively rewrite:
- `pkg/server/server.go` header comment ("mirrors picotera's but…")
- `db/migrations/001_initial.sql` header comment ("Identity schema lifted from picotera…")
- Any other inline doc comments referencing picotera

Skip comments in the spec/audit reports (those are intentional historical references).

- [ ] **Step 6: Build and test**

```bash
go build ./...
go test ./...
grep -rni picotera pkg/ cmd/ db/   # expect no output
```

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "$(cat <<'EOF'
refactor: rename errorx.PicoTeraError → errorx.Error; lift RPDisplayName to config

Cosmetic decoupling pass. Type name and WebAuthn relying-party display
name no longer hardcode picotera's branding. configx.Config gains a
WebAuthn substruct that holds RPID / RPDisplayName / RPOrigins; default
RPDisplayName is "Prohibitorum".

No behavioral changes; tests unchanged.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Schema rewrite — migrations 001+002 + queries + Go shape

**Goal:** Rewrite the two existing migrations in place per the spec, regenerate sqlc types, drop picotera-flavored fields from contract / handlers / account / enrollment, and add the four new core tables (session, credential_event, auth_throttle, revoked_jti) with queries. End state: schema matches §"Data model" §`db/migrations/001_initial.sql` and §`db/migrations/002_oidc.sql`; `mise run db:up` applies cleanly; `go build ./...` clean; existing tests updated to the new shapes.

**This is the largest task.** Allow a single coherent commit.

**Files:**
- Modify: `db/migrations/001_initial.sql` — full rewrite
- Modify: `db/migrations/002_oidc.sql` — full rewrite
- Modify: `db/queries/account.sql` — rewrite using attributes
- Modify: `db/queries/enrollment.sql` — rewrite using template_attributes + expected_upstream_idp_slug
- Modify: `db/queries/webauthn_credential.sql` — add columns
- Modify: `db/queries/oidc.sql` — rewrite for signing_key + extended oidc_client + revoked_jti
- Create: `db/queries/session.sql`
- Create: `db/queries/credential_event.sql`
- Create: `db/queries/auth_throttle.sql`
- Modify: `sqlc.yaml` — extend overrides for new SERIAL columns to int32
- Regen: `pkg/db/` (sqlc output)
- Modify: `pkg/contract/auth.go` — drop Permission/Permissions; add Attributes; remove RequirePermission
- Modify: `pkg/account/account.go` — drop PermissionsView + HasPermission; rewrite ListAccounts projection for attributes
- Modify: `pkg/account/account_test.go` — adjust to attributes
- Modify: `pkg/credential/enrollment/enrollment.go` — `EnrollmentTemplate.Perms` → `Attributes map[string]any`; binding params
- Modify: `pkg/credential/enrollment/enrollment_test.go` — adjust
- Modify: `pkg/credential/webauthn/webauthn.go` — wire user_handle / cose_alg / uv_initialized into the credential projection
- Modify: `pkg/server/handle_account.go` — drop CanXxx references; use attributes map
- Modify: `pkg/server/handle_enrollment.go` — same
- Modify: `pkg/server/handle_me.go` — adapt SessionView
- Modify: `pkg/server/handle_auth.go` — adapt sessionView projection
- Modify: `pkg/server/server.go` — drop any references to RequirePermission

**Acceptance Criteria:**
- [ ] `mise run db:up` applies cleanly against a fresh Postgres
- [ ] `sqlc generate` (via the project's mise task or `sqlc generate` direct) produces `pkg/db/` with no errors
- [ ] `go build ./...` succeeds
- [ ] `go test ./...` passes — tests updated to reflect attributes-based model
- [ ] `grep -rn 'can_view_own\|can_manage_own\|view_models\|PermViewOwnUsage' pkg/ cmd/ db/` returns no hits
- [ ] `db/migrations/001_initial.sql` contains: account, session, webauthn_credential, enrollment, credential_event, auth_throttle
- [ ] `db/migrations/002_oidc.sql` contains: signing_key (unified, with use/not_before), oidc_client (extended), revoked_jti

**Verify:**
```
PROHIBITORUM_DATABASE_URL=postgres://test mise run db:up && go build ./... && go test ./...
```

(Run against a disposable Postgres database — `docker run --rm postgres:16` or equivalent.)

**Steps:**

- [ ] **Step 1: Set up a disposable Postgres for verification**

```bash
# Skip if you already have one available.
docker run -d --name prohibitorum-test-pg -e POSTGRES_PASSWORD=test -p 55432:5432 postgres:16
export PROHIBITORUM_DATABASE_URL=postgres://postgres:test@localhost:55432/postgres?sslmode=disable
# Verify connectivity:
psql "$PROHIBITORUM_DATABASE_URL" -c 'SELECT 1'
```

If you're not using Docker, point `PROHIBITORUM_DATABASE_URL` at any Postgres 14+ you have access to. The database should be empty before this task starts.

- [ ] **Step 2: Rewrite migration 001**

Replace `db/migrations/001_initial.sql` entirely with the schema documented in `docs/superpowers/specs/2026-05-24-multi-protocol-rescope-design.md` §`db/migrations/001_initial.sql` (lines 147–248 of the spec). Add `-- +goose Up` header and a `-- +goose Down` block at the bottom that drops the tables in reverse dependency order:

```sql
-- +goose Down
DROP TABLE IF EXISTS auth_throttle;
DROP TABLE IF EXISTS credential_event;
DROP TABLE IF EXISTS enrollment;
DROP TABLE IF EXISTS webauthn_credential;
DROP TABLE IF EXISTS session;
DROP TABLE IF EXISTS account;
```

**Two adjustments from the spec text** to keep sqlc-int32 conventions:
- Change `id bigserial PRIMARY KEY` → `id SERIAL PRIMARY KEY` on `account` and `webauthn_credential` (matches existing v0.1 behavior).
- `credential_event.id` keeps `bigserial` (it's an audit log; expected to grow large).

Verify with goose:

```bash
mise run db:up
mise run db:status
# Expected: 001_initial.sql shows "Applied"
```

If migration fails (syntax / dependency order), fix the SQL and re-run.

- [ ] **Step 3: Rewrite migration 002**

Replace `db/migrations/002_oidc.sql` entirely with the schema in spec §`db/migrations/002_oidc.sql` (lines ~250–308). Same `-- +goose Up` / `-- +goose Down` envelope.

```bash
mise run db:status  # 002 should now show "Pending"
# Reset and re-run from scratch to confirm both apply cleanly:
goose -dir db/migrations postgres "$PROHIBITORUM_DATABASE_URL" reset
mise run db:up
mise run db:status  # both 001 and 002 should show "Applied"
```

- [ ] **Step 4: Update sqlc.yaml for new SERIAL columns**

Open `sqlc.yaml`. Add overrides for new tables whose IDs should remain int32 in Go (matches existing conventions):

```yaml
overrides:
  - column: "account.id"
    go_type: "int32"
  - column: "webauthn_credential.id"
    go_type: "int32"
  - column: "webauthn_credential.account_id"
    go_type: "int32"
  # NEW — for any column referencing account.id, plus new bigserial PKs:
  - column: "webauthn_credential.user_handle"
    go_type:
      type: "[]byte"
  - column: "session.account_id"
    go_type: "int32"
  - column: "session.upstream_idp_id"
    go_type: "*int32"
  - column: "credential_event.account_id"
    go_type: "*int32"
  - column: "auth_throttle.account_id"
    go_type: "int32"
```

(For columns added in migrations 003/004/005, defer overrides to those tasks.)

- [ ] **Step 5: Rewrite queries — account.sql**

Replace `db/queries/account.sql`. Strip every `can_*` column reference; replace with `attributes`. Example shape:

```sql
-- name: GetAccountByID :one
SELECT * FROM account WHERE id = $1;

-- name: GetAccountByUsername :one
SELECT * FROM account WHERE username = $1;

-- name: ListAccounts :many
SELECT id, username, display_name, role, attributes, disabled, created_at, updated_at
FROM account
ORDER BY id
LIMIT $1 OFFSET $2;

-- name: InsertAccount :one
INSERT INTO account (
  username, display_name, webauthn_user_handle, role, attributes, disabled
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateAccount :exec
UPDATE account
SET display_name = $2, role = $3, attributes = $4, disabled = $5,
    updated_at = now()
WHERE id = $1;

-- name: DisableAccount :exec
UPDATE account SET disabled = TRUE, updated_at = now() WHERE id = $1;
```

(Read the v0.1 file to capture every existing query name — preserve them by name so handlers don't need to rename calls, just change column references.)

- [ ] **Step 6: Rewrite queries — enrollment.sql**

Strip `template_can_*` references; replace with `template_attributes jsonb`. Add `expected_upstream_idp_slug` parameter on `InsertEnrollment` (nullable). Preserve the existing query names (`GetEnrollmentByToken`, `InsertEnrollment`, `ConsumeEnrollment`, `ConsumeInviteEnrollment`, etc.) so handler code only needs param-list adjustments.

- [ ] **Step 7: Rewrite queries — webauthn_credential.sql**

Add the new columns to INSERT and SELECT statements:
- `user_handle` (required)
- `cose_alg` (required)
- `uv_initialized` (boolean, defaults to false)
- `clone_warning_at` (nullable, set via dedicated UPDATE)

Add a new query for setting clone_warning_at:

```sql
-- name: SetCredentialCloneWarning :exec
UPDATE webauthn_credential
SET clone_warning_at = now()
WHERE id = $1 AND clone_warning_at IS NULL;
```

- [ ] **Step 8: Rewrite queries — oidc.sql**

Cover `signing_key` (was `oidc_signing_key`), expanded `oidc_client` (all new columns), and `revoked_jti`. Sample queries:

```sql
-- name: GetActiveSigningKey :one
SELECT * FROM signing_key
WHERE active AND use = $1
LIMIT 1;

-- name: ListSigningKeys :many
SELECT * FROM signing_key
WHERE retired_at IS NULL OR retired_at > $1
ORDER BY created_at DESC;

-- name: InsertSigningKey :one
INSERT INTO signing_key (kid, algorithm, use, public_jwk, x509_cert_pem, private_pem, active, not_before)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: RetireSigningKey :exec
UPDATE signing_key SET retired_at = now(), active = FALSE WHERE kid = $1;

-- name: GetOIDCClient :one
SELECT * FROM oidc_client WHERE client_id = $1 AND NOT disabled;

-- name: ListOIDCClients :many
SELECT * FROM oidc_client ORDER BY display_name;

-- name: InsertOIDCClient :one
INSERT INTO oidc_client (...) VALUES (...) RETURNING *;
-- (enumerate every column from spec migration 002)

-- name: RevokeJTI :exec
INSERT INTO revoked_jti (jti, expires_at, reason) VALUES ($1, $2, $3)
ON CONFLICT (jti) DO NOTHING;

-- name: IsJTIRevoked :one
SELECT EXISTS(SELECT 1 FROM revoked_jti WHERE jti = $1) AS revoked;

-- name: PruneRevokedJTI :exec
DELETE FROM revoked_jti WHERE expires_at < now();
```

- [ ] **Step 9: Create db/queries/session.sql**

```sql
-- name: InsertSession :one
INSERT INTO session (id, account_id, auth_time, amr, acr)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetSession :one
SELECT * FROM session WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeSession :exec
UPDATE session SET revoked_at = now() WHERE id = $1;

-- name: ListSessionsByAccount :many
SELECT * FROM session
WHERE account_id = $1 AND revoked_at IS NULL
ORDER BY created_at DESC;

-- name: RevokeAllSessionsByAccount :exec
UPDATE session SET revoked_at = now()
WHERE account_id = $1 AND revoked_at IS NULL;
```

- [ ] **Step 10: Create db/queries/credential_event.sql**

```sql
-- name: InsertCredentialEvent :exec
INSERT INTO credential_event (account_id, factor, event, credential_ref, ip, user_agent, detail)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListCredentialEventsByAccount :many
SELECT * FROM credential_event
WHERE account_id = $1
ORDER BY at DESC
LIMIT $2 OFFSET $3;

-- name: ListCredentialEventsByFactor :many
SELECT * FROM credential_event
WHERE factor = $1 AND at > $2
ORDER BY at DESC
LIMIT $3;
```

- [ ] **Step 11: Create db/queries/auth_throttle.sql**

```sql
-- name: GetAuthThrottle :one
SELECT * FROM auth_throttle WHERE account_id = $1 AND factor = $2;

-- name: UpsertAuthThrottle :one
INSERT INTO auth_throttle (account_id, factor, failed_attempts, window_start, locked_until)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (account_id, factor) DO UPDATE
SET failed_attempts = EXCLUDED.failed_attempts,
    window_start = EXCLUDED.window_start,
    locked_until = EXCLUDED.locked_until
RETURNING *;

-- name: ResetAuthThrottle :exec
DELETE FROM auth_throttle WHERE account_id = $1 AND factor = $2;
```

- [ ] **Step 12: Regenerate sqlc types**

```bash
sqlc generate
```

Expected: no errors. The `pkg/db/` directory's contents change substantially — many new query functions, removed `*CanXxx*` fields on `Account` and `Enrollment`, new `Session` / `CredentialEvent` / `AuthThrottle` types.

If sqlc complains about column types it can't infer, add overrides in `sqlc.yaml` and re-run.

- [ ] **Step 13: Drop Permission / Permissions from contract**

Open `pkg/contract/auth.go`. Delete:
- `Permission` type
- `PermViewOwnUsage` / `PermManageOwnAPIKeys` / `PermViewModels` / `PermViewOwnTraces` / `PermManageOwnProjects` constants
- `AuthPermissionKind` constant from `AuthKind` enum
- `RequirePermission` function
- `Permissions` struct
- `Permissions` field from `SessionView`, `AccountView`, `EnrollmentTemplate`

Add to `SessionView`, `AccountView`, `EnrollmentTemplate`:

```go
Attributes map[string]any `json:"attributes,omitempty"`
```

For `AuthKind`, the remaining variants are `AuthPublic`, `AuthSession`, `AuthAdmin`. Renumber if needed.

- [ ] **Step 14: Rewrite pkg/account/account.go**

Remove:
- `PermissionsView` function
- `HasPermission` function
- Any reference to `db.Account.CanXxx` fields (they no longer exist)

The account projection becomes:

```go
func AccountView(a db.Account) contract.AccountView {
    return contract.AccountView{
        ID:           a.ID,
        Username:     a.Username,
        DisplayName:  a.DisplayName,
        Role:         a.Role,
        Attributes:   decodeAttributes(a.Attributes),
        Disabled:     a.Disabled,
        CreatedAt:    a.CreatedAt.Time,
        UpdatedAt:    a.UpdatedAt.Time,
    }
}

// decodeAttributes converts the sqlc-generated JSONB-bytes type into a map.
// sqlc with pgx-v5 returns jsonb as []byte; unmarshal into map[string]any.
func decodeAttributes(raw []byte) map[string]any {
    if len(raw) == 0 {
        return nil
    }
    var m map[string]any
    if err := json.Unmarshal(raw, &m); err != nil {
        return nil
    }
    return m
}
```

Update `pkg/account/account_test.go` to assert against `Attributes` instead of the deleted permission flags. If a test specifically exercised `HasPermission`, delete it (the function is gone) — those gates now live in handler code via attribute lookups (`attrs["can_admin_users"] == true`, etc.).

- [ ] **Step 15: Rewrite pkg/credential/enrollment/enrollment.go**

Change `EnrollmentTemplate`:

```go
type EnrollmentTemplate struct {
    Username    string
    DisplayName string
    Role        string
    Attributes  map[string]any
    ExpectedUpstreamIDPSlug *string
}
```

In `IssueEnrollment`, marshal `Attributes` to JSON bytes for the `template_attributes jsonb` column:

```go
var tplAttrs []byte
if tpl != nil && tpl.Attributes != nil {
    tplAttrs, err = json.Marshal(tpl.Attributes)
    if err != nil {
        return "", time.Time{}, fmt.Errorf("enrollment: marshal attributes: %w", err)
    }
}
params := db.InsertEnrollmentParams{
    Token:    token,
    Intent:   string(intent),
    // ... existing fields ...
    TemplateAttributes: tplAttrs,  // nullable jsonb
    ExpectedUpstreamIdpSlug: pgtypeText(tpl.ExpectedUpstreamIDPSlug),
}
```

Update `pkg/credential/enrollment/enrollment_test.go` accordingly.

- [ ] **Step 16: Update pkg/credential/webauthn/webauthn.go**

Where the file currently inserts a `webauthn_credential`, add the new required columns:

```go
params := db.InsertWebauthnCredentialParams{
    AccountID:      accountID,
    CredentialID:   cred.ID,
    PublicKey:      cred.PublicKey,
    CoseAlg:        int32(coseAlgFromAttestation(att)),  // helper, parses COSE_Key from attestation
    UserHandle:     userHandle,
    SignCount:      int64(cred.Authenticator.SignCount),
    Transports:     transportsToText(cred.Transports),
    Aaguid:         cred.Authenticator.AAGUID,
    AttestationType: pgtypeText(&attType),
    BackupEligible: pgtypeBool(&cred.Flags.BackupEligible),
    BackupState:    pgtypeBool(&cred.Flags.BackupState),
    UvInitialized:  cred.Flags.UserVerified,
    Nickname:       pgtypeText(nickname),
}
```

Add a `coseAlgFromAttestation` helper. The simplest implementation reads the COSE_Key parsed by go-webauthn: `cred.PublicKey` is the marshalled CBOR; go-webauthn exposes the algorithm via `cred.Authenticator.PublicKeyAlgorithm` (verify the exact field name against the library version pinned in `go.mod`).

For sign-count regression detection (`clone_warning_at`), find where `UpdateCredentialUsage` is called (currently in `pkg/server/handle_auth.go` and `handle_sudo.go`). Before calling that update, check `if newSignCount < oldSignCount { q.SetCredentialCloneWarning(...) }`. Don't refuse the login — just stamp the warning. Add the regression check at the call site, not inside `UpdateCredentialUsage`, so the latter stays mechanical.

- [ ] **Step 17: Update handlers — handle_account.go, handle_enrollment.go, handle_me.go, handle_auth.go, server.go**

For each:
1. Remove every `CanViewOwnUsage` / `CanManageOwnApiKeys` / etc. field reference.
2. Replace `accountViewFromRow` / `accountViewFromAccount` to use the new `account.AccountView` helper (or inline `decodeAttributes`).
3. Replace `Permissions: perms` in handler output with `Attributes: attrs` (decoded from `db.Account.Attributes`).
4. In `handle_account.go`, the invite-default and admin-bootstrap code paths that set `Perms: contract.Permissions{ViewOwnUsage: true, ...}` become `Attributes: nil` (admin role is enough for the bootstrap admin; invite defaults to no attributes — admin can edit post-consume).
5. Remove `RequirePermission` from the operation-registration calls in `server.go`. Replace with the appropriate `RequireAdmin` or a role check inside the handler.

The "changes" map in handle_account.go's `EditAccount` audit log loses its picotera entries — drop them. Add one diff entry if `attributes` changed (compare maps):

```go
if !reflect.DeepEqual(current.Attributes, updated.Attributes) {
    changes["attributes"] = []any{current.Attributes, updated.Attributes}
}
```

- [ ] **Step 18: Build and test**

```bash
go build ./...
go test ./...
grep -rn 'can_view_own\|can_manage_own\|view_models\|PermViewOwnUsage' pkg/ cmd/ db/  # expect no output
```

If tests fail because they hardcode old permission shapes, update them. If a test deeply exercised picotera permission semantics and has no natural translation to attributes, delete it (we'll add attribute-specific gating tests when a real RP uses them).

- [ ] **Step 19: Run migrations end-to-end**

```bash
goose -dir db/migrations postgres "$PROHIBITORUM_DATABASE_URL" reset
mise run db:up
mise run db:status  # 001 + 002 Applied
psql "$PROHIBITORUM_DATABASE_URL" -c "\dt"  # list all tables — confirm shape
psql "$PROHIBITORUM_DATABASE_URL" -c "\d account"  # confirm attributes jsonb + no can_*
psql "$PROHIBITORUM_DATABASE_URL" -c "\d session"
psql "$PROHIBITORUM_DATABASE_URL" -c "\d webauthn_credential"  # confirm user_handle, cose_alg, uv_initialized, clone_warning_at
psql "$PROHIBITORUM_DATABASE_URL" -c "\d oidc_client"  # confirm all the new columns
```

- [ ] **Step 20: Commit**

```bash
git add -A
git commit -m "$(cat <<'EOF'
schema: rewrite migrations 001+002 — attributes jsonb, unified signing_key, audit tables

Replaces picotera-flavored boolean permission columns with attributes
jsonb on account and template_attributes on enrollment. Generalizes
oidc_signing_key → signing_key (use sig|enc, not_before) so both OIDC
and SAML rotate from one keyset.

Adds session, credential_event, auth_throttle, revoked_jti tables to
satisfy audit findings (OIDC Core §2 auth_time/amr/acr/sid; RFC 9068
jti revocation; RFC 4226 §7.3 cross-restart throttle; NIST §4 audit).

oidc_client gains ~10 columns required by the audit (post_logout_
redirect_uris, token_endpoint_auth_method, id_token_signed_response_
alg, subject_type, application_type, allowed_code_challenge_methods,
default_max_age, require_auth_time, contacts, logo_uri, tos_uri,
policy_uri, disabled).

webauthn_credential gains user_handle / cose_alg / uv_initialized /
clone_warning_at per WebAuthn L3 §4.

Picotera vocabulary stripped from sqlc-generated types, pkg/contract,
pkg/account, pkg/credential/enrollment, and all server handlers.
Permission / Permissions / RequirePermission removed; AccountView
gains Attributes map[string]any. Admin-bootstrap and invite-default
code paths no longer carry picotera-specific claims.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Migration 003 — password + TOTP + recovery_code

**Goal:** Add the credential storage tables for v0.2. Pure schema + queries + sqlc regen; no Go logic yet.

**Files:**
- Create: `db/migrations/003_password_totp.sql`
- Create: `db/queries/password_credential.sql`
- Create: `db/queries/totp_credential.sql`
- Create: `db/queries/recovery_code.sql`
- Modify: `sqlc.yaml` — overrides for new account_id refs
- Regen: `pkg/db/`

**Acceptance Criteria:**
- [ ] `mise run db:up` applies 003 cleanly on top of 001+002
- [ ] `sqlc generate` succeeds; `PasswordCredential`, `TotpCredential`, `RecoveryCode` types appear in `pkg/db/`
- [ ] `go build ./...` succeeds (nothing imports the new types yet, but the package compiles)

**Verify:** `mise run db:up && sqlc generate && go build ./...`

**Steps:**

- [ ] **Step 1: Write migration 003**

Replace the spec's §`db/migrations/003_password_totp.sql` (lines ~309–343 of the spec). Add the `-- +goose Up` / `-- +goose Down` envelope. Down drops in reverse order:

```sql
-- +goose Down
DROP TABLE IF EXISTS recovery_code;
DROP TABLE IF EXISTS totp_credential;
DROP TABLE IF EXISTS password_credential;
```

- [ ] **Step 2: Apply**

```bash
mise run db:up
mise run db:status   # 003 Applied
psql "$PROHIBITORUM_DATABASE_URL" -c "\d password_credential"
psql "$PROHIBITORUM_DATABASE_URL" -c "\d totp_credential"
psql "$PROHIBITORUM_DATABASE_URL" -c "\d recovery_code"
```

- [ ] **Step 3: Write password_credential queries**

`db/queries/password_credential.sql`:

```sql
-- name: GetPasswordCredential :one
SELECT * FROM password_credential WHERE account_id = $1;

-- name: UpsertPasswordCredential :exec
INSERT INTO password_credential (account_id, hash)
VALUES ($1, $2)
ON CONFLICT (account_id) DO UPDATE
SET hash = EXCLUDED.hash,
    password_changed_at = now(),
    updated_at = now();

-- name: DeletePasswordCredential :exec
DELETE FROM password_credential WHERE account_id = $1;
```

- [ ] **Step 4: Write totp_credential queries**

```sql
-- name: GetTOTPCredential :one
SELECT * FROM totp_credential WHERE account_id = $1;

-- name: InsertTOTPCredential :one
INSERT INTO totp_credential (account_id, secret_enc, secret_nonce, key_version, period, digits, algorithm)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ConfirmTOTPCredential :exec
UPDATE totp_credential SET confirmed_at = now() WHERE account_id = $1 AND confirmed_at IS NULL;

-- name: UpdateTOTPLastStep :exec
UPDATE totp_credential SET last_step = $2 WHERE account_id = $1 AND $2 > last_step;

-- name: DeleteTOTPCredential :exec
DELETE FROM totp_credential WHERE account_id = $1;
```

- [ ] **Step 5: Write recovery_code queries**

```sql
-- name: ListRecoveryCodesByAccount :many
SELECT * FROM recovery_code
WHERE account_id = $1 AND used_at IS NULL
ORDER BY id;

-- name: InsertRecoveryCode :one
INSERT INTO recovery_code (account_id, hash) VALUES ($1, $2) RETURNING *;

-- name: ConsumeRecoveryCode :one
UPDATE recovery_code
SET used_at = now(), used_session_id = $2, used_ip = $3
WHERE id = $1 AND used_at IS NULL
RETURNING *;

-- name: DeleteAllRecoveryCodesByAccount :exec
DELETE FROM recovery_code WHERE account_id = $1;
```

- [ ] **Step 6: Update sqlc.yaml overrides**

Add:

```yaml
  - column: "password_credential.account_id"
    go_type: "int32"
  - column: "totp_credential.account_id"
    go_type: "int32"
  - column: "recovery_code.account_id"
    go_type: "int32"
```

- [ ] **Step 7: Regen + build**

```bash
sqlc generate
go build ./...
```

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "$(cat <<'EOF'
schema: migration 003 — password_credential, totp_credential, recovery_code

Password hashes use PHC argon2id format (algorithm metadata in the hash
string, not a separate column). TOTP stores AES-GCM ciphertext +
key_version per audit (DEK rotation); last_step prevents same-step
replay (RFC 6238 §5.2). Recovery codes argon2id-hashed; redemption
context (session, IP) captured for audit.

Queries and sqlc types generated; no Go usage yet — v0.2 work consumes them.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Migration 004 — federation tables

**Goal:** Add `upstream_idp` and `account_identity` per spec, plus the forward FK from `session` to `upstream_idp`.

**Files:**
- Create: `db/migrations/004_federation.sql`
- Create: `db/queries/upstream_idp.sql`
- Create: `db/queries/account_identity.sql`
- Modify: `sqlc.yaml`
- Regen: `pkg/db/`

**Acceptance Criteria:**
- [ ] `mise run db:up` applies cleanly
- [ ] `session.upstream_idp_id` exists post-migration
- [ ] `sqlc generate` + `go build ./...` succeed

**Verify:** `mise run db:up && sqlc generate && go build ./...`

**Steps:**

- [ ] **Step 1: Write migration 004**

Per spec §`db/migrations/004_federation.sql` (lines ~344–390). Goose envelope; Down reverses the ALTER and drops the tables:

```sql
-- +goose Down
ALTER TABLE session DROP COLUMN IF EXISTS upstream_idp_id;
DROP TABLE IF EXISTS account_identity;
DROP TABLE IF EXISTS upstream_idp;
```

- [ ] **Step 2: Apply**

```bash
mise run db:up
psql "$PROHIBITORUM_DATABASE_URL" -c "\d upstream_idp"
psql "$PROHIBITORUM_DATABASE_URL" -c "\d account_identity"
psql "$PROHIBITORUM_DATABASE_URL" -c "\d session"   # confirm upstream_idp_id added
```

- [ ] **Step 3: Write upstream_idp queries**

```sql
-- name: GetUpstreamIDPBySlug :one
SELECT * FROM upstream_idp WHERE slug = $1 AND NOT disabled;

-- name: ListUpstreamIDPs :many
SELECT * FROM upstream_idp WHERE NOT disabled ORDER BY display_name;

-- name: InsertUpstreamIDP :one
INSERT INTO upstream_idp (slug, display_name, issuer_url, client_id,
  client_secret_enc, secret_nonce, key_version, scopes, mode,
  allowed_domains, username_claim, display_name_claim, email_claim)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING *;

-- name: UpdateUpstreamIDP :exec
UPDATE upstream_idp
SET display_name = $2, issuer_url = $3, client_id = $4,
    client_secret_enc = $5, secret_nonce = $6, key_version = $7,
    scopes = $8, mode = $9, allowed_domains = $10,
    username_claim = $11, display_name_claim = $12, email_claim = $13,
    disabled = $14
WHERE id = $1;

-- name: DeleteUpstreamIDP :exec
DELETE FROM upstream_idp WHERE id = $1;
```

- [ ] **Step 4: Write account_identity queries**

```sql
-- name: GetAccountIdentityByIssuerSub :one
SELECT * FROM account_identity WHERE upstream_iss = $1 AND upstream_sub = $2;

-- name: ListAccountIdentitiesByAccount :many
SELECT ai.*, ip.slug, ip.display_name AS idp_display_name
FROM account_identity ai
JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
WHERE ai.account_id = $1;

-- name: InsertAccountIdentity :one
INSERT INTO account_identity (account_id, upstream_idp_id, upstream_iss, upstream_sub, upstream_email)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: DeleteAccountIdentity :exec
DELETE FROM account_identity WHERE id = $1 AND account_id = $2;
```

- [ ] **Step 5: sqlc overrides**

```yaml
  - column: "upstream_idp.id"
    go_type: "int32"
  - column: "account_identity.account_id"
    go_type: "int32"
  - column: "account_identity.upstream_idp_id"
    go_type: "int32"
```

- [ ] **Step 6: Regen + build + commit**

```bash
sqlc generate
go build ./...
git add -A
git commit -m "$(cat <<'EOF'
schema: migration 004 — upstream_idp + account_identity

Federation tables per the spec. account_identity unique key is
(upstream_iss, upstream_sub) per OIDC Core §2 — sub uniqueness is
per-issuer, so the FK to upstream_idp.id is for cascade ergonomics
only, not for keying.

session.upstream_idp_id added (forward FK from migration 001's session
table) so we can answer "which IdP authenticated this session?" for
ID-token amr/acr claims.

Queries + sqlc types generated; no Go usage yet — v0.3 work consumes them.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Migration 005 — SAML tables

**Goal:** Add the five SAML tables (saml_sp + 4 child/companion tables) per spec.

**Files:**
- Create: `db/migrations/005_saml.sql`
- Create: `db/queries/saml_sp.sql`
- Modify: `sqlc.yaml`
- Regen: `pkg/db/`

**Acceptance Criteria:**
- [ ] `mise run db:up` applies cleanly through 005
- [ ] All five SAML tables exist post-migration
- [ ] `sqlc generate` + `go build ./...` succeed

**Verify:** `mise run db:up && sqlc generate && go build ./...`

**Steps:**

- [ ] **Step 1: Write migration 005**

Per spec §`db/migrations/005_saml.sql` (lines ~391–465). Goose envelope; Down drops all five tables in reverse FK order:

```sql
-- +goose Down
DROP TABLE IF EXISTS saml_session;
DROP TABLE IF EXISTS saml_subject_id;
DROP TABLE IF EXISTS saml_sp_key;
DROP TABLE IF EXISTS saml_sp_acs;
DROP TABLE IF EXISTS saml_sp;
```

- [ ] **Step 2: Apply**

```bash
mise run db:up
for t in saml_sp saml_sp_acs saml_sp_key saml_subject_id saml_session; do
  psql "$PROHIBITORUM_DATABASE_URL" -c "\d $t"
done
```

- [ ] **Step 3: Write saml_sp queries**

Cover the parent table + all child tables in one file:

```sql
-- name: GetSAMLSPByEntityID :one
SELECT * FROM saml_sp WHERE entity_id = $1;

-- name: ListSAMLSPs :many
SELECT * FROM saml_sp ORDER BY display_name;

-- name: InsertSAMLSP :one
INSERT INTO saml_sp (entity_id, display_name, sp_kind, name_id_format, name_id_claim,
  attribute_map, want_assertions_signed, authn_requests_signed,
  require_signed_authn_request, session_lifetime, metadata_xml,
  metadata_valid_until, metadata_cache_duration, metadata_fetched_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING *;

-- name: ListSAMLSPACSEndpoints :many
SELECT * FROM saml_sp_acs WHERE sp_id = $1 ORDER BY idx;

-- name: InsertSAMLSPACS :exec
INSERT INTO saml_sp_acs (sp_id, idx, binding, location, is_default)
VALUES ($1, $2, $3, $4, $5);

-- name: ListSAMLSPKeys :many
SELECT * FROM saml_sp_key WHERE sp_id = $1 AND use = $2
ORDER BY added_at DESC;

-- name: InsertSAMLSPKey :exec
INSERT INTO saml_sp_key (sp_id, use, cert_pem, not_after)
VALUES ($1, $2, $3, $4);

-- name: GetSAMLSubjectID :one
SELECT * FROM saml_subject_id WHERE account_id = $1 AND sp_id = $2;

-- name: InsertSAMLSubjectID :one
INSERT INTO saml_subject_id (account_id, sp_id, name_id, name_id_format)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: InsertSAMLSession :one
INSERT INTO saml_session (session_id, sp_id, name_id, session_index, not_on_or_after)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListSAMLSessionsBySession :many
SELECT * FROM saml_session WHERE session_id = $1;

-- name: ListSAMLSessionsByNameID :many
SELECT * FROM saml_session WHERE sp_id = $1 AND name_id = $2;
```

- [ ] **Step 4: sqlc overrides**

```yaml
  - column: "saml_sp.id"
    go_type: "int32"
  - column: "saml_sp_acs.sp_id"
    go_type: "int32"
  - column: "saml_sp_key.sp_id"
    go_type: "int32"
  - column: "saml_subject_id.account_id"
    go_type: "int32"
  - column: "saml_subject_id.sp_id"
    go_type: "int32"
  - column: "saml_session.sp_id"
    go_type: "int32"
```

- [ ] **Step 5: Regen + build + commit**

```bash
sqlc generate
go build ./...
git add -A
git commit -m "$(cat <<'EOF'
schema: migration 005 — SAML tables

saml_sp + saml_sp_acs (multi-endpoint ACS per SAML Metadata §2.4.4) +
saml_sp_key (multi-cert per use, supports rotation + future encryption
SPs) + saml_subject_id (stable pairwise NameID per Core §8.3.7) +
saml_session (forward-compat stub populated from day one; consumed
when SLO lands in v0.5).

attribute_map shape is an ordered jsonb array of
{local, name, friendly_name, name_format, multi} — required for
GHES multi-valued + URI NameFormat attributes (emails, public_keys).

Queries + sqlc types generated; no Go usage yet — v0.5 work consumes them.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Stub package files + configx extensions

**Goal:** Land the empty implementations for v0.2+ packages so the import graph is whole, and extend configx with all the new substructs the spec calls for. No business logic — just signatures, TODO markers, and config plumbing.

**Files:**
- Create: `pkg/credential/password/password.go`
- Create: `pkg/credential/totp/totp.go`
- Create: `pkg/federation/oidc/federation.go`
- Create: `pkg/protocol/saml/saml.go`
- Create: `pkg/authn/flow.go`
- Create: `pkg/audit/event.go`
- Modify: `pkg/configx/configx.go`

**Acceptance Criteria:**
- [ ] Every new package has at least one exported symbol referenced from server wiring (so `go vet` doesn't flag dead code)
- [ ] `pkg/audit.Writer` exposes `Record(ctx, event)` and is wired into `pkg/server/server.go` via constructor injection (even though most call sites land in v0.2+)
- [ ] `configx.Config` has the substructs listed in the spec §"Skeleton commit scope" item 8
- [ ] `go build ./...` succeeds; `go vet ./...` clean

**Verify:** `go build ./... && go vet ./...`

**Steps:**

- [ ] **Step 1: pkg/audit/event.go**

```go
package audit

import (
    "context"
    "net"
    "net/netip"

    "prohibitorum/pkg/db"
)

// Factor identifies the credential type involved in an event. String constants
// to match the database CHECK-friendly column shape.
type Factor string

const (
    FactorWebAuthn       Factor = "webauthn"
    FactorPassword       Factor = "password"
    FactorTOTP           Factor = "totp"
    FactorRecoveryCode   Factor = "recovery_code"
    FactorFederationOIDC Factor = "federation_oidc"
    FactorEnrollment     Factor = "enrollment"
    FactorSession        Factor = "session"
    FactorOIDCClient     Factor = "oidc_client"
    FactorSAMLSP         Factor = "saml_sp"
)

// Event names; not exhaustive — handlers may pass any string.
const (
    EventRegister         = "register"
    EventUse              = "use"
    EventFail             = "fail"
    EventRevoke           = "revoke"
    EventCloneWarning     = "clone_warning"
    EventLink             = "link"
    EventUnlink           = "unlink"
    EventEnrollmentIssued = "enrollment_issued"
    EventSessionStart     = "session_start"
    EventSessionEnd       = "session_end"
)

// Record is the input shape for an audit event.
type Record struct {
    AccountID     *int32
    Factor        Factor
    Event         string
    CredentialRef *int64
    IP            *netip.Addr
    UserAgent     string
    Detail        map[string]any
}

// Writer persists audit records. Backed by pkg/db; in tests this can be a fake.
type Writer interface {
    Record(ctx context.Context, r Record) error
}

// NewWriter constructs a DB-backed writer.
func NewWriter(q db.Querier) Writer {
    return &dbWriter{q: q}
}

type dbWriter struct{ q db.Querier }

func (w *dbWriter) Record(ctx context.Context, r Record) error {
    // TODO(v0.1.1): implement — marshal Detail to JSONB, call q.InsertCredentialEvent.
    return nil
}

// MustParseIP is a small helper for handlers that want to attach an IP from a
// string without erroring out the request when parsing fails.
func MustParseIP(s string) *netip.Addr {
    if s == "" {
        return nil
    }
    a, err := netip.ParseAddr(s)
    if err != nil {
        // Try splitting host:port.
        host, _, splitErr := net.SplitHostPort(s)
        if splitErr != nil {
            return nil
        }
        a, err = netip.ParseAddr(host)
        if err != nil {
            return nil
        }
    }
    return &a
}
```

- [ ] **Step 2: pkg/credential/password/password.go**

```go
package password

import (
    "context"
    "errors"

    "prohibitorum/pkg/db"
)

// ErrPasswordNotSet is returned when an account has no password credential.
var ErrPasswordNotSet = errors.New("password: not set")

// ErrPasswordIncorrect is returned by Verify when the provided password is wrong.
var ErrPasswordIncorrect = errors.New("password: incorrect")

// Verifier exposes the password verification entry point used by the login flow.
type Verifier interface {
    Verify(ctx context.Context, accountID int32, password string) error
}

// Setter exposes the password set/replace entry point used by /me and admin flows.
type Setter interface {
    Set(ctx context.Context, accountID int32, password string) error
}

// Store is the concrete implementation backed by pkg/db.
type Store struct {
    q db.Querier
    // params PasswordHashParams — injected from configx by NewStore.
}

// NewStore constructs a password store.
func NewStore(q db.Querier /* params configx.PasswordHashParams */) *Store {
    return &Store{q: q}
}

func (s *Store) Verify(ctx context.Context, accountID int32, password string) error {
    // TODO(v0.2): fetch hash via q.GetPasswordCredential, verify via argon2id
    // PHC-string verify, re-hash if params have been upgraded.
    return ErrPasswordNotSet
}

func (s *Store) Set(ctx context.Context, accountID int32, password string) error {
    // TODO(v0.2): hash via argon2id PHC, q.UpsertPasswordCredential.
    return nil
}
```

- [ ] **Step 3: pkg/credential/totp/totp.go**

```go
package totp

import (
    "context"
    "errors"

    "prohibitorum/pkg/db"
)

var (
    ErrTOTPNotSet     = errors.New("totp: not set")
    ErrTOTPUnconfirmed = errors.New("totp: enrollment not confirmed")
    ErrTOTPInvalidCode = errors.New("totp: invalid code")
    ErrTOTPReplay     = errors.New("totp: code already used (RFC 6238 §5.2)")
)

// Enrollment is the response of Begin — contains the secret + provisioning URI
// for QR code display. Shown to the user once; never logged.
type Enrollment struct {
    SecretBase32     string
    ProvisioningURI  string  // otpauth:// URL
    RecoveryCodes    []string // 10 codes, shown once
}

// Store is backed by pkg/db with AES-GCM at-rest encryption per the spec
// §"Cryptographic and behavioral policies".
type Store struct {
    q db.Querier
    // dek configx.DataEncryptionKeys — injected.
}

func NewStore(q db.Querier) *Store {
    return &Store{q: q}
}

func (s *Store) Begin(ctx context.Context, accountID int32, label string) (*Enrollment, error) {
    // TODO(v0.2): generate secret (RFC 6238 §3), AES-GCM encrypt with AAD
    // 'totp:'||account_id||':'||key_version, insert into totp_credential
    // (confirmed_at NULL), mint 10 recovery codes (argon2id-hash, insert).
    return nil, errors.New("totp.Begin: TODO(v0.2)")
}

func (s *Store) Verify(ctx context.Context, accountID int32, code string) error {
    // TODO(v0.2): GetTOTPCredential, decrypt with AAD, compute T = unix/period
    // with ±1 drift, reject if T <= last_step, otherwise UpdateTOTPLastStep
    // and ConfirmTOTPCredential if first verify.
    return ErrTOTPNotSet
}

func (s *Store) VerifyRecoveryCode(ctx context.Context, accountID int32, code string, sessionID string, ip string) error {
    // TODO(v0.2): argon2id-verify against each ListRecoveryCodesByAccount row;
    // first match → ConsumeRecoveryCode.
    return ErrTOTPInvalidCode
}
```

- [ ] **Step 4: pkg/federation/oidc/federation.go**

```go
package oidc

import (
    "context"
    "errors"

    "prohibitorum/pkg/db"
)

var (
    ErrUnknownIDP        = errors.New("federation: unknown IdP")
    ErrModeRejection     = errors.New("federation: provisioning mode rejected this sign-in")
    ErrIssuerMismatch    = errors.New("federation: discovery issuer doesn't match configured issuer_url")
    ErrStaleState        = errors.New("federation: state TTL expired")
)

// LoginRequest is what /auth/federation/{slug}/login produces — the redirect
// URL plus the state key written to KV.
type LoginRequest struct {
    AuthorizeURL string
    StateKey     string
}

// CallbackResult carries the outcome of a successful upstream code exchange.
type CallbackResult struct {
    AccountID     int32
    SessionID     string
    NewAccount    bool
    Linked        bool
}

// Federator orchestrates the RP flow.
type Federator struct {
    q db.Querier
    // kv kv.Store — injected
    // dek configx.DataEncryptionKeys — injected
}

func NewFederator(q db.Querier) *Federator {
    return &Federator{q: q}
}

func (f *Federator) BeginLogin(ctx context.Context, idpSlug, returnTo string) (*LoginRequest, error) {
    // TODO(v0.3): GetUpstreamIDPBySlug, fetch+cache discovery doc, snapshot
    // expected_iss into KV state blob, build authorize URL with PKCE.
    return nil, ErrUnknownIDP
}

func (f *Federator) HandleCallback(ctx context.Context, idpSlug, code, state string) (*CallbackResult, error) {
    // TODO(v0.3): pull KV state, verify expected_iss matches issuer from ID token,
    // exchange code at upstream token endpoint, validate ID token, apply mode policy
    // (auto_provision / invite_only / link_only), upsert account_identity, mint session.
    return nil, ErrUnknownIDP
}
```

- [ ] **Step 5: pkg/protocol/saml/saml.go**

```go
package saml

import (
    "context"
    "errors"
    "net/http"

    "prohibitorum/pkg/db"
)

var (
    ErrUnknownSP        = errors.New("saml: unknown service provider")
    ErrInvalidACS       = errors.New("saml: ACS URL does not match registered endpoints")
    ErrMissingSignature = errors.New("saml: SP signature required but absent")
)

// IdP is the SAML identity provider. Implementations should embed
// crewjam/saml's IdentityProvider once v0.5 wires it; for now this is a stub
// exposing the HTTP endpoints we'll need.
type IdP struct {
    q db.Querier
    // crewjamIdP *saml.IdentityProvider — added in v0.5
    // dek configx.DataEncryptionKeys
}

func NewIdP(q db.Querier) *IdP {
    return &IdP{q: q}
}

// Metadata serves the IdP metadata XML at GET /saml/metadata.
func (i *IdP) Metadata(w http.ResponseWriter, r *http.Request) {
    // TODO(v0.5): render <EntityDescriptor> from signing_key + configx.SAML config.
    http.Error(w, "saml.Metadata: TODO(v0.5)", http.StatusNotImplemented)
}

// SSO handles AuthnRequest (HTTP-Redirect via GET, HTTP-POST via POST).
func (i *IdP) SSO(w http.ResponseWriter, r *http.Request) {
    // TODO(v0.5): parse AuthnRequest, validate against saml_sp config,
    // require session (redirect to /login if absent), build signed Response,
    // POST back to ACS URL.
    http.Error(w, "saml.SSO: TODO(v0.5)", http.StatusNotImplemented)
}

// SLO handles Single Logout (HTTP-Redirect and HTTP-POST).
func (i *IdP) SLO(w http.ResponseWriter, r *http.Request) {
    // TODO(v0.5): parse LogoutRequest, propagate to other SPs via saml_session,
    // revoke our session.
    http.Error(w, "saml.SLO: TODO(v0.5)", http.StatusNotImplemented)
}

// SubjectID returns the stable pairwise NameID for (account, SP). Generates and
// persists on first call per spec §"SAML assertion construction".
func (i *IdP) SubjectID(ctx context.Context, accountID, spID int32, format string) (string, error) {
    // TODO(v0.5): GetSAMLSubjectID; if absent, generate 32-byte random base64url,
    // InsertSAMLSubjectID, return.
    return "", ErrUnknownSP
}
```

- [ ] **Step 6: pkg/authn/flow.go**

```go
package authn

import (
    "context"
    "errors"

    "prohibitorum/pkg/db"
)

var ErrNoUsableMethod = errors.New("authn: account has no usable sign-in method; admin recovery required")

// Method enumerates the authentication methods available to a given account.
type Method string

const (
    MethodWebAuthn       Method = "webauthn"
    MethodPasswordTOTP   Method = "password_totp"
    MethodFederationOIDC Method = "federation_oidc"
)

// AvailableMethods inspects an account's enrolled credentials and returns the
// methods that can be presented at the login screen, in preference order.
// Order: WebAuthn > password+TOTP > federation suggestion.
func AvailableMethods(ctx context.Context, q db.Querier, accountID int32) ([]Method, error) {
    // TODO(v0.2+): query webauthn_credential / password_credential / totp_credential /
    // account_identity rows and return the available methods.
    return nil, ErrNoUsableMethod
}

// DisableNonWebAuthnFallbacks transactionally deletes password_credential,
// totp_credential, and recovery_code rows for an account. Called after a
// successful WebAuthn enrollment when the user opts into "disable backup".
func DisableNonWebAuthnFallbacks(ctx context.Context, q db.Querier, accountID int32) error {
    // TODO(v0.2): tx { DeletePasswordCredential, DeleteTOTPCredential,
    // DeleteAllRecoveryCodesByAccount } + audit event.
    return nil
}
```

- [ ] **Step 7: pkg/configx/configx.go — extend with new substructs**

Open `pkg/configx/configx.go`. Add:

```go
type OIDCConfig struct {
    Issuer            string         // e.g. https://auth.example.com
    AccessTokenTTL    time.Duration  // default 10m
    RefreshTokenTTL   time.Duration  // default 30d
    CodeTTL           time.Duration  // default 60s
    JWKSCacheMaxAge   time.Duration  // hint in discovery doc
}

type FederationConfig struct {
    StateTTL          time.Duration  // default 10m
    DefaultScopes     []string       // default openid profile email
}

type TOTPConfig struct {
    DefaultPeriod    int    // 30
    DefaultDigits    int    // 6
    DefaultAlgorithm string // "SHA1"
    DriftSteps       int    // 1 (±1 period)
    RecoveryCodeCount int   // 10
}

type SAMLConfig struct {
    EntityID            string         // e.g. https://auth.example.com
    BaseURL             string
    DefaultNameIDFormat string         // "urn:oasis:names:tc:SAML:1.1:nameid-format:persistent"
    SessionLifetime     time.Duration  // default for SessionNotOnOrAfter; SPs override
    MetadataRotationGrace time.Duration  // include retired keys this fresh in /saml/metadata; default 7d
}

type PasswordHashParams struct {
    MemoryKiB    uint32 // 65536 = 64 MiB
    Iterations   uint32 // 3
    Parallelism  uint8  // 4
}
```

Extend the top-level `Config`:

```go
type Config struct {
    // ... existing fields ...
    WebAuthn             WebAuthnConfig             // added in Task 2
    OIDC                 OIDCConfig
    Federation           FederationConfig
    TOTP                 TOTPConfig
    SAML                 SAMLConfig
    PasswordHashParams   PasswordHashParams
    DataEncryptionKeys   map[int][]byte // version → 32-byte AES-256 key
}
```

In the config-loading function, set defaults for any unset field. Parse `DataEncryptionKeys` from env: every `PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>` env var becomes an entry in the map (key = `<n>`, value = base64-decoded 32 bytes). At least one version must be present at startup or the function returns an error.

- [ ] **Step 8: Wire audit writer into server.go**

In `pkg/server/server.go`, add an `audit.Writer` field to the `Server` struct and pass it through the constructor:

```go
type Server struct {
    // existing fields ...
    Audit audit.Writer
}

func New(/* existing args */, auditWriter audit.Writer) *Server {
    return &Server{ /* existing */, Audit: auditWriter}
}
```

In `cmd/prohibitorum/main.go`, construct it before `server.New`:

```go
auditWriter := audit.NewWriter(queries)
s := server.New(/* existing */, auditWriter)
```

This wires the import even though current handlers won't call `s.Audit.Record` until v0.2.

- [ ] **Step 9: Build and vet**

```bash
go build ./...
go vet ./...
go test ./...
```

- [ ] **Step 10: Commit**

```bash
git add -A
git commit -m "$(cat <<'EOF'
feat: stub packages + configx extensions for v0.2+

Lands empty implementations of pkg/credential/password, pkg/credential/totp,
pkg/federation/oidc, pkg/protocol/saml, pkg/authn, and pkg/audit so the
import graph is whole for the rescoped architecture. All exported entry
points carry TODO(v0.X) markers indicating which version delivers the
body.

configx grows OIDCConfig, FederationConfig, TOTPConfig, SAMLConfig,
PasswordHashParams, and a versioned DataEncryptionKeys set
(PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>).

pkg/audit.Writer is wired into server.New; handlers will start recording
events in v0.2.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: User-facing doc rewrites

**Goal:** Bring DESIGN.md / STATUS.md / AUDIT.md / INTEGRATION.md / README.md into alignment with the spec. The user-facing docs should describe Prohibitorum on its own terms — no picotera references, multi-protocol scope front and center.

**Files:**
- Rewrite: `DESIGN.md`
- Rewrite: `STATUS.md`
- Rewrite: `AUDIT.md`
- Rewrite: `INTEGRATION.md`
- Rewrite: `README.md`

**Acceptance Criteria:**
- [ ] `grep -ni picotera DESIGN.md STATUS.md AUDIT.md INTEGRATION.md README.md` returns no hits
- [ ] DESIGN.md describes Approach A three-layer architecture, all four upstream methods (WebAuthn / Password / TOTP / OIDC federation) and both downstream protocols (OIDC OP / SAML IdP)
- [ ] STATUS.md reflects the v0.1 = "rescope + decoupling" framing from spec §Roadmap and updates the pending list (smoke test moves to v0.1.1; v0.2 = password+TOTP)
- [ ] AUDIT.md has new sections for Password (NIST 800-63B-4), TOTP (RFC 6238), Upstream OIDC federation, SAML IdP — each cross-referencing the audit reports in `docs/superpowers/specs/2026-05-24-audit-*.md`
- [ ] INTEGRATION.md adds Pattern C for SAML SP integration with a GHES configuration example
- [ ] README.md one-liner reflects the multi-method scope

**Verify:** `! grep -rni picotera DESIGN.md STATUS.md AUDIT.md INTEGRATION.md README.md`

**Steps:**

- [ ] **Step 1: DESIGN.md**

Replace the existing file with content aligned to the spec. Structure:

```
# Prohibitorum — Design

(One-paragraph what-it-is, lifted from spec §Scope summary.)

## What this is (and isn't)

- Is: single-tenant identity provider for a small org. Owns the account
  directory, can federate identity from upstream OIDC providers.
- Is not: multi-tenant SaaS IdP; SAML SP; authorization policy engine.

## Architecture

(Three-layer split: identity store / authentication / protocol. Diagram
mirroring spec §Architecture target package layout. Include the ASCII
box-and-arrow diagram if helpful.)

## Authentication methods

- WebAuthn (primary, phishing-resistant)
- Password + TOTP (fallback for users without passkey-capable devices)
- Upstream OIDC federation (per-IdP provisioning modes: auto / invite / link)

## Downstream protocols

- OIDC OP (Authorization Code + PKCE, RFC 9068 access tokens, refresh-token
  rotation with reuse detection)
- SAML IdP (SP-initiated SSO, GHES-compatible profile)

## Authentication ceremony

(Per spec §Authentication orchestrator and HTTP surface.)

## Authorization model

- account.attributes jsonb is opaque to Prohibitorum; flows verbatim into
  ID-token attributes claim and SAML AttributeStatement.
- account.role ∈ {user, admin}. Admin gates server-side admin endpoints.

## Data layout

(Brief summary; link to spec for full schema.)

## Cryptography

- RS256 signing keys, unified across OIDC + SAML
- AES-256-GCM for at-rest encrypted secrets (TOTP, upstream client secrets),
  versioned DEKs for rotation
- argon2id PHC for password / recovery / client-secret hashes
- WebAuthn ResidentKey=Required, UV=Required at register / Preferred at login

## Threat model

(Per spec §Threat model deltas — list with concise per-threat mitigation.)

## Out of scope

(Per spec §Scope summary "Out of scope" list.)
```

- [ ] **Step 2: STATUS.md**

Replace with:

```
# Status — what's done, what's pending

## v0.1 (current commit) — rescope + decoupling

Done:
- Approach A three-layer package layout: pkg/{account, credential/*,
  federation/oidc, session, authn, protocol/{oidc,saml}, audit}.
- Picotera vocabulary stripped from schema (account.attributes jsonb,
  enrollment.template_attributes jsonb, errorx.Error, configurable
  RPDisplayName).
- Five migrations applied: 001 (account + session + webauthn + enrollment +
  credential_event + auth_throttle), 002 (signing_key unified + extended
  oidc_client + revoked_jti), 003 (password + totp + recovery_code), 004
  (upstream_idp + account_identity + session.upstream_idp_id), 005
  (saml_sp + acs + key + subject_id + session).
- Stub packages with TODO(v0.X) markers for v0.2+ work.
- Doc rewrites (DESIGN/AUDIT/INTEGRATION/README/STATUS).

## v0.1.1 — smoke test (next session)

- go mod tidy verified locked
- Migrations applied to real Postgres
- Existing WebAuthn ceremony exercised end-to-end (enroll-admin →
  /enrollments/{token}/register → /me)
- /.well-known/openid-configuration returns a coherent doc (even though
  /authorize / /token still 501)

## v0.2 — password + TOTP

(Punch list of password_credential and totp_credential implementation tasks.)

## v0.3 — upstream OIDC federation

(Per-IdP RP flow via zitadel/oidc/v3, three provisioning modes, /me link UX.)

## v0.4 — OIDC OP downstream

(signing-key generate subcommand, /oauth/authorize, /oauth/token,
/oauth/userinfo, /oauth/introspect, RP-initiated logout, refresh-token
rotation with reuse detection.)

## v0.5 — SAML IdP

(crewjam/saml integration, metadata, SP-initiated SSO, signed assertions,
attribute mapping, optional SLO.)

## v0.6 — Frontend

(Vue 3 dashboard, passkey ceremony SDK, method-selection login UX.)

## v0.7+ — Hardening

(KMS-backed signing keys, audit-log export to SIEM, rotation UX,
admin UI for clients/SPs/IdPs.)
```

- [ ] **Step 3: AUDIT.md**

Restructure to have one section per layer of the new architecture. Cross-reference the three audit reports for the spec-vs-design comparison; this file is the project-level checklist of "what we comply with right now":

```
# Audit — OAuth 2.1 / OIDC / WebAuthn / SAML / NIST best-practice checklist

Compliance of the current codebase against authoritative standards. Items
marked **✅** are implemented; **⚠️ deferred** are intentional v0.x
omissions with a clear future path; **❌ gap** are unfinished and need
work before v1.0.

For the full spec-vs-design audit that drove the v0.1 schema decisions,
read `docs/superpowers/specs/2026-05-24-audit-{oidc,credentials,saml}.md`.

## Authentication

### WebAuthn (W3C Level 3)

(Table of items — port from spec §Threat model + audit-credentials.md
findings, with ✅ / ⚠️ / ❌ per item.)

### Password (NIST SP 800-63B-4)

(Table covering: min length, no composition rules, breach-corpus
recommendation, no periodic rotation, argon2id PHC, persistent throttle,
…)

### TOTP (RFC 6238 / RFC 4226)

(Table covering: secret entropy, period/digits, drift tolerance,
last_step replay, throttle, AES-GCM at-rest with versioned DEK + AAD.)

### Recovery codes

(Table covering: PHC hash, single-use, redemption-context capture.)

### Upstream OIDC federation (OIDC Core / RFC 9700)

(Table covering: issuer validation, mix-up resistance, expected_iss
snapshot, per-IdP modes, (iss, sub) keying.)

## OAuth 2.1 / OIDC OP

(Table — port from v0.1 AUDIT.md, augment with audit-oidc.md findings:
post_logout_redirect_uris, allowed_code_challenge_methods, jti, code-
consumed-not-deleted, etc.)

## SAML IdP

(Table — port from audit-saml.md: ACS multi-endpoint, signed Response+
Assertion, stable NameID via saml_subject_id, attribute_map array shape,
…)

## Cryptography

(Per spec §"Cryptographic and behavioral policies".)

## Operational

(Migrations, audit log, rate limit, session manager, admin UI.)

## Threats this codebase does NOT protect against

(Per spec §Threat model deltas "what v1 does NOT protect against".)
```

- [ ] **Step 4: INTEGRATION.md**

Restructure for three patterns:

```
# Integrating with Prohibitorum

| Pattern | When | Trust assumption |
|---|---|---|
| A. OIDC Authorization Code + PKCE | Any RP with a back-end | Standard. Strongest. Start here. |
| B. Cookie + Introspection | First-party RP co-located behind same reverse proxy | Acceptable for co-located first-party. |
| C. SAML 2.0 SP | Legacy SaaS / on-prem apps that only speak SAML (GHES, etc.) | Use when SAML is the only option. |

## Pattern A — OIDC Code + PKCE

(Existing v0.1 content, lightly updated.)

## Pattern B — Cookie + Introspection

(Existing v0.1 content.)

## Pattern C — SAML SP

### One-time setup

(SQL inserts for saml_sp + saml_sp_acs + saml_sp_key.)

### GHES-specific configuration

(IdP URL, entity ID, SSO URL, certificate fingerprint, attribute mapping
for emails/public_keys/gpg_keys/administrator.)

### Discovery

`GET /saml/metadata` returns the IdP metadata XML; import into the SP.
```

Use the GHES call-outs from `docs/superpowers/specs/2026-05-24-audit-saml.md` "GHES-specific call-outs" section verbatim where useful.

- [ ] **Step 5: README.md**

Update the one-liner and quickstart:

```
# Prohibitorum

> Index Librorum Prohibitorum. Who's allowed in and what they can do.

A homegrown identity & authorization service for small orgs.

- **Upstream auth methods:** WebAuthn (preferred), Password+TOTP (fallback),
  OIDC federation (Google/Entra/Keycloak/…).
- **Downstream protocols:** OIDC OP (for modern apps), SAML IdP (for GHES /
  legacy SaaS).
- **Single-tenant. First-party. No email channel; admin-issued enrollment
  is the only recovery path.**

(Quickstart commands, link to DESIGN/INTEGRATION/STATUS/AUDIT.)
```

- [ ] **Step 6: Verify no stale picotera mentions in user-facing docs**

```bash
grep -ni picotera DESIGN.md STATUS.md AUDIT.md INTEGRATION.md README.md
# Expected: no output
```

(The spec and audit reports in `docs/superpowers/specs/` intentionally retain picotera mentions as historical context.)

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "$(cat <<'EOF'
docs: rewrite DESIGN/STATUS/AUDIT/INTEGRATION/README for the rescoped service

Aligns user-facing docs with the multi-protocol rescope:

- DESIGN.md: three-layer architecture; four upstream methods; two
  downstream protocols.
- STATUS.md: v0.1 = rescope+decoupling done; v0.1.1 = smoke test;
  v0.2-v0.7 roadmap.
- AUDIT.md: one section per layer (WebAuthn / Password / TOTP /
  recovery codes / upstream OIDC / OIDC OP / SAML IdP /
  cryptography / operational), with ✅ / ⚠️ deferred / ❌ gap labels
  per item. Cross-references the three spec-vs-design audit reports.
- INTEGRATION.md: adds Pattern C (SAML SP) with GHES configuration
  example.
- README.md: multi-method scope front and center.

No picotera vocabulary remains in user-facing docs (the spec retains
it as the audit trail of what was removed).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Final verification

After Task 8 commits, run the full skeleton-commit acceptance check:

```bash
# 1. Build clean.
go build ./...

# 2. Tests pass.
go test ./...

# 3. Migrations apply against fresh DB.
goose -dir db/migrations postgres "$PROHIBITORUM_DATABASE_URL" reset
mise run db:up
mise run db:status
# Expected: 001..005 all Applied

# 4. No picotera vocabulary anywhere except the spec/audit files.
grep -rni picotera pkg/ cmd/ db/ DESIGN.md STATUS.md AUDIT.md INTEGRATION.md README.md
# Expected: no output

# 5. Spec-cited columns exist.
for col in attributes session credential_event auth_throttle revoked_jti \
           signing_key.use signing_key.not_before oidc_client.post_logout_redirect_uris \
           webauthn_credential.user_handle webauthn_credential.cose_alg \
           webauthn_credential.uv_initialized totp_credential.last_step \
           totp_credential.key_version account_identity.upstream_iss \
           saml_sp_acs saml_subject_id saml_session; do
  echo "Checking $col..."
done
# (manual spot-check via psql \d <table>)

# 6. Git log shows ~8-9 commits matching the plan.
git log --oneline -15
```

If everything passes, the skeleton commit is complete. Next session picks up at v0.1.1 (smoke test) per STATUS.md.

---

## Notes for the executor

- **TDD discipline:** This plan is mostly refactor + schema, where the existing test suite is the regression net. The discipline is "tests pass after each task" — not "write tests for each step." If you find a behavior you're touching that has no test (e.g. the account-attributes projection), add a test for it as part of the task.
- **Don't add features:** This is the skeleton commit. Stub bodies stay stubs. Resist the urge to implement password/TOTP/federation/SAML here — those are v0.2+ plans.
- **Spec is authoritative:** When the plan says "per spec §X", read the spec. The plan abbreviates SQL and code that's already complete in the spec.
- **Commit at each task boundary:** Each task ends with a commit. Don't bundle two tasks into one commit; don't split one task across two commits unless a task explicitly says so.
- **Database state:** Tasks 3–6 require a Postgres to verify migrations. Use a disposable Docker container or a project-local cluster.
