package branding

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"testing"
)

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

func TestProcessIcon_PNG256Square(t *testing.T) {
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
