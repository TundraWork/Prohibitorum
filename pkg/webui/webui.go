// Package webui embeds the built React SPA (pkg/webui/dist) and serves it
// same-origin with a SPA history fallback + strict security headers.
package webui

import (
	"context"
	"embed"
	"html"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

//go:embed all:dist
var embedded embed.FS

// Handler returns an http.Handler that serves embedded SPA assets, falling back
// to index.html for any path that does not match a built file (client-side
// routing). Intended as the chi router's NotFound handler, so it is only reached
// for paths no registered route matched.
//
// The shell's <title> is filled per request via instanceName, because an admin
// can rename the instance after startup; the template is split on the
// placeholder once so each response only substitutes the (escaped) name.
func Handler(instanceName func(ctx context.Context) string) http.Handler {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic("webui: dist not embedded: " + err.Error())
	}
	return newHandler(sub, instanceName)
}

// assetDir is where the build writes its content-hashed files. A name there
// never changes meaning, so a response for it is cacheable for good, and a
// name that is not there is a missing file rather than a page address.
const assetDir = "assets/"

// immutable caches a hashed asset for a year without revalidation.
const immutable = "public, max-age=31536000, immutable"

// precompressed are the copies the build writes beside each compressible
// asset, in the order they are preferred.
var precompressed = []struct{ coding, suffix string }{
	{"br", ".br"},
	{"gzip", ".gz"},
}

func newHandler(sub fs.FS, instanceName func(ctx context.Context) string) http.Handler {
	fileServer := http.FileServer(http.FS(sub))
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		panic("webui: dist/index.html missing — run the frontend build first")
	}
	shells := loadShells(sub, index)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w)
		// If the path maps to an existing embedded FILE, serve it; a directory
		// hit (e.g. /assets) falls through to the SPA shell rather than exposing
		// a directory listing.
		p := strings.TrimPrefix(r.URL.Path, "/")
		if strings.HasPrefix(p, assetDir) {
			serveAsset(w, r, sub, fileServer, p)
			return
		}
		if p != "" {
			if f, ferr := sub.Open(p); ferr == nil {
				info, statErr := f.Stat()
				_ = f.Close()
				if statErr == nil && !info.IsDir() {
					fileServer.ServeHTTP(w, r)
					return
				}
			}
		}
		name := instanceName(r.Context())
		if name == "" {
			name = "Prohibitorum"
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// The shell varies with the instance name, so no ETag is offered and
		// the response is uncachable — a renamed instance must not serve a
		// stale title from a cache or link preview.
		w.Header().Set("Cache-Control", "no-store")
		for i, part := range shells.forPath(r.URL.Path) {
			if i > 0 {
				_, _ = w.Write([]byte(html.EscapeString(name)))
			}
			_, _ = w.Write(part)
		}
	})
}

// serveAsset answers a path under assetDir: the file, immutable, in the best
// precompressed copy the request accepts; or 404 when there is no such file,
// as for an old page asking for a script a newer build replaced.
func serveAsset(w http.ResponseWriter, r *http.Request, sub fs.FS, fileServer http.Handler, p string) {
	info, err := fs.Stat(sub, p)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", immutable)
	accepted := r.Header.Get("Accept-Encoding")
	compressible := false
	for _, variant := range precompressed {
		f, err := sub.Open(p + variant.suffix)
		if err != nil {
			continue
		}
		compressible = true
		content, seekable := f.(io.ReadSeeker)
		if !seekable || !acceptsCoding(accepted, variant.coding) {
			_ = f.Close()
			continue
		}
		defer f.Close()
		w.Header().Set("Vary", "Accept-Encoding")
		if ctype := mime.TypeByExtension(path.Ext(p)); ctype != "" {
			w.Header().Set("Content-Type", ctype)
		}
		w.Header().Set("Content-Encoding", variant.coding)
		http.ServeContent(w, r, p, time.Time{}, content)
		return
	}
	// A request that accepts none of the copies gets the file itself; the
	// response still depends on what it accepted.
	if compressible {
		w.Header().Set("Vary", "Accept-Encoding")
	}
	fileServer.ServeHTTP(w, r)
}

// acceptsCoding reports whether an Accept-Encoding header allows coding: it is
// listed, or covered by "*", with a quality above zero.
func acceptsCoding(header, coding string) bool {
	allowed := false
	for _, entry := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(entry), ";")
		name = strings.TrimSpace(name)
		if !strings.EqualFold(name, coding) && name != "*" {
			continue
		}
		q := 1.0
		for _, param := range strings.Split(params, ";") {
			key, value, found := strings.Cut(strings.TrimSpace(param), "=")
			if found && strings.EqualFold(strings.TrimSpace(key), "q") {
				if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
					q = parsed
				}
			}
		}
		if strings.EqualFold(name, coding) {
			// An explicit entry decides, whatever "*" said.
			return q > 0
		}
		allowed = q > 0
	}
	return allowed
}

func setSecurityHeaders(w http.ResponseWriter) {
	// Base UI's scroll area injects a fixed rule hiding the native scrollbars;
	// allow only its hash.
	// Inline style attributes support positioning; scripts remain same-origin.
	// The sign-in page's Bing and Unsplash wallpapers load straight from their
	// image hosts.
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; script-src 'self'; style-src-elem 'self' 'sha256-kLmvWqfziFavKtqHqRsb90f006UAK2Dmd0It5Iz2KFA='; style-src-attr 'unsafe-inline'; connect-src 'self' blob:; img-src 'self' data: blob: https://www.bing.com https://images.unsplash.com; font-src 'self'; base-uri 'self'; form-action 'self'; object-src 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}
