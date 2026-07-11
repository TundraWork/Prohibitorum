// Package server — handle_admin_entity_icon.go
//
// Admin per-entity icon upload/remove for OIDC apps, SAML apps, and upstream
// IdPs. Mirrors the instance-icon pattern (handle_admin_settings.go): the raw
// image PUT is registered via registerOpHTTP(admin) with an in-handler
// requireFreshSudo (the sudo wrapper rejects non-JSON content-types + caps at
// 64 KiB); the DELETE is registered via registerSudoOpHTTP (admin + sudo).
// Icons are processed by branding.ProcessIcon (center-crop → PNG 256²) and
// stored in entity_icon keyed by (owner_kind, owner_id).
package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/branding"
	"prohibitorum/pkg/db"
)

// maxEntityIconRead is one byte past the 5 MiB limit so we can detect oversized
// payloads before handing off to ProcessIcon (which re-checks internally).
const maxEntityIconRead = 5<<20 + 1

// putEntityIcon is the shared upload helper: validates fresh sudo, reads the
// raw body, processes it through branding.ProcessIcon, and persists the result
// as (owner_kind, owner_id) in entity_icon.
func (s *Server) putEntityIcon(w http.ResponseWriter, r *http.Request, kind, id string, factor audit.Factor) {
	sess := authn.SessionFromContext(r.Context())
	if s.requireFreshSudo(r.Context(), w, sess) {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxEntityIconRead))
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	// One decode produces both the stored WebP and the backdrop accent. A
	// fully-transparent icon yields no accent (left NULL → client falls back to a
	// name-derived tint).
	out, etag, accentHex, perr := branding.ProcessIconWithAccent(raw)
	if perr != nil {
		if errors.Is(perr, branding.ErrTooLarge) {
			writeAvatarErr(w, "avatar_too_large", "icon: image exceeds 5 MiB")
			return
		}
		writeAvatarErr(w, "avatar_invalid_image", "icon: invalid or unsupported image format")
		return
	}
	var accent pgtype.Text
	if accentHex != "" {
		accent = pgtype.Text{String: accentHex, Valid: true}
	}
	if err := s.queries.SetEntityIcon(r.Context(), db.SetEntityIconParams{
		OwnerKind: kind, OwnerID: id, Png: out, Etag: etag, AccentColor: accent,
	}); err != nil {
		writeAuthErr(w, err)
		return
	}
	s.auditEntityIcon(r, factor, kind, id, "icon_updated")
	w.WriteHeader(http.StatusNoContent)
}

// deleteEntityIcon is the shared removal helper: removes the (owner_kind,
// owner_id) row from entity_icon. A missing row is silently tolerated (the
// DELETE is idempotent — no row = already gone).
func (s *Server) deleteEntityIcon(w http.ResponseWriter, r *http.Request, kind, id string, factor audit.Factor) {
	if err := s.queries.DeleteEntityIcon(r.Context(), db.DeleteEntityIconParams{OwnerKind: kind, OwnerID: id}); err != nil {
		writeAuthErr(w, err)
		return
	}
	s.auditEntityIcon(r, factor, kind, id, "icon_removed")
	w.WriteHeader(http.StatusNoContent)
}

// auditEntityIcon records an icon-mutation admin audit event.
func (s *Server) auditEntityIcon(r *http.Request, factor audit.Factor, kind, id, reason string) {
	var acct *int32
	if sess := authn.SessionFromContext(r.Context()); sess != nil && sess.Account != nil {
		v := sess.Account.ID
		acct = &v
	}
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: acct,
		Factor:    factor,
		Event:     audit.EventUpdate,
		IP:        audit.ParseIPOrNil(s.clientIP.IP(r)),
		UserAgent: r.UserAgent(),
		Detail:    map[string]any{"reason": reason, "owner_kind": kind, "owner_id": id},
	})
}

// ----- OIDC application icon -----------------------------------------------

// PUT /api/prohibitorum/oidc-applications/{clientId}/icon
// Registered via plain registerOpHTTP(admin) — fresh-sudo enforced in-handler.
func (s *Server) handlePutOIDCAppIconHTTP(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "clientId")
	if _, err := s.queries.GetOIDCClientAny(r.Context(), id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrClientNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handlePutOIDCAppIcon: lookup: %w", err))
		return
	}
	s.putEntityIcon(w, r, "oidc_client", id, audit.FactorOIDCClient)
}

// DELETE /api/prohibitorum/oidc-applications/{clientId}/icon
// Registered via registerSudoOpHTTP — admin + fresh sudo via wrapper.
func (s *Server) handleDeleteOIDCAppIconHTTP(w http.ResponseWriter, r *http.Request) {
	s.deleteEntityIcon(w, r, "oidc_client", chi.URLParam(r, "clientId"), audit.FactorOIDCClient)
}

// ----- SAML application icon -----------------------------------------------

// PUT /api/prohibitorum/saml-applications/{id}/icon
// Registered via plain registerOpHTTP(admin) — fresh-sudo enforced in-handler.
func (s *Server) handlePutSAMLAppIconHTTP(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if _, err := s.queries.GetSAMLSPByID(r.Context(), id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, samlSPNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handlePutSAMLAppIcon: lookup: %w", err))
		return
	}
	s.putEntityIcon(w, r, "saml_sp", idStr, audit.FactorSAMLSP)
}

// DELETE /api/prohibitorum/saml-applications/{id}/icon
// Registered via registerSudoOpHTTP — admin + fresh sudo via wrapper.
func (s *Server) handleDeleteSAMLAppIconHTTP(w http.ResponseWriter, r *http.Request) {
	s.deleteEntityIcon(w, r, "saml_sp", chi.URLParam(r, "id"), audit.FactorSAMLSP)
}

// ----- Upstream IdP icon ---------------------------------------------------

// PUT /api/prohibitorum/identity-providers/{slug}/icon
// Registered via plain registerOpHTTP(admin) — fresh-sudo enforced in-handler.
func (s *Server) handlePutIdentityProviderIconHTTP(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, err := s.queries.GetUpstreamIDPBySlugAny(r.Context(), slug); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrUpstreamIDPNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handlePutIdentityProviderIcon: lookup: %w", err))
		return
	}
	s.putEntityIcon(w, r, "upstream_idp", slug, audit.FactorUpstreamIDP)
}

// DELETE /api/prohibitorum/identity-providers/{slug}/icon
// Registered via registerSudoOpHTTP — admin + fresh sudo via wrapper.
func (s *Server) handleDeleteIdentityProviderIconHTTP(w http.ResponseWriter, r *http.Request) {
	s.deleteEntityIcon(w, r, "upstream_idp", chi.URLParam(r, "slug"), audit.FactorUpstreamIDP)
}
