# Session handoff — Login + Consent UI DONE (12 tasks + per-task & final review)

> Future Claude: Prohibitorum now has its first browser frontend (a Vue 3 SPA
> embedded same-origin in the Go binary) AND a real OIDC consent flow. The whole
> chunk is committed on master, two-stage reviewed per task, integration-validated
> (full smoke green), and the final whole-feature review's findings are addressed.
> Prior chunks: session-cookie scoping (`2026-05-31-session-cookie-scoping-handoff.md`),
> v0.6 (`2026-05-31-session-handoff.md`).

## TL;DR — DONE
A Vue 3 + Vite + **Nuxt UI v4** (standalone, Reka UI + Tailwind v4) SPA — bilingual
**zh-CN + en** — providing `/login` (WebAuthn passkey + password→TOTP + federation),
OIDC `/consent`, `/logout`, and `/error`. It is built into `pkg/webui/dist` and
**embedded in the Go binary** (`go:embed`), served **same-origin** via the chi
`NotFound` handler with a strict CSP. Backed by a **net-new OIDC consent** flow:
a stored-grants table, a single-use account-bound consent KV ticket, the
`authorize` consent step (remembered-grant skip, `RequireConsent` trusted-skip,
`prompt=consent`/`prompt=none` handling, deny→`access_denied`), and a session-gated
consent context/decision API.

```
HEAD: fd7f219   branch: master   working tree: clean   (NO git remote)
go build ./... ✓   go vet ./... ✓   go test ./... ✓ (all ok)
frontend: npm run build ✓   vitest 8/8 ✓
smoke 1–113 ✓ (SMOKE_EXIT=0)   runtime: /login /consent /logout /error serve the SPA shell; discovery+API JSON un-shadowed; CSP present
```

## Commits (this chunk, after the spec `5c5432e` + plan `5f3a1dd`)
- `71348f8` migration 007 `oidc_consent` + sqlc queries
- `9b9a01f` `pkg/authn/consent.go` — single-use account-bound consent ticket (KV)
- `ec9d4a5` `authorize.go` consent step (remembered grants + ticket bounce; stale-reauth stripped)
- `1133314` consent context + decision API (`handle_consent.go`); approve stores grant union, deny→access_denied; same-origin `return_to` guard
- `3d20266` public `GET /api/prohibitorum/auth/federation` providers list
- `b6822ae` smoke steps 112–113 (consent flow e2e + federation list)
- `18837be` Vue scaffold (Vite + Nuxt UI v4.8.1 + router + vue-i18n zh/en + Pinia + api client + returnTo guard)
- `6b14b8c` embed SPA via `pkg/webui` + chi `NotFound` + strict CSP (Vite outDir → `pkg/webui/dist`; **dist committed**)
- `b103a58` `/login` page (passkey + password/TOTP + federation)
- `60e21bc` `/consent` page (scopes, approve/deny follow server redirect)
- `b009a64` `/logout` landing + `/error` page
- `8099d7d` **final-review fixes**: CSP `style-src 'self' 'unsafe-inline'` (Nuxt UI runtime style injection; script-src stays strict) + annotate unreachable logout return branch + `errors.server_error` locale
- `fd7f219` plan tasks.json (12/12 complete)

Spec: `docs/superpowers/specs/2026-05-31-login-consent-ui-design.md` (D1–D14).
Plan: `docs/superpowers/plans/2026-05-31-login-consent-ui.md` (+ `.tasks.json`).

## Frontend architecture (important for the next frontend chunk — the admin dashboard)
- **`dashboard/`** = Vue 3 + Vite + TS + Nuxt UI v4.8.1 (standalone via `@nuxt/ui/vite` plugin + `app.use(ui)` from `@nuxt/ui/vue-plugin`) + Tailwind v4 + vue-i18n (zh+en) + Pinia + vue-router.
- **Build → embed:** `vite build` outputs to **`pkg/webui/dist`** (NOT dashboard/dist — `go:embed` can't reach `..`). **`pkg/webui/dist` is COMMITTED** so `go build`/`go run`/smoke are self-contained. **After ANY Vue change you MUST `cd dashboard && npm run build` then `git add pkg/webui/dist`** — stale dist = stale UI in the binary. Asset hashes are content-derived (deterministic).
- **Serving:** `pkg/webui/webui.go` `Handler()` is wired as chi `router.NotFound` (in `NewServer`, after `registerOperations`); serves an embedded asset on a file hit, else `index.html` (SPA fallback); a directory hit (e.g. `/assets`) serves the shell (no listing). API/protocol routes register first so they're never shadowed.
- **CSP** (on every SPA response): `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; base-uri 'self'; form-action 'self'; object-src 'none'; frame-ancestors 'none'` + `X-Frame-Options: DENY` + `X-Content-Type-Options: nosniff`. `style-src` needs `'unsafe-inline'` because Nuxt UI/Reka UI inject `<style>` at runtime; `script-src` stays strict.
- **Dev:** Vite dev server proxies `/api`,`/oauth`,`/saml`,`/oidc`,`/.well-known` → `:8080`.
- **Vitest:** `vitest.config.ts` includes the `@nuxt/ui/vite` plugin so component tests can mount `U*` components. Tests: returnTo guard, PasswordTotpForm two-phase, ConsentScopeList.
- **Reusable patterns:** `lib/api.ts` (`api.get/post`, credentials:'include', defensive JSON parse, throws `{code,message}`); `lib/returnTo.ts` `safeReturnTo` (same-origin guard — re-guard at every entry point); error display = `te('errors.'+code) ? t(...) : (err.message ?? fallback)`; `busy` re-entrancy guard; explicit `type="button"` on non-submit `UButton`; `role="alert" aria-live="polite"` on error text.

## Backend consent (anchors)
- `db/migrations/007_oidc_consent.sql` + `db/queries/oidc_consent.sql` (`GetConsent`/`UpsertConsent`/`DeleteConsent`).
- `pkg/authn/consent.go` — `ConsentTicket{AccountID,ClientID,Scopes,RedirectURI,State}`, `DemandConsent`/`PeekConsent`(GET)/`ConsumeConsent`(POST, single-use), key `oidc:consent:<nonce>`, 10m TTL.
- `pkg/protocol/oidc/authorize.go` step 5 — consent check (uses `oidc_client.RequireConsent`; `RequireConsent=false` = trusted skip).
- `pkg/server/handle_consent.go` — `GET`/`POST /api/prohibitorum/consent` (session-gated; approve stores union grant + echoes same-origin-guarded `return_to`; deny → RP `access_denied`).
- `GET /api/prohibitorum/auth/federation` (public) → `[{slug,displayName}]`.

## Accepted / documented limitations (carry forward)
- **No real-HTTPS-browser visual verification of the styled UI** (no browser harness — same stance as the spec's no-browser-e2e). The CSP `style-src 'unsafe-inline'` fix unblocks Nuxt UI styling in principle, but a human/browser should confirm the login+consent screens render correctly over HTTPS before relying on them in production. A Playwright e2e is a good follow-up.
- **`prompt=login` + first-time (ungranted) consent → one extra login** after approval (single-use re-auth nonce can't span the consent round-trip). Fail-safe (over-auth), documented in `authorize.go`.
- **OIDC RP-initiated logout** redirects straight to the RP server-side (`/oidc/logout`); the SPA `/logout`'s `post_logout_redirect_uri` "Return to {app}" branch is currently UNREACHABLE (annotated, kept for a future `/oidc/logout`→SPA-landing wiring). D10's "auto-follows" wording over-claims vs. what shipped.
- Spec D5 says migration `006`; the file is `007` (006 was taken). Spec text is stale on that number only.

## Runtime quirks (unchanged — these bite)
- master, direct commits, **no git remote** (push/PR N/A). opus implementers for judgment-heavy, sonnet for mechanical; never haiku. Node via mise (`node = "24"`); npm (the dashboard has a package-lock; do NOT switch it to pnpm).
- Trust `go build ./...` exit 0 + `go vet`, NOT gopls `<new-diagnostics>` — it FALSELY reports `//go:embed all:dist` "no matching files", regenerated-sqlc methods "undefined", and `DeleteExpiredSAMLSessions` "undefined". Hit constantly this session; the build is authoritative.
- NEVER `pkill -f 'prohibitorum'` bare (kills the PG at `/tmp/prohibitorum-pg`). Smoke via `setsid bash /tmp/run_v06.sh`, poll `/tmp/v06.result` for `DONE`/`SMOKE_EXIT=0`. Precise kills: `pkill -f 'go-build.*/prohibitorum'` + `pkill -f 'cmd/prohibitorum'`.
- `mise exec --` prefix; `mise exec sqlc -- sqlc generate`; the `mise WARN ... goose` line is harmless.

## What's next (brainstorm/spec fresh)
- **Admin dashboard** — the natural next frontend chunk; the Vue/Nuxt UI foundation is laid (router, i18n, api client, embedding all reusable). Manage accounts/enrollments/OIDC clients/SAML SPs (currently CLI-only). Add `/me` self-service too.
- **A Playwright e2e** to close the no-browser-verification gap on login/consent.
- Security/ops hardening: HSM/KMS (v0.7+ deferral in AUDIT.md), SAML front-channel SLO + assertion encryption (deferred from v0.5).
