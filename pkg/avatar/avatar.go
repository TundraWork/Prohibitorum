// Package avatar validates and normalizes uploaded images to a fixed-size
// WebP for storage, and builds the public avatar URL. The image pipeline itself
// (decode → crop → scale → WebP) lives in pkg/imageutil and is shared with the
// icon pipeline; this package only fixes the avatar size and the URL shape.
package avatar

import (
	"net/url"

	"prohibitorum/pkg/db"
	"prohibitorum/pkg/imageutil"
)

// Errors are re-exported from imageutil so callers can keep matching on
// avatar.ErrTooLarge / avatar.ErrInvalidImage.
var (
	ErrTooLarge     = imageutil.ErrTooLarge
	ErrInvalidImage = imageutil.ErrInvalidImage
)

// Process normalizes raw to a 512×512 lossy WebP (quality 90) + sha256 etag.
// Avatars are photographic, so lossy is the right size/quality trade-off.
func Process(raw []byte) (out []byte, etag string, err error) {
	return imageutil.ProcessSquareWebP(raw, imageutil.Size, false)
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
