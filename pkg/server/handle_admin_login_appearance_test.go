// Package server — handle_admin_login_appearance_test.go
//
// DB-free tests for the sign-in page endpoints, served through the real router
// so route registration, admin guards and path parameters are exercised too.
// The store and the wallpaper service are in-memory fakes.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/branding"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/federation"
	"prohibitorum/pkg/wallpaper"
)

type appearanceStore struct {
	fakeBrandingStore
	appearance *branding.Appearance
	key        *branding.SealedKey
	images     []appearanceImage
	nextID     int64
}

type appearanceImage struct {
	ref  branding.ImageRef
	data []byte
}

func (f *appearanceStore) Get(ctx context.Context) (branding.Settings, error) {
	s, _ := f.fakeBrandingStore.Get(ctx)
	s.Appearance, s.UnsplashKey = f.appearance, f.key
	for _, img := range f.images {
		s.Images = append(s.Images, img.ref)
	}
	return s, nil
}

func (f *appearanceStore) SetAppearance(_ context.Context, a branding.Appearance, key *branding.SealedKey) error {
	if key != nil {
		f.key = key
	} else if a.Background.Source == branding.SourceUnsplash && f.key == nil {
		return branding.ErrUnsplashKeyRequired
	}
	f.appearance = &a
	return nil
}

func (f *appearanceStore) ClearUnsplashKey(context.Context) error {
	if f.appearance != nil && f.appearance.Background.Source == branding.SourceUnsplash {
		return branding.ErrUnsplashKeyInUse
	}
	f.key = nil
	return nil
}

func (f *appearanceStore) AddImage(_ context.Context, data []byte, etag string) (branding.ImageRef, error) {
	if len(f.images) >= branding.MaxLoginImages {
		return branding.ImageRef{}, branding.ErrImageLimit
	}
	f.nextID++
	ref := branding.ImageRef{ID: f.nextID, Etag: etag}
	f.images = append(f.images, appearanceImage{ref: ref, data: data})
	return ref, nil
}

func (f *appearanceStore) DeleteImage(_ context.Context, id int64) error {
	for i, img := range f.images {
		if img.ref.ID == id {
			f.images = append(f.images[:i], f.images[i+1:]...)
			return nil
		}
	}
	return branding.ErrImageNotFound
}

func (f *appearanceStore) ImageData(_ context.Context, id int64) ([]byte, string, error) {
	for _, img := range f.images {
		if img.ref.ID == id {
			return img.data, img.ref.Etag, nil
		}
	}
	return nil, "", branding.ErrImageNotFound
}

type fakeWallpaper struct {
	bing        wallpaper.Picture
	bingErr     error
	unsplash    wallpaper.Picture
	unsplashErr error
	verifyErr   error
	forgot      int
	lastKey     string
	lastQuery   string
	lastMarket  string
}

func (f *fakeWallpaper) Bing(_ context.Context, market string) (wallpaper.Picture, error) {
	f.lastMarket = market
	return f.bing, f.bingErr
}

func (f *fakeWallpaper) Unsplash(_ context.Context, key, query string) (wallpaper.Picture, error) {
	f.lastKey, f.lastQuery = key, query
	return f.unsplash, f.unsplashErr
}

func (f *fakeWallpaper) Verify(_ context.Context, key string) error {
	f.lastKey = key
	return f.verifyErr
}

func (f *fakeWallpaper) Forget() { f.forgot++ }

var testDEK = bytes.Repeat([]byte{0x5a}, 32)

type loginAppearanceHarness struct {
	router   *chi.Mux
	store    *appearanceStore
	resolver *branding.Resolver
	wp       *fakeWallpaper
}

func newLoginAppearanceHarness(t *testing.T) *loginAppearanceHarness {
	t.Helper()
	router, s := realAdminOnlyRouter(t)
	h := &loginAppearanceHarness{
		router: router,
		store:  &appearanceStore{},
		wp: &fakeWallpaper{
			bing: wallpaper.Picture{
				ImageURL: "https://www.bing.com/th?id=OHR.X_UHD.jpg&w=2560", Title: "Title",
				Copyright: "Copyright", CopyrightURL: "https://www.bing.com/search?q=x",
			},
			unsplash: wallpaper.Picture{
				ImageURL: "https://images.unsplash.com/photo-1?w=2560", Photographer: "Jane",
				PhotographerURL: "https://unsplash.com/@jane?utm_source=prohibitorum&utm_medium=referral",
				PhotoURL:        "https://unsplash.com/photos/1?utm_source=prohibitorum&utm_medium=referral",
			},
		},
	}
	h.resolver = branding.NewWithStore("TestCo", h.store)
	s.branding = h.resolver
	s.wallpaper = h.wp
	s.Audit = noopAuditWriter{}
	s.config = &configx.Config{DataEncryptionKeys: map[int][]byte{1: testDEK}}
	return h
}

// do serves one request as a fresh-sudo admin, or anonymously when sess is nil.
func (h *loginAppearanceHarness) do(method, path, body, contentType string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	h.router.ServeHTTP(rr, reqWithSession(method, path, body, contentType, adminSession(time.Now().Add(time.Hour))))
	return rr
}

func (h *loginAppearanceHarness) anonymous(method, path string, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	rr := httptest.NewRecorder()
	h.router.ServeHTTP(rr, req)
	return rr
}

func appearanceBody(t *testing.T, a branding.Appearance, key *string) string {
	t.Helper()
	body := map[string]any{"appearance": a}
	if key != nil {
		body["unsplashAccessKey"] = *key
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func wantCode(t *testing.T, rr *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rr.Code != status {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, status, rr.Body.String())
	}
	if code != "" && !strings.Contains(rr.Body.String(), `"code":"`+code+`"`) {
		t.Fatalf("body = %s, want code %s", rr.Body.String(), code)
	}
}

const appearancePath = "/api/prohibitorum/admin/settings/login-appearance"

func TestLoginAppearance_PutStrictDecoding(t *testing.T) {
	h := newLoginAppearanceHarness(t)
	valid := appearanceBody(t, branding.DefaultAppearance(), nil)
	missingBlur := strings.Replace(valid, `"blur":true,`, ``, 1)
	if missingBlur == valid {
		missingBlur = strings.Replace(valid, `,"blur":true`, ``, 1)
	}
	for name, body := range map[string]string{
		"unknown top-level field":  strings.Replace(valid, `{"appearance"`, `{"extra":1,"appearance"`, 1),
		"unknown appearance field": strings.Replace(valid, `"card":{`, `"card":{"shadow":true,`, 1),
		"missing field":            missingBlur,
		"missing appearance":       `{}`,
		"null appearance":          `{"appearance":null}`,
		"uppercase color":          strings.Replace(valid, `#1f6f8b`, `#1F6F8B`, 1),
		"malformed key":            appearanceBody(t, branding.DefaultAppearance(), ptr("has space")),
		"empty key":                appearanceBody(t, branding.DefaultAppearance(), ptr("")),
		"trailing data":            valid + `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			wantCode(t, h.do("PUT", appearancePath, body, ""), http.StatusBadRequest, "bad_request")
		})
	}
	if h.store.appearance != nil {
		t.Fatal("a rejected body reached the store")
	}

	a := branding.DefaultAppearance()
	a.Background.Source = branding.SourceImages
	a.Background.Images = branding.ImageOptions{Order: "carousel", IntervalSeconds: 15}
	a.Card = branding.Surface{Translucent: true, Opacity: 60, Blur: true}
	a.CardPosition = branding.CardLeft
	a.Theme = branding.ThemeDark
	saved := appearanceBody(t, a, nil)
	wantCode(t, h.do("PUT", appearancePath, saved, ""), http.StatusNoContent, "")
	if h.wp.forgot != 1 {
		t.Fatalf("Forget called %d times, want 1", h.wp.forgot)
	}

	// A document without a theme is refused and the saved one stays.
	missingTheme := strings.Replace(saved, `,"theme":"dark"`, ``, 1)
	if missingTheme == saved {
		t.Fatalf("body %s has no theme to drop", saved)
	}
	wantCode(t, h.do("PUT", appearancePath, missingTheme, ""), http.StatusBadRequest, "bad_request")
	if *h.store.appearance != a {
		t.Fatalf("stored = %+v after a rejected PUT, want %+v", *h.store.appearance, a)
	}

	rr := h.do("GET", appearancePath, "", "")
	wantCode(t, rr, http.StatusOK, "")
	var got struct {
		Appearance     branding.Appearance `json:"appearance"`
		HasUnsplashKey bool                `json:"hasUnsplashKey"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Appearance != a || got.HasUnsplashKey {
		t.Fatalf("GET = %+v, want the saved appearance and no key", got)
	}

	var cfg struct {
		LoginAppearance branding.Appearance `json:"loginAppearance"`
	}
	if err := json.Unmarshal(h.anonymous("GET", "/api/prohibitorum/config", nil).Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.LoginAppearance != a {
		t.Fatalf("/config appearance = %+v, want %+v", cfg.LoginAppearance, a)
	}
}

func ptr[T any](v T) *T { return &v }

func TestLoginAppearance_UnsplashKey(t *testing.T) {
	h := newLoginAppearanceHarness(t)
	a := branding.DefaultAppearance()
	a.Background.Source = branding.SourceUnsplash

	wantCode(t, h.do("PUT", appearancePath, appearanceBody(t, a, nil), ""), http.StatusBadRequest, "unsplash_key_required")

	h.wp.verifyErr = wallpaper.ErrKeyRejected
	wantCode(t, h.do("PUT", appearancePath, appearanceBody(t, a, ptr("bad-key")), ""), http.StatusBadRequest, "unsplash_key_invalid")
	h.wp.verifyErr = fmt.Errorf("%w: timeout", wallpaper.ErrUnavailable)
	wantCode(t, h.do("PUT", appearancePath, appearanceBody(t, a, ptr("any-key")), ""), http.StatusBadGateway, "unsplash_unreachable")
	if h.store.key != nil || h.store.appearance != nil {
		t.Fatal("a key Unsplash did not accept was saved")
	}

	h.wp.verifyErr = nil
	wantCode(t, h.do("PUT", appearancePath, appearanceBody(t, a, ptr("Good_key-1")), ""), http.StatusNoContent, "")
	if h.store.key == nil {
		t.Fatal("key not saved")
	}
	plain, err := federation.OpenInstanceSecret(testDEK, federation.SealedSecret{
		Ciphertext: h.store.key.Ciphertext, Nonce: h.store.key.Nonce, KeyVersion: h.store.key.KeyVersion,
	}, unsplashKeyName)
	if err != nil || string(plain) != "Good_key-1" {
		t.Fatalf("sealed key opens to %q, err %v", plain, err)
	}
	if strings.Contains(h.do("GET", appearancePath, "", "").Body.String(), "Good_key-1") {
		t.Fatal("GET echoed the key")
	}

	// Saving again without a key keeps the saved one.
	wantCode(t, h.do("PUT", appearancePath, appearanceBody(t, a, nil), ""), http.StatusNoContent, "")

	keyPath := appearancePath + "/unsplash-key"
	wantCode(t, h.do("DELETE", keyPath, "", ""), http.StatusConflict, "unsplash_key_in_use")
	a.Background.Source = branding.SourceNone
	wantCode(t, h.do("PUT", appearancePath, appearanceBody(t, a, nil), ""), http.StatusNoContent, "")
	wantCode(t, h.do("DELETE", keyPath, "", ""), http.StatusNoContent, "")
	if h.store.key != nil {
		t.Fatal("key survived DELETE")
	}
	// Removing a key that is not there is still a success.
	wantCode(t, h.do("DELETE", keyPath, "", ""), http.StatusNoContent, "")
}

func pngOfSize(t *testing.T, w int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, 9))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestLoginImages_UploadServeDelete(t *testing.T) {
	h := newLoginAppearanceHarness(t)
	const imagesPath = "/api/prohibitorum/admin/settings/login-images"

	var first struct {
		ID   int64  `json:"id"`
		URL  string `json:"url"`
		Etag string `json:"etag"`
	}
	raw := pngOfSize(t, 16)
	rr := h.do("POST", imagesPath, string(raw), "image/png")
	wantCode(t, rr, http.StatusCreated, "")
	if err := json.Unmarshal(rr.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first.URL != fmt.Sprintf("/branding/login-images/%d", first.ID) || first.Etag == "" {
		t.Fatalf("created = %+v", first)
	}

	served := h.anonymous("GET", first.URL, nil)
	wantCode(t, served, http.StatusOK, "")
	if !bytes.Equal(served.Body.Bytes(), raw) {
		t.Fatal("served bytes differ from the upload — images must be verbatim")
	}
	if served.Header().Get("Cache-Control") != "public, max-age=300" || served.Header().Get("ETag") != `"`+first.Etag+`"` {
		t.Fatalf("headers = %v", served.Header())
	}
	notModified := h.anonymous("GET", first.URL, http.Header{"If-None-Match": {`"` + first.Etag + `"`}})
	wantCode(t, notModified, http.StatusNotModified, "")

	wantCode(t, h.do("POST", imagesPath, "not an image", "image/png"), http.StatusBadRequest, "avatar_invalid_image")
	for i := 2; i <= branding.MaxLoginImages; i++ {
		wantCode(t, h.do("POST", imagesPath, string(pngOfSize(t, 16+i)), "image/png"), http.StatusCreated, "")
	}
	wantCode(t, h.do("POST", imagesPath, string(pngOfSize(t, 40)), "image/png"), http.StatusConflict, "login_images_full")

	cfg := h.anonymous("GET", "/api/prohibitorum/config", nil)
	var parsed struct {
		LoginImages []struct {
			ID int64 `json:"id"`
		} `json:"loginImages"`
	}
	if err := json.Unmarshal(cfg.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.LoginImages) != branding.MaxLoginImages || parsed.LoginImages[0].ID != first.ID {
		t.Fatalf("/config images = %+v, want all ten in upload order", parsed.LoginImages)
	}

	wantCode(t, h.do("DELETE", imagesPath+"/999", "", ""), http.StatusNotFound, "login_image_not_found")
	wantCode(t, h.do("DELETE", imagesPath+"/abc", "", ""), http.StatusNotFound, "login_image_not_found")
	wantCode(t, h.do("DELETE", fmt.Sprintf("%s/%d", imagesPath, first.ID), "", ""), http.StatusNoContent, "")
	wantCode(t, h.anonymous("GET", first.URL, nil), http.StatusNotFound, "")
	wantCode(t, h.anonymous("GET", "/branding/login-images/0", nil), http.StatusNotFound, "")
	wantCode(t, h.anonymous("GET", "/branding/login-images/x", nil), http.StatusNotFound, "")
}

func TestLoginImages_UploadNeedsFreshSudo(t *testing.T) {
	h := newLoginAppearanceHarness(t)
	rr := httptest.NewRecorder()
	h.router.ServeHTTP(rr, reqWithSession("POST", "/api/prohibitorum/admin/settings/login-images",
		string(pngOfSize(t, 8)), "image/png", adminSession(time.Time{})))
	wantCode(t, rr, http.StatusUnauthorized, "sudo_required")
	if len(h.store.images) != 0 {
		t.Fatal("upload without sudo was stored")
	}
}

func TestLoginAppearance_AdminOnly(t *testing.T) {
	h := newLoginAppearanceHarness(t)
	for _, r := range []struct{ method, path, body, ct string }{
		{"GET", appearancePath, "", ""},
		{"GET", appearancePath + "/wallpaper?source=bing&market=zh-CN", "", ""},
		{"POST", "/api/prohibitorum/admin/settings/login-images", string(pngOfSize(t, 8)), "image/png"},
		{"PUT", appearancePath, appearanceBody(t, branding.DefaultAppearance(), nil), ""},
		{"DELETE", "/api/prohibitorum/admin/settings/login-images/1", "", ""},
	} {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			h.router.ServeHTTP(rr, reqWithSession(r.method, r.path, r.body, r.ct, sessionForRole("user")))
			if rr.Code != http.StatusForbidden {
				t.Fatalf("member: status = %d, want 403; body: %s", rr.Code, rr.Body.String())
			}
		})
	}
}

func (h *loginAppearanceHarness) save(t *testing.T, edit func(*branding.Appearance)) {
	t.Helper()
	a := branding.DefaultAppearance()
	edit(&a)
	h.store.appearance = &a
	h.store.key = nil
	if a.Background.Source == branding.SourceUnsplash {
		sealed, err := federation.SealInstanceSecret(testDEK, []byte("saved-key"), unsplashKeyName, 1)
		if err != nil {
			t.Fatal(err)
		}
		h.store.key = &branding.SealedKey{Ciphertext: sealed.Ciphertext, Nonce: sealed.Nonce, KeyVersion: 1}
	}
	h.resolver.Invalidate()
}

// dropKey removes the saved key behind the handlers' back.
func (h *loginAppearanceHarness) dropKey() {
	h.store.key = nil
	h.resolver.Invalidate()
}

func TestPublicWallpaper(t *testing.T) {
	h := newLoginAppearanceHarness(t)

	rr := h.anonymous("GET", "/branding/wallpaper", nil)
	wantCode(t, rr, http.StatusNotFound, "wallpaper_not_configured")
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", rr.Header().Get("Cache-Control"))
	}

	h.save(t, func(a *branding.Appearance) {
		a.Background.Source = branding.SourceBing
		a.Background.Bing = branding.BingOptions{Market: "ja-JP", ShowCaption: true}
	})
	rr = h.anonymous("GET", "/branding/wallpaper", nil)
	wantCode(t, rr, http.StatusOK, "")
	if h.wp.lastMarket != "ja-JP" || !strings.Contains(rr.Body.String(), `"title":"Title"`) ||
		!strings.Contains(rr.Body.String(), `"source":"bing"`) {
		t.Fatalf("bing wallpaper = %s (market %s)", rr.Body.String(), h.wp.lastMarket)
	}

	h.save(t, func(a *branding.Appearance) {
		a.Background.Source = branding.SourceBing
		a.Background.Bing.ShowCaption = false
	})
	rr = h.anonymous("GET", "/branding/wallpaper", nil)
	wantCode(t, rr, http.StatusOK, "")
	for _, hidden := range []string{"title", "copyright", "copyrightUrl"} {
		if strings.Contains(rr.Body.String(), `"`+hidden+`"`) {
			t.Fatalf("caption hidden, but %s present: %s", hidden, rr.Body.String())
		}
	}
	h.wp.bingErr = wallpaper.ErrUnavailable
	wantCode(t, h.anonymous("GET", "/branding/wallpaper", nil), http.StatusServiceUnavailable, "wallpaper_unavailable")

	h.save(t, func(a *branding.Appearance) {
		a.Background.Source = branding.SourceUnsplash
		a.Background.Unsplash.Query = "sea"
	})
	rr = h.anonymous("GET", "/branding/wallpaper", nil)
	wantCode(t, rr, http.StatusOK, "")
	if h.wp.lastKey != "saved-key" || h.wp.lastQuery != "sea" || !strings.Contains(rr.Body.String(), `"photographer":"Jane"`) {
		t.Fatalf("unsplash wallpaper = %s (key %q query %q)", rr.Body.String(), h.wp.lastKey, h.wp.lastQuery)
	}
	h.wp.unsplashErr = wallpaper.ErrKeyRejected
	wantCode(t, h.anonymous("GET", "/branding/wallpaper", nil), http.StatusServiceUnavailable, "wallpaper_unavailable")
	h.dropKey()
	wantCode(t, h.anonymous("GET", "/branding/wallpaper", nil), http.StatusServiceUnavailable, "wallpaper_unavailable")
}

func TestWallpaperPreview(t *testing.T) {
	h := newLoginAppearanceHarness(t)
	previewPath := appearancePath + "/wallpaper"

	for _, q := range []string{"", "?source=gradient", "?source=bing&market=xx-XX", "?source=unsplash&query=%20sea"} {
		wantCode(t, h.do("GET", previewPath+q, "", ""), http.StatusBadRequest, "bad_request")
	}

	// The saved caption setting does not hide the preview's caption.
	h.save(t, func(a *branding.Appearance) { a.Background.Bing.ShowCaption = false })
	rr := h.do("GET", previewPath+"?source=bing&market=en-GB", "", "")
	wantCode(t, rr, http.StatusOK, "")
	if h.wp.lastMarket != "en-GB" || !strings.Contains(rr.Body.String(), `"copyright":"Copyright"`) {
		t.Fatalf("preview = %s (market %s)", rr.Body.String(), h.wp.lastMarket)
	}

	h.dropKey()
	wantCode(t, h.do("GET", previewPath+"?source=unsplash&query=sea", "", ""), http.StatusBadRequest, "unsplash_key_required")
	h.save(t, func(a *branding.Appearance) { a.Background.Source = branding.SourceUnsplash })
	rr = h.do("GET", previewPath+"?source=unsplash&query=forest", "", "")
	wantCode(t, rr, http.StatusOK, "")
	if h.wp.lastQuery != "forest" {
		t.Fatalf("preview query = %q, want the draft's", h.wp.lastQuery)
	}
	h.wp.unsplashErr = wallpaper.ErrUnavailable
	wantCode(t, h.do("GET", previewPath+"?source=unsplash&query=forest", "", ""), http.StatusServiceUnavailable, "wallpaper_unavailable")
}
