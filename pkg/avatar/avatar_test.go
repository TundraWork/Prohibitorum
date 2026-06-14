package avatar

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	_ "github.com/gen2brain/webp" // register webp decoder for image.Decode

	"github.com/gen2brain/webp"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestProcess_ValidProducesSquareWebP(t *testing.T) {
	out, etag, err := Process(pngBytes(t, 800, 600))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if etag == "" {
		t.Fatal("empty etag")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("output not decodable: %v", err)
	}
	if format != "webp" {
		t.Fatalf("format = %q, want webp", format)
	}
	if cfg.Width != 512 || cfg.Height != 512 {
		t.Fatalf("size = %dx%d, want 512x512", cfg.Width, cfg.Height)
	}
	_ = webp.DefaultQuality
}

func TestProcess_TooLarge(t *testing.T) {
	if _, _, err := Process(make([]byte, 5<<20+1)); err != ErrTooLarge {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestProcess_Garbage(t *testing.T) {
	if _, _, err := Process([]byte("not an image")); err != ErrInvalidImage {
		t.Fatalf("err = %v, want ErrInvalidImage", err)
	}
}

func TestPublicURL(t *testing.T) {
	got := PublicURL("11111111-2222-3333-4444-555555555555", "deadbeefcafe", "https://auth.example.com")
	want := "https://auth.example.com/avatar/11111111-2222-3333-4444-555555555555?v=deadbeef"
	if got != want {
		t.Fatalf("PublicURL = %q, want %q", got, want)
	}
	if PublicURL("sub", "", "https://x") != "" {
		t.Fatal("empty etag must yield empty URL")
	}
}

func TestSourceURL(t *testing.T) {
	got := SourceURL("11111111-2222-3333-4444-555555555555", "upstream", "deadbeefcafe", "https://x")
	if got != "https://x/avatar/11111111-2222-3333-4444-555555555555?source=upstream&v=deadbeef" {
		t.Fatalf("got %q", got)
	}
	if SourceURL("s", "user", "", "https://x") != "" {
		t.Fatal("empty etag -> empty")
	}
}
