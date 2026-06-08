# Session handover — 2026-06-08

**Branch:** `master` (no remote, no worktree — commit directly). **HEAD:** `ab60be3`. **Tree:** clean.

Read project memory first (`MEMORY.md` + `project_current_state.md` + `reference_backend_backlog.md` + `reference_for_update_audit_fk_deadlock.md`), then this note.

## ▶ RESUME POINT (do this next)
**Execute the Tier-1 plan — it is written, committed, and unstarted.**
- Plan: `docs/superpowers/plans/2026-06-08-self-service-admin-reads.md` (+ `.tasks.json`, 8 tasks, all `pending`).
- Spec: `docs/superpowers/specs/2026-06-08-self-service-admin-reads-design.md`.
- How: `superpowers-extended-cc:subagent-driven-development` (this-session coordinator) or `/superpowers-extended-cc:executing-plans docs/superpowers/plans/2026-06-08-self-service-admin-reads.md` (fresh session). The plan has full backend code + precise FE specs; backend tasks (1,2,4,6) land before their FE consumers (3,5,7); task 8 is independent.
- Scope (5 items, backend + FE, no migration): `PUT /me` (self displayName) · `GET /me/factors` (Security card badges) · admin `GET /accounts/{id}/sessions` + per-session revoke (admin+sudo) · SAML PUT `attribute_map`/`name_id_claim` (+ fix the `authn_requests_signed=$4` alias bug) · admin account attributes editor (FE-only).

## What happened this session (all DONE, committed to master)
1. **Frontend rebuild Spec 3c — DONE** (`1795364`..`4cbb6d3` + fixes): admin Upstream IdPs + Signing keys + Audit log. The rebuilt dashboard reached **full admin parity** with the backend. Handoff: `notes/2026-06-08-frontend-3c-upstream-idps-signing-keys-audit-DONE-handoff.md`. (Live visual review of the whole rebuild still pending — open loop.)
2. **Backend backlog review** — consolidated/tiered every deferred backend item into `notes/2026-06-08-backend-backlog.md` (+ memory `reference_backend_backlog`). **Scope decision:** SAML is **downstream-only**; "SAML-as-login (upstream SAML RP)" is OUT OF SCOPE (removed, not deferred); breach-list WILL-NOT-IMPLEMENT.
3. **Backend security hardening (Tier 2) — DONE** (`5257598`..`13173f8`): SetNX primitive; refresh-rotation idempotency window (no victim-lockout); SAML replay atomic+fail-closed; revoke tx+lockout-guard; passkey-delete serialize; OIDC client-id timing oracle. The smoke caught 2 real bugs unit tests structurally couldn't (a revoke `FOR UPDATE`-vs-audit-FK **deadlock** → fixed by `audit.NewWriter(qtx)`; a stale refresh-reuse smoke assertion → realigned). Handoff: `notes/2026-06-08-backend-hardening-DONE-handoff.md`. New memory: `reference_for_update_audit_fk_deadlock`.
4. **Tier-1 self-service & admin-reads — SPEC + PLAN written, NOT executed** (the resume point above).

## Environment notes (important)
- **The dev/smoke Postgres cluster was REBUILT this session.** `/tmp/prohibitorum-pg` (mise postgres 18.4, :55432) had corrupted relfilenode files (`/tmp` cleanup evicting PGDATA — a hazard of PGDATA-in-`/tmp`). It was rebuilt: `pg_ctl stop` → `rm -rf /tmp/prohibitorum-pg` → `initdb -U tundra --auth=trust` → `pg_ctl start -o "-p 55432"`. It is healthy now (last smoke SMOKE_EXIT=0). The `prohibitorum_dev` DB was wiped — `dev-env.sh` recreates it on the next `mise dev-server`; `mise dev-seed` re-seeds. If the smoke fails on a DB error again, the cluster may have been re-evicted — rebuild it the same way.
- No dev server is currently running (the smoke runner kills it; any `mise dev-server` started for review was stopped).

## Conventions / quirks the next session inherits
- **Workflow:** brainstorm → writing-plans → subagent-driven-development (sonnet impl + spec-then-quality review per task + opus final review) → finishing (done-gate + memory + handoff). Per-task: fix review findings via the same implementer, amend; mark task done + sync `.tasks.json` + small `chore(plan)` commit; keep the tree clean between reviewers.
- **`go build ./... && go vet ./...` exit 0 is authoritative** over stale gopls — which false-positives "undefined"/"missing method" constantly after out-of-editor `sqlc generate` (run via `mise exec -- sqlc generate`). The harness `<new-diagnostics>` showed this ~every backend task this session; always verify with a real build, don't trust the diagnostics.
- **gofmt is repo-wide pre-existing-dirty** (gofmt-version mismatch): `gofmt -l pkg/` lists many untouched files. Keep YOUR edited region gofmt-clean; do NOT whole-file reformat. (A repo-wide `gofmt -w` is its own separate chore.)
- **The `dbPool==nil` unit-test seam does NOT cover real tx/FOR UPDATE/FK behavior — the smoke does.** Run the smoke for anything touching transactions/locks. (This is how the Tier-2 deadlock was caught.) See `reference_for_update_audit_fk_deadlock`.
- **en.ts apostrophe guard** after every en.ts edit: `grep -nP "\x{2018}"` / `:\s*\x{2019}` → both empty.
- **Frontend:** Vue 3 + Vite + Tailwind v4 + shadcn-vue, embedded via `pkg/webui/dist` (go:embed, COMMITTED). After Vue edits run `cd dashboard && npm run build` + `git add pkg/webui/dist` — but only at the DONE-GATE (Vite hashes non-deterministic; discard interim dist dirt with `git checkout -- pkg/webui/dist`). Run git/dist from repo root.
- **Smoke:** detached runner `/tmp/run_v06.sh` (resets the smoke `postgres` DB, builds+runs the server, runs `cmd/smoke`); poll `/tmp/v06.result` for `DONE` + `SMOKE_EXIT=`. NEVER bare `pkill -f prohibitorum` (its cmdline matches the dev PG `-D /tmp/prohibitorum-pg`); stop the PG with `pg_ctl stop -D /tmp/prohibitorum-pg`, kill the server with precise `pkill -f 'go-build.*/prohibitorum'` / `pkill -f 'cmd/prohibitorum'`.

## After Tier-1: remaining backlog (see `notes/2026-06-08-backend-backlog.md`)
- **D — password/TOTP enrollment ceremony** (migration + ceremony endpoints + FE) — next backend cycle after Tier-1.
- AAGUID→provider names + WebAuthn Signal API (own small cycle); email/SMTP channel; downstream SAML SLO front-channel / assertion encryption / AttributeQuery; OIDC PAR/JAR/DPoP/mTLS/DCR/pairwise/device; `009` migration; multi-replica cache coherence; SIEM; HSM/KMS; Playwright e2e.
- **Open loop:** the live browser visual review of the whole rebuilt UI (no screenshot tool — user is the verifier). Worth `mise dev-server` + `mise enroll-admin -- --new` + `mise dev-seed` + a reload-and-react pass.
