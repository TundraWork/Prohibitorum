# UX improvements: avatar source choice + admin polish + a11y sweep

**Date:** 2026-06-14
**Status:** Approved (design) — pending implementation plan
**Scope:** Backend (Go) + dashboard SPA. Five UX improvements bundled into one phased spec:
(1) IdP slug visibility, (2) the disable toggle as its own section, (3) an upstream-scope
combobox, (4) user-selectable avatar source (inherited / uploaded / none), (5) a targeted
accessibility sweep.

## Problem / goal

A grab-bag of UX gaps surfaced after the federated-avatar feature shipped:
1. **IdP slug** — the identity-providers **list** buries the slug as a muted sub-line under the
   display name, and the **detail** page never shows it at all (it's only in the URL).
2. **Disable toggle** — `disabled` is mixed into the functional settings group; it should be a
   distinct, deliberate section.
3. **Upstream scopes** — configured via a free-text `TagInput`, with no guidance on valid values.
4. **Avatar source** — a federated user may have BOTH an inherited (upstream) avatar and an
   uploaded one, but they overwrite a single slot; the user can't choose which to show (or none).
5. **Cursor / a11y** — interactive elements (esp. buttons) show the default arrow cursor (Tailwind
   v4 Preflight removed `cursor: pointer` from buttons) and other a11y gaps exist.

## Decisions (locked during brainstorming)

- **Avatar (#4):** retain **two stored blobs** (one per source) + an **active-selection pointer** —
  NOT the re-fetch-on-demand approach. Both images persist; switching is instant; the user's
  choice is sticky.
- **Upstream scopes (#3):** a **combobox** — dropdown of common scopes *with descriptions* + free
  **custom** entry. The OIDC **application** scopes stay as the fixed checkbox set (custom is
  invalid there; the backend validates against `{openid, profile, email, offline_access}`).
- **a11y (#5):** a **targeted, checklist-driven sweep** (cursors + aria-labels + focus-visible +
  semantics), NOT a full WCAG 2.1 AA audit.
- **Packaging:** one phased spec (avatar → admin-UX → a11y). #4 is the only backend change.

## 1. Avatar dual-source

### 1.1 Data model — migration `013_avatar_dual_source.sql` (forward; `011`/`012` immutable)

Today (`011`/`012`): `account_avatar(account_id int PK FK, bytes bytea)` + on `account`:
`avatar_content_type`, `avatar_etag`, `avatar_source`. One blob per account; `avatar_source` =
provenance of that one blob.

Restructure to **one row per source, with per-row meta**:

```sql
-- +goose Up
ALTER TABLE account_avatar ADD COLUMN IF NOT EXISTS source       text;
ALTER TABLE account_avatar ADD COLUMN IF NOT EXISTS content_type text;
ALTER TABLE account_avatar ADD COLUMN IF NOT EXISTS etag         text;
-- Migrate each existing single blob into the row for its current provenance.
UPDATE account_avatar av SET
  source       = COALESCE((SELECT a.avatar_source       FROM account a WHERE a.id = av.account_id), 'user'),
  content_type = (SELECT a.avatar_content_type FROM account a WHERE a.id = av.account_id),
  etag         = (SELECT a.avatar_etag         FROM account a WHERE a.id = av.account_id)
WHERE source IS NULL;
ALTER TABLE account_avatar ALTER COLUMN source SET NOT NULL;
ALTER TABLE account_avatar DROP CONSTRAINT IF EXISTS account_avatar_pkey;
ALTER TABLE account_avatar ADD PRIMARY KEY (account_id, source);   -- up to 2 rows/account

-- +goose Down  (best-effort; lossy if an account has 2 rows — must dedup first)
DELETE FROM account_avatar a USING account_avatar b
  WHERE a.account_id = b.account_id AND a.source = 'upstream' AND b.source = 'user';
ALTER TABLE account_avatar DROP CONSTRAINT IF EXISTS account_avatar_pkey;
ALTER TABLE account_avatar ADD PRIMARY KEY (account_id);
ALTER TABLE account_avatar DROP COLUMN IF EXISTS etag;
ALTER TABLE account_avatar DROP COLUMN IF EXISTS content_type;
ALTER TABLE account_avatar DROP COLUMN IF EXISTS source;
```

**`account.avatar_source` is repurposed as the ACTIVE selection, a 4-state value:**
- `NULL` — never chosen (the inherit job MAY auto-activate `'upstream'`).
- `'none'` — explicitly no avatar (a deliberate choice; the inherit job must NOT override it).
- `'upstream'` — show the inherited blob.
- `'user'` — show the uploaded blob.

`account.avatar_etag` / `avatar_content_type` are a **denormalized cache of the active row's meta**
(kept in sync on every selection / upload / clear), so the existing public-URL builder
(`avatar.AccountURL` reads `a.AvatarEtag`) and `SessionView.avatarUrl` keep working unchanged.
When the active source is `'none'`/`NULL`, these are `NULL`.

> **The `'none'`-vs-`NULL` distinction is load-bearing:** it's the only way the inherit job can tell
> "user deliberately chose no avatar" (don't re-activate upstream) from "brand-new account, never
> chosen" (safe to auto-activate upstream on first inherit).

**Backfill outcome:** existing avatars get an `account_avatar` row keyed by their provenance, and
`account.avatar_source` already equals that provenance → active = their one source (no visible
change). Accounts with no avatar keep `avatar_source = NULL`.

### 1.2 Queries (`db/queries/account.sql` → regen `pkg/db`)

Replace the single-blob queries (`UpsertAccountAvatarBytes`, `SetAccountAvatarMetaUpstream`,
`SetAccountAvatarMetaUser`, `ClearAccountAvatarBytes`, `ClearAccountAvatarMeta`, `GetAvatarBySubject`)
with source-aware ones:

- `UpsertAvatarSource :exec` — `INSERT INTO account_avatar (account_id, source, bytes, content_type, etag)
  VALUES (...) ON CONFLICT (account_id, source) DO UPDATE SET bytes=EXCLUDED.bytes,
  content_type=EXCLUDED.content_type, etag=EXCLUDED.etag`.
- `SetActiveAvatar :exec` — `UPDATE account SET avatar_source=$2,
  avatar_etag=(SELECT etag FROM account_avatar WHERE account_id=$1 AND source=$2),
  avatar_content_type=(SELECT content_type FROM account_avatar WHERE account_id=$1 AND source=$2),
  updated_at=now() WHERE id=$1` (caller guarantees the row exists; `$2` ∈ upstream/user).
- `ClearActiveAvatar :exec` — `UPDATE account SET avatar_source=$2 /* 'none' or NULL */,
  avatar_etag=NULL, avatar_content_type=NULL, updated_at=now() WHERE id=$1`.
- `DeleteAvatarSource :exec` — `DELETE FROM account_avatar WHERE account_id=$1 AND source=$2`.
- `GetActiveAvatarBySubject :one` — serve the active source:
  `SELECT av.bytes, av.content_type, av.etag, a.disabled FROM account a
   JOIN account_avatar av ON av.account_id=a.id AND av.source=a.avatar_source
   WHERE a.oidc_subject=$1`. (When active is `none`/`NULL`, no `av` row matches → `ErrNoRows` → 404.)
- `GetAvatarSourceBySubject :one` — for `?source=` preview: same JOIN but `av.source=$2`.
- `ListAvatarSourcesByAccount :many` — `SELECT source, etag FROM account_avatar WHERE account_id=$1`.

### 1.3 URL helper (`pkg/avatar`)

`AccountURL(a, origin)` unchanged (active etag). Add `SourceURL(subject, source, etag, origin)` →
`<origin>/avatar/<subject>?source=<source>&v=<etag[:8]>` for per-source previews.

### 1.4 Handlers (`pkg/server/handle_avatar.go`)

- `PUT /api/prohibitorum/me/avatar` (authed; existing): `avatar.Process(raw)` → tx:
  `UpsertAvatarSource('user', ...)` + `SetActiveAvatar('user')` → refresh session meta. `204`.
- **NEW** `PUT /api/prohibitorum/me/avatar/selection` (authed): body `{source: "upstream"|"user"|"none"}`.
  For `upstream`/`user`: the row must exist (else `400 avatar_source_unavailable`) → `SetActiveAvatar`.
  For `none`: `ClearActiveAvatar('none')`. Refresh session. `204`.
- `DELETE /api/prohibitorum/me/avatar` (authed; existing, semantics refined): delete the **uploaded**
  row (`DeleteAvatarSource('user')`); if the active selection WAS `'user'`, fall back —
  `SetActiveAvatar('upstream')` if that row exists, else `ClearActiveAvatar('none')`. `204`.
- `GET /avatar/{subject}` (public; existing): if `?source=` present → `GetAvatarSourceBySubject`;
  else `GetActiveAvatarBySubject`. `404` for unknown subject / disabled / no active / missing source.
  Same `image/webp` + `ETag` + `Cache-Control: public, max-age=86400` + `304`.

Route registration for the new selection PUT in `server.go` (alongside the avatar routes).

### 1.5 Inherit job (`pkg/federation/oidc/federation.go` `runAvatarInherit`)

Rework — the old `avatar_source='user'` clobber-guard is REMOVED (both blobs now coexist):
1. Resolve picture (unchanged: id_token claim → UserInfo fallback) → fetch → `avatar.Process` → etag.
2. Read the current `'upstream'` row; if its etag == new etag, skip (no-op refresh).
3. `UpsertAvatarSource('upstream', bytes, content_type, etag)`.
4. Read `account.avatar_source`. **`SetActiveAvatar('upstream')` ONLY if `avatar_source IS NULL`**
   (never chosen). If it's `'none'`/`'user'`/`'upstream'` → leave the active selection untouched
   (a deliberate choice, or already upstream — in the already-upstream case the cache etag still
   needs refreshing, so re-run `SetActiveAvatar('upstream')` when active is already `'upstream'`).
   → so: activate-upstream when `avatar_source IS NULL OR avatar_source = 'upstream'`.
   `FederatorQueries` swaps the old avatar methods for the new ones + `GetAccountByID` (already present).

### 1.6 Contract / views (`pkg/contract/auth.go`)

`SessionView` (the `/me` body) gains:
- `AvatarSource *string` (`json:"avatarSource,omitempty"`) — the active selection (`upstream`/`user`/
  `none`); omitted when `NULL`.
- `AvatarSourceUrls map[string]string` (`json:"avatarSourceUrls,omitempty"`) — per existing source row,
  `source → SourceURL(...)` (so the picker can preview each without knowing the subject).

`avatarUrl` stays (the active source's URL, or absent when none). `AccountView` (admin) unchanged.
`sessionView()` populates the two new fields via `ListAvatarSourcesByAccount` + `avatar.SourceURL`.

### 1.7 Frontend (`dashboard/`)

- **`EditProfileDialog.vue`** — the avatar block becomes a **source picker**: a selectable option per
  available source — **Inherited** (`auth.me.avatarSourceUrls.upstream`, preview) · **Uploaded**
  (`auth.me.avatarSourceUrls.user`, preview) · always **None** — rendered as radio cards with the
  active one (`auth.me.avatarSource`) highlighted. Selecting → `api.put('/me/avatar/selection',{source})`
  → `auth.reload()`. **Upload** button keeps the existing crop flow (`AvatarCropper`) → `PUT /me/avatar`
  (writes `'user'` + activates). **Remove upload** (shown when the `user` source exists) → `DELETE /me/avatar`.
- **`stores/auth.ts`** `SessionView` interface gains `avatarSource?: string` + `avatarSourceUrls?: Record<string,string>`.
- `UserAvatar`/`NavUser` unchanged (active `avatarUrl`).
- i18n: `accountMenu.avatarSource*` (inherited / uploaded / none + a short hint).

## 2. IdP slug (#1)

- **List** (`AdminUpstreamIdpsView.vue`): split the stacked name/slug cell into a dedicated **Slug**
  column. Columns: **Name · Slug · Mode · State**. i18n `admin.upstream.colSlug`.
- **Detail** (`AdminUpstreamIdpDetailView.vue`): add a **read-only slug** display near the top (the
  slug is the immutable URL key — a labeled value or a `disabled` Input). i18n `admin.upstream.slug`
  (exists).

## 3. Disable as its own section (#2)

In the three detail views, lift the `disabled` Switch out of the mixed settings group into its own
**Status** `FormSection` (its own titled block), visually separated:
- `AdminUpstreamIdpDetailView.vue` (out of the `requireVerifiedEmail` group),
- `AdminOidcClientDetailView.vue` (out of the `requireConsent` group),
- `AdminSamlProviderDetailView.vue` (its `disabled` toggle).
i18n: a section title per view (e.g. `admin.*.statusSection` + a one-line description).

## 4. Upstream scopes combobox (#3)

New reusable **`components/custom/ComboboxTokenInput.vue`**: chips (`string[]`) + a dropdown of
**suggestions `{value, description}`** + free **custom** entry. Built on the Reka `Combobox`/`Popover`
+ `TagsInput` primitives (keyboard-accessible: type-to-filter, ↑/↓/Enter/Escape, focus-visible,
chip-remove with aria-label). Props: `modelValue: string[]`, `suggestions: {value,description}[]`,
`placeholder`, `ariaLabel`, `allowCustom` (default true). Used for the **upstream IdP** scopes in
`AdminUpstreamIdpsView` (create) + `AdminUpstreamIdpDetailView` (edit), replacing `TagInput`.
Suggestions = a `COMMON_UPSTREAM_SCOPES` const: `openid`, `profile`, `email`, `offline_access`,
`address`, `phone` (each with a one-line description). Custom values allowed (forwarded to the OP).
**OIDC application scopes unchanged** (checkboxes). i18n: scope descriptions under `admin.upstream.scopeDesc.*`.

## 5. Accessibility sweep (#5)

Checklist-driven, app-wide, fix-what's-found:
- **Cursor:** add `cursor-pointer` to the `Button` cva, and to every interactive primitive that
  lacks it — `Switch`, `Checkbox`, `Select` trigger, `Tabs` trigger, dropdown-menu items, clickable
  table rows (audit each list view), `RadioCardGroup`/`SegmentedControl` options, the new combobox,
  copy/icon buttons, `LocaleSwitcher`. Add `disabled:cursor-not-allowed` where an element renders
  disabled without `pointer-events-none`.
- **Labels:** `aria-label` on every icon-only button (chip-remove ×, copy buttons, sidebar toggle, etc.).
- **Focus:** verify a visible `focus-visible` ring on every interactive element.
- **Semantics:** correct button-vs-div usage; ensure every `Dialog` has a `DialogTitle`/`Description`
  and every `Alert` an appropriate `role`/`aria-live`.
- A cheap regression test asserting the `Button` cva string contains `cursor-pointer`.

## Error handling

| Condition | Result |
|---|---|
| `PUT /me/avatar/selection` to a source with no stored blob | `400 avatar_source_unavailable` |
| `PUT /me/avatar/selection {source:"none"}` | active → `'none'`, no avatar served |
| `DELETE /me/avatar` when active was `user` | delete user blob; active → `upstream` if present else `none` |
| `GET /avatar/{subject}` active=`none`/`NULL` / unknown / disabled / missing `?source` | `404` |
| inherit job, `avatar_source` is `'none'` or `'user'` | upstream blob updated, active **unchanged** |
| inherit job, `avatar_source` is `NULL` or `'upstream'` | upstream blob updated + active set `'upstream'` |
| upstream scope custom value | accepted, forwarded to the OP (no client-side rejection) |

## Testing

- **`pkg/avatar`:** `SourceURL` builder.
- **`pkg/server` (avatar handlers):** upload → `user` row + active=`user`; selection switch
  upstream/user/none (+ `avatar_source_unavailable`); delete-upload fallback (active user→upstream→none);
  `GET /avatar/{subject}` active + `?source=` preview + 404 cases; `/me` reports `avatarSource` +
  `avatarSourceUrls`.
- **`pkg/federation/oidc`:** inherit job activates upstream only when `avatar_source` is `NULL`/`upstream`,
  leaves `'none'`/`'user'` untouched; both rows coexist; etag-skip.
- **Frontend (vitest):** `EditProfileDialog` picker (renders available sources + active, switch calls
  selection PUT + reload, upload, remove-upload); `ComboboxTokenInput` (pick suggestion, add custom,
  remove chip, keyboard); IdP list slug column + detail slug display; disable-section placement +
  unchanged save payload; a Button-cursor assertion.
- **Smoke (`cmd/smoke`):** update the avatar / avatar-fed steps to the dual-source model — upload
  (user) + federated inherit (upstream) coexist; switch active to upstream/user/none and assert
  `GET /avatar/{subject}` serves the selected one (+ `?source=` previews); the old "no-clobber"
  assertion is replaced by "both rows persist, active = the user's last choice".

## Files

**Backend:** migration `013`; `db/queries/account.sql` (+ regen `pkg/db`); `pkg/avatar/avatar.go`
(`SourceURL`); `pkg/server/handle_avatar.go` (selection handler, `?source`, delete-fallback) +
`server.go` (route); `pkg/federation/oidc/federation.go` (inherit-job rework + `FederatorQueries`);
`pkg/contract/auth.go` (`SessionView` fields) + `handle_me.go` (`sessionView`); `cmd/smoke/main.go`.
**Frontend:** `components/custom/EditProfileDialog.vue` (+ test), `stores/auth.ts`,
`components/custom/ComboboxTokenInput.vue` (+ test), `pages/admin/AdminUpstreamIdpsView.vue` +
`AdminUpstreamIdpDetailView.vue` (slug, disable section, scopes combobox),
`AdminOidcClientDetailView.vue` + `AdminSamlProviderDetailView.vue` (disable section),
`components/ui/button/index.ts` (+ other ui primitives for cursor/aria), `locales/en.ts`.

## Implementation phasing (for the plan)

1. **Avatar backend** — migration `013` + queries + regen; `SourceURL`; handlers (upload/selection/
   delete/GET); inherit-job rework; `SessionView` fields + `sessionView`.
2. **Avatar frontend** — `EditProfileDialog` picker + `stores/auth` fields + i18n.
3. **Admin UX** — IdP slug (list column + detail) ; disable-as-section (3 detail views) ;
   `ComboboxTokenInput` + wire upstream scopes (2 views).
4. **a11y sweep** — Button cva + interactive primitives + aria-labels + focus/semantics.
5. **Smoke + done-gate** — update avatar/avatar-fed smoke to dual-source; full gate; rebuild dist.

## Done-gate

`CGO_ENABLED=0 go build -tags nodynamic ./...` / `go vet` / `go test ./...` (0), `vitest` (green),
`vue-tsc -b` (0), smoke `SMOKE_EXIT=0` (incl. the avatar source-switch round-trip), rebuild + commit
`pkg/webui/dist`.
