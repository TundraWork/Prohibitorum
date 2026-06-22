// Package branding — accent.go
//
// AccentColor extracts one representative sRGB colour from a processed icon, for
// tinting the launchpad tile backdrop. It is computed once at upload time (and
// lazily backfilled for legacy icons), never per render.
//
// The estimate is alpha-aware (transparent pixels are ignored, so a logo on a
// transparent background contributes only its mark) and saturation-weighted, so
// the result leans toward the icon's vivid colour rather than a muddy average.
// Near-grayscale icons (e.g. a black wordmark) fall back to a plain alpha
// average, which the client renders as a near-neutral backdrop.
package branding

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"  // decode legacy GIF icons
	_ "image/jpeg" // decode legacy JPEG icons
	_ "image/png"  // decode legacy PNG icons
	"math"

	_ "github.com/gen2brain/webp" // decode current WebP icons

	"prohibitorum/pkg/imageutil"
)

// ErrNoOpaquePixels means the icon was fully (or almost entirely) transparent,
// so no representative colour could be derived. Callers should treat the icon as
// having no accent and fall back to a name-derived tint.
var ErrNoOpaquePixels = errors.New("branding: icon has no opaque pixels")

// alphaCutoff: pixels below this straight-alpha are treated as background.
const alphaCutoff = 0.35

// ProcessIconWithAccent normalizes raw to a 512×512 WebP (like ProcessIcon) AND
// derives the backdrop accent from the SAME decode — no second pass over the
// image. accent is "" when the icon is fully transparent (no representative
// colour); callers store NULL and fall back to a name-derived tint.
func ProcessIconWithAccent(raw []byte) (out []byte, etag, accent string, err error) {
	img, derr := imageutil.DecodeCropScale(raw)
	if derr != nil {
		return nil, "", "", derr
	}
	if hex, aerr := AccentColor(img); aerr == nil {
		accent = hex
	}
	out, etag, err = imageutil.EncodeWebP(img)
	return out, etag, accent, err
}

// AccentColorBytes decodes raw (any registered format: WebP for current icons,
// PNG/JPEG/GIF for legacy rows being healed) and returns its accent.
func AccentColorBytes(raw []byte) (string, error) {
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("branding.AccentColorBytes: decode: %w", err)
	}
	return AccentColor(img)
}

// AccentColor returns a representative colour of an already-decoded icon as an
// "#rrggbb" string.
func AccentColor(img image.Image) (string, error) {
	b := img.Bounds()
	// Stride keeps this cheap on a 256² icon without changing the estimate.
	stride := 1
	if d := b.Dx(); d > 64 {
		stride = d / 64
	}

	// Two accumulators: saturation+alpha weighted (vivid bias) and a plain
	// alpha-weighted average used as a fallback for near-grayscale icons.
	var vr, vg, vb, vw float64
	var ar, ag, ab, aw float64

	for y := b.Min.Y; y < b.Max.Y; y += stride {
		for x := b.Min.X; x < b.Max.X; x += stride {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			a := float64(c.A) / 255
			if a < alphaCutoff {
				continue
			}
			r := float64(c.R) / 255
			g := float64(c.G) / 255
			bl := float64(c.B) / 255

			max := math.Max(r, math.Max(g, bl))
			min := math.Min(r, math.Min(g, bl))
			sat := 0.0
			if max > 0 {
				sat = (max - min) / max
			}

			ar += r * a
			ag += g * a
			ab += bl * a
			aw += a

			w := a * (0.15 + sat) // floor so muted-but-present colours still count
			vr += r * w
			vg += g * w
			vb += bl * w
			vw += w
		}
	}

	if aw == 0 {
		return "", ErrNoOpaquePixels
	}

	var r, g, bl float64
	if vw > 1e-4 {
		r, g, bl = vr/vw, vg/vw, vb/vw
	} else {
		r, g, bl = ar/aw, ag/aw, ab/aw
	}
	return fmt.Sprintf("#%02x%02x%02x", to8(r), to8(g), to8(bl)), nil
}

func to8(v float64) uint8 {
	n := int(math.Round(v * 255))
	if n < 0 {
		n = 0
	}
	if n > 255 {
		n = 255
	}
	return uint8(n)
}
