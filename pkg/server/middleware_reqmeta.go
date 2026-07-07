package server

import (
	"net/http"

	"prohibitorum/pkg/audit"
)

// requestMetaMW stashes the resolved client IP + User-Agent into the request ctx so
// audit records auto-carry them (see audit.WithRequestMeta). Mounted for every route
// via router.Use, before LoadSession.
func requestMetaMW(ipOf func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := audit.WithRequestMeta(r.Context(), ipOf(r), r.UserAgent())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
