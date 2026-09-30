// Package branding resolves the effective instance name + icon with
// DB-override → config-default → built-in precedence, processes uploaded icons
// to a square PNG, and holds the sign-in page's appearance and background
// images. The resolver caches the DB row; admin mutations call Invalidate() so
// changes apply immediately.
package branding

import (
	"context"
	"errors"
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
	// Appearance is the saved sign-in page look; nil means DefaultAppearance.
	Appearance *Appearance
	// Images are the uploaded sign-in backgrounds in upload order, without bytes.
	Images []ImageRef
	// UnsplashKey is the sealed Unsplash access key, nil when none is saved.
	UnsplashKey *SealedKey
}

// ImageRef identifies an uploaded sign-in background.
type ImageRef struct {
	ID   int64
	Etag string
}

// SealedKey is an AES-GCM sealed secret as stored in instance_settings.
type SealedKey struct {
	Ciphertext []byte
	Nonce      []byte
	KeyVersion int32
}

// Store is the persistence seam (real impl in store_pg.go; fakes in tests).
type Store interface {
	Get(ctx context.Context) (Settings, error)
	SetName(ctx context.Context, name *string) error
	SetIcon(ctx context.Context, png []byte, etag string) error
	ClearIcon(ctx context.Context) error
	SetMaintenance(ctx context.Context, on bool, message *string) error
	// SetAppearance saves a and, when key is non-nil, the Unsplash key in the
	// same transaction. It returns ErrUnsplashKeyRequired when a selects
	// Unsplash and neither key nor a saved key exists.
	SetAppearance(ctx context.Context, a Appearance, key *SealedKey) error
	// ClearUnsplashKey returns ErrUnsplashKeyInUse while the saved source is Unsplash.
	ClearUnsplashKey(ctx context.Context) error
	// AddImage returns ErrImageLimit once MaxLoginImages are stored.
	AddImage(ctx context.Context, data []byte, etag string) (ImageRef, error)
	// DeleteImage returns ErrImageNotFound for an unknown id.
	DeleteImage(ctx context.Context, id int64) error
	// ImageData returns ErrImageNotFound for an unknown id.
	ImageData(ctx context.Context, id int64) ([]byte, string, error)
}

// Sign-in page store errors.
var (
	ErrImageLimit          = errors.New("branding: sign-in background image limit reached")
	ErrImageNotFound       = errors.New("branding: sign-in background image not found")
	ErrUnsplashKeyRequired = errors.New("branding: Unsplash source needs an access key")
	ErrUnsplashKeyInUse    = errors.New("branding: Unsplash access key is in use")
)

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

// Appearance returns the saved sign-in page look, or DefaultAppearance.
func (r *Resolver) Appearance(ctx context.Context) Appearance {
	if s := r.load(ctx); s.Appearance != nil {
		return *s.Appearance
	}
	return DefaultAppearance()
}

// LoginImages lists the uploaded sign-in backgrounds in upload order.
func (r *Resolver) LoginImages(ctx context.Context) []ImageRef {
	return r.load(ctx).Images
}

// LoginImageEtag returns the cached etag of image id, without reading its bytes.
func (r *Resolver) LoginImageEtag(ctx context.Context, id int64) (string, bool) {
	for _, img := range r.LoginImages(ctx) {
		if img.ID == id {
			return img.Etag, true
		}
	}
	return "", false
}

// LoginImage reads the bytes of image id, verbatim as uploaded.
func (r *Resolver) LoginImage(ctx context.Context, id int64) ([]byte, string, error) {
	return r.st.ImageData(ctx, id)
}

// UnsplashKey returns the sealed Unsplash access key, or nil when none is saved.
func (r *Resolver) UnsplashKey(ctx context.Context) *SealedKey {
	return r.load(ctx).UnsplashKey
}

// SetAppearance validates and saves a, together with key when it is non-nil.
func (r *Resolver) SetAppearance(ctx context.Context, a Appearance, key *SealedKey) error {
	if err := ValidateAppearance(a); err != nil {
		return err
	}
	defer r.Invalidate()
	return r.st.SetAppearance(ctx, a, key)
}

// ClearUnsplashKey removes the saved Unsplash key unless it is in use.
func (r *Resolver) ClearUnsplashKey(ctx context.Context) error {
	defer r.Invalidate()
	return r.st.ClearUnsplashKey(ctx)
}

// AddLoginImage validates raw (size, format, dimensions) without modifying it
// and stores the exact bytes; the public endpoint serves them byte-for-byte.
func (r *Resolver) AddLoginImage(ctx context.Context, raw []byte) (ImageRef, error) {
	etag, err := imageutil.ValidateRaw(raw)
	if err != nil {
		return ImageRef{}, err
	}
	defer r.Invalidate()
	return r.st.AddImage(ctx, raw, etag)
}

// DeleteLoginImage removes image id.
func (r *Resolver) DeleteLoginImage(ctx context.Context, id int64) error {
	defer r.Invalidate()
	return r.st.DeleteImage(ctx, id)
}

// ProcessIcon normalizes raw to a 512×512 lossless WebP + sha256 etag, sharing
// the exact pipeline with avatars via pkg/imageutil. Lossless keeps a
// transparent logo's edges crisp. Used where no backdrop accent is needed (the
// instance icon). For app/entity icons use ProcessIconWithAccent, which derives
// the accent from the same decode.
func ProcessIcon(raw []byte) (out []byte, etag string, err error) {
	return imageutil.ProcessSquareWebP(raw, imageutil.Size, true)
}
