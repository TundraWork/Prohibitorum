// Package server — handle_entity_icon.go
// Public icon serve for apps & providers: GET /icon/{kind}/{id} → the stored
// PNG with ETag/304. Public because the /login page (pre-auth) shows IdP icons;
// icons are not sensitive. Mirrors handle_branding.go's serve shape.
package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/db"
)

func (s *Server) handleGetEntityIconHTTP(w http.ResponseWriter, r *http.Request) {
	kind := chi.URLParam(r, "kind")
	id := chi.URLParam(r, "id")
	if !entityIconKinds[kind] {
		http.Error(w, "bad kind", http.StatusBadRequest)
		return
	}
	row, err := s.queries.GetEntityIcon(r.Context(), db.GetEntityIconParams{OwnerKind: kind, OwnerID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeIconResponse(w, r, row.Png, row.Etag)
}

// writeIconResponse serves stored icon bytes with ETag/304 + a 5-minute public
// cache. Shared by the entity-icon and instance-icon (branding) serves.
func writeIconResponse(w http.ResponseWriter, r *http.Request, data []byte, etag string) {
	quoted := `"` + etag + `"`
	if r.Header.Get("If-None-Match") == quoted {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", imageContentType(data))
	w.Header().Set("ETag", quoted)
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(data)
}

// imageContentType sniffs stored icon bytes. Icons may be WebP (current
// pipeline) or PNG (legacy rows / the embedded default) during the migration,
// so the type is detected from the bytes rather than hard-coded. net/http's
// sniffer doesn't reliably classify WebP, so check the RIFF/WEBP magic first.
func imageContentType(b []byte) string {
	if len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP" {
		return "image/webp"
	}
	return http.DetectContentType(b)
}
