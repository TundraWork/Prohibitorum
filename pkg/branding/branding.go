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
type Store interface {
	Get(ctx context.Context) (Settings, error)
	SetName(ctx context.Context, name *string) error
	SetIcon(ctx context.Context, png []byte, etag string) error
	ClearIcon(ctx context.Context) error
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

// ProcessIcon decodes raw image bytes, center-crops to square, resizes to
// 256x256, and re-encodes as PNG. Returns the PNG + a sha256 etag.
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
