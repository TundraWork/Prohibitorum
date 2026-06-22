// Package server — launchpad_url.go
// Pure resolution of an OIDC client's launch URL for the end-user launchpad.
package server

import (
	"net/url"
	"strings"
)

// resolveOIDCLaunchURL picks where the launchpad opens an OIDC app: the explicit
// admin launch_url when set, else the scheme://host origin of the first
// parseable redirect URI (its login start usually lives at the app root), else
// "" meaning "not launchable" (the caller omits the app).
func resolveOIDCLaunchURL(launch string, redirectURIs []string) string {
	if s := strings.TrimSpace(launch); s != "" {
		return s
	}
	for _, ru := range redirectURIs {
		if u, err := url.Parse(strings.TrimSpace(ru)); err == nil && u.Scheme != "" && u.Host != "" {
			return u.Scheme + "://" + u.Host + "/"
		}
	}
	return ""
}
