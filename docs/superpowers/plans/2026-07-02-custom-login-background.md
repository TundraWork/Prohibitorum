# Custom Login Page Background Image — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an admin upload a custom background image for the unauthenticated threshold pages; store and serve the uploaded bytes verbatim (validated, never re-encoded).

**Architecture:** Mirror the existing instance-icon branding path (`instance_settings` singleton → `pkg/branding` resolver → admin PUT/DELETE + public GET). The one departure: instead of `imageutil.ProcessSquareWebP` (crop + WebP re-encode), a new `imageutil.ValidateRaw` only *sniffs* the image (size, format allowlist, dimensions) and returns a sha256 etag over the unmodified bytes; those exact bytes are stored and served. Frontend: the branding store exposes `backgroundSrc`, `AuthBackdrop` uses it as the top precedence tier (DB → build-time asset → CSS gradient), and the admin SettingsView gets an upload/remove card.

**Tech Stack:** Go (chi, pgx), `pkg/imageutil` (gen2brain/webp WASM, build `-tags nodynamic`), Postgres migrations, Vue 3 + Pinia + Tailwind v4 + shadcn-vue, vitest, vue-i18n (en + zh), the `cmd/smoke` end-to-end driver.

**User decisions (already made):**
- Keep the `AuthBackdrop` contrast scrim over the custom image (legibility preserved).
- Apply the background to **all** threshold pages (they share one `AuthBackdrop`).
- Size cap **5 MiB** (same as icons).
- **No postprocessing** — store and serve the raw uploaded bytes byte-for-byte.
- Format allowlist `{png, jpeg, webp}`; DB-only (no config-file default; the build-time `auth-scene.*` asset is the mid-tier fallback).

**Spec:** `docs/superpowers/specs/2026-07-02-custom-login-background-design.md`

---

## File Structure

**Backend (Go)**
- `pkg/imageutil/imageutil.go` — add `ValidateRaw` (validate-not-transform). Test: `pkg/imageutil/validate_test.go` (new).
- `db/migrations/026_login_background.sql` — new columns on `instance_settings`.
- `pkg/branding/branding.go` — `Settings` fields, `Store` methods, resolver `Background`/`HasCustomBackground`/`SetLoginBackground`/`ClearLoginBackground`.
- `pkg/branding/store_pg.go` — extend `Get`, add `SetLoginBG`/`ClearLoginBG`.
- `pkg/branding/branding_test.go` — extend `fakeStore`, add resolver round-trip test.
- `pkg/contract/auth.go` — `PublicConfig` fields.
- `pkg/server/handle_branding.go` — config payload + `handleGetBrandingBackgroundHTTP`.
- `pkg/server/handle_admin_settings.go` — put/delete background handlers.
- `pkg/server/server.go` — 3 route registrations.
- `pkg/server/handle_branding_test.go` + `handle_admin_settings_test.go` — extend fakes, add tests.

**Frontend (Vue)**
- `dashboard/src/stores/branding.ts` (+ `branding.test.ts`).
- `dashboard/src/components/custom/AuthBackdrop.vue`.
- `dashboard/src/pages/admin/SettingsView.vue` (+ `SettingsView.test.ts`).
- `dashboard/src/locales/en.ts` + `zh.ts`.

**End-to-end**
- `cmd/smoke/main.go` — a new "login background" step block.

---

### Task 1: `imageutil.ValidateRaw` — validate-not-transform helper

**Goal:** A pure function that accepts raw image bytes, verifies they are a web-renderable image within limits WITHOUT decoding/re-encoding, and returns a sha256 etag over the unmodified bytes.

**Files:**
- Modify: `pkg/imageutil/imageutil.go` (append after `ProcessSquareWebP`, around line 88)
- Create: `pkg/imageutil/validate_test.go`

**Acceptance Criteria:**
- [ ] `ValidateRaw` accepts PNG, JPEG, and WebP and returns a 64-hex-char etag equal to `sha256(raw)`.
- [ ] Rejects input `> MaxInputBytes` with `ErrTooLarge`.
- [ ] Rejects non-images and disallowed formats (e.g. GIF) with `ErrInvalidImage`.
- [ ] Rejects images whose declared dimensions exceed `MaxInputDim` with `ErrInvalidImage`.
- [ ] Does not modify the input bytes (etag is over the exact input).

**Verify:** `go test -tags nodynamic ./pkg/imageutil/ -run ValidateRaw -v` → PASS

**Steps:**

- [ ] **Step 1: Write the failing tests** — create `pkg/imageutil/validate_test.go`:

```go
package imageutil

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/gen2brain/webp"
)

func solidImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 120, B: 200, A: 255})
		}
	}
	return img
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, solidImage(w, h)); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, solidImage(w, h), nil); err != nil {
		t.Fatalf("jpeg encode: %v", err)
	}
	return buf.Bytes()
}

func webpBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := webp.Encode(&buf, solidImage(w, h), webp.Options{Quality: 80}); err != nil {
		t.Fatalf("webp encode: %v", err)
	}
	return buf.Bytes()
}

func gifBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := gif.Encode(&buf, solidImage(w, h), nil); err != nil {
		t.Fatalf("gif encode: %v", err)
	}
	return buf.Bytes()
}

func TestValidateRaw_AcceptsWebFormats(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  []byte
	}{
		{"png", pngBytes(t, 64, 48)},
		{"jpeg", jpegBytes(t, 64, 48)},
		{"webp", webpBytes(t, 64, 48)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			etag, err := ValidateRaw(tc.raw)
			if err != nil {
				t.Fatalf("ValidateRaw(%s) err = %v, want nil", tc.name, err)
			}
			sum := sha256.Sum256(tc.raw)
			if want := hex.EncodeToString(sum[:]); etag != want {
				t.Errorf("etag = %q, want sha256 over raw %q", etag, want)
			}
		})
	}
}

func TestValidateRaw_RejectsTooLarge(t *testing.T) {
	raw := make([]byte, MaxInputBytes+1)
	if _, err := ValidateRaw(raw); err != ErrTooLarge {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestValidateRaw_RejectsNonImage(t *testing.T) {
	if _, err := ValidateRaw([]byte("this is definitely not an image")); err != ErrInvalidImage {
		t.Fatalf("err = %v, want ErrInvalidImage", err)
	}
}

func TestValidateRaw_RejectsDisallowedFormat(t *testing.T) {
	if _, err := ValidateRaw(gifBytes(t, 32, 32)); err != ErrInvalidImage {
		t.Fatalf("gif err = %v, want ErrInvalidImage", err)
	}
}

func TestValidateRaw_RejectsOversizeDimensions(t *testing.T) {
	// 10001×1 solid PNG: a valid, tiny-byte image whose width exceeds MaxInputDim.
	raw := pngBytes(t, MaxInputDim+1, 1)
	if len(raw) > MaxInputBytes {
		t.Skipf("fixture unexpectedly large (%d bytes)", len(raw))
	}
	if _, err := ValidateRaw(raw); err != ErrInvalidImage {
		t.Fatalf("err = %v, want ErrInvalidImage", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -tags nodynamic ./pkg/imageutil/ -run ValidateRaw -v`
Expected: FAIL — `undefined: ValidateRaw`.

- [ ] **Step 3: Add `ValidateRaw`** to `pkg/imageutil/imageutil.go` (after `ProcessSquareWebP`, before `cropSquare`). No new imports are needed (`bytes`, `crypto/sha256`, `encoding/hex`, `image` are already imported):

```go
// ValidateRaw verifies raw is a web-renderable image within the size and
// dimension limits WITHOUT decoding pixels or re-encoding. It is for assets that
// must be served back byte-for-byte (the login-page background): the bytes are
// never transformed, only sniffed via image.DecodeConfig. It returns a sha256-hex
// etag over the unmodified bytes.
//
// Rejects: oversize input (ErrTooLarge); anything DecodeConfig can't parse, a
// format outside {png, jpeg, webp}, or out-of-range dimensions (ErrInvalidImage).
func ValidateRaw(raw []byte) (etag string, err error) {
	if len(raw) > MaxInputBytes {
		return "", ErrTooLarge
	}
	cfg, format, derr := image.DecodeConfig(bytes.NewReader(raw))
	if derr != nil {
		return "", ErrInvalidImage
	}
	switch format {
	case "png", "jpeg", "webp":
	default:
		return "", ErrInvalidImage
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxInputDim || cfg.Height > MaxInputDim {
		return "", ErrInvalidImage
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -tags nodynamic ./pkg/imageutil/ -run ValidateRaw -v`
Expected: PASS (all five tests).

- [ ] **Step 5: Commit**

```bash
git add pkg/imageutil/imageutil.go pkg/imageutil/validate_test.go
git commit -m "feat(imageutil): ValidateRaw — sniff image without re-encoding (for verbatim serve)"
```

---

### Task 2: Migration + branding persistence & resolver

**Goal:** Persist the login background in `instance_settings` and expose resolver methods that validate-and-store the raw bytes and read them back.

**Files:**
- Create: `db/migrations/026_login_background.sql`
- Modify: `pkg/branding/branding.go` (`Settings` ~line 26, `Store` ~line 35, add resolver methods after `ClearIcon` ~line 197)
- Modify: `pkg/branding/store_pg.go` (`Get` lines 15-31; add two methods)
- Modify: `pkg/branding/branding_test.go` (`fakeStore` lines 11-31; add test)

**Acceptance Criteria:**
- [ ] `SetLoginBackground(ctx, raw)` validates via `imageutil.ValidateRaw`, stores the exact bytes + etag, and invalidates the cache.
- [ ] `Background(ctx)` returns `(bytes, etag, true)` when set and `(nil, "", false)` when not.
- [ ] `ClearLoginBackground(ctx)` clears the override and invalidates the cache.
- [ ] Invalid uploads propagate `ErrTooLarge` / `ErrInvalidImage` and never touch the store.

**Verify:** `go test -tags nodynamic ./pkg/branding/ -v` → PASS

**Steps:**

- [ ] **Step 1: Create the migration** `db/migrations/026_login_background.sql`:

```sql
-- 026_login_background.sql — custom login-page background image.
-- Stored and served VERBATIM (no re-encode). NULL = no override; the SPA then
-- falls back to its build-time asset / CSS gradient.
ALTER TABLE instance_settings
  ADD COLUMN IF NOT EXISTS login_bg      bytea NULL,
  ADD COLUMN IF NOT EXISTS login_bg_etag text  NULL;
```

- [ ] **Step 2: Write the failing resolver test** — add to `pkg/branding/branding_test.go`. First extend `fakeStore` (lines 11-31) to carry and return the new columns and implement the two new interface methods:

Replace the `fakeStore` struct + its `Get` + add the two methods so the block reads:

```go
type fakeStore struct {
	name        *string
	iconPNG     []byte
	iconEtag    *string
	maint       bool
	maintMsg    *string
	loginBG     []byte
	loginBGEtag *string
}

func (f *fakeStore) Get(context.Context) (Settings, error) {
	return Settings{
		Name: f.name, IconPNG: f.iconPNG, IconEtag: f.iconEtag,
		Maintenance: f.maint, MaintenanceMessage: f.maintMsg,
		LoginBG: f.loginBG, LoginBGEtag: f.loginBGEtag,
	}, nil
}
func (f *fakeStore) SetName(_ context.Context, n *string) error { f.name = n; return nil }
func (f *fakeStore) SetIcon(_ context.Context, png []byte, etag string) error {
	f.iconPNG, f.iconEtag = png, &etag
	return nil
}
func (f *fakeStore) ClearIcon(context.Context) error { f.iconPNG, f.iconEtag = nil, nil; return nil }
func (f *fakeStore) SetMaintenance(_ context.Context, on bool, msg *string) error {
	f.maint, f.maintMsg = on, msg
	return nil
}
func (f *fakeStore) SetLoginBG(_ context.Context, raw []byte, etag string) error {
	e := etag
	f.loginBG, f.loginBGEtag = raw, &e
	return nil
}
func (f *fakeStore) ClearLoginBG(context.Context) error { f.loginBG, f.loginBGEtag = nil, nil; return nil }
```

Then add the test function (uses the already-imported `bytes`, `image`, `image/png`):

```go
func TestLoginBackground_RoundTrip(t *testing.T) {
	fs := &fakeStore{}
	r := NewWithStore("X", fs)

	// No override initially.
	if data, etag, custom := r.Background(context.Background()); custom || data != nil || etag != "" {
		t.Fatalf("Background() = (%v,%q,%v), want (nil,\"\",false)", data, etag, custom)
	}

	// A real tiny PNG.
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatalf("encode: %v", err)
	}
	raw := buf.Bytes()

	if err := r.SetLoginBackground(context.Background(), raw); err != nil {
		t.Fatalf("SetLoginBackground: %v", err)
	}
	data, etag, custom := r.Background(context.Background())
	if !custom || etag == "" {
		t.Fatalf("after set: custom=%v etag=%q, want true + non-empty", custom, etag)
	}
	if !bytes.Equal(data, raw) {
		t.Fatalf("stored bytes differ from input — background must be verbatim")
	}

	// Invalid upload must not touch the store.
	if err := r.SetLoginBackground(context.Background(), []byte("not an image")); err == nil {
		t.Fatal("SetLoginBackground(bad) = nil err, want ErrInvalidImage")
	}

	if err := r.ClearLoginBackground(context.Background()); err != nil {
		t.Fatalf("ClearLoginBackground: %v", err)
	}
	if _, _, custom := r.Background(context.Background()); custom {
		t.Fatal("after clear: custom=true, want false")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test -tags nodynamic ./pkg/branding/ -run LoginBackground -v`
Expected: FAIL — `r.Background`/`SetLoginBackground`/`ClearLoginBackground` undefined; `Settings` has no `LoginBG`.

- [ ] **Step 4: Extend `Settings` and `Store`** in `pkg/branding/branding.go`.

In the `Settings` struct (after `MaintenanceMessage *string`, ~line 31) add:

```go
	LoginBG     []byte
	LoginBGEtag *string
```

In the `Store` interface (after `SetMaintenance(...)`, ~line 40) add:

```go
	SetLoginBG(ctx context.Context, raw []byte, etag string) error
	ClearLoginBG(ctx context.Context) error
```

- [ ] **Step 5: Add the resolver methods** to `pkg/branding/branding.go` (after `ClearIcon`, ~line 197, before `ProcessIcon`):

```go
// Background returns the DB login-page background bytes + etag, and whether one
// is set (custom=true). Unlike Icon there is no config-file or built-in default:
// with no DB override this returns (nil, "", false) and the frontend falls back
// to its build-time asset / gradient. Bytes are served verbatim — never processed.
func (r *Resolver) Background(ctx context.Context) (data []byte, etag string, custom bool) {
	s := r.load(ctx)
	if len(s.LoginBG) == 0 {
		return nil, "", false
	}
	if s.LoginBGEtag != nil {
		etag = *s.LoginBGEtag
	}
	return s.LoginBG, etag, true
}

// HasCustomBackground reports whether a DB login-page background is set.
func (r *Resolver) HasCustomBackground(ctx context.Context) bool {
	_, _, custom := r.Background(ctx)
	return custom
}

// SetLoginBackground validates raw (size, format, dimensions) WITHOUT modifying
// it and stores the exact bytes as the DB override, then invalidates the cache.
// The stored bytes are what the public serve endpoint returns byte-for-byte.
func (r *Resolver) SetLoginBackground(ctx context.Context, raw []byte) error {
	etag, err := imageutil.ValidateRaw(raw)
	if err != nil {
		return err
	}
	if err := r.st.SetLoginBG(ctx, raw, etag); err != nil {
		return err
	}
	r.Invalidate()
	return nil
}

// ClearLoginBackground removes the DB login-page background override.
func (r *Resolver) ClearLoginBackground(ctx context.Context) error {
	if err := r.st.ClearLoginBG(ctx); err != nil {
		return err
	}
	r.Invalidate()
	return nil
}
```

- [ ] **Step 6: Extend `PGStore`** in `pkg/branding/store_pg.go`.

Replace the `Get` method (lines 15-31) with:

```go
func (s *PGStore) Get(ctx context.Context) (Settings, error) {
	var out Settings
	row := s.pool.QueryRow(ctx,
		`SELECT instance_name, icon_png, icon_etag, maintenance_mode, maintenance_message, login_bg, login_bg_etag
		   FROM instance_settings WHERE id = 1`)
	var name *string
	var icon []byte
	var etag *string
	var maintenance bool
	var message *string
	var loginBG []byte
	var loginBGEtag *string
	if err := row.Scan(&name, &icon, &etag, &maintenance, &message, &loginBG, &loginBGEtag); err != nil {
		return Settings{}, err
	}
	out.Name, out.IconPNG, out.IconEtag = name, icon, etag
	out.Maintenance, out.MaintenanceMessage = maintenance, message
	out.LoginBG, out.LoginBGEtag = loginBG, loginBGEtag
	return out, nil
}
```

Add at the end of the file:

```go
func (s *PGStore) SetLoginBG(ctx context.Context, raw []byte, etag string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE instance_settings SET login_bg = $1, login_bg_etag = $2, updated_at = now() WHERE id = 1`, raw, etag)
	return err
}

func (s *PGStore) ClearLoginBG(ctx context.Context) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE instance_settings SET login_bg = NULL, login_bg_etag = NULL, updated_at = now() WHERE id = 1`)
	return err
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test -tags nodynamic ./pkg/branding/ -v`
Expected: PASS (existing tests + `TestLoginBackground_RoundTrip`).

- [ ] **Step 8: Commit**

```bash
git add db/migrations/026_login_background.sql pkg/branding/branding.go pkg/branding/store_pg.go pkg/branding/branding_test.go
git commit -m "feat(branding): persist + resolve verbatim login-page background"
```

---

### Task 3: Backend HTTP surface — contract, public config/serve, admin handlers, routes

**Goal:** Expose the background over HTTP: public config flags + verbatim serve, and admin upload/remove behind the fresh-sudo gate; prove byte-for-byte round-trip.

**Files:**
- Modify: `pkg/contract/auth.go` (`PublicConfig`, lines 143-153)
- Modify: `pkg/server/handle_branding.go` (config handler lines 13-28; add serve handler)
- Modify: `pkg/server/handle_admin_settings.go` (add two handlers after `handleDeleteInstanceIconHTTP`, ~line 113)
- Modify: `pkg/server/server.go` (after line 426; after line 500)
- Modify: `pkg/server/handle_branding_test.go` (fake lines 13-27; config + serve tests)
- Modify: `pkg/server/handle_admin_settings_test.go` (fake lines 27-58; add background round-trip + delete tests)

**Acceptance Criteria:**
- [ ] `GET /api/prohibitorum/config` includes `hasCustomBackground`, `backgroundUrl:"/branding/background"`, `backgroundEtag`.
- [ ] `GET /branding/background` returns the exact uploaded bytes with an ETag (and 304 on `If-None-Match`), or 404 when none is set.
- [ ] `PUT /api/prohibitorum/admin/settings/background` stores the raw bytes; `DELETE` clears them; both require fresh sudo.
- [ ] Byte-for-byte: bytes served by GET equal bytes sent to PUT.

**Verify:** `go test -tags nodynamic ./pkg/server/ -run 'Branding|AdminSettings' -v` → PASS

**Steps:**

- [ ] **Step 1: Extend the contract** — in `pkg/contract/auth.go`, inside `PublicConfig` (after `MaintenanceMessage string`, line 152) add:

```go
	// Login-page background: served verbatim (no re-encode) from BackgroundURL when
	// an admin uploaded one. HasCustomBackground=false → the SPA uses its build-time
	// asset / gradient fallback.
	HasCustomBackground bool   `json:"hasCustomBackground"`
	BackgroundURL       string `json:"backgroundUrl"`
	BackgroundEtag      string `json:"backgroundEtag"`
```

- [ ] **Step 2: Write failing server tests.**

In `pkg/server/handle_branding_test.go`, extend `fakeBrandingStore` (lines 13-27) to carry/return the columns and satisfy the interface:

```go
type fakeBrandingStore struct {
	name        *string
	icon        []byte
	etag        *string
	maint       bool
	maintMsg    *string
	loginBG     []byte
	loginBGEtag *string
}

func (f *fakeBrandingStore) Get(context.Context) (branding.Settings, error) {
	return branding.Settings{
		Name: f.name, IconPNG: f.icon, IconEtag: f.etag,
		Maintenance: f.maint, MaintenanceMessage: f.maintMsg,
		LoginBG: f.loginBG, LoginBGEtag: f.loginBGEtag,
	}, nil
}
func (f *fakeBrandingStore) SetName(context.Context, *string) error              { return nil }
func (f *fakeBrandingStore) SetIcon(context.Context, []byte, string) error       { return nil }
func (f *fakeBrandingStore) ClearIcon(context.Context) error                     { return nil }
func (f *fakeBrandingStore) SetMaintenance(context.Context, bool, *string) error { return nil }
func (f *fakeBrandingStore) SetLoginBG(context.Context, []byte, string) error    { return nil }
func (f *fakeBrandingStore) ClearLoginBG(context.Context) error                  { return nil }
```

Extend `TestBrandingConfigEndpoint` (line 37) to also assert the background fields — replace its `want` slice with:

```go
	for _, want := range []string{`"instanceName":"TestCo"`, `"iconUrl":"/branding/icon"`, `"hasCustomIcon":false`, `"hasCustomBackground":false`, `"backgroundUrl":"/branding/background"`} {
```

Add a serve 404 test:

```go
func TestBrandingBackground_404WhenUnset(t *testing.T) {
	s := &Server{branding: branding.NewWithStore("TestCo", &fakeBrandingStore{})}
	rec := httptest.NewRecorder()
	s.handleGetBrandingBackgroundHTTP(rec, httptest.NewRequest(http.MethodGet, "/branding/background", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
```

In `pkg/server/handle_admin_settings_test.go`, extend `settingsBrandingStore` (lines 27-58) with the columns + interface methods (append fields to the struct and these methods):

Add fields to the struct: `loginBG []byte` and `loginBGEtag *string`. Update its `Get` to include `LoginBG: f.loginBG, LoginBGEtag: f.loginBGEtag,`. Then add:

```go
func (f *settingsBrandingStore) SetLoginBG(_ context.Context, raw []byte, etag string) error {
	e := etag
	f.loginBG, f.loginBGEtag = raw, &e
	return nil
}
func (f *settingsBrandingStore) ClearLoginBG(_ context.Context) error {
	f.cleared = true
	f.loginBG, f.loginBGEtag = nil, nil
	return nil
}
```

Add the round-trip + delete tests (imports needed in this file: add `"bytes"`, `"image"`, `"image/png"` to the import block):

```go
func pngFixture(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 12, 8))); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestAdminSettings_PutAndServeBackground_Verbatim(t *testing.T) {
	t.Parallel()

	st := &settingsBrandingStore{}
	s := &Server{branding: branding.NewWithStore("TestDefault", st), Audit: noopAuditWriter{}}

	raw := pngFixture(t)
	sess := adminSession(time.Now().Add(time.Hour)) // fresh sudo
	req := reqWithSession("PUT", "/api/prohibitorum/admin/settings/background", string(raw), "image/png", sess)
	rr := httptest.NewRecorder()
	s.handlePutInstanceBackgroundHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("PUT status = %d, want 204; body: %s", rr.Code, rr.Body.String())
	}

	// Serve MUST return the exact uploaded bytes (no postprocess).
	grr := httptest.NewRecorder()
	s.handleGetBrandingBackgroundHTTP(grr, httptest.NewRequest("GET", "/branding/background", nil))
	if grr.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", grr.Code)
	}
	if !bytes.Equal(grr.Body.Bytes(), raw) {
		t.Fatalf("served %d bytes != uploaded %d bytes — background must be verbatim", grr.Body.Len(), len(raw))
	}
}

func TestAdminSettings_DeleteBackground(t *testing.T) {
	t.Parallel()

	raw := pngFixture(t)
	etag := "abc123"
	st := &settingsBrandingStore{loginBG: raw, loginBGEtag: &etag}
	s := &Server{branding: branding.NewWithStore("TestDefault", st), Audit: noopAuditWriter{}}

	sess := adminSession(time.Now().Add(time.Hour))
	req := reqWithSession("DELETE", "/api/prohibitorum/admin/settings/background", "", "", sess)
	rr := httptest.NewRecorder()
	s.handleDeleteInstanceBackgroundHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body: %s", rr.Code, rr.Body.String())
	}
	if !st.cleared {
		t.Error("store.ClearLoginBG was not called")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test -tags nodynamic ./pkg/server/ -run 'Branding|AdminSettings' -v`
Expected: FAIL — `handleGetBrandingBackgroundHTTP` / `handlePutInstanceBackgroundHTTP` / `handleDeleteInstanceBackgroundHTTP` undefined; config body missing the new fields.

- [ ] **Step 4: Extend the public config + add the serve handler** in `pkg/server/handle_branding.go`.

Replace `handleGetPublicConfigHTTP` (lines 13-28) with:

```go
// GET /api/prohibitorum/config (public)
func (s *Server) handleGetPublicConfigHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, etag, _ := s.branding.Icon(ctx)
	maintenance, maintenanceMsg := s.branding.Maintenance(ctx)
	_, bgEtag, hasBG := s.branding.Background(ctx)
	cfg := contract.PublicConfig{
		InstanceName:        s.branding.InstanceName(ctx),
		HasCustomIcon:       s.branding.HasCustomIcon(ctx),
		IconURL:             "/branding/icon",
		IconEtag:            etag,
		MaintenanceMode:     maintenance,
		MaintenanceMessage:  maintenanceMsg,
		HasCustomBackground: hasBG,
		BackgroundURL:       "/branding/background",
		BackgroundEtag:      bgEtag,
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(cfg)
}
```

Append the serve handler at the end of the file:

```go
// GET /branding/background (public) — serves the custom login-page background
// verbatim (byte-for-byte) with ETag/304, or 404 when none is set.
func (s *Server) handleGetBrandingBackgroundHTTP(w http.ResponseWriter, r *http.Request) {
	data, etag, ok := s.branding.Background(r.Context())
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeIconResponse(w, r, data, etag)
}
```

- [ ] **Step 5: Add the admin handlers** to `pkg/server/handle_admin_settings.go` (after `handleDeleteInstanceIconHTTP`, ~line 113). Reuses the existing `maxIconRead` const, `authn`, `branding`, `io`, `errors` imports:

```go
// PUT /api/prohibitorum/admin/settings/background  (raw image body, up to 5 MiB)
// Same shape as the icon upload: registerOpHTTP(admin) + an in-handler fresh-sudo
// gate (the sudo wrapper rejects non-JSON bodies and caps size). The image is
// stored and later served VERBATIM — validated but never re-encoded.
func (s *Server) handlePutInstanceBackgroundHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if s.requireFreshSudo(r.Context(), w, sess) {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxIconRead))
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if err := s.branding.SetLoginBackground(r.Context(), raw); err != nil {
		if errors.Is(err, branding.ErrTooLarge) {
			writeAvatarErr(w, "avatar_too_large", "background: image exceeds 5 MiB")
			return
		}
		writeAvatarErr(w, "avatar_invalid_image", "background: invalid or unsupported image format")
		return
	}
	s.auditBranding(r, "instance_login_background_updated")
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/prohibitorum/admin/settings/background
// Registered via registerSudoOpHTTP — admin role + fresh sudo enforced by wrapper.
func (s *Server) handleDeleteInstanceBackgroundHTTP(w http.ResponseWriter, r *http.Request) {
	if err := s.branding.ClearLoginBackground(r.Context()); err != nil {
		writeAuthErr(w, err)
		return
	}
	s.auditBranding(r, "instance_login_background_removed")
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 6: Register routes** in `pkg/server/server.go`.

After line 426 (`registerOpHTTP(... "/branding/icon" ...)`) add:

```go
	registerOpHTTP(s.router, "GET", "/branding/background", publicReq, s.handleGetBrandingBackgroundHTTP)
```

After line 500 (`... "DELETE", "/api/prohibitorum/admin/settings/icon" ...`) add:

```go
	registerOpHTTP(s.router, "PUT", "/api/prohibitorum/admin/settings/background", admin, s.handlePutInstanceBackgroundHTTP)
	s.registerSudoOpHTTP(s.router, "DELETE", "/api/prohibitorum/admin/settings/background", admin, s.handleDeleteInstanceBackgroundHTTP)
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test -tags nodynamic ./pkg/server/ -run 'Branding|AdminSettings' -v`
Expected: PASS. Then `go build -tags nodynamic ./...` → no errors.

- [ ] **Step 8: Commit**

```bash
git add pkg/contract/auth.go pkg/server/handle_branding.go pkg/server/handle_admin_settings.go pkg/server/server.go pkg/server/handle_branding_test.go pkg/server/handle_admin_settings_test.go
git commit -m "feat(server): admin upload/remove + public verbatim serve of login background"
```

---

### Task 4: Frontend — branding store + AuthBackdrop

**Goal:** The branding store exposes `hasCustomBackground` + a cache-busted `backgroundSrc`, and `AuthBackdrop` uses it as the top precedence tier over the build-time asset and CSS gradient.

**Files:**
- Modify: `dashboard/src/stores/branding.ts`
- Modify: `dashboard/src/components/custom/AuthBackdrop.vue`
- Modify: `dashboard/src/stores/branding.test.ts`

**Acceptance Criteria:**
- [ ] Store defaults: `hasCustomBackground=false`, `backgroundSrc="/branding/background"`.
- [ ] After `load()` with `{hasCustomBackground:true, backgroundEtag:'bg123456xx'}`, `backgroundSrc==="/branding/background?v=bg123456"`.
- [ ] `AuthBackdrop` renders the DB background when `hasCustomBackground`, else the build-time asset, else the placeholder class; the scrim always renders.

**Verify:** `cd dashboard && npx vitest run src/stores/branding.test.ts` → PASS; `npx vue-tsc -b` → 0 errors.

**Steps:**

- [ ] **Step 1: Add failing store tests** — append to `dashboard/src/stores/branding.test.ts`:

```ts
  it('defaults to no custom background', () => {
    const b = useBrandingStore()
    expect(b.hasCustomBackground).toBe(false)
    expect(b.backgroundSrc).toBe('/branding/background')
  })

  it('load() builds a cache-busted backgroundSrc', async () => {
    vi.mocked(api.get).mockResolvedValue({
      instanceName: 'Acme SSO', hasCustomIcon: false, iconUrl: '/branding/icon', iconEtag: '',
      hasCustomBackground: true, backgroundUrl: '/branding/background', backgroundEtag: 'bg123456xx',
    })
    const b = useBrandingStore()
    await b.load()
    expect(b.hasCustomBackground).toBe(true)
    expect(b.backgroundSrc).toBe('/branding/background?v=bg123456')
  })
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd dashboard && npx vitest run src/stores/branding.test.ts`
Expected: FAIL — `b.hasCustomBackground` / `b.backgroundSrc` undefined.

- [ ] **Step 3: Extend the store** `dashboard/src/stores/branding.ts`.

Add to the `PublicConfig` interface (after `maintenanceMessage: string`):

```ts
  hasCustomBackground: boolean
  backgroundUrl: string
  backgroundEtag: string
```

Add reactive state (after `const maintenanceMessage = ref('')`):

```ts
  const hasCustomBackground = ref(false)
  const backgroundEtag = ref('')
```

Add the computed (after the `iconSrc` computed):

```ts
  const backgroundSrc = computed(() => {
    const v = backgroundEtag.value ? backgroundEtag.value.slice(0, 8) : ''
    return v ? `/branding/background?v=${v}` : '/branding/background'
  })
```

In `load()` (after `maintenanceMessage.value = cfg.maintenanceMessage ?? ''`) add:

```ts
      hasCustomBackground.value = !!cfg.hasCustomBackground
      backgroundEtag.value = cfg.backgroundEtag ?? ''
```

In the returned object add `hasCustomBackground, backgroundEtag, backgroundSrc`:

```ts
  return {
    instanceName, hasCustomIcon, iconEtag, iconSrc,
    maintenanceMode, maintenanceMessage,
    hasCustomBackground, backgroundEtag, backgroundSrc,
    load, ensureLoaded,
  }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd dashboard && npx vitest run src/stores/branding.test.ts`
Expected: PASS.

- [ ] **Step 5: Wire `AuthBackdrop`** `dashboard/src/components/custom/AuthBackdrop.vue`.

Replace the `<script setup>` body (lines 20-27, the asset lookup) so it computes precedence — DB background → build-time asset → placeholder:

```ts
import { computed } from 'vue'
import { useBrandingStore } from '@/stores/branding'

const branding = useBrandingStore()

// Optional real scene asset resolved at build time (empty object → no match).
const sceneModules = import.meta.glob(
  '../../assets/auth-scene.{png,jpg,jpeg,webp,avif}',
  { eager: true, query: '?url', import: 'default' },
) as Record<string, string>
const assetUrl: string | undefined = Object.values(sceneModules)[0]

// Precedence: admin-uploaded DB background → build-time asset → CSS placeholder.
const sceneUrl = computed<string | undefined>(() =>
  branding.hasCustomBackground ? branding.backgroundSrc : assetUrl,
)
```

The template stays as-is — `sceneUrl` is now a computed ref, auto-unwrapped in the template, so `:class="{ 'auth-backdrop__scene--placeholder': !sceneUrl }"` and `:style="sceneUrl ? { backgroundImage: \`url(${sceneUrl})\` } : undefined"` keep working. (Update the doc-comment's "Scene source" note to mention the DB background takes precedence.)

- [ ] **Step 6: Typecheck**

Run: `cd dashboard && npx vue-tsc -b`
Expected: 0 errors.

- [ ] **Step 7: Commit**

```bash
git add dashboard/src/stores/branding.ts dashboard/src/components/custom/AuthBackdrop.vue dashboard/src/stores/branding.test.ts
git commit -m "feat(dashboard): AuthBackdrop uses admin login background (DB → asset → gradient)"
```

---

### Task 5: Frontend — admin SettingsView background card + i18n

**Goal:** Add a "Login background" card to `/admin/settings` that uploads (sudo) and removes the background, mirroring the icon card, with en + zh strings.

**Files:**
- Modify: `dashboard/src/pages/admin/SettingsView.vue`
- Modify: `dashboard/src/locales/en.ts` (`admin.settings`, ~line 556-575)
- Modify: `dashboard/src/locales/zh.ts` (`admin.settings`, ~line 552-571)
- Modify: `dashboard/src/pages/admin/SettingsView.test.ts`

**Acceptance Criteria:**
- [ ] The card renders an Upload button (`data-test="upload-background"`); a Remove button appears only when `hasCustomBackground`.
- [ ] Upload calls `api.upload('/api/prohibitorum/admin/settings/background', file)`; Remove calls `api.del('/api/prohibitorum/admin/settings/background')` — both via `withSudo`.
- [ ] en and zh both define `backgroundLabel/backgroundHint/backgroundUpload/backgroundRemove/backgroundError`.

**Verify:** `cd dashboard && npx vitest run src/pages/admin/SettingsView.test.ts` → PASS; `npx vue-tsc -b` → 0 errors.

**Steps:**

- [ ] **Step 1: Add i18n keys.**

In `dashboard/src/locales/en.ts`, inside `admin.settings` (after `maintenanceSave: 'Save maintenance settings',`, before the closing `},` at line 575) add:

```ts
      backgroundLabel: 'Login background',
      backgroundHint: 'A full-page background for the sign-in and other pre-login pages (PNG, JPEG, or WebP, up to 5 MB). Served exactly as uploaded — no cropping or re-encoding.',
      backgroundUpload: 'Upload background',
      backgroundRemove: 'Remove background',
      backgroundError: 'That image could not be used. Try a PNG, JPEG, or WebP under 5 MB.',
```

In `dashboard/src/locales/zh.ts`, inside `admin.settings` (after `maintenanceSave: '保存维护设置',`, before the closing `},` at line 571) add:

```ts
      backgroundLabel: '登录页背景',
      backgroundHint: '用于登录页及其他未登录页面的整页背景图片（PNG、JPEG 或 WebP，最大 5 MB）。将按上传的原图直接使用，不做裁剪或重新编码。',
      backgroundUpload: '上传背景',
      backgroundRemove: '移除背景',
      backgroundError: '无法使用该图片，请换一张小于 5 MB 的 PNG、JPEG 或 WebP。',
```

> After editing en.ts, verify no smart-quote corruption: `grep -nP "[\x{2018}\x{2019}]" dashboard/src/locales/en.ts` → no output. (See the en.ts apostrophe hazard note.)

- [ ] **Step 2: Add failing view tests** — append to `dashboard/src/pages/admin/SettingsView.test.ts`.

First update `mountView` to also seed the background flag (replace the `$patch` line, line 21):

```ts
  branding.$patch({ instanceName: 'TestInstance', hasCustomIcon, iconSrc: '/api/prohibitorum/icon', hasCustomBackground: hasCustomIcon, backgroundSrc: '/branding/background' })
```

Then add:

```ts
  it('renders the Upload background button', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.find('[data-test="upload-background"]').exists()).toBe(true)
  })

  it('does not show Remove background when hasCustomBackground is false', async () => {
    const w = mountView(false)
    await flushPromises()
    expect(w.find('[data-test="remove-background"]').exists()).toBe(false)
  })

  it('shows Remove background when set and calls api.del on click', async () => {
    del.mockResolvedValue({})
    const w = mountView(true)
    await flushPromises()
    const btn = w.find('[data-test="remove-background"]')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    await flushPromises()
    expect(del).toHaveBeenCalledWith('/api/prohibitorum/admin/settings/background')
  })
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd dashboard && npx vitest run src/pages/admin/SettingsView.test.ts`
Expected: FAIL — no `upload-background` element.

- [ ] **Step 4: Add the card + handlers** to `dashboard/src/pages/admin/SettingsView.vue`.

In `<script setup>` (after the `uploadError`/`fileInput` refs, ~line 24) add:

```ts
const bgInput = ref<HTMLInputElement | null>(null)
const bgError = ref('')
```

After `removeIcon()` (end of the script, ~line 85) add:

```ts
async function onPickBackground(e: Event): Promise<void> {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  bgError.value = ''
  const ok = await run(() => withSudo(async () => {
    await api.upload('/api/prohibitorum/admin/settings/background', file)
    return true as const
  }))
  if (ok) {
    await branding.load()
  } else {
    bgError.value = t('admin.settings.backgroundError')
  }
  if (bgInput.value) bgInput.value.value = ''
}

async function removeBackground(): Promise<void> {
  const ok = await run(() => withSudo(async () => {
    await api.del('/api/prohibitorum/admin/settings/background')
    return true as const
  }))
  if (ok) await branding.load()
}
```

In the `<template>`, after the icon `<Card>` (closes at line 159) and before the final `</div>` (line 160) add:

```html
    <Card>
      <CardHeader><CardTitle>{{ t('admin.settings.backgroundLabel') }}</CardTitle></CardHeader>
      <CardContent class="flex flex-col gap-3">
        <div class="flex items-start gap-4">
          <span class="inline-flex h-16 w-28 items-center justify-center overflow-hidden rounded-md bg-ember/10 ring-1 ring-inset ring-border">
            <img v-if="branding.hasCustomBackground" :src="branding.backgroundSrc" :alt="t('admin.settings.backgroundLabel')" class="size-full object-cover" />
          </span>
          <div class="flex flex-col gap-2">
            <p class="text-xs text-muted">{{ t('admin.settings.backgroundHint') }}</p>
            <div class="flex gap-2">
              <input ref="bgInput" type="file" accept="image/png,image/jpeg,image/webp" class="hidden" data-test="background-input" @change="onPickBackground" />
              <Button variant="outline" size="sm" :disabled="busy" data-test="upload-background" @click="bgInput?.click()">{{ t('admin.settings.backgroundUpload') }}</Button>
              <Button v-if="branding.hasCustomBackground" variant="outline" size="sm" :disabled="busy" data-test="remove-background" @click="removeBackground">{{ t('admin.settings.backgroundRemove') }}</Button>
            </div>
            <p v-if="bgError" class="text-sm text-destructive" role="alert">{{ bgError }}</p>
          </div>
        </div>
      </CardContent>
    </Card>
```

- [ ] **Step 5: Run tests + typecheck**

Run: `cd dashboard && npx vitest run src/pages/admin/SettingsView.test.ts && npx vue-tsc -b`
Expected: PASS + 0 typecheck errors.

- [ ] **Step 6: Full frontend test + i18n parity**

Run: `cd dashboard && npx vitest run`
Expected: all suites PASS (including any en/zh parity guard — both locales now define the 5 new keys).

- [ ] **Step 7: Commit**

```bash
git add dashboard/src/pages/admin/SettingsView.vue dashboard/src/locales/en.ts dashboard/src/locales/zh.ts dashboard/src/pages/admin/SettingsView.test.ts
git commit -m "feat(dashboard): admin login-background upload/remove card (en + zh)"
```

---

### Task 6: End-to-end smoke coverage + full gate + dist

**Goal:** Prove the verbatim round-trip against a live server, run the full done-gate, and commit the rebuilt dashboard bundle.

**Files:**
- Modify: `cmd/smoke/main.go` (insert a block after the maintenance block, ~line 4490; update the final banner string ~line 4493)
- Modify: `dashboard/dist/**` (rebuilt bundle)

**Acceptance Criteria:**
- [ ] Smoke uploads a PNG (admin sudo PUT), fetches `/branding/background`, and asserts byte-for-byte equality; asserts `/config.hasCustomBackground=true`; removes it and asserts a 404.
- [ ] Full gate green: `go build -tags nodynamic ./...`, `go vet ./...`, `go test -tags nodynamic ./...`, `cd dashboard && npx vitest run`, `npx vue-tsc -b`, live smoke `SMOKE_EXIT=0`.
- [ ] `dashboard/dist` rebuilt and committed.

**Verify:** run the smoke against a live server (below) → prints `✓ smoke OK …` and exits 0.

**Steps:**

- [ ] **Step 1: Add the smoke block** — in `cmd/smoke/main.go`, immediately after the maintenance block closes (the `}` at line 4490, before `fmt.Println()` at line 4492), insert:

```go
	// =====================================================================
	//  LOGIN BACKGROUND — admin uploads a custom login-page background; verify
	//  it is served BYTE-FOR-BYTE (no postprocess), /config reflects it, and
	//  removal restores the 404 (frontend then falls back to its build-time asset).
	// =====================================================================
	{
		const nBg = 4

		var bgBuf bytes.Buffer
		if err := png.Encode(&bgBuf, image.NewRGBA(image.Rect(0, 0, 12, 8))); err != nil {
			log.Fatalf("bg: encode PNG: %v", err)
		}
		bgBytes := bgBuf.Bytes()

		step(fmt.Sprintf("bg %d/%d — admin uploads login background (sudo PUT raw PNG) → 204", 1, nBg))
		if err := sudoWebAuthn(c, auth, *baseURL); err != nil {
			log.Fatalf("bg: sudo (upload): %v", err)
		}
		{
			req, err := http.NewRequest(http.MethodPut, *baseURL+"/api/prohibitorum/admin/settings/background", bytes.NewReader(bgBytes))
			if err != nil {
				log.Fatalf("bg: build PUT: %v", err)
			}
			req.Header.Set("Content-Type", "image/png")
			for _, ck := range c.cookies() {
				req.AddCookie(ck)
			}
			resp, err := c.hc.Do(req)
			if err != nil {
				log.Fatalf("bg: PUT background: %v", err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				log.Fatalf("bg: PUT background: want 204, got %d — %s", resp.StatusCode, firstN(string(body), 300))
			}
		}
		log.Printf("  PUT /admin/settings/background → 204 ✓")

		step(fmt.Sprintf("bg %d/%d — public GET /branding/background returns the EXACT uploaded bytes", 2, nBg))
		got, err := c.getBytes("/branding/background")
		if err != nil {
			log.Fatalf("bg: GET /branding/background: %v", err)
		}
		if !bytes.Equal(got, bgBytes) {
			log.Fatalf("bg: served %d bytes != uploaded %d bytes — background must be verbatim (no postprocess)", len(got), len(bgBytes))
		}
		log.Printf("  GET /branding/background = %d bytes, byte-for-byte identical ✓", len(got))

		step(fmt.Sprintf("bg %d/%d — /config reflects hasCustomBackground=true", 3, nBg))
		var cfg struct {
			HasCustomBackground bool   `json:"hasCustomBackground"`
			BackgroundURL       string `json:"backgroundUrl"`
			BackgroundEtag      string `json:"backgroundEtag"`
		}
		if err := c.get("/api/prohibitorum/config", &cfg); err != nil {
			log.Fatalf("bg: GET /config: %v", err)
		}
		if !cfg.HasCustomBackground || cfg.BackgroundURL != "/branding/background" || cfg.BackgroundEtag == "" {
			log.Fatalf("bg: /config = %+v, want hasCustomBackground=true + backgroundUrl=/branding/background + non-empty etag", cfg)
		}
		log.Printf("  /config hasCustomBackground=true etag=%s… ✓", firstN(cfg.BackgroundEtag, 8))

		step(fmt.Sprintf("bg %d/%d — admin removes background (sudo DELETE) → GET 404", 4, nBg))
		if err := sudoWebAuthn(c, auth, *baseURL); err != nil {
			log.Fatalf("bg: sudo (remove): %v", err)
		}
		{
			req, err := http.NewRequest(http.MethodDelete, *baseURL+"/api/prohibitorum/admin/settings/background", nil)
			if err != nil {
				log.Fatalf("bg: build DELETE: %v", err)
			}
			for _, ck := range c.cookies() {
				req.AddCookie(ck)
			}
			resp, err := c.hc.Do(req)
			if err != nil {
				log.Fatalf("bg: DELETE background: %v", err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				log.Fatalf("bg: DELETE background: want 204, got %d — %s", resp.StatusCode, firstN(string(body), 300))
			}
		}
		if resp, err := c.getRaw("/branding/background"); err != nil {
			log.Fatalf("bg: GET after delete: %v", err)
		} else {
			resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				log.Fatalf("bg: GET /branding/background after delete: want 404, got %d", resp.StatusCode)
			}
		}
		log.Printf("  DELETE background → GET /branding/background 404 ✓")
	}
```

Then append a short clause to the final success banner string (the `fmt.Println("✓ smoke OK — …` at line 4493), before ` + DB-state assertions passed against`:

```
 + login-background (admin sudo PUT custom login-page background → public GET /branding/background byte-for-byte verbatim; /config hasCustomBackground round-trip; sudo DELETE → 404)
```

(`bytes`, `image`, `image/png`, `io`, `net/http`, `fmt`, `log` are already imported in this file.)

- [ ] **Step 2: Backend gate**

Run:
```bash
go build -tags nodynamic ./... && go vet ./... && go test -tags nodynamic ./...
```
Expected: builds clean; vet clean; tests pass. (Note: `pkg/server` can flake ~1/3 under parallel shared-DB runs — re-run the specific package in isolation to confirm any failure is the known flake, not a regression.)

- [ ] **Step 3: Rebuild the dashboard bundle**

Run:
```bash
cd dashboard && npx vue-tsc -b && npm run build
```
Expected: typecheck 0 errors; Vite writes `dashboard/dist/**`.

- [ ] **Step 4: Run the live smoke.**

Start a throwaway DB + a server on a free port at migration ≥ 26, then run the smoke against it (mirror the project's smoke procedure; the DB is managed by `scripts/db.sh` / `mise run db …`). Concretely:

```bash
# from repo root — build the server with the required tag
go build -tags nodynamic -o /tmp/prohibitorum ./cmd/prohibitorum
# bring up a throwaway DB, apply migrations (incl. 026), boot the server on a free
# port, enroll an admin, then:
go run -tags nodynamic ./cmd/smoke --base-url http://127.0.0.1:<port> ; echo "SMOKE_EXIT=$?"
```
Expected: prints `✓ smoke OK — … + login-background (…)` and `SMOKE_EXIT=0`.

> If the user's dev server holds the default port, run against a fresh `prohibitorum_smoke` DB on a different port (see the dev-postgres note). Kill the wrapped script PID directly if using a `mise run` wrapper (long-task orphan note).

- [ ] **Step 5: Commit smoke + dist**

```bash
git add cmd/smoke/main.go dashboard/dist
git commit -m "test(smoke): verbatim login-background round-trip; rebuild dist"
```

---

## Self-Review

**1. Spec coverage:**
- Migration 026 → Task 2. `ValidateRaw` (validate-not-transform, allowlist, dims, etag) → Task 1. Branding Settings/Store/Resolver (Background/HasCustomBackground/Set/Clear, DB-only) → Task 2. `PublicConfig` + config handler + verbatim serve → Task 3. Admin PUT/DELETE (sudo shapes, audit reasons, error codes) → Task 3. Store fakes updated → Tasks 2 & 3. Frontend store `backgroundSrc` + `AuthBackdrop` precedence (scrim kept, all threshold pages) → Task 4. Admin SettingsView card + en/zh i18n → Task 5. Tests (imageutil unit, handler byte-for-byte, ETag/304, vitest store + view) → Tasks 1/3/4/5. Live smoke + full gate + dist → Task 6. No spec requirement is unmapped.
- Out-of-scope items (per-theme background, config-file default, cropping UI, client re-encode) are correctly absent.

**2. Placeholder scan:** Every code step contains complete code; every verify step names an exact command + expected result. The one intentionally-flagged note in Task 6 Step 4 (live-smoke boot steps use the project's existing DB/server procedure rather than reproducing it) references concrete tooling (`scripts/db.sh`, `go build -tags nodynamic`, `--base-url`) and an exact success assertion (`SMOKE_EXIT=0`). No "TBD/TODO/handle appropriately".

**3. Type consistency:** `ValidateRaw(raw []byte) (etag string, err error)` — same signature used by the resolver in Task 2. `Background(ctx) (data []byte, etag string, custom bool)` — consumers in Task 3 read `_, bgEtag, hasBG` and `data, etag, ok` consistently. Store methods `SetLoginBG(ctx, []byte, string)` / `ClearLoginBG(ctx)` — identical across interface, `PGStore`, and all three test fakes (`fakeStore`, `fakeBrandingStore`, `settingsBrandingStore`). Contract JSON keys `hasCustomBackground`/`backgroundUrl`/`backgroundEtag` match the store's `PublicConfig` interface and the smoke's struct tags. Endpoint paths (`/branding/background`, `/api/prohibitorum/admin/settings/background`) are identical in handlers, routes, frontend, and smoke. i18n keys (`backgroundLabel/Hint/Upload/Remove/Error`) match between template usage and both locale files.
