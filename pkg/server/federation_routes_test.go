package server

import (
	"testing"

	"github.com/go-chi/chi/v5"
)

// A provider slug must reach its own routes even when it matches a fixed
// segment, so the routes not keyed by a provider live under federation.ReservedSlug.
func TestFederationRoutesDoNotShadowProviderSlugs(t *testing.T) {
	t.Parallel()
	router, _ := newRouteTable(nil, nil)
	const base = "/api/prohibitorum/auth/federation/"
	for _, tc := range []struct{ method, path, want string }{
		{"GET", base + "flows/login", base + "{slug}/login"},
		{"GET", base + "flows/callback", base + "{slug}/callback"},
		{"GET", base + "flows/test/callback", base + "{slug}/test/callback"},
		{"GET", base + "confirm/login", base + "{slug}/login"},
		{"GET", base + "confirm/callback", base + "{slug}/callback"},
		{"GET", base + "-/flows/flow-id", base + "-/flows/{flow}"},
		{"POST", base + "-/flows/flow-id/prepare", base + "-/flows/{flow}/prepare"},
		{"POST", base + "-/flows/flow-id/verify", base + "-/flows/{flow}/verify"},
		{"GET", base + "-/confirm", base + "-/confirm"},
		{"POST", base + "-/confirm", base + "-/confirm"},
		{"POST", base + "-/confirm/decline", base + "-/confirm/decline"},
	} {
		if got := router.Find(chi.NewRouteContext(), tc.method, tc.path); got != tc.want {
			t.Errorf("%s %s routed to %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}
}
