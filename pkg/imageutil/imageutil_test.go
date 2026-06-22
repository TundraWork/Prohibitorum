package imageutil

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func pngInput(t *testing.T, w, h int, fill func(x, y int) color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, fill(x, y))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode input: %v", err)
	}
	return buf.Bytes()
}

func TestProcessSquareWebP_SizeFormatEtag(t *testing.T) {
	in := pngInput(t, 400, 200, func(x, y int) color.NRGBA { return color.NRGBA{R: 0x33, G: 0x88, B: 0xcc, A: 0xff} })
	out, etag, err := ProcessSquareWebP(in)
	if err != nil {
		t.Fatalf("ProcessSquareWebP: %v", err)
	}
	if etag == "" {
		t.Fatal("empty etag")
	}
	cfg, format, derr := image.DecodeConfig(bytes.NewReader(out))
	if derr != nil || format != "webp" {
		t.Fatalf("format=%q err=%v", format, derr)
	}
	if cfg.Width != Size || cfg.Height != Size {
		t.Fatalf("size %dx%d want %dx%d", cfg.Width, cfg.Height, Size, Size)
	}
}

// Transparent regions of an icon must survive the WebP round-trip — otherwise a
// logo on a transparent background would gain a solid box.
func TestProcessSquareWebP_PreservesTransparency(t *testing.T) {
	in := pngInput(t, 256, 256, func(x, y int) color.NRGBA {
		if x >= 96 && x < 160 && y >= 96 && y < 160 {
			return color.NRGBA{R: 0xff, A: 0xff} // opaque red center
		}
		return color.NRGBA{} // transparent elsewhere
	})
	out, _, err := ProcessSquareWebP(in)
	if err != nil {
		t.Fatalf("ProcessSquareWebP: %v", err)
	}
	img, _, derr := image.Decode(bytes.NewReader(out))
	if derr != nil {
		t.Fatalf("decode: %v", derr)
	}
	// Input is 256²; output is Size² — the opaque centre maps to the middle.
	cornerA := color.NRGBAModel.Convert(img.At(8, 8)).(color.NRGBA).A
	centerA := color.NRGBAModel.Convert(img.At(Size/2, Size/2)).(color.NRGBA).A
	if cornerA > 40 {
		t.Fatalf("corner should be transparent, alpha=%d", cornerA)
	}
	if centerA < 200 {
		t.Fatalf("center should be opaque, alpha=%d", centerA)
	}
}

func TestProcessSquareWebP_Errors(t *testing.T) {
	if _, _, err := ProcessSquareWebP([]byte("not an image")); err != ErrInvalidImage {
		t.Fatalf("garbage: want ErrInvalidImage, got %v", err)
	}
	if _, _, err := ProcessSquareWebP(make([]byte, MaxInputBytes+1)); err != ErrTooLarge {
		t.Fatalf("oversize: want ErrTooLarge, got %v", err)
	}
}
