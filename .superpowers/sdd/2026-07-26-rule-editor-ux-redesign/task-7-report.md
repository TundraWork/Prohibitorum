# Task 7 report — Workspace cutover, persisted summaries, and regression coverage

## Status
Complete.

## Delivered
- Replaced the two recursive rule-editor forms in `AppPolicyWorkspace` with one `RuleEditor` driven by an active create/edit descriptor.
- Preserved parent-owned rule mutations with exact create and update payloads; create supplies immutable `kind: "rule"`, while update sends only mutable group fields.
- Added dirty-draft protection before create, edit, and persisted preview transitions. Saved preview opens only after an explicit discard decision and preserves the active editor when cancelled.
- Preserved saved-row Preview/Explain pagination and explanation rendering.
- Added compact persisted `RuleMeaning` plus a full read-only disclosure for each saved rule row.
- Passed provider `{ slug, displayName }` descriptors and the unsaved `${basePath}/rule-preview` endpoint into the shared editor.
- On save, holds editor mutation state through workspace reload to prevent duplicate submission, then announces success and focuses the saved group row.
- Removed `RuleConditionEditor.vue` and its obsolete recursive-editor test suite.
- Regenerated `pkg/webui/dist` from the final dashboard build.

## TDD evidence
1. Replaced old workspace recursive-editor tests with shared-editor integration assertions.
2. Ran the focused workspace test red: five intended cutover assertions failed because the old integration did not render `RuleEditor`, did not emit the new payload flow, and did not render persisted `RuleMeaning`.
3. Implemented the cutover and reran the workspace suite green.
4. Reviewer caught a post-save reload race; added a focused regression proving the editor stays busy until reload and teardown complete.

## Verification
- `cd dashboard && npm test -- --run src/components/custom/AppPolicyWorkspace.test.ts src/components/custom/RuleEditor.test.ts src/components/custom/RuleVisualBuilder.test.ts src/components/custom/RuleJsonEditor.test.ts`
  - PASS after final race fix: 4 files, 51 tests.
- `cd dashboard && npm test`
  - PASS after final race fix: 117 files, 1,247 tests.
- `cd dashboard && npm run build`
  - PASS after final race fix: `vue-tsc -b && vite build` completed and refreshed `pkg/webui/dist`.
- `cd dashboard && node -e "const fs=require('fs');const s=fs.readFileSync('src/locales/en.ts','utf8');if(/[\u2018\u2019]/u.test(s))process.exit(1)"`
  - FAIL: existing out-of-scope `en.ts` content contains U+2019 at lines 245 and 1028 (three total); the same guard also fails against the pre-Task-7 commit `ee6407d0`.
- `grep RuleConditionEditor dashboard`
  - PASS: no references after removal.
- Browser smoke: launched the dashboard at `/manage/applications/oidc/client%2Falpha`; the unauthenticated application shell redirected to the expected sign-in screen. Authenticated policy interaction is covered by the workspace integration suite.
## Concerns
- The required locale apostrophe guard remains red because of pre-existing, out-of-scope `en.ts` U+2019 characters. Task 7 left locale files untouched; the full dashboard locale compile/parity tests pass.
- The production build reports pre-existing Rollup dynamic/static import and chunk-size warnings; build succeeds.
