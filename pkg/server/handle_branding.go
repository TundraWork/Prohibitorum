// Package server — handle_branding.go
// Public branding endpoints: the SPA config payload, the icon image, the
// sign-in page's background images and its daily wallpaper.
package server

import (
	"context"
	"encoding/json"
	"errors"
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
// can answer without reaching Bing or Unsplash.
type wallpaperService interface {
	Bing(ctx context.Context, market string) (wallpaper.Picture, error)
	Unsplash(ctx context.Context, key, query string) (wallpaper.Picture, error)
	Verify(ctx context.Context, key string) error
	Forget()
}

// unsplashKeyName binds the sealed Unsplash key to its instance_settings column.
const unsplashKeyName = "unsplash_access_key"

// GET /api/prohibitorum/config (public)
func (s *Server) handleGetPublicConfigHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, etag, _ := s.branding.Icon(ctx)
	maintenance, maintenanceMsg := s.branding.Maintenance(ctx)
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
		InstanceName:       s.branding.InstanceName(ctx),
		HasCustomIcon:      s.branding.HasCustomIcon(ctx),
		IconURL:            "/branding/icon",
		IconEtag:           etag,
		MaintenanceMode:    maintenance,
		MaintenanceMessage: maintenanceMsg,
		LoginAppearance:    s.branding.Appearance(ctx),
		LoginImages:        loginImageViews(s.branding.LoginImages(ctx)),
		TOTP:               totpConfig,
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

// GET /branding/wallpaper (public) — the Bing or Unsplash wallpaper for the
// saved background source.
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

func (s *Server) bingWallpaper(ctx context.Context, market string, caption bool) (contract.Wallpaper, error) {
	pic, err := s.wallpaper.Bing(ctx, market)
	if err != nil {
		return contract.Wallpaper{}, authn.ErrWallpaperUnavailable()
	}
	out := contract.Wallpaper{Source: branding.SourceBing, ImageURL: pic.ImageURL}
	if caption {
		out.Title, out.Copyright, out.CopyrightURL = pic.Title, pic.Copyright, pic.CopyrightURL
	}
	return out, nil
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
	pic, err := s.wallpaper.Unsplash(ctx, key, query)
	if err != nil {
		return contract.Wallpaper{}, authn.ErrWallpaperUnavailable()
	}
	return contract.Wallpaper{
		Source:          branding.SourceUnsplash,
		ImageURL:        pic.ImageURL,
		Photographer:    pic.Photographer,
		PhotographerURL: pic.PhotographerURL,
		PhotoURL:        pic.PhotoURL,
	}, nil
}

// openUnsplashKey decrypts the saved Unsplash key; ok is false when none is saved.
func (s *Server) openUnsplashKey(ctx context.Context) (key string, ok bool, err error) {
	sealed := s.branding.UnsplashKey(ctx)
	if sealed == nil {
		return "", false, nil
	}
	if s.config == nil {
		return "", false, errors.New("handle_branding: no data encryption keys configured")
	}
	dek, found := s.config.DataEncryptionKeys[int(sealed.KeyVersion)]
	if !found {
		return "", false, errors.New("handle_branding: Unsplash key sealed with an unknown key version")
	}
	plain, err := federation.OpenInstanceSecret(dek, federation.SealedSecret{
		Ciphertext: sealed.Ciphertext,
		Nonce:      sealed.Nonce,
		KeyVersion: sealed.KeyVersion,
	}, unsplashKeyName)
	if err != nil {
		return "", false, err
	}
	return string(plain), true, nil
}
