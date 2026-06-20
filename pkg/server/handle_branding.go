// Package server — handle_branding.go
// Public branding endpoints: the SPA config payload and the icon image.
package server

import (
	"encoding/json"
	"net/http"

	"prohibitorum/pkg/contract"
)

// GET /api/prohibitorum/config (public)
func (s *Server) handleGetPublicConfigHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, etag, _ := s.branding.Icon(ctx)
	cfg := contract.PublicConfig{
		InstanceName:  s.branding.InstanceName(ctx),
		HasCustomIcon: s.branding.HasCustomIcon(ctx),
		IconURL:       "/branding/icon",
		IconEtag:      etag,
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(cfg)
}

// GET /branding/icon (public) — serves the effective icon PNG with ETag/304.
func (s *Server) handleGetBrandingIconHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	png, etag, _ := s.branding.Icon(ctx)
	quoted := `"` + etag + `"`
	if r.Header.Get("If-None-Match") == quoted {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("ETag", quoted)
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(png)
}
