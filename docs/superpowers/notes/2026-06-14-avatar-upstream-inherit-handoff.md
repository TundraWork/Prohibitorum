# Handoff — Account avatars DONE; NEXT: inherit avatar from upstream IdP

**Date:** 2026-06-14
**Branch:** `master` (direct commits; **26 commits unpushed** — `origin/master..HEAD`).
**Resume by reading this file first, then the avatar spec/plan below.**

---

## TL;DR

- **Account avatars (uploaded-only) shipped end-to-end** this session: storage, image
  pipeline, endpoints, dashboard UI (incl. a crop step), and downstream exposure
  (OIDC `picture` claim + SAML `avatar_url` source). All gates were green (go
  build/vet/test, vitest, vue-tsc, smoke `SMOKE_EXIT=0` incl. an avatar round-trip,
  dist committed).
- **NEXT FEATURE (this handoff's purpose):** *inherit a user's avatar from the
  upstream IdP* on federated login — read the OIDC `picture` claim, fetch + resize the
  image, store it as the account's avatar. This was the explicit, deliberately-deferred
  follow-up from the avatar spec (it was scoped OUT of v1).
- ⚠️ **OPEN RISK before any prod deploy — migration numbering** (see below).

## How to resume the NEXT feature

1. Read the avatar design spec: `docs/superpowers/specs/2026-06-13-account-avatars-design.md`
   — its "Non-goals" section names this follow-up; its architecture is what you extend.
2. Read the avatar plan: `docs/superpowers/plans/2026-06-13-account-avatars.md` (the
   shipped tasks; `.tasks.json` all `completed`).
3. Brainstorm → spec → plan the upstream-inherit feature (it has real design decisions —
   see "Open questions" below). Use the same brainstorm→writing-plans→subagent-driven flow.

## Shipped this session (all on master, unpushed)

Account-avatar feature (commits `d39e7bd`..`374e6e5`):
- **DB:** `account_avatar(account_id PK FK, bytes bytea)` table + `account.avatar_content_type`/`avatar_etag` (small, on `account`). Image bytes live in the separate table so ordinary `SELECT *` account reads never load them. sqlc queries: `UpsertAccountAvatarBytes`, `SetAccountAvatarMeta`, `ClearAccountAvatarBytes`, `ClearAccountAvatarMeta`, `GetAvatarBySubject`.
- **Pipeline `pkg/avatar`:** `Process(raw) → (webp512², etag)` — decode png/jpeg/gif/webp, center-crop square, resize 512² (`x/image/draw`), encode **WebP q85/m6** via `gen2brain/webp` (libwebp→WASM via wazero, **`nodynamic` tag, no cgo**). 5 MiB + 10000px guards. `encodeAvatar()` is the only format-aware spot. `PublicURL(subject,etag,origin)` / `AccountURL(a,origin)` build `<origin>/avatar/<subject>?v=<etag8>` (one URL builder, used everywhere).
- **Endpoints (`pkg/server/handle_avatar.go`):** `PUT`/`DELETE /api/prohibitorum/me/avatar` (authed self, sudo-free, tx via `dbPool`/`avatarQueriesOverride` seam) + **public `GET /avatar/{subject}`** (by `oidc_subject` UUID; `image/webp` + ETag + `Cache-Control: public, max-age=86400`; 304; 404 for none/unknown/**disabled**). Registered in `pkg/server/server.go` (~line 370).
- **Contracts:** `SessionView.AvatarURL` + `AccountView.AvatarURL` (both `*string`, built via `avatar.AccountURL`). `sessionView` is a `*Server` method in `handle_auth.go:41`.
- **Downstream:** OIDC `picture` in `profileClaims(a, origin)` (`pkg/protocol/oidc/claims.go`, profile-scope-gated; origin = `idTokenInput.AvatarOrigin` / userinfo passes `PublicOrigins[0]`, with an empty-PublicOrigins guard — see `8ecb768`/`0082428`). SAML `avatar_url` source in `resolveSource(..., origin)` (`pkg/protocol/saml/attributes.go`; origin = `baseURL()` = `PublicOrigins[0]`).
- **Dashboard:** `UserAvatar.vue` (`src` → `<img>` w/ error fallback to initials → icon), `EditProfileDialog.vue` (avatar upload/remove + display name; **crop step** via `AvatarCropper.vue` + `vue-advanced-cropper@2.8.9` MIT), `NavUser.vue` shows avatar, admin list/detail render avatars. `api.upload`/`api.del` + `auth.reload()` added.
- **Post-ship fixes (real bugs found in live use):**
  - `7dd0e8d` remove-avatar did nothing: `api.request` returned `undefined` for 204 bodies so the dialog's `if (ok!==undefined)` skipped the reload → now returns `{}` and the dialog gates on `!error.value`. (The dialog test had *mocked* `del` to return `{}`, hiding it; added an api-layer 204 regression test.)
  - `748034f` cropper blanked/overflowed on **large** images → `min-w-0` on the cropper + container so it scales to the dialog instead of expanding to the image's natural width.
  - `374e6e5` **the real cropper blocker**: the CSP (`pkg/webui/webui.go`) had `connect-src 'self'` / `img-src 'self' data:` — no `blob:` — which blocked the cropper reading/displaying the page-created `blob:` image. Now `connect-src 'self' blob:` + `img-src 'self' data: blob:`. Same commit: Vite was inlining small woff2 subsets as `data:` URIs which `font-src 'self'` blocked (dashboard fell back to system fonts) → `vite.config.ts` now sets `assetsInlineLimit` to never inline fonts (served from `/assets`).

Also shipped earlier this session (same unpushed batch): the **account dropdown + Security as the default page** (NavUser dropdown replaced the Profile page; `/` → `/security`); the **Podman dev-environment refactor**; the **no-`Co-Authored-By`-trailer** rule (see memory `feedback_no_coauthor_commits`).

## ⚠️ OPEN RISK — migration numbering (verify before prod deploy)

`db/migrations/002_avatar.sql` is **goose version 2**. The migrations were squashed
(`39f86f4` "single consolidated migration") into `001_initial.sql` (v1) — but a DB
migrated under the **old** scheme sits at **goose version 10**. `goose up` only applies
versions `> current`, so **`002` (v2) will NEVER apply to a pre-squash v10 database**
(it silently does nothing → the `avatar_*` columns + `account_avatar` table are missing →
`column "avatar_content_type" does not exist` at runtime). This was hit locally this
session; the user fixed their *local* dev DB (the Podman container is fresh/v2) but **did
NOT renumber the migration**. **Action for the new session:** determine whether any
deployed/kept DB is pre-squash (v10). If yes (or unsure), **renumber `002_avatar.sql` →
`011_avatar.sql`** (version > 10) and use `ADD COLUMN IF NOT EXISTS` / `CREATE TABLE IF
NOT EXISTS` so it applies cleanly on both fresh (v1/v2) and pre-squash (v10) databases.
Establish the convention that post-squash migrations start at 11.

## NEXT FEATURE — inherit avatar from upstream IdP

**Goal:** on federated OIDC login, if the upstream provides a `picture` claim (a URL),
fetch that image, run it through `pkg/avatar.Process`, and store it as the account's
avatar — so federated users get an avatar automatically (which then flows downstream via
the OIDC `picture` claim + SAML `avatar_url` already built).

**Integration points (verified):**
- Claim mapping: `pkg/federation/oidc/modes.go:155-157` maps `username`/`displayName`/`email`
  via `ClaimString(tokens.Raw, idp.<X>Claim)` (helper in `client.go:82`). The
  auto-provision insert is `InsertAccount` (~modes.go:216). Re-login email refresh is
  `federation.go:608` (`UpdateAccountEmail`).
- `upstream_idp` table (in `001_initial.sql`) has `username_claim`/`display_name_claim`/
  `email_claim` columns (defaults `preferred_username`/`name`/`email`). Add a
  `picture_claim` column (default `picture`) the same way; surface it in the admin
  Upstream-IdP form + the `upstream-idp` CLI + the contract/handlers.
- Outbound fetch of the `picture` URL **must go through the SSRF dial-screen** the
  federation client already uses for token/userinfo (see `CONFIG.md` "outbound-federation
  SSRF dial-screen" + `PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK`). Bound size
  (reuse `avatar`'s 5 MiB cap) + a timeout. Don't let a hostile `picture` URL be an SSRF
  vector.
- Store via the existing tx path: `UpsertAccountAvatarBytes` + `SetAccountAvatarMeta`
  (reuse, don't reinvent).

**Open questions to brainstorm (don't pre-decide):**
- **When to set it:** only on first auto-provision, or refresh every federated login?
- **Don't clobber user uploads:** if the account already has a (user-uploaded) avatar,
  do we overwrite from upstream? Likely need an "avatar source" notion (uploaded vs
  upstream) or "only set if none" so a user's deliberate upload isn't replaced on next
  login. There is currently NO avatar-source column — decide whether to add one.
- **Fetch failures are non-fatal:** a missing/oversized/unreachable `picture` must not
  block login — log + continue.
- **Scope:** OIDC federation only (SAML-as-login is OUT OF SCOPE per project memory).

## Dev environment + gate (current)

- **Dev Postgres runs in a Podman container** (`podman compose up -d`, `compose.yaml`,
  localhost:**5432**, db `prohibitorum_dev`, user/pass `prohibitorum`). The old hand-started
  `/tmp/prohibitorum-pg` cluster on `:55432` is RETIRED (see memory
  `reference_dev_postgres_podman`). If your shell has a stale `PROHIBITORUM_DATABASE_URL`
  pointing at `:55432`, unset it — `dev-env.sh` honors an already-set value.
- Run: `podman compose up -d` → `mise dev-server` → `mise enroll-admin -- --new` → open
  `http://localhost:8080`. `mise db:up`/`db:status` now source `dev-env.sh`.
- **Gate:** `CGO_ENABLED=0 go build -tags nodynamic ./... && go vet ./... && go test ./...`
  (the `nodynamic` tag keeps the avatar WASM path cgo-free); `cd dashboard && npm run test
  && npm run build`; smoke `SMOKE_EXIT=0` (runbook in
  `2026-06-09-tier1-self-service-admin-reads-DONE-handoff.md`; needs
  `PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK=true`); rebuild + commit `pkg/webui/dist`
  (Vite hashes are non-deterministic; embedded via `go:embed`).
- **Commits:** NO `Co-Authored-By` / AI-attribution trailer (firm user rule). Direct to
  master. 26 commits unpushed — the user pushes; `git push` fast-forwards.

## Browser-debugging tip (used to crack the cropper)

System `chromium` is at `/usr/sbin/chromium`. For UI bugs that need real-browser
evidence without auth: stand up a tiny Vite probe page mounting the component (+ the
project's `main.css` / a Reka `Dialog`), then `chromium --headless --no-sandbox
--allow-file-access-from-files --virtual-time-budget=8000 --screenshot=/tmp/x.png <url>`
and Read the PNG. A CSP `<meta>` + Vite HMR makes virtual-time hang, so test the CSP via
the served header instead. This is how the `blob:`/CSP root cause was found.
