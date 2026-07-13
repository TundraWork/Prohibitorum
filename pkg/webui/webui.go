// Package webui embeds the built Vue SPA (pkg/webui/dist) and serves it
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
	// Style CSP is split into the two finer-grained directives now that the
	// frontend is Tailwind v4 + shadcn-vue (no Nuxt UI runtime <style> injection):
	//   style-src-elem 'self' 'sha256-…' — all CSS ships as a static, same-origin
	//                                <link> stylesheet. The ONE exception is a
	//                                runtime <style> element Reka UI's Select
	//                                component injects to hide scrollbars on the
	//                                viewport:
	//                                  [data-reka-select-viewport] {
	//                                    scrollbar-width:none;
	//                                    -ms-overflow-style: none;
	//                                    -webkit-overflow-scrolling: touch;
	//                                  }
	//                                  [data-reka-select-viewport]::-webkit-scrollbar {
	//                                    display: none;
	//                                  }
	//                                whose browser-confirmed hash is
	//                                sha256-60LHlRjW/B3CtzIoE/Lf1/NEDvko9efWMFaGVhHu/cs=
	//                                (bound to the installed reka-ui version).
	//                                A narrow hash allows ONLY that exact style
	//                                element; any other inline <style> — including
	//                                a changed scrollbar rule from a dependency
	//                                bump — is blocked. This is strictly tighter
	//                                than 'unsafe-inline', which would permit any
	//                                inline style content.
	//   style-src-attr 'unsafe-inline' — Reka UI writes inline style *attributes*
	//                                for popover/dialog positioning, and a few of
	//                                our components bind :style (e.g. the card's
	//                                overlay shadow, the auth backdrop image). Only
	//                                the attribute channel is loosened, not <style>.
	// script-src stays 'self' (the only script is the same-origin module bundle;
	// no inline JS). font-src 'self' — webfonts are bundled into /assets and served
	// same-origin (Vite is configured not to inline them as data: URIs).
	// default-src 'self' no longer covers the enumerated directives.
	//
	// connect-src / img-src allow blob: in addition to 'self' for the avatar
	// cropper: the client reads the chosen image as a page-created blob: URL to
	// detect EXIF orientation (connect-src) and renders the crop preview from it
	// (img-src). blob: is same-origin and page-created — it cannot reach external
	// hosts, so this does not weaken the egress posture.
	//
	// Fallback: if style-src-attr ever proves too strict in a real HTTPS browser
	// check (e.g. a dependency starts emitting inline <style> elements), revert to
	// "style-src 'self' 'unsafe-inline'" — no worse than the pre-rebuild policy.
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; script-src 'self'; style-src-elem 'self' 'sha256-60LHlRjW/B3CtzIoE/Lf1/NEDvko9efWMFaGVhHu/cs='; style-src-attr 'unsafe-inline'; connect-src 'self' blob:; img-src 'self' data: blob:; font-src 'self'; base-uri 'self'; form-action 'self'; object-src 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}
