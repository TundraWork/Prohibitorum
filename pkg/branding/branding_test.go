package branding

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"testing"
)

type fakeStore struct {
	name       *string
	iconPNG    []byte
	iconEtag   *string
	maint      bool
	maintMsg   *string
	appearance *Appearance
	key        *SealedKey
	images     []fakeImage
	nextID     int64
	gets       int
}

type fakeImage struct {
	ref  ImageRef
	data []byte
}

func (f *fakeStore) Get(context.Context) (Settings, error) {
	f.gets++
	var refs []ImageRef
	for _, img := range f.images {
		refs = append(refs, img.ref)
	}
	return Settings{
		Name: f.name, IconPNG: f.iconPNG, IconEtag: f.iconEtag,
		Maintenance: f.maint, MaintenanceMessage: f.maintMsg,
		Appearance: f.appearance, Images: refs, UnsplashKey: f.key,
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
func (f *fakeStore) SetAppearance(_ context.Context, a Appearance, key *SealedKey) error {
	if key != nil {
		f.key = key
	} else if a.Background.Source == SourceUnsplash && f.key == nil {
		return ErrUnsplashKeyRequired
	}
	f.appearance = &a
	return nil
}
func (f *fakeStore) ClearUnsplashKey(context.Context) error {
	if f.appearance != nil && f.appearance.Background.Source == SourceUnsplash {
		return ErrUnsplashKeyInUse
	}
	f.key = nil
	return nil
}
func (f *fakeStore) AddImage(_ context.Context, data []byte, etag string) (ImageRef, error) {
	if len(f.images) >= MaxLoginImages {
		return ImageRef{}, ErrImageLimit
	}
	f.nextID++
	ref := ImageRef{ID: f.nextID, Etag: etag}
	f.images = append(f.images, fakeImage{ref: ref, data: data})
	return ref, nil
}
func (f *fakeStore) DeleteImage(_ context.Context, id int64) error {
	for i, img := range f.images {
		if img.ref.ID == id {
			f.images = append(f.images[:i], f.images[i+1:]...)
			return nil
		}
	}
	return ErrImageNotFound
}
func (f *fakeStore) ImageData(_ context.Context, id int64) ([]byte, string, error) {
	for _, img := range f.images {
		if img.ref.ID == id {
			return img.data, img.ref.Etag, nil
		}
	}
	return nil, "", ErrImageNotFound
}

func strp(s string) *string { return &s }

// TestMaintenance_RoundTrip verifies SetMaintenance persists + invalidates the
// cache so Maintenance reflects the new state (off by default).
func TestMaintenance_RoundTrip(t *testing.T) {
	ctx := context.Background()
	r := NewWithStore("X", &fakeStore{})
	if on, _ := r.Maintenance(ctx); on {
		t.Fatal("default maintenance should be off")
	}
	if err := r.SetMaintenance(ctx, true, "Down for upgrade"); err != nil {
		t.Fatalf("SetMaintenance: %v", err)
	}
	if on, msg := r.Maintenance(ctx); !on || msg != "Down for upgrade" {
		t.Errorf("Maintenance = (%v,%q), want (true,Down for upgrade)", on, msg)
	}
}

func TestInstanceName_Precedence(t *testing.T) {
	ctx := context.Background()
	r := NewWithStore("ConfigName", &fakeStore{name: strp("DBName")})
	if got := r.InstanceName(ctx); got != "DBName" {
		t.Fatalf("DB override: got %q want DBName", got)
	}
	r = NewWithStore("ConfigName", &fakeStore{})
	if got := r.InstanceName(ctx); got != "ConfigName" {
		t.Fatalf("config: got %q want ConfigName", got)
	}
	r = NewWithStore("", &fakeStore{})
	if got := r.InstanceName(ctx); got != "Prohibitorum" {
		t.Fatalf("default: got %q want Prohibitorum", got)
	}
}

func TestIcon_Precedence_And_HasCustom(t *testing.T) {
	ctx := context.Background()
	r := NewWithStore("X", &fakeStore{})
	png0, etag0, _ := r.Icon(ctx)
	if len(png0) == 0 || etag0 != defaultIconEtag {
		t.Fatalf("default icon: len=%d etag=%q", len(png0), etag0)
	}
	if r.HasCustomIcon(ctx) {
		t.Fatal("HasCustomIcon should be false with no DB/config icon")
	}
	r = NewWithStore("X", &fakeStore{iconPNG: []byte("PNGBYTES"), iconEtag: strp("abc")})
	png1, etag1, _ := r.Icon(ctx)
	if string(png1) != "PNGBYTES" || etag1 != "abc" {
		t.Fatalf("db icon: %q %q", png1, etag1)
	}
	if !r.HasCustomIcon(ctx) {
		t.Fatal("HasCustomIcon should be true with a DB icon")
	}
}

func TestProcessIcon_WebP512Square(t *testing.T) {
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
	if derr != nil || format != "webp" {
		t.Fatalf("decode: format=%q err=%v", format, derr)
	}
	if b := img.Bounds(); b.Dx() != 512 || b.Dy() != 512 {
		t.Fatalf("size: %dx%d want 512x512", b.Dx(), b.Dy())
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
	r := NewWithStore("Cfg", fs)
	_ = r.InstanceName(ctx)
	fs.name = strp("Second")
	if got := r.InstanceName(ctx); got != "First" {
		t.Fatalf("pre-invalidate should be cached First, got %q", got)
	}
	r.Invalidate()
	if got := r.InstanceName(ctx); got != "Second" {
		t.Fatalf("post-invalidate should reload Second, got %q", got)
	}
}

func tinyPNG(t *testing.T, side int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, side, side))); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func TestLoginImages_VerbatimAndLimit(t *testing.T) {
	ctx := context.Background()
	fs := &fakeStore{}
	r := NewWithStore("X", fs)
	if got := r.LoginImages(ctx); len(got) != 0 {
		t.Fatalf("LoginImages() = %v, want none", got)
	}

	raw := tinyPNG(t, 8)
	ref, err := r.AddLoginImage(ctx, raw)
	if err != nil {
		t.Fatalf("AddLoginImage: %v", err)
	}
	if etag, ok := r.LoginImageEtag(ctx, ref.ID); !ok || etag != ref.Etag || etag == "" {
		t.Fatalf("LoginImageEtag = (%q,%v), want the upload's etag", etag, ok)
	}
	data, _, err := r.LoginImage(ctx, ref.ID)
	if err != nil || !bytes.Equal(data, raw) {
		t.Fatalf("LoginImage returned different bytes (err %v) — backgrounds must be verbatim", err)
	}

	// An invalid upload never reaches the store.
	if _, err := r.AddLoginImage(ctx, []byte("not an image")); err == nil {
		t.Fatal("AddLoginImage(bad) = nil err, want ErrInvalidImage")
	}
	for i := 2; i <= MaxLoginImages; i++ {
		if _, err := r.AddLoginImage(ctx, tinyPNG(t, i+8)); err != nil {
			t.Fatalf("image %d: %v", i, err)
		}
	}
	if _, err := r.AddLoginImage(ctx, tinyPNG(t, 30)); !errors.Is(err, ErrImageLimit) {
		t.Fatalf("image 11: err = %v, want ErrImageLimit", err)
	}
	if got := len(r.LoginImages(ctx)); got != MaxLoginImages {
		t.Fatalf("LoginImages() has %d, want %d", got, MaxLoginImages)
	}

	if err := r.DeleteLoginImage(ctx, ref.ID); err != nil {
		t.Fatalf("DeleteLoginImage: %v", err)
	}
	if _, ok := r.LoginImageEtag(ctx, ref.ID); ok {
		t.Fatal("deleted image still listed")
	}
	if err := r.DeleteLoginImage(ctx, ref.ID); !errors.Is(err, ErrImageNotFound) {
		t.Fatalf("second delete: err = %v, want ErrImageNotFound", err)
	}
}

func TestAppearance_DefaultSaveAndInvalidate(t *testing.T) {
	ctx := context.Background()
	fs := &fakeStore{}
	r := NewWithStore("X", fs)
	if got := r.Appearance(ctx); got != DefaultAppearance() {
		t.Fatalf("Appearance() = %+v, want the default", got)
	}
	gets := fs.gets
	_ = r.Appearance(ctx)
	_ = r.LoginImages(ctx)
	if fs.gets != gets {
		t.Fatal("repeated reads went back to the store; the row should be cached")
	}

	a := DefaultAppearance()
	a.Background.Source = SourceGradient
	a.Background.Gradient = "aurora"
	a.Card = Surface{Translucent: true, Opacity: 60, Blur: false}
	if err := r.SetAppearance(ctx, a, nil); err != nil {
		t.Fatalf("SetAppearance: %v", err)
	}
	if got := r.Appearance(ctx); got != a {
		t.Fatalf("after save Appearance() = %+v, want %+v", got, a)
	}

	bad := a
	bad.Card.Opacity = 101
	if err := r.SetAppearance(ctx, bad, nil); !errors.Is(err, ErrInvalidAppearance) {
		t.Fatalf("SetAppearance(opacity 101) err = %v, want ErrInvalidAppearance", err)
	}

	a.Background.Source = SourceUnsplash
	if err := r.SetAppearance(ctx, a, nil); !errors.Is(err, ErrUnsplashKeyRequired) {
		t.Fatalf("unsplash without key: err = %v, want ErrUnsplashKeyRequired", err)
	}
	key := &SealedKey{Ciphertext: []byte("c"), Nonce: []byte("n"), KeyVersion: 1}
	if err := r.SetAppearance(ctx, a, key); err != nil {
		t.Fatalf("unsplash with key: %v", err)
	}
	if got := r.UnsplashKey(ctx); got == nil || got.KeyVersion != 1 {
		t.Fatalf("UnsplashKey() = %+v, want the saved key", got)
	}
	if err := r.ClearUnsplashKey(ctx); !errors.Is(err, ErrUnsplashKeyInUse) {
		t.Fatalf("clear in-use key: err = %v, want ErrUnsplashKeyInUse", err)
	}
	a.Background.Source = SourceNone
	if err := r.SetAppearance(ctx, a, nil); err != nil {
		t.Fatalf("switch away: %v", err)
	}
	if err := r.ClearUnsplashKey(ctx); err != nil {
		t.Fatalf("ClearUnsplashKey: %v", err)
	}
	if r.UnsplashKey(ctx) != nil {
		t.Fatal("key still reported after clearing")
	}
}
