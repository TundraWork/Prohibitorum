package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"prohibitorum/pkg/branding"
)

type fakeBrandingStore struct {
	name        *string
	icon        []byte
	etag        *string
	maint       bool
	maintMsg    *string
	loginBG     []byte
	loginBGEtag *string
}

func (f *fakeBrandingStore) Get(context.Context) (branding.Settings, error) {
	return branding.Settings{
		Name: f.name, IconPNG: f.icon, IconEtag: f.etag,
		Maintenance: f.maint, MaintenanceMessage: f.maintMsg,
		LoginBG: f.loginBG, LoginBGEtag: f.loginBGEtag,
	}, nil
}
func (f *fakeBrandingStore) SetName(context.Context, *string) error              { return nil }
func (f *fakeBrandingStore) SetIcon(context.Context, []byte, string) error       { return nil }
func (f *fakeBrandingStore) ClearIcon(context.Context) error                     { return nil }
func (f *fakeBrandingStore) SetMaintenance(context.Context, bool, *string) error { return nil }
func (f *fakeBrandingStore) SetLoginBG(context.Context, []byte, string) error    { return nil }
func (f *fakeBrandingStore) ClearLoginBG(context.Context) error                  { return nil }

func TestBrandingConfigEndpoint(t *testing.T) {
	s := &Server{branding: branding.NewWithStore("TestCo", &fakeBrandingStore{})}
	rec := httptest.NewRecorder()
	s.handleGetPublicConfigHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/prohibitorum/config", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`"instanceName":"TestCo"`, `"iconUrl":"/branding/icon"`, `"hasCustomIcon":false`, `"hasCustomBackground":false`, `"backgroundUrl":"/branding/background"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}

func TestBrandingIconDefault_AndETag304(t *testing.T) {
	s := &Server{branding: branding.NewWithStore("TestCo", &fakeBrandingStore{})}
	rec := httptest.NewRecorder()
	s.handleGetBrandingIconHTTP(rec, httptest.NewRequest(http.MethodGet, "/branding/icon", nil))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("status=%d ct=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}
	req := httptest.NewRequest(http.MethodGet, "/branding/icon", nil)
	req.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()
	s.handleGetBrandingIconHTTP(rec2, req)
	if rec2.Code != http.StatusNotModified {
		t.Fatalf("conditional: status %d want 304", rec2.Code)
	}
}

func TestBrandingBackground_404WhenUnset(t *testing.T) {
	s := &Server{branding: branding.NewWithStore("TestCo", &fakeBrandingStore{})}
	rec := httptest.NewRecorder()
	s.handleGetBrandingBackgroundHTTP(rec, httptest.NewRequest(http.MethodGet, "/branding/background", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
