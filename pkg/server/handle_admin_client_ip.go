// Package server — handle_admin_client_ip.go
//
// Admin read/write of the client-IP resolution policy (how the effective remote/user
// IP is extracted behind a CDN/reverse proxy). GET is a plain admin read; PUT goes
// through registerSudoOpHTTP (admin role + fresh sudo enforced by the wrapper).
package server

import (
	"encoding/json"
	"net/http"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/clientip"
)

type clientIPBody struct {
	Strategy       string   `json:"strategy"`
	Header         string   `json:"header"`
	TrustedProxies []string `json:"trustedProxies"`
}

// GET /api/prohibitorum/admin/settings/client-ip
func (s *Server) handleGetClientIPHTTP(w http.ResponseWriter, r *http.Request) {
	raw := s.clientIP.Stored(r.Context())
	writeJSON(w, clientIPBody{
		Strategy:       raw.Strategy,
		Header:         raw.Header,
		TrustedProxies: raw.TrustedProxies,
	})
}

// PUT /api/prohibitorum/admin/settings/client-ip — registerSudoOpHTTP (admin + sudo).
func (s *Server) handlePutClientIPHTTP(w http.ResponseWriter, r *http.Request) {
	var body clientIPBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	stored := clientip.Stored{
		Strategy:       body.Strategy,
		Header:         body.Header,
		TrustedProxies: body.TrustedProxies,
	}
	// clientip.ParseStored (invoked by Set) validates strategy/header/CIDRs/length.
	if err := s.clientIP.Set(r.Context(), stored); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	s.auditBranding(r, "client_ip_config_updated")
	w.WriteHeader(http.StatusNoContent)
}
