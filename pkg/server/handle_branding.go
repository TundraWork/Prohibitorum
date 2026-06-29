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
	maintenance, maintenanceMsg := s.branding.Maintenance(ctx)
	cfg := contract.PublicConfig{
		InstanceName:       s.branding.InstanceName(ctx),
		HasCustomIcon:      s.branding.HasCustomIcon(ctx),
		IconURL:            "/branding/icon",
		IconEtag:           etag,
		MaintenanceMode:    maintenance,
		MaintenanceMessage: maintenanceMsg,
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(cfg)
}

// GET /branding/icon (public) — serves the effective icon with ETag/304.
func (s *Server) handleGetBrandingIconHTTP(w http.ResponseWriter, r *http.Request) {
	icon, etag, _ := s.branding.Icon(r.Context())
	writeIconResponse(w, r, icon, etag)
}
