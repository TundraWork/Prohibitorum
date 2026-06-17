// Package avatar validates and normalizes uploaded images to a fixed-size
// WebP for storage, and builds the public avatar URL.
//
// WebP encoding uses gen2brain/webp (libwebp compiled to WASM, run via the
// pure-Go wazero runtime). Build with the `nodynamic` tag so it never tries to
// dlopen a system libwebp -- embedded WASM only, no cgo.
package avatar

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/url"

	"github.com/gen2brain/webp"
	"golang.org/x/image/draw"

	"prohibitorum/pkg/db"
)

const (
	maxInputBytes = 5 << 20
	maxInputDim   = 10000
	size          = 512
	quality       = 85
	method        = 6
)

var (
	ErrTooLarge     = errors.New("avatar: input too large")
	ErrInvalidImage = errors.New("avatar: invalid or unsupported image")
)

func Process(raw []byte) (out []byte, etag string, err error) {
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
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), square, square.Bounds(), draw.Over, nil)

	var buf bytes.Buffer
	if eerr := encodeAvatar(&buf, dst); eerr != nil {
		return nil, "", ErrInvalidImage
	}
	out = buf.Bytes()
	sum := sha256.Sum256(out)
	return out, hex.EncodeToString(sum[:]), nil
}

// encodeAvatar is the ONLY place that knows the output format/params.
func encodeAvatar(buf *bytes.Buffer, img image.Image) error {
	return webp.Encode(buf, img, webp.Options{Quality: quality, Method: method})
}

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

// PublicURL builds the cache-busting avatar URL, or "" when there is no etag.
func PublicURL(subject, etag, origin string) string {
	if subject == "" || etag == "" {
		return ""
	}
	v := etag
	if len(v) > 8 {
		v = v[:8]
	}
	return origin + "/avatar/" + subject + "?v=" + v
}

// AccountURL is PublicURL for a db.Account (extracts subject + etag).
func AccountURL(a db.Account, origin string) string {
	if !a.AvatarEtag.Valid {
		return ""
	}
	return PublicURL(a.OidcSubject.String(), a.AvatarEtag.String, origin)
}

// SourceURL builds the cache-busting avatar URL for a SPECIFIC source, or "" when no etag.
func SourceURL(subject, source, etag, origin string) string {
	if subject == "" || etag == "" {
		return ""
	}
	v := etag
	if len(v) > 8 {
		v = v[:8]
	}
	// Escape the source: it carries an upstream slug ("upstream:<slug>") and the
	// slug column has no charset CHECK, so don't assume URL-safety. The browser
	// decodes it back before the serve handler reads ?source=.
	return origin + "/avatar/" + subject + "?source=" + url.QueryEscape(source) + "&v=" + v
}
