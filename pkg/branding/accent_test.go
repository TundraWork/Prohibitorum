package branding

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"testing"

	"prohibitorum/pkg/imageutil"
)

// encodePNG renders a fill function over a sizexsize NRGBA image and PNG-encodes it.
func encodePNG(t *testing.T, size int, fill func(x, y int) color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetNRGBA(x, y, fill(x, y))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func TestAccentColor_VividDominatesOverTransparent(t *testing.T) {
	// A blue mark on a transparent background: only the opaque blue pixels count,
	// so the accent must be clearly blue (B channel dominant), not washed toward
	// the transparent area.
	blue := color.NRGBA{R: 0x25, G: 0x63, B: 0xeb, A: 0xff}
	pngBytes := encodePNG(t, 64, func(x, y int) color.NRGBA {
		if x >= 20 && x < 44 && y >= 20 && y < 44 {
			return blue
		}
		return color.NRGBA{} // transparent
	})
	hex, err := AccentColorBytes(pngBytes)
	if err != nil {
		t.Fatalf("AccentColor: %v", err)
	}
	r, g, b := hexToRGB(t, hex)
	if !(b > g && g > r) {
		t.Fatalf("expected blue-dominant accent, got %s (r=%d g=%d b=%d)", hex, r, g, b)
	}
}

func TestAccentColor_GrayscaleStaysNeutral(t *testing.T) {
	// A black mark on white (no saturation anywhere): the accent must be neutral
	// (R≈G≈B), so the client renders a near-neutral backdrop.
	pngBytes := encodePNG(t, 64, func(x, y int) color.NRGBA {
		if x >= 24 && x < 40 && y >= 24 && y < 40 {
			return color.NRGBA{R: 0x10, G: 0x10, B: 0x10, A: 0xff}
		}
		return color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	})
	hex, err := AccentColorBytes(pngBytes)
	if err != nil {
		t.Fatalf("AccentColor: %v", err)
	}
	r, g, b := hexToRGB(t, hex)
	if max(r, max(g, b))-min(r, min(g, b)) > 12 {
		t.Fatalf("expected neutral accent, got %s", hex)
	}
}

func TestAccentColor_FullyTransparentErrors(t *testing.T) {
	pngBytes := encodePNG(t, 32, func(x, y int) color.NRGBA { return color.NRGBA{} })
	if _, err := AccentColorBytes(pngBytes); err == nil {
		t.Fatal("expected ErrNoOpaquePixels for a fully transparent icon")
	}
}

// Icons are stored as WebP now, so AccentColor must read WebP, not just PNG.
func TestAccentColor_ReadsWebP(t *testing.T) {
	webpBytes, _, err := imageutil.ProcessSquareWebP(
		encodePNG(t, 64, func(x, y int) color.NRGBA { return color.NRGBA{R: 0x20, G: 0x9a, B: 0x4a, A: 0xff} }),
	)
	if err != nil {
		t.Fatalf("encode webp: %v", err)
	}
	hex, err := AccentColorBytes(webpBytes)
	if err != nil {
		t.Fatalf("AccentColor(webp): %v", err)
	}
	r, g, b := hexToRGB(t, hex)
	if !(g > r && g > b) {
		t.Fatalf("expected green-dominant accent from webp, got %s", hex)
	}
}

func hexToRGB(t *testing.T, hex string) (r, g, b int) {
	t.Helper()
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); err != nil {
		t.Fatalf("parse %q: %v", hex, err)
	}
	return r, g, b
}
