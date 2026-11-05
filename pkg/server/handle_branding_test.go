package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"prohibitorum/pkg/branding"
	"prohibitorum/pkg/configx"
)

type fakeBrandingStore struct {
	name        *string
	icon        []byte
	etag        *string
	maint       bool
	maintNotice branding.MaintenanceNotice
}

func (f *fakeBrandingStore) Get(context.Context) (branding.Settings, error) {
	return branding.Settings{
		Name: f.name, IconPNG: f.icon, IconEtag: f.etag,
		Maintenance: f.maint, MaintenanceNotice: f.maintNotice,
	}, nil
}
func (f *fakeBrandingStore) SetName(context.Context, *string) error        { return nil }
func (f *fakeBrandingStore) SetIcon(context.Context, []byte, string) error { return nil }
func (f *fakeBrandingStore) ClearIcon(context.Context) error               { return nil }
func (f *fakeBrandingStore) SetMaintenance(context.Context, bool, branding.MaintenanceNotice) error {
	return nil
}
func (f *fakeBrandingStore) SetAppearance(context.Context, branding.Appearance, branding.NewKeys) error {
	return nil
}
func (f *fakeBrandingStore) ClearUnsplashKey(context.Context) error { return nil }
func (f *fakeBrandingStore) ClearPixabayKey(context.Context) error  { return nil }
func (f *fakeBrandingStore) AddImage(context.Context, []byte, string) (branding.ImageRef, error) {
	return branding.ImageRef{}, nil
}
func (f *fakeBrandingStore) DeleteImage(context.Context, int64) error {
	return branding.ErrImageNotFound
}
func (f *fakeBrandingStore) ImageData(context.Context, int64) ([]byte, string, error) {
	return nil, "", branding.ErrImageNotFound
}

func TestBrandingConfigEndpoint(t *testing.T) {
	s := &Server{
		branding: branding.NewWithStore("TestCo", &fakeBrandingStore{}),
		config: &configx.Config{TOTP: configx.TOTPConfig{
			Issuer: "TestCo Auth", DefaultAlgorithm: "SHA256", DefaultDigits: 8, DefaultPeriod: 45,
		}},
	}
	rec := httptest.NewRecorder()
	s.handleGetPublicConfigHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/prohibitorum/config", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`"instanceName":"TestCo"`, `"iconUrl":"/branding/icon"`, `"hasCustomIcon":false`, `"loginAppearance":{"background":{"source":"none"`, `"loginImages":[]`, `"totp":{"issuer":"TestCo Auth","algorithm":"SHA256","digits":8,"period":45}`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}

// TestBrandingConfigEndpoint_TOTPIssuerFollowsInstanceName pins the fallback
// for a deployment without totp.issuer: the effective instance name, including
// a name saved in the settings, rather than the configured default.
func TestBrandingConfigEndpoint_TOTPIssuerFollowsInstanceName(t *testing.T) {
	saved := "Saved Name"
	s := &Server{
		branding: branding.NewWithStore("Config Name", &fakeBrandingStore{name: &saved}),
		config: &configx.Config{
			TOTP:     configx.TOTPConfig{DefaultAlgorithm: "SHA1", DefaultDigits: 6, DefaultPeriod: 30},
			WebAuthn: configx.WebAuthnConfig{RPDisplayName: "RP Name"},
		},
	}
	rec := httptest.NewRecorder()
	s.handleGetPublicConfigHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/prohibitorum/config", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if want := `"totp":{"issuer":"Saved Name",`; !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("body missing %q: %s", want, rec.Body.String())
	}
}

// TestWebAuthnRPName pins the RP display name choice: the configured name when
// set, else the effective instance name.
func TestWebAuthnRPName(t *testing.T) {
	saved := "Saved Name"
	resolver := branding.NewWithStore("Config Name", &fakeBrandingStore{name: &saved})
	s := &Server{branding: resolver, config: &configx.Config{}}
	if got := s.webauthnRPName(context.Background()); got != saved {
		t.Fatalf("unset rp_display_name: got %q, want %q", got, saved)
	}
	s.config.WebAuthn.RPDisplayName = "RP Name"
	if got := s.webauthnRPName(context.Background()); got != "RP Name" {
		t.Fatalf("explicit rp_display_name: got %q, want %q", got, "RP Name")
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
