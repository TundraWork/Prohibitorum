# Task 2 Report: Restrict NOT to Fact Conditions

## RED

- Added parser coverage for each supported fact leaf under `not.child`: `connection.provider`, `connection.protocol`, `login_method`, and `avatar`.
- Added rejection coverage for `not` wrapping `all`, `any`, or `not`, requiring `$.condition.child` with reason `not_requires_fact`.
- Ran:

  ```sh
  go test ./pkg/appaccess -run 'ParseAndValidateRule.*Not|NotRequiresFact' -count=1 -v
  ```

- Result before implementation: failed as expected. Each new non-fact child test received no error because nested/group NOT forms were still accepted.

## GREEN

- `validateCombinator` now recognizes `all`, `any`, and `not` in a NOT child and returns `ruleError(path+".child", "not_requires_fact")` before recursive validation.
- Malformed and unknown operators continue through normal validation, preserving their existing `invalid_shape` and `invalid_op` behavior.
- Retained boundary coverage for maximum child count and replaced depth nesting with valid `all` nesting because nested NOT is no longer valid.
- Added handler coverage proving NOT-to-ALL produces `invalid_group_rule` with `{path:"$.condition.child", reason:"not_requires_fact"}` and a leaf-NOT group remains creatable.
- Existing demo seed test continues to assert exact persisted ASTs for `demo-no-user-avatar` and `demo-trusted-federated-profile`, and previews both in the seeded evaluation matrix.
- Ran:

  ```sh
  go test ./pkg/appaccess ./pkg/server ./cmd/prohibitorum -run 'Rule|Not|Managed.*Rule|SeedAppPolicyDemo' -count=1 -v
  ```

- Result: passed (`3 packages ok`; 4 database-dependent command-package tests skipped because their test database setting was absent).

## Files

- `pkg/appaccess/rule.go`
- `pkg/appaccess/rule_test.go`
- `pkg/server/handle_app_policies_test.go`
- `cmd/prohibitorum/dev_seed_app_policy_test.go` retained unchanged because it already supplies the required exact-AST and preview coverage.

## Self-Review

- Reviewed against Task 2 requirements after the focused GREEN run.
- Corrected validation precedence so only recognized nested combinators yield `not_requires_fact`; `op:null` and unknown operators preserve their prior validation outcomes.
- Restored the existing maximum-children boundary test that was inadvertently displaced while replacing the now-invalid nested-NOT depth fixture.
- No remaining findings.
