// Package server — entity_icon_url.go
// Shared helper for building the public, cache-busted icon URL for an entity.
package server

import (
	"context"
	"net/url"

	"prohibitorum/pkg/db"
)

// entityIconKinds is the fixed allowlist of icon owner kinds.
var entityIconKinds = map[string]bool{
	"oidc_client":  true,
	"saml_sp":      true,
	"upstream_idp": true,
}

// entityIconURLPtr returns a *string icon URL (nil when no icon) for a view.
func entityIconURLPtr(kind, id, etag string) *string {
	if u := entityIconURL(kind, id, etag); u != "" {
		return &u
	}
	return nil
}

// lookupEntityIconEtag returns the icon etag for (kind,id), or "" when none.
func (s *Server) lookupEntityIconEtag(ctx context.Context, kind, id string) string {
	etag, err := s.queries.GetEntityIconEtag(ctx, db.GetEntityIconEtagParams{OwnerKind: kind, OwnerID: id})
	if err != nil {
		return ""
	}
	return etag
}

// entityIconURL returns the public icon URL for (kind, id), cache-busted by the
// first 8 chars of the etag. Returns "" when etag is empty (no icon), so callers
// can map that to a nil *string in the wire view.
func entityIconURL(kind, id, etag string) string {
	if etag == "" {
		return ""
	}
	v := etag
	if len(v) > 8 {
		v = v[:8]
	}
	return "/icon/" + kind + "/" + url.PathEscape(id) + "?v=" + v
}
