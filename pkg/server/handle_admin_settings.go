// Package server — handle_admin_settings.go
//
// Admin instance-branding overrides (name + icon). Name PUT + icon DELETE go
// through registerSudoOpHTTP (JSON / no body). The icon UPLOAD uses
// registerOpHTTP(admin) + an in-handler fresh-sudo gate because the sudo
// wrapper rejects non-JSON content-types and caps bodies at 64 KiB.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/branding"
)

// maxIconRead is one byte past the 5 MiB limit so we can detect oversized
// payloads before handing off to ProcessIcon (which re-checks internally).
const maxIconRead = 5<<20 + 1

// PUT /api/prohibitorum/admin/settings  {"instanceName":"..."}
// Registered via registerSudoOpHTTP — admin role + fresh sudo enforced by wrapper.
func (s *Server) handlePutInstanceNameHTTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanceName string `json:"instanceName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if len([]rune(body.InstanceName)) > 64 {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if err := s.branding.SetName(r.Context(), body.InstanceName); err != nil {
		writeAuthErr(w, err)
		return
	}
	s.auditBranding(r, "instance_name_updated")
	w.WriteHeader(http.StatusNoContent)
}

// PUT /api/prohibitorum/admin/settings/icon  (raw image body, up to 5 MiB)
// Registered via plain registerOpHTTP(admin) — fresh-sudo enforced in-handler
// because the sudo wrapper rejects non-JSON content-types and caps bodies.
func (s *Server) handlePutInstanceIconHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if s.requireFreshSudo(r.Context(), w, sess) {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxIconRead))
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if err := s.branding.SetIcon(r.Context(), raw); err != nil {
		if errors.Is(err, branding.ErrTooLarge) {
			writeAvatarErr(w, "avatar_too_large", "icon: image exceeds 5 MiB")
			return
		}
		writeAvatarErr(w, "avatar_invalid_image", "icon: invalid or unsupported image format")
		return
	}
	s.auditBranding(r, "instance_icon_updated")
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/prohibitorum/admin/settings/icon
// Registered via registerSudoOpHTTP — admin role + fresh sudo enforced by wrapper.
func (s *Server) handleDeleteInstanceIconHTTP(w http.ResponseWriter, r *http.Request) {
	if err := s.branding.ClearIcon(r.Context()); err != nil {
		writeAuthErr(w, err)
		return
	}
	s.auditBranding(r, "instance_icon_removed")
	w.WriteHeader(http.StatusNoContent)
}

// auditBranding records a branding-mutation admin audit event.
// Uses FactorSigningKey (the closest admin system-config factor in the audit
// vocabulary) and EventUpdate (all three mutations are configuration updates).
// Errors are silently ignored — the same pattern used throughout the server.
func (s *Server) auditBranding(r *http.Request, reason string) {
	var acct *int32
	if sess := authn.SessionFromContext(r.Context()); sess != nil && sess.Account != nil {
		id := sess.Account.ID
		acct = &id
	}
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: acct,
		Factor:    audit.FactorSigningKey,
		Event:     audit.EventUpdate,
		IP:        audit.ParseIPOrNil(r.RemoteAddr),
		UserAgent: r.UserAgent(),
		Detail:    map[string]any{"reason": reason},
	})
}
