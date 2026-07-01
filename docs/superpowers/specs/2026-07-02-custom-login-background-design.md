# Custom Login Page Background Image — Design

**Date:** 2026-07-02
**Status:** Approved (design), pending implementation plan

## Summary

Let an admin upload a custom background image for the unauthenticated threshold
pages (login, consent, logout, error, enroll, pair, welcome, maintenance). Unlike
the instance icon and avatars — which are re-encoded to a 512×512 WebP — the
background is **stored and served verbatim**: the exact uploaded bytes reach the
browser, with no crop, resize, or re-encode. The only server-side work is
*validation* (is it a real, web-renderable image within size/dimension limits),
which never mutates the bytes.

The feature mirrors the existing instance-icon branding path (`instance_settings`
singleton + `pkg/branding` resolver + admin PUT/DELETE + public GET), so it slots
cleanly into established patterns.

## Decisions (from brainstorming)

- **Legibility overlay:** keep the existing `AuthBackdrop` contrast scrim layered
  over the custom image. The image bytes are served untouched, but the readability
  overlay stays so the login card and corner chrome never become unreadable over a
  busy or bright image.
- **Scope:** apply to **all** threshold pages. They share one `AuthBackdrop`, so
  this is a single component change and a consistent branded look.
- **Size cap:** **5 MiB**, same as icons. The raw file is served to every
  unauthenticated visitor, so this bounds login-page load time and DB storage.
- **No postprocessing:** validate-but-don't-transform. Store and serve the raw
  bytes exactly as uploaded.

## Precedence

A clean 3-tier fallback, introducing **no new config plumbing** (the background is
DB-only — there is deliberately no config-file default, unlike the icon):

1. **DB background** (admin upload) — top precedence.
2. **Build-time asset** — the existing `AuthBackdrop` mechanism: a real image
   dropped at `dashboard/src/assets/auth-scene.{png,jpg,jpeg,webp,avif}`.
3. **CSS gradient placeholder** — the existing painterly "Drenched" default.

The contrast scrim sits on top of whichever tier is active.

## Validation vs. processing

`imageutil.ValidateRaw(raw) (etag string, err error)` — new helper that only
*sniffs*, never transforms:

- Reject if `len(raw) > imageutil.MaxInputBytes` (5 MiB) → `ErrTooLarge`.
- `image.DecodeConfig(bytes.NewReader(raw))` to read format + dimensions without a
  full decode. On error → `ErrInvalidImage`.
- Reject if width/height ≤ 0 or > `imageutil.MaxInputDim` (10000) → `ErrInvalidImage`.
- Reject if the sniffed format is not in the allowlist `{png, jpeg, webp}` →
  `ErrInvalidImage`. (Enforced so we never serve, e.g., BMP/TIFF/GIF as an image
  type a browser may not render, and so the raw bytes are always web-safe.)
- Return the sha256 hex digest computed over the **unmodified** raw bytes as the
  etag.

Rationale: validation rejects non-images and non-web formats (so we never serve
arbitrary admin-uploaded bytes under an `image/*` content type) without altering a
single byte. This preserves the "no postprocess" requirement while keeping the
served content trustworthy.

## Backend

### Migration — `db/migrations/026_login_background.sql`

```sql
ALTER TABLE instance_settings
  ADD COLUMN IF NOT EXISTS login_bg      bytea NULL,
  ADD COLUMN IF NOT EXISTS login_bg_etag text  NULL;
```

(Highest existing migration is `025_maintenance_mode.sql`; `026` is next. Uses
`IF NOT EXISTS` for idempotency, matching the migration house style.)

### `pkg/imageutil/imageutil.go`

Add `ValidateRaw(raw []byte) (etag string, err error)` as described above. Reuses
the existing `MaxInputBytes`, `MaxInputDim`, `ErrTooLarge`, `ErrInvalidImage`. Does
**not** call `DecodeCropScale` or `EncodeWebP`.

### `pkg/branding/branding.go`

- `Settings` struct: add `LoginBG []byte` and `LoginBGEtag *string`.
- `Store` interface: add `SetLoginBG(ctx, raw []byte, etag string) error` and
  `ClearLoginBG(ctx) error`.
- `Resolver`:
  - `Background(ctx) (bytes []byte, etag string, custom bool)` — returns the DB
    background if present (`custom=true`), else `(nil, "", false)`. **DB-only**;
    no config-file tier.
  - `HasCustomBackground(ctx) bool`.
  - `SetLoginBackground(ctx, raw []byte) error` — calls `imageutil.ValidateRaw`,
    stores the **raw bytes** (not a processed output) + etag via
    `st.SetLoginBG`, then `Invalidate()`.
  - `ClearLoginBackground(ctx) error` — `st.ClearLoginBG` + `Invalidate()`.

### `pkg/branding/store_pg.go`

- Extend the `Get` SELECT to include `login_bg, login_bg_etag`; scan into the new
  `Settings` fields.
- `SetLoginBG`: `UPDATE instance_settings SET login_bg = $1, login_bg_etag = $2, updated_at = now() WHERE id = 1`.
- `ClearLoginBG`: `UPDATE instance_settings SET login_bg = NULL, login_bg_etag = NULL, updated_at = now() WHERE id = 1`.

Test fakes implementing `Store` (in the server package tests) must gain the two new
methods.

### `pkg/contract/auth.go`

Extend `PublicConfig`:

```go
HasCustomBackground bool   `json:"hasCustomBackground"`
BackgroundURL       string `json:"backgroundUrl"`
BackgroundEtag      string `json:"backgroundEtag"`
```

### `pkg/server/handle_branding.go`

- `handleGetPublicConfigHTTP`: populate `HasCustomBackground`,
  `BackgroundURL: "/branding/background"`, `BackgroundEtag` from
  `s.branding.Background(ctx)`.
- New `handleGetBrandingBackgroundHTTP(w, r)`:
  ```go
  bg, etag, _ := s.branding.Background(r.Context())
  if len(bg) == 0 { http.NotFound(w, r); return }
  writeIconResponse(w, r, bg, etag)
  ```
  `writeIconResponse` + `imageContentType` already handle ETag/304, the 5-minute
  public cache, and png/jpeg/webp content-type sniffing — no changes needed there.

### `pkg/server/handle_admin_settings.go`

Mirror the icon handlers:

- `PUT /api/prohibitorum/admin/settings/background` — `handlePutInstanceBackgroundHTTP`:
  `registerOpHTTP(admin)` + in-handler `requireFreshSudo` (the sudo wrapper rejects
  non-JSON bodies and caps at 64 KiB, so large raw uploads use the in-handler gate,
  exactly like the icon upload). `io.ReadAll(io.LimitReader(r.Body, maxIconRead))`
  (the existing `5<<20 + 1` const, reused). Call `s.branding.SetLoginBackground`;
  map `ErrTooLarge` → `writeAvatarErr(w, "avatar_too_large", "background: image exceeds 5 MiB")`
  and other errors → `writeAvatarErr(w, "avatar_invalid_image", "background: invalid or unsupported image format")`.
  Audit `instance_login_background_updated`. `204 No Content`.
- `DELETE /api/prohibitorum/admin/settings/background` — `handleDeleteInstanceBackgroundHTTP`:
  `registerSudoOpHTTP` (admin + fresh sudo via wrapper; no body). Call
  `s.branding.ClearLoginBackground`. Audit `instance_login_background_removed`.
  `204 No Content`.

### `pkg/server/server.go`

Register routes alongside the existing branding routes:

- Public: `registerOpHTTP(s.router, "GET", "/branding/background", publicReq, s.handleGetBrandingBackgroundHTTP)`.
- Admin: `registerOpHTTP(s.router, "PUT", "/api/prohibitorum/admin/settings/background", admin, s.handlePutInstanceBackgroundHTTP)`
  and `s.registerSudoOpHTTP(s.router, "DELETE", "/api/prohibitorum/admin/settings/background", admin, s.handleDeleteInstanceBackgroundHTTP)`.

## Frontend

### `dashboard/src/stores/branding.ts`

- Extend the `PublicConfig` interface + reactive state with `hasCustomBackground`
  and `backgroundEtag`.
- Add `backgroundSrc` computed, mirroring `iconSrc`:
  ```ts
  const backgroundSrc = computed(() => {
    const v = backgroundEtag.value ? backgroundEtag.value.slice(0, 8) : ''
    return v ? `/branding/background?v=${v}` : '/branding/background'
  })
  ```
- Wire both into `load()` and the returned object.

### `dashboard/src/components/custom/AuthBackdrop.vue`

- Import the branding store.
- Scene source precedence: if `branding.hasCustomBackground`, use
  `branding.backgroundSrc`; else fall back to the existing build-time `sceneUrl`
  asset; else the CSS placeholder class (unchanged).
- The `--placeholder` class applies only when neither a DB background nor a
  build-time asset is present.
- Scrim unchanged (always on).
- Note: `AuthBackdrop` currently has no store dependency and is `aria-hidden`.
  Adding the store is safe — `ensureLoaded()` runs at boot (App.vue). Before the
  config resolves, `hasCustomBackground` is `false`, so the backdrop shows the
  asset/placeholder and swaps to the custom image once loaded — the same
  first-paint behavior the icon (`hasCustomIcon`) already has.

### `dashboard/src/pages/admin/SettingsView.vue`

Add a "Login background" card mirroring the existing icon card:

- Thumbnail preview of `branding.backgroundSrc` (shown only when
  `branding.hasCustomBackground`).
- Hidden file input `accept="image/png,image/jpeg,image/webp"` + an Upload button
  that clicks it; `onPickFile` → `withSudo(() => api.upload('/api/prohibitorum/admin/settings/background', file))`,
  then `branding.load()`; inline error on failure via the existing `useApi` pattern.
- Remove button (shown only when `branding.hasCustomBackground`) →
  `withSudo(() => api.del('/api/prohibitorum/admin/settings/background'))`, then
  `branding.load()`.
- New i18n keys in `dashboard/src/i18n/en.ts` and `zh.ts` (label, description,
  upload/remove/preview alt, upload error), mirroring the `admin.settings.*` icon
  keys.

## Testing & gate

- **Go unit — `imageutil.ValidateRaw`:** valid png/jpeg/webp pass and return a
  stable etag; the returned-nothing path leaves bytes unmodified (validate does not
  transform — assert by re-serving); oversize → `ErrTooLarge`; truncated/non-image
  and a disallowed format (e.g. GIF/BMP) → `ErrInvalidImage`.
- **Go handler tests** (mirror `handle_admin_settings_test.go` +
  `handle_branding_test.go`): PUT stores + GET serves the **exact** uploaded bytes
  (byte-for-byte equality — the core "no postprocess" guarantee); DELETE clears +
  GET 404s; the public config reflects `hasCustomBackground`/`backgroundEtag`;
  ETag/304 on the serve endpoint; sudo gate enforced on PUT and DELETE.
- **Frontend vitest:** the store's `backgroundSrc` computed (with/without etag);
  the SettingsView "Login background" section renders and calls the right endpoints.
- **Live smoke:** upload → `GET /branding/background` returns identical bytes →
  remove → 404. Extend the existing smoke script.
- **Full gate (done-gate):** `go build -tags nodynamic ./...`, `go vet`,
  `go test ./...`, `vitest`, `vue-tsc`, live smoke `SMOKE_EXIT=0`; rebuild and
  commit `dashboard/dist`.

## Out of scope / non-goals

- No per-theme (light/dark) background — one image regardless of theme.
- No config-file default background (the build-time asset already fills that role).
- No cropping/positioning UI — CSS `background-size: cover; background-position: center`
  (the existing `AuthBackdrop` behavior) governs fit; the bytes are untouched.
- No client-side re-encode/compression before upload.
