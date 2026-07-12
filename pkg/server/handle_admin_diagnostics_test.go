// Package server — handle_admin_diagnostics_test.go
//
// Tests for the admin request-diagnostic lookup endpoint:
//
//	GET /api/prohibitorum/diagnostics/{requestId}
//
// The route requires admin + fresh sudo, enforces a per-account rate limit,
// emits an audit event, and performs exact-ID lookup only — no enumeration.
// Expired or absent records return 404.
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/diagnostic"
)

// fakeDiagStore implements diagnostic.StoreReader for testing.
type fakeDiagStore struct {
	rows map[string]diagnostic.Record
}

func newFakeDiagStore() *fakeDiagStore {
	return &fakeDiagStore{rows: map[string]diagnostic.Record{}}
}

func (f *fakeDiagStore) Lookup(_ context.Context, rid string) (diagnostic.Record, error) {
	rec, ok := f.rows[rid]
	if !ok {
		return diagnostic.Record{}, diagnostic.ErrNotFound
	}
	return rec, nil
}

func (f *fakeDiagStore) addRow(rid, code string) {
	now := time.Now()
	f.rows[rid] = diagnostic.Record{
		RequestID:  rid,
		Code:       code,
		Operation:  "oidc.exchange",
		Method:     "POST",
		Route:      "/oauth/token",
		OccurredAt: now,
		ExpiresAt:  now.Add(7 * 24 * time.Hour),
	}
}

// captureAuditWriter records every audit.Record call so the test can assert
// the diagnostic_lookup event was emitted.
type captureAuditWriter struct {
	events []audit.Record
}

func (w *captureAuditWriter) Record(_ context.Context, r audit.Record) error {
	w.events = append(w.events, r)
	return nil
}

func diagHandlerServer(t *testing.T, store diagnostic.StoreReader) (*Server, *captureAuditWriter) {
	t.Helper()
	aw := &captureAuditWriter{}
	s := &Server{
		config:      &configx.Config{},
		rateLimiter: authn.NewRateLimiter(),
		clientIP:    newDirectResolver(),
		Audit:       aw,
		diagStore:   store,
	}
	return s, aw
}

func adminSudoSession() *authn.Session {
	return &authn.Session{
		Account: &db.Account{ID: 1, Role: "admin"},
		Token:   "tok",
		Data:    &authn.SessionData{SudoUntil: time.Now().Add(5 * time.Minute)},
	}
}

func diagGetReq(path string, sess *authn.Session) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	rctx := chi.NewRouteContext()
	parts := strings.Split(path, "/")
	rctx.URLParams.Add("requestId", parts[len(parts)-1])
	return r.WithContext(context.WithValue(
		authn.WithSession(r.Context(), sess), chi.RouteCtxKey, rctx))
}

func diagGetReqNoSession(path string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	rctx := chi.NewRouteContext()
	parts := strings.Split(path, "/")
	rctx.URLParams.Add("requestId", parts[len(parts)-1])
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestDiagnosticLookup_NoSession_Returns401(t *testing.T) {
	store := newFakeDiagStore()
	s, _ := diagHandlerServer(t, store)
	req := diagGetReqNoSession("/api/prohibitorum/diagnostics/rid")
	rec := httptest.NewRecorder()
	s.handleAdminDiagnosticLookupHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (no_session)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "no_session") {
		t.Fatalf("body = %q, want no_session", rec.Body.String())
	}
}

func TestDiagnosticLookup_NoFreshSudo_Returns401(t *testing.T) {
	// Use the real router so registerSudoOpHTTP's withFreshSudo gate fires.
	router := chi.NewMux()
	s := &Server{
		config:      &configx.Config{},
		rateLimiter: authn.NewRateLimiter(),
		clientIP:    newDirectResolver(),
		diagStore:   newFakeDiagStore(),
	}
	s.registerSudoOpHTTP(router, "GET", "/api/prohibitorum/diagnostics/{requestId}",
		contract.AuthRequirement{Kind: contract.AuthAdmin}, s.handleAdminDiagnosticLookupHTTP)

	sess := adminSession(time.Time{}) // zero SudoUntil = no fresh sudo
	req := reqWithSession(http.MethodGet, "/api/prohibitorum/diagnostics/rid", "", "", sess)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (sudo_required)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "sudo_required") {
		t.Fatalf("body = %q, want sudo_required", rec.Body.String())
	}
}

func TestDiagnosticLookup_ExactID_Returns200(t *testing.T) {
	store := newFakeDiagStore()
	store.addRow("rid-200", "oidc_exchange_failed")
	s, _ := diagHandlerServer(t, store)
	sess := adminSudoSession()
	req := diagGetReq("/api/prohibitorum/diagnostics/rid-200", sess)
	rec := httptest.NewRecorder()
	s.handleAdminDiagnosticLookupHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if body["requestId"] != "rid-200" {
		t.Fatalf("response requestId = %v", body["requestId"])
	}
	if body["code"] != "oidc_exchange_failed" {
		t.Fatalf("response code = %v", body["code"])
	}
}

func TestDiagnosticLookup_AbsentID_Returns404(t *testing.T) {
	store := newFakeDiagStore()
	s, _ := diagHandlerServer(t, store)
	sess := adminSudoSession()
	req := diagGetReq("/api/prohibitorum/diagnostics/no-such-rid", sess)
	rec := httptest.NewRecorder()
	s.handleAdminDiagnosticLookupHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDiagnosticLookup_RateLimitedAfterCap(t *testing.T) {
	store := newFakeDiagStore()
	store.addRow("rid-rl", "oidc_exchange_failed")
	s, _ := diagHandlerServer(t, store)
	sess := adminSudoSession()
	for i := range diagnosticLookupLimit {
		req := diagGetReq("/api/prohibitorum/diagnostics/rid-rl", sess)
		rec := httptest.NewRecorder()
		s.handleAdminDiagnosticLookupHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d rate-limited early (limit=%d)", i+1, diagnosticLookupLimit)
		}
	}
	// Next request must be rate-limited.
	req := diagGetReq("/api/prohibitorum/diagnostics/rid-rl", sess)
	rec := httptest.NewRecorder()
	s.handleAdminDiagnosticLookupHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 (rate limited)", rec.Code)
	}
}

func TestDiagnosticLookup_EmitsAuditEvent(t *testing.T) {
	store := newFakeDiagStore()
	store.addRow("rid-audit", "oidc_exchange_failed")
	s, aw := diagHandlerServer(t, store)
	sess := adminSudoSession()
	req := diagGetReq("/api/prohibitorum/diagnostics/rid-audit", sess)
	rec := httptest.NewRecorder()
	s.handleAdminDiagnosticLookupHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	found := false
	for _, ev := range aw.events {
		if ev.Event == audit.EventDiagnosticLookup {
			found = true
			if ev.Detail == nil {
				t.Fatal("audit event has nil Detail")
			}
			if ev.Detail["requestId"] != "rid-audit" {
				t.Fatalf("audit detail requestId = %v", ev.Detail["requestId"])
			}
		}
	}
	if !found {
		t.Fatal("diagnostic_lookup audit event was not emitted")
	}
}

func TestDiagnosticLookup_SecondLookupAlsoAudits(t *testing.T) {
	store := newFakeDiagStore()
	store.addRow("rid-2x", "oidc_exchange_failed")
	s, aw := diagHandlerServer(t, store)
	sess := adminSudoSession()
	for i := range 2 {
		req := diagGetReq("/api/prohibitorum/diagnostics/rid-2x", sess)
		rec := httptest.NewRecorder()
		s.handleAdminDiagnosticLookupHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("lookup %d: status = %d, want 200", i+1, rec.Code)
		}
	}
	count := 0
	for _, ev := range aw.events {
		if ev.Event == audit.EventDiagnosticLookup {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("audit events = %d, want 2", count)
	}
}

func TestDiagnosticLookup_ResponseContainsNoRawErrorOrSecret(t *testing.T) {
	store := newFakeDiagStore()
	store.addRow("rid-safe", "oidc_exchange_failed")
	s, _ := diagHandlerServer(t, store)
	sess := adminSudoSession()
	req := diagGetReq("/api/prohibitorum/diagnostics/rid-safe", sess)
	rec := httptest.NewRecorder()
	s.handleAdminDiagnosticLookupHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "postgres://") {
		t.Fatalf("response body leaked DSN: %s", body)
	}
	if strings.Contains(body, "secret") {
		t.Fatalf("response body leaked secret: %s", body)
	}
	if strings.Contains(body, "rawCause") {
		t.Fatalf("response body leaked rawCause: %s", body)
	}
}

// TestDiagnosticLookup_NoBulkRouteRegistered verifies that only the exact-ID
// GET route exists — no list/bulk/enumeration endpoint is registered.
func TestDiagnosticLookup_NoBulkRouteRegistered(t *testing.T) {
	router, _ := realAdminOnlyRouter(t)
	// The exact-ID route must be registered.
	req := reqWithSession(http.MethodGet, "/api/prohibitorum/diagnostics/rid", "", "", adminSession(time.Time{}))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	// Should be 401 (sudo_required) — not 404 — proving the route is registered
	// and the sudo gate fires. A 404 means the route was never registered.
	if rec.Code == http.StatusNotFound {
		t.Fatalf("exact-ID diagnostic route not registered (got 404)")
	}

	// A bulk/list route must NOT exist.
	req2 := reqWithSession(http.MethodGet, "/api/prohibitorum/diagnostics", "", "", adminSudoSession())
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	// 404 is expected — no list endpoint.
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("list route returned %d, want 404 (no bulk endpoint)", rec2.Code)
	}
}
