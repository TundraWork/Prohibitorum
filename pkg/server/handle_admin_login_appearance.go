// Package server — handle_admin_login_appearance.go
//
// Admin settings for the sign-in page: its appearance (background source and
// surface style), the Unsplash access key, the draft wallpaper preview and the
// uploaded background images. JSON writes go through registerAdminBodyOpHTTP;
// the image upload uses plain registerOpHTTP(admin), like the icon upload,
// because the admin body controls only accept small JSON bodies. None of these
// need sudo.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"

	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/branding"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/federation"
	"prohibitorum/pkg/wallpaper"
)

var unsplashKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// GET /api/prohibitorum/admin/settings/login-appearance
func (s *Server) handleGetLoginAppearanceHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(contract.AdminLoginAppearance{
		Appearance:     s.branding.Appearance(ctx),
		HasUnsplashKey: s.branding.UnsplashKey(ctx) != nil,
	})
}

// PUT /api/prohibitorum/admin/settings/login-appearance
//
//	{"appearance": {...}, "unsplashAccessKey": "..."}
//
// Both fields are decoded strictly. unsplashAccessKey is optional: left out,
// the saved key stays. A new key is checked with Unsplash before it is sealed.
func (s *Server) handlePutLoginAppearanceHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		Appearance        json.RawMessage `json:"appearance"`
		UnsplashAccessKey *string         `json:"unsplashAccessKey"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || dec.More() || len(body.Appearance) == 0 {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	appearance, err := branding.DecodeAppearance(body.Appearance)
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.UnsplashAccessKey != nil && !unsplashKeyPattern.MatchString(*body.UnsplashAccessKey) {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.UnsplashAccessKey == nil && appearance.Background.Source == branding.SourceUnsplash &&
		s.branding.UnsplashKey(ctx) == nil {
		writeAuthErr(w, authn.ErrUnsplashKeyRequired())
		return
	}

	var sealed *branding.SealedKey
	if body.UnsplashAccessKey != nil {
		if err := s.wallpaper.Verify(ctx, *body.UnsplashAccessKey); err != nil {
			if errors.Is(err, wallpaper.ErrKeyRejected) {
				writeAuthErr(w, authn.ErrUnsplashKeyInvalid())
				return
			}
			writeAuthErr(w, authn.ErrUnsplashUnreachable())
			return
		}
		version, dek, err := s.currentDEK()
		if err != nil {
			writeAuthErr(w, err)
			return
		}
		secret, err := federation.SealInstanceSecret(dek, []byte(*body.UnsplashAccessKey), unsplashKeyName, version)
		if err != nil {
			writeAuthErr(w, err)
			return
		}
		sealed = &branding.SealedKey{Ciphertext: secret.Ciphertext, Nonce: secret.Nonce, KeyVersion: secret.KeyVersion}
	}

	if err := s.branding.SetAppearance(ctx, appearance, sealed); err != nil {
		if errors.Is(err, branding.ErrUnsplashKeyRequired) {
			writeAuthErr(w, authn.ErrUnsplashKeyRequired())
			return
		}
		writeAuthErr(w, err)
		return
	}
	// A new keyword or key applies on the next wallpaper request.
	s.wallpaper.Forget()
	s.auditBranding(r, "login_appearance_updated")
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/prohibitorum/admin/settings/login-appearance/unsplash-key
// Refused while the saved background source is Unsplash.
func (s *Server) handleDeleteUnsplashKeyHTTP(w http.ResponseWriter, r *http.Request) {
	if err := s.branding.ClearUnsplashKey(r.Context()); err != nil {
		if errors.Is(err, branding.ErrUnsplashKeyInUse) {
			writeAuthErr(w, authn.ErrUnsplashKeyInUse())
			return
		}
		writeAuthErr(w, err)
		return
	}
	s.wallpaper.Forget()
	s.auditBranding(r, "unsplash_key_removed")
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/prohibitorum/admin/settings/login-appearance/wallpaper
//
//	?source=bing&market=<market>
//	?source=unsplash&query=<q>
//
// The settings preview's wallpaper for draft parameters, before they are
// saved. Bing's caption is always included; Unsplash uses the saved key.
func (s *Server) handleGetWallpaperPreviewHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	var (
		out contract.Wallpaper
		err error
	)
	switch q.Get("source") {
	case branding.SourceBing:
		market := q.Get("market")
		if !branding.ValidBingMarket(market) {
			writeAuthErr(w, authn.ErrBadRequest())
			return
		}
		out, err = s.bingWallpaper(ctx, market, true)
	case branding.SourceUnsplash:
		query := q.Get("query")
		if !branding.ValidUnsplashQuery(query) {
			writeAuthErr(w, authn.ErrBadRequest())
			return
		}
		out, err = s.unsplashWallpaper(ctx, query)
	default:
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// POST /api/prohibitorum/admin/settings/login-images  (raw image body, up to 5 MiB)
// The image is validated but stored and served byte-for-byte.
func (s *Server) handlePostLoginImageHTTP(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxIconRead))
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	ref, err := s.branding.AddLoginImage(r.Context(), raw)
	switch {
	case errors.Is(err, branding.ErrTooLarge):
		writeAvatarErr(w, "avatar_too_large", "login image: image exceeds 5 MiB")
		return
	case errors.Is(err, branding.ErrInvalidImage):
		writeAvatarErr(w, "avatar_invalid_image", "login image: invalid or unsupported image format")
		return
	case errors.Is(err, branding.ErrImageLimit):
		writeAuthErr(w, authn.ErrLoginImagesFull())
		return
	case err != nil:
		writeAuthErr(w, err)
		return
	}
	s.auditBranding(r, "login_image_added", map[string]any{"imageId": ref.ID})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(loginImageView(ref))
}

// DELETE /api/prohibitorum/admin/settings/login-images/{id}
// Removing the last image is allowed even while the source is images; the
// sign-in page then shows its own background.
func (s *Server) handleDeleteLoginImageHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeAuthErr(w, authn.ErrLoginImageNotFound())
		return
	}
	if err := s.branding.DeleteLoginImage(r.Context(), id); err != nil {
		if errors.Is(err, branding.ErrImageNotFound) {
			writeAuthErr(w, authn.ErrLoginImageNotFound())
			return
		}
		writeAuthErr(w, err)
		return
	}
	s.auditBranding(r, "login_image_removed", map[string]any{"imageId": id})
	w.WriteHeader(http.StatusNoContent)
}
