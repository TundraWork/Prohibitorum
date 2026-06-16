# RBAC execution handover — 2026-06-16

Mid-execution handover for the **RBAC — "control which users each app is authorized
for"** feature. Another agent will continue the subagent-driven execution from here.

## Canonical documents (read these first)

- **Spec:** `docs/superpowers/specs/2026-06-16-rbac-design.md` — the approved design, decisions table, data model, enforcement, claim emission, scope boundaries.
- **Plan:** `docs/superpowers/plans/2026-06-16-rbac-app-authorization.md` — 11 tasks with concrete code/steps. THIS is the source of truth for what each task does.
- **Task tracker:** `docs/superpowers/plans/2026-06-16-rbac-app-authorization.md.tasks.json` — per-task status + dependencies + embedded metadata. Keep it in sync after each task.

## What this feature is (one paragraph)

A coarse per-app access gate on top of the existing "RP enforces policy" model: an admin marks an OIDC client / SAML SP `access_restricted`, then controls who may sign in via first-class **groups** and/or **individual accounts**. Groups with `exposed_to_downstream=true` ALSO flow to apps as an OIDC `groups` claim / SAML `groups` attribute (two-level opt-in: group flag + per-app request). No admin bypass. Denied interactive users land on the IdP `/error` page; `prompt=none` / SAML passive use the protocol-native denial. The end-user launchpad is OUT of scope.

## Execution method + conventions (FOLLOW THESE)

- **Skill:** `superpowers-extended-cc:subagent-driven-development`. Per task: dispatch an **implementer** subagent (full task text + context, never make it read the plan file) → **spec-compliance reviewer** subagent (verifies by reading code, not trusting the report) → **code-quality reviewer** subagent (`superpowers-extended-cc:code-reviewer`, BASE_SHA→HEAD_SHA) → implementer fixes any issues → re-verify → mark task complete → sync `.tasks.json`.
- **Branch:** working **directly on `master`** (user's explicit choice; their established per-cycle pattern). Currently **11 commits ahead of origin/master** — do NOT push unless asked; never rewrite at/below `origin/master`.
- **NEVER add a `Co-Authored-By` trailer** to any commit (firm user rule).
- **Model selection:** sonnet for implementers/reviewers of well-specified mechanical/integration tasks; reserve opus for judgment-heavy work. Never haiku. (Task 7 enforcement and Task 11 final review are the most judgment-heavy — consider opus there.)
- `SendMessage` is NOT available in this harness — to apply review fixes, dispatch a fresh agent with exact instructions (worked fine).

## Progress so far

| Task | State | Commits |
|------|-------|---------|
| **T1** schema + sqlc queries | ✅ DONE (spec✅ + quality✅, 2 fixes applied) | `d87d2fc`, `728e910` |
| **T2** groups admin API | ✅ DONE (spec✅ + quality✅, 2 fix rounds) | `7fdf6d0`, `496d95f`, `923eed7` |
| **T3** groups admin SPA | ✅ DONE (spec✅ + quality✅, 1 critical fix) | `4f1913e`, `9ff6bd8` |
| **T4** account-detail membership | ⏳ **IN PROGRESS** — implemented `4ce5e70`, **spec review ✅ PASSED**, **code-quality review NOT yet run** | `4ce5e70` |
| **T5–T11** | pending | — |

`f570ade` is a task-tracker sync commit (no code).

### IMMEDIATE NEXT ACTION

Run the **code-quality review for Task 4** (it has only passed spec review):
- `superpowers-extended-cc:code-reviewer`, BASE_SHA `9ff6bd8`, HEAD_SHA `4ce5e70`, diff `dashboard/src` + `pkg/contract` + `pkg/server` (the commit has NO dist — confirmed).
- Spec review already confirmed: separate `useApi()` instances (no busy-guard race), admin gating (`registerOp(..., admin)`, no sudo), `addableGroups` excludes current members, sudo-wrapped mutations, en.ts clean, 423/423 vitest + `vue-tsc -b` clean, `go build/vet/test ./pkg/server -run Account` green.
- If quality review is clean (or after fixes): mark Task 4 (`TaskUpdate #9 status completed`) and set `.tasks.json` id 3 → `completed`, then proceed to Task 5.

Then continue **T5 → T6 → T7 → T8 → T9 → T10 → T11** in dependency order (see `.tasks.json` `blockedBy`). T8 and T9 only depend on T1; T7 depends on T1+T5; T11 depends on everything.

## Hard-won lessons / gotchas (DO NOT relearn these)

1. **`useApi()` busy-guard race (CRITICAL pattern).** `useApi().run()` short-circuits if `busy` is already true. Two fetches in one `Promise.all` sharing ONE `useApi()` instance → the second silently no-ops. This shipped a broken (empty) member picker in T3 and was only caught by the code-quality reviewer at runtime, not by tests/build. **Every FE view that loads 2+ things concurrently must use a SEPARATE `useApi()` instance per concern (or sequence the awaits).** Always add a test that asserts the picker/list actually POPULATES.
2. **`pkg/webui/dist` churn.** `npm run build` rewrites all hashed dist chunks. The plan defers the authoritative dist rebuild+commit to **Task 11 only**. T3's implementer committed dist (noise, already in `4f1913e`); since then implementers are told to **typecheck with `npx vue-tsc -b` (no dist write), NOT `npm run build`**, and to **never `git add pkg/webui/dist`** until Task 11. If a step dirties dist, discard it: `git checkout -- pkg/webui/dist && git clean -fq pkg/webui/dist`.
3. **`pkg/server` suite flakes ~1/3** under parallel shared-DB runs (sudo test → bad_credentials/401). Re-run a failing case in isolation (`-run TheExactTest -count=1`) before treating it as real. (See memory `reference_flaky_server_suite`.)
4. **sqlc** is run via `mise exec -- sqlc generate` (config `sqlc.yaml` → generates `pkg/db`). IDE diagnostics go stale after generation — trust `go build`/`go test`.
5. **Editing a pre-deployment migration** (`015_rbac.sql` is unpushed): edit IN PLACE, don't chain a cleanup migration (memory `feedback_picotera_decoupling`). The dev DB already has v15 applied, so to test an amended 015, roll it with goose down→up (the agent found the goose invocation `mise db:up` uses) — its Down only drops the new RBAC tables/columns, safe in dev.
6. **Dev environment:** Postgres via `podman compose up -d` (`prohibitorum_dev`); migrations `mise db:up`; KV in-process memory. (memory `reference_dev_postgres_podman`)
7. **en.ts hazards:** escape literal `@` as `{'@'}` (prod vue-i18n compiler throws, `en.compile.test.ts` guards); after editing, grep for curly apostrophes U+2018/U+2019 the Edit tool can introduce. (memory `reference_vue_i18n_prod_compiler`, `reference_en_ts_apostrophe_edit_hazard`)
8. **Error helpers drive FE i18n.** Don't reuse a semantically-wrong `Err*` constructor — add a dedicated one (T2 added `ErrGroupNotFound` 404 / `ErrGroupSlugConflict` 409 in `pkg/authn/errors.go`). Pre-existing lint `pkg/authn/errors.go:444` (`errors.As`→`AsType`) is NOT ours — leave it.

## Decisions locked in (from brainstorming; don't relitigate)

Groups + direct accounts; per-app `access_restricted` flag (default false, existing apps untouched); groups exposed via per-group `exposed_to_downstream` flag (default **true**) + per-app opt-in; **no admin bypass**; denied UX = IdP `/error` page (interactive) / `access_denied` (OIDC prompt=none) / `RequestDenied` (SAML passive); refresh-token re-check; slug rename allowed with UI warning; CLI parity included.

## What exists now for upcoming tasks

**Generated sqlc (package `db`, via `s.queries`):** `CreateGroup/GetGroup/GetGroupBySlug/ListGroups/UpdateGroup/DeleteGroup`, `AddGroupMember/RemoveGroupMember/ListGroupMembers/ListGroupsForAccount`, `ListExposedGroupSlugsByAccount(accountID) []string` (T8/T9 use this), grant/revoke/list for `oidc_client_access` + `saml_sp_access` (group + account variants), `SetOIDCClientAccessRestricted/SetSAMLSPAccessRestricted`, and the predicates **`IsAccountAuthorizedForOIDCClient`/`IsAccountAuthorizedForSAMLSP`** (T7 uses these). `db.OidcClient`/`db.SamlSp` have `AccessRestricted bool`. Confirm exact `*Params`/field names in `pkg/db/rbac.sql.go`.

**Contract (`pkg/contract/auth.go`):** `GroupView`, `GroupMemberView`, `GroupRef`, `AccountRef` (the last two are forward-declared for T5 — used there), plus group + account-groups operations.

**Audit (`pkg/audit/event.go`):** `FactorGroup` added in T2. **T5 must add** `EventAccessGranted/EventAccessRevoked/EventAccessRestrictedSet/EventAccessDenied` consts (T7 uses `EventAccessDenied`).

**Key insertion points for T7 enforcement** (already mapped in the plan):
- OIDC: `pkg/protocol/oidc/authorize.go` after the session gate (~line 126); refresh in `pkg/protocol/oidc/refresh.go`; new helper `pkg/protocol/oidc/access.go`. Error codes incl. `errCodeAccessDenied` already exist in `errors.go`.
- SAML: `pkg/protocol/saml/sso.go::HandleSSO` after session gate (~line 139) + `HandleIdPInitiated`; add `statusRequestDenied` const; reuse `buildStatusResponse`/`writeAutoPost`.
- T8 claims: `pkg/protocol/oidc/claims.go` (`groupsClaims`, thread via `idTokenInput.Groups`), `oidc.go` (discovery `scopes_supported`), `token.go::mintAccessAndIDTokens` (fetch slugs when scope has `groups` — covers both grants), `userinfo.go`, `supportedOIDCScopes` in `handle_admin_oidc_clients.go`.
- T9 attr: `pkg/protocol/saml/attributes.go` (`"groups"` source alongside `attributes.administrator`), thread slugs into `projectAttributes` (change signature; update ALL call sites incl. `sso.go` + `idp_initiated.go`).

**Admin handler/route convention:** typed GET via `registerOp(mgmt, contract.OperationX, s.handleX, admin)`; raw sudo mutation via `s.registerSudoOpHTTP(s.router, method, path, admin, s.handleXHTTP)` — all in `server.go::registerOperations()`. Mirror `handle_admin_oidc_clients.go`. SPA admin pages mirror `AdminOidcClientDetailView.vue`; sudo via `@/lib/sudo` `withSudo`.

## Gate (run before declaring any task done; full gate at T11)

```
go build -tags nodynamic ./... && go vet ./... && go test ./...
cd dashboard && npm test && npx vue-tsc -b      # full `npm run build` + dist commit ONLY at T11
```
T11 also: live smoke (`SMOKE_EXIT=0`, see README "End-to-end smoke" — needs `PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK=true`), `mise build` to rebuild `pkg/webui/dist`, then `git add pkg/webui/dist`, tick README RBAC box, update api.md/ARCHITECTURE.md/STATUS.md.

## Working-tree state at handover

Clean except the `.tasks.json` sync edit (committed alongside this handover). Committed dist is stale (reflects T3) — that's fine; T11 rebuilds authoritatively.

---

## COMPLETION — 2026-06-16 (all 11 tasks done)

The continuing agent finished **T4 → T11**. Every task ran the full subagent loop (implementer → spec review → code-quality review → fixes → re-verify → sync). Final state: **all 11 plan tasks `completed`** in `.tasks.json`.

**Per-task outcome (commits on top of `origin/master`):**
- **T4** finalize: code-quality review CLEAN; applied 2 test nits (`b295ec9`).
- **T5** app-access API (`d0ba20b`): quality CLEAN. Spec reviewer flagged "no handler round-trip test" — **accepted as house convention** (the template `handle_admin_oidc_clients_test.go` / `handle_admin_groups_test.go` are *also* DB-free unit tests; admin handlers use a concrete `*db.Queries`, not a fakeable interface; round-trip is covered by the T11 smoke).
- **T6** Access card (`861eb0d`): PASS+CLEAN; `useApi` race avoided (3 separate instances), populate-asserting tests. (Found+fixed an unrelated stale `node_modules`: `vue-advanced-cropper` was missing — `npm install` restored it; suite is 439 tests.)
- **T7** enforcement (`9ad88f2`): spec PASS + adversarial quality CLEAN (no fail-open / bypass / identity-confusion; durable refresh-family revoke; full audit). Predicate returns `pgtype.Bool` (use `.Bool`); account ids are `pgtype.Int4`. Known accepted NIT: OIDC gate uses `prompt == "none"` exact-match (a malformed `prompt=none login` denial routes to interactive `/error` instead of RP `access_denied` — still denied; both reviewers OK to leave).
- **T8** OIDC groups claim (`4140b35`): PASS+CLEAN; present-but-empty `[]` vs absent verified; `userinfoClaims` signature changed + all callers updated; fixed `TestValidateOIDCScopes` (groups now valid).
- **T9** SAML groups attr (`06698a5`): PASS; removed a dead `attrSourceGroups` i18n key (amended). Accepted: the SAML SSO groups fetch is **unconditional** (cold path; matches plan's simplicity choice) rather than gated like OIDC's `hasScope`.
- **T10** CLI (`2d06379`): PASS; all 8 grant/revoke wirings verified correct; **live-validated** the `group` verb against a throwaway DB; fixed a gofmt-only nit (amended).
- **T11** smoke + docs + dist (`3291a64`, `14fb428`, `0ab1ebf`): smoke arc rbac 1–7 appended; docs (README box ticked, api.md, ARCHITECTURE, STATUS v0.7); dist rebuilt + committed.

**Final gate — GREEN:** `go build -tags nodynamic ./...` ✓ · `go vet ./...` ✓ · `go test ./...` ✓ (16 pkgs) · `cd dashboard && npm test` ✓ (439) · `npm run build` ✓ · **live smoke `SMOKE_EXIT=0`** including the RBAC arc (deny non-member → `/error?reason=app_access_denied` no code; via-group grant → code → `groups` claim in id_token + userinfo).

**Environment note for future live smokes here:** podman/docker are unavailable, but the Homebrew `postgres` 18.4 binary is — spin a throwaway cluster with `initdb`/`pg_ctl` on a spare port (e.g. 55432, trust auth, user `prohibitorum`), point `PROHIBITORUM_DATABASE_URL` at it. The smoke shells out via `mise exec`, and `mise.toml` pins `postgres = "18.4"` which fails to build from source (no pkg-config/ICU) — run the smoke with **`MISE_DISABLE_TOOLS=postgres`** so mise skips it (the smoke uses the system `psql`, not mise's postgres).

**Note on push state:** at completion `origin/master` was at `313772e` (the T1–T4 + handover commits had been pushed); T5–T11 are local commits on top. Per the firm rule, nothing was pushed — leave that to the user.
