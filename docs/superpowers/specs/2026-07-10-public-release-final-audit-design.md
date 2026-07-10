# Public Release Final Audit Design

**Date:** 2026-07-10  
**Scope:** Prohibitorum code at commit `4702e512`, plus the audit artifacts
created afterward  
**Purpose:** Establish whether the behavior already implemented is coherent,
secure, operable, and understandable enough for public release without expanding
the product's capability scope.

## 1. Audit Objective

The audit evaluates the executable system, not the aspirations described in its
documentation. Code, database constraints, generated artifacts, tests, and
observed runtime behavior are authoritative. Documentation is treated as a set
of claims that must be checked against those sources.

The audit must answer five questions:

1. Does every implemented authentication, authorization, administration, and
   protocol flow enforce its stated invariants on success, failure, replay,
   cancellation, and concurrent execution?
2. Can an unauthenticated user, ordinary member, administrator, upstream
   provider, or downstream relying party cross a trust boundary they should not
   cross?
3. Do persistence, configuration, deployment, upgrade, and recovery behavior
   preserve the security and availability properties claimed by the service?
4. Can members and administrators complete the existing product flows on
   desktop and mobile, with keyboard and assistive technology, without being
   misled or trapped?
5. Are the repository's public documentation and release artifacts accurate
   enough that an operator can deploy the system safely?

Documented capability omissions that the product owner has accepted are not
defects merely because a larger IdP implements them. They become findings only
when they cause one of the following:

- existing behavior contradicts its own API, UI, or protocol contract;
- an implemented trust boundary is incomplete or misleading;
- the omission creates an undocumented unsafe deployment default;
- the UI exposes a control or promise that cannot work correctly;
- current code has made the documented limitation obsolete or inaccurate.

## 2. Non-Goals

The audit will not propose or implement new protocols, authentication factors,
tenant models, policy engines, email delivery, SIEM export, HSM/KMS support, or
other net-new capabilities.

It will not grade Prohibitorum against feature parity with Keycloak, Authentik,
Okta, or another general-purpose IdP. Those products may inform threat models
and usability expectations, but their breadth is not the release requirement.

It will not treat the mere absence of optional protocol extensions as a defect.
For example, an extension is in scope only if Prohibitorum advertises it,
partially implements it, or relies on it for the safety of an existing flow.

## 3. Review Principles

### 3.1 Evidence before assertion

Every reported defect must include:

- a stable finding identifier;
- severity and confidence;
- affected actor and supported configuration;
- exact source locations;
- the invariant or user expectation being violated;
- a concrete execution path, test, trace, or reproducible static proof;
- security, correctness, operational, or user impact;
- the smallest behavior-preserving remediation;
- a verification method that would fail before the fix and pass afterward.

A suspicious pattern without a reachable impact is recorded as an investigation
note, not promoted to a release finding.

### 3.2 Adversarial and constructive review

For each flow, reviewers inspect the successful path and at least these adverse
classes where applicable:

- malformed, missing, duplicated, oversized, and stale inputs;
- replay, retry, cancellation, and browser back/forward behavior;
- simultaneous requests and transaction rollback;
- user or resource disablement between ceremony stages;
- cross-account, cross-client, cross-provider, and cross-session substitution;
- origin, redirect, issuer, audience, recipient, and host confusion;
- partial dependency failure, restart, and expired state;
- insufficient privilege, stale privilege, and step-up expiry;
- localization, small viewport, keyboard-only, and error-state operation.

### 3.3 Code truth over document truth

When documentation and code disagree, the reviewer determines which behavior is
internally coherent and safe. The finding identifies both the runtime defect and
the documentation drift separately when both have user impact. A document is not
used to dismiss a demonstrable defect.

### 3.4 No speculative feature expansion

Recommendations must repair an existing invariant, clarify an existing flow, or
remove friction from an existing capability. If a recommendation requires a new
product decision, protocol surface, persistence model, or operator commitment,
it is explicitly labeled out of scope instead of entering the remediation plan.

## 4. Audit Dimensions

### 4.1 Architecture and trust-boundary consistency

Review package boundaries and runtime wiring from request entry to persistence
and issued assertion. Verify that the documented three-layer model is reflected
in dependencies and that security decisions are made at authoritative
chokepoints rather than duplicated inconsistently.

Coverage includes:

- route registration, middleware ordering, method/content-type/body limits;
- session and recent-auth contracts shared by APIs and protocols;
- account disabled state and role changes during active sessions;
- single-tenant assumptions and public-origin/host handling;
- error translation, logging, and avoidance of sensitive detail leakage;
- consistency between CLI, HTTP API, dashboard, and protocol entry points.

### 4.2 Authentication and credential lifecycle

Inspect WebAuthn, password, TOTP, recovery codes, enrollment, device pairing,
upstream OIDC/Steam federation, sudo reauthentication, and logout.

For every ceremony, trace begin, complete, retry, cancel, expire, replay, and
revocation behavior. Verify binding among browser, account, session, request
metadata, relying-party identity, and stored transient state. Review credential
at-rest protection, versioned key handling, password parameters, TOTP replay
prevention, recovery-code single use, passkey sign-count behavior, and audit
events.

### 4.3 Session and authorization correctness

Review session creation, cookie attributes and scoping, rotation, expiry,
revocation, cache/database interaction, sudo freshness, PAT authentication,
member/admin separation, per-application RBAC, group membership, maintenance
mode, and forward-auth decisions.

Every mutation and sensitive read is checked for authentication, authorization,
fresh-auth, ownership, CSRF, and disabled-account enforcement. TOCTOU windows and
authorization rechecks are inspected where state can change after a ceremony
begins.

### 4.4 Downstream OIDC provider

Trace discovery, authorization, consent, code issuance, token exchange, refresh
rotation and reuse, userinfo, introspection, revocation, logout, JWKS, signing-key
rotation, claims, and error responses.

Check exact client and redirect binding; PKCE; `state`, `nonce`, `iss`, issuer,
audience, authorized-party and token-type semantics; code and refresh replay;
prompt behavior; scope/consent narrowing; RBAC reevaluation; disabled resources;
key publication windows; HTTP cache and content types; and mismatch between
advertised metadata and accepted behavior.

### 4.5 SAML identity provider

Trace metadata, Redirect/POST AuthnRequest parsing, signature policy, ACS
selection, consent, passive behavior, IdP-initiated launch, assertion creation,
attributes, NameID stability, key rollover, and protocol-native errors.

Check XML parser hardening; signature wrapping defenses; request/response
binding; destination, recipient, audience, issuer, timing, InResponseTo, RelayState,
and AuthnContext; configured SP state; RBAC; and metadata accuracy.

### 4.6 Federation and outbound-request safety

Review discovery and callback binding, issuer and subject identity, state/nonce,
PKCE where supported, invitation/provisioning modes, linking confirmation,
account collision handling, secret sealing, private-network policy, redirects,
timeouts, response-size limits, DNS/rebinding assumptions, and mid-flight
configuration changes.

All server-side HTTP fetches, metadata ingestion, icon/avatar processing, and
operator-provided URLs are included in SSRF and resource-exhaustion review.

### 4.7 Persistence, concurrency, and migrations

Inspect schema constraints, foreign keys, uniqueness, deletion behavior, SQL
queries, transaction boundaries, isolation assumptions, optimistic/pessimistic
locking, timestamp precision, pagination, and cleanup/reconciliation jobs.

Migrations are reviewed both as a clean install and as an ordered upgrade. The
audit checks that security invariants are enforced in the database where races
could bypass application-only checks, and that generated sqlc code matches query
intent.

### 4.8 Administration, auditability, and privacy

Inventory every admin and self-service mutation and sensitive read. Verify route
gates, object ownership, fresh sudo, secret write-only behavior, redaction,
transactional audit recording, actor attribution, event completeness, and safe
pagination/filtering.

Review logs, API errors, audit details, OpenAPI/schema output, frontend error
rendering, and release diagnostics for credential material, tokens, internal
keys, private URLs, or unnecessary personal data.

### 4.9 Configuration and operational safety

Map every configuration input from definition through parsing, validation, use,
and documentation. Check unsafe combinations, proxy/client-IP trust, TLS/public
origin assumptions, database and KV failure behavior, in-memory driver caveats,
encryption-key rotation, first boot, key bootstrap, health/readiness, shutdown,
maintenance mode, backups/upgrades, and resource limits.

Review container/release configuration, embedded frontend freshness, reproducible
tooling, dependency pinning, GitHub Actions permissions, artifact provenance,
signing, SBOM generation, and supported-platform claims.

### 4.10 Frontend logic and functional completeness

Inventory all routes, role gates, API integrations, forms, dialogs, transient
states, and protocol threshold pages. Compare frontend request/response
assumptions to backend contracts and check loading, empty, success, partial,
error, unauthorized, expired-session, stale-sudo, cancellation, and retry states.

The review checks that existing backend capabilities exposed in the product are
actually completable in the UI, without requiring the UI to expose every CLI or
operator-only capability.

### 4.11 UX, accessibility, responsive behavior, and visual system

Use PRODUCT.md and DESIGN.md as design intent while verifying their claims
against the Vue/CSS implementation. Apply the Impeccable product audit rubric
and separately report its 0-20 health score.

Coverage includes:

- WCAG 2.2 AA semantics, names, roles, states, labels, errors, status messages,
  landmarks, heading order, contrast, focus visibility, and reflow;
- full keyboard completion of login, enrollment, TOTP, passkey, consent,
  recovery, session, and admin flows;
- 44 by 44 CSS-pixel targets where WCAG 2.2 target-size exceptions do not apply;
- 320 CSS-pixel viewport behavior and 200%/400% text zoom risk;
- dark/light themes, reduced motion, color-independent state, and token use;
- information architecture, terminology, progressive disclosure, destructive
  confirmations, recovery guidance, and security-copy precision;
- component-state completeness and consistency across member/admin surfaces;
- bundle composition, lazy-loaded routes, asset sizes, rendering hot spots, and
  unnecessary network work.

### 4.12 Testing and documentation reliability

Map tests to trust boundaries and critical flows rather than using line coverage
as a proxy for assurance. Identify untested negative paths, mocks that bypass the
real boundary, timing-flaky tests, weak assertions, and high-value integration
gaps.

Cross-check README.md, ARCHITECTURE.md, AUDIT.md, CONFIG.md, INTEGRATION.md,
STATUS.md, api.md, OpenAPI output, CLI help, UI copy, and release instructions
against current code. Historical design and handoff notes are context only.

## 5. Parallel Review Structure

All first-pass agents are read-only. They may run tests and create temporary
artifacts under `/tmp`, but they do not edit production files. This prevents
concurrent audit work from hiding evidence or creating merge conflicts.

### Wave 1: Independent deep review

Three agents work concurrently while the primary auditor runs the repository
baseline and constructs the cross-system inventory.

**Agent A: Security, authentication, and protocol adversary**

- Owns dimensions 4.2 through 4.6.
- Closely reads the relevant production code, SQL, tests, and runtime wiring.
- Traces all supported ceremonies and protocol endpoints end to end.
- Checks current dependency/security advisories using primary sources when a
  version or standard claim is time-sensitive.
- Returns only evidence-backed findings plus a coverage ledger and positive
  controls that resisted the attempted attack class.

**Agent B: Backend correctness, data, administration, and operations**

- Owns dimensions 4.1, 4.7 through 4.9, and backend portions of 4.12.
- Inventories every server operation and its gates, transactions, SQL effects,
  audit events, configuration path, migration, CLI path, and release control.
- Looks for races, inconsistent lifecycle transitions, partial failures,
  unsafe defaults, stale/generated artifacts, and code/document drift.
- Returns evidence-backed findings, an endpoint/config/migration coverage
  ledger, and positive invariants.

**Agent C: Frontend logic, UX, accessibility, and performance**

- Owns dimensions 4.10, 4.11, and frontend portions of 4.12.
- Reads every handwritten Vue/TypeScript/CSS/localization file, excluding only
  vendored dependencies and generated distribution bundles except for drift and
  bundle inspection.
- Applies the Impeccable audit scoring rubric, checks API contract alignment,
  and exercises existing component/page tests.
- Returns evidence-backed findings, a route/state coverage ledger, the 0-20 UI
  health score, systemic patterns, and positive findings.

**Primary auditor: Integration and independent baseline**

- Builds the route, actor, trust-boundary, configuration, migration, and test
  inventory used to detect gaps between agent scopes.
- Runs baseline verification and focuses on cross-cutting behavior that no
  package-local review owns.
- Reproduces all candidate release blockers rather than accepting agent
  summaries as proof.

### Wave 2: Cross-review and challenge

After Wave 1 findings are normalized, agents receive focused challenge tasks:

- attempt to falsify P0/P1 findings outside their original domain;
- inspect the boundary between frontend assumptions and backend enforcement;
- compare protocol/runtime claims with public documentation;
- search uncovered files and flows from the combined coverage ledgers;
- identify duplicates and shared root causes.

A finding survives only if its evidence remains valid after challenge. Conflicts
are resolved by direct source inspection or a minimal reproduction.

## 6. Automated and Manual Evidence

The baseline attempts the following checks using the repository-pinned toolchain
where available:

```text
git status --short
go vet ./...
go build -tags nodynamic ./...
go test ./...
go test -race ./...
cd dashboard && npm test
cd dashboard && npm run build
node dashboard/scripts/check-contrast.mjs
mise run ci:smoke
mise run ci:lint-actions
mise run ci:release-check
```

Additional focused tests are selected from the code review. Generated artifact
drift is checked for sqlc output, OpenAPI output, and `pkg/webui/dist`. Database
or container-dependent checks are recorded as not executed only after available
local alternatives are exhausted.

Static review includes searches for ignored errors, panic/fatal paths, weak
randomness, permissive URL handling, direct SQL outside intended boundaries,
unbounded reads, token/secret logging, missing contexts/timeouts, unchecked type
assertions, unsafe HTML, missing frontend labels, hard-coded colors, and
authorization decisions made only in the client.

Passing automated checks are evidence of the paths they execute, not evidence
that untested paths are correct.

## 7. Finding Model

### 7.1 Severity

- **P0, blocking:** A reachable authentication or authorization bypass; signing
  or encryption key compromise; remotely exploitable code execution; broad
  credential/session compromise; irreversible cross-account data loss; or a
  supported primary flow that cannot be completed. Public release is blocked.
- **P1, major:** A practical security weakness with meaningful impact; incorrect
  token/assertion or lifecycle behavior; persistent data-integrity risk; unsafe
  default deployment; material privacy leak; WCAG AA failure or UX defect that
  prevents a substantial user group from completing an existing critical flow;
  or a release/upgrade defect likely to break supported deployments. Fix before
  public release unless the owner explicitly accepts the exact residual risk.
- **P2, moderate:** A contained correctness edge case, defense-in-depth gap,
  confusing or inefficient flow with a workaround, incomplete audit detail,
  misleading non-critical documentation, or maintainability problem likely to
  produce future defects. Schedule with an explicit owner.
- **P3, polish:** Low-impact consistency, copy, visual, test-quality, or
  documentation improvement. Include only when concrete and worth acting on.

Severity is based on impact and reachability, not code ugliness or standards
name-dropping.

### 7.2 Confidence

- **Confirmed:** reproduced dynamically or proven by a complete reachable code
  path with no unresolved guard.
- **High:** strong evidence, but reproduction requires unavailable external
  infrastructure or a destructive environment.
- **Medium:** plausible and actionable, with one explicitly stated uncertainty.
- **Low:** kept as an investigation note, not counted in release totals.

### 7.3 Deduplication

Multiple symptoms caused by one missing invariant become one systemic finding
with all affected locations. Independently fixable failures remain separate.
Counts are based on remediation units, not the number of grep hits.

## 8. On-the-Fly Fix Boundary

The audit may implement a UX or accessibility correction only when all of these
conditions hold:

1. the intended existing behavior is unambiguous;
2. the change is local and behavior-preserving;
3. it does not alter an API, schema, protocol, authorization rule, security
   policy, dependency set, or documented capability boundary;
4. it does not mask evidence needed by another reviewer;
5. a focused regression test or deterministic static check can verify it;
6. the primary auditor has consolidated the relevant first-pass findings.

Examples include correcting an accessible name, associating an existing label,
restoring visible focus, fixing a clear localization mismatch, preventing
existing content from overflowing on a small viewport, or making an existing
error message state the next action.

Security-sensitive fixes, backend logic changes, database changes, protocol
changes, dependency upgrades, broad component redesigns, and ambiguous product
choices are never made opportunistically. They receive remediation plans first.

Every on-the-fly fix uses a failing regression test or check before the
implementation, is reviewed against the audit boundary, and is included in the
final change/evidence ledger.

## 9. Remediation Planning

Confirmed findings are grouped by shared root cause and ordered as follows:

1. containment of P0/P1 exploit or data-integrity risk;
2. authoritative invariant repair at the narrowest shared chokepoint;
3. migration or compatibility handling where persisted state is affected;
4. regression and adversarial tests;
5. observability, audit, and operator-facing failure behavior;
6. frontend recovery/error-state alignment;
7. documentation correction;
8. full release-gate verification.

Independent subsystems receive separate implementation plans so each plan
produces testable software and can be reviewed or deferred without entangling
unrelated fixes. Plans must name exact files and interfaces, specify failing
tests first, state expected command output, and contain no feature expansion.

## 10. Deliverables

The audit produces:

1. `docs/superpowers/notes/2026-07-10-public-release-final-audit.md`, containing
   the release verdict, methodology, baseline results, coverage ledgers,
   evidence-linked findings, UI health score, systemic patterns, positive
   controls, accepted limitations, documentation drift, and residual risk;
2. one dependency-ordered implementation plan per independent remediation
   subsystem under `docs/superpowers/plans/`;
3. focused UX/accessibility fixes allowed by section 8, with regression tests;
4. a final verification table distinguishing passed, failed, unavailable, and
   not-applicable checks.

## 11. Release Decision Rule

The final verdict is one of:

- **Ready:** no confirmed P0 or P1 findings remain; baseline and release gates
  pass; accepted limitations are accurately documented.
- **Conditionally ready:** no P0 remains, and every remaining P1 has an explicit
  owner-approved risk acceptance with bounded impact, compensating controls,
  and accurate documentation.
- **Not ready:** any confirmed P0 remains; an unaccepted P1 remains; critical
  verification cannot be performed; or public deployment guidance would expose
  operators to an unknown material risk.

P2/P3 findings do not automatically block release. They are assessed for
systemic accumulation and whether their combined effect invalidates a product
claim such as keyboard-first operation or safe self-hosting.

## 12. Completion Criteria

The audit is complete only when:

- every handwritten production file is assigned to and represented in a
  coverage ledger;
- every public route and CLI mutation has an identified authentication,
  authorization, validation, persistence, audit, and error-handling path;
- all P0/P1 candidates have been independently challenged;
- automated baseline failures are explained rather than merely listed;
- frontend findings include the required measurable Impeccable report;
- documentation differences are classified as code defects, documentation
  defects, or intentional accepted limitations;
- remediation plans cover every confirmed P0/P1 and actionable P2 without
  introducing new capability scope;
- the release verdict follows the decision rule in section 11.
