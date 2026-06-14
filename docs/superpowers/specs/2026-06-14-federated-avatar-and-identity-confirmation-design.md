# Federated identity confirmation + upstream avatar inheritance

**Date:** 2026-06-14
**Status:** Approved (design) — pending implementation plan
**Scope:** Backend (Go) + dashboard SPA. On upstream-OIDC federated login: (1) a
first-time (unconfirmed) identity must be **confirmed by the user** on a `/welcome`
interstitial before any durable session is granted; (2) the user's avatar is
**inherited from the upstream IdP** (id_token `picture`, else UserInfo) in the
background, normalized via `pkg/avatar`, and stored — unless the user has taken
control of their own avatar.

## Problem / goal

Two related gaps on the federated-login path:

1. **No identity confirmation.** Auto-provisioned federated accounts are created and
   signed in silently. There is no point at which the user confirms "yes, this is the
   upstream identity I want to connect" — a wrong-account selection at the OP silently
   creates and logs into an account.
2. **No avatar.** Federated users get the initials fallback; the uploaded-avatar
   feature (shipped) has no way to seed an avatar from the upstream `picture` claim.

This design adds a `/welcome` confirmation interstitial (gating the durable session)
and inherits the upstream avatar in the background, surfacing fetch progress so the UI
can show a spinner and the interstitial can wait for it.

## Decisions (locked during brainstorming)

- **Avatar refresh policy: source-tracked.** A new `account.avatar_source` column
  (`NULL` | `'upstream'` | `'user'`). Federation refreshes the avatar from upstream on
  every (confirmed) login **unless** `avatar_source = 'user'`. A user upload **or**
  deliberate removal sets `'user'` (the user owns the decision; upstream must not touch).
- **Picture source: id_token + UserInfo fallback.** Read `picture` from the id_token
  claims; if absent, call the UserInfo endpoint through the same SSRF-hardened RP client.
  (Entra needs Graph and remains uncovered — out of scope.)
- **Execution model: background.** The fetch+process+store runs in a detached goroutine
  after the callback; login is never blocked on a network image fetch. A short-TTL KV
  status key is polled by the frontend.
- **Identity confirmation: `/welcome` interstitial, session withheld until YES.** A new
  unconfirmed `account_identity` does **not** grant a dashboard session. The callback
  creates a short-lived **confirmation grant** (KV + cookie, mirroring the federation-
  state pattern) and redirects to `/welcome`. The session is issued only by the confirm
  endpoint on **YES**. Closing the page (or **NO**) leaves the identity unconfirmed and
  no session is ever issued — equivalent effects.
- **Confirmation is an `account_identity.confirmed_at` flag.** New auto-provision
  identities are pending (`NULL`); confirming sets `now()`. **Existing** identities are
  backfilled as confirmed (`= linked_at`) so this feature never forces current federated
  users back through confirmation.
- **Invite-federation auto-confirms.** The invite *is* the authorization; gating it
  would risk burning the consumed invite. Invite identities are inserted and immediately
  confirmed in the same tx → normal session, no `/welcome` gate. (The background avatar
  fetch still runs; the dashboard spinner covers it.)
- **Re-login with a still-unconfirmed identity re-enters `/welcome`** (reuses the dormant
  account — no re-provision, no username collision).
- **Reuse, don't reinvent:** the SSRF dial-screen (`httpclient.go`), `avatar.Process`,
  the avatar store queries, the KV-state + browser-binding cookie pattern, and the
  `SetNX` primitive.

## Non-goals

- **Entra/Graph avatar fetch**, gravatar fallback, animated avatars.
- **SAML-as-login** (out of scope per project memory; SAML stays downstream-only).
- **Editing the provisioned identity** on `/welcome` (display name etc.). Confirm or
  decline only; edits happen later in account settings.
- **Per-request "needs confirmation" middleware gate.** Not needed — the session is
  withheld until confirm, so there is no half-authenticated session to police.
- **Re-confirmation on later logins.** Confirmation is once per identity.

## Architecture

### 1. Database — one new migration (`db/migrations/<NN>_federation_confirm_avatar.sql`)

> **Migration numbering (prerequisite — see Risks).** The repo has an unresolved risk:
> `002_avatar.sql` is goose v2 but pre-squash DBs sit at v10, so it never applies there.
> This migration MUST be numbered above any deployed DB's version and use
> `IF NOT EXISTS` / idempotent guards. Resolve the `002 → 011` renumber first, then number
> this `012`. (Confirm with the operator before deploy; for fresh dev DBs any number > the
> last applied works.)

Columns + backfills (all guarded / idempotent):

- `account.avatar_source text` — `NULL` | `'upstream'` | `'user'`.
  - **Backfill:** `UPDATE account SET avatar_source = 'user' WHERE avatar_etag IS NOT NULL;`
    Every avatar that exists today was user-uploaded → protect it from upstream clobber.
- `upstream_idp.picture_claim text NOT NULL DEFAULT 'picture'` — mirrors
  `username_claim` / `display_name_claim` / `email_claim`.
- `account_identity.confirmed_at timestamptz` (nullable; pending = `NULL`).
  - **Backfill:** `UPDATE account_identity SET confirmed_at = linked_at WHERE confirmed_at IS NULL;`
    Existing identities count as already confirmed.

sqlc (`db/queries/*.sql` → regenerate `pkg/db`):

- `SetAccountAvatarMeta` (existing): extend to also set `avatar_source` — i.e. the
  upstream store path sets `avatar_source = 'upstream'`, the user-upload path sets
  `'user'`. Cleanest: a small dedicated query per source, or one param. Decided shape:
  - `SetAccountAvatarMetaUpstream :exec` → sets content_type/etag + `avatar_source='upstream'`.
  - `SetAccountAvatarMetaUser :exec` → sets content_type/etag + `avatar_source='user'`.
  - `ClearAccountAvatarMeta` (existing): on user remove, also set `avatar_source='user'`
    (sticky lockout). On *upstream* clear (rare — not used in v1) it would set `'upstream'`,
    but v1 never clears from upstream, so the existing clear is the user-remove path.
- `ConfirmAccountIdentity :exec` — `UPDATE account_identity SET confirmed_at = now()
  WHERE id = $1 AND confirmed_at IS NULL` (idempotent).
- `InsertAccountIdentity` (existing) inserts with `confirmed_at` left `NULL` (pending) —
  no signature change. `applyInviteOnly` calls `ConfirmAccountIdentity` in its tx.
- `GetAccountIdentityByIssuerSub` / `GetAccountByID` auto-gain the new columns (they are
  `SELECT *` / return the row); the federation re-login path reads `confirmed_at`.

`FederatorQueries` (and `ModesQueries` where needed) gain: `UpsertAccountAvatarBytes`,
`SetAccountAvatarMetaUpstream`, `ConfirmAccountIdentity`, `GetAvatarMetaByAccount`
(small read of current etag/source for the source guard + etag de-dupe).

### 2. Claim plumbing — `pkg/federation/oidc/client.go`

- **Hoist `picture`** into `Tokens.Raw` so `ClaimString(raw, idp.PictureClaim)` resolves
  the default `'picture'`: add `if claims.Picture != "" { raw["picture"] = claims.Picture }`
  alongside the existing name/email/sub/iss hoists. (Today `picture` is parsed into the
  typed `UserInfoProfile.Picture` field and dropped from `claims.Claims`.)
- **`Client.UserInfo(ctx, accessToken string) (map[string]any, error)`** — calls
  `rp.Userinfo[*oidc.UserInfo]` through the embedded RelyingParty (so it rides the same
  hardened, SSRF-screened, size-capped HTTP client). Returns a `map[string]any` of the
  userinfo claims (typed `Picture` hoisted under `"picture"`, plus `.Claims` extras) so
  `ClaimString` works uniformly. Errors are returned (caller treats them as non-fatal).

### 3. SSRF-guarded image fetch — `pkg/federation/oidc/avatar_fetch.go`

- `fetchUpstreamAvatar(ctx, rawURL string, allowPrivate bool) ([]byte, error)`:
  - Parse the URL; require `https`. (Reject non-https / userinfo, like `ValidateIssuerURL`.)
  - GET through a hardened client built from the **same** `screenDialControl(allowPrivate)`
    + capping transport, with the response cap raised to **5 MiB** to match
    `avatar.Process`'s input cap, plus the standard redirect/timeout bounds.
  - Reject non-`image/*` `Content-Type` (cheap fail-fast; `avatar.Process` is the real
    validator).
  - Return raw bytes (caller passes to `avatar.Process`).
- Refactor `hardenedHTTPClient` to accept a `maxBytes` parameter (current callers pass the
  existing `maxFederationResponseBytes`; the avatar fetch passes 5 MiB) so the dial-screen
  logic stays single-sourced.

### 4. Background avatar job + KV status

When the callback determines a refresh should run (see §5 for *when*), the Federator
launches a **detached goroutine** (`context.Background()` + its own timeout — never the
request context, which is cancelled when the HTTP response completes):

1. `kvStore.SetNX(avatarFetchKey(accountID), "pending", 60s)` — the `SetNX` dedupes
   concurrent logins; if the key already exists, the goroutine exits (another fetch is in
   flight).
2. Resolve the picture URL: `ClaimString(tokens.Raw, idp.PictureClaim)`; if `""`, call
   `Client.UserInfo` and `ClaimString(userinfo, idp.PictureClaim)`. If still `""` → clear
   the key and exit (no picture; not an error).
3. `fetchUpstreamAvatar` → `avatar.Process` → `(webp, etag)`.
4. If `etag == current avatar_etag` → skip the write (no-op refresh). Else store in a tx:
   `UpsertAccountAvatarBytes` + `SetAccountAvatarMetaUpstream` (sets `avatar_source='upstream'`).
5. Always clear the KV key at the end (success or failure). **All errors are non-fatal:**
   log + clear + return.

**Source guard:** the goroutine re-reads avatar meta and bails if `avatar_source = 'user'`
(belt-and-suspenders; the caller also checks before launching). This prevents clobbering a
user upload even across a race.

Status key helper lives next to the federation KV keys (`state.go`): `avatarFetchKey(id)`.

### 5. Confirmation flow (the load-bearing change)

`Resolve` / `applyInviteOnly` return an outcome that tells the callback whether to issue a
session directly or route to confirmation. Introduce:

```go
type ResolveOutcome struct {
    AccountID int32
    IsNew     bool
    Confirmed bool // true → issue session now; false → /welcome confirmation gate
}
```

- **Auto-provision (new account):** insert identity pending → `Confirmed=false`.
- **Re-login (existing identity):** `Confirmed = existing.ConfirmedAt.Valid`.
- **Invite-federation:** insert identity + `ConfirmAccountIdentity` in-tx → `Confirmed=true`.

`HandleCallback` (federation.go) after resolving + the disabled-account check:

- **`Confirmed == true`** → behave as today: `sessionStore.Issue` + redirect to `ReturnTo`.
  Then launch the background avatar job (unless `avatar_source='user'`).
- **`Confirmed == false`** → do **not** issue a session. Launch the background avatar job
  (so the avatar is ready to show on `/welcome`). Create a **confirmation grant** and
  redirect to `/welcome`.

**Confirmation grant** (mirrors the federation-state pattern in `state.go` + the
`FedStateCookieName` browser-binding cookie):

- KV entry under `ConfirmKey(token)` holding `{accountID, idpID, idpSlug, returnTo,
  browserBinding}` with a short TTL (~15 min). `browserBinding` = SHA-256 of an
  anti-forgery cookie value (single-use, like the login flow).
- The HTTP layer sets the anti-forgery cookie and redirects to `/welcome` (relative, this
  origin).

**HTTP endpoints (`pkg/server/handle_federation_confirm.go`):**

- `GET /api/prohibitorum/auth/federation/confirm` — **grant-scoped** (reads the grant
  cookie; no session). Validates the browser-binding, returns the identity to confirm:
  `{ idpDisplayName, displayName, username, email, avatarUrl, avatarPending }`.
  `avatarUrl`/`avatarPending` are read from the account row + the KV status key. The SPA
  polls this until `avatarPending == false`.
- `POST /api/prohibitorum/auth/federation/confirm` — **YES**. Pops the grant (single-use),
  re-checks browser-binding, `ConfirmAccountIdentity(identityID)`, `sessionStore.Issue`,
  sets the session cookie, clears the grant cookie, returns `{ redirect: returnTo }`.
- `POST /api/prohibitorum/auth/federation/confirm/decline` — **NO** (optional but tidy):
  Pops the grant + clears the cookie (proactively invalidates). The SPA then routes to
  `/login`. Closing the tab without calling this simply lets the grant expire — same
  effect.

**Re-login routing:** when `Resolve` matches an existing **unconfirmed** identity, it
returns `Confirmed=false` with the existing `AccountID` (no insert) → the callback creates
a fresh grant and redirects to `/welcome` again. The dormant account is reused.

### 6. Avatar source tracking on the self-service paths (`pkg/server/handle_avatar.go`)

- `PUT /me/avatar` → store via `SetAccountAvatarMetaUser` (sets `avatar_source='user'`).
- `DELETE /me/avatar` → `ClearAccountAvatarBytes` + clear meta with `avatar_source='user'`
  (sticky: a deliberate removal locks out upstream refill).

### 7. Status surfaces (two readers of the same KV key)

- **Grant-scoped** (pre-session, for `/welcome`): folded into the
  `GET .../federation/confirm` payload (`avatarPending`).
- **Session-scoped** (for the dashboard refresh spinner, returning users):
  `GET /api/prohibitorum/me/avatar/status` (authed) → `{ pending: bool }`. Optionally a
  `avatarPending` field on `/me`'s `SessionView` gives the SPA the initial signal without
  an extra call; the dedicated endpoint is for polling.

### 8. Frontend (dashboard SPA)

- **`WelcomeView.vue`** — new standalone (no sidebar) route `/welcome`. On mount, `GET
  .../federation/confirm`. Renders: "via {idpDisplayName}", a large `UserAvatar`
  (`:src="avatarUrl"`), display name, username, email. While `avatarPending` → spinner on
  the avatar + poll the confirm GET (capped, e.g. stop after ~30 s so the page never
  hangs). Two actions: **Continue** (`POST .../confirm` → `window.location = redirect`) and
  **Not me** (`POST .../confirm/decline` → go to `/login`).
  - **Continue gating:** disabled while `avatarPending` is true **and** within the poll
    window (default cap ~30 s); enabled the moment the fetch settles (success or failure)
    or the poll cap is reached. This honors "wait for the avatar to finish" without ever
    permanently hanging the page on a slow/missing/failed upstream image. The hard
    identity gate remains the explicit **YES**, not the avatar.
  - **Not me** is always enabled.
- **Router:** register `/welcome` as a standalone route (peer of `/login`, `/consent`).
  It is reached only by the server redirect; a direct visit without a valid grant gets a
  401 from the confirm GET → redirect to `/login`.
- **Dashboard refresh spinner:** `UserAvatar` gains an optional `loading` state; `NavUser`
  (and optionally the header avatar) checks `me.avatarPending` on load, shows the spinner,
  polls `/me/avatar/status`, and calls `auth.reload()` when it clears.
- **i18n** (`en.ts`): `welcome.*` (title, "via {idp}", continue, notMe, fetching avatar),
  any new `errors.*`. Grep apostrophes after editing `en.ts`.

### 9. Contracts / views

- `contract.SessionView` gains `AvatarPending bool` (`json:"avatarPending,omitempty"`) —
  populated from the KV status key in `sessionView`.
- New `contract.FederationConfirmView { IDPDisplayName, DisplayName, Username, Email
  string; AvatarURL *string; AvatarPending bool }` for the confirm GET.

## Data flow

```
Federated callback (Exchange → Resolve)
   │
   ├─ Confirmed=true (re-login / invite) ─▶ Issue session ─▶ redirect ReturnTo
   │        └─ launch bg avatar job (if avatar_source≠'user')
   │
   └─ Confirmed=false (new / unconfirmed) ─▶ NO session
            ├─ launch bg avatar job
            ├─ create ConfirmKey(token) grant + anti-forgery cookie
            └─ redirect ▶ /welcome

/welcome (grant cookie, no session)
   GET  .../federation/confirm ─▶ {identity, avatarUrl, avatarPending}  (poll until !pending)
   YES  POST .../confirm        ─▶ ConfirmAccountIdentity + Issue session + cookie ─▶ ReturnTo
   NO   POST .../confirm/decline ─▶ pop grant ─▶ /login     (close tab = grant expires)

bg avatar job (detached goroutine)
   SetNX avatar_fetch:<id>=pending(60s) ─▶ resolve picture (id_token|UserInfo)
      ─▶ fetchUpstreamAvatar (SSRF, ≤5MiB, image/*) ─▶ avatar.Process
      ─▶ if etag changed: Upsert bytes + SetMeta(source='upstream') ─▶ clear key
      (any failure: log + clear key; never blocks login)

dashboard (session): /me.avatarPending ─▶ spinner ─▶ poll /me/avatar/status ─▶ auth.reload()
```

## Error handling

| Condition | Result |
|---|---|
| No `picture` in id_token or UserInfo | bg job clears key, no avatar (not an error) |
| UserInfo call fails | logged; treated as no picture; login unaffected |
| Picture URL non-https / blocked IP / oversized / non-image / unreachable | bg job logs + clears key; login unaffected |
| Fetched image undecodable (`avatar.Process` error) | logged + key cleared |
| `avatar_source = 'user'` | bg job is not launched (and bails if it races) |
| bg goroutine dies before clearing key | KV TTL (60s) expires the key; FE poll cap backstops the UI |
| Confirm grant missing/expired/browser-binding mismatch | confirm GET/POST → 401/`federation_state_invalid`; SPA → `/login` |
| User closes `/welcome` | grant expires; identity stays unconfirmed; no session (= NO) |
| Re-login, identity still unconfirmed | back to `/welcome` (reuse account) |

## Security considerations

- **SSRF:** the picture fetch reuses the dial-time IP screen (loopback/RFC1918/ULA/
  link-local/metadata), https-only, redirect cap, 5 MiB body cap, timeout. A hostile
  `picture` URL cannot reach internal addresses or OOM the process. `allowPrivate` follows
  `cfg.AllowPrivateNetwork`.
- **Confirmation grant:** single-use (KV `Pop`), browser-bound (anti-forgery cookie hash),
  short TTL. A leaked grant token alone cannot confirm without the matching cookie.
- **Session withheld:** an unconfirmed identity grants no session, so there is no
  half-authenticated state to exploit; "close = NO" holds.
- **Anti-enumeration:** the confirm GET returns only the grant-holder's own just-
  authenticated identity. No new enumeration surface.
- **No invite burn:** invite identities auto-confirm in-tx; the gate never strands a
  consumed invite.

## Testing

- **`pkg/federation/oidc` unit:** `picture` hoist in `Exchange`'s `raw`; `Client.UserInfo`
  picture resolution (id_token-absent → UserInfo); `fetchUpstreamAvatar` rejects
  non-https / blocked IP / non-image / oversized (table-driven, reusing the dial-screen
  tests' shape); `Resolve` outcome (`Confirmed` for new vs existing-confirmed vs
  existing-unconfirmed); `applyInviteOnly` confirms in-tx; the bg job source-guard +
  `SetNX` dedup + etag-skip (with a fake querier + fake fetcher).
- **`pkg/server` handler:** confirm GET returns identity + `avatarPending`; confirm POST
  sets `confirmed_at` + issues a session + redirect; decline pops the grant; browser-
  binding mismatch → 401; `/me/avatar/status`; the callback issues a session for confirmed
  and redirects to `/welcome` (no session cookie) for unconfirmed; `PUT`/`DELETE /me/avatar`
  set `avatar_source='user'`.
- **Frontend (vitest):** `WelcomeView` renders identity, shows the spinner while
  `avatarPending`, polls and enables Continue when it settles / poll cap, Continue posts +
  navigates, Not-me declines + routes to `/login`; dashboard avatar spinner polls +
  reloads. Reka idioms; mock the confirm/status API.
- **Smoke (`cmd/smoke`):** drive a federated auto-provision against the test OP (the smoke
  already exercises federation) → assert the callback redirects to `/welcome` with **no**
  session cookie → `GET .../federation/confirm` returns the identity → poll until
  `avatarPending=false` → `POST .../confirm` yields a session + redirect → `GET
  /avatar/{subject}` 200 `image/webp` → OIDC `picture` present. Second case: a confirmed
  user who uploads an avatar (`avatar_source='user'`) then re-logs in federated — assert
  the upstream refresh does **not** overwrite it.

## Files

**Backend — new:** the migration; `pkg/federation/oidc/avatar_fetch.go` (+ test);
`pkg/server/handle_federation_confirm.go` (+ test).
**Backend — modified:** `db/queries/*.sql` (+ regen `pkg/db`); `pkg/federation/oidc/`
`client.go` (picture hoist + `UserInfo`), `httpclient.go` (parameterize `maxBytes`),
`modes.go` (`ResolveOutcome`, invite confirm, launch bg job hook), `federation.go`
(`HandleCallback` branch, `FederatorQueries` additions, `ConfirmKey`, grant create + bg
job), `state.go` (`ConfirmKey`, `avatarFetchKey`); `pkg/server/handle_federation.go`
(confirmed → session, unconfirmed → grant+redirect), `handle_avatar.go` (source on
upload/remove), `handle_me.go` (`avatarPending`), `server.go` (routes);
`pkg/contract/auth.go` (`SessionView.AvatarPending`, `FederationConfirmView`).
**Frontend — new:** `dashboard/src/pages/WelcomeView.vue` (+ test).
**Frontend — modified:** router; `UserAvatar.vue` (loading state); `NavUser.vue` (poll);
`stores/auth.ts` (`avatarPending`, status poll helper); `lib/api.ts` if needed;
`locales/en.ts`. Admin upstream-IdP form/detail + the `upstream-idp` CLI + contract gain
`pictureClaim` (mirror existing claim fields).

## Implementation phasing (for the plan)

1. **Schema + queries** — migration (3 columns + 2 backfills), sqlc, source-aware avatar
   meta queries, `ConfirmAccountIdentity`, querier-interface additions.
2. **Claim plumbing + fetch** — `client.go` picture hoist + `UserInfo`; `httpclient.go`
   `maxBytes`; `avatar_fetch.go`.
3. **Background job + status** — detached goroutine, `SetNX` dedup, source guard, etag-skip;
   `avatarFetchKey`; `/me/avatar/status`; `SessionView.avatarPending`.
4. **Confirmation backend** — `ResolveOutcome`, invite auto-confirm, `ConfirmKey` grant +
   cookie, confirm GET/POST/decline, `HandleCallback` branch + re-login routing.
5. **Frontend** — `WelcomeView` + route, dashboard spinner, store/api, i18n; admin/CLI
   `pictureClaim`.
6. **Smoke + done-gate** — federated confirm→avatar round-trip + the no-clobber case;
   rebuild + commit `pkg/webui/dist`.

## Risks / prerequisites

- **Migration numbering (prerequisite).** Resolve the open `002_avatar.sql` → `011`
  renumber (+`IF NOT EXISTS`) before adding this as `012`; otherwise pre-squash (v10) DBs
  silently skip both. Confirm with the operator whether any pre-squash DB exists.
- **Multi-instance KV.** The status key + confirmation grant live in KV. Dev uses an
  in-process memory KV (single instance); a multi-instance deployment needs a shared KV
  (Redis) for the spinner/grant to work across instances. (Federation state already has
  this property.)
- **Background goroutine lifetime.** Uses `context.Background()` + an explicit timeout;
  it must not capture the request context. On process shutdown an in-flight fetch is
  abandoned (acceptable — non-fatal, refetched next login).
- **Entra coverage.** `picture` is unavailable from Entra's id_token/UserInfo (Graph
  only); those users get no inherited avatar. Documented, not addressed.

## Done-gate

`CGO_ENABLED=0 go build -tags nodynamic ./...` / `go vet` / `go test ./...` (0),
`vitest` (green), `vue-tsc -b` (0), smoke `SMOKE_EXIT=0` (incl. the federated
confirm→avatar round-trip + the no-clobber case), rebuild + commit `pkg/webui/dist`.
