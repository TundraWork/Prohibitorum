package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOIDCSeedProviderConfigUsesCurrentPluginSchema(t *testing.T) {
	raw, err := oidcSeedProviderConfig("https://issuer.example", "client", []string{"example.com"}, true, true)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if len(config) != 10 || config["issuerUrl"] != "https://issuer.example" || config["clientId"] != "client" || config["pictureClaim"] != "picture" || config["requireVerifiedEmail"] != true || config["allowPrivateNetwork"] != true {
		t.Fatalf("config = %#v", config)
	}
}

func TestBeginEnrollmentRedactsBearerTokenAndResponse(t *testing.T) {
	const token = "enrollment-secret-token"
	assertError := func(t *testing.T, c *client, want string) {
		t.Helper()
		_, err := c.beginEnrollment(token, "user", "User", "passkey")
		if err == nil {
			t.Fatal("beginEnrollment unexpectedly succeeded")
		}
		if got := err.Error(); got != want {
			t.Fatalf("error = %q; want %q", got, want)
		}
		for _, secret := range []string{token, "/enrollments/", "sensitive response"} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error exposed %q: %v", secret, err)
			}
		}
	}

	t.Run("non-2xx", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "sensitive response at "+r.URL.Path, http.StatusInternalServerError)
		}))
		defer server.Close()
		c, err := newClient(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		assertError(t, c, "enrollment register/begin: unexpected HTTP status 500")
	})

	t.Run("decode", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"challenge":"sensitive response"`))
		}))
		defer server.Close()
		c, err := newClient(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		assertError(t, c, "enrollment register/begin: invalid JSON response")
	})

	t.Run("transport", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		c, err := newClient(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		server.Close()
		assertError(t, c, "enrollment register/begin: transport failed")
	})
}

func TestEnrollmentPreviewRedactsBearerTokenAndResponse(t *testing.T) {
	const token = "preview-secret-token"
	assertError := func(t *testing.T, c *client, want string) {
		t.Helper()
		v := &vrchatSmoke{}
		_, _, err := v.preview(c, token)
		if err == nil {
			t.Fatal("preview unexpectedly succeeded")
		}
		if got := err.Error(); got != want {
			t.Fatalf("error = %q; want %q", got, want)
		}
		for _, secret := range []string{token, "/enrollments/", "sensitive response"} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error exposed %q: %v", secret, err)
			}
		}
	}

	t.Run("non-2xx", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "sensitive response at "+r.URL.Path, http.StatusInternalServerError)
		}))
		defer server.Close()
		c, err := newClient(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		assertError(t, c, "enrollment preview: unexpected HTTP status 500")
	})

	t.Run("decode", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"intent":"sensitive response"`))
		}))
		defer server.Close()
		c, err := newClient(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		assertError(t, c, "enrollment preview: invalid JSON response")
	})

	t.Run("transport", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		c, err := newClient(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		server.Close()
		assertError(t, c, "enrollment preview: transport failed")
	})
}
