package imageutil

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/gen2brain/webp"
)

func solidImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 120, B: 200, A: 255})
		}
	}
	return img
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, solidImage(w, h)); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, solidImage(w, h), nil); err != nil {
		t.Fatalf("jpeg encode: %v", err)
	}
	return buf.Bytes()
}

func webpBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := webp.Encode(&buf, solidImage(w, h), webp.Options{Quality: 80}); err != nil {
		t.Fatalf("webp encode: %v", err)
	}
	return buf.Bytes()
}

func gifBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := gif.Encode(&buf, solidImage(w, h), nil); err != nil {
		t.Fatalf("gif encode: %v", err)
	}
	return buf.Bytes()
}

func TestValidateRaw_AcceptsWebFormats(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  []byte
	}{
		{"png", pngBytes(t, 64, 48)},
		{"jpeg", jpegBytes(t, 64, 48)},
		{"webp", webpBytes(t, 64, 48)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			etag, err := ValidateRaw(tc.raw)
			if err != nil {
				t.Fatalf("ValidateRaw(%s) err = %v, want nil", tc.name, err)
			}
			sum := sha256.Sum256(tc.raw)
			if want := hex.EncodeToString(sum[:]); etag != want {
				t.Errorf("etag = %q, want sha256 over raw %q", etag, want)
			}
		})
	}
}

func TestValidateRaw_RejectsTooLarge(t *testing.T) {
	raw := make([]byte, MaxInputBytes+1)
	if _, err := ValidateRaw(raw); err != ErrTooLarge {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestValidateRaw_RejectsNonImage(t *testing.T) {
	if _, err := ValidateRaw([]byte("this is definitely not an image")); err != ErrInvalidImage {
		t.Fatalf("err = %v, want ErrInvalidImage", err)
	}
}

func TestValidateRaw_RejectsDisallowedFormat(t *testing.T) {
	if _, err := ValidateRaw(gifBytes(t, 32, 32)); err != ErrInvalidImage {
		t.Fatalf("gif err = %v, want ErrInvalidImage", err)
	}
}

func TestValidateRaw_RejectsOversizeDimensions(t *testing.T) {
	// 10001×1 solid PNG: a valid, tiny-byte image whose width exceeds MaxInputDim.
	raw := pngBytes(t, MaxInputDim+1, 1)
	if len(raw) > MaxInputBytes {
		t.Skipf("fixture unexpectedly large (%d bytes)", len(raw))
	}
	if _, err := ValidateRaw(raw); err != ErrInvalidImage {
		t.Fatalf("err = %v, want ErrInvalidImage", err)
	}
}
