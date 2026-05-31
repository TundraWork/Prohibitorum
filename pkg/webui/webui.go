// Package webui embeds the built Vue SPA (pkg/webui/dist) and serves it
// same-origin with a SPA history fallback + strict security headers.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

// Handler returns an http.Handler that serves embedded SPA assets, falling back
// to index.html for any path that does not match a built file (client-side
// routing). Intended as the chi router's NotFound handler, so it is only reached
// for paths no registered route matched.
func Handler() http.Handler {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic("webui: dist not embedded: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		panic("webui: dist/index.html missing — run the frontend build first")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w)
		// If the path maps to an existing embedded FILE, serve it; a directory
		// hit (e.g. /assets) falls through to the SPA shell rather than exposing
		// a directory listing.
		p := strings.TrimPrefix(r.URL.Path, "/")
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
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
}

func setSecurityHeaders(w http.ResponseWriter) {
	// style-src needs 'unsafe-inline' because Nuxt UI / Reka UI inject inline
	// <style> elements at runtime (theme colors, transition suppression) via
	// document.createElement("style"); a strict style-src would block these and
	// break styling in production HTTPS (the HTTP curl smoke can't catch it).
	// script-src is listed explicitly as 'self' so loosening style-src does NOT
	// also loosen scripts — once we enumerate directives, default-src no longer
	// covers script-src, and we keep it tight (no unsafe-inline for scripts).
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; base-uri 'self'; form-action 'self'; object-src 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}
