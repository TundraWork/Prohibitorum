# Frontend Theme and Discoverability Remediation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: use test-driven-development and Impeccable product rules.

**Goal:** Make the explicit app theme override the OS consistently, expose app-tile actions on keyboard/touch, and give the public-JWK dialog a complete accessible description without changing flows.

**Architecture:** Register Tailwind v4's `dark` variant against the existing `.dark` root class. Keep destructive labels white because the dark variant's 60% blended fill clears AA; switching to `text-destructive-foreground` would fail on that blend. Make the tile menu a persistent affordance and use the existing Reka dialog-description primitive for the JWK context.

**Tech Stack:** Vue 3, Tailwind CSS 4, Vitest, browser verification.

### Task 1: Bind dark utilities to app theme

**Files:** Modify `dashboard/src/assets/main.css`.

**Acceptance Criteria:** Built `dark:` selectors use `.dark` rather than `prefers-color-scheme`; app-light/OS-dark and app-dark/OS-light both render correct banner, input, and destructive states; destructive normal text contrast remains at least 4.5:1.

**Verify:** isolated `npm run build`; compiled CSS contains class-based dark selectors and no dark-utility `prefers-color-scheme` block; browser fixture checks all four app/OS combinations.

**Steps:** Build before the fix and capture the failing OS/app mismatch; add `@custom-variant dark (&:where(.dark, .dark *));` after Tailwind import; rebuild and verify all combinations. Do not change destructive foreground classes.

### Task 2: Keep AppTile actions visible

**Files:** Modify `dashboard/src/components/custom/AppTile.vue`, `dashboard/src/components/custom/AppTile.test.ts`.

**Acceptance Criteria:** The actions trigger is visible at rest, remains keyboard-operable and touch-discoverable, stays pinned while open, and does not obscure the launch link or protocol badge at 320 px.

**Verify:** `cd dashboard && npm test -- src/components/custom/AppTile.test.ts` exits 0; browser check at 320 px and desktop.

**Steps:** Add a failing mounted-DOM assertion that the menu container is not opacity-zero at rest; remove hover/focus-only visibility while preserving transition/open behavior; run focused tests and browser check.

### Task 3: Describe the public-JWK dialog

**Files:** Modify `dashboard/src/pages/admin/AdminSigningKeysView.vue`, its test, and both locale files using the existing locale-parity convention.

**Acceptance Criteria:** Opening the JWK dialog emits no Reka missing-description warning; `DialogContent` references a visible or visually-hidden description that identifies the selected key; English and Chinese locale keys remain parallel.

**Verify:** `cd dashboard && npm test -- src/pages/admin/AdminSigningKeysView.test.ts src/locales/locales.parity.test.ts` exits 0 with no missing-description stderr.

**Steps:** Add a failing test that opens the dialog and asserts an accessible description plus no warning; import/render `DialogDescription` with localized selected-key context; update locale keys; run focused tests to green.

### Task 4: Remove HTML-like text from locale messages

**Files:** Modify `dashboard/src/locales/en.ts`, `dashboard/src/locales/zh.ts`; use existing compile/parity tests.

**Acceptance Criteria:** Attribute-source guidance still communicates `attributes.key`; vue-i18n emits no HTML-detection warning during affected component/page tests; locale parity remains intact.

**Verify:** `cd dashboard && npm test -- src/components/custom/AttributeMapEditor.test.ts src/pages/admin/AdminSamlProviderDetailView.test.ts src/locales` exits 0 without intlify warnings.

**Steps:** Capture the existing warning in the focused test run; replace literal `attributes.<key>` with non-HTML `attributes.key` in both locale messages; rerun focused tests to clean output.
