// Package branding resolves the effective instance name + icon with
// DB-override → config-default → built-in precedence, and processes uploaded
// icons to a square PNG. The resolver caches the DB row; admin mutations call
// Invalidate() so changes apply immediately.
package branding

import (
	"context"
	"os"
	"sync"

	"prohibitorum/pkg/imageutil"
)

const defaultName = "Prohibitorum"

// Errors are re-exported from imageutil so callers can keep matching on
// branding.ErrTooLarge / branding.ErrInvalidImage.
var (
	ErrTooLarge     = imageutil.ErrTooLarge
	ErrInvalidImage = imageutil.ErrInvalidImage
)

// Settings is the raw DB-override row (nil fields = no override). Exported so
// the server package's tests/wiring can build a Store fake.
type Settings struct {
	Name               *string
	IconPNG            []byte
	IconEtag           *string
	Maintenance        bool
	MaintenanceMessage *string
	LoginBG            []byte
	LoginBGEtag        *string
}

// Store is the persistence seam (real impl in store_pg.go; fakes in tests).
type Store interface {
	Get(ctx context.Context) (Settings, error)
	SetName(ctx context.Context, name *string) error
	SetIcon(ctx context.Context, png []byte, etag string) error
	ClearIcon(ctx context.Context) error
	SetMaintenance(ctx context.Context, on bool, message *string) error
	SetLoginBG(ctx context.Context, raw []byte, etag string) error
	ClearLoginBG(ctx context.Context) error
}

// Resolver resolves the effective instance name and icon with DB → config →
// built-in precedence. The DB row is cached after the first load; call
// Invalidate() after any admin mutation.
type Resolver struct {
	cfgName     string
	cfgIcon     []byte // processed config-file icon (nil if none)
	cfgIconEtag string
	st          Store

	mu    sync.RWMutex
	cache *Settings // nil = not loaded
}

// NewWithStore builds a resolver with no config-file icon (tests + simple wiring).
func NewWithStore(cfgName string, st Store) *Resolver {
	return &Resolver{cfgName: cfgName, st: st}
}

// New builds the resolver. cfgIconPath (optional) is read + processed once; a
// missing/invalid file returns an error for the caller to handle.
func New(cfgName, cfgIconPath string, st Store) (*Resolver, error) {
	r := &Resolver{cfgName: cfgName, st: st}
	if cfgIconPath != "" {
		raw, err := os.ReadFile(cfgIconPath)
		if err != nil {
			return nil, err
		}
		out, etag, perr := ProcessIcon(raw)
		if perr != nil {
			return nil, perr
		}
		r.cfgIcon, r.cfgIconEtag = out, etag
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
		s = Settings{} // read error → behave as "no override" rather than fail the page
	}
	r.mu.Lock()
	r.cache = &s
	r.mu.Unlock()
	return s
}

// Invalidate clears the cached DB row, forcing the next read to reload.
func (r *Resolver) Invalidate() {
	r.mu.Lock()
	r.cache = nil
	r.mu.Unlock()
}

// InstanceName returns the effective instance name: DB override → config → built-in default.
func (r *Resolver) InstanceName(ctx context.Context) string {
	if s := r.load(ctx); s.Name != nil && *s.Name != "" {
		return *s.Name
	}
	if r.cfgName != "" {
		return r.cfgName
	}
	return defaultName
}

// Icon returns the effective icon PNG + etag, and whether it is a DB/config
// override (custom=true) vs the built-in default (custom=false).
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

// HasCustomIcon returns true when the icon is a DB or config-file override
// (as opposed to the built-in default).
func (r *Resolver) HasCustomIcon(ctx context.Context) bool {
	_, _, custom := r.Icon(ctx)
	return custom
}

// Maintenance reports whether maintenance mode is on and the optional
// admin-authored message. Cached with the rest of the singleton row, so this is
// a free read on the hot path until the next admin toggle invalidates it.
func (r *Resolver) Maintenance(ctx context.Context) (on bool, message string) {
	s := r.load(ctx)
	if s.MaintenanceMessage != nil {
		message = *s.MaintenanceMessage
	}
	return s.Maintenance, message
}

// SetMaintenance toggles maintenance mode (and the optional message) and
// invalidates the cache so the change applies immediately. An empty message
// clears the stored note.
func (r *Resolver) SetMaintenance(ctx context.Context, on bool, message string) error {
	var p *string
	if message != "" {
		p = &message
	}
	if err := r.st.SetMaintenance(ctx, on, p); err != nil {
		return err
	}
	r.Invalidate()
	return nil
}

// SetName updates the DB override and invalidates the cache. Pass an empty
// string to clear the override (falls back to config/default).
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

// SetIcon processes raw image bytes and stores the result as the DB override.
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

// ClearIcon removes the DB icon override and invalidates the cache.
func (r *Resolver) ClearIcon(ctx context.Context) error {
	if err := r.st.ClearIcon(ctx); err != nil {
		return err
	}
	r.Invalidate()
	return nil
}

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

// ProcessIcon normalizes raw to a 512×512 lossless WebP + sha256 etag, sharing
// the exact pipeline with avatars via pkg/imageutil. Lossless keeps a
// transparent logo's edges crisp. Used where no backdrop accent is needed (the
// instance icon). For app/entity icons use ProcessIconWithAccent, which derives
// the accent from the same decode.
func ProcessIcon(raw []byte) (out []byte, etag string, err error) {
	return imageutil.ProcessSquareWebP(raw, imageutil.Size, true)
}
