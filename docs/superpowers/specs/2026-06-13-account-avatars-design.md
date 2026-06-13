# Account avatars (uploaded-only, with downstream exposure)

**Date:** 2026-06-13
**Status:** Approved (design) — pending implementation plan
**Scope:** Backend (Go) + dashboard SPA. Adds a per-account avatar image, stored in Postgres, served from a public endpoint, surfaced in the dashboard, and exposed downstream via OIDC (`picture` claim) and SAML (mappable attribute).

## Problem / goal

The dashboard renders an initials `UserAvatar`, but there is no way to set an actual avatar image, and downstream relying parties get no profile picture. Add a mature avatar feature: users upload an image; it is normalized/compressed and stored; it renders across the dashboard; and it flows to relying parties as the standard OIDC `picture` claim and as a mappable SAML attribute.

## Non-goals (v1)

- **No federated `picture` ingestion.** Pulling the OIDC `picture` claim from upstream IdPs on federated login (fetch + resize) is a clean follow-up, deliberately out of scope.
- **No object storage / CDN / filesystem.** Bytes live in Postgres — consistent with the single-binary + Postgres model.
- **No animated avatars**, no per-RP avatar variants, no gravatar-style external fallback.
- **No pairwise-subject interaction work.** The public avatar URL is keyed by the existing global `oidc_subject`; pairwise `sub` remains a separate conditional item.

## Decisions (locked during brainstorming)

- **Storage:** raw bytes in Postgres (`BYTEA`), not base64 text.
- **Serving:** a single **public** endpoint keyed by the non-enumerable `oidc_subject` UUID, with HTTP caching. (Public because the OIDC `picture` claim is a URL a relying party / browser must fetch cross-origin without our session.)
- **Size:** center-crop to square, resize to **512×512**.
- **Encoder:** **WebP lossy (q≈80)** via `gen2brain/webp` — libwebp compiled to WASM, run with the pure-Go `wazero` runtime, built with the `nodynamic` tag (embedded WASM only, no `dlopen` of a system lib, no cgo). Isolated behind one `encodeAvatar()` function so the format can change later with no data migration. Trade-off accepted: embedded `libwebp.wasm` adds ~0.5–1.5 MB to the binary; WASM encode is ~tens of ms (acceptable for a user-initiated, non-hot path).
- **Decode (input):** png/jpeg/gif via stdlib, webp via `gen2brain/webp`; resize via `golang.org/x/image/draw`.
- **Upload UI:** the account-dropdown's "Edit display name" dialog becomes **"Edit profile"** (avatar + display name).
- **Migration:** a **new** `002_avatar.sql` — the consolidated `001_initial` is immutable now that the repo has a remote/may be deployed.

## Architecture

### 1. Database — `db/migrations/002_avatar.sql`

Add nullable columns to `account`:
- `avatar bytea` — the processed WebP bytes (null = no avatar → initials fallback).
- `avatar_content_type text` — always `image/webp` in v1; stored for forward-compat.
- `avatar_etag text` — hex SHA-256 of `avatar`, for `ETag` / cache-busting.

sqlc (`db/queries/account.sql` → regenerate `pkg/db`):
- `SetAccountAvatar` — UPDATE avatar, avatar_content_type, avatar_etag, updated_at WHERE id.
- `ClearAccountAvatar` — UPDATE the three avatar columns to NULL WHERE id.
- `GetAvatarBySubject :one` — SELECT avatar, avatar_content_type, avatar_etag, disabled WHERE oidc_subject = $1.
- Ensure both `oidc_subject` and `avatar_etag` are selected in the rows backing `SessionView` and `AccountView` — `GetAccountByID` already carries the account (gains `avatar_etag` as a new column); `ListAccounts` must add `oidc_subject` and `avatar_etag` to its projection so the admin list can build avatar URLs without an extra round-trip.

### 2. Image pipeline — `pkg/avatar/` (one focused unit)

- `Process(raw []byte) (out []byte, etag string, err error)`:
  - Reject `len(raw) > 5 MiB` → `ErrAvatarTooLarge`.
  - Decode (png/jpeg/gif stdlib; webp via `gen2brain/webp`). Undecodable → `ErrAvatarInvalidImage`.
  - Reject absurd decoded dimensions (e.g. either side `> 10000 px`) → `ErrAvatarInvalidImage` (decompression-bomb guard).
  - Center-crop to a square (cover), resize to 512×512 with `golang.org/x/image/draw` (CatmullRom).
  - `encodeAvatar(img) []byte` — WebP lossy q≈80 via `gen2brain/webp` (`nodynamic`). **Only this function knows the output format.**
  - `etag = hex(sha256(out))`.
- Pure function over bytes — unit-testable with no DB/HTTP.

### 3. URL helper (single source of truth)

`avatarURL(a db.Account) string` (or a small helper taking subject+etag): returns
`<config.PublicOrigins[0]>/avatar/<oidc_subject>?v=<etag[:8]>` when `avatar_etag` is set, else `""`.
Used by OIDC claims, SAML attributes, `SessionView`, and `AccountView` — one consistent, cache-busting URL.

### 4. Endpoints

Self-service (authed, `sessionReq`; sudo-free, matching `PUT /me` display-name):
- `PUT /api/prohibitorum/me/avatar` — `registerOpHTTP`; raw image body → `avatar.Process` → `SetAccountAvatar`; updates the in-memory session; returns `200` with `{ avatarUrl }` (or `204`). Errors map to `avatar_too_large` / `avatar_invalid_image`.
- `DELETE /api/prohibitorum/me/avatar` — `ClearAccountAvatar`; `204`.

Public serving:
- `GET /avatar/{subject}` — **root path, `AuthPublic`**, `registerOpHTTP`. `chi.URLParam(r, "subject")` → `GetAvatarBySubject`. Returns `404` for unknown subject, no avatar, **or disabled account**. On hit: `Content-Type: image/webp`, `ETag: "<etag>"`, `Cache-Control: public, max-age=86400`, and `304` when `If-None-Match` matches. No separate admin endpoint — admins use this same URL.

### 5. Downstream exposure

- **OIDC** (`pkg/protocol/oidc/claims.go::profileClaims`): when the `profile` scope is granted (already gated at the call sites) and the account has an avatar, set `picture = avatarURL(a)`. Omit the claim entirely when no avatar. Flows into both the ID token and `/userinfo`.
- **SAML** (`pkg/protocol/saml/attributes.go::resolveSource`): add a `source == "avatar_url"` case returning `[avatarURL(a)]` (or `nil` when unset). SPs opt in via their `attribute_map`. Document `avatar_url` as an available source in the dashboard's SAML attribute-map hint.

### 6. Contracts / views

- `contract.SessionView` gains `AvatarURL *string` (json `avatarUrl,omitempty`); `sessionView()` populates it via the helper.
- `contract.AccountView` gains `AvatarURL *string`; both projection functions in `handle_account.go` populate it (requires `avatar_etag` + `oidc_subject` available in those rows — see §1).

### 7. Frontend

- `dashboard/src/lib/api.ts`: add `upload(path, body: Blob)` — raw `PUT`, `credentials: 'include'`, **no** `Content-Type: application/json` (let the browser/omit set it); parse JSON response if any; throw `ApiError` on non-2xx (reuse existing error shape).
- `dashboard/src/stores/auth.ts`: `SessionView` gains `avatarUrl?: string`; no logic change (it flows from `/me`).
- `UserAvatar.vue`: add optional `src?: string | null`. When present render `<img :src>` (object-cover, rounded, the existing size classes) with `@error` falling back to initials; else the current initials → icon fallback. Keep `aria-hidden`.
- `NavUser.vue`: pass `:src="auth.me.avatarUrl"` to both `UserAvatar` instances (trigger + menu header).
- **`EditDisplayNameDialog.vue` → `EditProfileDialog.vue`**: add an avatar block above the display-name field — current avatar preview (`UserAvatar` at a larger size), a hidden `<input type="file" accept="image/png,image/jpeg,image/webp,image/gif">` behind an "Upload" button, and a "Remove" button (shown only when an avatar exists). Client guards: reject `> 5 MB` before upload with a friendly message; on `PUT`/`DELETE` success refresh the avatar (re-fetch `/me` or patch the store's `avatarUrl`); surface mapped server errors inline. The display-name save path is unchanged. Rename the dropdown item/dialog title to **"Edit profile"** (`accountMenu.editProfile`).
- Admin: `AdminAccountsView` (list) and `AdminAccountDetailView` render `UserAvatar :src="row.avatarUrl"`.
- i18n (`en.ts`): `accountMenu.editProfile`, profile-dialog avatar labels (Upload / Remove / hint), and `errors.avatar_too_large` / `errors.avatar_invalid_image`. (Grep-verify apostrophes after editing `en.ts`.)

## Data flow

```
upload (browser) ──PUT /me/avatar (raw bytes)──▶ avatar.Process ──▶ SetAccountAvatar (BYTEA + etag)
                                                                          │
GET /avatar/{subject}?v=etag ◀── <img> (dashboard) / OIDC picture / SAML avatar_url
   └─ GetAvatarBySubject → 200 image/webp + ETag + Cache-Control (304 on If-None-Match; 404 none/disabled)

/me  ─▶ SessionView.avatarUrl ─▶ dashboard sidebar + edit dialog
profile-scope OIDC ─▶ claims.picture = avatarURL(a)
SAML attribute_map source "avatar_url" ─▶ AttributeStatement
```

## Error handling

| Condition | Result |
|---|---|
| Upload `> 5 MB` | `400 avatar_too_large` (checked before decode) |
| Undecodable / unsupported / absurd dimensions | `400 avatar_invalid_image` |
| `GET /avatar/{subject}`: unknown subject, no avatar, or disabled account | `404` |
| `If-None-Match` matches stored etag | `304 Not Modified` |
| OIDC profile scope but no avatar | `picture` claim omitted |
| SAML `avatar_url` source but no avatar | attribute omitted |

## Testing

- **`pkg/avatar` unit:** valid png/jpeg/webp inputs → a decodable 512×512 WebP out + stable etag; oversized bytes → `ErrAvatarTooLarge`; garbage / absurd dimensions → `ErrAvatarInvalidImage`; non-square input is center-cropped (assert output is square).
- **Endpoints:** `PUT` stores + returns url; `DELETE` clears; `GET /avatar/{subject}` returns bytes + `ETag`, `304` on `If-None-Match`, `404` for none/unknown/disabled; `PUT`/`DELETE` require a session.
- **OIDC:** `profileClaims` includes `picture` with profile scope + avatar, omits it without the scope or without an avatar.
- **SAML:** `resolveSource("avatar_url")` returns the URL when set, `nil` when unset.
- **Frontend (vitest):** `UserAvatar` renders `<img>` with `src` and falls back to initials on error/empty; `EditProfileDialog` upload (calls the upload helper) + remove + error mapping; auth-store carries `avatarUrl`. Reka idioms.
- **Smoke (cmd/smoke):** upload an avatar → `GET /avatar/{subject}` 200 → fetch `/userinfo` (or decode the ID token) with `profile` scope and assert `picture` equals the public URL.

## Files

**Backend — new:** `db/migrations/002_avatar.sql`; `pkg/avatar/avatar.go` (+ `avatar_test.go`); `pkg/server/handle_avatar.go` (PUT/DELETE/public GET handlers).
**Backend — modified:** `db/queries/account.sql` (+ regenerated `pkg/db/*`); `pkg/contract/auth.go` (SessionView, AccountView); `pkg/server/handle_me.go` (sessionView), `handle_account.go` (account views); `pkg/server/server.go` (+ `operations.go` if needed) route registration; `pkg/protocol/oidc/claims.go`; `pkg/protocol/saml/attributes.go`; `go.mod`/`go.sum` (gen2brain/webp, wazero, x/image).
**Frontend — new:** `dashboard/src/components/custom/EditProfileDialog.vue` (replaces EditDisplayNameDialog) (+ test).
**Frontend — modified:** `dashboard/src/lib/api.ts`; `dashboard/src/stores/auth.ts`; `dashboard/src/components/custom/UserAvatar.vue` (+ test); `dashboard/src/components/custom/NavUser.vue`; admin accounts list/detail views; `dashboard/src/locales/en.ts`. Remove `EditDisplayNameDialog.vue` + its test.

## Implementation phasing (for the plan)

1. **Storage + pipeline + endpoints** — migration, sqlc, `pkg/avatar`, PUT/DELETE/public-GET, `SessionView.avatarUrl`.
2. **Dashboard** — api upload helper, `UserAvatar` src, `NavUser`, `EditProfileDialog`, admin views, i18n.
3. **Downstream** — OIDC `picture`, SAML `avatar_url`, `AccountView.avatarUrl`, smoke coverage.

## Done-gate

`go build` / `vet` / `test` (0), `vitest` (green), `vue-tsc -b` (0), smoke `SMOKE_EXIT=0` (incl. the avatar/`picture` round-trip), rebuild + commit `pkg/webui/dist`.
