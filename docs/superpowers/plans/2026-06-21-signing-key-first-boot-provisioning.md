# First-boot OIDC signing-key auto-provisioning — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On server boot, auto-provision + activate an OIDC signing key when none is active, so a fresh instance has a working OP (and forward-auth) without a manual `signing-key generate`.

**Architecture:** A small `ensureActiveSigningKey` boot helper in `pkg/server`, called from `NewServer` after `queries` is built. It reuses `oidc.InsertPendingKey` + `oidc.ActivateSigningKey` (the same calls the `signing-key generate` CLI and the dev `ensureSigningKey` use). Idempotent (no-op when a key is active) and warn-not-fatal.

**Tech Stack:** Go (pgx, the existing `pkg/protocol/oidc` key lifecycle, `logx`). Backend-only; no migration, no SPA/dist change.

**Spec:** `docs/superpowers/specs/2026-06-21-signing-key-first-boot-provisioning-design.md`

**Conventions:** build `go build -tags nodynamic ./...`; gate adds `go vet ./...` + `go test ./...`. NO `Co-Authored-By` trailer. Runtime verification: the harness kills controller-launched servers — use a **subagent** to hold a live server; podman Postgres is up on :5432 (DB `prohibitorum_dev`, user/pass `prohibitorum`); `source scripts/dev-env.sh` for the DSN.

**Verified anchors:**
- `NewServer` (`pkg/server/server.go`): `queries := db.New(conn)` at ~line 139; `conn` is `*pgxpool.Pool`, `queries` is `*db.Queries`, `config` is `*configx.Config` in scope. Migrations already ran above.
- `q.GetActiveSigningKey(ctx) (db.SigningKey, error)` → `pgx.ErrNoRows` when none.
- `oidc.InsertPendingKey(ctx, q *db.Queries, dek []byte, keyVer int32) (db.SigningKey, error)`.
- `oidc.ActivateSigningKey(ctx, pool txBeginner, q *db.Queries, kid string, grace time.Duration) (db.SigningKey, error)` (`*pgxpool.Pool` satisfies `txBeginner`).
- The CLI + dev use `grace := config.SAML.MetadataRotationGrace` for activation — match it.
- DEK selection mirrors the CLI `mustCurrentDEK`: highest version in `config.DataEncryptionKeys map[int][]byte`.
- `logx.WithContext(ctx).Info/Warn(...)`, `.WithError(err)`, `.WithFields(logrus.Fields{...})` (match the existing usage in `server.go`, e.g. the reconcile loop + branding wiring).

---

### Task 1: `ensureActiveSigningKey` helper + call site + unit test

**Goal:** The boot helper that provisions+activates an initial signing key when none is active, wired into `NewServer`, with a pure unit test for the DEK selection.

**Files:**
- Create: `pkg/server/signing_key_bootstrap.go`
- Modify: `pkg/server/server.go` (call site in `NewServer`)
- Create: `pkg/server/signing_key_bootstrap_test.go`

**Acceptance Criteria:**
- [ ] `ensureActiveSigningKey` is a no-op when `GetActiveSigningKey` succeeds; on `pgx.ErrNoRows` it provisions (`InsertPendingKey` + `ActivateSigningKey`) and logs at info; any other `GetActiveSigningKey` error → warn + return.
- [ ] Empty `DataEncryptionKeys` → warn + return (no provision).
- [ ] Provision errors (insert/activate) → warn + return (NOT fatal; boot continues).
- [ ] `NewServer` calls it after `queries := db.New(conn)`.
- [ ] `currentDEK(cfg)` returns the highest-version key + `ok=false` when none; unit-tested.
- [ ] `go build -tags nodynamic ./...` → 0; `go test ./pkg/server/ -run SigningKeyBootstrap` → ok.

**Verify:** `go build -tags nodynamic ./... && go test ./pkg/server/ -run SigningKeyBootstrap -count=1`

**Steps:**

- [ ] **Step 1: write the failing test** `pkg/server/signing_key_bootstrap_test.go` (the pure DEK-selection seam — the orchestration is covered by the runtime task):

```go
package server

import (
	"testing"

	"prohibitorum/pkg/configx"
)

func TestSigningKeyBootstrap_CurrentDEK(t *testing.T) {
	// No keys → not ok.
	if _, _, ok := currentDEK(&configx.Config{}); ok {
		t.Fatal("empty key set should return ok=false")
	}
	// Highest version wins.
	cfg := &configx.Config{DataEncryptionKeys: map[int][]byte{1: []byte("aaaa"), 3: []byte("cccc"), 2: []byte("bbbb")}}
	ver, key, ok := currentDEK(cfg)
	if !ok || ver != 3 || string(key) != "cccc" {
		t.Fatalf("currentDEK = (%d, %q, %v), want (3, cccc, true)", ver, key, ok)
	}
}
```

- [ ] **Step 2: run to verify it fails**

Run: `go test ./pkg/server/ -run SigningKeyBootstrap -count=1`
Expected: FAIL (`currentDEK` undefined).

- [ ] **Step 3: implement** `pkg/server/signing_key_bootstrap.go`:

```go
// Package server — signing_key_bootstrap.go
//
// First-boot OIDC signing-key auto-provisioning. A fresh instance has no active
// signing key, which makes the whole OIDC OP (and forward-auth) non-functional
// until an admin runs `signing-key generate`. ensureActiveSigningKey closes that
// gap at boot, reusing the same lifecycle calls as the CLI/dev tooling.
package server

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/logx"
	oidc "prohibitorum/pkg/protocol/oidc"
)

// ensureActiveSigningKey provisions + activates an initial OIDC signing key when
// none is active. Idempotent (no-op when a key is active) and warn-not-fatal: a
// transient error or a missing DEK never crashes boot — the operator can still
// run `signing-key generate`.
func ensureActiveSigningKey(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, cfg *configx.Config) {
	if _, err := q.GetActiveSigningKey(ctx); err == nil {
		return // active key already present
	} else if !errors.Is(err, pgx.ErrNoRows) {
		logx.WithContext(ctx).WithError(err).Warn("signing key: could not check for an active key; skipping auto-provision")
		return
	}

	keyVer, dek, ok := currentDEK(cfg)
	if !ok {
		logx.WithContext(ctx).Warn("signing key: no data encryption key configured; cannot auto-provision (set PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>, then run `signing-key generate`)")
		return
	}

	pending, err := oidc.InsertPendingKey(ctx, q, dek, keyVer)
	if err != nil {
		logx.WithContext(ctx).WithError(err).Warn("signing key: auto-provision insert failed; run `signing-key generate` manually")
		return
	}
	if _, err := oidc.ActivateSigningKey(ctx, pool, q, pending.Kid, cfg.SAML.MetadataRotationGrace); err != nil {
		logx.WithContext(ctx).WithError(err).Warn("signing key: auto-provision activate failed; run `signing-key generate` manually")
		return
	}
	logx.WithContext(ctx).WithFields(logrus.Fields{"kid": pending.Kid}).Info("auto-provisioned initial OIDC signing key")
}

// currentDEK returns the highest-version data encryption key from cfg, or
// ok=false when none is configured. Mirrors the CLI's mustCurrentDEK selection.
func currentDEK(cfg *configx.Config) (int32, []byte, bool) {
	if len(cfg.DataEncryptionKeys) == 0 {
		return 0, nil, false
	}
	maxVer := 0
	for v := range cfg.DataEncryptionKeys {
		if v > maxVer {
			maxVer = v
		}
	}
	return int32(maxVer), cfg.DataEncryptionKeys[maxVer], true
}
```

(Confirm the `logx`/`logrus` import paths + that `oidc` is the package name used elsewhere in `pkg/server` — `server.go` imports `pkg/protocol/oidc` as `oidcop`; use the SAME alias the file already uses to avoid a duplicate import. If `server.go` uses `oidcop`, write `oidcop.InsertPendingKey`/`oidcop.ActivateSigningKey` and import accordingly in the new file, e.g. `oidcop "prohibitorum/pkg/protocol/oidc"`. Verify `logx.WithContext(...).WithFields(...)` exists; if `logx` exposes a different field helper, match it.)

- [ ] **Step 4: call site** — in `pkg/server/server.go`, immediately after `queries := db.New(conn)`:

```go
	queries := db.New(conn)

	// First-boot: ensure an active OIDC signing key exists (no-op if one does).
	ensureActiveSigningKey(ctx, conn, queries, config)
```

- [ ] **Step 5: run to verify pass + build**

Run: `go test ./pkg/server/ -run SigningKeyBootstrap -count=1 && go build -tags nodynamic ./... && go vet ./pkg/server/...`
Expected: PASS + 0. `gofmt -l` the two new/changed files → clean.

- [ ] **Step 6: commit**

```bash
git add pkg/server/signing_key_bootstrap.go pkg/server/signing_key_bootstrap_test.go pkg/server/server.go
git commit -m "feat(server): auto-provision an OIDC signing key on first boot"
```

---

### Task 2: Runtime verification (fresh keyset boots functional)

**Goal:** Prove that an instance with no signing key boots and provisions a working one (JWKS exposes it; `/oauth/authorize` no longer fails for a missing key).

**Files:** none (verification only).

**Acceptance Criteria:**
- [ ] With the `signing_key` table empty, the server boots and logs `auto-provisioned initial OIDC signing key`.
- [ ] After boot, `signing_key` has exactly one `active` row, and `GET /oauth/jwks` (or the discovery `jwks_uri`) exposes one key.
- [ ] A forward-auth `verify` 302 reaches `/oauth/authorize` and that authorize step does not fail for a missing key (it proceeds to login).
- [ ] Re-booting is a no-op (still exactly one active key; no duplicate).

**Verify:** runtime evidence captured (below).

**Steps:**

- [ ] **Step 1: full gate**

```bash
go vet ./... && go build -tags nodynamic ./... && go test ./...
```
Expected: 0 / 0 / all `ok`.

- [ ] **Step 2: runtime (subagent — controller servers get killed).** In a subagent: build `/tmp/proh-sk` from HEAD; `source scripts/dev-env.sh`. Empty the keyset on the dev DB: `psql "postgres://prohibitorum:prohibitorum@localhost:5432/prohibitorum_dev?sslmode=disable" -c "DELETE FROM signing_key;"`. Launch the server (`setsid /tmp/proh-sk >/tmp/proh-sk.log 2>&1 < /dev/null &`), wait for readiness. Then:
  - `grep -i "auto-provisioned initial OIDC signing key" /tmp/proh-sk.log` → present.
  - `psql ... -c "SELECT count(*) FROM signing_key WHERE status='active';"` → 1.
  - `curl -s http://localhost:8080/oauth/jwks` → JSON with one key (`keys[0].kid`).
  - `curl -s -o /dev/null -w "%{http_code} %{redirect_url}\n" -H 'X-Forwarded-Host: app.acme.io' -H 'X-Forwarded-Proto: https' -H 'X-Forwarded-Uri: /foo' http://localhost:8080/api/prohibitorum/forward-auth/verify` after registering a forward-auth app → `302` to `/oauth/authorize` (the authorize step works because a key exists). (Optional — the JWKS check is the primary evidence.)
  - Restart the server; confirm the keyset still has exactly one active key (no duplicate provisioning). Tear down.

- [ ] **Step 3: record evidence** in the task notes (log line, active-key count, JWKS key). No commit (verification only). No dist/SPA change.

---

## Self-Review Notes
- **Spec coverage:** the `ensureActiveSigningKey` helper + call site + idempotent/no-DEK/warn-not-fatal behavior (Task 1) ↔ spec Design §1–3; runtime fresh-boot proof + re-boot no-op (Task 2) ↔ spec Testing. No config opt-out (spec non-goal). Concurrency self-heal (spec) is handled by warn-not-fatal + the existing reconcile loop — no extra code.
- **Type consistency:** `ensureActiveSigningKey(ctx, pool, q, cfg)` + `currentDEK(cfg)` used consistently; reuses existing `GetActiveSigningKey`/`InsertPendingKey`/`ActivateSigningKey`.
- **Verify-before-assert flags:** the `oidc` import alias in `pkg/server` (use `oidcop` if that's what `server.go` uses); the `logx` field-logging API (`WithFields(logrus.Fields{...})` vs another shape) — match existing `server.go` usage.
