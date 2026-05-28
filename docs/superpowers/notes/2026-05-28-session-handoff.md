# Session handoff — v0.2 audit + recovery rewrite shipped; v0.3 federation in progress (Tasks 0-1 done)

> Written so a fresh-context session can resume work without re-deriving
> the chain of reasoning. Future Claude: read this once at session start,
> then `git log --oneline -15` and `cat STATUS.md` to confirm on-disk
> state matches.

## TL;DR — where we are

**Done since the last handoff (2026-05-25):**

- **v0.2 Password + TOTP** — 10-task plan executed via subagent-driven development. Includes argon2id PHC, RFC 6238 TOTP with AES-GCM at-rest secrets, 80-bit recovery codes, exponential-backoff throttle, sudo extension to 3 methods, 5 sensitive /me/* endpoints, cmd/smoke extended to 45 steps + DB assertions. All green against live PG + dev server.
- **v0.2 security audit** — Three parallel opus auditors (crypto + race/logic + standards) produced ~23 findings (4 Critical, 4 High, 6 Medium, 7 Low, 2 Info). All Critical + High closed in code. Medium/Low closed in code or documented as deferred. Bundle commits: 8f6b4fd, bc1fb97, a911e85.
- **Recovery ceremony redesign** — Replaced `recovery_code` as a sudo step-up (NIST SP 800-63B-4 §5.2 takeover risk) with a forced re-enrollment ceremony. New endpoints `/auth/recovery/totp/begin` and `/auth/recovery/totp/verify`; `/auth/recovery-code/verify` no longer issues a session. Commit a9910eb + follow-ups.
- **v0.3 design spec written** — Commit 5c1b9bd. 14 decisions resolved (D1-D14). Includes per-IdP `require_verified_email`, three provisioning modes, AMR pass-through, `/me/identities` last-sign-in-method safety, mix-up + replay defenses, AAD format for upstream client secrets, smoke extension plan with mock OP.
- **v0.3 implementation plan written** — Commit ae75d33. 12 tasks, ~2676 lines. Tasks 0-1 done; Tasks 2-11 pending.
- **v0.3 Task 0 done** (commit dba441a): added `require_verified_email` column via migration 006, added `github.com/zitadel/oidc/v3 v3.47.5` direct dep, regenerated sqlc. Pre-deployment baseline considered, new migration chosen over amending 004.
- **v0.3 Task 1 done** (commit df114e7): `pkg/federation/oidc/secret.go` AES-256-GCM helper for `client_secret_enc` with AAD `'upstream_idp:'+id+':'+key_version`. 5/5 tests pass. Mirrors `pkg/credential/totp/aead.go`.

**Current state:**

```
last commit: df114e7 feat(federation/oidc): AES-256-GCM helper for client_secret_enc
branch:      master
working tree: clean (after the next commit, which is this handoff)
```

## What's next — v0.3 Tasks 2–11

Read `docs/superpowers/plans/2026-05-28-v0.3-upstream-oidc-federation.md` for the full plan. Tasks status from `.tasks.json`:

- **Task 0** ✅ — Schema + dep + configx. Commit dba441a.
- **Task 1** ✅ — AES-GCM helper. Commit df114e7.
- **Task 2** ⏳ — In-process mock OIDC OP (`cmd/smoke/mockop/`). ~250 lines. ES256 signing, PKCE verification, test hooks for claims/error/iss-override. Blocked only by Task 0 (done).
- **Task 3** — OIDC RP client wrapper (`pkg/federation/oidc/client.go`). Wraps zitadel/oidc/v3. Blocked by 1 + 2.
- **Task 4** — State KV + Mode policies (`pkg/federation/oidc/{state,modes}.go`). Blocked by 1 + 3.
- **Task 5** — Federator orchestration — replace stubs in `pkg/federation/oidc/federation.go`. Blocked by 3 + 4.
- **Task 6** — Authn flow extension + 8 new error constructors. Blocked by 5.
- **Task 7** — Federation HTTP endpoints (`pkg/server/handle_federation.go`). Blocked by 6.
- **Task 8** — `/me/identities` HTTP endpoints. Blocked by 6.
- **Task 9** — Server wiring. Blocked by 7 + 8.
- **Task 10** — cmd/smoke v0.3 extension. Blocked by 9.
- **Task 11** — Docs. Blocked by 10.

After Task 11 lands, run a parallel three-auditor pass (crypto + race/logic + standards) against the v0.3 commit range — same discipline that produced ~23 v0.2 findings.

## How to resume

```
/superpowers-extended-cc:executing-plans docs/superpowers/plans/2026-05-28-v0.3-upstream-oidc-federation.md
```

OR (preferred — user already chose subagent-driven for this plan):

```
/superpowers-extended-cc:subagent-driven-development
# argument: execute docs/superpowers/plans/2026-05-28-v0.3-upstream-oidc-federation.md
```

The fresh session will read the plan, see Tasks 0+1 marked completed in
`.tasks.json`, and dispatch Task 2 next.

## Important context for the resuming session

### Conventions (don't re-derive these)

- **Master branch** — user has explicitly approved working on master across the entire project lifecycle. Per project memory `MEMORY.md`.
- **Subagent model selection** — per `feedback_subagent_model_selection.md`, **opus for implementers**, sonnet OK for reviewers, never haiku.
- **Verify fixes at runtime** — per `feedback_always_verify_fixes.md`, cmd/smoke 45/45 (or more after Task 10) is the integration gate.
- **No wrapper / forwarding files** — per `feedback_picotera_decoupling.md`, Begin/BeginPreservingRecovery and similar variant-pair functions MUST share implementation via private helpers with flag parameters, not be thin forwarding wrappers.
- **Audit-driven hardening discipline** — per the v0.2 cycle, every behavior change has a unit test; race-prevention claims have race tests; "smoke-verified" claims in docs must map to actual smoke steps.

### Quirks specific to where v0.3 left off

1. **Blank import in `pkg/federation/oidc/federation.go`.** Task 0's implementer added `_ "github.com/zitadel/oidc/v3/pkg/client/rp"` to keep the dep from being trimmed by `go mod tidy`. There's a comment in the file marking it for removal in Task 5 when the real RP client wrapper introduces actual usage. **Task 3's implementer should NOT remove it** (Task 3 creates `client.go` which will import the package properly; Task 5 does the cleanup).

2. **Postgres state.** `/tmp/prohibitorum-pg` was re-bootstrapped during Task 0 because `/tmp` is volatile. PG 18 is running on port 55432 as of the last task. If the fresh session finds it dead:
   ```bash
   PGDATA=/tmp/prohibitorum-pg pg_ctl start  # or `mise exec -- pg_ctl ...`
   ```
   Then re-apply migrations:
   ```bash
   goose -dir db/migrations postgres "postgres://tundra@localhost:55432/postgres?sslmode=disable" up
   ```

3. **Dev server NOT running.** v0.3 doesn't need the dev server until Task 9+ (HTTP handlers + server wiring + smoke). Don't bother starting it until then.

4. **`mise.toml` goose@3.27.0 pin** — still doesn't resolve in mise's default registry. Workaround: `aqua:pressly/goose 3.27.1` via `~/.config/mise/config.toml` (already configured locally). Documented in v0.1.1 handoff; unchanged.

5. **Pre-existing TOTP-step-boundary flakiness** — `TestSudoComplete_PasswordTOTPSuccess` occasionally fails under full-test parallelism (period-boundary timing) but passes when run in isolation. Not introduced by current work. If a future implementer hits it, re-run; or pin time via the existing `s.now` injection hook.

6. **v0.3 Task 0's `require_verified_email` default.** New `upstream_idp` rows get `true` by default. Tests that insert via raw SQL must set it (or omit it to rely on the default). Tests that construct `db.InsertUpstreamIDPParams` must include the field.

### Audit history pointer

The v0.2 audit findings and their resolution status are summarized in the
final session message that produced commit a911e85. AUDIT.md (in the
project root) carries the post-implementation audit table. Future
audit cycles should produce similar artifacts for v0.3.

### Recovery ceremony reference

The recovery ceremony rewrite (commit a9910eb) introduced new endpoints
that aren't yet in INTEGRATION.md's full surface map. STATUS.md describes
them. Don't be surprised by the dichotomy between `/auth/recovery/totp/begin`
(no session yet, recovery_session_token) and `/me/totp/begin` (logged-in,
sudo-gated).

## Runtime environment (as of session end)

- **Postgres:** PID running on `:55432`, `/tmp/prohibitorum-pg`.
- **Dev server:** not running.
- **Test status:** `go test ./...` clean, `go build ./...` clean, `go vet ./...` clean as of df114e7.

## Commit log of v0.3 + audit + recovery work (chronological)

```
df114e7  feat(federation/oidc): AES-256-GCM helper for client_secret_enc
dba441a  schema(federation): add require_verified_email column; add zitadel/oidc/v3 dep
ae75d33  docs: v0.3 upstream OIDC federation implementation plan + tasks.json
5c1b9bd  docs: v0.3 upstream OIDC federation design spec
49afbbd  docs(server): correct recovery_session_token comment — 128 bits, not 32 bytes
443d3b8  docs: align recovery design with shipped behavior (defer wipe + single-use on verify)
a9910eb  feat(security): recovery ceremony — drop recovery_code sudo, add forced re-enrollment
b555438  docs: amend recovery design — defer recovery-code wipe to /verify
a911e85  fix(security): low-severity hardening + deployment notes
bc1fb97  fix(security): atomic recovery-code mint + audit-revoke on wipe
8f6b4fd  fix(security): atomic single-use tokens + TOTP race + throttle race + step-2 disabled + revoke order + enum-oracle close
cd8a531  docs: draft recovery ceremony design (deferred — security hardening of recovery_code sudo)
918c0b6  chore: mark v0.2 tasks.json complete — 10/10 tasks shipped + smoke-verified
baea02c  docs: fix v0.2 doc/code mismatches — recovery amr + sudo/begin status
ff6011b  docs: v0.2 Password+TOTP shipped — smoke-verified 45/45
5ccf3fe  test(smoke): v0.2 extension — password+TOTP+sudo+throttle+revoke (45 steps)
c958838  fix(server): distinguish DB error from missing TOTP row in regenerate; drop stale test comment
d71b4a5  feat(server): /me/{password/set, totp, recovery-codes/regenerate, auth/revoke-password-totp}
8eb5449  refactor(session,server): drop unused SudoTTL constant + redundant sudo handler guards
c0b0e11  feat(server): sudo accepts webauthn|password_totp|recovery_code
a488fd5  fix(server): reject disabled accounts at /auth/password/begin
ff3a238  feat(server): /auth/password/begin + /auth/totp/verify + /auth/recovery-code/verify
3f1f362  test(authn): cover DisableNonWebAuthnFallbacks delete-error paths
731664c  feat(authn): real AvailableMethods + DisableNonWebAuthnFallbacks
... (Tasks 0-3 of v0.2 above this; see prior handoff for full v0.2 log)
```

## Spec / plan / handoff artifact index

Durable durable docs (do NOT modify lightly):

- `docs/superpowers/specs/2026-05-24-multi-protocol-rescope-design.md` — the v0.1+ master spec; source of truth for schema and behavior across all versions.
- `docs/superpowers/specs/2026-05-24-audit-{oidc,credentials,saml}.md` — the three protocol-vs-design audit reports.
- `docs/superpowers/specs/2026-05-25-v0.2-password-totp-design.md` — v0.2 delta on master spec.
- `docs/superpowers/specs/2026-05-25-doc-vs-code-reality-audit.md` — v0.1.1 doc-vs-code reality audit template.
- `docs/superpowers/specs/2026-05-27-recovery-ceremony-design.md` — recovery ceremony rewrite spec (the document was amended twice to align with shipped behavior; latest at commit 443d3b8).
- `docs/superpowers/specs/2026-05-28-v0.3-upstream-oidc-federation-design.md` — v0.3 design.
- `docs/superpowers/plans/2026-05-24-multi-protocol-rescope.md` — v0.1 plan (completed).
- `docs/superpowers/plans/2026-05-25-v0.2-password-totp.md` + `.tasks.json` — v0.2 plan (10/10 completed).
- `docs/superpowers/plans/2026-05-28-v0.3-upstream-oidc-federation.md` + `.tasks.json` — v0.3 plan (2/12 completed).
- `docs/superpowers/notes/2026-05-28-v0.2-deployment-notes.md` — v0.2 operational deployment caveats from the audit Bundle 3.
- `docs/superpowers/notes/2026-05-25-session-handoff.md` — previous handoff (end of v0.1.1).
- `STATUS.md` / `DESIGN.md` / `AUDIT.md` / `INTEGRATION.md` / `README.md` — user-facing docs.

## Memories that govern future work

Read these before doing anything substantial. They are in
`~/.claude/projects/-home-tundra-projects-tundra-prohibitorum/memory/`
and indexed in `MEMORY.md`:

1. **`feedback_picotera_decoupling.md`** — strip-and-squash discipline; no wrapper / forwarding files.
2. **`feedback_subagent_model_selection.md`** — opus for implementers on this plan.
3. **`feedback_doc_writing_anchor_to_code.md`** — every concrete doc claim must verify against actual code state.
4. **`feedback_always_verify_fixes.md`** — every code change produces runtime evidence; cmd/smoke is the harness.

`project_current_state.md` points at this handoff doc.
