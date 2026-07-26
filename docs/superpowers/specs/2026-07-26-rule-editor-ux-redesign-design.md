# Rule editor UX redesign

**Date:** 2026-07-26  
**Status:** Approved direction; awaiting written-spec review

## Summary

Replace the current recursive AST-shaped rule editor with a modern, slim visual builder that makes `ALL`, `ANY`, `NOT`, and nested scope immediately legible. The visual builder and an advanced canonical-JSON editor operate on the same draft rule. A read-only compact expression and a plain-language explanation let managers verify meaning without learning JSON or mentally executing the tree.

The redesign keeps the existing version-1 rule contract, validation limits, APIs, and policy semantics. It changes authoring, review, error prevention, accessibility, draft preview, and wording; it does not add a second persisted rule language.

## Research basis

The design adapts these established patterns:

- [Carbon filtering](https://carbondesignsystem.com/patterns/filtering/): separate categories, use batch application when users make several interdependent choices, expose applied state, and provide clear reset paths.
- [NN/g on complex applications](https://www.nngroup.com/articles/usability-heuristics-complex-applications/) and [form cognitive load](https://www.nngroup.com/articles/4-principles-reduce-cognitive-load/): structure, transparency, clarity, progressive disclosure, recognition over recall, and precise local recovery.
- [Microsoft Entra dynamic rule builder](https://learn.microsoft.com/en-us/entra/identity/users/groups-dynamic-membership): readable property/operator/value clauses and a separate advanced representation for rules beyond the visual surface.
- [LaunchDarkly targeting rules](https://launchdarkly.com/docs/home/flags/target-rules): conditions expressed as scope/attribute, operator, and value, with condition summaries visible at rest.
- [W3C keyboard guidance](https://www.w3.org/WAI/WCAG22/Understanding/keyboard.html) and [disclosure pattern](https://www.w3.org/WAI/ARIA/apg/patterns/disclosure/): complete keyboard equivalence, conventional controls, explicit expanded state, and predictable focus.

## Problems in the current editor

- One “Condition type” select mixes structural operators and facts.
- “Leaf,” “All,” “Any,” and “Not” expose storage vocabulary instead of access intent.
- Repeated rounded, bordered containers create visual noise and obscure parent/child scope.
- The valid default (`ALL → any avatar`) is broad and can be saved without deliberate rule construction.
- Preview is available only after saving a live rule.
- Changing node type can discard an entire subtree without warning or undo.
- Saved rows summarize only the root operator, so rules are indistinguishable at a glance.
- Repeated accessible names such as “Condition type,” “Value,” and “Remove condition” do not identify branches.
- Advanced claim exposure and slug configuration compete with the primary authoring task.
- Save success, structural edits, limits, and invalid nodes lack precise local feedback.

The current surface scored 19/40 in a source-based Nielsen heuristic review, with six of eight cognitive-load checks failing. The deterministic Impeccable source scan reported no syntactic design-rule findings; the problem is interaction design rather than forbidden styling primitives.

## Design principles

1. **Scope before decoration.** Logical hierarchy must remain evident without relying on nested cards, shadows, or color alone.
2. **One grammar at every depth.** Root and nested groups use identical controls and wording.
3. **Operator is the control.** The visible `ALL`, `ANY`, or `NOT` rail label changes the group mode directly.
4. **No destructive mode surprises.** Compatible changes preserve children; incompatible changes wrap or require an explicit, reversible decision.
5. **Meaning stays visible.** Plain language, compact expression, and draft impact update with the rule.
6. **Draft before effect.** Nothing changes access until review and save.
7. **One persisted language.** JSON is the advanced source format and existing API contract; the compact expression is read-only.
8. **Keyboard-first and screen-reader exact.** Every branch, action, status, and focus move is identifiable without vision.

## Visual architecture

### One outer surface

The rule form has one outer editor surface. It does not render a card or rounded frame for each nested group.

- A thin neutral divider separates editor and review panel on wide screens.
- On narrow screens, the review panel follows the editor.
- Rounded boxes are reserved for editable predicate controls, the impact panel, and the outer form—not structural nesting.
- Nested hierarchy uses indentation, a slim vertical operator rail, short description text, and explicit connector labels.
- Rails have text labels and restrained semantic tint; color never carries mode alone.

### Logic rails

Each group starts with a slim clickable rail label:

- `ALL` — “Every condition in this group must be true.”
- `ANY` — “At least one condition in this group must be true.”
- `NOT` with a leaf — “This condition must not be true.”
- `NOT` with a group — “This group must not be true.”

The redundant sentence “Match an account when all of the following are true” is removed. The root group begins immediately and owns its description like every nested group.

Between children, the builder renders explicit `AND` or `OR` connector text. `NOT` has exactly one child and no sibling connector inside its scope.

### Predicate rows

A leaf reads left-to-right as a clause:

```text
[Connection provider] [is] [Downstream]
[Login method]       [is] [Passkey]
[Avatar]             [is] [User-uploaded]
```

The first select contains facts only. Structural operators do not appear in it. The fixed operator word is currently `is`; negation belongs to group mode rather than a second negative value vocabulary.


The workspace provider descriptor expands from `{ slug }` to `{ slug, displayName }`. The visual builder labels providers with `displayName` and shows `slug` as secondary exact text; JSON and expression output continue to use the slug.
Provider choices use the required human-readable `displayName` with the slug as secondary exact text. Protocol and method labels use the existing localized names.

## Editing behavior

### Adding content

Every `ALL` or `ANY` group exposes only:

- `Add condition`
- `Add nested group`

`Add condition` creates an incomplete predicate row with no broad default. It is invalid until the manager chooses a fact and value.

`Add nested group` creates an `ALL` group with one incomplete predicate. The rail label can immediately change its mode.

There is no standalone “Add NOT” button. A group’s rail mode menu can wrap that group in `NOT`; a predicate row’s overflow menu exposes `Must not match`, which wraps that leaf in `NOT`.

### Changing mode

Selecting an `ALL` or `ANY` rail label opens a menu containing `ALL`, `ANY`, and `NOT` with their description lines. Selecting a `NOT` rail opens `Remove NOT`, `Wrap in another NOT`, and—when its child is a group—`Change wrapped group to ALL` / `Change wrapped group to ANY`.

- `ALL ↔ ANY`: mutate the current combinator and preserve every child in place.
- predicate `Must not match`: wrap the predicate in `NOT`; preserve the predicate.
- group rail → `NOT`: wrap the whole group in `NOT`; preserve mode and children inside.
- `Remove NOT`: unwrap the child exactly as stored.
- `NOT` child group → `Change wrapped group to ALL` or `ANY`: mutate only the wrapped child’s combinator and preserve its children; the outer `NOT` remains.
- `Wrap in another NOT`: wrap the complete current `NOT` node in another `NOT`. Repeated negation remains visible as repeated rails and is never silently normalized.

Changing a fact type replaces only that predicate’s incompatible value. If a structural operation would discard content, show a confirmation naming what would be removed and provide Undo after completion.

### Removing and undo

Removal labels identify branch and content, for example:

- “Remove condition 2, Login method is Passkey”
- “Remove nested ANY group, 2 conditions”

After removal, focus moves to the next condition, previous condition, or parent add action in that order. A transient Undo action restores the exact removed subtree.

### Reordering

Within an `ALL` or `ANY` group, moving conditions does not change semantics but improves readability. Provide Move up / Move down actions in each row’s overflow menu and keyboard-accessible equivalents. Do not make drag-and-drop the only path. `NOT` has one child and cannot reorder internally.

## Visual and JSON modes

The condition area has a two-option mode control:

```text
Visual builder | JSON
```

### Shared draft

Both modes edit the same unsaved version-1 `Rule` draft.

- Entering JSON serializes the last valid visual draft with stable two-space formatting.
- Valid JSON updates the shared draft and visual builder immediately.
- Switching back to Visual is allowed only when JSON is valid and supported by the visual vocabulary.
- Invalid JSON stays in the text editor; it never overwrites the last valid shared draft.
- If JSON is invalid, switching to Visual explains the exact problem and focuses the first error instead of discarding text.
- Cancel discards both visual and JSON changes together, subject to dirty-draft confirmation.

### JSON editor

Use the existing frontend stack without adding a code-editor or syntax-highlighting dependency:

- A native textarea with an adjacent, synchronized line-number gutter.
- IBM Plex Mono and horizontal scrolling by default.
- Tab inserts two spaces within the editor; `Escape`, then `Tab` moves focus out, and this keyboard behavior appears in helper text associated with the textarea.
- Format JSON action.
- Copy JSON action.
- JSON parse errors include line and column from the parser. Semantic errors—unknown field, rule version, node shape, provider, depth, node count, and child count—always include the exact JSON path; they include line/column only when the client can map that path to a source location without reparsing through a second grammar.
- No raw server error text.

Canonical editable shape:

```json
{
  "version": 1,
  "condition": {
    "op": "all",
    "children": []
  }
}
```

### Read-only expression

Beneath valid JSON, show a compact expression generated from the parsed rule, for example:

```text
provider("downstream") && protocol("oidc") &&
(federation || passkey) && !(password_totp)
```

The expression is explanatory only:

- It cannot be edited or persisted.
- It always derives from the valid shared rule.
- It uses documented stable fact names and conventional `&&`, `||`, and `!` notation.
- It is labeled “Equivalent expression” and includes a Copy action.
- Parentheses are emitted at every combinator boundary so nested scope is never dependent on precedence knowledge.

## Plain-language rendering

The review panel always derives a sentence from the last valid shared rule. It emphasizes values but remains text, not chips or nested boxes.

Example:

> Connected through **Downstream** over **OIDC**, using **federation or a passkey**, without **password + TOTP**.

For arbitrary deep rules, use a sentence outline rather than forcing one grammatical sentence:

```text
Every condition is true:
  • Provider is Downstream
  • At least one condition is true:
      • Login method is Federation
      • Login method is Passkey
  • The following is not true:
      • Login method is Password + TOTP
```

Collapsed saved rows show the concise outline’s first line plus leaf count and nesting count; expanding “View rule” reveals the full read-only sentence outline without entering edit mode.

## Draft impact and review

### Draft preview

Preview evaluates the unsaved draft, not only persisted groups. The frontend needs a managed-application draft-preview endpoint that accepts a validated version-1 rule and returns the existing paginated safe account summaries and match results without writing the group.

The review panel shows:

- “N active accounts match” from the response’s exact `matchedCount`.
- Matched/non-matched account preview with existing pagination.
- “Previewing unsaved changes” status.
- Loading, stale, and retry states.
- Existing saved rule impact until a debounced draft preview completes; label stale results explicitly.

Preview requests debounce after valid draft changes and cancel/ignore stale responses. Invalid drafts keep the last successful preview but mark it out of date.

### Review and save

Primary action is `Review and save`, not `Save`.

Review summarizes:

- Rule name.
- Plain-language rule.
- Match impact.
- Access consequence: “A matching account can access this app unless manually denied.”
- Claim exposure if enabled.
- For edits, the review states only the proposed rule’s exact `matchedCount`; newly matching and no-longer-matching counts are out of scope because the endpoint does not compare two rules.

Saving occurs only from this review state. Button copy becomes `Saving…`, then a polite live region announces “Rule group saved.” The row remains expanded long enough to confirm success or moves focus to the saved row heading.

## Metadata and progressive disclosure

Primary form order:

1. Display name.
2. Rule builder and live meaning.
3. Optional description.
4. Review and save.

`Slug` and `Expose in downstream group claims` move under an accessible `Advanced options` disclosure.

- For new groups, slug auto-generates from display name until manually edited.
- Slug remains exact, visible, and editable before first save.
- Existing slug is not changed when display name changes.
- Claims exposure keeps its existing precise helper copy.
- The disclosure uses a button with `aria-expanded` and `aria-controls`, opens with Enter/Space, and retains state while the form is open.

## Dirty drafts and user control

- Opening another rule, preview, create flow, or navigation while dirty prompts to keep editing or discard.
- The current draft remains open when previewing it.
- Cancel and close share the same dirty-state behavior.
- API failures preserve the full draft and appear next to the form with retry guidance.
- The browser’s unload warning is used only while a dirty draft exists.
- No autosave to the server; policy changes remain explicit.

## Accessibility

- Native button/select/textarea semantics or established project primitives.
- Every group has a semantic heading/legend including mode, nesting level, and child count.
- Every predicate control includes position and parent mode in its accessible name, e.g. “Condition 2 of 3 in ALL group, fact.”
- Rails are buttons with visible focus and menu state; mode is text, not color.
- Add/remove/move/mode changes announce through a polite live region.
- Focus moves to the first control in a new condition/group.
- After mode menus close, focus returns to the rail button.
- JSON errors associate with the editor and error list; selecting an error focuses its line when supported.
- Draft preview status and save success use live regions without stealing focus.
- Full functionality works without drag-and-drop or pointer input.
- At 200% zoom and narrow widths, controls stack while indentation is capped so nested content remains usable.

## Responsive behavior

- Wide: builder and review panel side by side.
- Medium/narrow: builder followed by a sticky-within-form review summary when space permits; otherwise normal document flow.
- Operator rails narrow but remain text-visible.
- Predicate controls stack fact above value; fixed “is” text remains associated for assistive technology even if visually omitted.
- Deep indentation uses a capped step and breadcrumb-style accessible labels; content width never shrinks below usable input width.
- JSON editor uses horizontal scrolling rather than soft-wrapping structural JSON by default, with a user-selectable wrap option if the editor supports it.

## Validation and limits

Errors attach to the exact group or predicate and use actionable copy:

- “Choose a condition type.”
- “Choose a provider.”
- “This ALL group needs at least one condition.”
- “Maximum nesting reached (8 levels).”
- “This rule has 64 conditions and groups, the maximum allowed.”
- “This group has 32 children, the maximum allowed.”

Disabled add controls expose the limiting reason in text or tooltip and through `aria-describedby`. The form-level summary links to each error; selecting an error focuses its control.

JSON and visual validation share one client-side parser/validator so the two modes cannot disagree. Server validation remains authoritative and maps stable reason codes back to the same messages.

## Component architecture

Split the current recursive component into focused units:

- `RuleEditor.vue`: shared draft, mode switching, dirty state, validation, preview orchestration, review/save state.
- `RuleVisualBuilder.vue`: root rendering and structural commands.
- `RuleGroupEditor.vue`: one `ALL`/`ANY`/`NOT` group, rail mode control, children, add/move/remove behavior.
- `RulePredicateRow.vue`: fact/value clause.
- `RuleJsonEditor.vue`: canonical JSON buffer, formatting, validation, expression display.
- `RuleMeaning.vue`: plain-language outline and compact expression generation.
- `RuleImpactPreview.vue`: unsaved preview status and account results.
- `ruleDraft.ts`: clone, path addressing, structural transformations, node counting, canonical serialization, validation, summary/expression generation.

`AppPolicyWorkspace.vue` retains workspace loading, persisted group CRUD, and overall section composition but delegates rule authoring to `RuleEditor.vue`. Create and edit share the same editor component and state contract.

## API changes

Add draft preview without persistence:

```text
POST /api/prohibitorum/managed-applications/{kind}/{appId}/rule-preview
```

Request:

```json
{
  "version": 1,
  "condition": { "fact": "login_method", "method": "passkey" },
  "cursor": "",
  "limit": 50
}
```

Response:

```json
{
  "items": [
    {
      "account": { "id": 7, "username": "alice", "displayName": "Alice Ng" },
      "matched": true
    }
  ],
  "matchedCount": 3,
  "nextCursor": "opaque-or-empty"
}
```

`matchedCount` is the exact number of active accounts matching the proposed rule across the full active directory, independent of pagination. It is calculated in the same read-only snapshot as the requested page. The endpoint:

- Applies the same scoped-manager authorization as persisted preview.
- Validates the same closed rule contract and limits.
- Reads live facts only and omits disabled accounts.
- Performs no writes and emits no raw rule/fact audit detail.
- Returns stable path/reason validation details that the client maps locally.
- Uses the existing keyset cursor conventions for `items`.
- Rate limits consistently with other delegated management reads.

## Wording

Replace schema vocabulary:

| Current | Replacement |
|---|---|
| Condition type | Signal |
| Value | Required value |
| Leaf | Condition |
| All conditions | ALL |
| Any condition | ANY |
| Not | NOT |
| Conditions | Access rule |
| New rule group | Create rule group |
| Save | Review and save |
| Resolve invalid or oversized conditions before saving. | Fix the highlighted rule items before review. |

Retain product terms that are exact: provider, protocol, login method, avatar, slug, downstream group claims.

## Testing and verification

### Unit tests

- Every structural transformation, including repeated NOT and preservation across mode changes.
- Canonical JSON round-trip visual → JSON → visual.
- Invalid JSON never changes last valid shared rule.
- Unknown fields, unsupported versions, bad providers, limits, and exact paths.
- Plain-language outline and fully parenthesized expression for every node kind.
- Stable focus target selection after add/remove/move.
- Slug generation stops after manual edit.

### Component tests

- Logic rail descriptions, connectors, indentation, and no recursive card frames.
- Rail mode menu semantics and preservation behavior.
- Condition/group add, remove, undo, and move.
- Dirty-draft confirmation.
- Visual/JSON switching and error recovery.
- Advanced disclosure semantics.
- Draft preview loading, stale, invalid, failure, pagination, and race handling.
- Review/save flow and live announcements.
- Keyboard-only operation and exact accessible names.
- Narrow layout and deep nesting classes.

### Backend tests

- Draft preview authorization for manager/admin/foreign manager.
- OIDC, forward-auth, and SAML application scoping.
- Closed validation parity and stable error paths.
- No persistence or audit leakage.
- Pagination and disabled-account omission.

### Browser verification

Run the actual dashboard and verify:

- Create, edit, cancel, dirty switch, JSON round-trip, invalid JSON recovery, nested repeated NOT, draft preview, and save.
- Light and dark themes.
- Desktop, tablet, and narrow mobile widths.
- Keyboard-only flow and visible focus.
- Screen-reader announcements and group/condition identity.
- 200% zoom and long provider/display names.

## Out of scope

- A new persisted expression language.
- Arbitrary text/CEL/Rego editing.
- New rule facts or operators.
- Drag-only reordering.
- Server autosave.
- Changing manual-decision precedence or app-access semantics.
- Changing the version-1 wire contract.
