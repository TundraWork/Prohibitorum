// Package server — handle_admin_settings_test.go
//
// Unit tests for the admin instance-branding endpoints. These tests are
// DB-free: they exercise the handler logic directly using the fakeBrandingStore
// from handle_branding_test.go (same package) and a noop audit writer.
package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/branding"
)

// noopAuditWriter satisfies audit.Writer without touching a DB.
type noopAuditWriter struct{}

func (noopAuditWriter) Record(context.Context, audit.Record) error { return nil }

// settingsBrandingStore is a fakeBrandingStore variant that records mutations
// so tests can assert the correct method was called.
type settingsBrandingStore struct {
	name    *string
	icon    []byte
	etag    *string
	cleared bool
}

func (f *settingsBrandingStore) Get(context.Context) (branding.Settings, error) {
	return branding.Settings{Name: f.name, IconPNG: f.icon, IconEtag: f.etag}, nil
}
func (f *settingsBrandingStore) SetName(_ context.Context, name *string) error {
	f.name = name
	return nil
}
func (f *settingsBrandingStore) SetIcon(_ context.Context, png []byte, etag string) error {
	f.icon = png
	e := etag
	f.etag = &e
	return nil
}
func (f *settingsBrandingStore) ClearIcon(_ context.Context) error {
	f.cleared = true
	f.icon = nil
	f.etag = nil
	return nil
}

// TestAdminSettings_PutName verifies that a valid instanceName JSON body sets
// the branding name and returns 204.
func TestAdminSettings_PutName(t *testing.T) {
	t.Parallel()

	st := &settingsBrandingStore{}
	s := &Server{
		branding: branding.NewWithStore("TestDefault", st),
		Audit:    noopAuditWriter{},
	}

	sess := adminSession(time.Now().Add(time.Hour)) // fresh sudo
	req := reqWithSession("PUT", "/api/prohibitorum/admin/settings",
		`{"instanceName":"Acme SSO"}`, "", sess)
	rr := httptest.NewRecorder()
	s.handlePutInstanceNameHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body: %s", rr.Code, rr.Body.String())
	}
	if got := s.branding.InstanceName(context.Background()); got != "Acme SSO" {
		t.Errorf("InstanceName = %q, want %q", got, "Acme SSO")
	}
}

// TestAdminSettings_PutName_TooLong verifies that a 65-rune instanceName is
// rejected with 400 before touching the store.
func TestAdminSettings_PutName_TooLong(t *testing.T) {
	t.Parallel()

	st := &settingsBrandingStore{}
	s := &Server{
		branding: branding.NewWithStore("TestDefault", st),
		Audit:    noopAuditWriter{},
	}

	longName := strings.Repeat("x", 65)
	sess := adminSession(time.Now().Add(time.Hour))
	req := reqWithSession("PUT", "/api/prohibitorum/admin/settings",
		`{"instanceName":"`+longName+`"}`, "", sess)
	rr := httptest.NewRecorder()
	s.handlePutInstanceNameHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rr.Code, rr.Body.String())
	}
	// Store must not have been touched.
	if st.name != nil {
		t.Error("store.SetName was called despite validation failure")
	}
}

// TestAdminSettings_DeleteIcon verifies that DELETE clears the icon and returns
// 204.
func TestAdminSettings_DeleteIcon(t *testing.T) {
	t.Parallel()

	icon := []byte("fake-png-bytes")
	etag := "abc123"
	st := &settingsBrandingStore{icon: icon, etag: &etag}
	s := &Server{
		branding: branding.NewWithStore("TestDefault", st),
		Audit:    noopAuditWriter{},
	}

	sess := adminSession(time.Now().Add(time.Hour))
	req := reqWithSession("DELETE", "/api/prohibitorum/admin/settings/icon", "", "", sess)
	rr := httptest.NewRecorder()
	s.handleDeleteInstanceIconHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body: %s", rr.Code, rr.Body.String())
	}
	if !st.cleared {
		t.Error("store.ClearIcon was not called")
	}
}
