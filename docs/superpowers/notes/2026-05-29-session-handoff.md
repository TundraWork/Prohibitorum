# Session handoff — v0.3 fully shipped + audited; v0.4 OIDC OP in progress (Tasks 0+1 done)

> Future Claude: read this once, then `git log --oneline -20` and
> `cat docs/superpowers/plans/2026-05-29-v0.4-oidc-op-downstream.md.tasks.json`
> to confirm on-disk state, then resume v0.4 at Task 2.

## TL;DR — where we are

**v0.3 upstream OIDC federation: DONE, audited, hardened, smoke-verified.**
12-task plan executed + a deep 3-auditor pass (crypto/race-logic/standards)
+ a second deep audit (integration / data-integrity / schema-drift) whose
findings were all fixed. invite_only token-bearing redemption shipped. Final
smoke green **69/69** against live PG + dev server + in-process mock OP.

**v0.4 downstream OIDC OP: IN PROGRESS — 2 of 17 tasks done.**
- Spec: `docs/superpowers/specs/2026-05-29-v0.4-oidc-op-downstream-design.md` (decisions D1–D8).
- Plan: `docs/superpowers/plans/2026-05-29-v0.4-oidc-op-downstream.md` + `.tasks.json`.
- Executing via **subagent-driven-development** (opus implementers, sonnet reviewers, combined spec+quality review for mechanical tasks, two-stage for complex; per-task fix loops; `.tasks.json` synced after each).

```
last commit: 3a53963 feat(oidc): RS256 JWT sign/verify + signing-key cache + JWKS builder
branch:      master
working tree: docs/.../2026-05-29-v0.4-oidc-op-downstream.md.tasks.json modified
             (Task 1 marked completed — COMMIT THIS before/with resuming; see below)
```

## v0.4 task status (native task IDs ↔ plan task numbers)

The native TaskList IDs map to plan task numbers as **id = plan# + 30**:
- #30 = Task 0 ✅ completed (commit `354b4dd`)
- #31 = Task 1 ✅ completed (commit `3a53963`)
- #32 = Task 2 ⏳ pending (signing-key generate CLI) — **resume here**
- #33 = Task 3 (client.go), #34 = Task 4 (claims.go), #35 = Task 5 (codes.go),
  #36 = Task 6 (refresh.go store), #37 = Task 7 (errors+discovery+Provider widen)
- #38 = Task 8 (/authorize), #39 = Task 9 (/token auth_code), #40 = Task 10 (/token refresh),
  #41 = Task 11 (userinfo+introspect+revoke), #42 = Task 12 (/oidc/logout)
- #43 = Task 13 (oidc-client CLI), #44 = Task 14 (server wiring), #45 = Task 15 (smoke mock RP),
  #46 = Task 16 (docs)

Dependencies are already wired in the native TaskList AND in `.tasks.json` `blockedBy`.
After Task 0+1, the **ready (unblocked) tasks** are: **Task 2** (needs 1 ✅),
**Task 3, 4, 5, 6, 7** (all need only Task 0 ✅). So a resuming session can do
Task 2 next, or any of 3–7 (they're parallelizable but subagent-driven runs them
one at a time — go in ID order: 2, 3, 4, 5, 6, 7, then 8+).

## FIRST ACTION on resume

The working tree has `…v0.4-oidc-op-downstream.md.tasks.json` modified (Task 1 →
completed). Either commit it standalone or fold it into the Task 2 commit:
```bash
git add docs/superpowers/plans/2026-05-29-v0.4-oidc-op-downstream.md.tasks.json
git commit -m "chore: mark v0.4 Task 1 complete in tasks.json"
```
Then re-enter execution:
```
/superpowers-extended-cc:subagent-driven-development
# argument: execute docs/superpowers/plans/2026-05-29-v0.4-oidc-op-downstream.md — Tasks 0+1 done, resume at Task 2 (#32)
```

## The execution rhythm (established, keep it)

Per task: TaskUpdate #N in_progress → dispatch **opus** implementer with the FULL
task text from the plan (don't make them read the plan file; paste it, with the
reference code) + scene-setting context → handle their status → dispatch a
**sonnet** reviewer (combined spec+quality for mechanical 1–2-file tasks like the
CLIs/stores; separate two-stage only if a task is large) → fix loop until clean →
TaskUpdate #N completed → edit `.tasks.json` (set that task `"status":"completed"`
+ bump `lastUpdated`) → next. Always `git log --oneline -1` after a subagent to
confirm it committed.

## v0.4 design decisions already locked (don't re-litigate)

- **D5 Approach B**: hand-rolled OP in `pkg/protocol/oidc`, `go-jose/v4` for JWT
  only. NOT the zitadel `pkg/op` framework.
- **D6 sub** = `account.oidc_subject` (uuid, DB default `gen_random_uuid()`) — Task 0
  landed the column; `claims.go` (Task 4) reads it; sqlc maps it to `pgtype.UUID`
  (format to canonical string for the claim).
- **D8 storage**: auth codes + refresh tokens in **KV** (codes single-use w/ replay→family-revoke
  marker; refresh opaque w/ family record + rotation + reuse-detection). Access
  tokens = stateless RFC 9068 JWT, revoked via the existing `revoked_jti` PG table.
  ID tokens = stateless JWT.
- **D2 consent**: auto-approve; `oidc_client.require_consent` flag reserved (Task 0
  landed the column), `/authorize` returns `consent_required` when true (no UI till v0.6).
- **D3 rate limits**: per-`client_id` / per-`account_id` (NOT per-IP — v0.3 M5 stripped
  IP limits). Reuse `s.rateLimiter` with keys like `oidc:token:client:<id>`.
- **D7 no-session /authorize**: 302 to `Issuer + /login?return_to=<authz URL>`;
  `prompt=none` → `login_required` (no redirect).
- Audit factor = `audit.FactorOIDCClient` ("oidc_client"), structured reasons per event.

## Foundations already built (Tasks 0+1) — reuse, don't rebuild

- **Schema**: `account.oidc_subject`, `oidc_client.require_consent` (both via amended
  migrations 001/002 — pre-deployment squash). OP queries in `db/queries/oidc.sql`,
  generated into `pkg/db/{oidc.sql.go,account.sql.go,models.go,querier.go}`:
  `GetOIDCClient, InsertOIDCClient, ListOIDCClients, GetAccountByOIDCSubject,
  GetActiveSigningKey, ListActiveSigningKeys, InsertSigningKey, DeactivateSigningKeys,
  RetireSigningKey, InsertRevokedJTI, IsJTIRevoked, PruneExpiredRevokedJTI`.
- **`pkg/protocol/oidc/keys.go`**: `keyCache` (5-min refresh), `publicJWK`,
  `jwkThumbprint` (RFC 7638 kid), `parseRSAPrivatePEM` (PKCS#1+PKCS#8), `cachedKey`,
  `signingKeyQueries` interface.
- **`pkg/protocol/oidc/jwt.go`**: `(*Provider).signJWT(ctx, claims, typ)` +
  `verifyJWT(ctx, token)` via go-jose/v4. **go-jose/v4 API (v4.1.4) confirmed**:
  `jose.NewSigner(SigningKey{RS256, key}, (&SignerOptions{}).WithType(ContentType(typ)).WithHeader("kid", kid))`;
  `jwt.Signed(signer).Claims(m).Serialize()`; `jwt.ParseSigned(token, []jose.SignatureAlgorithm{jose.RS256})`
  (the 2-arg allowlist is what rejects alg:none/HS256 at parse time — Task 2+ MUST
  keep using it); `parsed.Headers[0].KeyID`; `parsed.Claims(pub, &dest)`.
- **`Provider` struct** (`oidc.go`) has a `keys *keyCache` field, **nil until Task 7
  widens `New(...)`**. Task 7 changes `New(cfg)` → `New(cfg, queries, kvStore,
  sessionStore, auditWriter, rateLimiter)` and sets `keys: newKeyCache(queries)`.
  Until then, tests construct `&Provider{keys: newKeyCache(fake)}` directly.
- **Reusable test helper**: `testSigningKeyRow(t) (db.SigningKey, *rsa.PrivateKey)`
  in `keys_test.go` (package-internal) — Tasks 9–12 use it to mint test keys.
- The Task-0 blank `_ "go-jose/v4"` import in oidc.go was **removed** in Task 1
  (jwt.go imports it for real → still a direct dep).

## Runtime environment + quirks (CRITICAL — these bit hard last session)

- **Postgres**: PG 18.4 on `:55432`, data dir `/tmp/prohibitorum-pg`, user `tundra`,
  db `postgres`, no password. DSN:
  `postgres://tundra@localhost:55432/postgres?sslmode=disable`.
- **NEVER `pkill -f 'prohibitorum'`** — the Postgres process's `-D /tmp/prohibitorum-pg`
  matches that pattern and you'll kill the DB. Use precise patterns:
  `pkill -9 -f 'go-build.*/prohibitorum'`, `pkill -9 -f 'cmd/prohibitorum'`,
  `pkill -9 -f 'cmd/smoke'`. To restart PG if killed:
  `rm -f /tmp/prohibitorum-pg/postmaster.pid && /home/tundra/.local/share/mise/installs/postgres/18.4/bin/pg_ctl -D /tmp/prohibitorum-pg -l /tmp/pg.log -o "-p 55432" start`.
- **The dev server is a compiled binary** (`go run` spawns a `go-build/.../prohibitorum`
  child that owns `:8080`). A `pkill -f 'go run …'` does NOT kill it. Check the real
  listener: `(ss -ltnp|grep :8080)`. A stale server with the wrong DEK silently serves
  and breaks crypto — always confirm the listener PID is yours after starting.
- **gopls diagnostics lie about cross-file/cross-commit edits** — it repeatedly reported
  "p.keys undefined" / "field X undefined" / "should be direct" on code that builds
  clean. After every subagent, trust `mise exec -- go build ./...` (filtered) over the
  `<new-diagnostics>` blocks. The mise `goose@3.27.0` registry WARN is permanent noise.
- **sqlc**: `mise exec sqlc -- sqlc generate` (sqlc pinned 1.30.0 in mise.toml).
- **goose binary** (for manual migration apply, e.g. before smoke):
  `/home/tundra/.local/share/mise/installs/go/1.26.3/bin/goose -dir db/migrations postgres "<DSN>" up`.
  Or just let the dev server / `migrations.UpWithResult` apply them on boot.
- **Smoke runner discipline** (Task 15): run it as a fully-detached `setsid bash script`
  writing to a result file you Read (the Bash tool SIGPIPEs / exit-144s on long
  chained pipelines, and backgrounded `nohup` servers get reaped when the tool's shell
  exits). Schema-reset → start ONE server (confirm listener) → poll readiness → run
  smoke → read result file. See last session's `/tmp/v03_final.sh` pattern.
- **DEK**: `PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(openssl rand -base64 32)"`,
  `PROHIBITORUM_PUBLIC_ORIGIN=http://localhost:8080` (this is also `OIDC.Issuer` base).

## Conventions (project-wide, don't re-derive)

- **Master branch** — user has authorized master-branch work for the whole project.
- **opus implementers, sonnet reviewers, never haiku** (`feedback_subagent_model_selection`).
- **Pre-deployment squash**: amend existing migrations in place; don't chain cleanup
  migrations (`feedback_picotera_decoupling`).
- **No wrapper/forwarding funcs**: share via private helpers with flag params.
- **Verify at runtime**: cmd/smoke is the integration gate; unit-test-green ≠ done
  (`feedback_always_verify_fixes`). Last session a tx-scoped-audit bug passed unit tests
  (pool=nil → no rollback) and was ONLY caught by the live smoke — keep that lesson.
- **Docs anchor to code** (`feedback_doc_writing_anchor_to_code`): every concrete claim
  verifies against actual file:line.

## After Task 16: post-implementation audit

Per the v0.2/v0.3 discipline, after all 17 v0.4 tasks land, dispatch a parallel
3-auditor pass (crypto + protocol/standards + race/logic) against the v0.4 commit
range. Focus per the plan's final section: PKCE + code single-use/replay, refresh
rotation/reuse atomicity, JWT alg allowlist, RFC 9068 conformance, `revoked_jti`
denylist races, client-auth constant-time, logout session-revocation, error/enumeration
safety. Then a deep second pass (integration / data-integrity / schema-drift) — the
v0.3 deep pass found a Critical the first pass missed, so it's worth it.

## Artifact index

- Specs: `docs/superpowers/specs/2026-05-29-v0.4-oidc-op-downstream-design.md` (v0.4),
  `…/2026-05-28-v0.3-upstream-oidc-federation-design.md` (v0.3),
  `…/2026-05-24-multi-protocol-rescope-design.md` (master).
- Plans: `docs/superpowers/plans/2026-05-29-v0.4-oidc-op-downstream.md` + `.tasks.json` (v0.4, 2/17),
  `…/2026-05-28-v0.3-upstream-oidc-federation.md` + `.tasks.json` (v0.3, 12/12).
- Prior handoff: `docs/superpowers/notes/2026-05-28-session-handoff.md` (end of v0.2/audit/recovery, start of v0.3).
- User-facing: `STATUS.md` / `AUDIT.md` (the OIDC OP §"OIDC OP downstream" table IS the
  v0.4 conformance checklist — read it) / `INTEGRATION.md` / `DESIGN.md` / `README.md`.

## Memories that govern future work

In `~/.claude/projects/-home-tundra-projects-tundra-prohibitorum/memory/` (indexed in
`MEMORY.md`): `feedback_picotera_decoupling`, `feedback_subagent_model_selection`,
`feedback_doc_writing_anchor_to_code`, `feedback_always_verify_fixes`,
`project_current_state` (this handoff is its pointer target).
