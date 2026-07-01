package branding

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"testing"
)

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
