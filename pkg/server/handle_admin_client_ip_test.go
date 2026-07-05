package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"prohibitorum/pkg/clientip"
)

// memClientIPStore is an in-memory clientip.Store for testing (no DB needed).
type memClientIPStore struct{ s clientip.Stored }

func (m *memClientIPStore) Get(context.Context) (clientip.Stored, error) { return m.s, nil }
func (m *memClientIPStore) Set(_ context.Context, s clientip.Stored) error {
	m.s = s
	return nil
}

func TestClientIPPutValidation(t *testing.T) {
	t.Parallel()

	s := &Server{clientIP: clientip.NewResolver(&memClientIPStore{})}

	// header strategy with empty header name must be rejected.
	bad := `{"strategy":"header","header":"","trustedProxies":[]}`
	req := httptest.NewRequest(http.MethodPut, "/api/prohibitorum/admin/settings/client-ip", strings.NewReader(bad))
	rec := httptest.NewRecorder()
	s.handlePutClientIPHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad header strategy: status = %d, want 400", rec.Code)
	}

	// invalid CIDR must be rejected.
	badCIDR := `{"strategy":"forwarded","trustedProxies":["nope"]}`
	req2 := httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(badCIDR))
	rec2 := httptest.NewRecorder()
	s.handlePutClientIPHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("bad CIDR: status = %d, want 400", rec2.Code)
	}
}

func TestClientIPPutGetRoundTrip(t *testing.T) {
	t.Parallel()

	// The success path calls s.auditBranding -> s.Audit.Record(...); use the
	// noopAuditWriter defined in handle_admin_settings_test.go (same package).
	s := &Server{
		clientIP: clientip.NewResolver(&memClientIPStore{}),
		Audit:    noopAuditWriter{},
	}

	good := `{"strategy":"header","header":"CF-Connecting-IP","trustedProxies":["203.0.113.0/24"]}`
	req := httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(good))
	rec := httptest.NewRecorder()
	s.handlePutClientIPHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("valid PUT: status = %d, want 204; body: %s", rec.Code, rec.Body.String())
	}

	getRec := httptest.NewRecorder()
	s.handleGetClientIPHTTP(getRec, httptest.NewRequest(http.MethodGet, "/x", nil))
	body := getRec.Body.String()
	for _, want := range []string{`"strategy":"header"`, `"CF-Connecting-IP"`, `"203.0.113.0/24"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("GET body %q missing %q", body, want)
		}
	}
}
