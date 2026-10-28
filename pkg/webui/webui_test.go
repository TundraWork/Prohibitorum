package webui

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHandler_TemplatesTitlePerRequest(t *testing.T) {
	name := "Acme SSO"
	h := Handler(func(context.Context) string { return name })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), "<title>Acme SSO</title>") {
		t.Fatalf("title not templated; body head: %.200s", rec.Body.String())
	}

	// A rename must be visible on the very next shell response.
	name = "Renamed"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), "<title>Renamed</title>") {
		t.Fatalf("renamed title not templated; body head: %.200s", rec.Body.String())
	}
}

func TestHandler_EscapesTitle(t *testing.T) {
	h := Handler(func(context.Context) string { return `<b>&` })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(rec.Body.String(), "<title><b>&</title>") {
		t.Fatal("instance name was not HTML-escaped")
	}
	if !strings.Contains(rec.Body.String(), "<title>&lt;b&gt;&amp;</title>") {
		t.Fatalf("instance name not escaped as expected; body head: %.200s", rec.Body.String())
	}
}

func TestHandler_FallsBackWhenNameEmpty(t *testing.T) {
	h := Handler(func(context.Context) string { return "" })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), "<title>Prohibitorum</title>") {
		t.Fatalf("fallback title missing; body head: %.200s", rec.Body.String())
	}
}

func TestHandler_ShellNotCacheable(t *testing.T) {
	h := Handler(func(context.Context) string { return "Acme SSO" })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
	if rec.Header().Get("ETag") != "" {
		t.Fatalf("shell response must not offer an ETag, got %q", rec.Header().Get("ETag"))
	}
}

func TestSecurityHeaders_StyleSrcElem(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler(func(context.Context) string { return "Prohibitorum" }).ServeHTTP(
		rec, httptest.NewRequest(http.MethodGet, "/", nil))
	csp := rec.Header().Get("Content-Security-Policy")

	directives := make(map[string][]string)
	for _, part := range strings.Split(csp, ";") {
		fields := strings.Fields(part)
		if len(fields) > 0 {
			directives[fields[0]] = fields[1:]
		}
	}
	scripts := directives["script-src"]
	if len(scripts) != 1 || scripts[0] != "'self'" {
		t.Errorf("scripts must allow only same-origin resources: %s", csp)
	}
	scrollbarStyle := ".base-ui-disable-scrollbar{scrollbar-width:none}.base-ui-disable-scrollbar::-webkit-scrollbar{display:none}"
	hash := sha256.Sum256([]byte(scrollbarStyle))
	allowedHash := "'sha256-" + base64.StdEncoding.EncodeToString(hash[:]) + "'"
	styles := directives["style-src-elem"]
	if len(styles) != 2 || styles[0] != "'self'" || styles[1] != allowedHash {
		t.Errorf("styles must allow same-origin resources and the Base UI scroll area rule only: %s", csp)
	}
	images := strings.Join(directives["img-src"], " ")
	if images != "'self' data: blob: https://www.bing.com https://images.unsplash.com" {
		t.Errorf("images must allow same-origin, data:, blob: and the two wallpaper hosts only: %s", csp)
	}
}

// assetsFS is a build with one precompressed script and one font, which the
// build does not compress.
func assetsFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":            {Data: []byte("<title>__INSTANCE_NAME__</title>")},
		"assets/app-abc.js":     {Data: []byte("console.log('plain')")},
		"assets/app-abc.js.br":  {Data: []byte("brotli bytes")},
		"assets/app-abc.js.gz":  {Data: []byte("gzip bytes")},
		"assets/font-abc.woff2": {Data: []byte("font bytes")},
	}
}

func getAsset(t *testing.T, path, acceptEncoding string) *httptest.ResponseRecorder {
	t.Helper()
	h := newHandler(assetsFS(), func(context.Context) string { return "Acme SSO" })
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAsset_ServesBestPrecompressedCopy(t *testing.T) {
	for _, tc := range []struct{ accept, coding, body string }{
		{"gzip, deflate, br, zstd", "br", "brotli bytes"},
		{"gzip", "gzip", "gzip bytes"},
		{"br;q=0, gzip", "gzip", "gzip bytes"},
		{"*", "br", "brotli bytes"},
		{"", "", "console.log('plain')"},
		{"identity", "", "console.log('plain')"},
		{"*;q=0, identity", "", "console.log('plain')"},
	} {
		rec := getAsset(t, "/assets/app-abc.js", tc.accept)
		if rec.Code != http.StatusOK {
			t.Fatalf("Accept-Encoding %q: status %d", tc.accept, rec.Code)
		}
		if got := rec.Header().Get("Content-Encoding"); got != tc.coding {
			t.Errorf("Accept-Encoding %q: Content-Encoding = %q, want %q", tc.accept, got, tc.coding)
		}
		if rec.Body.String() != tc.body {
			t.Errorf("Accept-Encoding %q: body = %q, want %q", tc.accept, rec.Body.String(), tc.body)
		}
		if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
			t.Errorf("Accept-Encoding %q: Vary = %q", tc.accept, got)
		}
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/javascript") {
			t.Errorf("Accept-Encoding %q: Content-Type = %q", tc.accept, got)
		}
		if got := rec.Header().Get("Cache-Control"); got != immutable {
			t.Errorf("Accept-Encoding %q: Cache-Control = %q", tc.accept, got)
		}
	}
}

func TestAsset_UncompressedFileIsImmutableWithoutVary(t *testing.T) {
	rec := getAsset(t, "/assets/font-abc.woff2", "br, gzip")
	if rec.Code != http.StatusOK || rec.Body.String() != "font bytes" {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Encoding") != "" || rec.Header().Get("Vary") != "" {
		t.Fatalf("font must go out as is: Content-Encoding %q, Vary %q", rec.Header().Get("Content-Encoding"), rec.Header().Get("Vary"))
	}
	if got := rec.Header().Get("Cache-Control"); got != immutable {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func TestAsset_MissingFileIs404NotTheShell(t *testing.T) {
	rec := getAsset(t, "/assets/app-old.js", "br")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<title>") {
		t.Fatal("a missing asset must not be answered with the SPA shell")
	}
	if rec.Header().Get("Cache-Control") == immutable {
		t.Fatal("a 404 must not be cached as immutable")
	}
}

func TestShell_StillServedForPageAddresses(t *testing.T) {
	rec := getAsset(t, "/consent", "br")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>Acme SSO</title>") {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("the shell is filled per request and goes out uncompressed")
	}
}
