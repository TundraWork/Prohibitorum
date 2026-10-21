package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	sessstore "prohibitorum/pkg/session"
)

// registerOperations must install the guarded SPA NotFound; a machine path
// that matches no route answers a plain 404 through the production wiring,
// while a dashboard deep link still serves the shell.
func TestRegisterOperationsInstallsGuardedNotFound(t *testing.T) {
	t.Parallel()

	router := chi.NewMux()
	s := &Server{
		router: router,
		api:    humachi.New(router, humaConfig()),
		webUIHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<!doctype html><div id=\"root\">"))
		}),
	}
	registerSecurityScheme(s.api, sessstore.SessionCookieName)
	s.registerOperations()

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/oauth/userinfo/emails", nil))
	if rr.Code != http.StatusNotFound || strings.Contains(rr.Body.String(), `id="root"`) {
		t.Fatalf("GET /oauth/userinfo/emails: status=%d body=%.80s, want plain 404", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/sessions", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `id="root"`) {
		t.Fatalf("GET /sessions: status=%d, want 200 with the SPA shell", rr.Code)
	}
}
