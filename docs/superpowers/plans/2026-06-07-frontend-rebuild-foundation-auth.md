# Frontend Rebuild — Spec 1 (Foundation + Auth) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Nuxt UI frontend with a from-scratch Vue 3 + Tailwind v4 + shadcn-vue/Reka UI scaffold and rebuild the unauthenticated threshold pages (login, consent, logout, error, enroll) end-to-end, embedded in the Go binary.

**Architecture:** Fresh `dashboard/` SPA (Vite + Vue 3 + TS strict + Tailwind v4 + Reka UI via shadcn-vue, vue-router, vue-i18n English-first, Pinia). Three-tier components: vendored `ui/` (CLI-fenced, token-styled) + first-party `custom/` + `pages/`. Built to the committed `pkg/webui/dist` (`go:embed`), served same-origin via chi `NotFound` with a stricter CSP. The old `dashboard/` is advisory only (no authority) — re-derive logic with judgement; backend endpoints + security properties + `DESIGN.md`/`PRODUCT.md` are canonical.

**Tech Stack:** Vue 3.5, Vite 6, TypeScript 5 (strict), Tailwind CSS v4 (`@tailwindcss/vite`), reka-ui, shadcn-vue (CLI), vue-router 4, vue-i18n, pinia, `@simplewebauthn/browser`, `@vueuse/core`, cva + tailwind-merge + clsx, lucide-vue-next, `@fontsource-variable/hanken-grotesk` + `@fontsource/ibm-plex-mono`. Tests: vitest + `@vue/test-utils` + jsdom.

**Spec:** `docs/superpowers/specs/2026-06-07-frontend-rebuild-foundation-auth-design.md`

**Conventions:**
- Commit directly to `master` (no remote, no worktree — project convention).
- Node via mise (`node = "24"`); **npm** (a `package-lock.json` exists; do NOT switch to pnpm). Run frontend tooling from `dashboard/`.
- After any frontend change that must reach the binary: `cd dashboard && npm run build` then `git add pkg/webui/dist` (the binary embeds the COMMITTED dist).
- Go gate is authoritative: `mise exec -- go build ./... && go vet ./...` exit 0 (trust over gopls).
- Never `pkill -f 'prohibitorum'` bare (kills dev PG). Smoke: `setsid bash /tmp/run_v06.sh`, poll `/tmp/v06.result` for `SMOKE_EXIT=0`.
- **Advisory-not-authority:** read the old `dashboard/` (git `e45f356`) for edge cases only; re-derive cleanly, deviate freely.

---

### Task 0: Cleanup baseline

**Goal:** A clean working tree before the rewrite — gitignore the impeccable-skill artifact dirs and commit the design docs — without touching the frontend yet.

**Files:**
- Modify: `.gitignore`
- Commit (already on disk, untracked/modified): `DESIGN.md`, `PRODUCT.md`, `README.md`

**Acceptance Criteria:**
- [ ] `.agents/`, `.claude/`, `.impeccable/` are gitignored and untracked (`git status` no longer lists them).
- [ ] `DESIGN.md`, `PRODUCT.md`, and the `README.md` edit are committed.
- [ ] No absolute `$HOME` paths are committed (lesson from the `.vscode/` incident).

**Verify:** `git status --porcelain | grep -E '\.agents|\.claude|\.impeccable'` → empty; `git ls-files DESIGN.md PRODUCT.md` → both listed.

**Steps:**

- [ ] **Step 1: gitignore the skill artifact dirs.** Append to `.gitignore` (after the `.superpowers/` line):
```
# Impeccable design-skill artifacts + installed skill copies (tooling, not source)
/.impeccable/
/.agents/
/.claude/
```

- [ ] **Step 2: Commit the design docs + gitignore.**
```bash
git add .gitignore DESIGN.md PRODUCT.md README.md
git commit -m "chore: gitignore impeccable skill artifacts; commit design system docs

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 3: Confirm the tree.** Run `git status --short` — only the frontend (`dashboard/*`, `pkg/webui/dist/*`) should remain dirty; the skill dirs gone, docs committed.

---

### Task 1: Nuke old frontend + fresh scaffold + embed

**Goal:** Delete the Nuxt UI `dashboard/` and stand up a fresh Vite + Vue 3 + TS + Tailwind v4 + shadcn-vue scaffold that builds to `pkg/webui/dist` and embeds cleanly — committed in one step so `go build` is never broken.

**Files:**
- Delete: all of `dashboard/` (old) and `pkg/webui/dist/*` (regenerated this task)
- Create: `dashboard/package.json`, `dashboard/vite.config.ts`, `dashboard/tsconfig.json` (+ `tsconfig.app.json`, `tsconfig.node.json`), `dashboard/index.html`, `dashboard/components.json`, `dashboard/src/main.ts`, `dashboard/src/App.vue`, `dashboard/src/assets/main.css` (minimal for now; tokens in Task 2)
- Verify: `pkg/webui/webui.go` still embeds the new `dist`

**Acceptance Criteria:**
- [ ] `dashboard/` contains no `@nuxt/ui` dependency or usage.
- [ ] `npm install && npm run build` in `dashboard/` produces `pkg/webui/dist/index.html` + hashed assets.
- [ ] `components.json` sets `aliases.ui = @/components/ui` distinct from `aliases.components = @/components`; `@ → ./src` alias in `tsconfig.app.json` + `vite.config.ts`.
- [ ] `mise exec -- go build ./...` exit 0 with the new dist embedded; the server serves the shell at `/`.

**Verify:** `cd dashboard && npm run build` → `pkg/webui/dist/index.html` exists; `mise exec -- go build ./...` → exit 0.

**Steps:**

- [ ] **Step 1: Read the old config for parity, then delete.** Skim the old `dashboard/vite.config.ts` (proxy targets) and `package.json` (kept deps) — already captured below. Then:
```bash
rm -rf dashboard
git rm -r --quiet --cached dashboard pkg/webui/dist 2>/dev/null || true
mkdir -p dashboard/src/assets dashboard/src/components/ui dashboard/src/components/custom dashboard/src/pages dashboard/src/lib dashboard/src/composables dashboard/src/stores dashboard/src/router dashboard/src/locales
```

- [ ] **Step 2: `dashboard/package.json`** (npm; keeps `@simplewebauthn/browser` + the fontsource packages; drops `@nuxt/ui`, adds Reka/shadcn deps):
```json
{
  "name": "prohibitorum-dashboard",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vue-tsc -b && vite build",
    "preview": "vite preview",
    "test": "vitest run"
  },
  "dependencies": {
    "@fontsource-variable/hanken-grotesk": "^5.2.8",
    "@fontsource/ibm-plex-mono": "^5.2.7",
    "@simplewebauthn/browser": "^13",
    "@vueuse/core": "^11",
    "class-variance-authority": "^0.7",
    "clsx": "^2",
    "lucide-vue-next": "^0.460",
    "pinia": "^2",
    "reka-ui": "^2",
    "tailwind-merge": "^2",
    "vue": "^3.5",
    "vue-i18n": "^10",
    "vue-router": "^4"
  },
  "devDependencies": {
    "@tailwindcss/vite": "^4",
    "@types/node": "^24",
    "@vitejs/plugin-vue": "^5",
    "@vue/test-utils": "^2",
    "jsdom": "^25",
    "tailwindcss": "^4",
    "typescript": "^5",
    "vite": "^6",
    "vitest": "^2",
    "vue-tsc": "^2"
  }
}
```
> Versions: pin to whatever `npm install` resolves as current-latest for that major at implementation time; the majors above match the spec. Run `npm install` and commit the resulting `package-lock.json`.

- [ ] **Step 3: `dashboard/vite.config.ts`** (Tailwind v4 plugin; `@` alias; keep the old proxy targets; outDir → the embedded dist):
```ts
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  plugins: [vue(), tailwindcss()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/oauth': 'http://localhost:8080',
      '/saml': 'http://localhost:8080',
      '/oidc': 'http://localhost:8080',
      '/.well-known': 'http://localhost:8080',
    },
  },
  build: { outDir: '../pkg/webui/dist', emptyOutDir: true },
})
```

- [ ] **Step 4: tsconfig.** `tsconfig.json` references app+node (as the old did). `tsconfig.app.json` must include `"compilerOptions": { "strict": true, "baseUrl": ".", "paths": { "@/*": ["./src/*"] }, ... }` (standard Vue-TS strict config) plus `"include": ["src"]`. `tsconfig.node.json` covers `vite.config.ts`. (Use the `npm create vue` strict template as the base; add the `@/*` path.)

- [ ] **Step 5: `dashboard/components.json`** (the shadcn-vue alias fence — `ui` ≠ `components`):
```json
{
  "$schema": "https://shadcn-vue.com/schema.json",
  "style": "new-york",
  "typescript": true,
  "tailwind": { "config": "", "css": "src/assets/main.css", "baseColor": "neutral", "cssVariables": true },
  "aliases": {
    "components": "@/components",
    "ui": "@/components/ui",
    "composables": "@/composables",
    "lib": "@/lib",
    "utils": "@/lib/utils"
  },
  "iconLibrary": "lucide"
}
```

- [ ] **Step 6: `index.html`, `src/main.ts`, `src/App.vue`, minimal `src/assets/main.css`.**
`index.html`: standard Vite Vue shell loading `/src/main.ts`, `<div id="app">`, `<title>Prohibitorum</title>`, `lang="en"`.
`src/assets/main.css` (minimal now — full tokens in Task 2):
```css
@import "tailwindcss";
@import "@fontsource-variable/hanken-grotesk";
@import "@fontsource/ibm-plex-mono";
```
`src/main.ts`:
```ts
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import { i18n } from './i18n'
import './assets/main.css'

createApp(App).use(createPinia()).use(router).use(i18n).mount('#app')
```
> `router` and `i18n` are created in Task 3. For THIS task, stub them minimally (an empty router with a `/` route rendering a placeholder, and a minimal i18n) so the scaffold builds and serves; Task 3 fleshes them out. `App.vue` = `<template><RouterView /></template>`.

- [ ] **Step 7: install, build, verify embed.**
```bash
cd dashboard && npm install && npm run build && cd ..
mise exec -- go build ./...   # exit 0, dist embedded
```
Optionally boot `mise dev-server` and curl `/` → the shell HTML.

- [ ] **Step 8: Commit (nuke + scaffold + new dist together).**
```bash
git add -A dashboard pkg/webui/dist
git commit -m "feat(web): nuke Nuxt UI frontend; scaffold Vue 3 + Tailwind v4 + shadcn-vue

Fresh dashboard/ (no @nuxt/ui): Vite + Vue 3 + TS strict + Tailwind v4 +
reka-ui/shadcn-vue, @ alias, components.json alias fence (ui != components),
@simplewebauthn/browser kept. dist regenerated + committed so the go:embed
build is never broken.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Design tokens (Tailwind v4 @theme from DESIGN.md)

**Goal:** Wire the "Welcoming Vault" design system into `main.css` as Tailwind v4 `@theme` tokens, mapped to shadcn-vue's semantic aliases, with a dark-mode block structured but unfilled.

**Files:**
- Modify: `dashboard/src/assets/main.css`

**Acceptance Criteria:**
- [ ] OKLCH tokens for tide/tide-strong/ember/sage/amber/rose/ink/muted/border/surface/sunken/bg exist as `@theme` custom properties, matching `DESIGN.md` §2 values.
- [ ] Fonts (Hanken Grotesk display/body, IBM Plex Mono code), radii (sm6/md10/lg14/full), and the two elevation shadows are defined.
- [ ] shadcn-vue's `--background/--foreground/--primary/--ring/...` aliases map onto these tokens (so vendored `ui/` components inherit the design).
- [ ] A `.dark { }` block exists with a comment marking it reserved (no dark values yet).

**Verify:** `cd dashboard && npm run build` → exit 0; a temporary `<UiButton>` renders Tide-filled with the Tide focus ring (visual check via `mise dev-server`).

**Steps:**

- [ ] **Step 1: Author `main.css`.** Replace the minimal file with the `@theme` block transcribing `DESIGN.md` §2 (colors), §3 (type), §4 (elevation), §"rounded"/"spacing". Concretely (abbreviated — fill all tokens from DESIGN.md):
```css
@import "tailwindcss";
@import "@fontsource-variable/hanken-grotesk";
@import "@fontsource/ibm-plex-mono";

@theme {
  --color-tide: oklch(0.55 0.118 205);
  --color-tide-strong: oklch(0.47 0.130 205);
  --color-ember: oklch(0.70 0.150 42);
  --color-sage: oklch(0.62 0.130 150);
  --color-amber: oklch(0.76 0.140 75);
  --color-rose: oklch(0.58 0.180 22);
  --color-ink: oklch(0.22 0.015 230);
  --color-muted: oklch(0.50 0.012 230);
  --color-border: oklch(0.920 0.006 205);
  --color-surface: oklch(0.985 0.005 205);
  --color-sunken: oklch(0.965 0.006 205);
  --color-bg: oklch(1 0 0);
  --font-sans: "Hanken Grotesk Variable", ui-sans-serif, system-ui, sans-serif;
  --font-mono: "IBM Plex Mono", ui-monospace, SFMono-Regular, Menlo, monospace;
  --radius-sm: 6px; --radius-md: 10px; --radius-lg: 14px; --radius-full: 9999px;
  --shadow-raised: 0 1px 2px oklch(0.22 0.015 230 / 0.06), 0 2px 8px oklch(0.22 0.015 230 / 0.06);
  --shadow-overlay: 0 8px 32px oklch(0.22 0.015 230 / 0.14);
}

/* shadcn-vue semantic aliases mapped onto the Welcoming Vault tokens. */
:root {
  --background: var(--color-bg);
  --foreground: var(--color-ink);
  --primary: var(--color-tide-strong);
  --primary-foreground: var(--color-bg);
  --muted-foreground: var(--color-muted);
  --border: var(--color-border);
  --ring: var(--color-tide);
  /* …map card/popover/destructive(rose)/accent(ember) etc. per DESIGN.md… */
  --radius: 10px;
}

/* Dark mode mirrors these roles — RESERVED, values land in a later polish. */
.dark {
  /* TODO(dark): bg ≈ oklch(0.16 0.008 230), ink ≈ oklch(0.95 0.005 230),
     tide/ember lightened ~0.12–0.15 L. Not in scope for Spec 1. */
}

@layer base {
  body { background: var(--color-bg); color: var(--color-ink); font-family: var(--font-sans); }
}
```

- [ ] **Step 2: Build + visual sanity.** `npm run build`; if you added a temporary button, confirm Tide fill + focus ring; remove the temp markup.

- [ ] **Step 3: Commit.**
```bash
git add dashboard/src/assets/main.css
git commit -m "feat(web): wire DESIGN.md tokens into Tailwind v4 @theme (+ shadcn aliases)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Core libs, composables, store, i18n, router

**Goal:** Re-derive the stack-agnostic foundation — a typed API client, the same-origin returnTo guard, the WebAuthn ceremony wrapper, devMode, the `cn` util, shared composables, the auth store, i18n (English), and the router — to a clean-architecture bar, with unit tests for the pure logic.

**Files:**
- Create: `dashboard/src/lib/{utils.ts,api.ts,returnTo.ts,webauthn.ts,devMode.ts}`
- Create: `dashboard/src/composables/{useApi.ts,useWebauthn.ts,useReturnTo.ts}`
- Create: `dashboard/src/stores/auth.ts`
- Create: `dashboard/src/i18n.ts`, `dashboard/src/locales/en.ts`
- Create: `dashboard/src/router/index.ts`
- Test: `dashboard/src/lib/{returnTo.test.ts,api.test.ts}`, `dashboard/vitest.config.ts`

**Acceptance Criteria:**
- [ ] `api` exposes typed `get/post/put`, sends `credentials:'include'`, defensively parses JSON, and throws `{code, message}` on non-2xx.
- [ ] `safeReturnTo(raw)` returns a same-origin relative path or a safe default (`/`), rejecting absolute/cross-origin/`//` and `javascript:` inputs.
- [ ] `webauthn` wraps `@simplewebauthn/browser` `startAuthentication`/`startRegistration` with an `AbortController` and surfaces user-cancel distinctly.
- [ ] auth store has `me`, `ensureLoaded()`, `isAdmin`, `clear()`.
- [ ] i18n is English-first with an `errors.*` namespace; router defines the threshold routes (pages stubbed until Tasks 6–9) + a guard scaffold (`requiresAuth`/`requiresAdmin` reserved).
- [ ] `npm run test` passes (returnTo + api unit tests).

**Verify:** `cd dashboard && npm run test` → PASS; `npm run build` → exit 0.

**Steps:**

- [ ] **Step 1: `vitest.config.ts`** (jsdom env, `@` alias, vue plugin):
```ts
import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'
export default defineConfig({
  plugins: [vue()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  test: { environment: 'jsdom', globals: true },
})
```

- [ ] **Step 2: `lib/utils.ts`** — `cn`:
```ts
import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'
export function cn(...inputs: ClassValue[]) { return twMerge(clsx(inputs)) }
```

- [ ] **Step 3: TDD `lib/returnTo.ts`.** Write `returnTo.test.ts` first:
```ts
import { describe, it, expect } from 'vitest'
import { safeReturnTo } from './returnTo'
describe('safeReturnTo', () => {
  it('keeps a same-origin relative path', () => expect(safeReturnTo('/me/security')).toBe('/me/security'))
  it('defaults empty/undefined to /', () => { expect(safeReturnTo('')).toBe('/'); expect(safeReturnTo(undefined)).toBe('/') })
  it('rejects absolute cross-origin', () => expect(safeReturnTo('https://evil.test/x')).toBe('/'))
  it('rejects protocol-relative //', () => expect(safeReturnTo('//evil.test')).toBe('/'))
  it('rejects javascript: scheme', () => expect(safeReturnTo('javascript:alert(1)')).toBe('/'))
})
```
Run `npm run test` → FAIL. Then implement `returnTo.ts`: accept a string|undefined; only allow values that start with a single `/` (not `//`), contain no scheme, and resolve same-origin via `new URL(raw, location.origin)` whose `.origin === location.origin`; else return `/`. Run → PASS.

- [ ] **Step 4: TDD `lib/api.ts`.** Write `api.test.ts` (mock `fetch`): a 200 JSON returns parsed body; a 400 `{code,message}` throws an object with those fields; a non-JSON 500 throws `{code:'server_error', message:<text>}`; every call includes `credentials:'include'`. Run → FAIL. Implement a typed client:
```ts
export interface ApiError { code: string; message: string }
async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method, credentials: 'include',
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  const text = await res.text()
  let data: any = undefined
  if (text) { try { data = JSON.parse(text) } catch { /* non-JSON */ } }
  if (!res.ok) {
    const err: ApiError = (data && data.code) ? data : { code: 'server_error', message: text || res.statusText }
    throw err
  }
  return data as T
}
export const api = {
  get: <T>(p: string) => request<T>('GET', p),
  post: <T>(p: string, b?: unknown) => request<T>('POST', p, b),
  put: <T>(p: string, b?: unknown) => request<T>('PUT', p, b),
}
```
Run → PASS.

- [ ] **Step 5: `lib/webauthn.ts`** — wrap `@simplewebauthn/browser` (it owns base64url + `navigator.credentials`); add `AbortController` + cancel detection:
```ts
import { startAuthentication, startRegistration } from '@simplewebauthn/browser'
export async function passkeyGet(optionsJSON: any) {
  return startAuthentication({ optionsJSON }) // throws on user-cancel (NotAllowedError)
}
export async function passkeyRegister(optionsJSON: any) {
  return startRegistration({ optionsJSON })
}
export function isUserCancel(e: unknown) { return e instanceof Error && e.name === 'NotAllowedError' }
```
> Confirm the v13 `@simplewebauthn/browser` API shape (`{ optionsJSON }` arg) against its docs at implementation; adjust if the signature differs. The server returns the `publicKey` options JSON from `/begin`; pass it through verbatim.

- [ ] **Step 6: `lib/devMode.ts`** — `isDevMode()` = `import.meta.env.DEV || ['localhost','127.0.0.1','[::1]'].includes(location.hostname)`.

- [ ] **Step 7: `stores/auth.ts`** (Pinia setup store): state `me: SessionView | null`; `ensureLoaded()` calls `api.get('/api/prohibitorum/me')` once (cache), tolerates 401 → `me=null`; `isAdmin` computed (`me?.role === 'admin'`); `clear()` resets. Type `SessionView` from the backend contract (`{id,username,displayName,role,attributes?}`).

- [ ] **Step 8: composables.** `useApi` (thin wrapper exposing `busy`/`error` refs + a `run(fn)` helper that sets busy + maps thrown `{code,message}`); `useWebauthn` (wraps `passkeyGet/Register` with busy/cancel handling); `useReturnTo` (reads `route.query.return_to`, applies `safeReturnTo`, exposes `goReturnTo()` via `window.location.assign`). Keep each focused; these are the shared flow logic the pages consume (no per-page duplication).

- [ ] **Step 9: i18n.** `locales/en.ts` with namespaces `common`, `login`, `consent`, `logout`, `error`, `enroll`, and `errors` (map known backend codes → messages; fall back to `err.message`). `i18n.ts`: `createI18n({ legacy:false, locale:'en', fallbackLocale:'en', messages:{ en } })`. (zh added in a later pass; structure ready.)

- [ ] **Step 10: router.** `router/index.ts`: `createRouter(createWebHistory())` with routes `/login`,`/consent`,`/logout`,`/error`,`/enroll/:token` (lazy-import the page components — created in Tasks 6–9; stub with placeholder components for now so the build passes) and a catch-all → `/error`. Add an `installGuard(router)` that defines `requiresAuth`/`requiresAdmin` meta handling (reserved for Spec 2/3; for Spec 1 the threshold routes are public). Export `default router`.

- [ ] **Step 11: run + commit.** `npm run test` (PASS) + `npm run build` (exit 0).
```bash
git add dashboard/src/lib dashboard/src/composables dashboard/src/stores dashboard/src/i18n.ts dashboard/src/locales dashboard/src/router dashboard/vitest.config.ts
git commit -m "feat(web): core libs/composables/store/i18n/router (typed api, returnTo guard, webauthn)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Base `ui/` component kit (shadcn-vue, token-styled)

**Goal:** Add the vendored shadcn-vue primitives the auth slice needs and verify they render with the Welcoming Vault tokens — kept pristine (CLI-fenced to `ui/`), styled only via tokens.

**Files:**
- Create (via CLI): `dashboard/src/components/ui/{button,input,label,card,dialog,badge,field,alert}/*`

**Acceptance Criteria:**
- [ ] `npx shadcn-vue@latest add button input label card dialog badge field alert` writes only under `components/ui/` (alias fence holds).
- [ ] Primitives render with the tokens (Tide primary, Tide focus ring, `md` radius, mono available); no `@nuxt/ui` imports anywhere.
- [ ] No hand-edits to primitive markup beyond token/variant wiring (so they stay re-syncable); any bespoke styling deltas are achieved through tokens/cva variants, documented in a short `components/ui/README.md`.

**Verify:** `cd dashboard && npm run build` → exit 0; `git status` shows new files only under `src/components/ui/`.

**Steps:**

- [ ] **Step 1: init + add.** `cd dashboard && npx shadcn-vue@latest init` (accept the `components.json` already present; new-york, OKLCH, `@`). Then `npx shadcn-vue@latest add button input label card dialog badge field alert`. Confirm files landed under `src/components/ui/` only.

- [ ] **Step 2: token reconciliation.** Ensure the CLI-added CSS variables in `main.css` don't override Task 2's tokens — the `@theme` + `:root` alias mapping from Task 2 is the source of truth; if the CLI appended its own `:root`/`.dark` blocks, merge them into Task 2's mapping (keep our OKLCH values, keep shadcn's variable NAMES). Re-run `npm run build`.

- [ ] **Step 3: smoke a couple of primitives.** Temporarily drop a Button (default/ghost/danger), Input (focus ring), and Badge into a scratch route; verify against DESIGN.md §5 (Tide-strong fill + white label, ghost = Tide-strong text, danger = Rose, 2px Tide focus ring, 10px radius). Remove the scratch markup.

- [ ] **Step 4: README + commit.** Add `src/components/ui/README.md`: "Vendored shadcn-vue primitives. Do NOT hand-edit markup — restyle via tokens in `assets/main.css`. Re-sync with `npx shadcn-vue add <name> --overwrite`. Bespoke components live in `../custom/`."
```bash
git add dashboard/src/components/ui dashboard/src/assets/main.css dashboard/components.json
git commit -m "feat(web): vendored shadcn-vue ui/ primitives, token-styled (button/input/card/dialog/badge/field/alert)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Threshold shell — AuthBackdrop, LocaleSwitcher, CenteredLayout

**Goal:** The shared chrome for the unauthenticated pages: the scoped "Drenched" painted backdrop with an AA contrast scrim, a locale switcher, and a centered-card layout.

**Files:**
- Create: `dashboard/src/components/custom/AuthBackdrop.vue`, `dashboard/src/components/custom/LocaleSwitcher.vue`, `dashboard/src/pages/CenteredLayout.vue`
- Create: `dashboard/src/assets/auth-scene.*` (painterly CSS placeholder per DESIGN.md §Threshold Exception)
- Test: `dashboard/src/components/custom/LocaleSwitcher.test.ts`

**Acceptance Criteria:**
- [ ] `CenteredLayout` renders a centered, near-opaque card over `AuthBackdrop` with a contrast scrim guaranteeing WCAG 2.2 AA for the card/header over any image (per DESIGN.md §Threshold Exception).
- [ ] `AuthBackdrop` uses a painterly CSS placeholder; dropping a real `src/assets/auth-scene.*` replaces it without code changes.
- [ ] `LocaleSwitcher` lists available locales and switches `i18n.global.locale` (only `en` for now; structured for `zh`).
- [ ] Ember appears only in the brand mark (Scarce Accent Rule); the surface/card uses the restrained system.

**Verify:** `cd dashboard && npm run test` → PASS; `mise dev-server` → `/login` (Task 6) shows the centered card over the backdrop.

**Steps:**

- [ ] **Step 1: AuthBackdrop.vue** — full-bleed fixed background; painterly CSS gradient placeholder; an absolutely-positioned scrim layer (`bg` at ~0.7–0.85 alpha or a gradient) ensuring the card region holds AA contrast. Honors `prefers-reduced-motion` (no animation). Accepts no props; reads an optional `auth-scene` asset if present.

- [ ] **Step 2: CenteredLayout.vue** — `<AuthBackdrop/>` + a centered `Card` (Surface, `lg` radius, Overlay shadow only if floating) with a `<slot/>`, a brand mark (the one Ember moment), and `<LocaleSwitcher/>` in a corner. Used by the threshold pages via a `<RouterView/>` nested layout OR imported per page (choose per-page import for Spec 1 simplicity).

- [ ] **Step 3: TDD LocaleSwitcher.** Test: mounting shows the current locale; selecting another calls the i18n locale setter. Implement with the `ui/` primitives (a small select/menu). (Only `en` available now — assert the switch mechanism, not multiple locales.)

- [ ] **Step 4: build + commit.**
```bash
git add dashboard/src/components/custom dashboard/src/pages/CenteredLayout.vue dashboard/src/assets
git commit -m "feat(web): threshold shell — AuthBackdrop (Drenched), CenteredLayout, LocaleSwitcher

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: `/login` page + auth flows

**Goal:** The login page with all three methods — WebAuthn passkey (primary), password→TOTP fallback, and federation — re-derived cleanly, with focused components and tests.

**Files:**
- Create: `dashboard/src/pages/LoginView.vue`
- Create: `dashboard/src/components/custom/{PasskeyButton.vue,PasswordTotpForm.vue,FederationButtons.vue}`
- Test: `dashboard/src/pages/LoginView.test.ts`, `dashboard/src/components/custom/PasswordTotpForm.test.ts`

**Acceptance Criteria:**
- [ ] Passkey: `POST /api/prohibitorum/auth/login/begin` → `passkeyGet(options)` → `POST /api/prohibitorum/auth/login/complete` → on success `goReturnTo()` (guarded `return_to`); user-cancel is handled quietly (no scary error).
- [ ] Password→TOTP: `POST /api/prohibitorum/auth/password/begin` → on success advance to the TOTP step → `POST /api/prohibitorum/auth/totp/verify` → success → `goReturnTo()`. Two-phase state is explicit.
- [ ] Federation: `GET /api/prohibitorum/auth/federation` lists providers; clicking → full-page redirect to `/api/prohibitorum/auth/federation/{slug}/login`.
- [ ] Errors render via `errors.<code>` (fallback to message) in `role="alert" aria-live="polite"`; `busy` guards re-entrancy; full keyboard path + visible Tide focus ring.

**Verify:** `cd dashboard && npm run test` → PASS (mocked api + webauthn); `mise dev-server` + `mise enroll-admin` → register a passkey → `/login` passkey sign-in lands on `return_to`.

**Steps:**

- [ ] **Step 1: FederationButtons.vue** — props: none; on mount `api.get('/api/prohibitorum/auth/federation')` → `[{slug,displayName}]`; render a button per provider; click → `window.location.assign('/api/prohibitorum/auth/federation/'+slug+'/login')`. Empty list → render nothing.

- [ ] **Step 2: PasswordTotpForm.vue** — explicit two-phase state machine (`phase: 'password' | 'totp'`): phase 1 username+password → `auth/password/begin`; on success set phase 'totp'; phase 2 code → `auth/totp/verify`; emits `success` on completion. Uses `useApi` for busy/error. TDD: test asserts phase-1 submit calls `/password/begin` with the body, advances to phase 2 on success, and phase-2 submit calls `/totp/verify`; assert a UI effect (the TOTP field appears) — not just the mock.

- [ ] **Step 3: PasskeyButton.vue** — click → `useWebauthn` flow: `api.post('/auth/login/begin')` → `passkeyGet(options)` → `api.post('/auth/login/complete', assertion)` → emit `success`; `isUserCancel` → silent reset (no error banner).

- [ ] **Step 4: LoginView.vue** — wraps the three components in `CenteredLayout`; passkey primary (prominent), password/TOTP + federation secondary; on any `success` → `goReturnTo()`. TDD `LoginView.test.ts`: mock api so passkey-complete resolves → assert `goReturnTo` / location change invoked.

- [ ] **Step 5: run + commit.** `npm run test` (PASS) + `npm run build`.
```bash
git add dashboard/src/pages/LoginView.vue dashboard/src/components/custom dashboard/src/pages/LoginView.test.ts dashboard/src/components/custom/PasswordTotpForm.test.ts
git commit -m "feat(web): /login — passkey + password/TOTP + federation flows

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: `/consent` page

**Goal:** The OIDC consent screen — fetch context, render scopes, approve/deny, follow the server redirect.

**Files:**
- Create: `dashboard/src/pages/ConsentView.vue`, `dashboard/src/components/custom/ConsentScopeList.vue`
- Test: `dashboard/src/pages/ConsentView.test.ts`

**Acceptance Criteria:**
- [ ] On mount: `GET /api/prohibitorum/consent` → `{client, account, scopes}` rendered (client name, account, scope list with i18n scope descriptions; unknown scopes shown raw).
- [ ] Approve → `POST /api/prohibitorum/consent {decision:'approve'}` → follow the server's returned redirect (same-origin-guarded). Deny → `{decision:'deny'}` → follow redirect (RP `access_denied`).
- [ ] 401/expired ticket → redirect to `/error` (or `/login`) with a clear message.

**Verify:** `cd dashboard && npm run test` → PASS; covered e2e by smoke step 112 (consent flow) after the dist rebuild.

**Steps:**

- [ ] **Step 1: ConsentScopeList.vue** — props `scopes: string[]`; render each with `t('consent.scope.'+scope)` falling back to the raw scope; mono for any raw/technical scope.

- [ ] **Step 2: ConsentView.vue** — `useApi` to GET context; Approve/Deny buttons (Tide primary / ghost); POST decision; the server returns a redirect target → `window.location.assign(safeReturnTo(target))` (or follow a 3xx if the API responds with one — match the actual `handle_consent.go` contract: confirm whether it returns a JSON `{redirect}` or an HTTP redirect, and handle accordingly). TDD: mock GET → assert scopes render; mock approve → assert redirect invoked with the server value.

- [ ] **Step 3: run + commit.** `npm run test` + `npm run build`.
```bash
git add dashboard/src/pages/ConsentView.vue dashboard/src/components/custom/ConsentScopeList.vue dashboard/src/pages/ConsentView.test.ts
git commit -m "feat(web): /consent — context fetch, scope list, approve/deny → server redirect

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: `/logout` + `/error` pages

**Goal:** The logout landing (revoke + clear store) and the error page.

**Files:**
- Create: `dashboard/src/pages/LogoutView.vue`, `dashboard/src/pages/ErrorView.vue`

**Acceptance Criteria:**
- [ ] `/logout`: `POST /api/prohibitorum/auth/logout` → auth store `clear()` → show a "signed out" landing with a link to `/login`. (The `post_logout_redirect_uri` "return to app" branch was unreachable in the old build — do NOT invent it; document the limitation in a code comment.)
- [ ] `/error`: reads `error`/`error_description` from `route.query`; renders plain-language copy via `errors.<code>` fallback to the description; a link back to `/login`.

**Verify:** `mise dev-server` → visit `/logout` (clears session, `/me` → 401 after) and `/error?error=access_denied`; covered by smoke SPA-shell assertions.

**Steps:**

- [ ] **Step 1: LogoutView.vue** — on mount POST logout (tolerate already-logged-out), `clear()`, render landing. Comment the unreachable post-logout-redirect branch.

- [ ] **Step 2: ErrorView.vue** — render from query; plain-language; no stack/jargon (PRODUCT.md).

- [ ] **Step 3: build + commit.**
```bash
git add dashboard/src/pages/LogoutView.vue dashboard/src/pages/ErrorView.vue
git commit -m "feat(web): /logout (revoke + clear) and /error pages

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: `/enroll/:token` page

**Goal:** The enrollment ceremony — preview the invite, collect identity (bootstrap/invite) or show target (reset), run passkey registration → auto-login; route federation-bound invites to start-federation.

**Files:**
- Create: `dashboard/src/pages/EnrollView.vue`
- Test: `dashboard/src/pages/EnrollView.test.ts`

**Acceptance Criteria:**
- [ ] On mount: preview the enrollment (the GET preview endpoint — confirm exact path against `handlePreviewEnrollment`); render per intent — bootstrap/invite collect **username + displayName**; reset shows the target username.
- [ ] Passkey registration: `POST /enrollments/{token}/register/begin` → `passkeyRegister(options)` → `POST /enrollments/{token}/register/complete` → on success auto-login (the complete response returns a session) → redirect to `/`.
- [ ] Federation-bound invites (preview indicates an `expected_upstream_idp_slug`) → route to `GET /enrollments/{token}/start-federation` instead of the passkey path.
- [ ] Invalid/expired/consumed token → `/error` with a clear message.

**Verify:** `cd dashboard && npm run test` → PASS; `mise enroll-admin` prints a token → `/enroll/<token>` registers a passkey and lands authenticated. (This is the path `mise enroll-admin` + the smoke's enrollment depend on — keep it working.)

**Steps:**

- [ ] **Step 1: confirm contracts.** Read `handlePreviewEnrollment` + `handleEnrollmentBeginHTTP`/`CompleteHTTP` + `handleEnrollmentStartFederationHTTP` in `pkg/server` for exact paths, the preview response shape (intent, target, federation slug), and the complete response (session). The old `EnrollView.vue` (git `e45f356`) is advisory for edge cases (e.g. which fields are required per intent) — verify against the handlers, don't trust the old code.

- [ ] **Step 2: EnrollView.vue** — `useApi` preview on mount; branch by intent; form (username+displayName) for bootstrap/invite, read-only target for reset; `useWebauthn` registration flow; federation branch → `window.location.assign(start-federation)`. Explicit loading/error states; invalid token → router push `/error`.

- [ ] **Step 3: TDD.** `EnrollView.test.ts`: mock preview (invite intent) → assert username+displayName fields render; mock register begin/complete → assert redirect to `/` on success; mock a federation-bound preview → assert it routes to start-federation, not the passkey path.

- [ ] **Step 4: run + commit.** `npm run test` + `npm run build`.
```bash
git add dashboard/src/pages/EnrollView.vue dashboard/src/pages/EnrollView.test.ts
git commit -m "feat(web): /enroll/:token — preview + passkey registration + federation branch

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: CSP hardening + smoke + done-gate

**Goal:** Tighten the CSP now that Nuxt UI's runtime style injection is gone, rebuild + commit the dist, keep the smoke's SPA-shell assertions green, and verify the full done-gate.

**Files:**
- Modify: `pkg/webui/webui.go` (`setSecurityHeaders`)
- Modify: `cmd/smoke/main.go` (only if route/shell assertions need updating)
- Modify: `pkg/webui/dist/*` (rebuilt, committed)

**Acceptance Criteria:**
- [ ] CSP is `default-src 'self'; script-src 'self'; style-src-elem 'self'; style-src-attr 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; font-src 'self'; base-uri 'self'; form-action 'self'; object-src 'none'; frame-ancestors 'none'`, with the comment rewritten to explain the static-CSS + positioning-attr rationale. **Fallback:** if `style-src-attr` breaks rendering in a real HTTPS browser check, revert to `style-src 'self' 'unsafe-inline'` (still no worse than before) and note why.
- [ ] No inline `<script>` in the built `dist/index.html` (grep clean).
- [ ] `cmd/smoke` SPA-shell assertions (login/consent/logout/error serve the shell; API/discovery un-shadowed; CSP header present) stay green; updated if the served route set changed.
- [ ] Done-gate: `go build/vet/test ./...` exit 0; `npm run test` green; `npm run build` clean + `pkg/webui/dist` committed; `cmd/smoke` `SMOKE_EXIT=0`.

**Verify:** `setsid bash /tmp/run_v06.sh`; `cat /tmp/v06.result` → `SMOKE_EXIT=0`.

**Steps:**

- [ ] **Step 1: update `setSecurityHeaders`.** Replace the `style-src 'self' 'unsafe-inline'` directive with `style-src-elem 'self'; style-src-attr 'unsafe-inline'`, add `font-src 'self'`, and rewrite the comment (no more "Nuxt UI injects <style>"; now: Tailwind v4 + shadcn emit static CSS, only Reka positioning style *attributes* need inline). Keep `script-src 'self'`, X-Frame-Options, nosniff.

- [ ] **Step 2: grep the built shell** for inline scripts: `grep -i '<script' pkg/webui/dist/index.html` should show only `src=`-based module scripts, no inline JS. If Vite injected an inline module-preload or inline script, configure `build.modulePreload`/`@vitejs` to externalize, or add the documented fallback.

- [ ] **Step 3: smoke.** Locate the existing SPA-shell assertions in `cmd/smoke/main.go` (the `/login`,`/consent`,`/logout`,`/error` shell checks + the "CSP header present" check + step 5b). Update assertions only if the served routes changed (they shouldn't — same paths). Rebuild the dist (`cd dashboard && npm run build`) before running so the binary embeds the new SPA.

- [ ] **Step 4: full gate + commit.**
```bash
cd dashboard && npm run build && cd ..
mise exec -- go build ./... && mise exec -- go vet ./... && mise exec -- go test ./...
setsid bash /tmp/run_v06.sh   # poll /tmp/v06.result for SMOKE_EXIT=0
git add pkg/webui/webui.go pkg/webui/dist cmd/smoke/main.go
git commit -m "feat(web): tighten CSP (static CSS → strict style-src-elem); rebuild dist; smoke green

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review

- **Spec coverage:** §4 cleanup → Task 0; §5 stack + §6 structure + §1 scaffold → Task 1; §7 tokens → Task 2; §6 libs/composables → Task 3; `ui/` kit → Task 4; `custom/` shell → Task 5; §8 login/consent/logout/error/enroll flows → Tasks 6–9; §10 embed/CSP/testing/done-gate → Task 10; §2 advisory stance + §9 quality bar → baked into every task's "read old impl with judgement, re-derive" framing. All spec sections mapped.
- **Placeholder scan:** no TBD/"handle edge cases"/"similar to Task N"; crux code (api, returnTo, webauthn, tokens, vite/components.json) is concrete. A few contract-confirmation steps (consent redirect shape, enrollment preview/complete shapes, simplewebauthn v13 signature) are explicit *verify-against-handler* steps, not placeholders — correct, since those are the canonical sources and must not be guessed.
- **Type/name consistency:** `safeReturnTo`, `api.{get,post,put}`, `passkeyGet/passkeyRegister/isUserCancel`, `useApi/useWebauthn/useReturnTo`, auth store `me/ensureLoaded/isAdmin/clear`, `components/{ui,custom}`, `CenteredLayout` used consistently across Tasks 3–9.
- **Risk note:** the riskiest tasks are 1 (scaffold/embed) and 9 (enrollment — `mise enroll-admin` + smoke depend on it). Both have explicit build/smoke verification gates.
