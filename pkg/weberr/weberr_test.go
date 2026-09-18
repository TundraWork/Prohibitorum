package weberr

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

func TestNewRef_Format(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}$`)
	a, b := NewRef(), NewRef()
	if !re.MatchString(a) || !re.MatchString(b) {
		t.Fatalf("ref not 8 hex chars: %q %q", a, b)
	}
	if a == b {
		t.Fatalf("two refs collided: %q", a)
	}
}

func TestRedirectToError(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?x=1", nil)
	RedirectToError(rec, req, "invalid_client", "deadbeef")
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if got, want := rec.Header().Get("Location"), "/error?error=invalid_client&ref=deadbeef"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
}

func TestRedirectToErrorWithReturn(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	RedirectToErrorWithReturn(rec, req, "server_error", "deadbeef", "/connected")
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if got, want := rec.Header().Get("Location"), "/error?error=server_error&ref=deadbeef&return_to=%2Fconnected"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

func TestRedirectToErrorWithReturn_EmptyOmitsParam(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	RedirectToErrorWithReturn(rec, req, "server_error", "deadbeef", "")
	if got, want := rec.Header().Get("Location"), "/error?error=server_error&ref=deadbeef"; got != want {
		t.Fatalf("Location = %q, want %q (return_to must be omitted when empty)", got, want)
	}
}

func TestRedirectToErrorWithReturnAndDetail_EncodesCanonicalValue(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	RedirectToErrorWithReturnAndDetail(rec, req, "federation_identity_conflict", "deadbeef", "", "federationName", "A&B + <身份>")

	got := rec.Header().Get("Location")
	want := "/error?error=federation_identity_conflict&ref=deadbeef&federationName=A%26B+%2B+%3C%E8%BA%AB%E4%BB%BD%3E"
	if got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

func TestRedirectToErrorWithReturnAndDetail_OmitsEmptyDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	RedirectToErrorWithReturnAndDetail(rec, req, "server_error", "deadbeef", "", "federationName", "")
	if got, want := rec.Header().Get("Location"), "/error?error=server_error&ref=deadbeef"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}
