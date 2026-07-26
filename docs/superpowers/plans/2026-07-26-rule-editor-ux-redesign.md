# Rule Editor UX Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the AST-shaped application rule editor with a slim, hierarchy-first visual builder, canonical JSON mode, plain-language/expression rendering, unsaved impact preview, and review-before-save workflow.

**Architecture:** Keep the existing version-1 rule wire contract and policy semantics. Extract all draft transformations and rendering into pure TypeScript, add one read-only backend endpoint that evaluates an unsaved validated rule against a single active-account snapshot, then compose focused Vue components behind one `RuleEditor` interface used by both create and edit flows.

**Tech Stack:** Go 1.26, pgx v5, sqlc 1.30, Chi, Vue 3.5, TypeScript 5, Reka UI/shadcn-vue primitives, Tailwind CSS 4, Vitest 2, Vue Test Utils, vue-i18n 10.

## Global Constraints

- Persist the existing version-1 JSON envelope and fact vocabulary; narrow `not` so its child must be one fact leaf. Do not add a persisted expression language or change access-decision semantics.
- One outer editor surface; nested hierarchy uses slim text-labelled rails, indentation, typography, and `AND`/`OR` connector text—not recursive cards or rounded structural boxes.
- `ALL` and `ANY` are visible group controls; color never carries mode alone.
- Remove the sentence “Match an account when all of the following are true.” Every group, including root, owns its own description.
- `ALL`: “Every condition in this group must be true.” `ANY`: “At least one condition in this group must be true.”
- Each predicate has a visible `is` / `is not` middle control. JSON encodes `is not` as one `not` node wrapping exactly one fact leaf. Group-level and nested NOT are invalid.
- `ALL ↔ ANY` and `is ↔ is not` preserve fact values and group children. Every remove, move, and undo operation preserves untouched subtrees and never silently discards content.
- New conditions start incomplete; never default to a broad valid condition such as any avatar.
- Advanced mode is `Visual builder | JSON`; JSON is editable and persisted, the fully-parenthesized expression is read-only and derived.
- Do not add a code-editor or syntax-highlighting dependency. Use a native textarea, synchronized line-number gutter, IBM Plex Mono, and exact parse/semantic errors.
- Invalid JSON never overwrites the last valid shared rule or silently disappears on mode switch.
- Draft preview performs no writes, returns exact `matchedCount`, omits disabled accounts, and uses the same manager/admin authorization and rule validation as persisted policy.
- Workspace provider descriptors become `{ slug, displayName }`; visual labels use display name, JSON/expression uses slug.
- `Slug` and downstream claim exposure move under accessible Advanced options. New slug auto-generation stops permanently after manual slug editing.
- Save occurs only from Review and save. If current impact preview fails, saving without preview requires a separate explicit confirmation.
- Dirty create/edit drafts survive preview and require confirmation before cancel, close, switching rules, or navigation.
- WCAG 2.2 AA and keyboard-first operation are binding. Drag-and-drop may not be the only reordering mechanism.
- English and Chinese locale keys change together. Keep `en.ts` free of U+2018/U+2019 apostrophe hazards and vue-i18n-invalid raw `@` characters.
- Do not whole-file format unrelated Go/Vue code. Rebuild `pkg/webui/dist` only after the source implementation and browser smoke work.

---

## File Structure

### Backend

- Modify `db/queries/rbac.sql`: add one all-active-account access-facts query for one-statement snapshot evaluation.
- Modify generated `pkg/db/rbac.sql.go`, `pkg/db/querier.go`: regenerate with sqlc.
- Modify `db/queries/upstream_idp.sql` and generated `pkg/db/upstream_idp.sql.go`: add safe provider slug/display-name descriptors.
- Modify `pkg/appaccess/service.go`: evaluate a validated unsaved rule against all active account facts.
- Modify `pkg/appaccess/service_test.go`: rule-preview evaluation and app-kind isolation.
- Modify `pkg/contract/appaccess.go`: provider display name and draft-preview response contract.
- Modify `pkg/server/handle_managed_applications.go`: route/interface registration and provider projection.
- Modify `pkg/server/handle_app_policies.go`: draft-preview request validation, cursor binding, exact count, response.
- Modify `pkg/server/handle_app_policies_test.go`, `pkg/server/handle_managed_applications_test.go`, `pkg/server/admin_route_policy_test.go`: endpoint, authorization, body controls, cursor, provider descriptor, and no-write coverage.

### Frontend model

- Create `dashboard/src/lib/ruleDraft.ts`: draft construction, cloning, path addressing, transforms, validation, JSON parsing/formatting, outline/expression, slug generation.
- Create `dashboard/src/lib/ruleDraft.test.ts`: exhaustive pure behavior tests.
- Modify `dashboard/src/lib/appAccess.ts`: provider display name and draft-preview response types.

### Frontend UI

- Create `dashboard/src/components/custom/RulePredicateRow.vue` and `.test.ts`.
- Create `dashboard/src/components/custom/RuleGroupEditor.vue` and `.test.ts`.
- Create `dashboard/src/components/custom/RuleVisualBuilder.vue` and `.test.ts`.
- Create `dashboard/src/components/custom/RuleJsonEditor.vue` and `.test.ts`.
- Create `dashboard/src/components/custom/RuleMeaning.vue` and `.test.ts`.
- Create `dashboard/src/components/custom/RuleImpactPreview.vue` and `.test.ts`.
- Create `dashboard/src/components/custom/RuleEditor.vue` and `.test.ts`.
- Modify `dashboard/src/components/custom/AppPolicyWorkspace.vue` and `.test.ts`: use the shared editor for create/edit and retain persisted-row preview/explain.
- Remove `dashboard/src/components/custom/RuleConditionEditor.vue` and `.test.ts` after all callsites/tests migrate.
- Modify `dashboard/src/locales/en.ts`, `dashboard/src/locales/zh.ts`: exact new vocabulary, errors, statuses, review, JSON help.
- Rebuild `pkg/webui/dist` at the final done gate.

---

### Task 1: Pure rule draft model, transformations, and renderers

**Files:**
- Create: `dashboard/src/lib/ruleDraft.ts`
- Create: `dashboard/src/lib/ruleDraft.test.ts`
- Modify: `dashboard/src/lib/appAccess.ts`

**Interfaces:**

```ts
export type RulePathSegment = number | 'child'
export type RulePath = readonly RulePathSegment[]
export type GroupMode = 'all' | 'any'

export interface RuleValidationIssue {
  path: string
  reason: string
  messageKey: string
}

export interface ParsedRuleJSON {
  ok: true
  rule: Rule
  formatted: string
}
export interface InvalidRuleJSON {
  ok: false
  source: string
  path: string
  reason: string
  line?: number
  column?: number
}

export interface RuleOutlineNode {
  kind: 'all' | 'any' | 'predicate'
  text: string
  negated: boolean
  children: RuleOutlineNode[]
  leafCount: number
  groupCount: number
}

export function makeEmptyRule(): Rule
export function cloneRule(rule: Rule): Rule
export function validateRule(rule: Rule, providers: ReadonlySet<string>): RuleValidationIssue[]
export function conditionAtPath(rule: Rule, path: RulePath): Condition | undefined
export function addPredicate(rule: Rule, parent: RulePath): Rule
export function addNestedGroup(rule: Rule, parent: RulePath): Rule
export function setGroupMode(rule: Rule, path: RulePath, mode: GroupMode): Rule
export function setPredicateNegated(rule: Rule, path: RulePath, negated: boolean): Rule
export function removeNode(rule: Rule, path: RulePath): { rule: Rule; removed: Condition; focusPath: RulePath }
export function restoreNode(rule: Rule, parent: RulePath, index: number, removed: Condition): Rule
export function moveNode(rule: Rule, path: RulePath, delta: -1 | 1): Rule
export function updatePredicateFact(rule: Rule, path: RulePath, fact: NonNullable<Condition['fact']>): Rule
export function updatePredicateValue(rule: Rule, path: RulePath, value: string): Rule
export function parseRuleJSON(source: string, providers: ReadonlySet<string>): ParsedRuleJSON | InvalidRuleJSON
export function formatRuleJSON(rule: Rule): string
export function ruleExpression(rule: Rule): string
export function ruleOutline(rule: Rule, providerLabels: ReadonlyMap<string, string>): RuleOutlineNode
export function slugFromDisplayName(value: string): string
```

Extend frontend types:

```ts
import type { Page } from '@/lib/pagination'

export interface ProviderDescriptor { slug: string; displayName: string }
export interface RulePreviewPage extends Page<GroupPreview> { matchedCount: number }
```

- [ ] **Step 1: Write failing transformation tests**

Cover:

```ts
expect(setGroupMode(allRule, [], 'any').condition).toEqual({ op: 'any', children: originalChildren })
expect(setPredicateNegated(leafRule, [], true).condition).toEqual({ op: 'not', child: originalLeaf })
expect(setPredicateNegated(negativeLeafRule, [], false).condition).toEqual(originalLeaf)
expect(addPredicate(leafRule, []).condition).toEqual({
  op: 'all',
  children: [originalLeaf, {}],
})
expect(moveNode(threeChildRule, [2], -1).condition.children).toEqual([
  firstChild,
  thirdChild,
  secondChild,
])
```

Also assert every function returns a new rule and leaves the input deeply unchanged.

- [ ] **Step 2: Run transformation tests RED**

Run:

```bash
cd dashboard && npm test -- --run src/lib/ruleDraft.test.ts
```

Expected: FAIL because `ruleDraft.ts` does not exist.

- [ ] **Step 3: Implement immutable path and transformation primitives**

Use closed `Condition` cloning only. `makeEmptyRule()` returns:

```ts
{ version: 1, condition: { op: 'all', children: [{}] } }
```

An incomplete `{}` exists only in the frontend draft and fails validation until completed. Existing persisted root leaves remain supported; adding a sibling to a root leaf wraps both leaves in `ALL`.

- [ ] **Step 4: Write failing validation and JSON round-trip tests**

Assert the same version/fact/operator/provider/depth/node/child rules as the revised `pkg/appaccess/rule.go`, including unknown JSON fields, `null`, trailing JSON, valid leaf NOT, rejected group-level NOT, rejected nested NOT, and JSON parse line/column extraction. Assert invalid JSON leaves the caller’s previous valid rule untouched.

- [ ] **Step 5: Implement closed validator and canonical JSON helpers**

`parseRuleJSON` must parse with a closed-key walk after `JSON.parse`; JavaScript’s parser rejects malformed/trailing JSON. Extract line/column from native syntax errors when present. Semantic failures return JSON path and stable reason matching backend vocabulary.

- [ ] **Step 6: Write failing expression, outline, and slug tests**

Expected expression for the approved nested example:

```text
(provider("downstream") && protocol("oidc") && (federation || passkey) && (!(password_totp)))
```

Every ALL/ANY combinator is parenthesized. Negative leaves emit `!(leaf)` and no group/nested NOT is accepted. Outline nodes retain explicit ALL/ANY labels, `is not` predicate wording, and provider display names. Slug generation lowercases, converts non-alphanumeric runs to one hyphen, trims hyphens, and never emits characters outside `^[a-z0-9](-?[a-z0-9])*$`.

- [ ] **Step 7: Implement expression, outline, and slug generation**

Use stable fact functions:

```text
provider("slug")
protocol("oidc")
passkey
password_totp
federation
avatar_any
avatar_user_uploaded
```

- [ ] **Step 8: Run pure tests GREEN and typecheck**

```bash
cd dashboard && npm test -- --run src/lib/ruleDraft.test.ts && npm run build
```

Expected: all ruleDraft tests PASS; build exits 0.

- [ ] **Step 9: Commit**

```bash
git add dashboard/src/lib/ruleDraft.ts dashboard/src/lib/ruleDraft.test.ts dashboard/src/lib/appAccess.ts
git commit -m "feat(policy-ui): add rule draft model"
```

### Task 2: Narrow backend NOT validation to one fact leaf

**Files:**
- Modify: `pkg/appaccess/rule.go`
- Modify: `pkg/appaccess/rule_test.go`
- Modify: `cmd/prohibitorum/dev_seed_app_policy_test.go`
- Modify: `pkg/server/handle_app_policies_test.go`

**Interfaces:**

The version-1 JSON shape remains `{"op":"not","child":...}`. `not.child` must be one fact leaf and must not contain `op`, `children`, or `child`. Add stable validation reason `not_requires_fact` at the NOT node’s `.child` path when the child is `all`, `any`, or `not`.

- [ ] **Step 1: Write failing parser tests**

Assert NOT-wrapped provider/protocol/login/avatar leaves parse. Assert NOT→ALL, NOT→ANY, and NOT→NOT fail with exact path `$.condition.child` and reason `not_requires_fact`. Keep unknown-field/null behavior unchanged. In `cmd/prohibitorum/dev_seed_app_policy_test.go`, keep exact-AST assertions proving `demo-no-user-avatar` and `demo-trusted-federated-profile` use valid leaf NOT nodes and still seed/preview successfully.

- [ ] **Step 2: Run parser tests RED**

```bash
go test ./pkg/appaccess -run 'ParseAndValidateRule.*Not|NotRequiresFact' -count=1 -v
```

Expected: group/nested NOT cases currently parse, so the new tests FAIL.

- [ ] **Step 3: Enforce the leaf-only invariant**

In `validateCombinator`, after confirming `child` is present, reject a child wire node whose `Op.present` is true with `ruleError(path+".child", "not_requires_fact")`; validate a fact child normally. Keep depth/node accounting and evaluator behavior unchanged.

- [ ] **Step 4: Add handler mapping coverage**

Creating or draft-previewing NOT→ALL returns `invalid_group_rule` with `{path:"$.condition.child",reason:"not_requires_fact"}`. NOT→fact remains accepted.

- [ ] **Step 5: Run focused tests GREEN**

```bash
go test ./pkg/appaccess ./pkg/server ./cmd/prohibitorum -run 'Rule|Not|Managed.*Rule|SeedAppPolicyDemo' -count=1 -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/appaccess pkg/server/handle_app_policies_test.go cmd/prohibitorum/dev_seed_app_policy_test.go
git commit -m "feat(policy): restrict NOT to fact conditions"
```

### Task 3: Provider descriptors and unsaved draft-preview API

**Files:**
- Modify: `db/queries/upstream_idp.sql`
- Modify: `db/queries/rbac.sql`
- Regenerate: `pkg/db/upstream_idp.sql.go`, `pkg/db/rbac.sql.go`, `pkg/db/querier.go`, `pkg/db/models.go` if sqlc changes it
- Modify: `pkg/appaccess/service.go`
- Modify: `pkg/appaccess/service_test.go`
- Modify: `pkg/contract/appaccess.go`
- Modify: `pkg/server/handle_managed_applications.go`
- Modify: `pkg/server/handle_app_policies.go`
- Modify: `pkg/server/handle_app_policies_test.go`
- Modify: `pkg/server/handle_managed_applications_test.go`
- Modify: `pkg/server/admin_route_policy_test.go`

**Interfaces:**

SQL:

```sql
-- name: ListKnownUpstreamIDPDescriptors :many
SELECT slug, display_name FROM upstream_idp ORDER BY display_name, slug;

-- name: ListActiveAccountAccessFacts :many
SELECT
  a.id,
  a.username,
  a.display_name,
  a.disabled,
  EXISTS (SELECT 1 FROM webauthn_credential w WHERE w.account_id = a.id) AS has_passkey,
  EXISTS (
    SELECT 1 FROM password_credential p
    WHERE p.account_id = a.id
      AND EXISTS (
        SELECT 1 FROM totp_credential t
        WHERE t.account_id = a.id AND t.confirmed_at IS NOT NULL
      )
  ) AS has_password_totp,
  EXISTS (
    SELECT 1 FROM account_identity ai
    JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
    WHERE ai.account_id = a.id
      AND ai.confirmed_at IS NOT NULL
      AND NOT ip.disabled
      AND ip.protocol <> 'vrchat'
  ) AS has_federation,
  ARRAY(
    SELECT DISTINCT ip.slug
    FROM account_identity ai
    JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
    WHERE ai.account_id = a.id AND ai.confirmed_at IS NOT NULL
    ORDER BY ip.slug
  )::text[] AS confirmed_provider_slugs,
  ARRAY(
    SELECT DISTINCT ip.protocol
    FROM account_identity ai
    JOIN upstream_idp ip ON ip.id = ai.upstream_idp_id
    WHERE ai.account_id = a.id AND ai.confirmed_at IS NOT NULL
    ORDER BY ip.protocol
  )::text[] AS confirmed_protocols,
  EXISTS (SELECT 1 FROM account_avatar av WHERE av.account_id = a.id) AS has_any_avatar,
  EXISTS (
    SELECT 1 FROM account_avatar av
    WHERE av.account_id = a.id AND av.source = 'user'
  ) AS has_user_avatar
FROM account a
WHERE NOT a.disabled
ORDER BY a.username, a.id;
```

Service:

```go
PreviewRule(context.Context, AppRef, Rule) ([]GroupPreview, error)
```

Contract:

```go
type ProviderDescriptorView struct {
    Slug        string `json:"slug"`
    DisplayName string `json:"displayName"`
}

type RulePreviewPageView struct {
    Items        []GroupPreviewView `json:"items"`
    MatchedCount int                `json:"matchedCount"`
    NextCursor   string             `json:"nextCursor"`
}
```

Request wire:

```go
type previewManagedRuleBody struct {
    Version   int             `json:"version"`
    Condition json.RawMessage `json:"condition"`
    Cursor    string          `json:"cursor"`
    Limit     int             `json:"limit"`
}
```

- [ ] **Step 1: Write failing service tests for unsaved rule preview**

In `pkg/appaccess/service_test.go`, assert:

- `PreviewRule` validates OIDC/forward-auth/SAML app kind through `validateAppRef`.
- It loads known providers and one all-active-account snapshot.
- It evaluates the supplied rule without loading a group or writing anything.
- Disabled accounts are absent because the query contract excludes them.
- The returned order is username/id order from the query.

- [ ] **Step 2: Add all-active-facts and provider-descriptor SQL, regenerate sqlc**

Duplicate the exact fact projection from `ListActiveAccountAccessFactsPage`; remove only cursor/limit clauses. Run:

```bash
mise exec -- sqlc generate
```

Expected: `ListActiveAccountAccessFacts` and `ListKnownUpstreamIDPDescriptors` generated; no errors.

- [ ] **Step 3: Implement `PreviewRule`**

Validate `ref`, load provider slugs, validate the supplied typed rule by marshaling and calling `ParseAndValidateRule`, load the one-statement fact snapshot, and evaluate each row with `EvaluateCondition`. Do not accept a group ID.

- [ ] **Step 4: Run service tests GREEN**

```bash
go test ./pkg/appaccess -run 'PreviewRule|PreviewGroup' -count=1 -v
```

Expected: PASS.

- [ ] **Step 5: Write failing HTTP tests**

Cover:

- route registration and app-manager auth;
- foreign manager/nonexistent/wrong-kind app returns non-enumerating `client_not_found`;
- JSON content-type and 64 KiB body controls;
- unknown request fields and malformed JSON rejected;
- invalid rule returns `invalid_group_rule` with exact `path` and `reason`;
- success returns all-page `matchedCount`, requested page, and cursor;
- cursor is bound to app kind/id and SHA-256 of canonical rule, so a cursor cannot paginate another draft;
- `limit` defaults to existing app-policy page size and remains within existing bounds;
- no create/update/audit write occurs;
- provider descriptors include disabled/invite-only rows and `{slug, displayName}`.

- [ ] **Step 6: Register and implement the endpoint**

Register with body controls but no sudo gate:

```go
s.registerAdminBodyOpHTTP(router, http.MethodPost,
    base+"/{kind}/{appId}/rule-preview", req,
    s.handlePreviewManagedRuleHTTP)
```

Decode with `json.Decoder.DisallowUnknownFields()` and reject trailing JSON. Build the raw closed rule with `json.Marshal(map[string]any{"version": body.Version, "condition": body.Condition})`; `json.RawMessage` preserves nested fields so the closed rule validator still rejects unknown members. Validate/canonicalize with the same provider set as writes. Compute `digest := sha256.Sum256(canonicalRule)` and bind the cursor with collection `managed_rule_preview`, sort `username`, filters from `managedAppCursorFilters(app.ref)` plus `ruleHash = hex.EncodeToString(digest[:])`. Call `PreviewRule` once, calculate `matchedCount` over the full result, locate the first row strictly after decoded `(username,id)` keys, return at most clamped `limit` rows, and encode the last returned row as the next cursor when more remain. One SQL statement supplies the full snapshot.

- [ ] **Step 7: Project provider display names**

Use `ListKnownUpstreamIDPDescriptors` in the workspace handler. Keep `ListKnownUpstreamIDPSlugs` for evaluator validation; do not make display names part of persisted rules.

- [ ] **Step 8: Run focused server tests and build**

```bash
go test ./pkg/server ./pkg/appaccess -run 'Managed.*Rule|PreviewRule|ProviderDescriptor|BodyControls' -count=1 -v
go build ./...
```

Expected: PASS/build exit 0.

- [ ] **Step 9: Commit**

```bash
git add db/queries pkg/db pkg/appaccess pkg/contract/appaccess.go pkg/server
git commit -m "feat(policy): preview unsaved access rules"
```

### Task 4: Slim visual hierarchy builder

**Files:**
- Create: `dashboard/src/components/custom/RulePredicateRow.vue`
- Create: `dashboard/src/components/custom/RulePredicateRow.test.ts`
- Create: `dashboard/src/components/custom/RuleGroupEditor.vue`
- Create: `dashboard/src/components/custom/RuleGroupEditor.test.ts`
- Create: `dashboard/src/components/custom/RuleVisualBuilder.vue`
- Create: `dashboard/src/components/custom/RuleVisualBuilder.test.ts`
- Modify: `dashboard/src/locales/en.ts`
- Modify: `dashboard/src/locales/zh.ts`

**Interfaces:**

```ts
// RuleVisualBuilder.vue
props: {
  modelValue: Rule
  providers: ProviderDescriptor[]
  issues: RuleValidationIssue[]
  maxDepth: number
  maxNodes: number
  maxChildren: number
}
emits: {
  'update:modelValue': [Rule]
  'announce': [string]
}
```

Child components receive the root rule plus immutable `RulePath`; they emit named commands upward rather than mutating nested props.

- [ ] **Step 1: Load the Impeccable UI implementation floor**

Before editing Vue templates, read `skill://impeccable/reference/craft-floor.md` and the Operate guidance referenced by Impeccable. Treat PRODUCT.md/DESIGN.md as binding; do not refresh their stale sidecars in this task.

- [ ] **Step 2: Write failing predicate-row tests**

Assert:

- fact selector contains only provider/protocol/login/avatar, never ALL/ANY/NOT;
- clause reads fact → `is` / `is not` → value;
- provider option shows display name and slug;
- incomplete row shows exact linked error;
- polarity control calls `setPredicateNegated` while preserving fact/value; move and remove actions use content-specific accessible names;
- keyboard selection emits immutable rule update.

- [ ] **Step 3: Implement `RulePredicateRow.vue`**

Use existing Select, Button, DropdownMenu, and Tooltip primitives. Rounded treatment belongs to the fact, polarity, and value controls only; the row has no enclosing rounded card.

- [ ] **Step 4: Write failing group-editor tests**

Assert:

- ALL/ANY rail button text and description copy; no NOT rail exists;
- explicit AND/OR connector text between children;
- one slim rail and indentation per group, no recursive `.rounded-*` structural container;
- ALL/ANY rail transformations preserve exact children;
- positive and negative predicates render `is` / `is not` and preserve value when polarity changes;
- group-level/nested NOT input produces the exact shared validation error and never renders as a group;
- Add condition and Add nested group only;
- disabled add action explains max depth/node/child reason;
- add/remove/move focus and live announcement contracts.

- [ ] **Step 5: Implement `RuleGroupEditor.vue` recursively**

The recursive component renders only ALL/ANY logical groups; structural CSS remains rail/indent/divider only. A valid JSON `not` node is consumed by `RulePredicateRow` as `is not` polarity and never creates a structural group. Use text plus restrained semantic tint; do not use color as the only differentiator.

- [ ] **Step 6: Implement root visual builder and undo**

`RuleVisualBuilder` renders persisted root leaves, root groups, and incomplete new rules. A root leaf exposes Add condition, which wraps it into ALL with a new incomplete sibling. Store the most recent removed subtree and insertion coordinates for one-level Undo; announce restore/removal.

- [ ] **Step 7: Add exact English/Chinese visual-builder copy**

Add keys for ALL/ANY descriptions, connector labels, `is` / `is not`, add actions, move/remove, limits, Undo, and accessible names. Delete Leaf and group-NOT terminology from active UI copy.

- [ ] **Step 8: Run visual-builder tests and build**

```bash
cd dashboard && npm test -- --run \
  src/components/custom/RulePredicateRow.test.ts \
  src/components/custom/RuleGroupEditor.test.ts \
  src/components/custom/RuleVisualBuilder.test.ts
npm run build
```

Expected: PASS/build exit 0.

- [ ] **Step 9: Commit**

```bash
git add dashboard/src/components/custom/RulePredicateRow* \
  dashboard/src/components/custom/RuleGroupEditor* \
  dashboard/src/components/custom/RuleVisualBuilder* \
  dashboard/src/locales/en.ts dashboard/src/locales/zh.ts
git commit -m "feat(policy-ui): add visual rule builder"
```

### Task 5: JSON mode and derived meaning

**Files:**
- Create: `dashboard/src/components/custom/RuleJsonEditor.vue`
- Create: `dashboard/src/components/custom/RuleJsonEditor.test.ts`
- Create: `dashboard/src/components/custom/RuleMeaning.vue`
- Create: `dashboard/src/components/custom/RuleMeaning.test.ts`
- Modify: `dashboard/src/locales/en.ts`
- Modify: `dashboard/src/locales/zh.ts`

**Interfaces:**

```ts
// RuleJsonEditor.vue
props: {
  source: string
  parsedRule: Rule
  error?: InvalidRuleJSON
  expression: string
}
emits: {
  'update:source': [string]
  'format': []
  'copy-json': []
  'copy-expression': []
  'escape-tab': []
}

// RuleMeaning.vue
props: { rule: Rule; providers: ProviderDescriptor[]; compact?: boolean }
```

- [ ] **Step 1: Write failing JSON-editor tests**

Assert synchronized line numbers/scroll, IBM Plex Mono class, `wrap="off"`, Tab inserts two spaces at selection, Escape then Tab exits, Format/Copy actions, parse line/column, semantic path, no raw error, and read-only expression with Copy.

- [ ] **Step 2: Implement native JSON editor**

Use Textarea only if it exposes selection/scroll needed; otherwise use a native `<textarea>` styled with project tokens. The gutter is `aria-hidden`; the textarea owns the label and error relationship. Do not add dependencies.

- [ ] **Step 3: Write failing meaning tests**

Cover positive leaf, negative leaf rendered with `is not`, ALL, ANY, unknown provider fallback to slug, compact saved-row summary, and full sentence outline. Assert group/nested NOT is rejected by the shared validator and the removed root sentence never renders.

- [ ] **Step 4: Implement `RuleMeaning.vue`**

Render prose/outline, not chips or recursive boxes. Use semantic nested lists for deep outlines. Compact mode returns first line, leaf count, and nested-group count.

- [ ] **Step 5: Add JSON/meaning locale copy and run tests**

```bash
cd dashboard && npm test -- --run \
  src/components/custom/RuleJsonEditor.test.ts \
  src/components/custom/RuleMeaning.test.ts \
  src/lib/ruleDraft.test.ts
npm run build
```

Expected: PASS/build exit 0.

- [ ] **Step 6: Commit**

```bash
git add dashboard/src/components/custom/RuleJsonEditor* \
  dashboard/src/components/custom/RuleMeaning* \
  dashboard/src/locales/en.ts dashboard/src/locales/zh.ts
git commit -m "feat(policy-ui): add JSON rule editing"
```

### Task 6: Draft impact preview and shared RuleEditor workflow

**Files:**
- Create: `dashboard/src/components/custom/RuleImpactPreview.vue`
- Create: `dashboard/src/components/custom/RuleImpactPreview.test.ts`
- Create: `dashboard/src/components/custom/RuleEditor.vue`
- Create: `dashboard/src/components/custom/RuleEditor.test.ts`
- Modify: `dashboard/src/locales/en.ts`
- Modify: `dashboard/src/locales/zh.ts`

**Interfaces:**

```ts
export interface RuleEditorDraft {
  slug: string
  displayName: string
  description: string
  exposedToDownstream: boolean
  rule: Rule
}

// RuleEditor.vue
props: {
  initialDraft: RuleEditorDraft
  providers: ProviderDescriptor[]
  previewEndpoint: string
  busy: boolean
  serverError?: ApiError
  mode: 'create' | 'edit'
}
emits: {
  save: [RuleEditorDraft]
  cancel: []
  'dirty-change': [boolean]
}
```

- [ ] **Step 1: Write failing impact-preview tests**

Mock POST preview and assert:

- 300 ms debounce only for valid drafts;
- stale response ignored after a later draft/request;
- current request body includes version, condition, cursor, limit;
- exact `matchedCount`, items, and pagination;
- invalid draft retains previous results marked out-of-date;
- failure shows Retry and marks results stale;
- screen-reader status announces loading/current/stale states.

- [ ] **Step 2: Implement `RuleImpactPreview.vue`**

Use a dedicated `useApi` instance and monotonically increasing request version. Pagination POSTs the same canonical rule with returned cursor. Render existing safe account summary/matched result vocabulary; do not expose facts.

- [ ] **Step 3: Write failing RuleEditor workflow tests**

Cover:

- initial incomplete condition and disabled Review;
- Visual/JSON segmented mode using existing `SegmentedControl`;
- valid JSON updates shared visual rule; invalid JSON preserves last valid rule and blocks Visual switch with focus/error;
- display-name slug generation until manual slug edit;
- Advanced disclosure (`aria-expanded`, `aria-controls`) contains slug and claims exposure;
- plain meaning and impact visible beside builder, stacked narrow;
- `Review and save` moves to review, not API save;
- preview failure requires explicit Save without preview confirmation;
- save emits exact closed payload only from review;
- Saving/status/success focus behavior;
- Cancel/close dirty confirmation and beforeunload only while dirty;
- server error preserves draft and appears locally.

- [ ] **Step 4: Implement `RuleEditor.vue`**

Own local draft, JSON buffer, last valid rule, editor mode, review state, dirty baseline, and one-level structural undo. Use standard Dialog primitives for dirty and save-without-preview confirmations; destructive action gets initial-safe focus. Use `StatusMessage` for saved announcements.

- [ ] **Step 5: Implement responsive slim composition**

Wide: builder and review panel columns separated by one divider. Narrow: builder then meaning/impact. Cap visual indent per depth; stack predicate fact/value. Rounded boxes remain limited to outer surface, editable controls, and impact panel.

- [ ] **Step 6: Add workflow copy and run component tests**

```bash
cd dashboard && npm test -- --run \
  src/components/custom/RuleImpactPreview.test.ts \
  src/components/custom/RuleEditor.test.ts
npm run build
```

Expected: PASS/build exit 0.

- [ ] **Step 7: Commit**

```bash
git add dashboard/src/components/custom/RuleImpactPreview* \
  dashboard/src/components/custom/RuleEditor* \
  dashboard/src/locales/en.ts dashboard/src/locales/zh.ts
git commit -m "feat(policy-ui): add reviewed rule workflow"
```

### Task 7: Workspace cutover, persisted summaries, and regression coverage

**Files:**
- Modify: `dashboard/src/components/custom/AppPolicyWorkspace.vue`
- Modify: `dashboard/src/components/custom/AppPolicyWorkspace.test.ts`
- Remove: `dashboard/src/components/custom/RuleConditionEditor.vue`
- Remove: `dashboard/src/components/custom/RuleConditionEditor.test.ts`
- Modify: `dashboard/src/pages/ManagedApplicationDetailView.test.ts` if provider/result shape assertions require it

**Interfaces:**
- Consumes `RuleEditorDraft` save event and existing POST/PUT group endpoints.
- Provides `previewEndpoint = ${basePath}/rule-preview`.
- Keeps persisted GET group preview/explain routes unchanged for saved-row inspection.

- [ ] **Step 1: Write failing workspace cutover tests**

Replace old recursive-editor assertions with:

- Create opens one RuleEditor with no broad valid default.
- Edit opens the same RuleEditor initialized from persisted rule.
- Save event sends exact create/update payload and immutable kind/app binding.
- Opening create/edit/saved preview while dirty asks before discard; preview never closes current draft without a decision.
- Saved rows use compact `RuleMeaning`, not only root operator.
- Provider display names reach RuleEditor.
- Existing saved preview/explain pagination remains unchanged.
- Successful save focuses/identifies the saved group and announces success.

- [ ] **Step 2: Refactor AppPolicyWorkspace state**

Replace `newRuleDraft`/`editRuleDraft` condition helpers with one active editor descriptor:

```ts
type ActiveRuleEditor =
  | { mode: 'create'; groupId: null; initialDraft: RuleEditorDraft }
  | { mode: 'edit'; groupId: number; initialDraft: RuleEditorDraft }
```

Retain parent-owned API mutation and workspace reload. RuleEditor owns unsaved editing and review state.

- [ ] **Step 3: Integrate create/edit and saved summaries**

Use `RuleEditor` once in the rule-group section. Do not duplicate create/edit markup. Keep delete confirmation and persisted Preview/Explain. Render `RuleMeaning compact` in each saved row and a disclosure for full read-only outline.

- [ ] **Step 4: Remove obsolete editor and migrate tests**

Delete `RuleConditionEditor.vue` and its test after imports/references are gone. Preserve every observable closed-rule, limit, and provider behavior in `ruleDraft`/new component tests.

- [ ] **Step 5: Run focused and full dashboard tests**

```bash
cd dashboard && npm test -- --run \
  src/components/custom/AppPolicyWorkspace.test.ts \
  src/components/custom/RuleEditor.test.ts \
  src/components/custom/RuleVisualBuilder.test.ts \
  src/components/custom/RuleJsonEditor.test.ts
npm test
npm run build
```

Expected: focused and full Vitest PASS; production build exits 0.

- [ ] **Step 6: Run locale guards**

```bash
node -e "const fs=require('fs');const s=fs.readFileSync('dashboard/src/locales/en.ts','utf8');if(/[\u2018\u2019]/u.test(s))process.exit(1)"
```

Also run the repository’s existing locale compile test through the full dashboard test command. Expected: no invalid apostrophes or vue-i18n compile errors.

- [ ] **Step 7: Commit**

```bash
git add dashboard/src/components/custom/AppPolicyWorkspace* \
  dashboard/src/pages/ManagedApplicationDetailView.test.ts \
  dashboard/src/components/custom/RuleConditionEditor.vue \
  dashboard/src/components/custom/RuleConditionEditor.test.ts
git commit -m "feat(policy-ui): replace rule group editor"
```

### Task 8: End-to-end verification, visual polish, and embedded dashboard build

**Files:**
- Modify only if browser evidence finds defects: files introduced/changed in Tasks 4–7
- Regenerate: `pkg/webui/dist/**`

**Interfaces:**
- No new product contract; this task verifies and polishes the completed surface.

- [ ] **Step 1: Run all backend and frontend verification**

```bash
go test ./pkg/appaccess ./pkg/server -count=1
cd dashboard && npm test && npm run build
cd .. && go test ./... -count=1
```

Expected: all suites PASS.

- [ ] **Step 2: Start the real development surface**

Use the existing `dev:federation` instance-A database because it contains nested ALL/ANY sample rules with negative leaf conditions. Start long-running processes through Hub, not Bash. Open the actual managed/admin application policy route in the browser and authenticate with the enrolled development account if the browser session is not already authenticated.

- [ ] **Step 3: Browser-drive the complete workflow**

Verify on the real surface:

1. Create starts incomplete.
2. Build nested ALL → ANY with both `is` and `is not` predicates.
3. Click ALL/ANY rail modes and condition polarity; values and children persist.
4. Remove, Undo, move up/down, and focus transitions.
5. Switch Visual → JSON → Visual with exact round-trip.
6. Break JSON; confirm line/path error and preserved visual draft.
7. Inspect fully parenthesized read-only expression.
8. Confirm exact draft match count and stale/loading states.
9. Review and save, then verify persisted compact/full meaning.
10. Edit, dirty-cancel, switch rule, saved preview/explain, API failure recovery.

- [ ] **Step 4: Inspect visual quality at required states**

Take browser screenshots at desktop, tablet, and narrow mobile widths in light and dark themes. Inspect:

- no recursive card/rounded-frame noise;
- rails/connectors remain unambiguous at depth 8;
- root and nested ALL descriptions match;
- clickable rails look interactive but slim;
- meaning/impact do not compete with editing controls;
- 200% zoom and long provider/display names remain usable;
- focus is visible and DOM/visual order align.

If defects appear, fix only the owning component, rerun its focused tests/build, and repeat the affected screenshot.

- [ ] **Step 5: Keyboard and accessibility smoke**

Complete create/edit without a pointer. Confirm mode menus, add/remove/move, JSON Escape→Tab, disclosures, dialogs, preview pagination, Review/save, announcements, and focus return. Inspect accessibility tree for unique group/condition names and correct `aria-expanded`/`aria-controls`.

- [ ] **Step 6: Rebuild embedded dashboard assets**

```bash
cd dashboard && npm run build
```

Confirm generated assets in `pkg/webui/dist` reflect the new source and no stale RuleConditionEditor chunk remains.

- [ ] **Step 7: Run final regression gate**

```bash
go test ./... -count=1
cd dashboard && npm test && npm run build
```

Expected: all green on the exact delivered tree.

- [ ] **Step 8: Commit polish and generated assets**

```bash
git add dashboard/src pkg/webui/dist
git commit -m "feat(policy-ui): polish rule editor experience"
```

- [ ] **Step 9: Request final review**

Use the `requesting-code-review` skill. Final review must explicitly inspect:

- rule-contract parity between Go and TypeScript;
- unsaved endpoint authorization/no-write/count/cursor correctness;
- immutable and non-destructive tree transforms;
- invalid JSON data preservation;
- dirty-draft and save-without-preview safety;
- hierarchy clarity without recursive framing;
- keyboard/screen-reader behavior;
- A/B app-kind isolation and existing persisted preview regression.
