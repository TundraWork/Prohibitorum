// Package server — maintenance_test.go
//
// DB-free unit tests for maintenance-mode enforcement: the request gate, the
// public-config exposure, and the admin toggle. End-to-end coverage (non-admin
// login/dashboard/gateway blocked while an admin works) lives in cmd/smoke.
package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/branding"
	"prohibitorum/pkg/db"
)

func maintenanceResolver(on bool) *branding.Resolver {
	return branding.NewWithStore("Test", &fakeBrandingStore{maint: on})
}

func sessionForRole(role string) *authn.Session {
	return &authn.Session{Account: &db.Account{ID: 1, Role: role}}
}

// TestMaintenanceGate exercises the request gate across the surfaces it
// classifies: blocked API, allowlisted reads, static shell, browser-nav SSO,
// admin bypass, and the maintenance-off / unauthenticated pass-throughs.
func TestMaintenanceGate(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	cases := []struct {
		name       string
		on         bool
		sess       *authn.Session
		method     string
		path       string
		wantStatus int
	}{
		{"non-admin blocked on dashboard mutation", true, sessionForRole("user"), "POST", "/api/prohibitorum/me/tokens", http.StatusServiceUnavailable},
		{"admin bypasses", true, sessionForRole("admin"), "POST", "/api/prohibitorum/me/tokens", http.StatusOK},
		{"unauthenticated passes", true, nil, "POST", "/api/prohibitorum/me/tokens", http.StatusOK},
		{"maintenance off passes", false, sessionForRole("user"), "POST", "/api/prohibitorum/me/tokens", http.StatusOK},
		{"non-admin GET /me allowlisted", true, sessionForRole("user"), "GET", "/api/prohibitorum/me", http.StatusOK},
		{"non-admin GET /config allowlisted", true, sessionForRole("user"), "GET", "/api/prohibitorum/config", http.StatusOK},
		{"non-admin logout allowlisted", true, sessionForRole("user"), "POST", "/api/prohibitorum/auth/logout", http.StatusOK},
		{"non-admin static shell passes", true, sessionForRole("user"), "GET", "/security", http.StatusOK},
		{"non-admin avatar allowlisted", true, sessionForRole("user"), "GET", "/avatar/abc", http.StatusOK},
		{"non-admin OIDC authorize redirects", true, sessionForRole("user"), "GET", "/oauth/authorize", http.StatusFound},
		{"non-admin SAML SSO redirects", true, sessionForRole("user"), "POST", "/saml/sso", http.StatusFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := maintenanceGateMW(maintenanceResolver(tc.on))(okHandler)
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.sess != nil {
				req = req.WithContext(authn.WithSession(req.Context(), tc.sess))
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", rr.Code, tc.wantStatus, rr.Body.String())
			}
		})
	}
}

// TestPublicConfig_Maintenance verifies the maintenance flag + message reach the
// public /config payload (the SPA's source of truth for the maintenance screen).
func TestPublicConfig_Maintenance(t *testing.T) {
	msg := "Back at 17:00 UTC"
	s := &Server{branding: branding.NewWithStore("TestCo", &fakeBrandingStore{maint: true, maintMsg: &msg})}
	rec := httptest.NewRecorder()
	s.handleGetPublicConfigHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/prohibitorum/config", nil))
	body := rec.Body.String()
	for _, want := range []string{`"maintenanceMode":true`, `"maintenanceMessage":"Back at 17:00 UTC"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("config body missing %q: %s", want, body)
		}
	}
}

// TestAdminSettings_PutMaintenance verifies the sudo-gated toggle persists the
// flag + message and the resolver reflects them immediately.
func TestAdminSettings_PutMaintenance(t *testing.T) {
	t.Parallel()

	st := &settingsBrandingStore{}
	s := &Server{branding: branding.NewWithStore("TestDefault", st), Audit: noopAuditWriter{}}

	sess := adminSession(time.Now().Add(time.Hour)) // fresh sudo
	req := reqWithSession("PUT", "/api/prohibitorum/admin/settings/maintenance",
		`{"maintenanceMode":true,"maintenanceMessage":"Upgrading"}`, "", sess)
	rr := httptest.NewRecorder()
	s.handlePutMaintenanceHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body: %s", rr.Code, rr.Body.String())
	}
	if !st.maint || st.maintMsg == nil || *st.maintMsg != "Upgrading" {
		t.Fatalf("store maintenance = (%v,%v), want (true,Upgrading)", st.maint, st.maintMsg)
	}
	if on, msg := s.branding.Maintenance(context.Background()); !on || msg != "Upgrading" {
		t.Errorf("resolver Maintenance = (%v,%q), want (true,Upgrading)", on, msg)
	}
}

// TestAdminSettings_PutMaintenance_MessageTooLong rejects a >500-rune message
// with 400 before touching the store.
func TestAdminSettings_PutMaintenance_MessageTooLong(t *testing.T) {
	t.Parallel()

	st := &settingsBrandingStore{}
	s := &Server{branding: branding.NewWithStore("TestDefault", st), Audit: noopAuditWriter{}}

	sess := adminSession(time.Now().Add(time.Hour))
	req := reqWithSession("PUT", "/api/prohibitorum/admin/settings/maintenance",
		`{"maintenanceMode":true,"maintenanceMessage":"`+strings.Repeat("x", 501)+`"}`, "", sess)
	rr := httptest.NewRecorder()
	s.handlePutMaintenanceHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if st.maint {
		t.Error("store mutated despite invalid request")
	}
}
