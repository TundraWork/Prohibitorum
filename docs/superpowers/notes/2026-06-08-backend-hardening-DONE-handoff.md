# Handoff — Backend security hardening (Tier 2) DONE

**Date:** 2026-06-08
**Branch:** `master` (no remote, no worktree — commit directly to master).
**HEAD:** `13173f8`. Feature commits `5257598`..`c2fe84f` + final-review fix `d007aab` + two done-gate fixes (`1c26291` deadlock, `13173f8` smoke).
**Tree:** clean. Done-gate GREEN — `go build/vet ./...` exit 0, `go test ./...` **14 packages, 0 failures**, `cmd/smoke` **SMOKE_EXIT=0 (121 steps)**. No migration, no frontend, no dist rebuild.

Read project memory first (`MEMORY.md` + `project_current_state.md` + `reference_backend_backlog.md`), then this note.

## What shipped — Tier-2 backend hardening (first of the Tier-1+2 backend cycles)
Decomposed Tier 1+2 into **Hardening (this) → Self-service/reads → D enrollment**; user chose hardening first. Spec: `docs/superpowers/specs/2026-06-08-backend-hardening-design.md`. Plan (6 tasks): `docs/superpowers/plans/2026-06-08-backend-hardening.md` (+`.tasks.json`). Subagent-driven: sonnet impl + spec-then-quality review/task + opus final whole-cycle review.

1. **`kv.Store.SetNX`** (`5257598`) — atomic set-if-absent; memory (mutex-guarded check+set) + redis (native `SET NX`); `ErrSetNXInvalidTTL`; fail-closed on backend error. 100-way concurrency test.
2. **Refresh-token rotation idempotency** (`34687aa`) — `SetNX` per-token rotation lock + a **previous-token idempotency window** (`PreviousToken`/`PreviousValidUntil`, 10s) on the family record. Benign concurrent/retried replay of the *immediately-previous* token returns the SAME successor (re-mints access/ID, returns the already-stored current refresh token — **no cached bearer envelope**, consistent with the already-raw-in-KV design); a stale/stolen token (beyond the single-previous window) still revokes the family. Fixes victim-lockout + double-mint. Audits `refresh_idempotent_replay` / `refresh_rotation_in_progress` / `refresh_reuse`.
3. **SAML AuthnRequest replay** (`03b46ff`) — atomic `SetNX`, key SP-scoped (`saml:authn_request_replay:{spEntityID}:{id}`), **fail-closed** on KV error. All 3 `sso.go` call sites updated.
4. **revoke-password-totp** (`3962476`) — pgx transaction + `GetAccountByIDForUpdate` account-row lock + lockout guard `would_remove_last_factor` (409) when no passkey AND no usable federation. New `CountUsableSignInFederation` query (excludes disabled-upstream); `AvailableMethods` tightened to use it.
5. **Passkey delete** (`b0a60c5`) — same account-row `FOR UPDATE` serialization (TOCTOU-safe vs #4).
6. **OIDC client-id timing oracle** (`c2fe84f`) — package-level dummy argon2id PHC (`password.DefaultParams()`, matching real client secrets) verified on the unknown/disabled-client path **only when a secret is presented** and **only for `errInvalidClient`** (not infra errors); uniform `invalid_client`. Testable via a `verifyClientSecret` seam.

**Final whole-cycle opus review** caught a real cross-cutting regression: tightening `AvailableMethods` (#4) without updating the sibling identity-**unlink** guard (still raw `len(identities)`) allowed a self-lockout (1 enabled + 1 disabled upstream). Fixed (`d007aab`): unlink now **deletes-then-checks `AvailableMethods` post-state** in the locked tx (rolls back if it would leave zero usable methods); regression test provably fails against the old logic.

## ⚠ Two real bugs the SMOKE caught that unit tests structurally could NOT
The Go unit tests for the revoke/passkey handlers use the `dbPool == nil` seam (inject a fake FlowQueries, skip the tx) — so they never exercise the real transaction, the `FOR UPDATE` lock, or the real FKs. The smoke (real Postgres) caught two issues at the done-gate:

- **`1c26291` — revoke deadlock (FOR UPDATE vs FK on a separate connection).** The revoke tx holds `FOR UPDATE` on the account row; `DisableNonWebAuthnFallbacks` then wrote `credential_event` audit rows (FK → `account`, needs `FOR KEY SHARE`) via the **pool-bound `s.Audit`** on a *separate* connection → app-level deadlock PG can't detect (the holder is idle-in-transaction). **Fix:** pass `audit.NewWriter(qtx)` so audit writes on the **same tx connection** (no self-conflict; audit now atomic with the deletes). *(Task 5 didn't hit this — it logs via `logx`, not a `credential_event` insert.)* **LESSON: any handler holding `FOR UPDATE` on a row must not write a row with an FK to it on a separate connection while the lock is held — route such writes through the tx querier.**
- **`13173f8` — smoke refresh-reuse assertion was stale.** Step 77 immediately replayed the just-rotated token expecting 400 (strict reuse); the approved idempotency window (#2) returns 200 same-successor for that case. Realigned the smoke to the approved semantics (and made it stronger: asserts idempotent replay → same successor, then a *two-generations-old* replay → 400 reuse + family revoke; no time-wait). The Go unit tests `TestRefreshReuseRevokesFamily` were already adapted similarly during Task 2.

## ⚠ Environment: the dev/smoke Postgres cluster was rebuilt
`/tmp/prohibitorum-pg` (mise postgres 18.4, :55432) was **corrupted** (missing relfilenode files in template1 + postgres — `/tmp` cleanup evicting PGDATA while running; a known hazard of PGDATA-in-`/tmp`). Rebuilt it: `pg_ctl stop` → `rm -rf /tmp/prohibitorum-pg` → `initdb -U tundra --auth=trust` → `pg_ctl start -o "-p 55432"`. The `prohibitorum_dev` DB was wiped — `dev-env.sh` recreates it on the next `mise dev-server`, and `mise dev-seed` re-seeds. The smoke's `postgres` DB is reset every run.

## Behavior changes to be aware of (downstream)
- **OIDC refresh reuse-detection now has a ~10s idempotency window** for the immediately-previous token (benign double-submit returns the same successor; documented tradeoff: a stolen previous token could redeem the successor within that window). Genuine reuse (older/stolen) still revokes the family. This is a deliberate, spec-approved change (spec §3).
- `AvailableMethods` now excludes federation identities whose upstream IdP is disabled (affects `/me/sudo/methods`, login method discovery, and the unlink guard — all correctly).

## ▶ NEXT WORK
Per the tiered backlog (`docs/superpowers/notes/2026-06-08-backend-backlog.md`, [[reference_backend_backlog]]):
- **Tier-1 self-service & admin reads cycle (next):** `GET /me/factors` (stateless Security cards), `PUT /me` (self displayName, no sudo), `GET /accounts/:id/sessions` (admin), `SAML PUT attribute_map/name_id_claim`, AAGUID→provider names, WebAuthn Signal. All code-only, no migration.
- **Then D — password/TOTP enrollment ceremony** (migration + new endpoints + frontend).
- SAML-as-login (ex-"E") is **OUT OF SCOPE** (SAML downstream-only). Breach-list **will not be implemented**.

## Process notes / quirks (these bit this cycle)
- **`dbPool==nil` unit-test seam ≠ real tx coverage.** The smoke is the only gate that exercises real transactions, `FOR UPDATE`, and FK locks. Run it; trust it for tx/lock/FK behavior.
- **gofmt drift is repo-wide and pre-existing** (gofmt-version mismatch): `gofmt -l pkg/` lists many untouched files (`pkg/account`, `pkg/audit`, `pkg/server/handle_me.go`, `cmd/smoke/main.go`, …). Do NOT whole-file reformat when editing these — keep your edited region gofmt-clean and leave the rest (a separate housekeeping pass). A repo-wide `gofmt -w` is its own chore, out of scope for feature work.
- **Stale-gopls after `sqlc generate`** false-positived "missing method CountUsableSignInFederation" / "undefined ErrWouldRemoveLastFactorAuth" constantly. `go build ./... && go vet ./...` exit 0 is authoritative (project memory gopls root-cause). `sqlc` runs via `mise exec -- sqlc generate`.
- NEVER bare `pkill -f prohibitorum` (kills the dev PG — its cmdline contains `prohibitorum-pg`). To stop the dev PG use `pg_ctl stop -D /tmp/prohibitorum-pg`. NO git remote.
