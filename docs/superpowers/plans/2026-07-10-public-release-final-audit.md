# Prohibitorum Public-Release Final Audit

**Audit window:** 2026-07-10 to 2026-07-11  
**Scope:** Go IdP backend, OIDC/SAML/authentication protocols, persistence and concurrency, operator configuration, CLI/release workflows, Vue dashboard, accessibility, localization, documentation, dependencies, and supply-chain posture.  
**Verdict:** **READY WITH DOCUMENTED RESIDUAL RISKS**

No confirmed P0 or P1 defect remains. Every confirmed P1 and actionable P2 finding selected by the audit was corrected at its root, covered by a focused regression test, and exercised by the final gates below. Refuted findings were not converted into unnecessary feature work.

## Method

The audit used independent domain passes for backend/operations, protocol/security, frontend/UX, dependencies/tooling, adversarial challenge, and post-remediation review. Findings were accepted only when reproducible from source, tests, configuration, or browser behavior. Documented product limitations were checked against current code rather than assumed current.

Coverage included:

- Authentication and credential transitions, session lifecycle, sudo gates, RBAC, enrollment, recovery, pairing, and rate limiting.
- OIDC issuer/discovery/authorization/token/userinfo behavior; SAML SSO/SLO, signatures, state, replay, and metadata handling.
- SQL transaction boundaries, KV single-use semantics, concurrent consumers, audit durability, and failure behavior.
- HTTP body limits, content types, error disclosure, outbound redirects, SSRF constraints, timeouts, and response caps.
- Configuration fail-closed behavior, encryption-key handling, trusted proxy semantics, operator scripts, and CLI inputs.
- Dashboard task flows, theme determinism, action discoverability, dialog semantics, localization, 320 px reflow, contrast, and keyboard hit targets.
- Go/npm dependency state, Go toolchain patch level, workflow safety, release configuration, and vulnerability reachability.

## Finding ledger

| ID | Finding | Pre-fix | Disposition |
|---|---|---:|---|
| COR-01 | Pairing approval consumed the one-shot token before the durable session transition, so a transient DB failure could strand the user | P1 | **Fixed.** KV compare-and-delete plus DB session transaction; losing consumers cannot receive sessions; retry-after-failure and concurrency tests added. |
| UX-01 | Tailwind dark variants followed OS media while the application theme followed the `.dark` class, allowing mixed light/dark surfaces | P1 | **Fixed.** Class-driven custom dark variant; deterministic light/dark rendering under both OS preferences; contrast gate added. |
| SEC-01 | Admin JSON body controls were inconsistent | P2 | **Fixed.** Shared admin body/content-type wrapper with route-policy coverage; correctly remains independent of fresh-sudo policy. |
| SEC-02 | Reported absence of fresh-sudo on admin mutations | P1 candidate | **Refuted.** Current route registration already applies fresh sudo to high-impact mutations and intentionally leaves reversible lower-impact changes admin-only. Canonical docs now describe the actual tiers. |
| SEC-03 | Some admin failures exposed raw internal error text | P2 | **Fixed.** Stable public errors plus structured server-side logging and regression tests. |
| SEC-04 | Avatar fetch redirect behavior could escape the original destination policy | P2 | **Fixed.** Every redirect hop is revalidated by the SSRF-safe client; TLS, timeout, response-size, and content constraints remain enforced. |
| SEC-05 | SAML logout request IDs were not consumed as replay-protected state | P2 | **Fixed.** Authenticated requests now use one-shot replay protection with ordering that preserves interoperability and response signing. |
| CFG-01 | An invalid active encryption key could allow insecure startup behavior | P2 | **Fixed.** Active DEK selection fails closed; rotation semantics and explicit misconfiguration tests added. |
| CLI-01 | OIDC/SAML metadata discovery paths lacked production-equivalent caps | P2 | **Fixed.** Shared hardened HTTP behavior and bounded reads; adversarial tests cover oversized responses and unsafe redirects. |
| UX-02 | Application tile launch/menu controls overlapped and relied on hover for discoverability | P2 | **Fixed.** Independent orthogonal controls, persistent visible menu affordance, explicit layering, and accessible names. |
| UX-03 | Signing-key/JWK descriptions could overflow and had insufficient dark-mode contrast | P2 | **Fixed.** Break-all mono value, readable prose/label separation, corrected theme colors, and 320 px tests. |
| OPS-01 | Best-effort audit insert failures could disappear without operational evidence | P2 | **Fixed for observability.** `RecordOrLog` emits a safe structured error without secret-bearing detail; callsites migrated; failure/success/nil tests added. Database audit rows remain intentionally best-effort. |
| DOC-01 | `PROHIBITORUM_TRUST_PROXY` documentation no longer matched runtime configuration | P3 | **Fixed.** Removed obsolete variable/script usage; documented admin-managed client-IP strategies and trusted proxy CIDRs. |
| TOOL-01 | Local Go 1.26.4 matched GO-2026-5856's vulnerable patch range | P2 | **Fixed.** Go/mise/tool lock pinned to 1.26.5 with `GOTOOLCHAIN=local`; release and vulnerability gates run on the patched compiler. |
| AUTH-01 | Reported OIDC refresh-token family reuse weakness | P1 candidate | **Refuted after source verification.** The service already rotates refresh tokens as single-use families, returns a successor on each successful refresh, detects superseded-token reuse, and revokes the family. A short idempotency window handles benign duplicate submissions without minting a second successor. |

## Remediation plans executed

- `docs/superpowers/plans/2026-07-11-pairing-atomicity-remediation.md`
- `docs/superpowers/plans/2026-07-11-request-boundary-hardening.md`
- `docs/superpowers/plans/2026-07-11-outbound-config-hardening.md`
- `docs/superpowers/plans/2026-07-11-saml-slo-replay-hardening.md`
- `docs/superpowers/plans/2026-07-11-frontend-theme-accessibility-remediation.md`
- `docs/superpowers/plans/2026-07-11-audit-doc-toolchain-remediation.md`

The remediation also removed the unused exported error catalogue after symbol-aware reference checks, corrected stale route-tier comments and product docs, cleaned test-router warning noise, and regenerated the embedded dashboard bundle from the audited source.

The final cross-system review initially returned `CHANGES_REQUIRED` for three integration gaps: raw audit-writer errors could reach logs, chunked/unknown-length admin bodies were only lazily capped, and the new internal pairing `consumed` state leaked through the public status endpoint. Each received a failing regression first, then a root-cause fix: generic secret-safe audit failure logging, eager bounded preflight before sudo/handler execution, and server-boundary mapping of `consumed` to the existing terminal `expired` state. The same reviewer re-checked the current files and returned **APPROVED** with no Critical or Important regression.

## Frontend quality scorecard

| Dimension | Score | Evidence |
|---|---:|---|
| Accessibility | 4/4 | 31/31 contrast pairs pass; menu and protocol controls are separate; accessible labels and dialog semantics tested. |
| Performance | 4/4 | Production build succeeds; lazy route chunks retained. Known Vite notices concern modules imported both statically and dynamically, not failed splitting or correctness. |
| Responsive design | 3/4 | Targeted 320 px browser/test checks pass for remediated controls and long JWK content. Dense admin tables still intentionally use bounded/scrollable desktop-oriented layouts. |
| Theming | 4/4 | Light output is identical under light/dark OS preference; dark output is identical under light/dark OS preference; application theme alone controls rendering. |
| Anti-patterns | 4/4 | No hover-only primary action, overlapping action target, blanket warning suppression, or fake control remains in the remediated flows. |
| **Total** | **19/20 — Excellent** | Release-quality for the stated self-hosted/small-organization scope. |

## Final verification evidence

| Gate | Result |
|---|---|
| `mise exec -- go vet ./...` | PASS |
| `mise exec -- go build -tags nodynamic ./...` | PASS |
| `mise exec -- go test ./...` | PASS across all Go packages |
| `mise exec -- go test -race ./...` | PASS across all Go packages |
| `mise run ci:smoke` | PASS; fresh database migration plus end-to-end authentication, credential, session, OIDC, SAML, admin, and federation scenarios |
| `npm test` | PASS — 95 files, 650 tests, no Vue Router warning stderr |
| `npm run build` | PASS — 2,637 modules transformed; embedded `pkg/webui/dist` regenerated |
| Isolated/current bundle comparison | PASS — current generated bundle byte-matches the isolated audited build |
| `node scripts/check-contrast.mjs` | PASS — 31/31 pairs |
| Browser matrix | PASS — explicit light/dark class is independent of light/dark OS preference; 320 px JWK has no horizontal overflow; tile controls remain separate and the menu is the top hit target |
| `govulncheck ./...` | PASS — 0 reachable vulnerabilities; 0 in imported packages; four module-only advisories are not called by this code |
| `mise run ci:lint-actions` | PASS — actionlint and zizmor report no findings (documented ignores/suppression only) |
| `mise run ci:release-check` | PASS — GoReleaser configuration validated |
| Shell syntax (`scripts/dev-federation.sh`, `scripts/dev-forward-auth.sh`) | PASS |
| Final independent remediation review | **APPROVED** — all three last-review root causes closed; no Critical or Important regression in the reviewed scope |

The release workflow itself was linted and its GoReleaser configuration validated; no artifact publication was attempted during the audit.

## Residual accepted risks

These are current design tradeoffs, not hidden defects:

1. **Database audit rows are best-effort.** A failed audit insert is now always represented by safe structured error telemetry, but it is not transactionally coupled to the protected mutation. Operators requiring immutable compliance evidence must collect and retain structured logs or add an external transactional audit pipeline.
2. **Administrative list APIs are sized for the documented small-organization deployment profile.** They use bounded operational assumptions rather than full cursor pagination everywhere.
3. **Outbound fetching permits validated public redirects.** Each hop is SSRF-filtered and bounded, but availability still depends on external endpoints and deployment DNS/network policy.
4. **Some API human-readable error text remains Chinese while machine-readable codes are stable.** The bundled UI localizes from codes. Fully localized third-party API prose would be a new compatibility/product feature, not a correctness fix.

## Release decision

**Ready for public release within the documented self-hosted and small-organization operating envelope.**

Release prerequisites are now concrete: use the pinned Go 1.26.5 toolchain, commit the regenerated embedded dashboard bundle with its source changes, retain the documented trusted-proxy and TLS deployment requirements, and configure structured-log retention if audit evidence is operationally significant.
