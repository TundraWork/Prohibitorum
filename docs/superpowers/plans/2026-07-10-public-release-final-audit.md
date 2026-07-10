# Public Release Final Audit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> `superpowers:dispatching-parallel-agents` for Tasks 2-4. Audit agents are
> read-only; the primary auditor owns consolidation and repository changes.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce an evidence-backed public-release verdict for the behavior
implemented at commit `4702e512`, plus exact remediation plans that do not
expand product functionality.

**Architecture:** The primary auditor establishes a reproducible baseline and
cross-system inventory while three independent agents deeply review security
and protocols, backend/data/operations, and frontend/UX. The primary auditor
then normalizes and reproduces findings, sends high-risk findings through a
cross-domain challenge wave, and writes the final report and remediation plans.

**Tech Stack:** Go 1.26, PostgreSQL 14+, Redis-compatible KV, Vue 3, TypeScript,
Vite 6, Vitest 2, Tailwind CSS 4, GitHub Actions, GoReleaser, Cosign.

## Global Constraints

- The executable code, database constraints, generated artifacts, tests, and
  observed runtime behavior are authoritative. Documentation is a claim to
  verify, not a source of truth.
- The code baseline is commit `4702e512`; audit documents and approved
  behavior-preserving UX corrections created afterward are included in the
  final change ledger.
- Do not propose or implement new protocols, factors, tenant models, policy
  engines, delivery channels, or other net-new capability.
- Accepted capability omissions are findings only if implemented behavior
  contradicts its own contract, weakens or misrepresents a trust boundary,
  creates an undocumented unsafe default, exposes a dead control, or has become
  inaccurate because code changed.
- All first-pass audit agents are read-only. They may run tests and write their
  report under `/tmp/prohibitorum-audit/`; they must not modify repository files.
- Every reported defect must identify exact source locations, affected actor,
  reachable execution path, violated invariant, impact, smallest in-scope
  remediation, verification method, severity, and confidence.
- Suspicious patterns without reachable impact remain investigation notes and
  are not counted as release findings.
- Frontend evaluation targets WCAG 2.2 AA and the PRODUCT.md keyboard-first
  commitment. It must include the Impeccable 0-20 audit health score.
- Only the primary auditor may apply an on-the-fly UX/accessibility correction,
  and only after first-pass consolidation confirms every condition in section 8
  of the audit design.
- Confirmed P0 findings and unaccepted confirmed P1 findings block release.

---

### Task 1: Establish the Baseline and Coverage Inventories

**Files:**

- Read: `docs/superpowers/specs/2026-07-10-public-release-final-audit-design.md`
- Read: `ARCHITECTURE.md`
- Read: `PRODUCT.md`
- Read: `DESIGN.md`
- Read: `AUDIT.md`
- Read: `CONFIG.md`
- Read: `STATUS.md`
- Read: `api.md`
- Read: `INTEGRATION.md`
- Read: `TOOLING.md`
- Read: `mise.toml`
- Read: `.github/workflows/ci.yml`
- Read: `.github/workflows/release.yml`
- Create during Task 8:
  `docs/superpowers/notes/2026-07-10-public-release-final-audit.md`

**Interfaces:**

- Consumes: Git commit `4702e512` and the approved audit design.
- Produces: Baseline command results; route, actor, trust-boundary,
  configuration, migration, source-file, and test inventories used to check
  agent coverage.

- [ ] **Step 1: Confirm the audit baseline and worktree state**

  Run:

  ```bash
  git status --short
  git log -2 --oneline --decorate
  git diff --check
  ```

  Expected: only intentional audit artifacts differ from the pre-audit code
  baseline; no unexplained user changes are touched.

- [ ] **Step 2: Inventory handwritten production and test files**

  Run:

  ```bash
  rg --files cmd pkg dashboard/src db .github | sort
  find cmd pkg dashboard/src db -type f \( -name '*.go' -o -name '*.vue' -o -name '*.ts' -o -name '*.css' -o -name '*.sql' \) | sort
  ```

  Expected: every handwritten file is assigned to the primary auditor or one
  Wave 1 agent. Generated `pkg/db/*.sql.go` and `pkg/webui/dist/**` are checked
  for drift and output safety, not reviewed as handwritten source.

- [ ] **Step 3: Inventory public entry points and security decisions**

  Run:

  ```bash
  rg -n 'HandleFunc|Method\(|huma\.Register|Register\(|Use\(|Mount\(' pkg/server pkg/protocol pkg/authn pkg/session cmd
  rg -n 'admin|sudo|Authorize|IsAccountAuthorized|disabled|csrf|origin|redirect' pkg/server pkg/protocol pkg/authn pkg/session
  rg -n 'BindPFlag|GetString|GetBool|GetDuration|PROHIBITORUM_' cmd pkg CONFIG.md
  ```

  Expected: the primary auditor can map each route and configuration value to
  authentication, authorization, validation, persistence, audit, and error
  handling.

- [ ] **Step 4: Run the Go fast gate**

  Run:

  ```bash
  go vet ./...
  go build -tags nodynamic ./...
  go test ./...
  ```

  Expected: exit 0 for each command. Any failure becomes a baseline finding or
  is explained as an environment limitation with captured output.

- [ ] **Step 5: Run the Go race gate**

  Run:

  ```bash
  go test -race ./...
  ```

  Expected: exit 0 with no race report. A package that cannot run under the race
  detector is named explicitly rather than silently excluded.

- [ ] **Step 6: Run the frontend gates**

  Run:

  ```bash
  cd dashboard && npm test
  cd dashboard && npm run build
  node dashboard/scripts/check-contrast.mjs
  git diff --exit-code -- pkg/webui/dist
  ```

  Expected: tests and build exit 0; contrast pairs pass; the committed embedded
  bundle matches the fresh build.

- [ ] **Step 7: Run integration and release controls**

  Run:

  ```bash
  mise run ci:smoke
  mise run ci:lint-actions
  mise run ci:release-check
  ```

  Expected: exit 0. If a container runtime or pinned tool is unavailable, record
  the exact command, error, and which release claim remains unverified.

### Task 2: Security, Authentication, and Protocol Deep Review

**Files:**

- Read every handwritten file under `pkg/authn/`, `pkg/credential/`,
  `pkg/session/`, `pkg/protocol/`, `pkg/federation/`, and `pkg/kv/`.
- Read corresponding route wiring and handlers in `pkg/server/`.
- Read corresponding SQL in `db/queries/` and schema history in
  `db/migrations/`.
- Read security-relevant configuration in `pkg/configx/` and
  `cmd/prohibitorum/`.
- Read the tests adjacent to every production file in this scope.
- Create: `/tmp/prohibitorum-audit/security-protocol-report.md`

**Interfaces:**

- Consumes: Task 1 inventories and audit-design sections 4.2 through 4.6.
- Produces: A file-by-file coverage ledger, ceremony/endpoint trace matrix,
  confirmed candidate findings, investigation notes, current-advisory checks,
  and positive security controls.

- [ ] **Step 1: Trace every supported authentication ceremony**

  Trace begin, complete, cancellation, retry, expiry, replay, revocation, account
  disablement, and concurrent completion for WebAuthn, password+TOTP, recovery
  codes, enrollment, pairing, sudo, upstream OIDC, Steam, and logout.

- [ ] **Step 2: Trace downstream OIDC end to end**

  Cover discovery, authorize, consent, token, refresh/reuse, userinfo,
  introspection, revocation, logout, JWKS, claims, keys, prompt behavior, RBAC,
  and protocol-native errors. Prove client, redirect, issuer, subject, audience,
  scope, nonce, PKCE, code, session, and refresh-family binding.

- [ ] **Step 3: Trace SAML IdP end to end**

  Cover metadata, Redirect/POST requests, signature policy, XML handling, ACS
  selection, consent, passive and IdP-initiated behavior, NameID, attributes,
  response/assertion signatures, key rollover, RBAC, RelayState, and errors.

- [ ] **Step 4: Review outbound-request and cryptographic safety**

  Cover discovery/metadata fetches, issuer mix-up, SSRF, DNS/private-network
  rules, timeouts and body limits, avatar/icon processing, random token sources,
  password hashing, sealed secrets, AAD, key versions, and signing lifecycle.

- [ ] **Step 5: Run focused adversarial tests**

  Run the narrowest existing package tests that exercise each candidate. Where
  current tests cannot distinguish safe from unsafe behavior, describe the
  minimal missing regression test without editing repository files.

- [ ] **Step 6: Check time-sensitive dependency and standard claims**

  Consult primary upstream advisories and official protocol/security sources for
  versions and behaviors that may have changed. Record URLs and distinguish a
  direct vulnerability from an unreachable dependency feature.

- [ ] **Step 7: Write the security/protocol report**

  Use the finding schema from the audit design. Include every reviewed file in
  the coverage ledger, including files with no findings. Return `DONE`, the
  report path, focused test commands/results, and any stated uncertainty.

### Task 3: Backend Correctness, Data, Administration, and Operations Review

**Files:**

- Read every handwritten file under `pkg/account/`, `pkg/audit/`, `pkg/avatar/`,
  `pkg/branding/`, `pkg/clientip/`, `pkg/configx/`, `pkg/contract/`, `pkg/db/`,
  `pkg/errorx/`, `pkg/imageutil/`, `pkg/logx/`, `pkg/server/`, `pkg/weberr/`, and
  `pkg/webui/`.
- Read every file under `cmd/prohibitorum/`, `cmd/smoke/`, and `cmd/steammock/`.
- Read every SQL source under `db/migrations/` and `db/queries/`, plus
  `sqlc.yaml`.
- Read `compose.yaml`, `.goreleaser.yaml`, `mise.toml`, `mise.lock`, all files
  under `.github/workflows/`, and all root public documentation.
- Create: `/tmp/prohibitorum-audit/backend-operations-report.md`

**Interfaces:**

- Consumes: Task 1 inventories and audit-design sections 4.1, 4.7 through 4.9,
  and backend portions of 4.12.
- Produces: File coverage, endpoint/gate matrix, config-use matrix,
  migration/query review, mutation/audit matrix, operational/release checks,
  candidate findings, document drift, and positive controls.

- [ ] **Step 1: Inventory every API, CLI, and background mutation**

  For each entry point, identify actor, authentication, ownership/admin gate,
  sudo requirement, validation/body limit, transaction, persistence effects,
  audit event, redaction, and error mapping.

- [ ] **Step 2: Review persistence and lifecycle invariants**

  Inspect constraints, foreign keys, uniqueness, deletes, pagination, time
  comparisons, transaction boundaries, isolation, concurrent updates, cleanup,
  reconciliation, generated-query correspondence, empty install, and ordered
  migration upgrade safety.

- [ ] **Step 3: Review configuration and failure behavior**

  Trace each config definition through parsing, validation, runtime use, and
  public docs. Check proxy trust, origin/TLS assumptions, database/KV failures,
  memory-driver limitations, first boot, encryption-key versions, signing-key
  bootstrap, shutdown, health/readiness, maintenance, and resource limits.

- [ ] **Step 4: Review auditability, privacy, and diagnostics**

  Check mutation coverage, transactional guarantees, actor attribution,
  redaction, secret write-only behavior, log/API/audit detail leakage, personal
  data minimization, and filter/pagination correctness.

- [ ] **Step 5: Review build and release integrity**

  Check pinned tools, action permissions and pinning, untrusted input use,
  generated artifacts, build tags, single-binary contents, OCI platforms, SBOM,
  provenance/signature workflow, release dry-run controls, and operator docs.

- [ ] **Step 6: Run focused correctness tests**

  Run the narrowest existing package/CLI tests for each candidate and record
  whether failures are deterministic, environmental, or currently untested.

- [ ] **Step 7: Write the backend/operations report**

  Use the finding schema from the audit design. Include every reviewed file in
  the coverage ledger. Return `DONE`, the report path, focused test
  commands/results, and any stated uncertainty.

### Task 4: Frontend Logic, UX, Accessibility, and Performance Review

**Files:**

- Read every handwritten file under `dashboard/src/` and every frontend
  configuration/script under `dashboard/`, excluding `dashboard/node_modules/`.
- Inspect `pkg/webui/dist/` only for build drift, bundle composition, and shipped
  artifact behavior.
- Read `PRODUCT.md` and `DESIGN.md` as intent to verify against code.
- Create: `/tmp/prohibitorum-audit/frontend-ux-report.md`

**Interfaces:**

- Consumes: Task 1 inventories, audit-design sections 4.10 through 4.12, and the
  Impeccable product audit rubric.
- Produces: Route/state/API matrix, complete handwritten-file ledger, P0-P3
  findings, systemic patterns, positive findings, bundle observations, and the
  0-20 UI audit health score.

- [ ] **Step 1: Inventory routes, roles, and API contracts**

  Map each route to actor/role, backend requests, success state, loading state,
  empty state, validation errors, server errors, session expiry, sudo expiry,
  permission denial, cancellation, and retry. Cross-check request and response
  shapes with backend handlers and contract types.

- [ ] **Step 2: Audit critical member and threshold flows**

  Trace login, passkey, password+TOTP, enrollment, pairing, welcome, OIDC/SAML
  consent, app access denial, logout, security credentials, sessions, devices,
  connected accounts, recovery codes, PATs, and app launchpad.

- [ ] **Step 3: Audit every admin flow**

  Trace accounts, invitations, groups, OIDC clients, SAML providers, upstream
  IdPs, forward-auth apps, signing keys, audit events, and settings, including
  destructive confirmation and stale-sudo recovery.

- [ ] **Step 4: Audit WCAG 2.2 AA and keyboard-first behavior**

  Check semantics, accessible names/roles/states, form associations, errors and
  live status, landmarks/headings, focus order/visibility, dialogs/popovers,
  passkey and TOTP keyboard paths, color independence, reduced motion, contrast,
  target size, 320-pixel reflow, and text zoom risk.

- [ ] **Step 5: Audit responsive design, theming, and visual consistency**

  Check desktop/mobile structure, overflow, tables, drawers, long localized
  strings, dark/light themes, tokens, component states, progressive disclosure,
  security copy, terminology, design-system adherence, and all Impeccable
  anti-patterns.

- [ ] **Step 6: Audit frontend performance and shipped assets**

  Check route lazy loading, dependency/import weight, asset dimensions and
  formats, unnecessary rerenders/watchers, expensive effects, network behavior,
  and generated-bundle composition. Use build output as evidence rather than
  estimating bundle size from source alone.

- [ ] **Step 7: Run focused frontend tests and write the report**

  Run `cd dashboard && npm test` plus focused Vitest files for candidate
  findings and `node dashboard/scripts/check-contrast.mjs`. Include the required
  health-score table, anti-pattern verdict first, findings by severity, systemic
  issues, positive findings, recommended Impeccable commands, and every reviewed
  handwritten file in the coverage ledger. Return `DONE`, the report path,
  commands/results, and uncertainty.

### Task 5: Normalize, Reproduce, and Deduplicate Wave 1 Findings

**Files:**

- Read: `/tmp/prohibitorum-audit/security-protocol-report.md`
- Read: `/tmp/prohibitorum-audit/backend-operations-report.md`
- Read: `/tmp/prohibitorum-audit/frontend-ux-report.md`
- Read exact source and tests cited by every candidate finding.
- Create during Task 8:
  `docs/superpowers/notes/2026-07-10-public-release-final-audit.md`

**Interfaces:**

- Consumes: Task 1 baseline and Tasks 2-4 reports.
- Produces: A normalized candidate ledger with stable IDs, deduplicated root
  causes, corrected severities, confidence, and primary reproduction status.

- [ ] **Step 1: Check report completeness against inventories**

  Reject unexplained gaps in file, route, ceremony, config, migration, UI-state,
  or test coverage. Send focused follow-ups to the responsible agent before
  accepting its report.

- [ ] **Step 2: Validate each candidate's source path and reachability**

  Read cited code and all guards on the execution path. Demote patterns without
  proven reachability to investigation notes.

- [ ] **Step 3: Reproduce every P0/P1 candidate independently**

  Run an existing focused test, write a non-repository `/tmp` proof harness, or
  provide a complete static proof. Record exact command and output. Do not use an
  agent's test summary as the sole evidence.

- [ ] **Step 4: Sample and systemic-check P2/P3 candidates**

  Reproduce each distinct systemic root cause and at least one representative
  location. Drop duplicative polish noise.

- [ ] **Step 5: Normalize the UI audit score and documentation differences**

  Verify all five Impeccable dimension scores from cited evidence. Classify each
  document mismatch as a runtime defect, documentation defect, intentional
  limitation, or historical note with no current impact.

### Task 6: Cross-Domain Challenge Review

**Files:**

- Read: the normalized candidate ledger prepared in Task 5.
- Read: exact source/tests for assigned challenge candidates.
- Append challenge results to the three `/tmp/prohibitorum-audit/*-report.md`
  files.

**Interfaces:**

- Consumes: normalized P0/P1 candidates, systemic P2 candidates, and identified
  coverage gaps.
- Produces: independent confirm/refute decisions, missing guards or impacts,
  boundary findings, and resolved coverage gaps.

- [ ] **Step 1: Challenge security findings from the backend/data perspective**

  Assign Agent B each security/protocol P0/P1 and ask it to trace persistence,
  transaction, configuration, and operational guards that could confirm or
  refute the candidate.

- [ ] **Step 2: Challenge backend findings from the adversarial perspective**

  Assign Agent A each backend P0/P1 and ask it to test whether the claimed actor
  and preconditions can actually reach the impact across session and protocol
  boundaries.

- [ ] **Step 3: Challenge frontend/backend boundary claims**

  Assign Agent C UI/API mismatches plus backend/frontend authorization gaps and
  ask it to determine whether backend enforcement, router guards, or recovery UI
  changes the stated impact.

- [ ] **Step 4: Close combined coverage gaps**

  Dispatch focused follow-ups for every file, route, ceremony, config path, or UI
  state absent from all ledgers. A report cannot be complete while a gap is
  merely listed.

- [ ] **Step 5: Adjudicate disagreements with direct evidence**

  The primary auditor reads the full execution path or runs a minimal focused
  reproduction. Record the final status and why the losing interpretation was
  rejected.

### Task 7: Triage Safe UX Corrections and Remediation Units

**Files:**

- Read: confirmed findings and section 8 of
  `docs/superpowers/specs/2026-07-10-public-release-final-audit-design.md`.
- Create exact per-fix plans under `docs/superpowers/plans/` only after the
  affected source and tests are known.

**Interfaces:**

- Consumes: challenged and confirmed findings.
- Produces: A list of behavior-preserving UX corrections eligible for immediate
  TDD execution, plus independent remediation units for all remaining findings.

- [ ] **Step 1: Apply all six eligibility tests to each UX candidate**

  Record pass/fail for unambiguous intent, locality, contract preservation,
  evidence preservation, deterministic verification, and completed first-pass
  consolidation. Any failed condition sends the candidate to remediation
  planning rather than opportunistic implementation.

- [ ] **Step 2: Group findings by authoritative root cause**

  One missing invariant with multiple symptoms becomes one remediation unit.
  Independently deployable fixes remain separate so reviewers can accept or
  defer them without coupling.

- [ ] **Step 3: Order remediation dependencies**

  Order containment, shared invariant repair, persisted-state compatibility,
  regression tests, observability/audit behavior, frontend recovery, docs, and
  full verification.

- [ ] **Step 4: Write exact implementation plans after locations are known**

  Each plan must name exact files, interfaces, failing tests, implementation,
  commands, expected output, and commits. No plan may add a capability excluded
  by the audit design.

### Task 8: Write and Verify the Final Audit Report

**Files:**

- Create:
  `docs/superpowers/notes/2026-07-10-public-release-final-audit.md`
- Create: independent remediation plans under `docs/superpowers/plans/`.
- Modify only when proven stale and necessary: public root documentation.

**Interfaces:**

- Consumes: baseline output, all coverage ledgers, normalized/challenged
  findings, UI score, accepted limitations, remediation units, and any approved
  UX fix evidence.
- Produces: release verdict, finding ledger, verification table, positive
  controls, residual risk, documentation drift, and executable plans.

- [ ] **Step 1: Write the executive release verdict**

  State Ready, Conditionally ready, or Not ready using section 11 of the audit
  design. Include P0/P1/P2/P3 counts and confirmed/high/medium confidence counts.

- [ ] **Step 2: Write evidence-linked findings**

  For each finding include the required schema, exact source link, reproduction,
  impact, remediation unit, and release effect. Keep investigation notes outside
  release totals.

- [ ] **Step 3: Write coverage and positive-control sections**

  Include agent file ledgers, endpoint/config/migration/route matrices, baseline
  commands, behavior that resisted adversarial review, and material limitations
  in what could be executed locally.

- [ ] **Step 4: Write the UI health report**

  Include anti-pattern verdict first, five 0-4 dimension scores, total/rating,
  severity counts, systemic issues, positive findings, and prioritized
  Impeccable commands ending in `$impeccable polish` when fixes are recommended.

- [ ] **Step 5: Self-review report and plans**

  Check every spec requirement has evidence; scan for placeholders; verify
  severity/count consistency, source links, plan interfaces, and distinction
  among code defects, doc defects, accepted limitations, and investigation
  notes.

- [ ] **Step 6: Run fresh final verification**

  Run the applicable complete gates from Task 1 again after any repository
  changes. Read full output and report exact pass/fail/unavailable status; do not
  infer success from earlier runs or agent summaries.

- [ ] **Step 7: Commit audit artifacts and eligible fixes**

  Run:

  ```bash
  git diff --check
  git status --short
  git diff --stat
  ```

  Review every changed file, then commit with messages that separate audit
  documentation from any behavior-preserving UX corrections.
