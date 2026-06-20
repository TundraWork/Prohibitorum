# Instance Branding & Dynamic Page Titles — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an operator customize the instance name + icon (config default → DB override), surface them as the favicon and in-app brand mark on every page, and set a dynamic `<page> · <instance>` document title per route.

**Architecture:** A `pkg/branding` resolver owns the effective name/icon with DB→config→default precedence and a cache; a singleton `instance_settings` DB row holds runtime overrides; public `GET /config` + `GET /branding/icon` feed the SPA and favicon; admin `/admin/settings` endpoints (admin+sudo) write the overrides; the SPA brand mark + a router title guard read a branding store; the webui handler templates the config instance name into `<title>`.

**Tech Stack:** Go (pgx, goose migrations, stdlib `image/png`), Vue 3 + Vite + Tailwind v4 + shadcn-vue/Reka, vitest, vue-i18n (en + zh), Playwright/chromium for runtime checks.

**Spec:** `docs/superpowers/specs/2026-06-20-instance-branding-and-page-titles-design.md`

**Standing conventions (project memory):**
- Backend build uses `-tags nodynamic`: `go build -tags nodynamic ./...`. Gate: that + `go vet ./...` + `go test ./...`; FE: `cd dashboard && npm run test`, `npx vue-tsc -b`, `node scripts/check-contrast.mjs`.
- `en.ts` apostrophe hazard: any English string with `'` MUST use double-quote delimiters; grep-verify no U+2019 after editing. No literal `@` in i18n messages.
- `zh.ts` must stay key-parity with `en.ts` (locale tests enforce) — add every new key to BOTH.
- Embedded SPA `pkg/webui/dist` is committed; rebuild + commit it at the done-gate (`cd dashboard && npm run build`).
- NEVER add a `Co-Authored-By` trailer to commits.
- Migrations are goose (`-- +goose Up` / `-- +goose Down`), embedded + auto-applied on boot; next number is **017**.
- **Runtime verification:** the harness kills servers launched from the controller's own Bash (exit 144); launch the verification server from a **subagent**. `mise run db:start` is broken — the podman Postgres is already up on :5432 (DB `prohibitorum_dev`, user/pass `prohibitorum`); `source scripts/dev-env.sh` for the DSN.

---

### Task 1: `pkg/branding` foundation (config + migration + resolver + icon pipeline)

**Goal:** A self-contained branding package: config defaults, the singleton DB table, a cached resolver with DB→config→default precedence, PNG icon processing, and the bundled default icon.

**Files:**
- Modify: `pkg/configx/configx.go` (add `Branding` substruct + field + default)
- Create: `db/migrations/017_instance_settings.sql`
- Create: `pkg/branding/branding.go` (resolver + store interface + ProcessIcon)
- Create: `pkg/branding/store_pg.go` (pgx-backed store)
- Create: `pkg/branding/default_icon.png` (bundled default) + `pkg/branding/default_icon.go` (`//go:embed`)
- Test: `pkg/branding/branding_test.go`

**Acceptance Criteria:**
- [ ] `configx.Config.Branding.InstanceName` defaults to `"Prohibitorum"`; `Branding.IconPath` defaults to `""`.
- [ ] Migration `017` creates a single-row `instance_settings` table (CHECK id=1) seeded with NULLs.
- [ ] `Resolver.InstanceName(ctx)` = DB name (non-empty) → config name → `"Prohibitorum"`.
- [ ] `Resolver.Icon(ctx)` = DB icon → config-file icon → embedded default; `HasCustomIcon(ctx)` true only for DB/config.
- [ ] `ProcessIcon(raw)` returns a square 256×256 PNG + sha256 etag; rejects >5 MiB and undecodable input.
- [ ] `Invalidate()` forces the next read to reload from the store.

**Verify:** `go test ./pkg/branding/...` → ok; `go build -tags nodynamic ./...` → 0

**Steps:**

- [ ] **Step 1: configx — add the Branding substruct.** In `pkg/configx/configx.go`, add the field to `Config` (next to `Auth AuthConfig`):

```go
	Branding BrandingConfig `mapstructure:"branding"`
```

Add the type (near `WebAuthnConfig`):

```go
// BrandingConfig holds the deploy-time instance identity. These are DEFAULTS:
// an admin can override the name + icon at runtime (stored in instance_settings).
// InstanceName is purely cosmetic — it does NOT change the WebAuthn RPDisplayName,
// the OIDC Issuer, or the TOTP issuer.
type BrandingConfig struct {
	InstanceName string `mapstructure:"instance_name"`
	// IconPath is an optional path to a PNG/JPEG/WebP the operator drops at
	// deploy. Empty = no config icon (fall back to the built-in default).
	IconPath string `mapstructure:"icon_path"`
}
```

Find where defaults are applied (the function with `config.WebAuthn.RPID == ""` etc.) and add:

```go
	if config.Branding.InstanceName == "" {
		config.Branding.InstanceName = "Prohibitorum"
	}
```

- [ ] **Step 2: Migration `017_instance_settings.sql`:**

```sql
-- +goose Up
-- Singleton instance-branding overrides. Exactly one row (id = 1); NULL columns
-- mean "no override — fall back to config / built-in default". The icon is a
-- pre-processed square PNG (see pkg/branding.ProcessIcon).
CREATE TABLE instance_settings (
  id            smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  instance_name text NULL,
  icon_png      bytea NULL,
  icon_etag     text NULL,
  updated_at    timestamptz NOT NULL DEFAULT now()
);
INSERT INTO instance_settings (id, instance_name, icon_png, icon_etag) VALUES (1, NULL, NULL, NULL);

-- +goose Down
DROP TABLE instance_settings;
```

- [ ] **Step 3: bundled default icon.** Create a 256×256 PNG at `pkg/branding/default_icon.png` (any simple square mark — e.g. a solid Ember rounded square with a white shield glyph; a placeholder solid-color PNG is acceptable for v1, refined later). Generate it however convenient (an image tool, or a tiny throwaway Go `image/png` snippet) and commit the bytes. Then `pkg/branding/default_icon.go`:

```go
package branding

import _ "embed"

//go:embed default_icon.png
var defaultIconPNG []byte

// defaultIconEtag is a fixed sentinel etag for the built-in icon (stable across
// boots so caches/304s work; it never changes without a code change).
const defaultIconEtag = "default"
```

- [ ] **Step 4: write the failing test** `pkg/branding/branding_test.go`:

```go
package branding

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"testing"
)

// fakeStore is an in-memory branding store for precedence tests.
type fakeStore struct {
	name     *string
	iconPNG  []byte
	iconEtag *string
}

func (f *fakeStore) Get(context.Context) (Settings, error) {
	return Settings{Name: f.name, IconPNG: f.iconPNG, IconEtag: f.iconEtag}, nil
}
func (f *fakeStore) SetName(_ context.Context, n *string) error { f.name = n; return nil }
func (f *fakeStore) SetIcon(_ context.Context, png []byte, etag string) error {
	f.iconPNG, f.iconEtag = png, &etag
	return nil
}
func (f *fakeStore) ClearIcon(context.Context) error { f.iconPNG, f.iconEtag = nil, nil; return nil }

func strp(s string) *string { return &s }

func TestInstanceName_Precedence(t *testing.T) {
	ctx := context.Background()
	// DB override wins.
	r, _ := New("ConfigName", "", &fakeStore{name: strp("DBName")})
	if got := r.InstanceName(ctx); got != "DBName" {
		t.Fatalf("DB override: got %q want DBName", got)
	}
	// Config wins when DB is NULL.
	r, _ = New("ConfigName", "", &fakeStore{})
	if got := r.InstanceName(ctx); got != "ConfigName" {
		t.Fatalf("config: got %q want ConfigName", got)
	}
	// Built-in default when config is empty too.
	r, _ = New("", "", &fakeStore{})
	if got := r.InstanceName(ctx); got != "Prohibitorum" {
		t.Fatalf("default: got %q want Prohibitorum", got)
	}
}

func TestIcon_Precedence_And_HasCustom(t *testing.T) {
	ctx := context.Background()
	// No DB, no config → built-in default, not custom.
	r, _ := New("X", "", &fakeStore{})
	png0, etag0, _ := r.Icon(ctx)
	if len(png0) == 0 || etag0 != defaultIconEtag {
		t.Fatalf("default icon: len=%d etag=%q", len(png0), etag0)
	}
	if r.HasCustomIcon(ctx) {
		t.Fatal("HasCustomIcon should be false with no DB/config icon")
	}
	// DB icon wins + is custom.
	r, _ = New("X", "", &fakeStore{iconPNG: []byte("PNGBYTES"), iconEtag: strp("abc")})
	png1, etag1, _ := r.Icon(ctx)
	if string(png1) != "PNGBYTES" || etag1 != "abc" {
		t.Fatalf("db icon: %q %q", png1, etag1)
	}
	if !r.HasCustomIcon(ctx) {
		t.Fatal("HasCustomIcon should be true with a DB icon")
	}
}

func TestProcessIcon_PNG256Square(t *testing.T) {
	// Build a 400x200 source PNG.
	src := image.NewRGBA(image.Rect(0, 0, 400, 200))
	var buf bytes.Buffer
	_ = png.Encode(&buf, src)

	out, etag, err := ProcessIcon(buf.Bytes())
	if err != nil {
		t.Fatalf("ProcessIcon: %v", err)
	}
	if etag == "" {
		t.Fatal("empty etag")
	}
	img, format, derr := image.Decode(bytes.NewReader(out))
	if derr != nil || format != "png" {
		t.Fatalf("decode: format=%q err=%v", format, derr)
	}
	if b := img.Bounds(); b.Dx() != 256 || b.Dy() != 256 {
		t.Fatalf("size: %dx%d want 256x256", b.Dx(), b.Dy())
	}
}

func TestProcessIcon_RejectsGarbage(t *testing.T) {
	if _, _, err := ProcessIcon([]byte("not an image")); err == nil {
		t.Fatal("expected error on garbage input")
	}
}

func TestInvalidate_Reloads(t *testing.T) {
	ctx := context.Background()
	fs := &fakeStore{name: strp("First")}
	r, _ := New("Cfg", "", fs)
	_ = r.InstanceName(ctx) // prime cache
	fs.name = strp("Second")
	if got := r.InstanceName(ctx); got != "First" {
		t.Fatalf("pre-invalidate should be cached First, got %q", got)
	}
	r.Invalidate()
	if got := r.InstanceName(ctx); got != "Second" {
		t.Fatalf("post-invalidate should reload Second, got %q", got)
	}
}
```

- [ ] **Step 5: implement `pkg/branding/branding.go`:**

```go
// Package branding resolves the effective instance name + icon with
// DB-override → config-default → built-in precedence, and processes uploaded
// icons to a square PNG. The resolver caches the DB row; admin mutations call
// Invalidate() so changes apply immediately.
package branding

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"sync"

	_ "github.com/gen2brain/webp" // register WebP decoder for uploaded icons
	"golang.org/x/image/draw"
)

const (
	maxInputBytes = 5 << 20
	maxInputDim   = 10000
	iconSize      = 256
	defaultName   = "Prohibitorum"
)

var (
	ErrTooLarge     = errors.New("branding: input too large")
	ErrInvalidImage = errors.New("branding: invalid or unsupported image")
)

// Settings is the raw DB-override row (nil fields = no override). Exported so
// the server package's tests/wiring can build a Store fake.
type Settings struct {
	Name     *string
	IconPNG  []byte
	IconEtag *string
}

// Store is the persistence seam (real impl in store_pg.go; fakes in tests).
// Exported because pkg/server tests inject their own fake.
type Store interface {
	Get(ctx context.Context) (Settings, error)
	SetName(ctx context.Context, name *string) error
	SetIcon(ctx context.Context, png []byte, etag string) error
	ClearIcon(ctx context.Context) error
}

type Resolver struct {
	cfgName     string
	cfgIcon     []byte // processed config-file icon (nil if none)
	cfgIconEtag string
	st          Store

	mu    sync.RWMutex
	cache *Settings // nil = not loaded
}

// NewWithStore builds a resolver with no config-file icon — used by tests and
// any caller that doesn't need the optional config icon path.
func NewWithStore(cfgName string, st Store) *Resolver {
	return &Resolver{cfgName: cfgName, st: st}
}

// New builds the resolver. cfgIconPath (optional) is read + processed once; a
// missing/invalid file is treated as "no config icon" (logged by the caller).
func New(cfgName, cfgIconPath string, st Store) (*Resolver, error) {
	r := &Resolver{cfgName: cfgName, st: st}
	if cfgIconPath != "" {
		raw, err := os.ReadFile(cfgIconPath)
		if err != nil {
			return nil, err
		}
		png, etag, perr := ProcessIcon(raw)
		if perr != nil {
			return nil, perr
		}
		r.cfgIcon, r.cfgIconEtag = png, etag
	}
	return r, nil
}

func (r *Resolver) load(ctx context.Context) Settings {
	r.mu.RLock()
	c := r.cache
	r.mu.RUnlock()
	if c != nil {
		return *c
	}
	s, err := r.st.Get(ctx)
	if err != nil {
		// On a read error, behave as "no override" rather than failing the page.
		s = Settings{}
	}
	r.mu.Lock()
	r.cache = &s
	r.mu.Unlock()
	return s
}

// Invalidate drops the cache so the next read reloads from the store.
func (r *Resolver) Invalidate() {
	r.mu.Lock()
	r.cache = nil
	r.mu.Unlock()
}

func (r *Resolver) InstanceName(ctx context.Context) string {
	if s := r.load(ctx); s.Name != nil && *s.Name != "" {
		return *s.Name
	}
	if r.cfgName != "" {
		return r.cfgName
	}
	return defaultName
}

// Icon returns the effective icon PNG + etag, and whether it is the built-in
// default (custom=false) vs a DB/config override (custom=true).
func (r *Resolver) Icon(ctx context.Context) (pngBytes []byte, etag string, custom bool) {
	if s := r.load(ctx); len(s.IconPNG) > 0 {
		e := defaultIconEtag
		if s.IconEtag != nil {
			e = *s.IconEtag
		}
		return s.IconPNG, e, true
	}
	if len(r.cfgIcon) > 0 {
		return r.cfgIcon, r.cfgIconEtag, true
	}
	return defaultIconPNG, defaultIconEtag, false
}

func (r *Resolver) HasCustomIcon(ctx context.Context) bool {
	_, _, custom := r.Icon(ctx)
	return custom
}

// SetName upserts the DB override (nil/empty reverts to config/default) + invalidates.
func (r *Resolver) SetName(ctx context.Context, name string) error {
	var p *string
	if name != "" {
		p = &name
	}
	if err := r.st.SetName(ctx, p); err != nil {
		return err
	}
	r.Invalidate()
	return nil
}

// SetIcon processes raw → PNG, stores it + invalidates.
func (r *Resolver) SetIcon(ctx context.Context, raw []byte) error {
	out, etag, err := ProcessIcon(raw)
	if err != nil {
		return err
	}
	if err := r.st.SetIcon(ctx, out, etag); err != nil {
		return err
	}
	r.Invalidate()
	return nil
}

// ClearIcon drops the DB icon override + invalidates.
func (r *Resolver) ClearIcon(ctx context.Context) error {
	if err := r.st.ClearIcon(ctx); err != nil {
		return err
	}
	r.Invalidate()
	return nil
}

// ProcessIcon decodes raw image bytes, center-crops to square, resizes to
// 256x256, and re-encodes as PNG (broad favicon compatibility). Returns the PNG
// + a sha256 etag.
func ProcessIcon(raw []byte) (out []byte, etag string, err error) {
	if len(raw) > maxInputBytes {
		return nil, "", ErrTooLarge
	}
	src, _, derr := image.Decode(bytes.NewReader(raw))
	if derr != nil {
		return nil, "", ErrInvalidImage
	}
	b := src.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 || b.Dx() > maxInputDim || b.Dy() > maxInputDim {
		return nil, "", ErrInvalidImage
	}
	square := cropSquare(src)
	dst := image.NewRGBA(image.Rect(0, 0, iconSize, iconSize))
	draw.CatmullRom.Scale(dst, dst.Bounds(), square, square.Bounds(), draw.Over, nil)

	var buf bytes.Buffer
	if eerr := png.Encode(&buf, dst); eerr != nil {
		return nil, "", ErrInvalidImage
	}
	out = buf.Bytes()
	sum := sha256.Sum256(out)
	return out, hex.EncodeToString(sum[:]), nil
}

// cropSquare center-crops to the largest centered square (duplicated from
// pkg/avatar deliberately — branding stays PNG-only and independent).
func cropSquare(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == h {
		return src
	}
	edge := w
	if h < w {
		edge = h
	}
	x0 := b.Min.X + (w-edge)/2
	y0 := b.Min.Y + (h-edge)/2
	rect := image.Rect(x0, y0, x0+edge, y0+edge)
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	if si, ok := src.(subImager); ok {
		return si.SubImage(rect)
	}
	dst := image.NewRGBA(image.Rect(0, 0, edge, edge))
	draw.Copy(dst, image.Point{}, src, rect, draw.Src, nil)
	return dst
}
```

- [ ] **Step 6: implement `pkg/branding/store_pg.go`** (raw pgx against the singleton row):

```go
package branding

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGStore is the production store backed by the instance_settings singleton row.
type PGStore struct{ pool *pgxpool.Pool }

func NewPGStore(pool *pgxpool.Pool) *PGStore { return &PGStore{pool: pool} }

func (s *PGStore) Get(ctx context.Context) (Settings, error) {
	var out Settings
	row := s.pool.QueryRow(ctx,
		`SELECT instance_name, icon_png, icon_etag FROM instance_settings WHERE id = 1`)
	var name *string
	var icon []byte
	var etag *string
	if err := row.Scan(&name, &icon, &etag); err != nil {
		return Settings{}, err
	}
	out.Name, out.IconPNG, out.IconEtag = name, icon, etag
	return out, nil
}

func (s *PGStore) SetName(ctx context.Context, name *string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE instance_settings SET instance_name = $1, updated_at = now() WHERE id = 1`, name)
	return err
}

func (s *PGStore) SetIcon(ctx context.Context, png []byte, etag string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE instance_settings SET icon_png = $1, icon_etag = $2, updated_at = now() WHERE id = 1`, png, etag)
	return err
}

func (s *PGStore) ClearIcon(ctx context.Context) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE instance_settings SET icon_png = NULL, icon_etag = NULL, updated_at = now() WHERE id = 1`)
	return err
}
```

- [ ] **Step 7: run tests + build**

Run: `go test ./pkg/branding/... && go build -tags nodynamic ./...`
Expected: branding tests PASS; build 0.

- [ ] **Step 8: commit**

```bash
git add pkg/branding/ pkg/configx/configx.go db/migrations/017_instance_settings.sql
git commit -m "feat(branding): instance-name/icon resolver, singleton settings table, PNG icon pipeline"
```

---

### Task 2: Wire resolver into Server + public `GET /config` and `GET /branding/icon`

**Goal:** Construct the branding resolver in the server and expose the two public endpoints the SPA + favicon use.

**Files:**
- Modify: `pkg/server/server.go` (add `branding *branding.Resolver` field; construct in `NewServer`; register two public routes)
- Create: `pkg/server/handle_branding.go` (the two handlers)
- Modify: `pkg/contract/auth.go` (add `PublicConfig` view) — confirm the contract file; if views live elsewhere, follow the existing location.
- Test: `pkg/server/handle_branding_test.go`

**Acceptance Criteria:**
- [ ] `GET /api/prohibitorum/config` (public) → JSON `{instanceName, hasCustomIcon, iconUrl:"/branding/icon", iconEtag}`.
- [ ] `GET /branding/icon` (public) → `Content-Type: image/png`, body = effective icon, `ETag` set, `Cache-Control: public, max-age=300`; `If-None-Match` match → `304`.
- [ ] Default (no override) serves the embedded default PNG with etag `default`.

**Verify:** `go test ./pkg/server/ -run Branding -count=1` → ok

**Steps:**

- [ ] **Step 1: Server field + construction.** In `pkg/server/server.go`, add to the `Server` struct (near `Audit`):

```go
	branding *branding.Resolver
```

Add the import `"prohibitorum/pkg/branding"`. In `NewServer`, after `queries := db.New(conn)` and where `conn`/`config` are available, construct it (log + continue on a bad config icon rather than failing boot):

```go
	brandingResolver, berr := branding.New(config.Branding.InstanceName, config.Branding.IconPath, branding.NewPGStore(conn))
	if berr != nil {
		log.Printf("branding: config icon_path unusable (%v); using built-in default", berr)
		brandingResolver, _ = branding.New(config.Branding.InstanceName, "", branding.NewPGStore(conn))
	}
```

Set it in the `&Server{...}` literal: `branding: brandingResolver,`. (Match the file's logger — if it uses `slog`/a field logger instead of `log`, use that.)

- [ ] **Step 2: contract view.** Add to `pkg/contract/auth.go` (alongside `AuthStatus`):

```go
// PublicConfig is the unauthenticated branding payload the SPA loads at boot.
type PublicConfig struct {
	InstanceName  string `json:"instanceName"`
	HasCustomIcon bool   `json:"hasCustomIcon"`
	IconURL       string `json:"iconUrl"`
	IconEtag      string `json:"iconEtag"`
}
```

- [ ] **Step 3: write the failing test** `pkg/server/handle_branding_test.go`. Build a minimal `Server{branding: ...}` with an in-memory resolver. Since `branding.New` needs a `store`, and the fake store type is unexported in `pkg/branding`, expose a tiny test constructor OR drive via a real resolver with the default icon (no DB). Simplest: construct `r, _ := branding.New("TestCo", "", branding.NewPGStore(nil))` will nil-panic on read — so instead add an exported `branding.NewWithStore(name string, st Store)` is overkill. **Use this approach:** in `pkg/branding`, export the store interface as `Store` and add `func NewWithStore(cfgName string, st Store) *Resolver` (no config icon) for tests/wiring flexibility. Then the server test can define its own `Store` fake. Update Task 1's interface name `store`→`Store` (exported) and add `NewWithStore`. (Adjust Task 1 code: rename `store` to `Store`, keep `New(...)` as-is calling through, add `NewWithStore`.)

```go
package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"prohibitorum/pkg/branding"
)

type fakeBrandingStore struct{ name *string; icon []byte; etag *string }
func (f *fakeBrandingStore) Get(context.Context) (branding.Settings, error) {
	return branding.Settings{Name: f.name, IconPNG: f.icon, IconEtag: f.etag}, nil
}
func (f *fakeBrandingStore) SetName(context.Context, *string) error { return nil }
func (f *fakeBrandingStore) SetIcon(context.Context, []byte, string) error { return nil }
func (f *fakeBrandingStore) ClearIcon(context.Context) error { return nil }

func TestBrandingConfigEndpoint(t *testing.T) {
	s := &Server{branding: branding.NewWithStore("TestCo", &fakeBrandingStore{})}
	req := httptest.NewRequest(http.MethodGet, "/api/prohibitorum/config", nil)
	rec := httptest.NewRecorder()
	s.handleGetPublicConfigHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	if !contains(body, `"instanceName":"TestCo"`) || !contains(body, `"iconUrl":"/branding/icon"`) || !contains(body, `"hasCustomIcon":false`) {
		t.Fatalf("body: %s", body)
	}
}

func TestBrandingIconDefault_AndETag304(t *testing.T) {
	s := &Server{branding: branding.NewWithStore("TestCo", &fakeBrandingStore{})}
	// First fetch → 200 PNG with ETag.
	rec := httptest.NewRecorder()
	s.handleGetBrandingIconHTTP(rec, httptest.NewRequest(http.MethodGet, "/branding/icon", nil))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("status=%d ct=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}
	// Conditional fetch → 304.
	req := httptest.NewRequest(http.MethodGet, "/branding/icon", nil)
	req.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()
	s.handleGetBrandingIconHTTP(rec2, req)
	if rec2.Code != http.StatusNotModified {
		t.Fatalf("conditional: status %d want 304", rec2.Code)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (stringIndex(s, sub) >= 0) }
func stringIndex(s, sub string) int { return strings_Index(s, sub) }
```

(Use the stdlib `strings.Contains` directly instead of the helper shims above — they're shown only to avoid an unused import if you copy literally; prefer `strings.Contains(body, ...)`.)

- [ ] **Step 4: implement `pkg/server/handle_branding.go`:**

```go
// Package server — handle_branding.go
// Public branding endpoints: the SPA config payload and the icon image.
package server

import (
	"bytes"
	"encoding/json"
	"net/http"

	"prohibitorum/pkg/contract"
)

// GET /api/prohibitorum/config (public)
func (s *Server) handleGetPublicConfigHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, etag, _ := s.branding.Icon(ctx)
	cfg := contract.PublicConfig{
		InstanceName:  s.branding.InstanceName(ctx),
		HasCustomIcon: s.branding.HasCustomIcon(ctx),
		IconURL:       "/branding/icon",
		IconEtag:      etag,
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(cfg)
}

// GET /branding/icon (public) — serves the effective icon PNG with ETag/304.
func (s *Server) handleGetBrandingIconHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	png, etag, _ := s.branding.Icon(ctx)
	quoted := `"` + etag + `"`
	if match := r.Header.Get("If-None-Match"); match == quoted {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("ETag", quoted)
	w.Header().Set("Cache-Control", "public, max-age=300")
	http.ServeContent(w, r, "icon.png", timeZero(), bytes.NewReader(png))
}
```

Add a `timeZero()` helper or use `time.Time{}` inline: replace `http.ServeContent(... timeZero() ...)` with `w.Write(png)` after the headers if you prefer (ServeContent adds range support but needs a modtime; a plain `_, _ = w.Write(png)` is fine here since we already handle ETag/304). **Use the plain write** to keep it simple:

```go
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("ETag", quoted)
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(png)
```

- [ ] **Step 5: register routes.** In `registerOperations()` in `server.go`, next to the avatar public route (`/avatar/{subject}`):

```go
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/config", contract.AuthRequirement{Kind: contract.AuthPublic}, s.handleGetPublicConfigHTTP)
	registerOpHTTP(s.router, "GET", "/branding/icon", contract.AuthRequirement{Kind: contract.AuthPublic}, s.handleGetBrandingIconHTTP)
```

- [ ] **Step 6: run tests + build**

Run: `go test ./pkg/server/ -run Branding -count=1 && go build -tags nodynamic ./...`
Expected: PASS + build 0. (Use `strings.Contains` in the test; add `"strings"` import.)

- [ ] **Step 7: commit**

```bash
git add pkg/server/server.go pkg/server/handle_branding.go pkg/server/handle_branding_test.go pkg/contract/auth.go pkg/branding/branding.go
git commit -m "feat(server): public /config + /branding/icon endpoints (branding resolver wired in)"
```

---

### Task 3: Admin settings endpoints (name + icon, admin+sudo)

**Goal:** Admin-gated mutations that write the DB overrides, audit, and invalidate the resolver cache.

**Files:**
- Create: `pkg/server/handle_admin_settings.go`
- Modify: `pkg/server/server.go` (register three routes)
- Test: `pkg/server/handle_admin_settings_test.go`

**Acceptance Criteria:**
- [ ] `PUT /api/prohibitorum/admin/settings` (admin+sudo) `{instanceName}` → sets DB name (empty → revert), audits, invalidates. 1–64 char validation (400 on too long).
- [ ] `PUT /api/prohibitorum/admin/settings/icon` (admin role + manual fresh-sudo + ≤5 MiB raw body) → `ProcessIcon` → store, audit, invalidate. `avatar_too_large`/`avatar_invalid_image`-style 400s for bad input.
- [ ] `DELETE /api/prohibitorum/admin/settings/icon` (admin+sudo) → clears the DB icon, audits, invalidates.
- [ ] Name PUT + icon DELETE are covered by `TestAdminMutationRoutesRequireSudo` (they use `registerSudoOpHTTP`); the icon PUT enforces sudo in-handler (document why it can't use the wrapper: the wrapper rejects non-JSON + caps at 64 KiB).

**Verify:** `go test ./pkg/server/ -run 'AdminSettings|AdminMutationRoutesRequireSudo' -count=1` → ok

**Steps:**

- [ ] **Step 1: implement `pkg/server/handle_admin_settings.go`:**

```go
// Package server — handle_admin_settings.go
// Admin instance-branding overrides (name + icon). Mutations are admin-gated;
// the name + icon-delete go through registerSudoOpHTTP (JSON, small bodies). The
// icon UPLOAD is registered via registerOpHTTP(admin) + an in-handler fresh-sudo
// gate because the sudo wrapper rejects non-JSON content-types and caps bodies
// at 64 KiB — neither fits a raw image upload.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/branding"
)

const maxIconRead = 5<<20 + 1

// PUT /api/prohibitorum/admin/settings  {instanceName}
func (s *Server) handlePutInstanceNameHTTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanceName string `json:"instanceName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if len([]rune(body.InstanceName)) > 64 {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if err := s.branding.SetName(r.Context(), body.InstanceName); err != nil {
		writeAuthErr(w, err)
		return
	}
	s.auditBranding(r, "instance_name_updated")
	w.WriteHeader(http.StatusNoContent)
}

// PUT /api/prohibitorum/admin/settings/icon  (raw image body)
func (s *Server) handlePutInstanceIconHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if s.requireFreshSudo(r.Context(), w, sess) {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxIconRead))
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if err := s.branding.SetIcon(r.Context(), raw); err != nil {
		if errors.Is(err, branding.ErrTooLarge) {
			writeAvatarErr(w, "avatar_too_large", "icon: image exceeds 5 MiB")
			return
		}
		writeAvatarErr(w, "avatar_invalid_image", "icon: invalid or unsupported image format")
		return
	}
	s.auditBranding(r, "instance_icon_updated")
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/prohibitorum/admin/settings/icon
func (s *Server) handleDeleteInstanceIconHTTP(w http.ResponseWriter, r *http.Request) {
	if err := s.branding.ClearIcon(r.Context()); err != nil {
		writeAuthErr(w, err)
		return
	}
	s.auditBranding(r, "instance_icon_removed")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) auditBranding(r *http.Request, reason string) {
	var acct *int32
	if sess := authn.SessionFromContext(r.Context()); sess != nil && sess.Account != nil {
		id := sess.Account.ID
		acct = &id
	}
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: acct,
		Factor:    audit.FactorCredential,
		Event:     audit.EventUpdate,
		IP:        audit.ParseIPOrNil(sessstoreClientIPFallback(r)),
		UserAgent: r.UserAgent(),
		Detail:    map[string]any{"reason": reason},
	})
}
```

**Note for the implementer:** match the project's audit conventions — confirm `audit.FactorCredential` / `audit.EventUpdate` exist (grep `audit.Factor`/`audit.Event` constants; reuse whatever the admin-key/admin-client mutations use, e.g. the same factor/event the OIDC-client admin mutations audit with). Replace `sessstoreClientIPFallback(r)` with the project's client-IP helper (grep `ClientIP(`; the SAML/session code uses `sessstore.ClientIP(r, s.config.TrustProxy)`) — use `audit.ParseIPOrNil(r.RemoteAddr)` if simpler, matching `auditLogout` in `pkg/protocol/oidc/logout.go`.

- [ ] **Step 2: register routes** in `registerOperations()` (near the signing-keys admin block):

```go
	s.registerSudoOpHTTP(s.router, "PUT", "/api/prohibitorum/admin/settings", admin, s.handlePutInstanceNameHTTP)
	registerOpHTTP(s.router, "PUT", "/api/prohibitorum/admin/settings/icon", admin, s.handlePutInstanceIconHTTP)
	s.registerSudoOpHTTP(s.router, "DELETE", "/api/prohibitorum/admin/settings/icon", admin, s.handleDeleteInstanceIconHTTP)
```

- [ ] **Step 3: test** `pkg/server/handle_admin_settings_test.go` — reuse the branding fake store from Task 2 and an audit spy; assert: name PUT with a valid name returns 204 + the resolver name updates + an audit row; name >64 runes → 400; icon DELETE → 204. (For sudo behavior, rely on `TestAdminMutationRoutesRequireSudo` for the two wrapped routes; unit-test the handlers directly here with a session in context.) Model it on the existing `handle_admin_signing_keys_test.go` setup.

```go
func TestAdminSettings_PutName(t *testing.T) {
	st := &fakeBrandingStore{}
	s := &Server{branding: branding.NewWithStore("Cfg", st), Audit: &auditSpy{}}
	// ... attach an admin session to the request context per the existing test helper ...
	body := `{"instanceName":"Acme SSO"}`
	rec := httptest.NewRecorder()
	s.handlePutInstanceNameHTTP(rec, withAdminSession(httptest.NewRequest(http.MethodPut, "/api/prohibitorum/admin/settings", strings.NewReader(body))))
	if rec.Code != 204 {
		t.Fatalf("status %d", rec.Code)
	}
	if got := s.branding.InstanceName(context.Background()); got != "Acme SSO" {
		t.Fatalf("name not applied: %q", got)
	}
}

func TestAdminSettings_PutName_TooLong(t *testing.T) {
	s := &Server{branding: branding.NewWithStore("Cfg", &fakeBrandingStore{}), Audit: &auditSpy{}}
	long := `{"instanceName":"` + strings.Repeat("x", 65) + `"}`
	rec := httptest.NewRecorder()
	s.handlePutInstanceNameHTTP(rec, withAdminSession(httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(long))))
	if rec.Code != 400 {
		t.Fatalf("status %d want 400", rec.Code)
	}
}
```

(Grep an existing admin handler test for the `withAdminSession`/audit-spy helper names and reuse them verbatim; if none, attach `authn.Session` via `context.WithValue` with the same key the middleware uses.)

- [ ] **Step 4: run tests + build**

Run: `go test ./pkg/server/ -run 'AdminSettings|AdminMutationRoutesRequireSudo' -count=1 && go build -tags nodynamic ./...`
Expected: PASS + build 0.

- [ ] **Step 5: commit**

```bash
git add pkg/server/handle_admin_settings.go pkg/server/handle_admin_settings_test.go pkg/server/server.go
git commit -m "feat(server): admin instance-branding endpoints (name + icon, admin+sudo)"
```

---

### Task 4: webui `<title>` templating from the config instance name

**Goal:** The pre-boot `<title>` shows the configured instance name (flash-free for the config default) without coupling `pkg/webui` to the DB.

**Files:**
- Modify: `dashboard/index.html` (title placeholder)
- Modify: `pkg/webui/webui.go` (`Handler(instanceName string)`)
- Modify: `pkg/server/server.go:203` (call site)
- Modify: `pkg/webui/webui_test.go` if it exists (update `Handler()` calls) — grep first.

**Acceptance Criteria:**
- [ ] `dashboard/index.html` `<title>` is `__INSTANCE_NAME__`.
- [ ] `webui.Handler(name)` replaces `__INSTANCE_NAME__` with the HTML-escaped name in the served index.html.
- [ ] `server.go` passes `config.Branding.InstanceName`.

**Verify:** `go test ./pkg/webui/... -count=1` → ok; `go build -tags nodynamic ./...` → 0

**Steps:**

- [ ] **Step 1:** edit `dashboard/index.html` — change `<title>Prohibitorum</title>` to:

```html
    <title>__INSTANCE_NAME__</title>
```

- [ ] **Step 2:** modify `pkg/webui/webui.go` — change the signature + replace the placeholder. Add imports `"bytes"` and `"html"`:

```go
func Handler(instanceName string) http.Handler {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic("webui: dist not embedded: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		panic("webui: dist/index.html missing — run the frontend build first")
	}
	// Template the configured instance name into <title> (flash-free for the
	// config default; a DB override is corrected by the SPA on boot).
	if instanceName == "" {
		instanceName = "Prohibitorum"
	}
	index = bytes.ReplaceAll(index, []byte("__INSTANCE_NAME__"), []byte(html.EscapeString(instanceName)))
	// ... rest unchanged (the closure that serves files / index) ...
```

(The remainder of the function body is unchanged.)

- [ ] **Step 3:** update the call site `pkg/server/server.go:203`:

```go
	s.router.NotFound(webui.Handler(s.config.Branding.InstanceName).ServeHTTP)
```

- [ ] **Step 4:** if `pkg/webui/webui_test.go` exists, update any `Handler()` calls to `Handler("Prohibitorum")` and add an assertion:

```go
func TestHandler_TemplatesTitle(t *testing.T) {
	h := Handler("Acme SSO")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "<title>Acme SSO</title>") {
		t.Fatalf("title not templated: %s", rec.Body.String()[:200])
	}
}
```

(If no test file exists, create `pkg/webui/webui_test.go` with the above + imports. NOTE: this test requires `dist/index.html` to contain the `__INSTANCE_NAME__` placeholder, which only happens after the SPA is rebuilt in Task 8. Until then, the test asserts on the committed dist — so add the placeholder to the committed `pkg/webui/dist/index.html` directly in this task too, OR mark this test to run after Task 8's rebuild. Simplest: in this task, ALSO hand-edit `pkg/webui/dist/index.html`'s `<title>` to `__INSTANCE_NAME__` so the test passes now; Task 8's rebuild will regenerate it consistently from the source `index.html`.)

- [ ] **Step 5: run tests + build**

Run: `go test ./pkg/webui/... -count=1 && go build -tags nodynamic ./...`
Expected: PASS + build 0.

- [ ] **Step 6: commit**

```bash
git add dashboard/index.html pkg/webui/webui.go pkg/webui/webui_test.go pkg/server/server.go pkg/webui/dist/index.html
git commit -m "feat(webui): template configured instance name into the document <title>"
```

---

### Task 5: SPA branding store + brand marks + favicon

**Goal:** A branding store loaded at boot; the sidebar + login brand mark render the instance name and switch ShieldCheck↔custom icon; the favicon points at `/branding/icon`.

**Files:**
- Create: `dashboard/src/stores/branding.ts`
- Create: `dashboard/src/stores/branding.test.ts`
- Modify: `dashboard/src/App.vue` (load on boot)
- Modify: `dashboard/src/components/custom/AppSidebar.vue` (brand mark)
- Modify: `dashboard/src/pages/CenteredLayout.vue` (brand mark)
- Modify: `dashboard/index.html` (favicon href)
- Modify: `dashboard/src/components/custom/AppSidebar.test.ts` / `CenteredLayout` tests if they assert the literal "Prohibitorum" — grep first.

**Acceptance Criteria:**
- [ ] `useBrandingStore` has `instanceName` (default `"Prohibitorum"`), `hasCustomIcon`, `iconSrc` (`/branding/icon?v=<etag8>`), and `load()` that GETs `/api/prohibitorum/config`.
- [ ] AppSidebar + CenteredLayout show `instanceName`; render `<img :src="iconSrc">` when `hasCustomIcon`, else the styled `ShieldCheck`.
- [ ] `index.html` favicon `<link rel="icon" href="/branding/icon">`.

**Verify:** `cd dashboard && npm run test -- branding AppSidebar CenteredLayout` → PASS; `npx vue-tsc -b` → 0

**Steps:**

- [ ] **Step 1: favicon** — in `dashboard/index.html` change `<link rel="icon" href="/favicon.ico" />` to:

```html
    <link rel="icon" href="/branding/icon" />
```

- [ ] **Step 2: store** `dashboard/src/stores/branding.ts`:

```ts
/**
 * Branding store — the instance name + icon, loaded once from the public
 * /config endpoint at boot. Drives the sidebar/login brand mark + page titles.
 * Defaults keep the UI sane before load() resolves (and if it fails).
 */
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { api } from '@/lib/api'

interface PublicConfig {
  instanceName: string
  hasCustomIcon: boolean
  iconUrl: string
  iconEtag: string
}

export const useBrandingStore = defineStore('branding', () => {
  const instanceName = ref('Prohibitorum')
  const hasCustomIcon = ref(false)
  const iconEtag = ref('')

  const iconSrc = computed(() => {
    const v = iconEtag.value ? iconEtag.value.slice(0, 8) : ''
    return v ? `/branding/icon?v=${v}` : '/branding/icon'
  })

  async function load(): Promise<void> {
    try {
      const cfg = await api.get<PublicConfig>('/api/prohibitorum/config')
      if (cfg.instanceName) instanceName.value = cfg.instanceName
      hasCustomIcon.value = !!cfg.hasCustomIcon
      iconEtag.value = cfg.iconEtag ?? ''
    } catch {
      // Keep defaults — branding is non-critical.
    }
  }

  return { instanceName, hasCustomIcon, iconEtag, iconSrc, load }
})
```

- [ ] **Step 3: load at boot** — in `dashboard/src/App.vue` `<script setup>`:

```ts
import { useTheme } from '@/composables/useTheme'
import { useLocale } from '@/composables/useLocale'
import { useBrandingStore } from '@/stores/branding'
import SessionExpiredBanner from '@/components/custom/SessionExpiredBanner.vue'
useTheme()
useLocale()
void useBrandingStore().load()
```

(Template unchanged from the SessionExpiredBanner cycle: `<SessionExpiredBanner /><RouterView />`.)

- [ ] **Step 4: AppSidebar brand mark** — replace the header block in `AppSidebar.vue` (the `<div class="flex items-center gap-2.5 px-2 py-1.5">…</div>`). Add to `<script setup>`: `import { useBrandingStore } from '@/stores/branding'` and `const branding = useBrandingStore()`. Template:

```vue
      <div class="flex items-center gap-2.5 px-2 py-1.5">
        <span class="inline-flex size-8 items-center justify-center overflow-hidden rounded-md bg-ember/12 text-ember ring-1 ring-inset ring-ember/15">
          <img v-if="branding.hasCustomIcon" :src="branding.iconSrc" :alt="branding.instanceName" class="size-full object-cover" />
          <ShieldCheck v-else class="size-5" aria-hidden="true" />
        </span>
        <span class="text-base font-semibold tracking-tight text-ink">{{ branding.instanceName }}</span>
      </div>
```

- [ ] **Step 5: CenteredLayout brand mark** — in `CenteredLayout.vue` add the store import + `const branding = useBrandingStore()`, and replace the brand `<span>` + default title:

```vue
            <span class="inline-flex size-10 items-center justify-center overflow-hidden rounded-md bg-ember/10 text-ember">
              <img v-if="branding.hasCustomIcon" :src="branding.iconSrc" :alt="branding.instanceName" class="size-full object-cover" />
              <ShieldCheck v-else class="size-6" aria-hidden="true" />
            </span>
            <slot name="title">
              <span class="text-lg font-semibold tracking-tight text-ink">{{ branding.instanceName }}</span>
            </slot>
```

- [ ] **Step 6: store test** `dashboard/src/stores/branding.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useBrandingStore } from './branding'
import { api } from '@/lib/api'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))

describe('branding store', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('defaults to Prohibitorum, no custom icon', () => {
    const b = useBrandingStore()
    expect(b.instanceName).toBe('Prohibitorum')
    expect(b.hasCustomIcon).toBe(false)
    expect(b.iconSrc).toBe('/branding/icon')
  })

  it('load() populates from /config and builds a cache-busted iconSrc', async () => {
    vi.mocked(api.get).mockResolvedValue({ instanceName: 'Acme SSO', hasCustomIcon: true, iconUrl: '/branding/icon', iconEtag: 'abcdef1234' })
    const b = useBrandingStore()
    await b.load()
    expect(b.instanceName).toBe('Acme SSO')
    expect(b.hasCustomIcon).toBe(true)
    expect(b.iconSrc).toBe('/branding/icon?v=abcdef12')
  })

  it('keeps defaults if /config fails', async () => {
    vi.mocked(api.get).mockRejectedValue(new Error('net'))
    const b = useBrandingStore()
    await b.load()
    expect(b.instanceName).toBe('Prohibitorum')
  })
})
```

- [ ] **Step 7:** grep + fix any existing test asserting literal "Prohibitorum": `grep -rn "Prohibitorum" dashboard/src --include="*.test.ts"`. Update those assertions to mount with a pinia whose branding store has the expected name (or assert the default "Prohibitorum" still renders via the store default — which it does).

- [ ] **Step 8: run tests + typecheck**

Run: `cd dashboard && npm run test -- branding AppSidebar CenteredLayout && npx vue-tsc -b`
Expected: PASS + 0 type errors.

- [ ] **Step 9: commit**

```bash
git add dashboard/src/stores/branding.ts dashboard/src/stores/branding.test.ts dashboard/src/App.vue dashboard/src/components/custom/AppSidebar.vue dashboard/src/pages/CenteredLayout.vue dashboard/index.html
git commit -m "feat(spa): branding store + dynamic brand mark (name + icon) + /branding/icon favicon"
```

---

### Task 6: Dynamic page titles

**Goal:** Each route sets `document.title = <page> · <instance>` (or `<instance>` alone), driven by route meta + the branding store.

**Files:**
- Create: `dashboard/src/lib/pageTitle.ts`
- Create: `dashboard/src/lib/pageTitle.test.ts`
- Modify: `dashboard/src/router/index.ts` (add `meta.titleKey` to routes + an `afterEach` guard)
- Modify: `dashboard/src/locales/en.ts` + `zh.ts` (`title.*` keys)

**Acceptance Criteria:**
- [ ] `buildTitle(pageName, instanceName)` returns `\`${pageName} · ${instanceName}\`` when pageName is non-empty, else `instanceName`.
- [ ] Every route has a `meta.titleKey` (threshold + dashboard + admin); the `afterEach` sets `document.title`.
- [ ] `title.*` keys exist in en + zh (parity tests pass).

**Verify:** `cd dashboard && npm run test -- pageTitle locales` → PASS; `npx vue-tsc -b` → 0

**Steps:**

- [ ] **Step 1: failing test** `dashboard/src/lib/pageTitle.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { buildTitle } from './pageTitle'

describe('buildTitle', () => {
  it('combines page and instance', () => {
    expect(buildTitle('Security', 'Acme SSO')).toBe('Security · Acme SSO')
  })
  it('returns instance alone when no page', () => {
    expect(buildTitle('', 'Acme SSO')).toBe('Acme SSO')
  })
})
```

- [ ] **Step 2: implement** `dashboard/src/lib/pageTitle.ts`:

```ts
/** buildTitle composes the document title: "<page> · <instance>", or just the
 * instance name when a route has no page title. */
export function buildTitle(pageName: string, instanceName: string): string {
  return pageName ? `${pageName} · ${instanceName}` : instanceName
}
```

- [ ] **Step 3: router meta + guard.** Extend the `RouteMeta` augmentation in `router/index.ts` with `titleKey?: string`. Add `meta: { titleKey: 'title.<x>' }` to every route (merge with existing meta). Examples — threshold: `login`→`title.login`, `consent`→`title.consent`, `logout`→`title.logout`, `error`→`title.error`, `enroll`→`title.enroll`, `pair`→`title.pair`, `welcome`→`title.welcome`. Dashboard: `security`→`title.security`, `sessions`→`title.sessions`, `connected`→`title.connected`, `devices`→`title.devices`. Admin: `admin-accounts`→`title.adminAccounts`, `admin-account-detail`→`title.adminAccountDetail`, `admin-invitations`→`title.adminInvitations`, `admin-oidc-applications`→`title.adminOidcApplications`, `admin-oidc-application-detail`→`title.adminOidcApplicationDetail`, `admin-saml-applications`→`title.adminSamlApplications`, `admin-saml-application-detail`→`title.adminSamlApplicationDetail`, `admin-identity-providers`→`title.adminIdentityProviders`, `admin-identity-provider-detail`→`title.adminIdentityProviderDetail`, `admin-signing-keys`→`title.adminSigningKeys`, `admin-audit`→`title.adminAudit`, `admin-groups`→`title.adminGroups`, `admin-group-detail`→`title.adminGroupDetail`, and (Task 7) `admin-settings`→`title.adminSettings`. The root redirect (`path:''`) needs none.

Add the guard at the bottom of `installGuard` (or as a separate `afterEach`), reading the store + i18n lazily to avoid init cycles:

```ts
import { buildTitle } from '@/lib/pageTitle'

// ... inside installGuard(router) or right after createRouter:
router.afterEach((to) => {
  void (async () => {
    const { useBrandingStore } = await import('@/stores/branding')
    const { i18n } = await import('@/i18n')
    const { getActivePinia } = await import('pinia')
    const pinia = getActivePinia()
    const name = pinia ? useBrandingStore(pinia).instanceName : 'Prohibitorum'
    const key = to.meta.titleKey
    // i18n global t: i18n.global.t
    const page = key ? i18n.global.t(key as string) : ''
    document.title = buildTitle(page, name)
  })()
})
```

(Confirm the i18n export shape — `src/i18n.ts` exports `i18n`; use `i18n.global.t`. If the project exposes a different accessor, match it.)

- [ ] **Step 4: i18n `title.*`.** Add a `title` block to `en.ts` (no apostrophes → single quotes fine):

```ts
  title: {
    login: 'Sign in',
    consent: 'Authorize',
    logout: 'Signed out',
    error: 'Error',
    enroll: 'Set up your account',
    pair: 'Pair a device',
    welcome: 'Welcome',
    security: 'Security',
    sessions: 'Sessions',
    connected: 'Connected accounts',
    devices: 'Devices',
    adminAccounts: 'Accounts',
    adminAccountDetail: 'Account',
    adminInvitations: 'Invitations',
    adminOidcApplications: 'OIDC applications',
    adminOidcApplicationDetail: 'OIDC application',
    adminSamlApplications: 'SAML applications',
    adminSamlApplicationDetail: 'SAML application',
    adminIdentityProviders: 'Identity providers',
    adminIdentityProviderDetail: 'Identity provider',
    adminSigningKeys: 'Signing keys',
    adminAudit: 'Audit log',
    adminGroups: 'Groups',
    adminGroupDetail: 'Group',
    adminSettings: 'Settings',
  },
```

Add the SAME keys to `zh.ts` with Chinese values (e.g. `login: '登录'`, `security: '安全'`, `sessions: '会话'`, `connected: '已连接账户'`, `devices: '设备'`, `adminAccounts: '账户'`, `adminSettings: '设置'`, … — translate each consistently with existing `nav.*`/`admin.nav.*` terms). After editing, run `grep -nP "\x{2019}" dashboard/src/locales/en.ts` → must be empty.

- [ ] **Step 5: run tests + typecheck**

Run: `cd dashboard && npm run test -- pageTitle locales && npx vue-tsc -b`
Expected: PASS (incl. locale parity) + 0 type errors.

- [ ] **Step 6: commit**

```bash
git add dashboard/src/lib/pageTitle.ts dashboard/src/lib/pageTitle.test.ts dashboard/src/router/index.ts dashboard/src/locales/en.ts dashboard/src/locales/zh.ts
git commit -m "feat(spa): dynamic per-page document titles (<page> · <instance>)"
```

---

### Task 7: Admin SettingsView (name + icon UI)

**Goal:** An admin page to edit the instance name and upload/remove the icon.

**Files:**
- Create: `dashboard/src/pages/admin/SettingsView.vue`
- Create: `dashboard/src/pages/admin/SettingsView.test.ts`
- Modify: `dashboard/src/router/index.ts` (route `admin-settings`)
- Modify: `dashboard/src/components/custom/AppSidebar.vue` (Admin-group Settings item)
- Modify: `dashboard/src/locales/en.ts` + `zh.ts` (`admin.settings.*`)

**Acceptance Criteria:**
- [ ] `/admin/settings` (requiresAdmin) renders a name field (save → `withSudo(PUT /admin/settings)`) and an icon section (upload → `withSudo(PUT /admin/settings/icon)`; Remove → `withSudo(DELETE /admin/settings/icon)`, shown only when a custom icon exists).
- [ ] Saving refreshes the branding store (`useBrandingStore().load()`) so the sidebar/title update live.
- [ ] Sidebar Admin group has a Settings item (admin-gated).
- [ ] `admin.settings.*` keys in en + zh.

**Verify:** `cd dashboard && npm run test -- SettingsView locales` → PASS; `npx vue-tsc -b` → 0

**Steps:**

- [ ] **Step 1: route.** In `router/index.ts`, add under the DashboardLayout children:

```ts
      { path: 'admin/settings', name: 'admin-settings', component: () => import('../pages/admin/SettingsView.vue'), meta: { requiresAdmin: true, titleKey: 'title.adminSettings' } },
```

- [ ] **Step 2: sidebar item.** In `AppSidebar.vue`, add to the admin nav items array (with a `Settings` lucide icon import):

```ts
  { to: '/admin/settings', label: t('admin.nav.settings'), icon: Settings },
```

- [ ] **Step 3: i18n.** Add `nav.settings`-style key under `admin.nav` (`settings: 'Settings'`), and an `admin.settings` block in en.ts:

```ts
    settings: {
      title: 'Instance settings',
      help: 'Customize how this instance is named and branded.',
      nameLabel: 'Instance name',
      nameHint: 'Shown in the sidebar, the sign-in page, browser tabs, and page titles.',
      save: 'Save',
      saved: 'Saved',
      iconLabel: 'Icon',
      iconHint: 'A square image (PNG, JPEG, or WebP). Used for the browser tab and the in-app mark.',
      upload: 'Upload icon',
      remove: 'Remove icon',
      uploadError: 'That image could not be used. Try a different square PNG or JPEG.',
    },
```

Add the SAME keys to `zh.ts` (translated). Grep-verify no U+2019 in en.ts.

- [ ] **Step 4: failing test** `dashboard/src/pages/admin/SettingsView.test.ts` — mount with i18n + pinia; mock `withSudo` to pass through; assert: typing a name + Save calls `api.put('/api/prohibitorum/admin/settings', {instanceName})` and then `branding.load()`; Remove (shown when `hasCustomIcon`) calls `api.del('/api/prohibitorum/admin/settings/icon')`. Follow the existing admin-view test idioms (e.g. `AdminSigningKeysView.test.ts`) for mocking `@/lib/sudo` + `@/lib/api`.

```ts
// sketch — match the repo's admin test harness
it('saves the instance name and refreshes branding', async () => {
  // mock api.put resolved; mount; set name input; click Save
  // expect api.put called with ('/api/prohibitorum/admin/settings', { instanceName: 'Acme SSO' })
})
```

- [ ] **Step 5: implement** `dashboard/src/pages/admin/SettingsView.vue`:

```vue
<script setup lang="ts">
/** SettingsView (/admin/settings) — edit instance name + icon (admin + sudo). */
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { useBrandingStore } from '@/stores/branding'
import { useTransientFlag } from '@/composables/useTransientFlag'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import FormSection from '@/components/custom/FormSection.vue'
import UserAvatar from '@/components/custom/UserAvatar.vue' // or a plain <img> preview

const { t } = useI18n()
const { busy, run, errorText } = useApi()
const branding = useBrandingStore()
const name = ref(branding.instanceName)
const saved = useTransientFlag()
const fileInput = ref<HTMLInputElement | null>(null)
const uploadError = ref('')

onMounted(() => { name.value = branding.instanceName })

async function saveName(): Promise<void> {
  const ok = await run(() => withSudo(async () => {
    await api.put('/api/prohibitorum/admin/settings', { instanceName: name.value })
    return true as const
  }))
  if (ok) { await branding.load(); saved.trigger() }
}

async function onPickFile(e: Event): Promise<void> {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  uploadError.value = ''
  const ok = await run(() => withSudo(async () => {
    await api.upload('/api/prohibitorum/admin/settings/icon', file)
    return true as const
  }))
  if (ok) { await branding.load() } else { uploadError.value = t('admin.settings.uploadError') }
  if (fileInput.value) fileInput.value.value = ''
}

async function removeIcon(): Promise<void> {
  const ok = await run(() => withSudo(async () => {
    await api.del('/api/prohibitorum/admin/settings/icon')
    return true as const
  }))
  if (ok) await branding.load()
}
</script>

<template>
  <div class="flex max-w-xl flex-col gap-6">
    <div class="flex flex-col gap-1">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('admin.settings.title') }}</h1>
      <p class="text-sm text-muted">{{ t('admin.settings.help') }}</p>
    </div>

    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>

    <Card>
      <CardHeader><CardTitle>{{ t('admin.settings.nameLabel') }}</CardTitle></CardHeader>
      <CardContent class="flex flex-col gap-3">
        <FormSection>
          <div class="flex flex-col gap-1.5">
            <Label for="instance-name">{{ t('admin.settings.nameLabel') }}</Label>
            <Input id="instance-name" v-model="name" :disabled="busy" maxlength="64" />
            <p class="text-xs text-muted">{{ t('admin.settings.nameHint') }}</p>
          </div>
          <div class="flex items-center gap-3">
            <Button :disabled="busy" @click="saveName">{{ t('admin.settings.save') }}</Button>
            <span v-if="saved.active.value" class="text-sm text-success-foreground" role="status">{{ t('admin.settings.saved') }}</span>
          </div>
        </FormSection>
      </CardContent>
    </Card>

    <Card>
      <CardHeader><CardTitle>{{ t('admin.settings.iconLabel') }}</CardTitle></CardHeader>
      <CardContent class="flex flex-col gap-3">
        <div class="flex items-center gap-4">
          <span class="inline-flex size-12 items-center justify-center overflow-hidden rounded-md bg-ember/10 ring-1 ring-inset ring-border">
            <img :src="branding.iconSrc" :alt="branding.instanceName" class="size-full object-cover" />
          </span>
          <div class="flex flex-col gap-2">
            <p class="text-xs text-muted">{{ t('admin.settings.iconHint') }}</p>
            <div class="flex gap-2">
              <input ref="fileInput" type="file" accept="image/png,image/jpeg,image/webp" class="hidden" @change="onPickFile" />
              <Button variant="outline" size="sm" :disabled="busy" @click="fileInput?.click()">{{ t('admin.settings.upload') }}</Button>
              <Button v-if="branding.hasCustomIcon" variant="outline" size="sm" :disabled="busy" @click="removeIcon">{{ t('admin.settings.remove') }}</Button>
            </div>
            <p v-if="uploadError" class="text-sm text-danger-foreground" role="alert">{{ uploadError }}</p>
          </div>
        </div>
      </CardContent>
    </Card>
  </div>
</template>
```

(Confirm component import paths + the `useTransientFlag` API shape against the codebase — grep `useTransientFlag` for its returned shape; adjust `saved.active.value`/`saved.trigger()` to match. `api.upload` exists and sends a Blob/File body.)

- [ ] **Step 6: run tests + typecheck**

Run: `cd dashboard && npm run test -- SettingsView locales && npx vue-tsc -b`
Expected: PASS + 0 type errors.

- [ ] **Step 7: commit**

```bash
git add dashboard/src/pages/admin/SettingsView.vue dashboard/src/pages/admin/SettingsView.test.ts dashboard/src/router/index.ts dashboard/src/components/custom/AppSidebar.vue dashboard/src/locales/en.ts dashboard/src/locales/zh.ts
git commit -m "feat(spa): admin Settings page — edit instance name + upload/remove icon"
```

---

### Task 8: Gate + runtime verification + dist rebuild

**Goal:** Full gate green, runtime-verified in chromium, embedded SPA rebuilt + committed; optional smoke assertion.

**Files:**
- Modify: `pkg/webui/dist/*` (rebuilt)
- Optional: the smoke (`cmd/smoke/main.go`) — a `/config` + `/branding/icon` assertion.

**Acceptance Criteria:**
- [ ] Backend gate: `go vet ./... && go build -tags nodynamic ./... && go test ./...` all 0 (migration 017 applies; branding + endpoint tests pass).
- [ ] Frontend gate: `npm run test` green, `npx vue-tsc -b` 0, `node scripts/check-contrast.mjs` passes.
- [ ] `pkg/webui/dist` rebuilt (placeholder `__INSTANCE_NAME__` present in `dist/index.html`) and committed; `ci:frontend` drift check passes.
- [ ] Runtime (chromium via subagent server): tab title differs per page (`Security · Prohibitorum`, `Sign in · Prohibitorum`); `/branding/icon` returns a PNG; PUT a custom name + icon via the API → sidebar/login text + favicon + brand `<img>` reflect it; document title uses the new name.

**Verify:** commands below all exit 0; runtime checklist captured.

**Steps:**

- [ ] **Step 1: backend gate**

```bash
go vet ./... && go build -tags nodynamic ./... && go test ./...
```
Expected: 0 / 0 / all `ok` (`pkg/server` may flake under parallel shared-DB — re-run in isolation if needed).

- [ ] **Step 2: frontend gate + dist rebuild**

```bash
cd dashboard && npm run test && npx vue-tsc -b && node scripts/check-contrast.mjs && npm run build
cd .. && git add pkg/webui/dist && git commit -m "build(webui): rebuild embedded SPA for instance branding + page titles"
```
Confirm `dist/index.html` contains `<title>__INSTANCE_NAME__</title>` (the source placeholder carried through Vite). Re-run `go test ./pkg/webui/...` after the rebuild to confirm the title-templating test passes against the freshly built dist.

- [ ] **Step 3: runtime verification (subagent-launched server — the controller's own Bash servers get killed; see conventions).** Build `/tmp/proh-verify` from HEAD, `source scripts/dev-env.sh`, `export PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK=true`, run on :8080. Then:
  - `curl -s http://localhost:8080/api/prohibitorum/config` → `{"instanceName":"Prohibitorum","hasCustomIcon":false,...}`.
  - `curl -sI http://localhost:8080/branding/icon` → `200`, `Content-Type: image/png`, an `ETag`.
  - `chromium --headless=new --no-sandbox --dump-dom http://localhost:8080/login` → DOM contains `<title>` and the brand text; check tab title via `--dump-dom` includes the instance name.
  - With an admin session (or directly against the DB via the resolver), set a custom name + icon, then re-fetch `/config` (custom name + `hasCustomIcon:true`) and `--dump-dom /security` to confirm the new name appears. (Authed flows need a session; for an unauthenticated smoke, verifying `/config` + `/branding/icon` + the threshold-page title is sufficient evidence; note any authed-only check deferred.)
  - Capture results; tear down the server.

- [ ] **Step 4: optional smoke assertion.** If extending `cmd/smoke/main.go`, add a step asserting `GET /api/prohibitorum/config` returns 200 with `"instanceName"` and `GET /branding/icon` returns 200 `image/png`. Re-run `mise run ci:smoke` → `SMOKE_EXIT=0`.

- [ ] **Step 5: final gate sanity**

```bash
go build -tags nodynamic ./... && go test ./... && cd dashboard && npm run test && npx vue-tsc -b
```
Expected: all green.

---

## Self-Review Notes

- **Spec coverage:** §A config+storage → Task 1 (configx + migration 017 + resolver). §B icon pipeline+serve → Task 1 (`ProcessIcon`, default icon) + Task 2 (`/branding/icon`). §C public delivery+index.html → Task 2 (`/config`) + Task 4 (title templating) + Task 5 (favicon href + store). §D dynamic titles → Task 6. §E admin Settings → Task 3 (endpoints) + Task 7 (UI). Testing → each task + Task 8 (gate+runtime). Non-goals respected (name stays separate from RP/issuer; single PNG; no cropper).
- **Deviation from spec (documented):** the spec's separate admin `GET /admin/settings` is omitted — SettingsView reads current values from the public `/config` store (which already carries `instanceName` + `hasCustomIcon`); the "source" hint is dropped (YAGNI). No functional loss.
- **Key constraint baked in:** the icon UPLOAD uses `registerOpHTTP(admin)` + in-handler `requireFreshSudo` + a 5 MiB cap because `registerSudoOpHTTP`/`withFreshSudo` reject non-JSON bodies and cap at 64 KiB (Task 3). Name PUT + icon DELETE use the standard sudo wrapper.
- **Type consistency:** `branding.Resolver` methods (`InstanceName`/`Icon`/`HasCustomIcon`/`SetName`/`SetIcon`/`ClearIcon`/`Invalidate`), the exported `Store` interface + `Settings` struct + `NewWithStore` (Task 1, used by the server tests in Tasks 2–3), `contract.PublicConfig` (Tasks 2,5), `useBrandingStore` (`instanceName`/`hasCustomIcon`/`iconSrc`/`load`, Tasks 5–7), and `buildTitle` (Task 6) are referenced consistently. NOTE: Task 1 must export the store interface as `Store` and the row struct as `Settings`, and add `NewWithStore(cfgName, st)` — the server tests depend on these exported names.
