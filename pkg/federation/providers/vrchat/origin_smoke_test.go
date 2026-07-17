//go:build smoke

package vrchat

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSmokeOriginValidationAndTrustedRequest(t *testing.T) {
	t.Run("missing origin", func(t *testing.T) {
		t.Setenv("PROHIBITORUM_VRCHAT_SMOKE_ORIGIN", "")
		t.Setenv("PROHIBITORUM_VRCHAT_SMOKE_CA_FILE", "")
		if _, err := NewClient("test", "https://public.test"); err == nil {
			t.Fatal("missing origin accepted")
		}
	})
	for _, raw := range []string{
		"http://127.0.0.1/api/1", "https://example.com/api/1", "https://user@127.0.0.1/api/1",
		"https://127.0.0.1/api/2", "https://127.0.0.1/api/1?q=x", "https://127.0.0.1/api/1#x",
	} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("PROHIBITORUM_VRCHAT_SMOKE_ORIGIN", raw)
			t.Setenv("PROHIBITORUM_VRCHAT_SMOKE_CA_FILE", "missing")
			if _, err := NewClient("test", "https://public.test"); err == nil {
				t.Fatal("invalid origin accepted")
			}
		})
	}
	t.Run("missing and invalid CA", func(t *testing.T) {
		t.Setenv("PROHIBITORUM_VRCHAT_SMOKE_ORIGIN", "https://127.0.0.1:4443/api/1")
		t.Setenv("PROHIBITORUM_VRCHAT_SMOKE_CA_FILE", "")
		if _, err := NewClient("test", "https://public.test"); err == nil {
			t.Fatal("missing CA accepted")
		}
		file := filepath.Join(t.TempDir(), "ca.pem")
		if err := os.WriteFile(file, []byte("not a certificate"), 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PROHIBITORUM_VRCHAT_SMOKE_CA_FILE", file)
		if _, err := NewClient("test", "https://public.test"); err == nil {
			t.Fatal("invalid CA accepted")
		}
	})
	t.Run("trusted request", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/1/auth/user" {
				t.Errorf("path = %q", r.URL.Path)
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"usr_12345678-1234-1234-1234-123456789abc","displayName":"Smoke"}`))
		}))
		defer server.Close()
		cert := server.Certificate()
		if cert == nil {
			t.Fatal("server has no certificate")
		}
		caFile := filepath.Join(t.TempDir(), "ca.pem")
		if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := x509.ParseCertificate(cert.Raw); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PROHIBITORUM_VRCHAT_SMOKE_CA_FILE", caFile)
		t.Setenv("PROHIBITORUM_VRCHAT_SMOKE_ORIGIN", server.URL+"/api/1?")
		if _, err := NewClient("test", "https://public.test"); err == nil {
			t.Fatal("trailing empty query accepted")
		}
		t.Setenv("PROHIBITORUM_VRCHAT_SMOKE_ORIGIN", server.URL+"/api/1")
		client, err := NewClient("test", "https://public.test")
		if err != nil {
			t.Fatal(err)
		}
		user, _, err := client.CurrentUser(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if user.ID != "usr_12345678-1234-1234-1234-123456789abc" {
			t.Fatalf("user = %#v", user)
		}
	})
}
