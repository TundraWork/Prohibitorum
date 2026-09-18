// Package webui embeds the built React SPA (pkg/webui/dist) and serves it
// same-origin with a SPA history fallback + strict security headers.
package webui

import (
	"bytes"
	"embed"
	"html"
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
func Handler(instanceName string) http.Handler {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic("webui: dist not embedded: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		panic("webui: dist/index.html missing — run the frontend build first")
	}
	if instanceName == "" {
		instanceName = "Prohibitorum"
	}
	index = bytes.ReplaceAll(index, []byte("__INSTANCE_NAME__"), []byte(html.EscapeString(instanceName)))
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
	// React Aria injects a fixed pressable touch-action rule; allow only its hash.
	// Inline style attributes support positioning; scripts remain same-origin.
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; script-src 'self'; style-src-elem 'self' 'sha256-38RhXrc7EdReTKsOm23ZPOCUgniTUUcjky8QOOrQx6o='; style-src-attr 'unsafe-inline'; connect-src 'self' blob:; img-src 'self' data: blob:; font-src 'self'; base-uri 'self'; form-action 'self'; object-src 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}
