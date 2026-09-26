// Package server — entity_icon_url.go
// Shared helper for building the public, cache-busted icon URL for an entity.
package server

import (
	"context"
	"errors"
	"net/url"

	"github.com/jackc/pgx/v5"
	"github.com/sirupsen/logrus"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/logx"
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
		if !errors.Is(err, pgx.ErrNoRows) {
			logx.WithContext(ctx).WithFields(logrus.Fields{
				"event":      "entity_icon.lookup_error",
				"owner_kind": kind,
				"owner_id":   id,
				"error":      err.Error(),
			}).Warn("entity icon etag lookup failed")
		}
		return ""
	}
	return etag
}

// enrichIconURL returns the cache-busted icon URL for (kind, id), or nil when
// the entity has no icon — the single-entity call sites attach it to a
// response view. Composes lookupEntityIconEtag + entityIconURLPtr so GET and
// mutation handlers return the same iconUrl for the same row.
func (s *Server) enrichIconURL(ctx context.Context, kind, id string) *string {
	return entityIconURLPtr(kind, id, s.lookupEntityIconEtag(ctx, kind, id))
}

// listIconURLs reads every icon etag of one kind in a single query and returns
// a lookup from owner id to the cache-busted URL, so a page of rows costs one
// query instead of one per row. Returns nil when nothing of that kind has an
// icon, which the list handlers treat as "no row carries iconUrl".
//
// The lookup covers more ids than the page returns. That is deliberate: the
// kinds are small (a handful of identity providers or applications per
// instance), and one query per page beats either an N+1 or a second query
// shape that would have to be kept in step with the page's keyset filters.
func (s *Server) listIconURLs(ctx context.Context, kind string) map[string]*string {
	if !entityIconKinds[kind] {
		return nil
	}
	rows, err := s.listQ().ListEntityIconEtags(ctx, kind)
	if err != nil {
		logx.WithContext(ctx).WithFields(logrus.Fields{
			"event":      "entity_icon.list_error",
			"owner_kind": kind,
			"error":      err.Error(),
		}).Warn("entity icon etag list failed")
		return nil
	}
	if len(rows) == 0 {
		return nil
	}
	urls := make(map[string]*string, len(rows))
	for _, row := range rows {
		urls[row.OwnerID] = entityIconURLPtr(kind, row.OwnerID, row.Etag)
	}
	return urls
}

// iconURLFor returns the icon URL for (kind, id) from a listIconURLs lookup,
// nil when the lookup is nil or the id has no icon. Keeps the list loops free
// of the map-existence dance.
func iconURLFor(urls map[string]*string, id string) *string {
	if urls == nil {
		return nil
	}
	return urls[id]
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
