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
		{"non-admin sign-in wallpaper allowlisted", true, sessionForRole("user"), "GET", "/branding/wallpaper", http.StatusOK},
		{"anonymous sign-in image allowlisted", true, nil, "GET", "/branding/login-images/1", http.StatusOK},
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

// TestPublicConfig_Maintenance verifies the maintenance flag + the multilingual
// notice reach the public /config payload (the SPA's source of truth for the
// maintenance screen).
func TestPublicConfig_Maintenance(t *testing.T) {
	s := &Server{branding: branding.NewWithStore("TestCo", &fakeBrandingStore{maint: true, maintNotice: branding.MaintenanceNotice{
		Messages:      map[string]string{"en": "Back at 17:00 UTC"},
		DefaultLocale: "en",
	}})}
	rec := httptest.NewRecorder()
	s.handleGetPublicConfigHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/prohibitorum/config", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`"maintenanceMode":true`,
		`"maintenanceMessages":{"en":"Back at 17:00 UTC"}`,
		`"maintenanceDefaultLocale":"en"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("config body missing %q: %s", want, body)
		}
	}
}

// TestPublicConfig_MaintenanceNoNotice pins `{}` (never null) for an instance
// without notices.
func TestPublicConfig_MaintenanceNoNotice(t *testing.T) {
	s := &Server{branding: branding.NewWithStore("TestCo", &fakeBrandingStore{maint: true})}
	rec := httptest.NewRecorder()
	s.handleGetPublicConfigHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/prohibitorum/config", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `"maintenanceMessages":{}`) {
		t.Fatalf("config body missing empty maintenanceMessages: %s", body)
	}
}

// TestAdminSettings_PutMaintenance verifies the toggle persists the flag and
// the multilingual notice, and the resolver reflects them immediately.
func TestAdminSettings_PutMaintenance(t *testing.T) {
	t.Parallel()

	st := &settingsBrandingStore{}
	s := &Server{branding: branding.NewWithStore("TestDefault", st), Audit: noopAuditWriter{}}

	sess := adminSession(time.Now().Add(time.Hour)) // fresh sudo
	req := reqWithSession("PUT", "/api/prohibitorum/admin/settings/maintenance",
		`{"maintenanceMode":true,"maintenanceMessages":{"en":"Upgrading","zh":"升级中"},"maintenanceDefaultLocale":"en"}`, "", sess)
	rr := httptest.NewRecorder()
	s.handlePutMaintenanceHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body: %s", rr.Code, rr.Body.String())
	}
	want := branding.MaintenanceNotice{
		Messages:      map[string]string{"en": "Upgrading", "zh": "升级中"},
		DefaultLocale: "en",
	}
	if !st.maint || !noticeEqual(st.maintNotice, want) {
		t.Fatalf("store maintenance = (%v,%+v), want (true,%+v)", st.maint, st.maintNotice, want)
	}
	if on, notice := s.branding.Maintenance(context.Background()); !on || !noticeEqual(notice, want) {
		t.Errorf("resolver Maintenance = (%v,%+v), want (true,%+v)", on, notice, want)
	}
}

func noticeEqual(a, b branding.MaintenanceNotice) bool {
	if a.DefaultLocale != b.DefaultLocale || len(a.Messages) != len(b.Messages) {
		return false
	}
	for locale, text := range a.Messages {
		if b.Messages[locale] != text {
			return false
		}
	}
	return true
}

// TestAdminSettings_PutMaintenance_Refused checks the 400 paths: unknown or
// missing default language, an unknown language key, and a notice over the
// rune limit are bad_request; a notice that skips the default language is
// maintenance_default_notice_required. Nothing reaches the store.
func TestAdminSettings_PutMaintenance_Refused(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		code string
	}{
		{
			"unknown message locale",
			`{"maintenanceMode":true,"maintenanceMessages":{"fr":"Bonjour"},"maintenanceDefaultLocale":"en"}`,
			"bad_request",
		},
		{
			"missing default locale",
			`{"maintenanceMode":true,"maintenanceMessages":{"en":"Hi"}}`,
			"bad_request",
		},
		{
			"unknown default locale",
			`{"maintenanceMode":true,"maintenanceMessages":{"en":"Hi"},"maintenanceDefaultLocale":"fr"}`,
			"bad_request",
		},
		{
			"notice over the rune limit",
			`{"maintenanceMode":true,"maintenanceMessages":{"en":"` + strings.Repeat("x", 501) + `"},"maintenanceDefaultLocale":"en"}`,
			"bad_request",
		},
		{
			"notice without the default language",
			`{"maintenanceMode":true,"maintenanceMessages":{"zh":"维护中"},"maintenanceDefaultLocale":"en"}`,
			"maintenance_default_notice_required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			st := &settingsBrandingStore{}
			s := &Server{branding: branding.NewWithStore("TestDefault", st), Audit: noopAuditWriter{}}

			sess := adminSession(time.Now().Add(time.Hour))
			req := reqWithSession("PUT", "/api/prohibitorum/admin/settings/maintenance", tc.body, "", sess)
			rr := httptest.NewRecorder()
			s.handlePutMaintenanceHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("body = %s, want code %q", rr.Body.String(), tc.code)
			}
			if st.maint {
				t.Error("store mutated despite invalid request")
			}
		})
	}
}
