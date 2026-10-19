# coss redesign — Phase 9: remove HeroUI and finish

> **For agentic workers:** REQUIRED SUB-SKILL: Use minipowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove HeroUI, react-aria and the interim plumbing from the dashboard so it runs on coss alone; delete what no page uses any more; bring the conventions and the design record in line with what shipped; and verify the whole application end to end.

**Architecture:** Every page already renders on coss. What remains is plumbing (`app/AppEnvironment.tsx`'s react-aria i18n provider, `components/custom/Form.tsx` built on HeroUI's form, `host-overlay-container` / `top-layer` that existed to stack HeroUI overlays, `ScrollArea` on overlayscrollbars), a few leftovers (`ConsoleCard`, `EntityIconCard`, test setup references) and the documents that still describe HeroUI.

**Tech Stack:** React 19, TanStack Router/Query/Form, Lingui 6, coss ui (Base UI 1.9), Tailwind 4, Vitest + Testing Library, Go (server embedding the built dashboard).

## Global Constraints

Owner decisions and lessons from real defects; anything else is a default the implementer may depart from with a stated reason.

- Work in `/home/tundra/projects/tundra/prohibitorum-worktrees/develop-coss` on branch `develop-coss`.
- Behaviour is preserved: no page may change behaviour or look because plumbing changed (verify with the existing tests and screenshots of representative pages before/after).
- One report per failure; tests by role, name, label and `data-*`; strings in `en` and `zh` with no invisible characters.
- New dependencies are fine when they suit the design (exact versions).
- Commits: Conventional Commits; no `Co-Authored-By` or other attribution trailer; nothing cites plans, specs or task numbers.
- The full gate (`mise run ci:frontend`) must be green at the end of each task; Go build/vet/test green where Go is touched.

---

### Task 1: Remove HeroUI, react-aria and the interim plumbing

**Files:** `dashboard/src/app/AppEnvironment.tsx`, `dashboard/src/components/custom/Form.tsx`, `host-overlay-container.ts` (+ test), `top-layer.ts` (+ test), `ScrollArea.tsx`, `ConsoleCard.tsx`, `EntityIconCard.tsx`, `Section.tsx`/`Section.test.tsx` references, `EntityPicker.tsx` (comment), `pages/public/Enroll.test.tsx`, `src/test/setup.ts`, `src/styles/index.css`, `dashboard/biome.json`, `dashboard/package.json` + lockfile.

**Interfaces:**
- Replace each remaining use with its coss/Base UI or plain-React equivalent: the react-aria i18n provider (find what still needs locale context — Base UI components take locale via props or none; date/number formatting goes through Lingui/Intl); the shared `Form` (a plain `<form>` with the same submit/validation contract its callers rely on — read every caller); overlay stacking helpers (delete if Base UI's portals and the console's layering no longer need them; keep any behaviour a test proves, e.g. dialogs above the sticky header); `ScrollArea` on coss `scroll-area` (or native overflow where it suffices).
- Delete components no page imports any more (`ConsoleCard`, `EntityIconCard` if unused, and any other dead file found with a usage sweep of `src/components/custom` and `src/lib`), with their tests.
- Remove `@heroui/react`, `@react-aria/*`, `react-aria`, `react-aria-components`, `tailwind-variants`, `overlayscrollbars`, `overlayscrollbars-react` from `package.json` (and any HeroUI CSS/theme imports and variables in `index.css`, `.heroui-docs` in biome.json); `pnpm install` to update the lockfile.
- `grep -rE "heroui|react-aria|tailwind-variants|overlayscrollbars" dashboard/src dashboard/*.json dashboard/*.ts` ends empty (catalogue source references excepted only if they are file paths that no longer exist — re-extract to clean them).
- [ ] **Step 1:** Sweep and list every remaining use and every dead file; screenshot six representative pages (sign-in, Security, an OIDC application, the rule builder, Users list, the sign-in page editor) at 1440 light en and 390 dark zh as the "before" set.
- [ ] **Step 2:** Replace and delete in coherent commits, running the touched tests each time.
- [ ] **Step 3:** Remove the dependencies; `pnpm install`; `mise run ci:frontend` green; the production build has no HeroUI in its bundle (check the emitted assets).
- [ ] **Step 4:** Retake the same six pages as the "after" set; they must match the before set (differences only where a bug was fixed — list them).
- [ ] **Step 5:** Commit(s) — e.g. `refactor(dashboard): drop HeroUI and react-aria`.

---

### Task 2: Conventions and design record

**Files:** `dashboard/AGENTS.md`, root `AGENTS.md`, `DESIGN.md` (and any `docs/` page that tells contributors to use HeroUI).

**Interfaces:**
- `dashboard/AGENTS.md`: remove every HeroUI instruction; describe the coss setup as it is (vendored registry components in `src/components/ui`, restyled only where the design needs it with their details preserved; shared patterns in `src/components/custom`; the console material and type rules; verification at 1440, 1920 and 390 in both themes and both locales). Keep the file's voice; keep every rule that is still true.
- Root `AGENTS.md`: fix its one HeroUI line.
- `DESIGN.md`: regenerate from the shipped dashboard (tokens, type, material, components, patterns as they are in the code), not from intentions. The `impeccable:impeccable-documenter` agent derives it from the built artifact.
- [ ] **Step 1:** Rewrite the two AGENTS.md files.
- [ ] **Step 2:** Regenerate DESIGN.md from the shipped code.
- [ ] **Step 3:** Commit — `docs: describe the dashboard as built on coss`.

---

### Task 3: Whole-application verification

**Interfaces:**
- `mise run ci:frontend` and `go build -tags nodynamic ./... && go vet -tags nodynamic ./... && go test -tags nodynamic ./...` green.
- `mise run ci:smoke` (end-to-end against a throwaway database) exits 0; fix any failure it finds at its root.
- A cross-page visual pass at 1440 light en, 1920 dark en and 390 dark zh over every route (public, member, admin), looking for inconsistencies between pages built in different phases (headers, section icons, list idioms, empty states, dialogs, toasts, focus rings, dark contrast); fix what it finds.
- [ ] **Step 1:** Run the gates and the smoke; fix failures.
- [ ] **Step 2:** Cross-page visual pass; fix what it finds.
- [ ] **Step 3:** Commit fixes; record the final gate results.
