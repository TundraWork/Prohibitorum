package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/contract"
)

// ---------------------------------------------------------------------------
// forwardAuthAppView projection tests
// ---------------------------------------------------------------------------

func TestForwardAuthAppView_MapsAllFields(t *testing.T) {
	t.Parallel()
	now := time.Now()
	scopes := []byte(`[{"name":"repo:read","description":"Read repos"},{"name":"repo:write"}]`)
	v := forwardAuthAppView("fa-client", "My App",
		pgtype.Text{String: "app.example.test", Valid: true},
		scopes,
		true, false,
		pgtype.Timestamptz{Time: now, Valid: true})
	if v.ClientID != "fa-client" || v.DisplayName != "My App" {
		t.Errorf("id/name mismatch: %+v", v)
	}
	if v.ForwardAuthHost != "app.example.test" {
		t.Errorf("host = %q", v.ForwardAuthHost)
	}
	if !v.AccessRestricted || v.Disabled {
		t.Errorf("flags mismatch: restricted=%v disabled=%v", v.AccessRestricted, v.Disabled)
	}
	if !v.CreatedAt.Equal(now) {
		t.Errorf("createdAt = %v, want %v", v.CreatedAt, now)
	}
	if len(v.Scopes) != 2 {
		t.Fatalf("Scopes len: want 2, got %d", len(v.Scopes))
	}
	if v.Scopes[0].Name != "repo:read" || v.Scopes[0].Description != "Read repos" {
		t.Errorf("Scopes[0]: %+v", v.Scopes[0])
	}
	if v.Scopes[1].Name != "repo:write" || v.Scopes[1].Description != "" {
		t.Errorf("Scopes[1]: %+v", v.Scopes[1])
	}
}

func TestForwardAuthAppView_EmptyHostAndTime(t *testing.T) {
	t.Parallel()
	v := forwardAuthAppView("c", "n", pgtype.Text{}, nil, false, true, pgtype.Timestamptz{})
	if v.ForwardAuthHost != "" {
		t.Errorf("invalid host should map to empty string, got %q", v.ForwardAuthHost)
	}
	if !v.CreatedAt.IsZero() {
		t.Errorf("invalid timestamptz should map to zero time, got %v", v.CreatedAt)
	}
	if v.Scopes == nil {
		t.Error("Scopes should be non-nil empty slice, got nil")
	}
	if len(v.Scopes) != 0 {
		t.Errorf("Scopes should be empty for nil scopesJSON, got %v", v.Scopes)
	}
}

func TestForwardAuthAppView_EmptyScopesJSON(t *testing.T) {
	t.Parallel()
	v := forwardAuthAppView("c", "n", pgtype.Text{}, []byte(`[]`), false, false, pgtype.Timestamptz{})
	if len(v.Scopes) != 0 {
		t.Errorf("Scopes: want empty, got %v", v.Scopes)
	}
}

// ---------------------------------------------------------------------------
// validateFAScopes tests
// ---------------------------------------------------------------------------

func TestValidateFAScopes_ValidScopes(t *testing.T) {
	t.Parallel()
	in := []contract.ForwardAuthScope{
		{Name: "repo:read", Description: "  Read access  "},
		{Name: "repo:write", Description: ""},
		{Name: "admin.users"},
		{Name: "a.b-c"}, // internal separators allowed
		{Name: "A"},     // single-char alphanumeric allowed
	}
	out, err := validateFAScopes(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 5 {
		t.Fatalf("len: want 5, got %d", len(out))
	}
	if out[0].Description != "Read access" {
		t.Errorf("description not trimmed: %q", out[0].Description)
	}
}

func TestValidateFAScopes_EmptyIsValid(t *testing.T) {
	t.Parallel()
	out, err := validateFAScopes(nil)
	if err != nil {
		t.Fatalf("nil input: unexpected error: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("want empty, got %v", out)
	}
	out2, err := validateFAScopes([]contract.ForwardAuthScope{})
	if err != nil {
		t.Fatalf("empty slice: unexpected error: %v", err)
	}
	if len(out2) != 0 {
		t.Errorf("want empty, got %v", out2)
	}
}

func TestValidateFAScopes_InvalidName_ReturnsError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"starts_with_dash", "-bad"},
		{"starts_with_dot", ".bad"},
		{"starts_with_colon", ":bad"},
		{"ends_with_dash", "a-"},
		{"ends_with_dot", "a."},
		{"ends_with_colon", "a:"},
		{"spaces", "repo read"},
		{"too_long", strings.Repeat("a", 65)},
		{"has_slash", "repo/read"},
		{"has_at", "repo@read"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := validateFAScopes([]contract.ForwardAuthScope{{Name: tc.input}})
			if err == nil {
				t.Errorf("input %q: expected error, got nil", tc.input)
			}
		})
	}
}

func TestValidateFAScopes_DescriptionTooLong_ReturnsError(t *testing.T) {
	t.Parallel()
	in := []contract.ForwardAuthScope{
		{Name: "repo:read", Description: strings.Repeat("x", 257)},
	}
	_, err := validateFAScopes(in)
	if err == nil {
		t.Fatal("expected error for over-long description, got nil")
	}
	// A 256-char description (the boundary) is accepted.
	ok := []contract.ForwardAuthScope{
		{Name: "repo:read", Description: strings.Repeat("x", 256)},
	}
	if _, err := validateFAScopes(ok); err != nil {
		t.Fatalf("256-char description should be accepted: %v", err)
	}
}

func TestValidateFAScopes_DuplicateName_ReturnsError(t *testing.T) {
	t.Parallel()
	in := []contract.ForwardAuthScope{
		{Name: "repo:read"},
		{Name: "repo:write"},
		{Name: "repo:read"}, // duplicate
	}
	_, err := validateFAScopes(in)
	if err == nil {
		t.Fatal("expected error for duplicate scope name, got nil")
	}
}

func TestValidateFAScopes_NormalizesName(t *testing.T) {
	t.Parallel()
	in := []contract.ForwardAuthScope{
		{Name: "  repo:read  ", Description: "  desc  "},
	}
	out, err := validateFAScopes(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out[0].Name != "repo:read" {
		t.Errorf("Name not trimmed: %q", out[0].Name)
	}
	if out[0].Description != "desc" {
		t.Errorf("Description not trimmed: %q", out[0].Description)
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

func TestHandleCreateForwardAuthApp_InvalidScopeName_BadRequest(t *testing.T) {
	t.Parallel()
	s := &Server{} // nil queries — guard must fire before DB
	body := `{"clientId":"fa1","host":"app.example.test","displayName":"FA","scopes":[{"name":"-bad-name"}]}`
	rr := postFAApp(t, "/api/prohibitorum/forward-auth-apps", body, s.handleCreateForwardAuthAppHTTP)
	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("status = %d; want 4xx for invalid scope name", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "bad_request") {
		t.Errorf("body = %q; want bad_request", rr.Body.String())
	}
}

func TestHandleCreateForwardAuthApp_DuplicateScopeName_BadRequest(t *testing.T) {
	t.Parallel()
	s := &Server{}
	body := `{"clientId":"fa1","host":"app.example.test","displayName":"FA","scopes":[{"name":"read"},{"name":"read"}]}`
	rr := postFAApp(t, "/api/prohibitorum/forward-auth-apps", body, s.handleCreateForwardAuthAppHTTP)
	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("status = %d; want 4xx for duplicate scope name", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "bad_request") {
		t.Errorf("body = %q; want bad_request", rr.Body.String())
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

// TestHandleUpdateForwardAuthApp_InvalidScopeName_BadRequest — PUT with a bad
// scope name must return 4xx/bad_request before touching the DB.
func TestHandleUpdateForwardAuthApp_InvalidScopeName_BadRequest(t *testing.T) {
	t.Parallel()
	s := &Server{}
	body := `{"displayName":"FA","host":"app.example.test","scopes":[{"name":"has space"}]}`
	rr := httptest.NewRecorder()
	req := withClientID(
		httptest.NewRequest("PUT", "/api/prohibitorum/forward-auth-apps/fa1", bytes.NewReader([]byte(body))),
		"fa1",
	)
	req.Header.Set("Content-Type", "application/json")
	s.handleUpdateForwardAuthAppHTTP(rr, req)
	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("status = %d; want 4xx for invalid scope name", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "bad_request") {
		t.Errorf("body = %q; want bad_request", rr.Body.String())
	}
}

// TestHandleUpdateForwardAuthApp_DuplicateScopeName_BadRequest — duplicate scope
// names must return bad_request.
func TestHandleUpdateForwardAuthApp_DuplicateScopeName_BadRequest(t *testing.T) {
	t.Parallel()
	s := &Server{}
	body := `{"displayName":"FA","host":"app.example.test","scopes":[{"name":"read"},{"name":"read"}]}`
	rr := httptest.NewRecorder()
	req := withClientID(
		httptest.NewRequest("PUT", "/api/prohibitorum/forward-auth-apps/fa1", bytes.NewReader([]byte(body))),
		"fa1",
	)
	req.Header.Set("Content-Type", "application/json")
	s.handleUpdateForwardAuthAppHTTP(rr, req)
	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("status = %d; want 4xx for duplicate scope name", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "bad_request") {
		t.Errorf("body = %q; want bad_request", rr.Body.String())
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

// ---------------------------------------------------------------------------
// Round-trip: validateFAScopes -> json.Marshal -> parseFAScopes
// (DB-free verification that the scope round-trip is lossless)
// ---------------------------------------------------------------------------

func TestForwardAuthScopeRoundTrip(t *testing.T) {
	t.Parallel()
	in := []contract.ForwardAuthScope{
		{Name: "repo:read", Description: "Read repositories"},
		{Name: "repo:write", Description: ""},
		{Name: "admin.users", Description: "User admin"},
	}
	validated, err := validateFAScopes(in)
	if err != nil {
		t.Fatalf("validateFAScopes: %v", err)
	}
	raw, err := json.Marshal(validated)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	parsed := parseFAScopes(raw)
	if len(parsed) != len(validated) {
		t.Fatalf("len mismatch: want %d, got %d", len(validated), len(parsed))
	}
	for i, sc := range validated {
		if parsed[i].Name != sc.Name || parsed[i].Description != sc.Description {
			t.Errorf("[%d]: want %+v, got %+v", i, sc, parsed[i])
		}
	}
	// Verify that forwardAuthAppView exposes the round-tripped scopes correctly.
	view := forwardAuthAppView("c", "n", pgtype.Text{String: "h", Valid: true}, raw, false, false, pgtype.Timestamptz{})
	if len(view.Scopes) != 3 {
		t.Fatalf("view.Scopes len: want 3, got %d", len(view.Scopes))
	}
	if view.Scopes[0].Name != "repo:read" {
		t.Errorf("view.Scopes[0].Name: %q", view.Scopes[0].Name)
	}
}
