# Admin Management API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose the IdP-configuration surface (OIDC clients, SAML SPs, upstream IdPs, signing keys, audit log, admin credential listing) over HTTP, sudo-gated, so the dashboard's five "Planned" admin pages can be built and the backend management surface is complete.

**Architecture:** New admin HTTP handlers reuse the existing domain functions and sqlc queries that the cobra CLI already drives (reuse, don't rebuild). Reads are typed Huma operations (`registerOp`, admin-gated); every trust/identity/credential/destructive mutation is a raw handler registered through a new `registerSudoOpHTTP` wrapper that centralizes admin auth + fresh-sudo + baseline protections. Signing keys move to an explicit DB lifecycle (`pending→active→decommissioning→retired`) via an expand→cutover→contract migration.

**Tech Stack:** Go, chi router, huma v2, sqlc + pgx v5, Postgres, goose migrations, cobra CLI. Tests: Go `testing` + the existing `cmd/smoke` virtual-authenticator harness.

**Conventions (from the codebase):**
- Commits go directly to `master` (no remote, no worktree).
- After editing `db/queries/*.sql` or `db/migrations/*.sql`: `mise exec sqlc -- sqlc generate` then `go build ./...`.
- Trust `go build ./... && go vet ./...` exit 0 over gopls diagnostics.
- Raw handler pattern: `sess := authn.SessionFromContext(r.Context())`; decode with `json.NewDecoder(r.Body).Decode(&body)`; errors via `writeAuthErr(w, authn.ErrX())`; success via `w.WriteHeader(204)` or JSON encode.
- Typed handler pattern: return `authErrToHuma(authn.ErrX())` for domain errors, `fmt.Errorf("handlerName: ...: %w", err)` for internal.
- Audit: `s.Audit.Record(ctx, audit.Record{AccountID:&id, Factor:…, Event:…, Detail:map[string]any{…}})`.
- Do NOT `pkill -f 'prohibitorum'` bare (kills dev PG). Use `pkill -f 'go-build.*/prohibitorum'` + `pkill -f 'cmd/prohibitorum'`.

**Spec:** `docs/superpowers/specs/2026-06-06-admin-management-api-design.md`

---

### Task 0: Sudo route-policy wrapper + audit constants

**Goal:** A `registerSudoOpHTTP` wrapper that makes "admin + fresh sudo + baseline protections" a route policy (impossible to forget), plus the new audit factor/event constants every later task uses.

**Files:**
- Modify: `pkg/server/operations.go` (add `withFreshSudo` + `registerSudoOpHTTP`)
- Modify: `pkg/audit/audit.go` (add `FactorUpstreamIDP`, `FactorSigningKey`, `EventUpdate`, `EventRotate`)
- Test: `pkg/server/operations_test.go` (new)

**Acceptance Criteria:**
- [ ] `registerSudoOpHTTP(router, method, path, admin, h)` registers a route that returns `sudo_required` (401) when the session lacks a fresh sudo grant, and runs `h` when it has one (consuming the grant).
- [ ] The wrapper applies `http.MaxBytesReader` (64 KiB) to the request body and rejects a non-`application/json` content-type on bodied methods with `bad_request` (400).
- [ ] `audit.FactorUpstreamIDP`, `audit.FactorSigningKey`, `audit.EventUpdate`, `audit.EventRotate` exist.

**Verify:** `go test ./pkg/server/ -run TestRegisterSudoOpHTTP -v` → PASS; `go build ./...` → exit 0

**Steps:**

- [ ] **Step 1: Add audit constants**

In `pkg/audit/audit.go`, add to the Factor block (after `FactorSAMLSP`):
```go
	FactorUpstreamIDP Factor = "upstream_idp"
	FactorSigningKey  Factor = "signing_key"
```
And to the event const block (after `EventRevoke`):
```go
	EventUpdate = "update"
	EventRotate = "rotate"
```

- [ ] **Step 2: Write the failing test**

Create `pkg/server/operations_test.go`:
```go
package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
)

// sessionInto returns a request whose context carries sess (mirrors LoadSession).
func reqWithSession(method, path, body string, sess *authn.Session) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	return r.WithContext(authn.ContextWithSession(r.Context(), sess))
}

func TestRegisterSudoOpHTTP_RejectsWithoutFreshSudo(t *testing.T) {
	s := &Server{} // sessionStore unused on the reject path
	router := chi.NewRouter()
	called := false
	registerSudoOpHTTP(router, "POST", "/x", contract.AuthRequirement{Kind: contract.AuthAdmin},
		func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) })

	admin := &authn.Session{
		Account: &authn.Account{ID: 1, Role: "admin"},
		Data:    &authn.SessionData{SudoUntil: time.Time{}}, // no fresh sudo
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, reqWithSession("POST", "/x", `{}`, admin))

	if called {
		t.Fatal("handler ran despite missing fresh sudo")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "sudo_required") {
		t.Fatalf("body = %q, want sudo_required", rr.Body.String())
	}
}

func TestRegisterSudoOpHTTP_RejectsNonJSON(t *testing.T) {
	s := &Server{}
	_ = s
	router := chi.NewRouter()
	registerSudoOpHTTP(router, "POST", "/x", contract.AuthRequirement{Kind: contract.AuthAdmin},
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })

	admin := &authn.Session{
		Account: &authn.Account{ID: 1, Role: "admin"},
		Data:    &authn.SessionData{SudoUntil: time.Now().Add(time.Minute)},
	}
	r := reqWithSession("POST", "/x", `not-json`, admin)
	r.Header.Set("Content-Type", "text/plain")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, r)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for non-json content-type", rr.Code)
	}
}
```

> Note: confirm the exact constructor/type names (`authn.ContextWithSession`, `authn.Account`, `authn.SessionData`, `SudoUntil`) against `pkg/authn/middleware.go` while implementing; adjust the test to the real names. `HasFreshSudo()` is the predicate `requireFreshSudo` uses.

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./pkg/server/ -run TestRegisterSudoOpHTTP -v`
Expected: FAIL — `registerSudoOpHTTP` undefined.

- [ ] **Step 4: Implement the wrapper**

In `pkg/server/operations.go`, add (after `registerOpHTTP`):
```go
// maxAdminBody bounds admin mutation request bodies.
const maxAdminBody = 64 << 10 // 64 KiB

// withFreshSudo wraps a raw handler so the fresh-sudo gate runs as route
// policy, not as the handler's first line. It also applies the baseline
// body-size limit and JSON content-type check for mutation bodies. This is
// the single chokepoint for 🔐 admin mutations — see the design's §4.
func (s *Server) withFreshSudo(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess := authn.SessionFromContext(r.Context())
		if s.requireFreshSudo(r.Context(), w, sess) {
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxAdminBody)
		}
		if r.ContentLength != 0 && r.Method != http.MethodGet {
			ct := r.Header.Get("Content-Type")
			if ct != "" && !strings.HasPrefix(ct, "application/json") {
				writeAuthErr(w, authn.ErrBadRequest())
				return
			}
		}
		h(w, r)
	}
}

// registerSudoOpHTTP = registerOpHTTP (admin auth) + withFreshSudo (sudo +
// baseline protections). Every admin mutation route MUST use this, never the
// bare registerOpHTTP, so the sudo policy cannot drift per-handler.
func (s *Server) registerSudoOpHTTP(router chiRouter, method, path string, req contract.AuthRequirement, h http.HandlerFunc) {
	registerOpHTTP(router, method, path, req, s.withFreshSudo(h))
}
```
Add `"strings"` to the imports.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./pkg/server/ -run TestRegisterSudoOpHTTP -v`
Expected: PASS. Then `go build ./...` → exit 0.

- [ ] **Step 6: Commit**

```bash
git add pkg/server/operations.go pkg/server/operations_test.go pkg/audit/audit.go
git commit -m "feat(admin): registerSudoOpHTTP wrapper + admin audit constants

Centralizes admin-mutation route policy (admin auth + fresh sudo + body-size
limit + JSON content-type) so a 🔐 route cannot be created with a missing
sudo check. Adds upstream_idp/signing_key factors and update/rotate events.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 1: Migration 008 — signing-key lifecycle columns + backfill

**Goal:** Add the explicit lifecycle state to `signing_key` (expand phase, non-destructive), backfill from legacy columns, and enforce one-active-per-`use` — without changing any read/write behavior yet.

**Files:**
- Create: `db/migrations/008_signing_key_lifecycle.sql`
- Test: `pkg/db/signing_key_backfill_test.go` (new)

**Acceptance Criteria:**
- [ ] Migration applies cleanly on a fresh DB and on a DB with existing `signing_key` rows.
- [ ] `status` ∈ {pending,active,decommissioning,retired}; `activated_at`/`decommissioned_at`/`retire_after` nullable timestamptz.
- [ ] Backfill: legacy `active=true,retired_at NULL → 'active'`; `retired_at NOT NULL → 'retired'`; else `'pending'`.
- [ ] Partial unique index `one_active_signing_key` on `(use) WHERE status='active'`.
- [ ] Legacy `active` + `retired_at` columns are retained.

**Verify:** `mise dev-server` boots and auto-migrates without error; `go test ./pkg/db/ -run TestSigningKeyBackfill -v` → PASS

**Steps:**

- [ ] **Step 1: Write the migration**

Create `db/migrations/008_signing_key_lifecycle.sql` (match goose pragma style of `007_oidc_consent.sql`):
```sql
-- +goose Up
ALTER TABLE signing_key
  ADD COLUMN status TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending','active','decommissioning','retired')),
  ADD COLUMN activated_at      TIMESTAMPTZ NULL,
  ADD COLUMN decommissioned_at TIMESTAMPTZ NULL,
  ADD COLUMN retire_after      TIMESTAMPTZ NULL;

-- Defensive: if somehow >1 active row per `use` exists, keep only the newest
-- as active so the partial unique index below can build.
UPDATE signing_key sk
SET active = false
WHERE active = true
  AND id <> (
    SELECT id FROM signing_key s2
    WHERE s2.use = sk.use AND s2.active = true
    ORDER BY created_at DESC LIMIT 1
  );

-- Backfill explicit lifecycle from legacy columns.
UPDATE signing_key SET
  status            = CASE
                        WHEN retired_at IS NOT NULL THEN 'retired'
                        WHEN active = true           THEN 'active'
                        ELSE 'pending'
                      END,
  activated_at      = CASE WHEN active = true AND retired_at IS NULL
                           THEN COALESCE(not_before, created_at) END,
  decommissioned_at = retired_at,
  retire_after      = retired_at;

CREATE UNIQUE INDEX one_active_signing_key
  ON signing_key (use) WHERE status = 'active';

-- +goose Down
DROP INDEX IF EXISTS one_active_signing_key;
ALTER TABLE signing_key
  DROP COLUMN status,
  DROP COLUMN activated_at,
  DROP COLUMN decommissioned_at,
  DROP COLUMN retire_after;
```

- [ ] **Step 2: Write the backfill test**

Create `pkg/db/signing_key_backfill_test.go`. Follow the existing db-test harness (look for how other `pkg/db/*_test.go` or `pkg/server` integration tests obtain a `*pgxpool.Pool` against the dev/test DB; reuse that helper). The test inserts three legacy rows (active+unretired, retired, neither) **before** asserting their backfilled `status`, OR — if migrations always run before inserts — inserts via the post-migration schema and asserts the CHECK + index. Concretely:
```go
//go:build integration

package db_test

// TestSigningKeyBackfill asserts the 008 backfill mapping. Requires a DB whose
// migrations are applied through 008. Insert legacy-shaped rows via raw SQL
// (status defaults to 'pending'), then re-run the 008 backfill UPDATE and
// assert the resulting status/timestamps.
func TestSigningKeyBackfill(t *testing.T) {
	// 1. pool := testPool(t)  (reuse the repo's existing test-pool helper)
	// 2. insert: (active=true, retired_at=NULL), (active=false, retired_at=now()),
	//    (active=false, retired_at=NULL) — each with a distinct `use` value or a
	//    fresh table fixture to avoid the one-active index conflict.
	// 3. run the backfill UPDATE from 008 (copy the statement).
	// 4. assert: row1.status='active' & activated_at not null;
	//            row2.status='retired' & decommissioned_at=retire_after=retired_at;
	//            row3.status='pending'.
}
```
> Use the repo's existing integration-test convention (build tag + test DB URL). If no such convention exists, assert the backfill logic with a SQL-only check run by the smoke instead, and note it here.

- [ ] **Step 3: Apply + verify**

Run: `mise dev-server` (auto-migrates) — confirm no migration error in the log; then `go test ./pkg/db/ -run TestSigningKeyBackfill -tags integration -v`.
Expected: migration applies; test PASS.

- [ ] **Step 4: Commit**

```bash
git add db/migrations/008_signing_key_lifecycle.sql pkg/db/signing_key_backfill_test.go
git commit -m "feat(db): migration 008 — signing_key explicit lifecycle (expand)

Adds status + activated_at/decommissioned_at/retire_after, backfills from
legacy active/retired_at, enforces one active key per use via partial unique
index. Legacy columns retained (dropped later in 009 after cutover).

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Signing-key lifecycle queries + domain cutover + reconcile loop

**Goal:** Cut over key selection/publication to `status`, add the lifecycle sqlc queries and domain transition functions (generate→pending, activate, retire→decommissioning, reconcile), and run reconcile in `Serve()`. This is the riskiest change — its own task, guarded by the existing OIDC/SAML smoke steps.

**Files:**
- Modify: `db/queries/oidc.sql` (signing-key queries)
- Modify: `pkg/protocol/oidc/keygen.go` (pending insert) and `pkg/protocol/oidc/keys.go` (publish/active selection by status)
- Modify: `pkg/protocol/saml/keys_saml.go` (publish selection by status, if it reads `active`/`retired_at` directly)
- Create: `pkg/protocol/oidc/keylifecycle.go` (Generate/Activate/Retire/Reconcile domain functions)
- Modify: `pkg/server/server.go` (reconcile loop in `Serve()`, alongside `PruneExpiredRevokedJTI`)
- Test: `pkg/protocol/oidc/keylifecycle_test.go` (new)

**Acceptance Criteria:**
- [ ] JWKS + SAML metadata publish keys with `status IN ('pending','active','decommissioning')`; `retired` excluded.
- [ ] Signing selects the single `status='active'` key.
- [ ] `Activate(kid)`: demotes prior active → decommissioning (sets `decommissioned_at`, `retire_after = now()+grace`), then promotes target pending → active — in one tx, demote first.
- [ ] `Retire(kid)`: target `active` → error `ErrActiveKeyNoReplacement` (handler maps to 409); pending/decommissioning → decommissioning.
- [ ] `Reconcile`: decommissioning with `now() >= retire_after` → retired; idempotent; does not touch active/pending.
- [ ] Two concurrent `Activate` calls cannot leave two active keys (one fails the unique index).
- [ ] Full `go test ./...` + existing smoke steps (OIDC/SAML token verify) stay green.

**Verify:** `go test ./pkg/protocol/oidc/ -run TestKeyLifecycle -v` → PASS; full smoke green (`SMOKE_EXIT=0`)

**Steps:**

- [ ] **Step 1: Replace/add signing-key queries**

In `db/queries/oidc.sql`, change the selection queries to status and add lifecycle queries. The legacy `active` column is **dual-written** during expand so `GetActiveSigningKey` works whichever predicate is used.
```sql
-- name: GetActiveSigningKey :one
SELECT * FROM signing_key WHERE use = 'sig' AND status = 'active';

-- name: ListPublishableSigningKeys :many
SELECT * FROM signing_key
WHERE use = 'sig' AND status IN ('pending','active','decommissioning')
ORDER BY created_at DESC;

-- name: ListAllSigningKeys :many
SELECT * FROM signing_key WHERE use = 'sig' ORDER BY created_at DESC;

-- name: GetSigningKeyByKID :one
SELECT * FROM signing_key WHERE kid = $1;

-- name: InsertPendingSigningKey :one
INSERT INTO signing_key (kid, algorithm, use, public_jwk, x509_cert_pem, private_pem, active, status, not_before)
VALUES ($1,$2,'sig',$3,$4,$5,false,'pending', now())
RETURNING *;

-- name: DemoteActiveSigningKey :exec
UPDATE signing_key
SET status='decommissioning', active=false, decommissioned_at=now(), retire_after=$1
WHERE use='sig' AND status='active';

-- name: PromoteSigningKey :one
UPDATE signing_key
SET status='active', active=true, activated_at=now()
WHERE kid=$1 AND status='pending'
RETURNING *;

-- name: RetireSigningKey :one
UPDATE signing_key
SET status='decommissioning', active=false,
    decommissioned_at=COALESCE(decommissioned_at, now()), retire_after=$2
WHERE kid=$1 AND status IN ('pending','decommissioning')
RETURNING *;

-- name: ReconcileRetiredSigningKeys :execrows
UPDATE signing_key SET status='retired'
WHERE use='sig' AND status='decommissioning'
  AND retire_after IS NOT NULL AND now() >= retire_after;
```
> The old `ListActiveSigningKeys`, `DeactivateSigningKeys`, and the old `RetireSigningKey` (`:exec`) are replaced. Update every caller. Run `mise exec sqlc -- sqlc generate`.

- [ ] **Step 2: Cut over keys.go selection**

In `pkg/protocol/oidc/keys.go`: the `signingKeyQueries` interface method `ListActiveSigningKeys` → `ListPublishableSigningKeys`; the cache stores `status` per key; `signingKey(ctx)` selects the entry with `status == "active"` (not `r.Active`); `jwks(ctx)` publishes all cached (publishable) keys. Mirror any `status`/`retired_at` reads in `pkg/protocol/saml/keys_saml.go`. `go build ./...` will flag every caller to update.

- [ ] **Step 3: Domain transition functions**

Create `pkg/protocol/oidc/keylifecycle.go`:
```go
package oidc

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"prohibitorum/pkg/db"
)

// ErrActiveKeyNoReplacement is returned by RetireSigningKey when the target is
// the active signing key — you must Activate a replacement (which demotes the
// current active to decommissioning) before retiring.
var ErrActiveKeyNoReplacement = errors.New("cannot retire the active signing key without a replacement")

// txBeginner is satisfied by *pgxpool.Pool.
type txBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// GenerateSigningKey creates a new RSA key as a PENDING key (published in
// JWKS/metadata for pre-fetch, not yet used for signing). Reuses GenerateSigningKey
// material from keygen.go.
func InsertPendingKey(ctx context.Context, q *db.Queries) (db.SigningKey, error) {
	params, err := GenerateSigningKey() // keygen.go — returns key material
	if err != nil {
		return db.SigningKey{}, err
	}
	return q.InsertPendingSigningKey(ctx, db.InsertPendingSigningKeyParams{
		Kid: params.Kid, Algorithm: params.Algorithm, PublicJwk: params.PublicJwk,
		X509CertPem: params.X509CertPem, PrivatePem: params.PrivatePem,
	})
}

// ActivateSigningKey promotes a pending key to active and demotes the prior
// active key to decommissioning, in one tx. Demote precedes promote so the
// partial unique index never sees two actives.
func ActivateSigningKey(ctx context.Context, pool txBeginner, q *db.Queries, kid string, grace time.Duration) (db.SigningKey, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return db.SigningKey{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qt := q.WithTx(tx)
	if err := qt.DemoteActiveSigningKey(ctx, pgTimestamp(time.Now().Add(grace))); err != nil {
		return db.SigningKey{}, err
	}
	key, err := qt.PromoteSigningKey(ctx, kid)
	if err != nil {
		return db.SigningKey{}, err // pgx.ErrNoRows ⇒ not a pending key
	}
	if err := tx.Commit(ctx); err != nil {
		return db.SigningKey{}, err
	}
	return key, nil
}

// RetireSigningKey moves a key to decommissioning (stays in JWKS until
// retire_after). Refuses to retire the active key.
func RetireSigningKey(ctx context.Context, q *db.Queries, kid string, grace time.Duration) (db.SigningKey, error) {
	cur, err := q.GetSigningKeyByKID(ctx, kid)
	if err != nil {
		return db.SigningKey{}, err
	}
	if cur.Status == "active" {
		return db.SigningKey{}, ErrActiveKeyNoReplacement
	}
	return q.RetireSigningKey(ctx, db.RetireSigningKeyParams{Kid: kid, RetireAfter: pgTimestamp(time.Now().Add(grace))})
}
```
> `pgTimestamp` converts `time.Time` → the `pgtype.Timestamptz` the generated params expect; copy the conversion helper the codebase already uses (grep for `pgtype.Timestamptz{`). Confirm `GenerateSigningKey()`'s return field names against `keygen.go` and adjust. `grace` is the configured signing-key rotation grace (grep `configx` for the rotation-grace field; SAML metadata already uses a 7d grace default — reuse that config value).

- [ ] **Step 4: Reconcile loop in Serve()**

In `pkg/server/server.go`, next to the `PruneExpiredRevokedJTI` goroutine (~line 219), add a ticker loop that calls `s.queries.ReconcileRetiredSigningKeys(ctx)`, logs the rows-affected count, and (best-effort) audits each transitioned kid. Idempotent; safe on every tick.

- [ ] **Step 5: Write the lifecycle tests**

Create `pkg/protocol/oidc/keylifecycle_test.go` with integration tests (against the test pool): `TestKeyLifecycle_ActivateDemotesPrior`, `TestKeyLifecycle_RetireActiveReturnsError`, `TestKeyLifecycle_ReconcileTransitions`, `TestKeyLifecycle_ConcurrentActivate` (start two `ActivateSigningKey` goroutines on two pending keys; assert exactly one ends active and the other errors on the unique index), `TestPublishSetSpansPendingActiveDecommissioning`. Reuse the repo's test-pool helper.

- [ ] **Step 6: Run tests + smoke**

Run: `go test ./pkg/protocol/... -v` then the full smoke (`setsid bash /tmp/run_v06.sh`; poll `/tmp/v06.result` for `SMOKE_EXIT=0`).
Expected: PASS; smoke green (the existing OIDC/SAML token-verify steps confirm the active-key cutover didn't break signing).

- [ ] **Step 7: Commit**

```bash
git add db/queries/oidc.sql pkg/db pkg/protocol/oidc pkg/protocol/saml/keys_saml.go pkg/server/server.go
git commit -m "feat(keys): cut over signing keys to explicit lifecycle + reconcile loop

status-based selection (active signs; pending+active+decommissioning publish);
Activate demotes prior active→decommissioning; Retire→decommissioning (409 on
active); background reconcile decommissioning→retired past retire_after.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Signing-key admin endpoints

**Goal:** `GET /signing-keys` (🔓) + generate/activate/retire (🔐), wired through `registerSudoOpHTTP`, with audit and the 409 mapping.

**Files:**
- Create: `pkg/server/handle_admin_signing_keys.go`
- Modify: `pkg/contract/auth.go` (add `OperationListSigningKeys` + `SigningKeyView`)
- Modify: `pkg/server/server.go` (route registration)
- Modify: `pkg/authn/errors.go` (add `ErrActiveKeyNoReplacement` 409 mapping, if a 409 AuthError doesn't exist)
- Test: `pkg/server/handle_admin_signing_keys_test.go`

**Acceptance Criteria:**
- [ ] `GET /signing-keys` returns `kid, algorithm, use, status, notBefore, activatedAt, decommissionedAt, retireAfter` and the public JWK — never `private_pem`.
- [ ] `POST /signing-keys/generate` (🔐) creates a pending key; `POST /signing-keys/{kid}/activate` (🔐) activates; `POST /signing-keys/{kid}/retire` (🔐) → decommissioning, 409 on the active key.
- [ ] Each mutation writes an audit row (`factor=signing_key`, event register/update/revoke, kid + status in detail; no private material).

**Verify:** `go test ./pkg/server/ -run TestAdminSigningKeys -v` → PASS

**Steps:**

- [ ] **Step 1: Contract view + read op**

In `pkg/contract/auth.go` add:
```go
type SigningKeyView struct {
	Kid              string     `json:"kid"`
	Algorithm        string     `json:"algorithm"`
	Use              string     `json:"use"`
	Status           string     `json:"status"`
	PublicJWK        map[string]any `json:"publicJwk"`
	NotBefore        *time.Time `json:"notBefore,omitempty"`
	ActivatedAt      *time.Time `json:"activatedAt,omitempty"`
	DecommissionedAt *time.Time `json:"decommissionedAt,omitempty"`
	RetireAfter      *time.Time `json:"retireAfter,omitempty"`
}

var OperationListSigningKeys = huma.Operation{
	OperationID: "listSigningKeys",
	Method:      http.MethodGet,
	Path:        "/signing-keys",
	Summary:     "List signing keys with lifecycle status (admin only). Private material is never returned.",
}
```

- [ ] **Step 2: Write the handler file**

Create `pkg/server/handle_admin_signing_keys.go` with:
- `handleListSigningKeys(ctx, *struct{}) (*listSigningKeysOut, error)` — calls `ListAllSigningKeys`, projects to `SigningKeyView` (unmarshal `public_jwk` []byte → map; never touch `private_pem`).
- `handleGenerateSigningKeyHTTP(w, r)` — `oidc.InsertPendingKey`; audit `EventRegister`; respond 201 + the new `SigningKeyView`.
- `handleActivateSigningKeyHTTP(w, r)` — `kid := chi.URLParam(r, "kid")`; `oidc.ActivateSigningKey(ctx, s.dbPool, s.queries, kid, grace)`; map `pgx.ErrNoRows` → `authn.ErrCredentialNotFound()` (no such pending key); audit `EventUpdate` (detail `{action:"activate",kid}`); respond 200 + view.
- `handleRetireSigningKeyHTTP(w, r)` — `oidc.RetireSigningKey(...)`; map `oidc.ErrActiveKeyNoReplacement` → a 409 AuthError; audit `EventRevoke`; respond 200 + view.

Use `writeJSON(w, status, body)` if such a helper exists (grep `func writeJSON`); else `w.Header().Set("Content-Type","application/json"); w.WriteHeader(status); json.NewEncoder(w).Encode(body)`.

- [ ] **Step 3: Register routes**

In `pkg/server/server.go`, after the existing admin block:
```go
registerOp(mgmt, contract.OperationListSigningKeys, s.handleListSigningKeys, admin)
s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/signing-keys/generate", admin, s.handleGenerateSigningKeyHTTP)
s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/signing-keys/{kid}/activate", admin, s.handleActivateSigningKeyHTTP)
s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/signing-keys/{kid}/retire", admin, s.handleRetireSigningKeyHTTP)
```
(`admin` is the existing `contract.AuthRequirement{Kind:contract.AuthAdmin}` value already used in this block.)

- [ ] **Step 4: 409 AuthError**

In `pkg/authn/errors.go`, if no 409 constructor exists, add:
```go
func ErrActiveKeyNoReplacement() *AuthError {
	return newErr(http.StatusConflict, "active_key_no_replacement", "Activate a replacement key before retiring the active signing key.")
}
```

- [ ] **Step 5: Tests**

Create `pkg/server/handle_admin_signing_keys_test.go`: assert the list view omits private material; assert generate→activate→list reflects the status transitions; assert retire-active returns 409; assert each mutation route rejects a no-sudo session with `sudo_required` (use the Task 0 session helper). Mock or use the test pool per the repo convention.

- [ ] **Step 6: Run + commit**

Run: `go test ./pkg/server/ -run TestAdminSigningKeys -v` → PASS; `go build ./...`.
```bash
git add pkg/server/handle_admin_signing_keys.go pkg/server/handle_admin_signing_keys_test.go pkg/contract/auth.go pkg/server/server.go pkg/authn/errors.go
git commit -m "feat(admin): signing-key endpoints (list 🔓; generate/activate/retire 🔐)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: OIDC client admin endpoints

**Goal:** Full OIDC-client management over HTTP — list/detail (🔓), create/update/rotate-secret/delete (🔐), reusing `oidc.BuildClientParams` and adding the missing queries + a `RotateClientSecret` domain helper. Secrets revealed exactly once.

**Files:**
- Modify: `db/queries/oidc.sql` (`UpdateOIDCClient`, `UpdateOIDCClientSecret`, `DeleteOIDCClient`)
- Create: `pkg/protocol/oidc/clientadmin.go` (`RotateClientSecret`)
- Create: `pkg/server/handle_admin_oidc_clients.go`
- Modify: `pkg/contract/auth.go` (`OIDCClientView`, read ops)
- Modify: `pkg/server/server.go` (routes)
- Test: `pkg/server/handle_admin_oidc_clients_test.go`

**Acceptance Criteria:**
- [ ] `GET /oidc-clients` (🔓) + `GET /oidc-clients/{clientId}` (🔓) return full config, never the secret/hash.
- [ ] `POST /oidc-clients` (🔐) reuses `BuildClientParams`; confidential → secret in the response body exactly once; list/detail never re-reveal.
- [ ] `PUT /oidc-clients/{clientId}` (🔐) updates redirect URIs, scopes, post-logout URIs, `require_consent`, `disabled`, display metadata.
- [ ] `POST /oidc-clients/rotate-secret` (🔐) → new secret once; old secret rejected at `/oauth/token`.
- [ ] `POST /oidc-clients/delete` (🔐).
- [ ] Each mutation audits (`factor=oidc_client`; register/update/rotate/revoke); no secret material in detail.

**Verify:** `go test ./pkg/server/ -run TestAdminOIDCClients -v` → PASS; smoke arc (Task 10) green.

**Steps:**

- [ ] **Step 1: Queries**

In `db/queries/oidc.sql` add (column list must match `002_oidc.sql`'s `oidc_client` — verify against the migration):
```sql
-- name: UpdateOIDCClient :one
UPDATE oidc_client SET
  display_name = $2, redirect_uris = $3, post_logout_redirect_uris = $4,
  allowed_scopes = $5, require_consent = $6, disabled = $7
WHERE client_id = $1
RETURNING *;

-- name: UpdateOIDCClientSecret :exec
UPDATE oidc_client SET client_secret_hash = $2 WHERE client_id = $1;

-- name: DeleteOIDCClient :execrows
DELETE FROM oidc_client WHERE client_id = $1;
```
Run `mise exec sqlc -- sqlc generate`.

- [ ] **Step 2: RotateClientSecret domain helper**

Create `pkg/protocol/oidc/clientadmin.go`:
```go
package oidc

import (
	"context"
	"prohibitorum/pkg/db"
)

// RotateClientSecret generates a new client secret, stores only its argon2id
// hash, and returns the cleartext once. Reuses the same secret-gen + hashing
// path BuildClientParams uses (extract the shared helper from clientgen.go so
// create + rotate share one implementation).
func RotateClientSecret(ctx context.Context, q *db.Queries, clientID string) (secret string, err error) {
	secret, hash, err := generateClientSecret() // shared helper (extracted from BuildClientParams)
	if err != nil {
		return "", err
	}
	if err := q.UpdateOIDCClientSecret(ctx, db.UpdateOIDCClientSecretParams{ClientID: clientID, ClientSecretHash: hash}); err != nil {
		return "", err
	}
	return secret, nil
}
```
> Refactor `BuildClientParams` (clientgen.go) to call the same `generateClientSecret()` so create and rotate cannot drift. Keep the CLI working (it calls `BuildClientParams`).

- [ ] **Step 3: Contract views + read ops**

In `pkg/contract/auth.go`, add `OIDCClientView` (clientId, displayName, redirectUris, postLogoutRedirectUris, allowedScopes, tokenEndpointAuthMethod, requireConsent, disabled — NO secret), and `OperationListOIDCClients` (`GET /oidc-clients`) + `OperationGetOIDCClient` (`GET /oidc-clients/{clientId}`).

- [ ] **Step 4: Handlers**

Create `pkg/server/handle_admin_oidc_clients.go`:
- `handleListOIDCClients` / `handleGetOIDCClient` (typed) — reuse `ListOIDCClients` / `GetOIDCClient`, project to `OIDCClientView`.
- `handleCreateOIDCClientHTTP(w,r)` — decode opts, `oidc.BuildClientParams`, `InsertOIDCClient`; audit `EventRegister`; respond 201 with `{client: OIDCClientView, secret: "<once>"}` (omit `secret` for public clients).
- `handleUpdateOIDCClientHTTP(w,r)` — `clientId := chi.URLParam(r,"clientId")`; decode; `UpdateOIDCClient`; map `pgx.ErrNoRows`→404; audit `EventUpdate` with a redacted field-diff in detail.
- `handleRotateOIDCClientSecretHTTP(w,r)` — decode `{clientId}`; `oidc.RotateClientSecret`; audit `EventRotate`; respond `{secret:"<once>"}`.
- `handleDeleteOIDCClientHTTP(w,r)` — decode `{clientId}`; `DeleteOIDCClient`; rows==0→404; audit `EventRevoke`.

- [ ] **Step 5: Routes**

```go
registerOp(mgmt, contract.OperationListOIDCClients, s.handleListOIDCClients, admin)
registerOp(mgmt, contract.OperationGetOIDCClient, s.handleGetOIDCClient, admin)
s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/oidc-clients", admin, s.handleCreateOIDCClientHTTP)
s.registerSudoOpHTTP(s.router, "PUT", "/api/prohibitorum/oidc-clients/{clientId}", admin, s.handleUpdateOIDCClientHTTP)
s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/oidc-clients/rotate-secret", admin, s.handleRotateOIDCClientSecretHTTP)
s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/oidc-clients/delete", admin, s.handleDeleteOIDCClientHTTP)
```

- [ ] **Step 6: Tests + commit**

`pkg/server/handle_admin_oidc_clients_test.go`: create reveals secret once; list/detail never reveal it; update changes config + audits; rotate returns a new secret; each mutation route rejects no-sudo with `sudo_required`.
Run: `go test ./pkg/server/ -run TestAdminOIDCClients -v` → PASS.
```bash
git add db/queries/oidc.sql pkg/db pkg/protocol/oidc/clientadmin.go pkg/protocol/oidc/clientgen.go pkg/server/handle_admin_oidc_clients.go pkg/server/handle_admin_oidc_clients_test.go pkg/contract/auth.go pkg/server/server.go
git commit -m "feat(admin): OIDC client management endpoints (reveal-once secret)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: SAML SP admin endpoints

**Goal:** SAML SP management — list/detail (🔓), create/update/reingest-metadata/delete (🔐), reusing the CLI's metadata-ingest domain path and adding the missing queries.

**Files:**
- Modify: `db/queries/saml_sp.sql` (`UpdateSAMLSP`, `DeleteSAMLSP`, `DeleteSAMLSPACSByID`, `DeleteSAMLSPKeysByID`)
- Create: `pkg/server/handle_admin_saml_sps.go`
- Modify: `pkg/contract/auth.go` (`SAMLProviderView` + ACS/key sub-views, read ops)
- Modify: `pkg/server/server.go` (routes)
- (Maybe) Modify: `pkg/protocol/saml/*` to expose the metadata-ingest function the CLI uses, if it's currently private to `cmd/`.
- Test: `pkg/server/handle_admin_saml_sps_test.go`

**Acceptance Criteria:**
- [ ] `GET /saml-providers` + `GET /saml-providers/{id}` (🔓) return SP config + ACS list + key fingerprints; no raw private material.
- [ ] `POST /saml-providers` (🔐) creates SP + ACS + key rows in one tx via the shared metadata-ingest path.
- [ ] `PUT /saml-providers/{id}` (🔐) updates `require_signed_authn_request`, `allow_idp_initiated`, `session_lifetime`, attribute map, NameID format.
- [ ] `POST /saml-providers/{id}/reingest-metadata` (🔐) replaces ACS + key children in one tx.
- [ ] `POST /saml-providers/delete` (🔐) removes SP + children.
- [ ] Each mutation audits (`factor=saml_sp`).

**Verify:** `go test ./pkg/server/ -run TestAdminSAMLSPs -v` → PASS

**Steps:**

- [ ] **Step 1: Locate/extract the metadata-ingest domain function.** Read the `saml-sp create` cobra command (`cmd/prohibitorum/main.go` ~line 351) to find the SP+ACS+key insert path. If the parsing/insert logic lives in the command closure, extract it into `pkg/protocol/saml` as `IngestSPMetadata(ctx, q, opts) (db.SamlSp, error)` (one tx), and have the CLI call it. This is the reuse point for both create and reingest.

- [ ] **Step 2: Queries.** In `db/queries/saml_sp.sql` add `UpdateSAMLSP` (RETURNING *), `DeleteSAMLSP` (`:execrows`), `DeleteSAMLSPACSByID` (`DELETE FROM saml_sp_acs WHERE sp_id=$1`), `DeleteSAMLSPKeysByID` (`DELETE FROM saml_sp_key WHERE sp_id=$1`). Verify column names against `005_saml.sql`. `mise exec sqlc -- sqlc generate`.

- [ ] **Step 3: Contract views.** Add `SAMLProviderView` (id, entityId, nameIdFormat, requireSignedAuthnRequest, allowIdpInitiated, sessionLifetime, attributeMap, acs []ACSView, keys []SAMLKeyView{fingerprint, use, notAfter}), `OperationListSAMLProviders`, `OperationGetSAMLProvider`.

- [ ] **Step 4: Handlers.** `handle_admin_saml_sps.go`: list/detail (typed, reuse `ListSAMLSPs`/`GetSAMLSPByID` + `ListSAMLSPACSEndpoints` + `ListSAMLSPKeys`); create (🔐, `saml.IngestSPMetadata`); update (🔐, `UpdateSAMLSP`); reingest (🔐 — tx: `DeleteSAMLSPACSByID`+`DeleteSAMLSPKeysByID`, then re-insert from fresh metadata); delete (🔐, tx delete children then `DeleteSAMLSP`). Audit each.

- [ ] **Step 5: Routes.**
```go
registerOp(mgmt, contract.OperationListSAMLProviders, s.handleListSAMLProviders, admin)
registerOp(mgmt, contract.OperationGetSAMLProvider, s.handleGetSAMLProvider, admin)
s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/saml-providers", admin, s.handleCreateSAMLProviderHTTP)
s.registerSudoOpHTTP(s.router, "PUT", "/api/prohibitorum/saml-providers/{id}", admin, s.handleUpdateSAMLProviderHTTP)
s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/saml-providers/{id}/reingest-metadata", admin, s.handleReingestSAMLProviderHTTP)
s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/saml-providers/delete", admin, s.handleDeleteSAMLProviderHTTP)
```

- [ ] **Step 6: Tests + commit.** `TestAdminSAMLSPs`: create from a metadata fixture → detail shows ACS+keys; update flips a flag; reingest swaps ACS; delete removes; no-sudo → `sudo_required`. `go test ./pkg/server/ -run TestAdminSAMLSPs -v`.
```bash
git add db/queries/saml_sp.sql pkg/db pkg/protocol/saml pkg/server/handle_admin_saml_sps.go pkg/server/handle_admin_saml_sps_test.go pkg/contract/auth.go pkg/server/server.go cmd/prohibitorum/main.go
git commit -m "feat(admin): SAML SP management endpoints (metadata-ingest shared with CLI)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Upstream IdP admin endpoints

**Goal:** Upstream-IdP management — list/detail (🔓, secret write-only), create/update/rotate-secret/delete (🔐), reusing the existing `upstream_idp` queries and `oidc.SealClientSecret`.

**Files:**
- Create: `pkg/server/handle_admin_upstream_idps.go`
- Modify: `pkg/contract/auth.go` (`UpstreamIDPView`, read ops)
- Modify: `pkg/server/server.go` (routes)
- (Maybe) Modify: `db/queries/upstream_idp.sql` if a secret-only update query is cleaner than reusing `UpdateUpstreamIDP`.
- Test: `pkg/server/handle_admin_upstream_idps_test.go`

**Acceptance Criteria:**
- [ ] `GET /upstream-idps` + `GET /upstream-idps/{slug}` (🔓) return config; never the sealed secret.
- [ ] `POST /upstream-idps` (🔐) seals the secret via `oidc.SealClientSecret`.
- [ ] `PUT /upstream-idps/{slug}` (🔐) updates mode, allowed_domains, scopes, claim overrides, `require_verified_email` — **excludes** the secret.
- [ ] `POST /upstream-idps/rotate-secret` (🔐) seals a new secret.
- [ ] `POST /upstream-idps/delete` (🔐).
- [ ] Each mutation audits (`factor=upstream_idp`).

**Verify:** `go test ./pkg/server/ -run TestAdminUpstreamIDPs -v` → PASS

**Steps:**

- [ ] **Step 1: Read existing surface.** Confirm `InsertUpstreamIDP`/`UpdateUpstreamIDP`/`DeleteUpstreamIDP`/`GetUpstreamIDPBySlug`/`ListUpstreamIDPs` params, and `oidc.SealClientSecret`'s signature (`pkg/federation/oidc/secret.go`). If `UpdateUpstreamIDP` includes the secret column, add a secret-only `UpdateUpstreamIDPSecret` query so `PUT` can exclude the secret and `rotate-secret` can set it alone.

- [ ] **Step 2: Contract view + read ops.** `UpstreamIDPView` (slug, displayName, issuerUrl, clientId, scopes, mode, allowedDomains, requireVerifiedEmail, claim overrides — NO secret), `OperationListUpstreamIDPs`, `OperationGetUpstreamIDP`.

- [ ] **Step 3: Handlers.** list/detail (typed); create (🔐 — seal secret, `InsertUpstreamIDP`); update (🔐 — `UpdateUpstreamIDP` minus secret); rotate-secret (🔐 — seal new, `UpdateUpstreamIDPSecret`); delete (🔐). Audit each; never put secret/sealed bytes in detail.

- [ ] **Step 4: Routes.**
```go
registerOp(mgmt, contract.OperationListUpstreamIDPs, s.handleListUpstreamIDPs, admin)
registerOp(mgmt, contract.OperationGetUpstreamIDP, s.handleGetUpstreamIDP, admin)
s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/upstream-idps", admin, s.handleCreateUpstreamIDPHTTP)
s.registerSudoOpHTTP(s.router, "PUT", "/api/prohibitorum/upstream-idps/{slug}", admin, s.handleUpdateUpstreamIDPHTTP)
s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/upstream-idps/rotate-secret", admin, s.handleRotateUpstreamIDPSecretHTTP)
s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/upstream-idps/delete", admin, s.handleDeleteUpstreamIDPHTTP)
```

- [ ] **Step 5: Tests + commit.** `TestAdminUpstreamIDPs`: create→detail (no secret); update excludes secret; rotate seals new; no-sudo→`sudo_required`. 
```bash
git add db/queries/upstream_idp.sql pkg/db pkg/server/handle_admin_upstream_idps.go pkg/server/handle_admin_upstream_idps_test.go pkg/contract/auth.go pkg/server/server.go
git commit -m "feat(admin): upstream IdP management endpoints (secret write-only)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Audit-events viewer

**Goal:** `GET /audit-events` (🔓) — a read-only, paginated, filterable view over `credential_event`, surfacing the config-change history the earlier tasks now emit.

**Files:**
- Modify: `db/queries/credential_event.sql` (`ListCredentialEvents`)
- Create: `pkg/server/handle_admin_audit.go`
- Modify: `pkg/contract/auth.go` (`AuditEventView`, `OperationListAuditEvents`)
- Modify: `pkg/server/server.go` (route)
- Test: `pkg/server/handle_admin_audit_test.go`

**Acceptance Criteria:**
- [ ] `GET /audit-events?factor=&event=&accountId=&since=&until=&before=&limit=` returns events newest-first with keyset pagination (`before` = the last seen id/timestamp cursor; `limit` capped, default 50).
- [ ] `AuditEventView` exposes `id, at, accountId, factor, event, detail` — and the redaction test proves `detail` never carries private keys, secrets, tokens, auth codes, or raw SAML.

**Verify:** `go test ./pkg/server/ -run TestAdminAuditEvents -v` → PASS

**Steps:**

- [ ] **Step 1: Query.**
```sql
-- name: ListCredentialEvents :many
SELECT * FROM credential_event
WHERE ($1::text IS NULL OR factor = $1)
  AND ($2::text IS NULL OR event = $2)
  AND ($3::int  IS NULL OR account_id = $3)
  AND ($4::timestamptz IS NULL OR at >= $4)
  AND ($5::timestamptz IS NULL OR at <= $5)
  AND ($6::bigint IS NULL OR id < $6)
ORDER BY id DESC
LIMIT $7;
```
> Adjust column types to match `credential_event` in `001_initial.sql` (id type, `at` column). `mise exec sqlc -- sqlc generate`.

- [ ] **Step 2: Contract + handler.** `AuditEventView` + `OperationListAuditEvents` (`GET /audit-events`). `handleListAuditEvents` parses query params (nil when absent), caps `limit` at e.g. 200 default 50, projects rows to views (decode `detail` jsonb → map).

- [ ] **Step 3: Route.** `registerOp(mgmt, contract.OperationListAuditEvents, s.handleListAuditEvents, admin)`.

- [ ] **Step 4: Redaction test.** `TestAdminAuditEvents`: drive a couple of admin mutations (via the Task 3/4 handlers or by inserting representative `credential_event` rows), then `GET /audit-events` and assert: results are newest-first; the filters narrow correctly; and a scan of every `detail` value contains no `private_pem`, no `client_secret`, no JWT-looking string, no `BEGIN ... PRIVATE KEY`, no base64 SAML assertion. This is the redaction guard.

- [ ] **Step 5: Commit.**
```bash
git add db/queries/credential_event.sql pkg/db pkg/server/handle_admin_audit.go pkg/server/handle_admin_audit_test.go pkg/contract/auth.go pkg/server/server.go
git commit -m "feat(admin): audit-events viewer (paginated, filterable, redaction-guarded)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Admin account-credentials list

**Goal:** `GET /accounts/{id}/credentials` (🔓) so the dashboard can enumerate an account's credentials and drive the existing `POST /accounts/credentials/delete` force-revoke (🔐).

**Files:**
- Modify: `pkg/server/handle_account.go` (add the list handler)
- Modify: `pkg/contract/auth.go` (`OperationListAccountCredentials`)
- Modify: `pkg/server/server.go` (route)
- Test: `pkg/server/handle_account_credentials_test.go`

**Acceptance Criteria:**
- [ ] `GET /accounts/{id}/credentials` returns `[]contract.CredentialView` (suffix only, never the full credential id) for the account; 404 if the account doesn't exist.
- [ ] Reuses the existing `ListCredentialsByAccount` query and `CredentialView` type.

**Verify:** `go test ./pkg/server/ -run TestListAccountCredentials -v` → PASS

**Steps:**

- [ ] **Step 1: Op.** In `pkg/contract/auth.go`:
```go
var OperationListAccountCredentials = huma.Operation{
	OperationID: "listAccountCredentials",
	Method:      http.MethodGet,
	Path:        "/accounts/{id}/credentials",
	Summary:     "List an account's WebAuthn credentials (admin only).",
}
```

- [ ] **Step 2: Handler.** In `handle_account.go`, add `handleListAccountCredentials(ctx, *getAccountIn) (*listCredentialsOut, error)`: `GetAccountByID` (404 on `pgx.ErrNoRows`), `ListCredentialsByAccount`, project to `CredentialView` (mirror the `/me` credential projection — find the existing `credentialViewFrom...` helper and reuse it so the suffix logic is identical).

- [ ] **Step 3: Route.** `registerOp(mgmt, contract.OperationListAccountCredentials, s.handleListAccountCredentials, admin)`.

- [ ] **Step 4: Test + commit.** `TestListAccountCredentials`: an account with N credentials returns N views with only the 4-char suffix; unknown account → 404.
```bash
git add pkg/server/handle_account.go pkg/server/handle_account_credentials_test.go pkg/contract/auth.go pkg/server/server.go
git commit -m "feat(admin): list an account's credentials (unblocks force-revoke UI)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Route-policy test — every mutation is sudo-wrapped

**Goal:** A single cross-cutting test that proves every admin mutation route rejects a fresh-but-no-sudo admin session with `sudo_required`. The whole security model depends on this; it must fail loudly if any future route skips `registerSudoOpHTTP`.

**Files:**
- Create: `pkg/server/admin_route_policy_test.go`

**Acceptance Criteria:**
- [ ] An enumerated list of every 🔐 admin route (method + path) is asserted: with an admin session that has NO fresh sudo, the response is 401 `sudo_required` and the underlying mutation did not occur.
- [ ] The test fails if a new 🔐 route is added but omitted from the list (guard comment instructs maintainers to add new mutations here).

**Verify:** `go test ./pkg/server/ -run TestAdminMutationRoutesRequireSudo -v` → PASS

**Steps:**

- [ ] **Step 1: Write the test.** Build the real router (`NewServer` against the test pool, or a minimal `Server` with the routes registered), inject an admin session with `SudoUntil` zero, and table-drive every 🔐 route:
```go
var sudoGatedRoutes = []struct{ method, path, body string }{
	{"POST", "/api/prohibitorum/signing-keys/generate", `{}`},
	{"POST", "/api/prohibitorum/signing-keys/abc/activate", `{}`},
	{"POST", "/api/prohibitorum/signing-keys/abc/retire", `{}`},
	{"POST", "/api/prohibitorum/oidc-clients", `{"clientId":"x"}`},
	{"PUT",  "/api/prohibitorum/oidc-clients/x", `{}`},
	{"POST", "/api/prohibitorum/oidc-clients/rotate-secret", `{"clientId":"x"}`},
	{"POST", "/api/prohibitorum/oidc-clients/delete", `{"clientId":"x"}`},
	{"POST", "/api/prohibitorum/saml-providers", `{}`},
	{"PUT",  "/api/prohibitorum/saml-providers/1", `{}`},
	{"POST", "/api/prohibitorum/saml-providers/1/reingest-metadata", `{}`},
	{"POST", "/api/prohibitorum/saml-providers/delete", `{"id":1}`},
	{"POST", "/api/prohibitorum/upstream-idps", `{}`},
	{"PUT",  "/api/prohibitorum/upstream-idps/x", `{}`},
	{"POST", "/api/prohibitorum/upstream-idps/rotate-secret", `{"slug":"x"}`},
	{"POST", "/api/prohibitorum/upstream-idps/delete", `{"slug":"x"}`},
	{"POST", "/api/prohibitorum/accounts/credentials/delete", `{"accountId":1,"credentialId":1}`},
}
// For each: serve with a no-sudo admin session; assert 401 + body contains "sudo_required".
```
Add a guard comment: *"Every `registerSudoOpHTTP` route MUST appear here. Adding a 🔐 route without adding it here is a security bug."*

> Note: `/accounts/credentials/delete` currently uses the typed `registerOp` path and is NOT sudo-gated today. Part of this task is to migrate it to `registerSudoOpHTTP` (the design firmly classifies admin credential-revoke as 🔐). Convert it to a raw handler and include it above.

- [ ] **Step 2: Convert `/accounts/credentials/delete` to 🔐.** Re-register it via `registerSudoOpHTTP` with a raw wrapper around the existing `handleDeleteAccountCredential` logic (or a new raw handler). Remove its `registerOp` registration.

- [ ] **Step 3: Run + commit.**
Run: `go test ./pkg/server/ -run TestAdminMutationRoutesRequireSudo -v` → PASS.
```bash
git add pkg/server/admin_route_policy_test.go pkg/server/server.go pkg/server/handle_account.go
git commit -m "test(admin): route-policy guard — every admin mutation requires sudo

Also promotes /accounts/credentials/delete to 🔐 per the design.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: CLI parity + smoke arc

**Goal:** Give the CLI matching verbs on the shared domain code path (signing-key lifecycle especially), and extend `cmd/smoke` with the admin-API arc (including the corrected key-lifecycle JWKS assertions).

**Files:**
- Modify: `cmd/prohibitorum/main.go` (signing-key `generate`/`activate`/`retire`; oidc-client `update`/`rotate-secret`/`delete`; saml-sp `update`/`delete`; upstream-idp subcommands if not present)
- Modify: `cmd/smoke/main.go` (admin-API arc)

**Acceptance Criteria:**
- [ ] `prohibitorum signing-key {generate,activate,retire}` work via the same `pkg/protocol/oidc` domain functions, with `--yes` confirmation on activate/retire.
- [ ] Smoke arc: create OIDC client (secret once; list/detail never reveal) → update → rotate-secret (old rejected at `/oauth/token`) → **generate** signing key (assert JWKS publishes *current active + new pending*) → **activate** (assert signing uses the new active; prior → decommissioning; JWKS publishes *new active + prior decommissioning*; a token signed by the prior key still verifies during grace) → `GET /audit-events` reflects the mutations → list account credentials → force-revoke.
- [ ] `SMOKE_EXIT=0` end-to-end.

**Verify:** full smoke run → `SMOKE_EXIT=0`

**Steps:**

- [ ] **Step 1: CLI subcommands.** Add cobra commands that call `oidc.InsertPendingKey`, `oidc.ActivateSigningKey`, `oidc.RetireSigningKey` (and the OIDC/SAML/upstream admin domain helpers), mirroring the existing `signing-key`/`oidc-client`/`saml-sp` command structure (config parse → migrate → pool → call domain fn → print). Add `--yes` gating on destructive verbs and operator-context audit (`s`-less: write a `credential_event` with a CLI marker, or log).

- [ ] **Step 2: Smoke arc.** Extend `cmd/smoke/main.go` after the existing last step. The smoke already holds an admin session (it shells `enroll-admin`); for 🔐 calls it must first drive `/me/sudo/{begin,complete}` (the WebAuthn sudo path the smoke already exercises) to get a fresh grant before each mutation. Implement the lifecycle assertions exactly as in the acceptance criteria — note the key correction: **after generate**, JWKS = current active + new pending; **after activate**, JWKS = new active + prior decommissioning, and a token minted under the prior key still verifies during grace.

- [ ] **Step 3: Run + commit.**
Run the smoke: `setsid bash /tmp/run_v06.sh`; poll `/tmp/v06.result` for `SMOKE_EXIT=0`.
```bash
git add cmd/prohibitorum/main.go cmd/smoke/main.go
git commit -m "feat(cli,smoke): admin-API CLI verbs + smoke arc (corrected key lifecycle)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 11: Docs

**Goal:** Reflect the new admin surface in the living docs.

**Files:**
- Modify: `AUDIT.md` (admin-management rows + sudo posture), `STATUS.md` (new phase), `ARCHITECTURE.md` (admin API surface), `api.md` (raw `registerSudoOpHTTP` routes — they lack OpenAPI).

**Acceptance Criteria:**
- [ ] `AUDIT.md` documents the admin-management endpoints, the sudo-gating model, the audit coverage, and the signing-key lifecycle.
- [ ] `STATUS.md` records the Admin Management API phase as done (with smoke-step references).
- [ ] `ARCHITECTURE.md` lists the admin API routes alongside the existing accounts/invitations surface.
- [ ] `api.md` documents the raw sudo-gated routes (method, path, gate, body, reveal-once semantics).

**Verify:** manual read; `docs anchor` — every "implemented" claim traces to a smoke step or a test name (per `feedback_doc_writing_anchor_to_code`).

**Steps:**

- [ ] **Step 1: Update each doc** with code-anchored claims (distinguish implemented / smoke-verified / unit-tested per the AUDIT.md label convention). Note the deferred `009` migration (drops `active`/`retired_at`) as the contract-phase follow-up.

- [ ] **Step 2: Commit.**
```bash
git add AUDIT.md STATUS.md ARCHITECTURE.md api.md
git commit -m "docs: Admin Management API — endpoints, sudo posture, key lifecycle

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review

- **Spec coverage:** §3 access-control → Task 0 wrapper + Task 9 guard; §4 baseline protections → Task 0; §5 endpoints → Tasks 3–8; §6 signing-key lifecycle → Tasks 1–3; §7 audit → Task 0 constants + per-handler audit + Task 7 redaction test; §8 queries/contract/files → Tasks 3–8; §9 testing → per-task tests + Tasks 9–10; §10 docs → Task 11. All spec sections map to a task.
- **Type consistency:** `registerSudoOpHTTP`/`withFreshSudo` (Task 0) used identically in Tasks 3–8; `oidc.InsertPendingKey`/`ActivateSigningKey`/`RetireSigningKey`/`ErrActiveKeyNoReplacement` defined in Task 2, consumed in Tasks 3 & 10; `ListPublishableSigningKeys`/`ListAllSigningKeys`/`GetSigningKeyByKID` defined in Task 2 used in Tasks 2–3; `SigningKeyView`/`OIDCClientView`/etc. defined where first used.
- **Open confirmations for the implementer (verify against code, don't assume):** exact `authn` session constructor/type names (Task 0 test); the repo's integration-test pool helper (Tasks 1–3); `GenerateSigningKey()` return field names + `pgtype.Timestamptz` conversion helper (Task 2); `oidc_client`/`saml_sp`/`upstream_idp`/`credential_event` exact column names from migrations 001/002/004/005 (Tasks 4–7); whether the SAML metadata-ingest logic is already extracted from the CLI (Task 5); the rotation-grace config field name (Task 2).
