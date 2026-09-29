package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ---------------------------------------------------------------------------
// forwardAuthAppView projection tests
// ---------------------------------------------------------------------------

func TestForwardAuthAppView_MapsAllFields(t *testing.T) {
	t.Parallel()
	now := time.Now()
	v := forwardAuthAppView("fa-client", "My App",
		pgtype.Text{String: "app.example.test", Valid: true},
		true, false,
		pgtype.Timestamptz{Time: now, Valid: true}, "verified_email")
	if v.ClientID != "fa-client" || v.DisplayName != "My App" {
		t.Errorf("id/name mismatch: %+v", v)
	}
	if v.ForwardAuthHost != "app.example.test" {
		t.Errorf("host = %q", v.ForwardAuthHost)
	}
	if !v.AccessRestricted || v.Disabled {
		t.Errorf("flags mismatch: restricted=%v disabled=%v", v.AccessRestricted, v.Disabled)
	}
	if v.RemoteUserSource != "verified_email" {
		t.Errorf("RemoteUserSource = %q", v.RemoteUserSource)
	}
	if !v.CreatedAt.Equal(now) {
		t.Errorf("createdAt = %v, want %v", v.CreatedAt, now)
	}
}

func TestForwardAuthAppView_EmptyHostAndTime(t *testing.T) {
	t.Parallel()
	v := forwardAuthAppView("c", "n", pgtype.Text{}, false, true, pgtype.Timestamptz{})
	if v.ForwardAuthHost != "" {
		t.Errorf("invalid host should map to empty string, got %q", v.ForwardAuthHost)
	}
	if !v.CreatedAt.IsZero() {
		t.Errorf("invalid timestamptz should map to zero time, got %v", v.CreatedAt)
	}
	if v.RemoteUserSource != "username" {
		t.Errorf("default RemoteUserSource = %q", v.RemoteUserSource)
	}
}

// ---------------------------------------------------------------------------
// HTTP handler guard-path tests for create / update
// (nil queries — validation must fire before any DB call)
// ---------------------------------------------------------------------------

func postFAApp(t *testing.T, path, body string, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	h(rr, req)
	return rr
}

// The scope vocabulary is gone: a client still sending it gets bad_request
// rather than having the field silently dropped.
func TestHandleCreateForwardAuthApp_ScopesField_BadRequest(t *testing.T) {
	t.Parallel()
	s := &Server{} // nil queries — the decoder must reject before any DB call
	for _, body := range []string{
		`{"clientId":"fa1","host":"app.example.test","displayName":"FA","scopes":[]}`,
		`{"clientId":"fa1","host":"app.example.test","displayName":"FA","scopes":[{"name":"read"}]}`,
		`{"clientId":"fa1","host":"app.example.test","displayName":"FA","unknown":1}`,
		`{"clientId":"fa1","host":"app.example.test","displayName":"FA"}{}`,
	} {
		rr := postFAApp(t, "/api/prohibitorum/forward-auth-apps", body, s.handleCreateForwardAuthAppHTTP)
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "bad_request") {
			t.Errorf("%s: status = %d body = %q; want 400 bad_request", body, rr.Code, rr.Body.String())
		}
	}
}

func TestHandleCreateForwardAuthApp_MissingHostAndClientID_BadRequest(t *testing.T) {
	t.Parallel()
	s := &Server{}
	body := `{"clientId":"","host":""}`
	rr := postFAApp(t, "/api/prohibitorum/forward-auth-apps", body, s.handleCreateForwardAuthAppHTTP)
	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("status = %d; want 4xx for missing clientId/host", rr.Code)
	}
}

// withClientID injects a chi route context with clientId="fa1" onto req.
func withClientID(req *http.Request, clientID string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("clientId", clientID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// TestHandleUpdateForwardAuthApp_ScopesField_BadRequest — PUT with the removed
// scopes field returns bad_request before touching the DB.
func TestHandleUpdateForwardAuthApp_ScopesField_BadRequest(t *testing.T) {
	t.Parallel()
	s := &Server{}
	for _, body := range []string{
		`{"displayName":"FA","host":"app.example.test","scopes":[]}`,
		`{"displayName":"FA","host":"app.example.test","scopes":[{"name":"read"}]}`,
	} {
		rr := httptest.NewRecorder()
		req := withClientID(
			httptest.NewRequest("PUT", "/api/prohibitorum/forward-auth-apps/fa1", bytes.NewReader([]byte(body))),
			"fa1",
		)
		req.Header.Set("Content-Type", "application/json")
		s.handleUpdateForwardAuthAppHTTP(rr, req)
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "bad_request") {
			t.Errorf("%s: status = %d body = %q; want 400 bad_request", body, rr.Code, rr.Body.String())
		}
	}
}

// TestHandleUpdateForwardAuthApp_MissingHost_BadRequest verifies the host guard.
func TestHandleUpdateForwardAuthApp_MissingHost_BadRequest(t *testing.T) {
	t.Parallel()
	s := &Server{}
	body := `{"displayName":"FA","host":""}`
	rr := httptest.NewRecorder()
	req := withClientID(
		httptest.NewRequest("PUT", "/api/prohibitorum/forward-auth-apps/fa1", bytes.NewReader([]byte(body))),
		"fa1",
	)
	req.Header.Set("Content-Type", "application/json")
	s.handleUpdateForwardAuthAppHTTP(rr, req)
	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("status = %d; want 4xx for missing host", rr.Code)
	}
}
