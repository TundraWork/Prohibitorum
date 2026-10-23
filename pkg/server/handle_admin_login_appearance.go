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
	"context"
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

var (
	unsplashKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	pixabayKeyPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
)

// GET /api/prohibitorum/admin/settings/login-appearance
func (s *Server) handleGetLoginAppearanceHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(contract.AdminLoginAppearance{
		Appearance:     s.branding.Appearance(ctx),
		HasUnsplashKey: s.branding.UnsplashKey(ctx) != nil,
		HasPixabayKey:  s.branding.PixabayKey(ctx) != nil,
	})
}

// PUT /api/prohibitorum/admin/settings/login-appearance
//
//	{"appearance": {...}, "unsplashAccessKey": "...", "pixabayApiKey": "..."}
//
// All fields are decoded strictly. Each key is optional: left out, the saved
// one stays. A new key is checked with its service before it is sealed; both
// keys may be given at once and are each verified.
func (s *Server) handlePutLoginAppearanceHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		Appearance        json.RawMessage `json:"appearance"`
		UnsplashAccessKey *string         `json:"unsplashAccessKey"`
		PixabayAPIKey     *string         `json:"pixabayApiKey"`
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
	if body.PixabayAPIKey != nil && !pixabayKeyPattern.MatchString(*body.PixabayAPIKey) {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.UnsplashAccessKey == nil && appearance.Background.Source == branding.SourceUnsplash &&
		s.branding.UnsplashKey(ctx) == nil {
		writeAuthErr(w, authn.ErrUnsplashKeyRequired())
		return
	}
	if body.PixabayAPIKey == nil && appearance.Background.Source == branding.SourcePixabay &&
		s.branding.PixabayKey(ctx) == nil {
		writeAuthErr(w, authn.ErrPixabayKeyRequired())
		return
	}

	keys := branding.NewKeys{}
	if body.UnsplashAccessKey != nil {
		sealed, err := s.verifyAndSealKey(ctx, *body.UnsplashAccessKey, unsplashKeyName,
			s.wallpaper.Verify, wallpaper.ErrKeyRejected, authn.ErrUnsplashKeyInvalid(), authn.ErrUnsplashUnreachable())
		if err != nil {
			writeAuthErr(w, err)
			return
		}
		keys.Unsplash = sealed
	}
	if body.PixabayAPIKey != nil {
		sealed, err := s.verifyAndSealKey(ctx, *body.PixabayAPIKey, pixabayKeyName,
			s.wallpaper.VerifyPixabay, wallpaper.ErrPixabayKeyRejected, authn.ErrPixabayKeyInvalid(), authn.ErrPixabayUnreachable())
		if err != nil {
			writeAuthErr(w, err)
			return
		}
		keys.Pixabay = sealed
	}

	if err := s.branding.SetAppearance(ctx, appearance, keys); err != nil {
		switch {
		case errors.Is(err, branding.ErrUnsplashKeyRequired):
			writeAuthErr(w, authn.ErrUnsplashKeyRequired())
		case errors.Is(err, branding.ErrPixabayKeyRequired):
			writeAuthErr(w, authn.ErrPixabayKeyRequired())
		default:
			writeAuthErr(w, err)
		}
		return
	}
	// A new keyword or key applies on the next wallpaper request.
	s.wallpaper.Forget()
	s.auditBranding(r, "login_appearance_updated")
	w.WriteHeader(http.StatusNoContent)
}

// verifyAndSealKey checks a key with its service and seals it with the
// current data encryption key. rejected is for a key the service refuses and
// unreachable for one that could not be checked.
func (s *Server) verifyAndSealKey(ctx context.Context, key, name string, verify func(context.Context, string) error,
	rejected error, rejectedErr, unreachableErr *authn.AuthError) (*branding.SealedKey, error) {
	if err := verify(ctx, key); err != nil {
		if errors.Is(err, rejected) {
			return nil, rejectedErr
		}
		return nil, unreachableErr
	}
	version, dek, err := s.currentDEK()
	if err != nil {
		return nil, err
	}
	secret, err := federation.SealInstanceSecret(dek, []byte(key), name, version)
	if err != nil {
		return nil, err
	}
	return &branding.SealedKey{Ciphertext: secret.Ciphertext, Nonce: secret.Nonce, KeyVersion: secret.KeyVersion}, nil
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

// DELETE /api/prohibitorum/admin/settings/login-appearance/pixabay-key
// Refused while the saved background source is Pixabay.
func (s *Server) handleDeletePixabayKeyHTTP(w http.ResponseWriter, r *http.Request) {
	if err := s.branding.ClearPixabayKey(r.Context()); err != nil {
		if errors.Is(err, branding.ErrPixabayKeyInUse) {
			writeAuthErr(w, authn.ErrPixabayKeyInUse())
			return
		}
		writeAuthErr(w, err)
		return
	}
	s.auditBranding(r, "pixabay_key_removed")
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/prohibitorum/admin/settings/login-appearance/wallpaper
//
//	?source=bing&market=<market>
//	?source=unsplash&query=<q>
//	?source=pixabay&query=<q>&lang=<lang>
//	?source=openverse&query=<q>
//
// The settings preview's wallpaper for draft parameters, before they are
// saved. Bing's caption is always included; Unsplash and Pixabay use the
// saved key, and Pixabay searches with the draft's keyword and language;
// Openverse is anonymous and searches with the draft's keyword, which it
// requires.
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
		if !branding.ValidWallpaperQuery(query) {
			writeAuthErr(w, authn.ErrBadRequest())
			return
		}
		out, err = s.unsplashWallpaper(ctx, query)
	case branding.SourcePixabay:
		query, lang := q.Get("query"), q.Get("lang")
		if !branding.ValidWallpaperQuery(query) || !branding.ValidPixabayLang(lang) {
			writeAuthErr(w, authn.ErrBadRequest())
			return
		}
		out, err = s.pixabayWallpaper(ctx, query, lang)
	case branding.SourceOpenverse:
		query := q.Get("query")
		if !branding.ValidWallpaperQuery(query) || query == "" {
			writeAuthErr(w, authn.ErrBadRequest())
			return
		}
		out, err = s.openverseWallpaper(ctx, query)
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
