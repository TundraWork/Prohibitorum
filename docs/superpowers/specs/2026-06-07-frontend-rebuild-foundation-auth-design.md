# Frontend Rebuild — Spec 1: Foundation + Auth slice

> Date: 2026-06-07
> Status: approved (brainstorm), pre-plan
> Scope: frontend. First slice of a from-scratch frontend rebuild that drops
> Nuxt UI. Delivers the new scaffold + the unauthenticated "threshold" pages
> end-to-end. The authenticated dashboard (Spec 2) and admin surface (Spec 3)
> follow as separate specs.

## 1. Why

The current frontend (`dashboard/`) is a Vue 3 + Vite + **Nuxt UI v4** (Reka UI +
Tailwind v4 + Tailwind Variants) SPA, embedded in the Go binary via
`pkg/webui` (`go:embed`) and served same-origin. We are **dropping Nuxt UI**:
its opinionated theme fights the bespoke "Welcoming Vault" design system
(`DESIGN.md`), and its runtime style injection forced a blanket
`style-src 'unsafe-inline'` CSP. We are rebuilding the frontend **from scratch**.

A library comparison (Reka UI vs Flowbite) was researched against this project's
constraints (custom design system, Tailwind v4, WCAG 2.2 AA + keyboard-first,
strict CSP, bilingual, embedded SPA). **Decision: Reka UI, scaffolded via
shadcn-vue.** Reka is the unstyled, accessible foundation *already underneath*
Nuxt UI — keeping it preserves the accessibility while dropping the opinionated
theme; shadcn-vue gives a styled head-start with full source ownership. Flowbite
was rejected: as a pre-styled, opinionated library it would reintroduce the
override-someone-else's-theme friction we are leaving, accessibility is not a
headline feature, and `flowbite-vue` docs are immature.

## 2. Sources of truth (canonical vs critical-reference)

- **Authoritative — preserve the contract:**
  - The backend API endpoints + request/response shapes (the Go server is
    stable; see `api.md` and the handlers under `pkg/server`).
  - The security properties that MUST hold: same-origin `return_to` guard,
    WebAuthn ceremony correctness, single-use enrollment/consent tokens, strict
    CSP, `credentials:'include'` session cookie flow.
  - `DESIGN.md` (the design system) and `PRODUCT.md` (users, principles,
    accessibility bar).
- **Critical reference only — re-engineer the logic:** the OLD `dashboard/`
  code (in git history at `e45f356` and the Login+Consent UI commits). Mine it
  to recover *what* each flow does and *which edge cases* exist, then **redesign
  how** — cleaner state, clearer naming, better error handling, focused units.
  The old logic quality is explicitly NOT a target to reproduce. Where the old
  approach was weak, the rewrite fixes it and says how.

## 3. Decomposition (this spec is slice 1 of 3)

- **Spec 1 (this doc):** new scaffold + base component kit + design tokens +
  embed/CSP + the threshold pages (`/login`, `/consent`, `/logout`, `/error`,
  `/enroll/:token`) with the WebAuthn / password→TOTP / federation / consent /
  enrollment flows.
- **Spec 2 (later):** authenticated dashboard — `DashboardLayout` + sidebar +
  Account pages (Profile, Security [passkeys/password/TOTP/recovery], Sessions,
  Connected accounts, Devices), the sudo step-up gate (`lib/sudo.ts` +
  `SudoModal`), and the signature `CodeField`/`CopyableUrl`/`StatusBadge`
  components.
- **Spec 3 (later):** Admin — accounts + detail, invitations, and the five
  now-API-backed admin areas (OIDC clients, SAML SPs, upstream IdPs, signing
  keys, audit), organized by feature.

## 4. Cleanup baseline (first plan steps, before the rewrite)

1. **gitignore** `.agents/`, `.claude/`, `.impeccable/` (impeccable-skill copies
   + small state — tooling artifacts, like `.superpowers/` already is). Never
   commit absolute `$HOME` paths (carry the `.vscode/` gitignore lesson).
2. **Commit** `DESIGN.md`, `PRODUCT.md` (the design spec driving the rebuild)
   and the trivial `README.md` edit.
3. **Nuke** `dashboard/` and `pkg/webui/dist/*` **in the same commit** that lands
   the new scaffold's first build, so `go build` is never left broken (the
   embed needs a non-empty `dist`).

## 5. Stack & dependencies

New `dashboard/package.json`:
- Runtime: `vue` 3, `vue-router`, `vue-i18n`, `pinia`, `reka-ui`,
  `@vueuse/core`, `class-variance-authority` + `tailwind-merge` + `clsx`
  (shadcn-vue variant/merge helpers), `lucide-vue-next` (icons),
  `@fontsource/hanken-grotesk` + `@fontsource/ibm-plex-mono` (self-hosted).
- Build/dev: `vite`, `@vitejs/plugin-vue`, `@tailwindcss/vite` (Tailwind v4),
  `typescript`, `vue-tsc`.
- Test: `vitest`, `@vue/test-utils`, `jsdom`.
- **Removed:** `@nuxt/ui` and its Vite plugin. shadcn-vue is a CLI/copy-paste
  tool, not a runtime dependency. TypeScript runs in `strict` mode.

## 6. Project structure

```
dashboard/src/
  main.ts                 bootstrap: router, i18n, pinia, mount <App/>
  App.vue                 <RouterView/> only (no global chrome)
  assets/main.css         Tailwind v4 entry + @theme tokens + base layer
  i18n.ts                 createI18n (English-first; zh later)
  locales/en.ts           messages
  router/index.ts         routes + installGuard (requiresAuth/requiresAdmin
                          reserved for Spec 2/3)
  stores/auth.ts          Pinia: session/me, ensureLoaded, isAdmin, clear
  lib/
    utils.ts              cn() (clsx + tailwind-merge)
    api.ts                typed api.get/post/put; credentials:'include';
                          defensive JSON parse; throws {code,message}
    returnTo.ts           safeReturnTo() same-origin guard
    webauthn.ts           passkeyGet/passkeyRegister (base64url, AbortController)
    devMode.ts            isDevMode() (loopback / vite-dev gate)
  composables/            useApi / useWebauthn / useReturnTo (shared flow logic)
  components/
    ui/                   vendored shadcn-vue primitives. PRISTINE; CLI-managed
                          via components.json aliases.ui; styled ONLY via tokens.
                          Spec 1: button, input, label, card, dialog, badge,
                          field, alert.
    custom/               OUR self-developed components; never CLI-touched.
                          Spec 1: AuthBackdrop, LocaleSwitcher, FederationButtons,
                          PasskeyButton, PasswordTotpForm.
  pages/                  LoginView, ConsentView, LogoutView, ErrorView,
                          EnrollView
```

**Component taxonomy (research-backed — see §11 refs):**
- `components/ui/` = vendored primitives, kept close to the shadcn-vue registry
  shape so they stay re-syncable. **Styled only through design tokens** (Tailwind
  `@theme` + CSS vars), never by hand-editing markup. No business logic.
- `components/custom/` = first-party, self-developed components (bespoke
  design-system pieces + compositions). The home for our own work; immune to
  CLI churn.
- **`components.json` alias fence:** `aliases.ui = @/components/ui` is set
  **distinct** from `aliases.components = @/components`, so `shadcn-vue add` only
  ever writes to `ui/` and never touches `custom/`. (`tsconfig` path alias
  `@ → ./src` must match.)
- **Spec 2/3 hybrid:** business logic lands as feature folders
  (`features/<domain>/` with its own components/composables) — NOT piled into
  `custom/`. `custom/` is for shared, cross-feature bespoke UI. Spec 1 stays flat
  (no premature feature nesting).

## 7. Design tokens (Tailwind v4 `@theme`)

`assets/main.css` defines the `DESIGN.md` system as CSS-first `@theme` custom
properties (OKLCH): brand `--color-tide`, `--color-tide-strong`,
`--color-ember`; state `--color-sage/amber/rose`; neutral
`--color-ink/muted/border/surface/sunken/bg`; the two font families
(Hanken Grotesk / IBM Plex Mono); radii (`sm 6 / md 10 / lg 14 / full`);
spacing scale; and the two elevation shadows (Raised, Overlay). shadcn-vue's
`--background`/`--foreground`/`--primary`/… aliases map onto these tokens.
Values are authored for **light mode**; a `.dark` block is **structured but left
for a later polish** (full dark theme out of scope here). Named rules from
`DESIGN.md` (Warm-Word-Cool-Hand, Scarce Accent, State-Has-a-Color,
Flat-Until-It-Acts, Code-Gets-Mono, the Threshold "Drenched" exception) govern
component styling.

## 8. Threshold pages, flows & the behavioral contract

The flows below are the **contract** (preserve the behavior + security
properties); the **implementation is re-derived** to a clean-architecture bar
(§9), reading the old code critically for edge cases.

**`/login?return_to=`** — method-selection auth on a centered card over
`AuthBackdrop`:
- WebAuthn passkey (primary): `POST /auth/login/begin` →
  `navigator.credentials.get(options)` (with `AbortController`) →
  `POST /auth/login/complete` → redirect to guarded `return_to`.
- Password→TOTP (fallback): `POST /auth/password/begin` → TOTP step →
  `POST /auth/totp/verify`.
- Federation: `GET /auth/federation` → provider buttons →
  `GET /auth/federation/{slug}/login` (full-page 302).
- Server-driven re-auth (`prompt=login` / SAML `ForceAuthn`) is transparent to
  the SPA; it only preserves `return_to`.

**`/consent`** — `GET /api/prohibitorum/consent` → `{client, account, scopes}` →
render scopes → Approve/Deny → `POST /api/prohibitorum/consent` → **follow the
server redirect** (same-origin-guarded).

**`/logout`** — landing page; `POST /auth/logout`; clear the auth store. The
`post_logout_redirect_uri` "return to app" branch was unreachable in the old
build — carry forward as a documented limitation (do not invent behavior).

**`/error`** — render `error` / `error_description` from the query; plain-language
copy per `PRODUCT.md`.

**`/enroll/:token`** — preview the enrollment (`GET` preview); for
bootstrap/invite collect **username + displayName**, for reset show the target;
run the passkey **registration** ceremony
(`/enrollments/{token}/register/{begin,complete}` →
`navigator.credentials.create`) → auto-login; federation-bound invites route to
`/start-federation`.

**Security-critical behaviors (preserve exactly, re-derive cleanly):**
- WebAuthn base64url encode/decode of challenge + credential fields;
  `AbortController` on `create`/`get`; pass the server's `publicKey` options
  through verbatim. (Conditional UI / autofill stays deferred — v0.7+.)
- `safeReturnTo()` same-origin guard re-applied at **every** entry point.
- `api` client: `credentials:'include'`, defensive JSON parse, throws
  `{code,message}`.
- Error display: `te('errors.'+code) ? t(...) : (err.message ?? fallback)`, in
  `role="alert" aria-live="polite"`; `busy` re-entrancy guard; explicit
  `type="button"` on non-submit buttons; full keyboard operability + a visible
  Tide focus ring on every interactive element.
- No inline `<script>` anywhere (`script-src 'self'`).

## 9. Quality bar (critical-review stance)

Every ported flow gets an explicit *"read old impl → note weaknesses →
redesign"* step in the plan — a port is not acceptable. Standards:
- TypeScript `strict`; a typed API layer (request/response types).
- Focused, single-responsibility units; shared flow logic extracted into
  composables (`useWebauthn`, `useReturnTo`, `useApi`).
- Explicit loading / error / empty states for every async surface.
- No prop-drilling — use the store or `provide/inject` where appropriate.
- Accessibility verified per component (focus management, keyboard paths, aria)
  against WCAG 2.2 AA + keyboard-first.
- Where the old code was tangled, the redesign states *how* it improved.

## 10. Embed, CSP, testing, done-gate

**Embed/build (carry forward):** Vite outputs to `pkg/webui/dist` (committed,
`go:embed`); `webui.Handler()` serves assets + `index.html` SPA fallback via chi
`NotFound`. After any frontend change: `cd dashboard && npm run build` then
`git add pkg/webui/dist`. Fonts self-hosted.

**CSP (target):**
`default-src 'self'; script-src 'self'; style-src-elem 'self';
style-src-attr 'unsafe-inline'; connect-src 'self'; img-src 'self' data:;
font-src 'self'; base-uri 'self'; form-action 'self'; object-src 'none';
frame-ancestors 'none'` + `X-Frame-Options: DENY` + `X-Content-Type-Options:
nosniff`. Restores strict `script-src` and strict style *elements* (Tailwind v4
+ shadcn-vue emit static CSS); `'unsafe-inline'` is scoped to Reka positioning
**style attributes** only. **Fallback:** `style-src 'self' 'unsafe-inline'` if
`style-src-attr` proves impractical (no worse than the old build). The plan
verifies no inline `<script>` exists in the built shell.

**Dev:** Vite dev server proxies `/api`,`/oauth`,`/saml`,`/oidc`,`/.well-known`
→ `:8080`; `mise dev-server` builds + serves; `mise dev-seed` seeds data.

**Testing:** vitest (no `@nuxt/ui` plugin) — component tests mount + assert real
call args AND a UI effect; focused unit tests for the critical logic (WebAuthn
ceremony, `safeReturnTo`, password/TOTP two-phase, consent decision). `cmd/smoke`
SPA-shell assertions (login/consent/logout/error serve the shell; API/discovery
un-shadowed; CSP header present) stay green, updated for any route changes.
Playwright stays deferred.

**Done-gate:** `go build/vet/test ./...` exit 0; vitest green; `npm run build`
clean + `pkg/webui/dist` committed; `cmd/smoke` `SMOKE_EXIT=0`.

## 11. References (research)

- Reka UI (headless, accessible; the foundation under Nuxt UI):
  https://reka-ui.com/ , https://github.com/unovue/reka-ui
- shadcn-vue (Reka + Tailwind v4, copy-paste/owned source):
  https://www.shadcn-vue.com/ , components.json aliases:
  https://www.shadcn-vue.com/docs/components-json
- Flowbite (rejected): https://flowbite-vue.com/ ,
  https://github.com/themesberg/flowbite-vue
- Component structure best practice (keep `ui/` pristine; alias fence;
  token-driven styling): https://shadcnspace.com/blog/shadcn-ui-handbook ,
  https://medium.com/write-a-catalyst/shadcn-ui-best-practices-for-2026-444efd204f44
- Vue large-app architecture (hybrid: shared UI layer + feature-based business
  logic): https://alexop.dev/posts/how-to-structure-vue-projects/ ,
  https://vueschool.io/articles/vuejs-tutorials/how-to-structure-a-large-scale-vue-js-application/
- CSP with headless overlay libs (style-src-elem nonce + style-src-attr):
  https://github.com/radix-ui/primitives/issues/3063 ,
  https://mui.com/material-ui/guides/content-security-policy/

## 12. Out of scope (this slice)

- The authenticated dashboard (Spec 2) and admin surface (Spec 3).
- Full dark theme (tokens structured for it; light ships now).
- The sudo step-up gate + `SudoModal` (Spec 2 — authenticated-area concern).
- `CodeField`/`CopyableUrl`/`StatusBadge` (Spec 2, when first needed).
- zh locale strings (English-first; i18n infra in place, zh in a later pass).
- Conditional UI / passkey autofill (v0.7+).
- Playwright e2e (deferred, unchanged).
