# Task 3 — Live Fact Loader and App Access Service

## Scope implemented

- Added `pkg/appaccess/facts.go` with generated-row projections for live account facts, including disabled-account rejection and slice-to-set normalization.
- Added `pkg/appaccess/service.go` with the required app contracts, narrow interfaces, query-only `Service`, OIDC/forward-auth and SAML evaluation, manual-group decoration, manager authorization, allowed-app listing, scoped rule preview, and account explanation.
- Added focused facts and service tests in `pkg/appaccess/facts_test.go` and `pkg/appaccess/service_test.go`.

The service keeps no policy, fact, provider, or reconciliation cache. Every standalone evaluation reloads live facts, providers, manual decision, and bound rules. `ListAllowedApps` intentionally shares one fact snapshot only within that single list operation.

## TDD record

### RED — required focused command before production files existed

Command:

```bash
go test ./pkg/appaccess -run 'TestFacts|TestService' -count=1
```

Exact output:

```text
# prohibitorum/pkg/appaccess [prohibitorum/pkg/appaccess.test]
pkg/appaccess/facts_test.go:24:16: undefined: factsFromRow
pkg/appaccess/facts_test.go:40:12: undefined: factsFromRow
pkg/appaccess/facts_test.go:41:21: undefined: ErrAccountDisabled
pkg/appaccess/service_test.go:24:14: undefined: NewService
pkg/appaccess/service_test.go:43:14: undefined: NewService
pkg/appaccess/service_test.go:66:14: undefined: NewService
pkg/appaccess/service_test.go:89:14: undefined: NewService
pkg/appaccess/service_test.go:102:12: undefined: NewService
pkg/appaccess/service_test.go:117:14: undefined: NewService
pkg/appaccess/service_test.go:118:21: undefined: ErrInvalidPolicy
pkg/appaccess/service_test.go:118:21: too many errors
FAIL	prohibitorum/pkg/appaccess [build failed]
FAIL
```

This failed for the intended reason: the requested fact loader and service did not yet exist.

### Initial full-package GREEN attempt

Command:

```bash
go test ./pkg/appaccess -count=1
```

Exact output:

```text
--- FAIL: TestServicePreviewAndExplainGroupReturnSafeProjection (0.00s)
    service_test.go:251: PreviewGroup() error = appaccess: app not found
FAIL
FAIL	prohibitorum/pkg/appaccess	0.004s
FAIL
```

Root cause: preview/explanation added an unnecessary app-row lookup before the generated scoped group query. The scoped `GetOIDCAppGroup`/`GetSAMLAppGroup` query already establishes the requested app binding; the extra lookup made the narrow preview fake require unrelated app data. The lookup was reduced to structural `AppRef` validation, retaining scoped group retrieval and non-enumerating errors.

### Focused repair verification

Command:

```bash
go test ./pkg/appaccess -run '^TestServicePreviewAndExplainGroupReturnSafeProjection$' -count=1
```

Exact output:

```text
ok  	prohibitorum/pkg/appaccess	0.002s
```

### GREEN — required focused package command

Command:

```bash
go test ./pkg/appaccess -count=1
```

Exact output:

```text
ok  	prohibitorum/pkg/appaccess	0.004s
```

No formatter, linter, build, frontend command, project-wide test, or unrelated package command was run.

### Review-driven wrong-kind regression

The read-only review found that preview/explanation must reject an OIDC `AppRef` that names a forward-auth client (or the inverse), rather than relying only on the shared OIDC-client group binding. A focused regression test was added before restoring exact `AppRef` kind validation.

RED command:

```bash
go test ./pkg/appaccess -run '^TestServicePreviewGroupRejectsWrongOIDCAppKind$' -count=1
```

Exact output:

```text
--- FAIL: TestServicePreviewGroupRejectsWrongOIDCAppKind (0.00s)
    service_test.go:286: PreviewGroup() error = <nil>, want ErrAppNotFound
FAIL
FAIL	prohibitorum/pkg/appaccess	0.002s
FAIL
```

GREEN command:

```bash
go test ./pkg/appaccess -run '^TestServicePreviewGroupRejectsWrongOIDCAppKind$' -count=1
```

Exact output:

```text
ok  	prohibitorum/pkg/appaccess	0.002s
```

Post-review focused regression command:

```bash
go test ./pkg/appaccess -count=1
```

Exact output:

```text
ok  	prohibitorum/pkg/appaccess	0.004s
```

## Acceptance-criterion self-review before commit

| Brief requirement | Review result |
| --- | --- |
| Query-facing live fact loader with generated Task 1 rows | `factsFromRow` and page-row projection use `db.GetAccountAccessFactsRow` / `db.ListActiveAccountAccessFactsPageRow`; disabled accounts return `ErrAccountDisabled`. |
| Exact app contracts and narrow protocol interfaces | `AppKind`, `AppRef`, `Scope`, `AppSummary`, `OIDCAuthorizer`, `SAMLAuthorizer`, and `AppLister` are present; compile-time assertions prove `Service` satisfies each interface. |
| Live OIDC/SAML decision evaluation | OIDC obtains the enabled client; SAML rejects disabled providers as `pgx.ErrNoRows`; both load facts, known providers, manual state, and app-bound rule groups live. |
| Manual decoration and rule preservation | Manual decisions load their app-scoped manual row into `Decision.ManualGroup`; `Decide` receives every evaluated rule match before manual precedence is applied. |
| Fail-closed malformed/unknown policy | Invalid persisted rules, manual effects, group kinds, and missing manual groups return a wrapped `ErrInvalidPolicy` with a zero-value denied decision. |
| Missing-app mapping | Evaluation returns underlying `pgx.ErrNoRows` unchanged for missing app/fact queries; disabled SAML is mapped to the same sentinel. |
| Manager authorization and non-enumeration | `admin` returns immediately; `app_manager` requires the matching generated assignment query and exact app kind; other roles, malformed refs, missing apps, wrong kinds, and unassigned assignments return `ErrAppNotFound`. |
| Allowed-app listing | Loads generated enabled OIDC, forward-auth, and launchable SAML candidate rows; evaluates all against one live fact snapshot and omits denied candidates. |
| Safe preview/explanation | Preview accepts generated pagination params, validates the exact `AppRef` kind, loads exactly one active-account-facts page, and returns only `ID`, `Username`, `DisplayName`, and `Matched`; explanation returns only Task 2 `Explanation` for an active account. Both require generated app-scoped rule-group lookups. |
| Focused test coverage | Facts normalization/disabled behavior; open, manual deny, manual allow plus claims, neutral OR, missing app, malformed rule, app isolation, SAML, manager authorization, listing, and preview/explanation are covered. |

## Review and limitations

A read-only Task 3 review completed after the initial commit and found the wrong-kind preview/explanation issue documented above; the focused regression was added and fixed. This task intentionally does not update server, protocol, UI, or legacy RBAC call sites; branch-wide compile failures from removed legacy query APIs remain outside Task 3 as directed.
