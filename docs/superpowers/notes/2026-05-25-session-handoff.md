# Session handoff — end of v0.1.1 (2026-05-25)

> Written so a fresh-context session (post-compaction or new) can resume
> work without re-deriving the chain of reasoning. Future Claude:
> read this once at session start, then `git log --oneline -10` and
> `cat STATUS.md` to confirm the on-disk state matches.

## TL;DR — where we are

v0.1 (multi-protocol rescope skeleton) **and** v0.1.1 (smoke test) are
**done and committed on master**. The codebase compiles clean, all
tests pass, and `cmd/smoke` runs 17/17 + DB-state assertions against a
live dev server. The next planned chunk of work is **v0.2: Password +
TOTP** per `STATUS.md`.

```
last commit: 0231178 docs: STATUS.md + AUDIT.md mark smoke-verified vs smoke-untested
branch:      master
working tree: clean
```

## Memories that govern future work

Read these before doing anything substantial. They are in
`~/.claude/projects/-home-tundra-projects-tundra-prohibitorum/memory/`
and listed in `MEMORY.md`:

1. **`feedback_picotera_decoupling.md`** — when rescoping toward a
   standalone service, strip all upstream-project vocabulary in one
   commit; squash pre-deployment migrations rather than chain cleanup
   migrations.
2. **`feedback_subagent_model_selection.md`** — for serious refactors
   on this plan, default Agent dispatch to **opus** for implementers.
   Sonnet stumbles on scope discipline (Task 1 wrappers, Task 2
   compat shim). Reviewers can stay at sonnet. Never haiku.
3. **`feedback_doc_writing_anchor_to_code.md`** — spec-compliance
   review ≠ truth review. User-facing doc rewrites on partial systems
   must verify every concrete claim against actual code state; the
   doc-vs-code reality audit at `docs/superpowers/specs/2026-05-25-doc-vs-code-reality-audit.md`
   is the template.
4. **`feedback_always_verify_fixes.md`** — every code change must
   produce concrete runtime evidence (DB row, HTTP response, log
   line). Build-green + test-green is not enough. List unverified
   paths explicitly. `cmd/smoke` is the project's verification harness.

## Where v0.1+v0.1.1 landed

### Architecture (per `docs/superpowers/specs/2026-05-24-multi-protocol-rescope-design.md`)

Three-layer split (Approach A):

```
pkg/
  account/                    directory + validators
  credential/
    webauthn/                 includes COSEAlg helper in cose.go
    password/                 v0.2 stubs
    totp/                     v0.2 stubs (includes recovery code)
    pairing/
    enrollment/
  federation/oidc/            v0.3 stubs
  session/                    KV store + PG session writer + middleware
  authn/                      AuthError types, Check, sudo, ratelimit, flow stubs
  protocol/
    oidc/                     OP — discovery + jwks mounted; rest 501 (v0.4)
    saml/                     IdP stubs (v0.5)
  audit/                      writer interface; no-op until v0.2
```

### Migrations (5 total, all applied)

- `001_initial.sql` — `account` (with `attributes jsonb`), `session`,
  `webauthn_credential` (with `cose_alg`, `user_handle`, `uv_initialized`,
  `clone_warning_at`), `enrollment` (with `template_attributes` +
  `expected_upstream_idp_slug`), `credential_event`, `auth_throttle`
- `002_oidc.sql` — `signing_key` (unified for OIDC + SAML, with `use`
  + `not_before`), `oidc_client` (20 columns per audit), `revoked_jti`
- `003_password_totp.sql` — `password_credential`, `totp_credential`
  (with `last_step`, `key_version`), `recovery_code` (with audit
  context columns)
- `004_federation.sql` — `upstream_idp`, `account_identity` (unique on
  `(upstream_iss, upstream_sub)` per OIDC Core §2), `ALTER session ADD
  upstream_idp_id`
- `005_saml.sql` — `saml_sp`, `saml_sp_acs`, `saml_sp_key`,
  `saml_subject_id`, `saml_session`

### HTTP surface mounted in v0.1

- `/api/prohibitorum/auth/*` — login/logout/status
- `/api/prohibitorum/enrollments/{token}/register/{begin,complete}`
- `/api/prohibitorum/me/*` — credentials, sessions, sudo, pairing
- `/api/prohibitorum/accounts/*` (admin)
- `/.well-known/openid-configuration` — real discovery doc; advertises
  v0.4 endpoints; `claims_supported` includes `auth_time, amr, acr,
  attributes` (no picotera leak)
- `/oauth/jwks` — returns `{"keys":[]}` until v0.4 mints signing keys

### NOT mounted in v0.1 (handlers exist but unreachable — return chi 404)

- `/oauth/authorize`, `/oauth/token`, `/oauth/userinfo`, `/oidc/logout`
- `/saml/sso`, `/saml/metadata`, `/saml/slo`

### `cmd/smoke` — the verification harness

In-process virtual-authenticator client (ECDSA-P256/ES256). 17 steps +
DB assertions. Covers:

- Bootstrap enrollment → first session → /me round-trip
- Logout → login → /me round-trip
- Second client B login via same authenticator (concurrent sessions)
- `/me/sessions` list → revoke B's session by ID → confirm 401
- Add second passkey via `/me/credentials/register/{begin,complete}`
- DB assertions: both `cose_alg=-7`, `session` table has ≥3 rows with
  `amr={hwk}`, ≥2 revoked

Smoke-untested code paths (acknowledged in STATUS.md):
- `RevokeAllForAccount` via admin `/accounts/{id}/revoke-sessions`
- `handle_pairing.go` session-issue with `amr=["hwk"]`

## Runtime environment (as of session end)

- **Postgres**: mise-managed PG 18 at `/tmp/prohibitorum-pg`, port
  55432, trust auth as `tundra` user. URL:
  `postgres://tundra@localhost:55432/postgres?sslmode=disable`
- **Migrations**: applied through version 5
- **Dev server**: was running on `:8080` as background task `bo17xuwvt`
  (PID 980775). May or may not still be alive when you read this —
  check with `ss -tlnp | grep :8080`.

To restart the dev server:
```bash
export PROHIBITORUM_DATABASE_URL="postgres://tundra@localhost:55432/postgres?sslmode=disable"
export PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(openssl rand -base64 32)"
export PROHIBITORUM_PUBLIC_ORIGIN="http://localhost:8080"
mise exec -- go run ./cmd/prohibitorum
```

To rerun the smoke (in a second terminal with the same env vars):
```bash
mise exec -- go run ./cmd/smoke -username smokeN-admin -display "SmokeN Admin"
```

## Outstanding gaps and follow-ups

### Smoke-untested code paths

Already documented in STATUS.md §"Smoke-untested runtime paths" and
AUDIT.md notes. Two paths from the v0.1.x fix sequence remain
structurally verified but not exercised:

- `pkg/session.SessionStore.RevokeAllForAccount` (admin endpoint
  `/accounts/{id}/revoke-sessions`). Extending the smoke requires a
  second account + admin-impersonation step.
- `pkg/server/handle_pairing.go:152` session issuer with
  `amr=["hwk"]`. Pairing is a 3-actor ceremony (two unauth + one auth
  client); the `amr` value is a literal constant identical to what's
  already verified.

### Pre-existing oddities (not regressions, just known)

- **`mise.toml` ships `goose = "3.27.0"`** but mise's default registry
  doesn't have goose. Workaround documented in STATUS.md: install
  manually OR change to `aqua:pressly/goose`. Did NOT change
  `mise.toml` in v0.1 per the doc-fix scope discipline; future task
  can fix it.
- **The first two smoke accounts (`smoke-admin` id=1, `smoke2-admin`
  id=2) in the test DB have `cose_alg=0`** because they were
  registered before the COSEAlg helper landed. Don't be surprised
  when inspecting the DB. Smoke runs after commit `e1ecccc` produce
  correct `-7` values.

## Next: v0.2 — Password + TOTP

Per `docs/superpowers/specs/2026-05-24-multi-protocol-rescope-design.md`
§Roadmap and STATUS.md §v0.2, the next chunk is password + TOTP
upstream method:

1. `pkg/credential/password/password.go` — argon2id PHC verify + set
   with re-hash on params upgrade. Replace the `errors.New("TODO(v0.2)")`
   stubs.
2. `pkg/credential/totp/totp.go` — RFC 6238 ±1 drift, `last_step`
   replay protection, AES-GCM at-rest with AAD
   `'totp:'||account_id||':'||key_version`, 10 recovery codes minted at
   confirmation. Replace stubs.
3. New endpoints (per spec §HTTP surface):
   - `POST /api/prohibitorum/auth/password/begin` — username+password →
     partial-session-token (5min KV TTL)
   - `POST /api/prohibitorum/auth/totp/verify` — partial-session-token +
     code → full session
   - `POST /api/prohibitorum/auth/recovery-code/verify` — same shape
   - `POST /api/prohibitorum/me/password/set`
   - `POST /api/prohibitorum/me/totp/begin` / `/verify` (the enrollment
     ceremony to confirm a TOTP secret + receive recovery codes)
4. Audit-throttle table writers (`pkg/credential/password.Store.Verify`
   stamps `auth_throttle` on failures).
5. `pkg/audit.Writer.Record` body becomes real (currently no-op).
   Handlers start emitting `credential_event` rows for register / use
   / fail / clone_warning.
6. `pkg/authn.DisableNonWebAuthnFallbacks` body becomes real (currently
   `errors.New("TODO(v0.2)")`). When a user enrolls WebAuthn, this gets
   called with the new "disable backup?" UI choice. Default-yes per
   spec.
7. **Extend `cmd/smoke`** to drive the password+TOTP flow end-to-end
   (per `feedback_always_verify_fixes.md` discipline). New steps:
   - Set password → set TOTP → confirm with code → password+TOTP login
     end-to-end → recovery-code redemption → throttle observation.

The login orchestrator in `pkg/authn/flow.go` (`AvailableMethods`) also
becomes real in v0.2 — it returns the available login methods given
what's enrolled on the account.

## Where to start v0.2

1. Re-read `docs/superpowers/specs/2026-05-24-multi-protocol-rescope-design.md`
   §"Authentication methods" → "Password + TOTP" and the relevant
   audit report `docs/superpowers/specs/2026-05-24-audit-credentials.md`.
2. Brainstorm via `superpowers-extended-cc:brainstorming` first (the
   spec is the design; the brainstorm is for any ambiguities found
   during implementation).
3. Write a v0.2 plan via `superpowers-extended-cc:writing-plans` into
   `docs/superpowers/plans/2026-05-DD-v0.2-password-totp.md`.
4. Execute via `superpowers-extended-cc:subagent-driven-development`,
   using **opus** for implementers per the model-selection memory.

## Spec / plan / audit artifact index

Authoritative durable docs (do NOT modify lightly):

- `docs/superpowers/specs/2026-05-24-multi-protocol-rescope-design.md`
  — the v0.1+ design spec; source of truth for schema and behavior.
- `docs/superpowers/specs/2026-05-24-audit-{oidc,credentials,saml}.md`
  — the three protocol-vs-design audit reports that drove the v0.1
  schema decisions.
- `docs/superpowers/specs/2026-05-25-doc-vs-code-reality-audit.md` —
  the doc-vs-code reality audit that caught the user-doc hallucinations
  (kept as the template for future doc-rewrite audits).
- `docs/superpowers/plans/2026-05-24-multi-protocol-rescope.md` —
  the v0.1 implementation plan (all 9 tasks marked completed in
  `.tasks.json`).
- `STATUS.md` / `DESIGN.md` / `AUDIT.md` / `INTEGRATION.md` /
  `README.md` — user-facing docs; verified against code state as of
  commit `0231178`.

## Commit log of this session (chronological)

```
0231178  docs: STATUS.md + AUDIT.md mark smoke-verified vs smoke-untested
a1ff8a6  test: extend cmd/smoke + add InsertSession-rollback unit test
ac1df8b  docs: STATUS.md + AUDIT.md reflect cose_alg / session-PG / claims_supported fixes
e1ecccc  fix: cose_alg extraction, session PG persistence, discovery claims_supported
d4a3a9e  feat: cmd/smoke — virtual-authenticator end-to-end smoke client
ba4d45a  docs: doc-vs-code reality fixes + mount /.well-known/openid-configuration + /oauth/jwks
846e7f1  docs: Task 8 polish — GHES attribute_map + dedupe AUDIT row
6cd2421  docs: rewrite DESIGN/STATUS/AUDIT/INTEGRATION/README (Task 8)
b178fc0  refactor: stub-package cleanup per Task 7 code review
99900bc  feat: stub packages + configx extensions for v0.2+ (Task 7)
8108a67  schema: migration 005 — SAML tables (Task 6)
090801b  schema: migration 004 — upstream_idp + account_identity (Task 5)
993b829  schema: migration 003 — password_credential / totp_credential / recovery_code (Task 4)
2cb1b93  fix: validate role at handler layer; delete authn tombstone (Task 3 follow-up)
0047f9f  schema: rewrite migrations 001+002, sqlc queries, contract, handlers (Task 3)
cc5ce41  refactor: drop unreachable RPDisplayName fallback (Task 2 follow-up)
79efd9d  refactor: drop WebAuthnRPID compat shim from configx (Task 2 follow-up)
e5e2b32  refactor: cosmetic decoupling from picotera vocabulary (Task 2)
c7ab580  refactor: drop forwarding-wrapper error files (Task 1 follow-up)
a240440  refactor: atomic package reorganization (Task 1)
ed81fb0  chore: establish baseline — go mod tidy, sqlc generate, logx fix (Task 0)
50fc730  docs: implementation plan for the skeleton commit
c448f6b  docs: spec — merge audit findings from OIDC/credentials/SAML reviews
26abbfa  docs: spec — fold picotera strip-out into the skeleton commit
9fa6875  docs: multi-protocol rescope spec
3d79583  v0.1: skeleton + identity extraction from picotera (pre-rescope snapshot)
```
