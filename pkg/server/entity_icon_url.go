// Package server — entity_icon_url.go
// Shared helper for building the public, cache-busted icon URL for an entity.
package server

import "net/url"

// entityIconKinds is the fixed allowlist of icon owner kinds.
var entityIconKinds = map[string]bool{
	"oidc_client":  true,
	"saml_sp":      true,
	"upstream_idp": true,
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
