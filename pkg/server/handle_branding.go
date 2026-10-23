// Package server — handle_branding.go
// Public branding endpoints: the SPA config payload, the icon image, the
// sign-in page's background images and its daily wallpaper.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/branding"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/federation"
	"prohibitorum/pkg/wallpaper"
)

// wallpaperService is the slice of pkg/wallpaper the handlers use, so tests
// can answer without reaching Bing, Unsplash or Pixabay.
type wallpaperService interface {
	Bing(ctx context.Context, market string) (wallpaper.Picture, error)
	Unsplash(ctx context.Context, key, query string) ([]wallpaper.Picture, error)
	Verify(ctx context.Context, key string) error
	Pixabay(ctx context.Context, key, query, lang string) ([]wallpaper.Picture, error)
	VerifyPixabay(ctx context.Context, key string) error
	Image(ctx context.Context, source, id string) (wallpaper.Image, error)
	Forget()
}

// unsplashKeyName binds the sealed Unsplash key to its instance_settings
// column; pixabayKeyName does the same for Pixabay's.
const (
	unsplashKeyName = "unsplash_access_key"
	pixabayKeyName  = "pixabay_api_key"
)

// GET /api/prohibitorum/config (public)
func (s *Server) handleGetPublicConfigHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, etag, _ := s.branding.Icon(ctx)
	maintenance, maintenanceNotice := s.branding.Maintenance(ctx)
	var totpConfig contract.PublicTOTPConfig
	if s.config != nil {
		totpConfig = contract.PublicTOTPConfig{
			Issuer:    s.config.TOTP.Issuer,
			Algorithm: s.config.TOTP.DefaultAlgorithm,
			Digits:    s.config.TOTP.DefaultDigits,
			Period:    s.config.TOTP.DefaultPeriod,
		}
	}
	cfg := contract.PublicConfig{
		InstanceName:             s.branding.InstanceName(ctx),
		HasCustomIcon:            s.branding.HasCustomIcon(ctx),
		IconURL:                  "/branding/icon",
		IconEtag:                 etag,
		MaintenanceMode:          maintenance,
		MaintenanceMessages:      maintenanceNotice.Messages,
		MaintenanceDefaultLocale: maintenanceNotice.DefaultLocale,
		LoginAppearance:          s.branding.Appearance(ctx),
		LoginImages:              loginImageViews(s.branding.LoginImages(ctx)),
		TOTP:                     totpConfig,
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(cfg)
}

func loginImageViews(refs []branding.ImageRef) []contract.LoginImage {
	out := make([]contract.LoginImage, 0, len(refs))
	for _, ref := range refs {
		out = append(out, loginImageView(ref))
	}
	return out
}

func loginImageView(ref branding.ImageRef) contract.LoginImage {
	return contract.LoginImage{
		ID:   ref.ID,
		URL:  "/branding/login-images/" + strconv.FormatInt(ref.ID, 10),
		Etag: ref.Etag,
	}
}

// GET /branding/icon (public) — serves the effective icon with ETag/304.
func (s *Server) handleGetBrandingIconHTTP(w http.ResponseWriter, r *http.Request) {
	icon, etag, _ := s.branding.Icon(r.Context())
	writeIconResponse(w, r, icon, etag)
}

// GET /branding/login-images/{id} (public) — serves an uploaded sign-in
// background verbatim with ETag/304. A matching If-None-Match is answered from
// the cached image list without reading the bytes.
func (s *Server) handleGetLoginImageHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	etag, ok := s.branding.LoginImageEtag(r.Context(), id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("If-None-Match") == `"`+etag+`"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	data, etag, err := s.branding.LoginImage(r.Context(), id)
	if errors.Is(err, branding.ErrImageNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	writeIconResponse(w, r, data, etag)
}

// GET /branding/wallpaper (public) — the picture batch for the saved
// background source, from Bing, Unsplash or Pixabay.
func (s *Server) handleGetWallpaperHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bg := s.branding.Appearance(ctx).Background
	var (
		out contract.Wallpaper
		err error
	)
	switch bg.Source {
	case branding.SourceBing:
		out, err = s.bingWallpaper(ctx, bg.Bing.Market, bg.Bing.ShowCaption)
	case branding.SourceUnsplash:
		out, err = s.unsplashWallpaper(ctx, bg.Unsplash.Query)
		if ae := authn.AsAuthError(err); ae != nil && ae.Code == "unsplash_key_required" {
			err = authn.ErrWallpaperUnavailable()
		}
	case branding.SourcePixabay:
		out, err = s.pixabayWallpaper(ctx, bg.Pixabay.Query, bg.Pixabay.Lang)
		if ae := authn.AsAuthError(err); ae != nil && ae.Code == "pixabay_key_required" {
			err = authn.ErrWallpaperUnavailable()
		}
	default:
		err = authn.ErrWallpaperNotConfigured()
	}
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// GET /branding/wallpaper/images/{source}/{id} (public) — one picture of the
// source's cached batch, downloaded by the server (Pixabay's terms forbid
// hotlinking) and served as this site's own bytes. The id's shape is each
// source's; the service checks it.
func (s *Server) handleGetWallpaperImageHTTP(w http.ResponseWriter, r *http.Request) {
	img, err := s.wallpaper.Image(r.Context(), chi.URLParam(r, "source"), chi.URLParam(r, "id"))
	if errors.Is(err, wallpaper.ErrImageNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeAuthErr(w, authn.ErrWallpaperUnavailable())
		return
	}
	if r.Header.Get("If-None-Match") == `"`+img.ETag+`"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", img.ContentType)
	w.Header().Set("ETag", `"`+img.ETag+`"`)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(img.Data)
}

func (s *Server) bingWallpaper(ctx context.Context, market string, caption bool) (contract.Wallpaper, error) {
	pic, err := s.wallpaper.Bing(ctx, market)
	if err != nil {
		return contract.Wallpaper{}, authn.ErrWallpaperUnavailable()
	}
	out := contract.WallpaperPicture{ImageURL: pic.ImageURL}
	if caption {
		out.Title, out.Copyright, out.CopyrightURL = pic.Title, pic.Copyright, pic.CopyrightURL
	}
	return contract.Wallpaper{Source: branding.SourceBing, Pictures: []contract.WallpaperPicture{out}}, nil
}

// unsplashWallpaper uses the saved key and reports it as required when none is
// saved; the public endpoint shows that as the wallpaper being unavailable.
func (s *Server) unsplashWallpaper(ctx context.Context, query string) (contract.Wallpaper, error) {
	key, ok, err := s.openUnsplashKey(ctx)
	if err != nil {
		return contract.Wallpaper{}, err
	}
	if !ok {
		return contract.Wallpaper{}, authn.ErrUnsplashKeyRequired()
	}
	batch, err := s.wallpaper.Unsplash(ctx, key, query)
	if err != nil {
		return contract.Wallpaper{}, authn.ErrWallpaperUnavailable()
	}
	return pictureBatch(branding.SourceUnsplash, batch), nil
}

// pixabayWallpaper uses the saved key and the saved keyword and language; a
// missing key reads as the wallpaper being unavailable on the public page.
func (s *Server) pixabayWallpaper(ctx context.Context, query, lang string) (contract.Wallpaper, error) {
	key, ok, err := s.openPixabayKey(ctx)
	if err != nil {
		return contract.Wallpaper{}, err
	}
	if !ok {
		return contract.Wallpaper{}, authn.ErrPixabayKeyRequired()
	}
	batch, err := s.wallpaper.Pixabay(ctx, key, query, lang)
	if err != nil {
		return contract.Wallpaper{}, authn.ErrWallpaperUnavailable()
	}
	return pictureBatch(branding.SourcePixabay, batch), nil
}

// pictureBatch maps a service batch to the wire shape.
func pictureBatch(source string, batch []wallpaper.Picture) contract.Wallpaper {
	out := contract.Wallpaper{Source: source, Pictures: make([]contract.WallpaperPicture, len(batch))}
	for i, pic := range batch {
		out.Pictures[i] = contract.WallpaperPicture{
			ImageURL:        pic.ImageURL,
			Title:           pic.Title,
			Photographer:    pic.Photographer,
			PhotographerURL: pic.PhotographerURL,
			PhotoURL:        pic.PhotoURL,
		}
	}
	return out
}

// openUnsplashKey decrypts the saved Unsplash key; ok is false when none is saved.
func (s *Server) openUnsplashKey(ctx context.Context) (key string, ok bool, err error) {
	return s.openWallpaperKey(s.branding.UnsplashKey(ctx), unsplashKeyName)
}

// openPixabayKey decrypts the saved Pixabay key; ok is false when none is saved.
func (s *Server) openPixabayKey(ctx context.Context) (key string, ok bool, err error) {
	return s.openWallpaperKey(s.branding.PixabayKey(ctx), pixabayKeyName)
}

// openWallpaperKey decrypts a sealed picture-service key. name binds the
// sealed value to its column, so one service's key cannot be opened as
// another's.
func (s *Server) openWallpaperKey(sealed *branding.SealedKey, name string) (key string, ok bool, err error) {
	if sealed == nil {
		return "", false, nil
	}
	if s.config == nil {
		return "", false, errors.New("handle_branding: no data encryption keys configured")
	}
	dek, found := s.config.DataEncryptionKeys[int(sealed.KeyVersion)]
	if !found {
		return "", false, fmt.Errorf("handle_branding: %s sealed with an unknown key version", name)
	}
	plain, err := federation.OpenInstanceSecret(dek, federation.SealedSecret{
		Ciphertext: sealed.Ciphertext,
		Nonce:      sealed.Nonce,
		KeyVersion: sealed.KeyVersion,
	}, name)
	if err != nil {
		return "", false, err
	}
	return string(plain), true, nil
}
