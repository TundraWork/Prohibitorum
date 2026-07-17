# Final Review Fix Report

## Scope

Addressed only the two Important final-review findings:

1. Bound VRChat response cookie input and merged/outbound cookie jars at `maxEncodedCookiesSize` before parsing, reuse, or request dispatch.
2. Canonicalize persisted diagnostic methods through a fixed HTTP verb allowlist, with `OTHER` for every other client token.

## RED evidence

Cookie regressions were added first and failed before production changes:

```text
--- FAIL: TestCookieValidateRejectsOversizedResponseWithoutExposingValue
    error type = <nil>, want cookie payload too large
--- FAIL: TestCookieMergeRejectsJarOverBudget
    mergeCookies() error type = <nil>, want cookie payload too large
--- FAIL: TestCookieOutboundJarOverBudgetRejectedBeforeRequest
    CurrentUser() error type = *vrchat.DecodeError, want cookie payload too large
```

The diagnostic regression initially failed with the untrusted method persisted verbatim:

```text
--- FAIL: TestDiagnosticCaptureCanonicalizesArbitraryMethodAfterMaintenanceShortCircuit
    method/operation/route = "PRIVATE-TOKEN"/"PRIVATE-TOKEN unmatched"/"unmatched"
```

## GREEN evidence

The new cookie regressions passed:

```text
go test ./pkg/federation/providers/vrchat -run 'TestCookie(ValidateRejectsOversizedResponseWithoutExposingValue|MergeRejectsJarOverBudget|OutboundJarOverBudgetRejectedBeforeRequest)' -count=1
go test: 1 packages ok
```

The new diagnostic regression passed:

```text
go test ./pkg/server -run TestDiagnosticCaptureCanonicalizesArbitraryMethodAfterMaintenanceShortCircuit -count=1
go test: 1 packages ok
```

Required focused and affected-package gates passed:

```text
go test ./pkg/federation/providers/vrchat ./pkg/server -run 'Cookie|DiagnosticCapture' -count=1
go test: 2 packages ok

go test ./pkg/federation/providers/vrchat ./pkg/server -count=1
go test: 2 packages ok
```

## Security and allocation review

- Response `Set-Cookie` line lengths are aggregated before cookie slice/map allocation or parsing.
- Outbound and merged jars use non-allocating length accounting for the exact `Cookie` header representation, including separators.
- The existing encoded JSON payload limit remains enforced independently.
- Origin, name, path, domain, expiry, Secure, HttpOnly, SameSite, duplicate-name, and attribute validation remain in place.
- Oversize errors are fixed generic sentinels and never interpolate cookie values.
- Tests avoid printing cookie values; failure messages report only error types and state.
- Diagnostic `Method` and the verb prefix of `Operation` can contain only a fixed canonical HTTP verb or `OTHER`; route recovery for short-circuited unknown methods uses only the fixed allowlist.
- The untracked root `package-lock.json` is intentionally excluded from the commit.

## Self-review

Reviewed the four source/test diffs and ran `git diff --check`. No whitespace errors, secret-bearing diagnostics, cookie-value error formatting, compatibility regressions, or unrelated cleanup were found. The only residual behavior is intentional: an unrecognized method may recover a path pattern through a canonical route match, but its persisted method remains `OTHER`.

## Final re-review correction

The response preflight now rejects more than the only two valid authentication cookie lines before allocating parser result capacity, and rejects empty lines during the same preflight.

RED:

```text
--- FAIL: TestCookieValidateRejectsExcessResponseLinesBeforeParsing
    error type = *errors.errorString, want cookie payload too large
```

GREEN:

```text
go test ./pkg/federation/providers/vrchat -run TestCookieValidateRejectsExcessResponseLinesBeforeParsing -count=1
go test: 1 packages ok

go test ./pkg/federation/providers/vrchat ./pkg/server -run 'Cookie|DiagnosticCapture' -count=1
go test: 2 packages ok

go test ./pkg/federation/providers/vrchat ./pkg/server -count=1
go test: 2 packages ok
```

The regression prints only the error type. It contains no cookie value, and the production errors remain fixed generic sentinels.
