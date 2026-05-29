# Session handoff — v0.4 downstream OIDC OP COMPLETE (all 17 tasks + audit)

> Future Claude: v0.4 is fully shipped, audited, and smoke-verified. This
> doc records the end state. `git log --oneline -35` shows the v0.4 range
> (354b4dd → 1747da7). The next chunk is a NEW version (v0.5+), not a
> continuation of v0.4 — re-brainstorm/spec it fresh.

## TL;DR — v0.4 is DONE

**Downstream OpenID Connect Provider — fully implemented, reviewed, audited,
smoke-verified end-to-end.** Authorization Code + PKCE (S256), `/oauth/token`
(auth_code + refresh with rotation/reuse-detection/family-revocation),
`/userinfo`, `/oauth/introspect` (RFC 7662), `/oauth/revoke` (RFC 7009),
`/oidc/logout` (RP-Initiated 1.0), real JWKS, expanded discovery, plus
`signing-key` and `oidc-client` CLIs.

- **All 17 plan tasks (0–16)** executed via subagent-driven development
  (opus implementers → sonnet reviewers → fix loops). Each task: build+vet+test
  green, then a spec+quality review, fixes applied, `.tasks.json` synced.
- **Smoke GREEN end-to-end**: `cmd/smoke` steps 70–87 drive the full code flow
  against live PG + a real dev server. Final: `45/45 (v0.2) + 46–69 (v0.3) +
  70–87 (v0.4)` all pass. Re-run: `setsid bash /tmp/run_v04.sh` then
  `cat /tmp/v04.result` (or see "How to re-run" below).
- **Post-implementation audit**: parallel 3-lens (crypto/protocol/race) + a
  deep second pass (integration/data-integrity/schema-drift). **No Critical.**
  The deep pass earned its keep — it found a High (disabled-session nil-deref
  panic on `/authorize`) and a Medium (unbounded `revoked_jti`) the
  schema-resetting smoke structurally could not catch. Both fixed. Full record
  in `AUDIT.md` → "v0.4 post-implementation audit (done)".

```
HEAD: 1747da7 docs(audit): record v0.4 post-implementation audit
branch: master   working tree: clean
go build ./... ✓   go vet ./... ✓   go test ./... ✓   smoke ✓
```

## What shipped (anchors)

- Handlers: `pkg/protocol/oidc/{oidc,authorize,token,refresh,userinfo,introspect,revoke,logout,errors,claims,client,codes,keys,jwt,keygen,clientgen}.go`.
- Routes mounted in `pkg/server/server.go` (~line 286): `/oauth/{authorize,token,userinfo,introspect,revoke,jwks}`, `/oidc/logout`, discovery.
- CLIs in `cmd/prohibitorum/main.go`: `signing-key generate [--activate|--retire]`, `oidc-client {create,list}`.
- Schema: `account.oidc_subject` (uuid, DEFAULT gen_random_uuid), `oidc_client.require_consent`, `signing_key`, `revoked_jti`, `session.{amr,acr,auth_time}` — all in migrations 001/002 (pre-deployment squash).
- Storage (D8): auth codes + refresh tokens in KV; access tokens stateless RFC 9068 JWT revoked via `revoked_jti` PG denylist; ID tokens stateless JWT.
- Docs: STATUS.md (v0.4 section), AUDIT.md (OIDC OP rows → smoke-verified + audit record), INTEGRATION.md (OIDC OP curl section).

## Known-deferred (carried forward — see AUDIT.md "Accepted / deferred")

Not bugs, tracked for a later version:
- `prompt=login` / `max_age` not honored (no step-up/forced-reauth); consent UI
  deferred (`require_consent` fails closed with `consent_required`).
- `oidc_client.require_pkce` / `allowed_code_challenge_methods` columns stored
  but not consulted (S256-required is hardcoded, fail-closed).
- `none` advertised for introspect/revoke; public clients can introspect/revoke
  their OWN tokens (ownership-checked). Decide if public-client introspection
  should be disallowed.
- Client-id timing oracle (unknown-client returns before argon2 verify) — Low.
- Concurrent code-replay during the mint window can escape family-revoke
  (single-use still holds; PKCE protects passive interceptors); concurrent
  refresh rotation race is non-immortalizing. Both need a KV compare-and-swap
  the `kv.Store` interface doesn't expose.

## Runtime environment + smoke discipline (unchanged from v0.3/v0.4 — these bite)

- **PG**: PG 18.4 on `:55432`, data dir `/tmp/prohibitorum-pg`, user `tundra`,
  db `postgres`, no password. DSN
  `postgres://tundra@localhost:55432/postgres?sslmode=disable`.
- **NEVER `pkill -f 'prohibitorum'`** — matches the Postgres `-D /tmp/prohibitorum-pg`
  and kills the DB. Use precise patterns: `pkill -9 -f 'go-build.*/prohibitorum'`,
  `pkill -9 -f 'cmd/prohibitorum'`, `pkill -9 -f 'cmd/smoke'`. Restart PG:
  `rm -f /tmp/prohibitorum-pg/postmaster.pid && /home/tundra/.local/share/mise/installs/postgres/18.4/bin/pg_ctl -D /tmp/prohibitorum-pg -l /tmp/pg.log -o "-p 55432" start`.
- **gopls `<new-diagnostics>` LIE** about cross-file/cross-commit edits in this
  repo — they repeatedly reported `OidcSubject`/`RequireConsent`/`GetAccountByOIDCSubject`/
  `InsertRevokedJTI`/cross-file-method "undefined" on code that builds clean.
  After every subagent, trust `mise exec -- go build ./...` (exit 0) + `go vet`,
  NOT the diagnostics. The mise `goose@3.27.0` WARN + "Did you mean? go goss
  choose" lines are permanent harmless noise.
- **Smoke runner** (`/tmp/run_v04.sh`): fully-detached `setsid bash` writing to
  `/tmp/v04.result` (the Bash tool SIGPIPEs on long pipelines / reaps nohup
  servers). Pattern: precise-kill → `DROP SCHEMA public CASCADE; CREATE SCHEMA
  public` → export `PROHIBITORUM_{DATABASE_URL,DATA_ENCRYPTION_KEY_V1,PUBLIC_ORIGIN}`
  → `setsid go run ./cmd/prohibitorum` → poll `/.well-known/openid-configuration`
  → confirm `:8080` listener is yours → `go run ./cmd/smoke -username
  smoke-v04-admin` → read `/tmp/v04.result`. The smoke shells out to the CLIs
  (they inherit the exported env) and asserts DB state via PROHIBITORUM_DATABASE_URL.
- DEK: `PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(openssl rand -base64 32)"`,
  `PROHIBITORUM_PUBLIC_ORIGIN=http://localhost:8080` (this is also `OIDC.Issuer`).

## Conventions (project-wide)

- **Master branch** — user authorized master-branch work for the whole project.
- opus implementers, sonnet reviewers, never haiku.
- Pre-deployment squash: amend migrations in place; don't chain cleanup migrations.
- No wrapper/forwarding funcs; share via existing helpers (reused `password.VerifyRaw`,
  `getRedirect`, etc.).
- Verify at runtime: `cmd/smoke` is the integration gate; unit-green ≠ done. The
  deep audit pass found a High the smoke couldn't (stateful disabled-mid-session
  transition) — keep doing the deep pass.
- Docs anchor to code: distinguish smoke-verified / unit-tested-only / designed.

## Artifact index

- Spec: `docs/superpowers/specs/2026-05-29-v0.4-oidc-op-downstream-design.md` (D1–D8).
- Plan: `docs/superpowers/plans/2026-05-29-v0.4-oidc-op-downstream.md` + `.tasks.json` (17/17 completed).
- Prior handoff: `docs/superpowers/notes/2026-05-29-session-handoff.md` (Tasks 0+1).
- User-facing: `STATUS.md` / `AUDIT.md` / `INTEGRATION.md` / `DESIGN.md` / `README.md`.

## What's next

v0.4 is a clean stopping point. The multi-protocol rescope spec
(`docs/superpowers/specs/2026-05-24-multi-protocol-rescope-design.md`) is the
master roadmap; v0.5+ (e.g. SAML SP/IdP per the AUDIT Pattern C references, or
the deferred OIDC items above) would be the next chunk — brainstorm + spec it
fresh rather than extending the v0.4 plan.
