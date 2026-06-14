// Package server — handle_admin_set_disabled_test.go
//
// Guard-path unit tests for the dedicated enable/disable endpoints:
//
//	POST /oidc-applications/set-disabled   (handleSetOIDCApplicationDisabledHTTP)
//	POST /identity-providers/set-disabled  (handleSetIdentityProviderDisabledHTTP)
//
// These flip ONLY the disabled flag, independent of the config PUT. Following the
// convention for the admin handlers (s.queries is a concrete *db.Queries that can
// not be faked), the DB-touching success path is exercised by the e2e smoke; the
// request-validation guards below run before any DB call, so a zero-value Server
// (nil queries) is sufficient and must never be reached past the guard.
package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postSetDisabled(t *testing.T, path, body string, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	h(rr, req)
	return rr
}

func TestHandleSetOIDCApplicationDisabled_EmptyClientID(t *testing.T) {
	t.Parallel()
	s := &Server{} // nil queries — the guard must return before any DB call
	rr := postSetDisabled(t, "/api/prohibitorum/oidc-applications/set-disabled",
		`{"clientId":"","disabled":true}`, s.handleSetOIDCApplicationDisabledHTTP)
	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("status = %d; want 4xx for empty clientId", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "bad_request") {
		t.Errorf("body = %q; want bad_request", rr.Body.String())
	}
}

func TestHandleSetOIDCApplicationDisabled_BadJSON(t *testing.T) {
	t.Parallel()
	s := &Server{}
	rr := postSetDisabled(t, "/api/prohibitorum/oidc-applications/set-disabled",
		`not json`, s.handleSetOIDCApplicationDisabledHTTP)
	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("status = %d; want 4xx for malformed body", rr.Code)
	}
}

func TestHandleSetIdentityProviderDisabled_EmptySlug(t *testing.T) {
	t.Parallel()
	s := &Server{}
	rr := postSetDisabled(t, "/api/prohibitorum/identity-providers/set-disabled",
		`{"slug":"","disabled":true}`, s.handleSetIdentityProviderDisabledHTTP)
	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("status = %d; want 4xx for empty slug", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "bad_request") {
		t.Errorf("body = %q; want bad_request", rr.Body.String())
	}
}

func TestHandleSetIdentityProviderDisabled_BadJSON(t *testing.T) {
	t.Parallel()
	s := &Server{}
	rr := postSetDisabled(t, "/api/prohibitorum/identity-providers/set-disabled",
		`{`, s.handleSetIdentityProviderDisabledHTTP)
	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("status = %d; want 4xx for malformed body", rr.Code)
	}
}
