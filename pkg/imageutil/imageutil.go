// Package imageutil normalizes an uploaded raster to a fixed-size square WebP.
// It is the single home of the decode → center-crop → scale → encode pipeline
// shared by pkg/avatar (user photos) and pkg/branding (instance + app icons),
// so the two never drift in crop logic, input guards, format, or quality.
//
// WebP encoding uses gen2brain/webp (libwebp compiled to WASM, run via the
// pure-Go wazero runtime). Build with the `nodynamic` tag so it never tries to
// dlopen a system libwebp — embedded WASM only, no cgo. Importing this package
// also registers the WebP decoder with image.Decode (via gen2brain/webp's init).
package imageutil

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	_ "image/gif"  // decode uploaded GIFs
	_ "image/jpeg" // decode uploaded JPEGs
	_ "image/png"  // decode uploaded PNGs

	"github.com/gen2brain/webp"
	"golang.org/x/image/draw"
)

const (
	// Size is the one canonical stored edge for every normalized image (avatars
	// and icons alike). Square, 512×512.
	Size = 512
	// MaxInputBytes / MaxInputDim bound the decode work before any allocation.
	MaxInputBytes = 5 << 20
	MaxInputDim   = 10000
	// webpQuality 90 is a deliberate step above the old 85: icons are flat-colour
	// marks where edge fidelity matters, and photos benefit too. Method 6 is the
	// slowest/best encode (these run once, at upload). Exact preserves RGB in
	// fully-transparent areas so a logo's halo can't bleed.
	webpQuality = 90
	webpMethod  = 6
)

var (
	ErrTooLarge     = errors.New("imageutil: input too large")
	ErrInvalidImage = errors.New("imageutil: invalid or unsupported image")
)

// DecodeCropScale decodes raw, validates its dimensions, center-crops to the
// largest centred square, and scales to Size×Size RGBA (alpha preserved).
func DecodeCropScale(raw []byte) (*image.RGBA, error) {
	if len(raw) > MaxInputBytes {
		return nil, ErrTooLarge
	}
	src, _, derr := image.Decode(bytes.NewReader(raw))
	if derr != nil {
		return nil, ErrInvalidImage
	}
	b := src.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 || b.Dx() > MaxInputDim || b.Dy() > MaxInputDim {
		return nil, ErrInvalidImage
	}
	square := cropSquare(src)
	dst := image.NewRGBA(image.Rect(0, 0, Size, Size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), square, square.Bounds(), draw.Over, nil)
	return dst, nil
}

// EncodeWebP encodes img as WebP (quality 90) and returns the bytes + a sha256
// etag over them.
func EncodeWebP(img image.Image) (out []byte, etag string, err error) {
	var buf bytes.Buffer
	if eerr := webp.Encode(&buf, img, webp.Options{Quality: webpQuality, Method: webpMethod, Exact: true}); eerr != nil {
		return nil, "", ErrInvalidImage
	}
	out = buf.Bytes()
	sum := sha256.Sum256(out)
	return out, hex.EncodeToString(sum[:]), nil
}

// ProcessSquareWebP is DecodeCropScale followed by EncodeWebP: raw bytes in,
// Size×Size WebP + etag out. Use this when you only need the encoded bytes; when
// you also need the decoded image (e.g. to derive an accent), call DecodeCropScale
// once and pass its result to EncodeWebP yourself.
func ProcessSquareWebP(raw []byte) (out []byte, etag string, err error) {
	img, derr := DecodeCropScale(raw)
	if derr != nil {
		return nil, "", derr
	}
	return EncodeWebP(img)
}

// ValidateRaw verifies raw is a web-renderable image within the size and
// dimension limits WITHOUT decoding pixels or re-encoding. It is for assets that
// must be served back byte-for-byte (the login-page background): the bytes are
// never transformed, only sniffed via image.DecodeConfig. It returns a sha256-hex
// etag over the unmodified bytes.
//
// Rejects: oversize input (ErrTooLarge); anything DecodeConfig can't parse, a
// format outside {png, jpeg, webp}, or out-of-range dimensions (ErrInvalidImage).
func ValidateRaw(raw []byte) (etag string, err error) {
	if len(raw) > MaxInputBytes {
		return "", ErrTooLarge
	}
	cfg, format, derr := image.DecodeConfig(bytes.NewReader(raw))
	if derr != nil {
		return "", ErrInvalidImage
	}
	switch format {
	case "png", "jpeg", "webp":
	default:
		return "", ErrInvalidImage
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxInputDim || cfg.Height > MaxInputDim {
		return "", ErrInvalidImage
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// cropSquare center-crops src to its largest centred square.
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
